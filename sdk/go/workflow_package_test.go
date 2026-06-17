package codex

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestWorkflowDirAndZipProduceIdenticalPackageAndFingerprint(t *testing.T) {
	root := createWorkflowFixture(t, "workflow-a")
	zipBytes := zipWorkflowFixture(t, root)

	dirPackage := buildPackageFromDir(t, WorkflowDir{Namespace: "team-a", ID: "writer", Path: root})
	zipPackage := buildPackageFromZip(t, WorkflowZip{Namespace: "team-a", ID: "writer", Reader: bytes.NewReader(zipBytes)})

	assertPackageEntriesEqual(t, dirPackage, zipPackage)
	if dirPackage.PackageFingerprint != zipPackage.PackageFingerprint {
		t.Fatalf("fingerprint mismatch dir=%s zip=%s", dirPackage.PackageFingerprint, zipPackage.PackageFingerprint)
	}
}

func TestWorkflowPackageFingerprintExcludesSourcePathAndIdentity(t *testing.T) {
	root := createWorkflowFixture(t, "workflow-a")
	moved := filepath.Join(t.TempDir(), "renamed-workflow")
	if err := copyFixtureTree(root, moved); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}

	first := buildPackageFromDir(t, WorkflowDir{Namespace: "team-a", ID: "writer", Path: root})
	second := buildPackageFromDir(t, WorkflowDir{Namespace: "team-b", ID: "writer-renamed", Path: moved})

	if first.PackageFingerprint != second.PackageFingerprint {
		t.Fatalf("fingerprint changed after path/identity change: %s != %s", first.PackageFingerprint, second.PackageFingerprint)
	}
	if first.Namespace == second.Namespace || first.ID == second.ID {
		t.Fatalf("logical identity did not remain distinct: %#v %#v", first, second)
	}
}

func TestWorkflowPackageRequiresExplicitIdentityAndRootConfig(t *testing.T) {
	root := createWorkflowFixture(t, "workflow-a")
	for _, tc := range []struct {
		name   string
		source WorkflowSource
		reason string
	}{
		{name: "missing namespace dir", source: WorkflowDir{ID: "writer", Path: root}, reason: "namespace_required"},
		{name: "missing id dir", source: WorkflowDir{Namespace: "team-a", Path: root}, reason: "workflow_id_required"},
		{name: "missing namespace zip", source: WorkflowZip{ID: "writer", Reader: bytes.NewReader(zipWorkflowFixture(t, root))}, reason: "namespace_required"},
		{name: "missing id zip", source: WorkflowZip{Namespace: "team-a", Reader: bytes.NewReader(zipWorkflowFixture(t, root))}, reason: "workflow_id_required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildWorkflowPackage(tc.source)
			assertPackageReason(t, err, tc.reason)
		})
	}

	noConfig := t.TempDir()
	writeFile(t, noConfig, "agents/writer.toml", "name = \"writer\"\n")
	_, err := BuildWorkflowPackage(WorkflowDir{Namespace: "team-a", ID: "writer", Path: noConfig})
	assertPackageReason(t, err, "config_missing")
}

func TestWorkflowPackageRejectsUnsafeZipAndDuplicatePaths(t *testing.T) {
	for _, tc := range []struct {
		name   string
		files  map[string]string
		reason string
	}{
		{name: "traversal", files: map[string]string{"config.toml": "model = \"test\"\n", "../escape.toml": "x = 1\n"}, reason: "traversal_rejected"},
		{name: "absolute", files: map[string]string{"config.toml": "model = \"test\"\n", "/abs.toml": "x = 1\n"}, reason: "absolute_path_rejected"},
		{name: "case duplicate", files: map[string]string{"config.toml": "model = \"test\"\n", "CONFIG.TOML": "model = \"test\"\n"}, reason: "duplicate_path_rejected"},
		{name: "reserved", files: map[string]string{"config.toml": "model = \"test\"\n", ".git/config": "secret = false\n"}, reason: "reserved_path_rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			zipBytes := zipFiles(t, tc.files)
			_, err := BuildWorkflowPackage(WorkflowZip{Namespace: "team-a", ID: "writer", Reader: bytes.NewReader(zipBytes)})
			assertPackageReason(t, err, tc.reason)
		})
	}
}

func TestWorkflowPackageSizeLimitsAndSecretScanner(t *testing.T) {
	underLimit := t.TempDir()
	writeFile(t, underLimit, "config.toml", "model = \"test\"\n")
	writeFile(t, underLimit, "references/note.md", strings.Repeat("a", 128))
	if _, err := BuildWorkflowPackage(WorkflowDir{Namespace: "team-a", ID: "writer", Path: underLimit}, WithWorkflowPackageMaxBytes(1024)); err != nil {
		t.Fatalf("under-limit package returned error: %v", err)
	}

	overLimit := t.TempDir()
	writeFile(t, overLimit, "config.toml", "model = \"test\"\n")
	writeFile(t, overLimit, "references/large.md", strings.Repeat("a", 2048))
	_, err := BuildWorkflowPackage(WorkflowDir{Namespace: "team-a", ID: "writer", Path: overLimit}, WithWorkflowPackageMaxBytes(256))
	assertPackageReason(t, err, "package_too_large")

	secretRoot := t.TempDir()
	writeFile(t, secretRoot, "config.toml", "model = \"test\"\n")
	writeFile(t, secretRoot, "references/secret.md", "Authorization: Bearer sk-not-a-real-token-but-secret-shaped-1234567890\n")
	_, err = BuildWorkflowPackage(WorkflowDir{Namespace: "team-a", ID: "writer", Path: secretRoot})
	assertPackageReason(t, err, "secret_like_content_rejected")
	if err != nil && strings.Contains(err.Error(), "sk-not-a-real-token") {
		t.Fatalf("error leaked secret-like literal: %v", err)
	}
}

func TestWorkflowPackageDoesNotReadGlobalCodexHome(t *testing.T) {
	codexHome := t.TempDir()
	writeFile(t, codexHome, "config.toml", "Authorization: Bearer sk-global-secret-should-not-be-read-1234567890\n")
	t.Setenv("CODEX_HOME", codexHome)

	root := createWorkflowFixture(t, "workflow-a")
	if _, err := BuildWorkflowPackage(WorkflowDir{Namespace: "team-a", ID: "writer", Path: root}); err != nil {
		t.Fatalf("BuildWorkflowPackage read or reacted to CODEX_HOME: %v", err)
	}
}

func TestWorkflowPackageIncludesProjectInstructions(t *testing.T) {
	root := createWorkflowFixture(t, "workflow-agents-md")
	writeFile(t, root, "AGENTS.md", "Project marker alpha.\n")
	writeFile(t, root, "AGENTS.override.md", "Project override marker alpha.\n")

	pkg := buildPackageFromDir(t, WorkflowDir{Namespace: "team-a", ID: "writer", Path: root})

	assertPackageHasPath(t, pkg, "AGENTS.md")
	assertPackageHasPath(t, pkg, "AGENTS.override.md")
}

func TestWorkflowPackageProtoCopiesPackageContents(t *testing.T) {
	root := createWorkflowFixture(t, "workflow-a")
	pkg := buildPackageFromDir(t, WorkflowDir{Namespace: "team-a", ID: "writer", Path: root})
	protoPackage := pkg.Proto()

	if protoPackage.GetWorkflow().GetNamespace() != "team-a" || protoPackage.GetWorkflow().GetWorkflowId() != "writer" {
		t.Fatalf("proto workflow identity = %#v", protoPackage.GetWorkflow())
	}
	if protoPackage.GetPackageFingerprint() != pkg.PackageFingerprint {
		t.Fatalf("proto fingerprint = %q, want %q", protoPackage.GetPackageFingerprint(), pkg.PackageFingerprint)
	}
	if len(protoPackage.GetFiles()) != len(pkg.Files) {
		t.Fatalf("proto file count = %d, want %d", len(protoPackage.GetFiles()), len(pkg.Files))
	}
	protoPackage.Files[0].Contents[0] = 'X'
	if pkg.Files[0].Contents[0] == 'X' {
		t.Fatal("Proto shared mutable file contents with package")
	}
}

func assertPackageHasPath(t *testing.T, pkg *WorkflowPackage, want string) {
	t.Helper()
	for _, file := range pkg.Files {
		if file.Path == want {
			return
		}
	}
	t.Fatalf("package is missing path %q; files=%v", want, packagePaths(pkg))
}

func packagePaths(pkg *WorkflowPackage) []string {
	paths := make([]string, 0, len(pkg.Files))
	for _, file := range pkg.Files {
		paths = append(paths, file.Path)
	}
	return paths
}

func buildPackageFromDir(t *testing.T, source WorkflowDir) *WorkflowPackage {
	t.Helper()
	pkg, err := BuildWorkflowPackage(source)
	if err != nil {
		t.Fatalf("BuildWorkflowPackage(%#v) error: %v", source, err)
	}
	return pkg
}

func buildPackageFromZip(t *testing.T, source WorkflowZip) *WorkflowPackage {
	t.Helper()
	pkg, err := BuildWorkflowPackage(source)
	if err != nil {
		t.Fatalf("BuildWorkflowPackage(zip) error: %v", err)
	}
	return pkg
}

func createWorkflowFixture(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	writeFile(t, root, "config.toml", "model = \"test-model\"\n[mcp_servers.writer]\ncommand = \"writer_notes_search\"\n")
	writeFile(t, root, "agents/writer.toml", "name = \"writer\"\ninstructions = \"Use references.\"\n")
	writeFile(t, root, "skills/writer/SKILL.md", "# Writer\n")
	writeFile(t, root, "references/note.md", "Harbor fire notes.\n")
	return root
}

func writeFile(t *testing.T, root string, rel string, contents string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(fullPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func zipWorkflowFixture(t *testing.T, root string) []byte {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(contents)
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixture: %v", err)
	}
	return zipFiles(t, files)
}

func zipFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o644)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(files[name])); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func assertPackageEntriesEqual(t *testing.T, first *WorkflowPackage, second *WorkflowPackage) {
	t.Helper()
	if len(first.Files) != len(second.Files) {
		t.Fatalf("file count mismatch %d != %d", len(first.Files), len(second.Files))
	}
	for i := range first.Files {
		if first.Files[i].Path != second.Files[i].Path ||
			first.Files[i].SHA256 != second.Files[i].SHA256 ||
			first.Files[i].SizeBytes != second.Files[i].SizeBytes ||
			first.Files[i].Executable != second.Files[i].Executable {
			t.Fatalf("entry %d mismatch:\n%#v\n%#v", i, first.Files[i], second.Files[i])
		}
	}
}

func assertPackageReason(t *testing.T, err error, reason string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected package error reason %q, got nil", reason)
	}
	if !errors.Is(err, ErrInvalidWorkflowPackage) {
		t.Fatalf("error does not wrap ErrInvalidWorkflowPackage: %v", err)
	}
	var packageErr *WorkflowPackageError
	if !errors.As(err, &packageErr) {
		t.Fatalf("error type = %T, want WorkflowPackageError", err)
	}
	if packageErr.Reason != reason {
		t.Fatalf("package reason = %q, want %q (err=%v)", packageErr.Reason, reason, err)
	}
}

func copyFixtureTree(source string, target string) error {
	return filepath.WalkDir(source, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, filePath)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		targetPath := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		contents, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(targetPath, contents, 0o644)
	})
}
