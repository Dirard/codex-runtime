package appserver

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type pathIdentity struct {
	path string
	key  string
	info os.FileInfo
}

func VerifyCodexHomeIdentity(expected string, actual string) error {
	expectedIdentity, err := canonicalPathIdentity(expected)
	if err != nil {
		return fmt.Errorf("expected codex home: %w", err)
	}
	actualIdentity, err := canonicalPathIdentity(actual)
	if err != nil {
		return fmt.Errorf("actual codex home: %w", err)
	}
	if samePathIdentity(expectedIdentity, actualIdentity) {
		return nil
	}
	return fmt.Errorf("codex home mismatch")
}

func canonicalPathIdentity(path string) (pathIdentity, error) {
	if path == "" || !filepath.IsAbs(path) {
		return pathIdentity{}, fmt.Errorf("path must be absolute")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return pathIdentity{}, err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return pathIdentity{}, err
	}
	return pathIdentity{
		path: filepath.Clean(canonical),
		key:  identityComparisonKey(canonical),
		info: info,
	}, nil
}

func samePathIdentity(left pathIdentity, right pathIdentity) bool {
	if left.info != nil && right.info != nil && os.SameFile(left.info, right.info) {
		return true
	}
	return left.key == right.key
}

func validateExecutablePermission(path string, info os.FileInfo) error {
	if runtime.GOOS == "windows" {
		extension := strings.ToLower(filepath.Ext(path))
		if extension != ".exe" {
			return fmt.Errorf("executable must resolve to a .exe file on Windows")
		}
		return nil
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("executable must be executable")
	}
	return nil
}

func validateExecutableTrust(info os.FileInfo) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("executable must not be group- or world-writable")
	}
	return nil
}

func validateExecutableParentTrust(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}

	for dir := filepath.Dir(path); ; {
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("executable parent directory must be inspectable: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("executable parent path %q must be a directory", dir)
		}
		if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("executable parent directory %q must not be group- or world-writable", dir)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

func identityComparisonKey(path string) string {
	cleaned := filepath.Clean(path)
	if runtime.GOOS != "windows" {
		return cleaned
	}
	normalized := cleaned
	if strings.HasPrefix(normalized, `\\?\UNC\`) {
		normalized = `\\` + strings.TrimPrefix(normalized, `\\?\UNC\`)
	} else {
		normalized = strings.TrimPrefix(normalized, `\\?\`)
	}
	normalized = strings.TrimPrefix(normalized, `\??\`)
	normalized = filepath.ToSlash(normalized)
	normalized = strings.TrimRight(normalized, "/")
	return strings.ToLower(normalized)
}
