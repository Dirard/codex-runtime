package workflowstorage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
)

const (
	SchemaVersion       = 1
	DefaultMaxBytes     = 10 * 1024 * 1024
	DefaultMaxFileBytes = 2 * 1024 * 1024
)

var (
	ErrInvalidPackage = errors.New("workflow package invalid")

	publicIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	secretPatterns  = []secretPattern{
		{name: "private_key", pattern: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
		{name: "authorization_bearer", pattern: regexp.MustCompile(`(?i)authorization\s*:\s*bearer\s+[A-Za-z0-9._~+/=-]{8,}`)},
		{name: "openai_api_key", pattern: regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`)},
		{name: "github_token", pattern: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`)},
	}
)

type PackageLimits struct {
	MaxBytes     int64
	MaxFileBytes int64
}

type Package struct {
	Namespace          string
	WorkflowID         string
	PackageFingerprint string
	Files              []PackageFile
	TotalSizeBytes     int64
}

type PackageFile struct {
	Path       string
	Contents   []byte
	SizeBytes  int64
	SHA256     string
	Executable bool
}

type PackageError struct {
	Reason      string
	DisplayPath string
	NextAction  string
	err         error
}

func (e *PackageError) Error() string {
	if e == nil {
		return ""
	}
	message := e.Reason
	if message == "" {
		message = "invalid_workflow_package"
	}
	if e.DisplayPath != "" {
		message += ": " + e.DisplayPath
	}
	if e.NextAction != "" {
		message += ": " + e.NextAction
	}
	return message
}

func (e *PackageError) Unwrap() error {
	if e == nil || e.err == nil {
		return ErrInvalidPackage
	}
	return e.err
}

func (e *PackageError) Is(target error) bool {
	return target == ErrInvalidPackage
}

func ValidateProtoPackage(input *pb.WorkflowPackage, limits PackageLimits) (*Package, error) {
	limits = normalizeLimits(limits)
	if input == nil {
		return nil, packageError("package_required", "", "send a workflow package", nil)
	}
	selector := input.GetWorkflow()
	namespace := strings.TrimSpace(selector.GetNamespace())
	workflowID := strings.TrimSpace(selector.GetWorkflowId())
	if err := validateIdentity(namespace, workflowID); err != nil {
		return nil, err
	}
	if input.GetSchemaVersion() != SchemaVersion {
		return nil, packageError("schema_version_unsupported", "", "send workflow package schema version 1", nil)
	}
	if input.GetPackageFingerprint() == "" {
		return nil, packageError("fingerprint_required", "", "send canonical package fingerprint", nil)
	}

	files := make([]PackageFile, 0, len(input.GetFiles()))
	for _, protoFile := range input.GetFiles() {
		normalized, err := validateRelativePath(protoFile.GetRelativePath())
		if err != nil {
			return nil, err
		}
		contents := protoFile.GetContents()
		if uint64(len(contents)) != protoFile.GetSizeBytes() {
			return nil, packageError("size_mismatch", normalized, "send size_bytes matching received file contents", nil)
		}
		if int64(len(contents)) > limits.MaxFileBytes {
			return nil, packageError("file_too_large", normalized, "reduce this workflow file or raise the explicit package limit", nil)
		}
		sum := sha256Hex(contents)
		if protoFile.GetSha256() != sum {
			return nil, packageError("hash_mismatch", normalized, "send sha256 matching received file contents", nil)
		}
		files = append(files, PackageFile{
			Path:       normalized,
			Contents:   append([]byte(nil), contents...),
			SizeBytes:  int64(len(contents)),
			SHA256:     sum,
			Executable: protoFile.GetExecutable(),
		})
	}
	return finalizePackage(namespace, workflowID, input.GetPackageFingerprint(), files, limits)
}

func normalizeLimits(limits PackageLimits) PackageLimits {
	if limits.MaxBytes == 0 {
		limits.MaxBytes = DefaultMaxBytes
	}
	if limits.MaxFileBytes == 0 {
		limits.MaxFileBytes = DefaultMaxFileBytes
	}
	return limits
}

func validateIdentity(namespace string, workflowID string) error {
	if namespace == "" {
		return packageError("namespace_required", "", "set namespace explicitly", nil)
	}
	if workflowID == "" {
		return packageError("workflow_id_required", "", "set workflow_id explicitly", nil)
	}
	if !publicIDPattern.MatchString(namespace) {
		return packageError("namespace_invalid", "", "use letters, digits, dot, underscore or dash", nil)
	}
	if !publicIDPattern.MatchString(workflowID) {
		return packageError("workflow_id_invalid", "", "use letters, digits, dot, underscore or dash", nil)
	}
	return nil
}

func validateRelativePath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.TrimSpace(name) == "" {
		return "", packageError("path_required", "", "workflow package file path is empty", nil)
	}
	if strings.HasPrefix(name, "/") || path.IsAbs(name) || strings.Contains(name, ":") {
		return "", packageError("absolute_path_rejected", safePackagePath(name), "use a relative workflow package path", nil)
	}
	clean := path.Clean(name)
	if clean == "." || clean != name {
		return "", packageError("path_not_clean", safePackagePath(name), "use clean relative path segments", nil)
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", packageError("traversal_rejected", safePackagePath(name), "remove parent-directory traversal", nil)
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", packageError("path_segment_rejected", safePackagePath(name), "use plain relative path segments", nil)
		}
		if reservedSegment(segment) {
			return "", packageError("reserved_path_rejected", safePackagePath(name), "remove reserved path segment", nil)
		}
	}
	return clean, nil
}

func finalizePackage(namespace string, workflowID string, fingerprint string, files []PackageFile, limits PackageLimits) (*Package, error) {
	if len(files) == 0 {
		return nil, packageError("package_empty", "", "include config.toml", nil)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	seen := map[string]string{}
	total := int64(0)
	hasConfig := false
	for _, file := range files {
		lower := strings.ToLower(file.Path)
		if previous, ok := seen[lower]; ok {
			return nil, packageError("duplicate_path_rejected", file.Path, fmt.Sprintf("case-conflicts with %s", previous), nil)
		}
		seen[lower] = file.Path
		if file.Path == "config.toml" {
			hasConfig = true
		}
		total += file.SizeBytes
		if total > limits.MaxBytes {
			return nil, packageError("package_too_large", "", "reduce workflow package contents or raise the explicit package limit", nil)
		}
		if finding := scanFileForSecrets(file); finding != "" {
			return nil, packageError("secret_like_content_rejected", file.Path, "replace raw secrets with env/file references", errors.New(finding))
		}
	}
	if !hasConfig {
		return nil, packageError("config_missing", "config.toml", "place config.toml at workflow root", nil)
	}
	recomputed, err := Fingerprint(files)
	if err != nil {
		return nil, err
	}
	if recomputed != fingerprint {
		return nil, packageError("fingerprint_mismatch", "", "send canonical package fingerprint computed from received files", nil)
	}
	return &Package{
		Namespace:          namespace,
		WorkflowID:         workflowID,
		PackageFingerprint: recomputed,
		Files:              cloneFiles(files),
		TotalSizeBytes:     total,
	}, nil
}

type fingerprintPayload struct {
	SchemaVersion int                      `json:"schema_version"`
	Files         []fingerprintFilePayload `json:"files"`
}

type fingerprintFilePayload struct {
	Path       string `json:"path"`
	SizeBytes  int64  `json:"size_bytes"`
	SHA256     string `json:"sha256"`
	Executable bool   `json:"executable"`
}

func Fingerprint(files []PackageFile) (string, error) {
	payload := fingerprintPayload{SchemaVersion: SchemaVersion}
	payload.Files = make([]fingerprintFilePayload, 0, len(files))
	for _, file := range files {
		payload.Files = append(payload.Files, fingerprintFilePayload{
			Path:       file.Path,
			SizeBytes:  file.SizeBytes,
			SHA256:     file.SHA256,
			Executable: file.Executable,
		})
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return sha256Hex(canonical), nil
}

func NewProtoPackage(namespace string, workflowID string, files []PackageFile) (*pb.WorkflowPackage, error) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for i := range files {
		files[i].SizeBytes = int64(len(files[i].Contents))
		files[i].SHA256 = sha256Hex(files[i].Contents)
	}
	fingerprint, err := Fingerprint(files)
	if err != nil {
		return nil, err
	}
	protoFiles := make([]*pb.WorkflowPackageFile, 0, len(files))
	total := uint64(0)
	for _, file := range files {
		total += uint64(len(file.Contents))
		protoFiles = append(protoFiles, &pb.WorkflowPackageFile{
			RelativePath: file.Path,
			Contents:     append([]byte(nil), file.Contents...),
			SizeBytes:    uint64(len(file.Contents)),
			Sha256:       file.SHA256,
			Executable:   file.Executable,
		})
	}
	return &pb.WorkflowPackage{
		SchemaVersion: SchemaVersion,
		Workflow: &pb.WorkflowSelector{
			Namespace:  namespace,
			WorkflowId: workflowID,
		},
		PackageFingerprint: fingerprint,
		Files:              protoFiles,
		TotalSizeBytes:     total,
	}, nil
}

func scanFileForSecrets(file PackageFile) string {
	if !looksTextual(file.Path, file.Contents) {
		return ""
	}
	text := string(file.Contents)
	for _, secret := range secretPatterns {
		if secret.pattern.MatchString(text) {
			return secret.name
		}
	}
	return ""
}

type secretPattern struct {
	name    string
	pattern *regexp.Regexp
}

func looksTextual(name string, contents []byte) bool {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".toml", ".md", ".txt", ".json", ".yaml", ".yml", ".env", ".sh", ".ps1":
		return true
	}
	for _, b := range contents {
		if b == 0 {
			return false
		}
	}
	return true
}

func reservedSegment(segment string) bool {
	lower := strings.ToLower(segment)
	if lower == ".git" || lower == ".supergoal" || lower == "node_modules" {
		return true
	}
	base := strings.TrimSuffix(lower, path.Ext(lower))
	switch base {
	case "con", "prn", "aux", "nul":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '1' && base[3] <= '9' {
		return true
	}
	return false
}

func cloneFiles(files []PackageFile) []PackageFile {
	cloned := make([]PackageFile, 0, len(files))
	for _, file := range files {
		file.Contents = append([]byte(nil), file.Contents...)
		cloned = append(cloned, file)
	}
	return cloned
}

func sha256Hex(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func safePackagePath(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimLeft(name, "/")
	parts := strings.Split(name, "/")
	safe := parts[:0]
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			continue
		}
		safe = append(safe, part)
	}
	if len(safe) == 0 {
		return ""
	}
	return strings.Join(safe, "/")
}

func packageError(reason string, displayPath string, nextAction string, err error) error {
	return &PackageError{
		Reason:      reason,
		DisplayPath: safePackagePath(displayPath),
		NextAction:  nextAction,
		err:         err,
	}
}
