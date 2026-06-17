package workflowstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
)

func TestMaterializeCreatesSafeLayoutAndMetadata(t *testing.T) {
	manager := newTestManager(t)
	record, err := manager.Materialize(context.Background(), validProtoPackage(t, "team.a", "writer.one"))
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}

	if strings.Contains(record.StorageKey, "/") || strings.Contains(record.StorageKey, "\\") {
		t.Fatalf("storage key contains path separator: %q", record.StorageKey)
	}
	if record.StorageKey == "team.a" || record.StorageKey == "writer.one" {
		t.Fatalf("storage key used public id directly: %q", record.StorageKey)
	}
	for _, dir := range []string{"current", "pending", "runtime"} {
		if info, err := os.Stat(filepath.Join(record.Root, dir)); err != nil || !info.IsDir() {
			t.Fatalf("%s dir missing: %v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(record.Root, "current", "config.toml")); err != nil {
		t.Fatalf("current config missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(RuntimeProjectConfigDir(record.Root), "config.toml")); err != nil {
		t.Fatalf("runtime .codex config missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(RuntimeProjectConfigDir(record.Root), "agents", "writer.toml")); err != nil {
		t.Fatalf("runtime .codex agent missing: %v", err)
	}

	metadataBytes, err := os.ReadFile(filepath.Join(record.Root, metadataFileName))
	if err != nil {
		t.Fatalf("ReadFile(metadata) error = %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		t.Fatalf("metadata JSON error = %v", err)
	}
	for _, forbidden := range []string{"prompt", "message", "event", "jsonl", "authorization", "token", "embedding", "vector"} {
		if _, ok := metadata[forbidden]; ok {
			t.Fatalf("metadata contains forbidden key %q: %s", forbidden, metadataBytes)
		}
	}
}

func TestMaterializePreservesPreviousAndRollsBackFailures(t *testing.T) {
	manager := newTestManager(t)
	first := validProtoPackage(t, "team-a", "writer")
	record, err := manager.Materialize(context.Background(), first)
	if err != nil {
		t.Fatalf("first Materialize() error = %v", err)
	}
	firstFingerprint := record.Metadata.ActivePackageFingerprint

	second := validProtoPackage(t, "team-a", "writer")
	second.Files = append(second.Files, protoPackageFile("skills/new/SKILL.md", []byte("# New\n")))
	refreshFingerprint(t, second)
	record, err = manager.Materialize(context.Background(), second)
	if err != nil {
		t.Fatalf("second Materialize() error = %v", err)
	}
	if record.Metadata.PreviousPackageFingerprint != firstFingerprint {
		t.Fatalf("previous fingerprint = %q, want %q", record.Metadata.PreviousPackageFingerprint, firstFingerprint)
	}
	if _, err := os.Stat(filepath.Join(record.Root, "previous", "config.toml")); err != nil {
		t.Fatalf("previous revision not preserved: %v", err)
	}

	manager.failAfterFiles = 1
	third := validProtoPackage(t, "team-a", "writer")
	third.Files = append(third.Files, protoPackageFile("skills/fail/SKILL.md", []byte("# Fail\n")))
	refreshFingerprint(t, third)
	if _, err := manager.Materialize(context.Background(), third); err == nil {
		t.Fatal("Materialize() succeeded with injected failure")
	}
	metadata, err := readMetadata(record.Root)
	if err != nil {
		t.Fatalf("readMetadata() error = %v", err)
	}
	if metadata.ActivePackageFingerprint != record.Metadata.ActivePackageFingerprint {
		t.Fatalf("active fingerprint changed after failed materialization")
	}
}

func TestRuntimeWorkspaceCodexMirrorReplacesStaleFiles(t *testing.T) {
	manager := newTestManager(t)
	first := validProtoPackage(t, "team-a", "mirror")
	record, err := manager.Materialize(context.Background(), first)
	if err != nil {
		t.Fatalf("first Materialize() error = %v", err)
	}
	assertFileContains(t, filepath.Join(RuntimeProjectConfigDir(record.Root), "references", "note.md"), "Harbor")

	second, err := NewProtoPackage("team-a", "mirror", []PackageFile{
		packageFile("config.toml", []byte("model = \"next\"\n")),
		packageFile("skills/new/SKILL.md", []byte("# New\n")),
	})
	if err != nil {
		t.Fatalf("NewProtoPackage(second) error = %v", err)
	}
	record, err = manager.Materialize(context.Background(), second)
	if err != nil {
		t.Fatalf("second Materialize() error = %v", err)
	}
	assertFileContains(t, filepath.Join(RuntimeProjectConfigDir(record.Root), "config.toml"), "next")
	assertFileContains(t, filepath.Join(RuntimeProjectConfigDir(record.Root), "skills", "new", "SKILL.md"), "New")
	if _, err := os.Stat(filepath.Join(RuntimeProjectConfigDir(record.Root), "references", "note.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale runtime .codex reference exists or stat failed unexpectedly: %v", err)
	}
}

func TestRuntimeWorkspacePlacesAgentsMdAtWorkspaceRoot(t *testing.T) {
	manager := newTestManager(t)
	first, err := NewProtoPackage("team-a", "agents-md", []PackageFile{
		packageFile("config.toml", []byte("model = \"test\"\n")),
		packageFile("agents/writer.toml", []byte("name = \"writer\"\n")),
		packageFile("AGENTS.md", []byte("AGENTS_MARKER_ALPHA\n")),
		packageFile("AGENTS.override.md", []byte("AGENTS_OVERRIDE_ALPHA\n")),
	})
	if err != nil {
		t.Fatalf("NewProtoPackage(first) error = %v", err)
	}
	record, err := manager.Materialize(context.Background(), first)
	if err != nil {
		t.Fatalf("Materialize(first) error = %v", err)
	}
	assertFileContains(t, filepath.Join(RuntimeWorkspaceDir(record.Root), "AGENTS.md"), "AGENTS_MARKER_ALPHA")
	assertFileContains(t, filepath.Join(RuntimeWorkspaceDir(record.Root), "AGENTS.override.md"), "AGENTS_OVERRIDE_ALPHA")
	if _, err := os.Stat(filepath.Join(RuntimeProjectConfigDir(record.Root), "AGENTS.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("AGENTS.md was mirrored into .codex or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(RuntimeProjectConfigDir(record.Root), "AGENTS.override.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("AGENTS.override.md was mirrored into .codex or stat failed unexpectedly: %v", err)
	}

	second, err := NewProtoPackage("team-a", "agents-md", []PackageFile{
		packageFile("config.toml", []byte("model = \"test\"\n")),
		packageFile("agents/writer.toml", []byte("name = \"writer\"\n")),
	})
	if err != nil {
		t.Fatalf("NewProtoPackage(second) error = %v", err)
	}
	record, err = manager.Materialize(context.Background(), second)
	if err != nil {
		t.Fatalf("Materialize(second) error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(RuntimeWorkspaceDir(record.Root), "AGENTS.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale AGENTS.md exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(RuntimeWorkspaceDir(record.Root), "AGENTS.override.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale AGENTS.override.md exists or stat failed unexpectedly: %v", err)
	}
}

func TestNamespaceNonCollisionAndConcurrentMaterialization(t *testing.T) {
	manager := newTestManager(t)
	first, err := manager.Materialize(context.Background(), validProtoPackage(t, "team-a", "writer"))
	if err != nil {
		t.Fatalf("team-a Materialize() error = %v", err)
	}
	second, err := manager.Materialize(context.Background(), validProtoPackage(t, "team-b", "writer"))
	if err != nil {
		t.Fatalf("team-b Materialize() error = %v", err)
	}
	if first.StorageKey == second.StorageKey {
		t.Fatalf("namespace collision: %q", first.StorageKey)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := manager.Materialize(context.Background(), validProtoPackage(t, "team-c", "writer"))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Materialize() error = %v", err)
		}
	}
	matches, err := filepath.Glob(filepath.Join(manager.root, SafeStorageKey("team-c", "writer")))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("same identity materialized %d roots, want 1", len(matches))
	}
}

func TestDeleteRefusesActiveUnlessForced(t *testing.T) {
	manager := newTestManager(t)
	if _, err := manager.Materialize(context.Background(), validProtoPackage(t, "team-a", "writer")); err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if err := manager.Delete(context.Background(), "team-a", "writer", true, false); err == nil {
		t.Fatal("Delete() succeeded for active workflow without force")
	}
	if err := manager.Delete(context.Background(), "team-a", "writer", true, true); err != nil {
		t.Fatalf("forced Delete() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(manager.root, SafeStorageKey("team-a", "writer"))); !os.IsNotExist(err) {
		t.Fatalf("workflow root still exists or stat failed unexpectedly: %v", err)
	}
}

func TestDeleteRetriesTransientRemoveFailure(t *testing.T) {
	manager := newTestManager(t)
	if _, err := manager.Materialize(context.Background(), validProtoPackage(t, "team-a", "writer")); err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	root := filepath.Join(manager.root, SafeStorageKey("team-a", "writer"))

	originalRemoveAll := removeAllFunc
	attempts := 0
	removeAllFunc = func(path string) error {
		if filepath.Clean(path) == filepath.Clean(root) {
			attempts++
			if attempts == 1 {
				return fmt.Errorf("transient remove lock")
			}
		}
		return originalRemoveAll(path)
	}
	t.Cleanup(func() {
		removeAllFunc = originalRemoveAll
	})

	if err := manager.Delete(context.Background(), "team-a", "writer", false, true); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if attempts < 2 {
		t.Fatalf("remove attempts = %d, want retry after transient failure", attempts)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("workflow root still exists or stat failed unexpectedly: %v", err)
	}
}

func TestGatewayRevalidationRejectsUnsafeForgedAndOverLimitPackages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(pkg *testPackage)
		reason string
	}{
		{name: "forged file hash", mutate: func(pkg *testPackage) { pkg.protoPackage.Files[0].Sha256 = strings.Repeat("0", 64) }, reason: "hash_mismatch"},
		{name: "forged fingerprint", mutate: func(pkg *testPackage) { pkg.protoPackage.PackageFingerprint = strings.Repeat("0", 64) }, reason: "fingerprint_mismatch"},
		{name: "unsafe path", mutate: func(pkg *testPackage) { pkg.protoPackage.Files[0].RelativePath = "../config.toml" }, reason: "traversal_rejected"},
		{name: "duplicate case", mutate: func(pkg *testPackage) {
			pkg.protoPackage.Files = append(pkg.protoPackage.Files, protoPackageFile("CONFIG.TOML", []byte("model = \"test\"\n")))
			refreshFingerprint(t, pkg.protoPackage)
		}, reason: "duplicate_path_rejected"},
		{name: "secret", mutate: func(pkg *testPackage) {
			pkg.protoPackage.Files = append(pkg.protoPackage.Files, protoPackageFile("references/secret.md", []byte("Authorization: Bearer sk-secret-shaped-value-1234567890\n")))
			refreshFingerprint(t, pkg.protoPackage)
		}, reason: "secret_like_content_rejected"},
		{name: "over limit", mutate: func(pkg *testPackage) { pkg.limits.MaxBytes = 32 }, reason: "package_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := &testPackage{protoPackage: validProtoPackage(t, "team-a", "writer"), limits: PackageLimits{MaxBytes: DefaultMaxBytes, MaxFileBytes: DefaultMaxFileBytes}}
			tc.mutate(pkg)
			_, err := ValidateProtoPackage(pkg.protoPackage, pkg.limits)
			assertPackageReason(t, err, tc.reason)
		})
	}
}

func TestGatewayPackageLimitAcceptsTenMiBAndRejectsOverLimit(t *testing.T) {
	configContents := []byte("model = \"test\"\n")
	files := []PackageFile{packageFile("config.toml", configContents)}
	remaining := DefaultMaxBytes - int64(len(configContents))
	for i := 0; remaining > 0; i++ {
		chunk := min(remaining, DefaultMaxFileBytes)
		files = append(files, packageFile("skills/large/file-"+string(rune('a'+i))+".bin", bytesOf('a', int(chunk))))
		remaining -= chunk
	}
	under, err := NewProtoPackage("team-a", "writer", files)
	if err != nil {
		t.Fatalf("NewProtoPackage() error = %v", err)
	}
	if _, err := ValidateProtoPackage(under, PackageLimits{}); err != nil {
		t.Fatalf("10 MiB package rejected: %v", err)
	}
	over := validProtoPackage(t, "team-a", "writer")
	over.Files = append(over.Files, protoPackageFile("skills/large.bin", bytesOf('a', int(DefaultMaxBytes))))
	refreshFingerprint(t, over)
	_, err = ValidateProtoPackage(over, PackageLimits{MaxBytes: DefaultMaxBytes, MaxFileBytes: DefaultMaxBytes + 1})
	assertPackageReason(t, err, "package_too_large")
}

func TestConfigRequiresSafeWorkflowStorageAndLimits(t *testing.T) {
	root := t.TempDir()
	manager, err := NewManager(root, PackageLimits{})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if manager.root != filepath.Clean(root) {
		t.Fatalf("manager root = %q, want clean root", manager.root)
	}
}

type testPackage struct {
	protoPackage *pb.WorkflowPackage
	limits       PackageLimits
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	manager, err := NewManager(t.TempDir(), PackageLimits{})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager
}

func validProtoPackage(t *testing.T, namespace string, workflowID string) *pb.WorkflowPackage {
	t.Helper()
	pkg, err := NewProtoPackage(namespace, workflowID, []PackageFile{
		packageFile("config.toml", []byte("model = \"test\"\n")),
		packageFile("agents/writer.toml", []byte("name = \"writer\"\n")),
		packageFile("references/note.md", []byte("Harbor fire notes.\n")),
	})
	if err != nil {
		t.Fatalf("NewProtoPackage() error = %v", err)
	}
	return pkg
}

func packageFile(name string, contents []byte) PackageFile {
	sum := sha256.Sum256(contents)
	return PackageFile{
		Path:      name,
		Contents:  append([]byte(nil), contents...),
		SizeBytes: int64(len(contents)),
		SHA256:    hex.EncodeToString(sum[:]),
	}
}

func protoPackageFile(name string, contents []byte) *pb.WorkflowPackageFile {
	sum := sha256.Sum256(contents)
	return &pb.WorkflowPackageFile{
		RelativePath: name,
		Contents:     append([]byte(nil), contents...),
		SizeBytes:    uint64(len(contents)),
		Sha256:       hex.EncodeToString(sum[:]),
	}
}

func refreshFingerprint(t *testing.T, protoPackage *pb.WorkflowPackage) {
	t.Helper()
	files := make([]PackageFile, 0, len(protoPackage.GetFiles()))
	for _, protoFile := range protoPackage.GetFiles() {
		files = append(files, PackageFile{
			Path:       protoFile.GetRelativePath(),
			Contents:   append([]byte(nil), protoFile.GetContents()...),
			SizeBytes:  int64(protoFile.GetSizeBytes()),
			SHA256:     protoFile.GetSha256(),
			Executable: protoFile.GetExecutable(),
		})
	}
	fingerprint, err := Fingerprint(files)
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	protoPackage.PackageFingerprint = fingerprint
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if !strings.Contains(string(contents), want) {
		t.Fatalf("file %s contents %q do not contain %q", path, string(contents), want)
	}
}

func assertPackageReason(t *testing.T, err error, reason string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected package error reason %q, got nil", reason)
	}
	if !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("error does not wrap ErrInvalidPackage: %v", err)
	}
	var packageErr *PackageError
	if !errors.As(err, &packageErr) {
		t.Fatalf("error type = %T, want PackageError", err)
	}
	if packageErr.Reason != reason {
		t.Fatalf("package reason = %q, want %q (err=%v)", packageErr.Reason, reason, err)
	}
}

func bytesOf(char byte, count int) []byte {
	buf := make([]byte, count)
	for i := range buf {
		buf[i] = char
	}
	return buf
}
