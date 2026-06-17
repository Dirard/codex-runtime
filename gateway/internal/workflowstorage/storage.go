package workflowstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
)

const (
	metadataFileName           = "metadata.json"
	deleteRemoveRetries        = 20
	deleteRemoveInitialBackoff = 25 * time.Millisecond
	deleteRemoveMaxBackoff     = 250 * time.Millisecond
	agentsMDFileName           = "AGENTS.md"
	agentsOverrideMDFileName   = "AGENTS.override.md"
)

var slugPattern = regexp.MustCompile(`[^a-z0-9._-]+`)

var removeAllFunc = os.RemoveAll

type Manager struct {
	root           string
	limits         PackageLimits
	mu             sync.Mutex
	locks          map[string]*sync.Mutex
	failAfterFiles int
}

type Record struct {
	StorageKey string
	Root       string
	Metadata   Metadata
}

type Metadata struct {
	SchemaVersion              int    `json:"schema_version"`
	Namespace                  string `json:"namespace"`
	WorkflowID                 string `json:"workflow_id"`
	StorageKey                 string `json:"storage_key"`
	ActivePackageFingerprint   string `json:"active_package_fingerprint"`
	PendingPackageFingerprint  string `json:"pending_package_fingerprint,omitempty"`
	PreviousPackageFingerprint string `json:"previous_package_fingerprint,omitempty"`
	RestartRequired            bool   `json:"restart_required"`
	Status                     string `json:"status"`
	CreatedAtUnixMS            int64  `json:"created_at_unix_ms"`
	UpdatedAtUnixMS            int64  `json:"updated_at_unix_ms"`
	LastError                  string `json:"last_error,omitempty"`
}

func NewManager(root string, limits PackageLimits) (*Manager, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("workflow storage root is required")
	}
	clean := filepath.Clean(root)
	if !filepath.IsAbs(clean) {
		return nil, fmt.Errorf("workflow storage root must be absolute")
	}
	if err := os.MkdirAll(clean, 0o700); err != nil {
		return nil, fmt.Errorf("create workflow storage root: %w", err)
	}
	return &Manager{
		root:   clean,
		limits: normalizeLimits(limits),
		locks:  map[string]*sync.Mutex{},
	}, nil
}

func (m *Manager) Validate(input *pb.WorkflowPackage) (*Package, error) {
	if m == nil {
		return nil, packageError("storage_unavailable", "", "configure workflow storage before init", nil)
	}
	return ValidateProtoPackage(input, m.limits)
}

func (m *Manager) Materialize(ctx context.Context, input *pb.WorkflowPackage) (*Record, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	pkg, err := ValidateProtoPackage(input, m.limits)
	if err != nil {
		return nil, err
	}
	key := SafeStorageKey(pkg.Namespace, pkg.WorkflowID)
	lock := m.lockFor(key)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root := filepath.Join(m.root, key)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create workflow root: %w", err)
	}
	previousMetadata, _ := readMetadata(root)
	pending := filepath.Join(root, "pending")
	if err := os.RemoveAll(pending); err != nil {
		return nil, fmt.Errorf("clear pending workflow revision: %w", err)
	}
	if err := os.MkdirAll(pending, 0o700); err != nil {
		return nil, fmt.Errorf("create pending workflow revision: %w", err)
	}
	if err := m.writePackageFiles(pending, pkg); err != nil {
		_ = os.RemoveAll(pending)
		return nil, err
	}

	current := filepath.Join(root, "current")
	previous := filepath.Join(root, "previous")
	if err := os.RemoveAll(previous); err != nil {
		_ = os.RemoveAll(pending)
		return nil, fmt.Errorf("clear previous workflow revision: %w", err)
	}
	if _, err := os.Stat(current); err == nil {
		if err := os.Rename(current, previous); err != nil {
			_ = os.RemoveAll(pending)
			return nil, fmt.Errorf("preserve previous workflow revision: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		_ = os.RemoveAll(pending)
		return nil, fmt.Errorf("inspect current workflow revision: %w", err)
	}
	if err := os.Rename(pending, current); err != nil {
		if _, statErr := os.Stat(previous); statErr == nil {
			_ = os.Rename(previous, current)
		}
		return nil, fmt.Errorf("promote pending workflow revision: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "pending"), 0o700); err != nil {
		return nil, fmt.Errorf("recreate pending workflow directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "runtime"), 0o700); err != nil {
		return nil, fmt.Errorf("create workflow runtime directory: %w", err)
	}
	if err := syncRuntimeWorkspace(root); err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	created := now
	if previousMetadata.CreatedAtUnixMS > 0 {
		created = previousMetadata.CreatedAtUnixMS
	}
	metadata := Metadata{
		SchemaVersion:              SchemaVersion,
		Namespace:                  pkg.Namespace,
		WorkflowID:                 pkg.WorkflowID,
		StorageKey:                 key,
		ActivePackageFingerprint:   pkg.PackageFingerprint,
		PreviousPackageFingerprint: previousMetadata.ActivePackageFingerprint,
		RestartRequired:            false,
		Status:                     "ready",
		CreatedAtUnixMS:            created,
		UpdatedAtUnixMS:            now,
	}
	if err := writeMetadata(root, metadata); err != nil {
		return nil, err
	}
	return &Record{StorageKey: key, Root: root, Metadata: metadata}, nil
}

func (m *Manager) Stage(ctx context.Context, input *pb.WorkflowPackage) (*Record, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	pkg, err := ValidateProtoPackage(input, m.limits)
	if err != nil {
		return nil, err
	}
	key := SafeStorageKey(pkg.Namespace, pkg.WorkflowID)
	lock := m.lockFor(key)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root := filepath.Join(m.root, key)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create workflow root: %w", err)
	}
	metadata, err := readMetadata(root)
	if err != nil || metadata.ActivePackageFingerprint == "" {
		return nil, packageError("current_revision_missing", "", "initialize workflow before staging an update", err)
	}
	pending := filepath.Join(root, "pending")
	if err := os.RemoveAll(pending); err != nil {
		return nil, fmt.Errorf("clear pending workflow revision: %w", err)
	}
	if err := os.MkdirAll(pending, 0o700); err != nil {
		return nil, fmt.Errorf("create pending workflow revision: %w", err)
	}
	if err := m.writePackageFiles(pending, pkg); err != nil {
		_ = os.RemoveAll(pending)
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "runtime"), 0o700); err != nil {
		return nil, fmt.Errorf("create workflow runtime directory: %w", err)
	}
	if err := syncRuntimeWorkspace(root); err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	metadata.SchemaVersion = SchemaVersion
	metadata.Namespace = pkg.Namespace
	metadata.WorkflowID = pkg.WorkflowID
	metadata.StorageKey = key
	metadata.PendingPackageFingerprint = pkg.PackageFingerprint
	metadata.RestartRequired = true
	metadata.Status = "restart_required"
	metadata.UpdatedAtUnixMS = now
	if metadata.CreatedAtUnixMS == 0 {
		metadata.CreatedAtUnixMS = now
	}
	if err := writeMetadata(root, metadata); err != nil {
		return nil, err
	}
	return &Record{StorageKey: key, Root: root, Metadata: metadata}, nil
}

func (m *Manager) PromotePending(ctx context.Context, namespace string, workflowID string) (*Record, error) {
	if err := validateIdentity(strings.TrimSpace(namespace), strings.TrimSpace(workflowID)); err != nil {
		return nil, err
	}
	key := SafeStorageKey(namespace, workflowID)
	lock := m.lockFor(key)
	lock.Lock()
	defer lock.Unlock()
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	root := filepath.Join(m.root, key)
	metadata, err := readMetadata(root)
	if err != nil {
		return nil, packageError("workflow_not_found", "", "initialize workflow before restart", err)
	}
	if metadata.PendingPackageFingerprint == "" {
		return &Record{StorageKey: key, Root: root, Metadata: metadata}, nil
	}

	pending := filepath.Join(root, "pending")
	current := filepath.Join(root, "current")
	previous := filepath.Join(root, "previous")
	if _, err := os.Stat(pending); err != nil {
		return nil, packageError("pending_revision_missing", "pending", "stage workflow update again", err)
	}
	if err := os.RemoveAll(previous); err != nil {
		return nil, fmt.Errorf("clear previous workflow revision: %w", err)
	}
	if _, err := os.Stat(current); err == nil {
		if err := os.Rename(current, previous); err != nil {
			return nil, fmt.Errorf("preserve previous workflow revision: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect current workflow revision: %w", err)
	}
	if err := os.Rename(pending, current); err != nil {
		if _, statErr := os.Stat(previous); statErr == nil {
			_ = os.Rename(previous, current)
		}
		return nil, fmt.Errorf("promote pending workflow revision: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "pending"), 0o700); err != nil {
		return nil, fmt.Errorf("recreate pending workflow directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "runtime"), 0o700); err != nil {
		return nil, fmt.Errorf("create workflow runtime directory: %w", err)
	}

	now := time.Now().UnixMilli()
	previousFingerprint := metadata.ActivePackageFingerprint
	metadata.ActivePackageFingerprint = metadata.PendingPackageFingerprint
	metadata.PendingPackageFingerprint = ""
	metadata.PreviousPackageFingerprint = previousFingerprint
	metadata.RestartRequired = false
	metadata.Status = "ready"
	metadata.UpdatedAtUnixMS = now
	if err := writeMetadata(root, metadata); err != nil {
		return nil, err
	}
	return &Record{StorageKey: key, Root: root, Metadata: metadata}, nil
}

func (m *Manager) Delete(ctx context.Context, namespace string, workflowID string, active bool, force bool) error {
	if err := validateIdentity(strings.TrimSpace(namespace), strings.TrimSpace(workflowID)); err != nil {
		return err
	}
	if active && !force {
		return packageError("active_work_refused", "", "stop active workflow work or pass force", nil)
	}
	key := SafeStorageKey(namespace, workflowID)
	lock := m.lockFor(key)
	lock.Lock()
	defer lock.Unlock()
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return removeAllWithRetry(ctx, filepath.Join(m.root, key))
}

func removeAllWithRetry(ctx context.Context, target string) error {
	var err error
	backoff := deleteRemoveInitialBackoff
	for attempt := 0; attempt < deleteRemoveRetries; attempt++ {
		if ctx != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
		}
		if err = removeAllFunc(target); err == nil {
			return nil
		}
		if attempt == deleteRemoveRetries-1 {
			break
		}
		if ctx == nil {
			time.Sleep(backoff)
		} else {
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return ctx.Err()
			case <-timer.C:
			}
		}
		backoff *= 2
		if backoff > deleteRemoveMaxBackoff {
			backoff = deleteRemoveMaxBackoff
		}
	}
	return err
}

func RuntimeWorkspaceDir(root string) string {
	return filepath.Join(root, "runtime", "workspace")
}

func RuntimeProjectConfigDir(root string) string {
	return filepath.Join(RuntimeWorkspaceDir(root), ".codex")
}

func syncRuntimeWorkspace(root string) error {
	current := filepath.Join(root, "current")
	runtimeDir := filepath.Join(root, "runtime")
	workspace := RuntimeWorkspaceDir(root)
	target := RuntimeProjectConfigDir(root)
	staging := filepath.Join(runtimeDir, ".codex-next")

	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return fmt.Errorf("create workflow runtime workspace: %w", err)
	}
	if !pathWithin(root, staging) || !pathWithin(root, target) {
		return fmt.Errorf("workflow runtime project config path escaped storage root")
	}
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("clear staged workflow runtime config: %w", err)
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return fmt.Errorf("create staged workflow runtime config: %w", err)
	}
	if err := copyDirectory(current, staging, isWorkflowProjectInstructionFile); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("clear workflow runtime config: %w", err)
	}
	if err := os.Rename(staging, target); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("promote workflow runtime config: %w", err)
	}
	if err := syncProjectInstructionFiles(current, workspace); err != nil {
		return err
	}
	return nil
}

func copyDirectory(source string, target string, skip func(string) bool) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return fmt.Errorf("resolve workflow runtime config path: %w", err)
		}
		if relative == "." {
			return nil
		}
		relativeSlash := filepath.ToSlash(relative)
		if skip != nil && skip(relativeSlash) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		cleanTarget := filepath.Clean(filepath.Join(target, relative))
		if !pathWithin(target, cleanTarget) {
			return fmt.Errorf("workflow runtime config path escaped staging root")
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect workflow runtime config file: %w", err)
		}
		if entry.IsDir() {
			return os.MkdirAll(cleanTarget, 0o700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("workflow runtime config contains non-regular file: %s", relative)
		}
		return copyFile(path, cleanTarget, info.Mode().Perm())
	})
}

func syncProjectInstructionFiles(current string, workspace string) error {
	for _, name := range []string{agentsMDFileName, agentsOverrideMDFileName} {
		source := filepath.Join(current, name)
		target := filepath.Join(workspace, name)
		if !pathWithin(workspace, target) {
			return fmt.Errorf("workflow project instructions path escaped workspace root")
		}
		info, err := os.Stat(source)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("clear stale workflow project instructions: %w", err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect workflow project instructions: %w", err)
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return fmt.Errorf("workflow project instructions must be regular files: %s", name)
		}
		if err := copyFile(source, target, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func isWorkflowProjectInstructionFile(relativePath string) bool {
	return relativePath == agentsMDFileName || relativePath == agentsOverrideMDFileName
}

func copyFile(source string, target string, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open workflow runtime config source: %w", err)
	}
	defer input.Close()

	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create workflow runtime config target: %w", err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return fmt.Errorf("copy workflow runtime config file: %w", err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close workflow runtime config target: %w", err)
	}
	return nil
}

func pathWithin(root string, target string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative))
}

func SafeStorageKey(namespace string, workflowID string) string {
	base := safeSlug(namespace) + "--" + safeSlug(workflowID)
	if len(base) > 80 {
		base = base[:80]
	}
	sum := sha256.Sum256([]byte(namespace + "\x00" + workflowID))
	return base + "--" + hex.EncodeToString(sum[:8])
}

func (m *Manager) lockFor(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock := m.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		m.locks[key] = lock
	}
	return lock
}

func (m *Manager) writePackageFiles(root string, pkg *Package) error {
	for index, file := range pkg.Files {
		if m.failAfterFiles > 0 && index >= m.failAfterFiles {
			return errors.New("injected workflow materialization failure")
		}
		target := filepath.Join(root, filepath.FromSlash(file.Path))
		cleanTarget := filepath.Clean(target)
		if !strings.HasPrefix(cleanTarget, filepath.Clean(root)+string(os.PathSeparator)) {
			return packageError("materialization_escape_rejected", file.Path, "workflow path escaped storage root", nil)
		}
		if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o700); err != nil {
			return fmt.Errorf("create workflow file parent: %w", err)
		}
		mode := os.FileMode(0o600)
		if file.Executable {
			mode = 0o700
		}
		if err := os.WriteFile(cleanTarget, file.Contents, mode); err != nil {
			return fmt.Errorf("write workflow file: %w", err)
		}
	}
	return nil
}

func readMetadata(root string) (Metadata, error) {
	var metadata Metadata
	contents, err := os.ReadFile(filepath.Join(root, metadataFileName))
	if err != nil {
		return Metadata{}, err
	}
	if err := json.Unmarshal(contents, &metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func writeMetadata(root string, metadata Metadata) error {
	contents, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	contents = append(contents, '\n')
	return os.WriteFile(filepath.Join(root, metadataFileName), contents, 0o600)
}

func safeSlug(value string) string {
	slug := strings.ToLower(strings.TrimSpace(value))
	slug = slugPattern.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-._")
	if slug == "" {
		return "workflow"
	}
	return slug
}
