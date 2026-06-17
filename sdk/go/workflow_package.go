package codex

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
)

const (
	DefaultWorkflowPackageMaxBytes = 10 * 1024 * 1024

	defaultWorkflowPackageMaxFileBytes      = 2 * 1024 * 1024
	defaultWorkflowPackageMaxReferenceBytes = 512 * 1024
	defaultWorkflowPackageMaxFiles          = 512
	defaultWorkflowPackageMaxPathBytes      = 240
	workflowPackageSchemaVersion            = 1
)

var (
	ErrInvalidWorkflowPackage = errors.New("codex: invalid workflow package")

	workflowIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	secretPatterns    = []secretPattern{
		{name: "private_key", pattern: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
		{name: "authorization_bearer", pattern: regexp.MustCompile(`(?i)authorization\s*:\s*bearer\s+[A-Za-z0-9._~+/=-]{8,}`)},
		{name: "openai_api_key", pattern: regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`)},
		{name: "github_token", pattern: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`)},
	}
)

type WorkflowSource interface {
	workflowSource()
}

type WorkflowDir struct {
	Namespace string
	ID        string
	Path      string
}

func (WorkflowDir) workflowSource() {}

type WorkflowZip struct {
	Namespace string
	ID        string
	Reader    io.Reader
}

func (WorkflowZip) workflowSource() {}

type WorkflowPackageOption func(*workflowPackageOptions) error

type workflowPackageOptions struct {
	maxTotalBytes     int64
	maxFileBytes      int64
	maxReferenceBytes int64
	maxFiles          int
	maxPathBytes      int
}

type WorkflowPackage struct {
	Namespace          string
	ID                 string
	PackageFingerprint string
	Files              []WorkflowPackageFile
	TotalSizeBytes     int64
}

type WorkflowPackageFile struct {
	Path       string
	Contents   []byte
	SizeBytes  int64
	SHA256     string
	Executable bool
}

type WorkflowPackageError struct {
	Reason      string
	DisplayPath string
	NextAction  string
	err         error
}

func (e *WorkflowPackageError) Error() string {
	if e == nil {
		return ""
	}
	message := "invalid workflow package"
	if e.Reason != "" {
		message = e.Reason
	}
	if e.DisplayPath != "" {
		message += ": " + e.DisplayPath
	}
	if e.NextAction != "" {
		message += ": " + e.NextAction
	}
	return message
}

func (e *WorkflowPackageError) Unwrap() error {
	if e == nil || e.err == nil {
		return ErrInvalidWorkflowPackage
	}
	return e.err
}

func (e *WorkflowPackageError) Is(target error) bool {
	return target == ErrInvalidWorkflowPackage
}

func WithWorkflowPackageMaxBytes(maxBytes int64) WorkflowPackageOption {
	return func(opts *workflowPackageOptions) error {
		if maxBytes <= 0 {
			return fmt.Errorf("%w: max package bytes must be positive", ErrInvalidConfiguration)
		}
		opts.maxTotalBytes = maxBytes
		return nil
	}
}

func BuildWorkflowPackage(source WorkflowSource, opts ...WorkflowPackageOption) (*WorkflowPackage, error) {
	options := defaultWorkflowPackageOptions()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&options); err != nil {
			return nil, err
		}
	}

	switch src := source.(type) {
	case WorkflowDir:
		return buildWorkflowPackageFromDir(src, options)
	case WorkflowZip:
		return buildWorkflowPackageFromZip(src, options)
	case nil:
		return nil, packageError("source_required", "", "provide WorkflowDir or WorkflowZip", nil)
	default:
		return nil, packageError("unsupported_source", "", "provide WorkflowDir or WorkflowZip", nil)
	}
}

func (pkg *WorkflowPackage) Proto() *pb.WorkflowPackage {
	if pkg == nil {
		return nil
	}
	files := make([]*pb.WorkflowPackageFile, 0, len(pkg.Files))
	for _, file := range pkg.Files {
		files = append(files, &pb.WorkflowPackageFile{
			RelativePath: file.Path,
			Contents:     append([]byte(nil), file.Contents...),
			SizeBytes:    uint64(file.SizeBytes),
			Sha256:       file.SHA256,
			Executable:   file.Executable,
		})
	}
	return &pb.WorkflowPackage{
		SchemaVersion: workflowPackageSchemaVersion,
		Workflow: &pb.WorkflowSelector{
			Namespace:  pkg.Namespace,
			WorkflowId: pkg.ID,
		},
		PackageFingerprint: pkg.PackageFingerprint,
		Files:              files,
		TotalSizeBytes:     uint64(pkg.TotalSizeBytes),
	}
}

func defaultWorkflowPackageOptions() workflowPackageOptions {
	return workflowPackageOptions{
		maxTotalBytes:     DefaultWorkflowPackageMaxBytes,
		maxFileBytes:      defaultWorkflowPackageMaxFileBytes,
		maxReferenceBytes: defaultWorkflowPackageMaxReferenceBytes,
		maxFiles:          defaultWorkflowPackageMaxFiles,
		maxPathBytes:      defaultWorkflowPackageMaxPathBytes,
	}
}

func buildWorkflowPackageFromDir(source WorkflowDir, opts workflowPackageOptions) (*WorkflowPackage, error) {
	if err := validateWorkflowIdentity(source.Namespace, source.ID); err != nil {
		return nil, err
	}
	root := strings.TrimSpace(source.Path)
	if root == "" {
		return nil, packageError("source_path_required", "", "provide a workflow folder path", nil)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, packageError("source_unavailable", "", "workflow folder cannot be read", err)
	}
	if !info.IsDir() {
		return nil, packageError("source_not_directory", "", "WorkflowDir.Path must point to a directory", nil)
	}

	var files []WorkflowPackageFile
	err = filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return packageError("source_unavailable", safeRelPath(root, filePath), "workflow entry cannot be read", walkErr)
		}
		if filePath == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return packageError("source_unavailable", safeRelPath(root, filePath), "workflow entry cannot be inspected", err)
		}
		mode := info.Mode()
		if mode&os.ModeSymlink != 0 {
			return packageError("symlink_rejected", safeRelPath(root, filePath), "replace symlink with a regular file inside the workflow package", nil)
		}
		if entry.IsDir() {
			return nil
		}
		if !mode.IsRegular() {
			return packageError("special_file_rejected", safeRelPath(root, filePath), "workflow packages may contain only regular files", nil)
		}
		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			return packageError("path_rejected", "", "workflow file must stay under the workflow folder", err)
		}
		normalized, err := validateWorkflowRelativePath(filepath.ToSlash(rel), opts)
		if err != nil {
			return err
		}
		contents, err := readRegularFile(filePath, info.Size(), normalized, opts)
		if err != nil {
			return err
		}
		files = append(files, WorkflowPackageFile{
			Path:       normalized,
			Contents:   contents,
			SizeBytes:  int64(len(contents)),
			SHA256:     sha256HexBytes(contents),
			Executable: mode&0o111 != 0,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return finalizeWorkflowPackage(source.Namespace, source.ID, files, opts)
}

func buildWorkflowPackageFromZip(source WorkflowZip, opts workflowPackageOptions) (*WorkflowPackage, error) {
	if err := validateWorkflowIdentity(source.Namespace, source.ID); err != nil {
		return nil, err
	}
	if source.Reader == nil {
		return nil, packageError("zip_reader_required", "", "provide a workflow ZIP reader", nil)
	}
	zipBytes, err := io.ReadAll(io.LimitReader(source.Reader, opts.maxTotalBytes+1))
	if err != nil {
		return nil, packageError("zip_unreadable", "", "workflow ZIP cannot be read", err)
	}
	if int64(len(zipBytes)) > opts.maxTotalBytes {
		return nil, packageError("package_too_large", "", "reduce workflow package contents or raise the explicit package limit", nil)
	}
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, packageError("zip_invalid", "", "provide a valid ZIP workflow package", err)
	}

	files := make([]WorkflowPackageFile, 0, len(reader.File))
	for _, zipFile := range reader.File {
		if zipFile.FileInfo().IsDir() {
			continue
		}
		mode := zipFile.Mode()
		if mode&os.ModeSymlink != 0 {
			return nil, packageError("symlink_rejected", zipFile.Name, "replace symlink with a regular file inside the workflow package", nil)
		}
		if !mode.IsRegular() {
			return nil, packageError("special_file_rejected", zipFile.Name, "workflow packages may contain only regular files", nil)
		}
		normalized, err := validateWorkflowRelativePath(zipFile.Name, opts)
		if err != nil {
			return nil, err
		}
		if zipFile.UncompressedSize64 > uint64(opts.maxFileBytes) {
			return nil, packageError("file_too_large", normalized, "reduce this workflow file or raise the explicit package limit", nil)
		}
		rc, err := zipFile.Open()
		if err != nil {
			return nil, packageError("zip_entry_unreadable", normalized, "workflow ZIP entry cannot be read", err)
		}
		contents, readErr := io.ReadAll(io.LimitReader(rc, opts.maxFileBytes+1))
		closeErr := rc.Close()
		if readErr != nil {
			return nil, packageError("zip_entry_unreadable", normalized, "workflow ZIP entry cannot be read", readErr)
		}
		if closeErr != nil {
			return nil, packageError("zip_entry_unreadable", normalized, "workflow ZIP entry cannot be closed", closeErr)
		}
		if int64(len(contents)) > opts.maxFileBytes {
			return nil, packageError("file_too_large", normalized, "reduce this workflow file or raise the explicit package limit", nil)
		}
		files = append(files, WorkflowPackageFile{
			Path:       normalized,
			Contents:   contents,
			SizeBytes:  int64(len(contents)),
			SHA256:     sha256HexBytes(contents),
			Executable: mode&0o111 != 0,
		})
	}
	return finalizeWorkflowPackage(source.Namespace, source.ID, files, opts)
}

func validateWorkflowIdentity(namespace string, workflowID string) error {
	if strings.TrimSpace(namespace) == "" {
		return packageError("namespace_required", "", "set WorkflowDir.Namespace or WorkflowZip.Namespace", nil)
	}
	if strings.TrimSpace(workflowID) == "" {
		return packageError("workflow_id_required", "", "set WorkflowDir.ID or WorkflowZip.ID explicitly", nil)
	}
	if !workflowIDPattern.MatchString(namespace) {
		return packageError("namespace_invalid", "", "use letters, digits, dot, underscore or dash; start with a letter or digit", nil)
	}
	if !workflowIDPattern.MatchString(workflowID) {
		return packageError("workflow_id_invalid", "", "use letters, digits, dot, underscore or dash; start with a letter or digit", nil)
	}
	return nil
}

func validateWorkflowRelativePath(name string, opts workflowPackageOptions) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.TrimSpace(name) == "" {
		return "", packageError("path_required", "", "workflow package file path is empty", nil)
	}
	if strings.HasPrefix(name, "/") || path.IsAbs(name) || strings.Contains(name, ":") {
		return "", packageError("absolute_path_rejected", safePackagePath(name), "use a relative path inside the workflow package", nil)
	}
	clean := path.Clean(name)
	if clean == "." || clean != name {
		return "", packageError("path_not_clean", safePackagePath(name), "use a clean relative path without dot segments", nil)
	}
	if strings.HasPrefix(clean, "../") || clean == ".." || strings.Contains(clean, "/../") {
		return "", packageError("traversal_rejected", safePackagePath(name), "remove parent-directory traversal from workflow package paths", nil)
	}
	if len(clean) > opts.maxPathBytes {
		return "", packageError("path_too_long", safePackagePath(clean), "shorten the workflow package path", nil)
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", packageError("path_segment_rejected", safePackagePath(clean), "use plain relative path segments", nil)
		}
		if isReservedWorkflowSegment(segment) {
			return "", packageError("reserved_path_rejected", safePackagePath(clean), "remove reserved workflow package path segment", nil)
		}
	}
	return clean, nil
}

func finalizeWorkflowPackage(namespace string, workflowID string, files []WorkflowPackageFile, opts workflowPackageOptions) (*WorkflowPackage, error) {
	if len(files) == 0 {
		return nil, packageError("package_empty", "", "include config.toml and any workflow assets", nil)
	}
	if len(files) > opts.maxFiles {
		return nil, packageError("too_many_files", "", "reduce workflow package file count", nil)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	seen := make(map[string]string, len(files))
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
		if total > opts.maxTotalBytes {
			return nil, packageError("package_too_large", "", "reduce workflow package contents or raise the explicit package limit", nil)
		}
		if strings.HasPrefix(file.Path, "references/") && file.SizeBytes > opts.maxReferenceBytes {
			return nil, packageError("reference_too_large", file.Path, "keep large corpora outside workflow packages", nil)
		}
		if finding := scanWorkflowFileForSecrets(file); finding != "" {
			return nil, packageError("secret_like_content_rejected", file.Path, "replace raw secret values with env/file secret references", errors.New(finding))
		}
	}
	if !hasConfig {
		return nil, packageError("config_missing", "config.toml", "place config.toml at the workflow root", nil)
	}

	fingerprint, err := workflowPackageFingerprint(files)
	if err != nil {
		return nil, err
	}
	return &WorkflowPackage{
		Namespace:          strings.TrimSpace(namespace),
		ID:                 strings.TrimSpace(workflowID),
		PackageFingerprint: fingerprint,
		Files:              cloneWorkflowPackageFiles(files),
		TotalSizeBytes:     total,
	}, nil
}

type workflowPackageFingerprintPayload struct {
	SchemaVersion int                                     `json:"schema_version"`
	Files         []workflowPackageFingerprintFilePayload `json:"files"`
}

type workflowPackageFingerprintFilePayload struct {
	Path       string `json:"path"`
	SizeBytes  int64  `json:"size_bytes"`
	SHA256     string `json:"sha256"`
	Executable bool   `json:"executable"`
}

func workflowPackageFingerprint(files []WorkflowPackageFile) (string, error) {
	payload := workflowPackageFingerprintPayload{
		SchemaVersion: workflowPackageSchemaVersion,
		Files:         make([]workflowPackageFingerprintFilePayload, 0, len(files)),
	}
	for _, file := range files {
		payload.Files = append(payload.Files, workflowPackageFingerprintFilePayload{
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
	return sha256HexBytes(canonical), nil
}

func readRegularFile(filePath string, size int64, displayPath string, opts workflowPackageOptions) ([]byte, error) {
	if size > opts.maxFileBytes {
		return nil, packageError("file_too_large", displayPath, "reduce this workflow file or raise the explicit package limit", nil)
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, packageError("source_unavailable", displayPath, "workflow file cannot be read", err)
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, opts.maxFileBytes+1))
	if err != nil {
		return nil, packageError("source_unavailable", displayPath, "workflow file cannot be read", err)
	}
	if int64(len(contents)) > opts.maxFileBytes {
		return nil, packageError("file_too_large", displayPath, "reduce this workflow file or raise the explicit package limit", nil)
	}
	return contents, nil
}

func scanWorkflowFileForSecrets(file WorkflowPackageFile) string {
	if !looksTextualWorkflowFile(file.Path, file.Contents) {
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

func looksTextualWorkflowFile(name string, contents []byte) bool {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".toml", ".md", ".txt", ".json", ".yaml", ".yml", ".xml", ".env", ".sh", ".ps1":
		return true
	}
	if len(contents) == 0 {
		return true
	}
	sample := contents
	if len(sample) > 4096 {
		sample = sample[:4096]
	}
	for _, b := range sample {
		if b == 0 {
			return false
		}
	}
	return true
}

func isReservedWorkflowSegment(segment string) bool {
	lower := strings.ToLower(segment)
	if lower == ".git" || lower == ".supergoal" || lower == "node_modules" {
		return true
	}
	base := strings.TrimSuffix(lower, path.Ext(lower))
	switch base {
	case "con", "prn", "aux", "nul":
		return true
	}
	if strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt") {
		if len(base) == 4 && base[3] >= '1' && base[3] <= '9' {
			return true
		}
	}
	return false
}

func cloneWorkflowPackageFiles(files []WorkflowPackageFile) []WorkflowPackageFile {
	cloned := make([]WorkflowPackageFile, 0, len(files))
	for _, file := range files {
		file.Contents = append([]byte(nil), file.Contents...)
		cloned = append(cloned, file)
	}
	return cloned
}

func sha256HexBytes(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func safeRelPath(root string, filePath string) string {
	rel, err := filepath.Rel(root, filePath)
	if err != nil {
		return ""
	}
	return safePackagePath(filepath.ToSlash(rel))
}

func safePackagePath(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimLeft(name, "/")
	parts := strings.Split(name, "/")
	safeParts := parts[:0]
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			continue
		}
		safeParts = append(safeParts, part)
	}
	if len(safeParts) == 0 {
		return ""
	}
	return strings.Join(safeParts, "/")
}

func packageError(reason string, displayPath string, nextAction string, err error) error {
	return &WorkflowPackageError{
		Reason:      reason,
		DisplayPath: safePackagePath(displayPath),
		NextAction:  nextAction,
		err:         err,
	}
}
