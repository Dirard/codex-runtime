package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type executableLookupMode int

const (
	executableLookupDisabled executableLookupMode = iota
	executableLookupEnabled
)

type canonicalPath struct {
	path string
	key  string
	info os.FileInfo
}

func canonicalizeExistingDir(raw string, field string) (canonicalPath, error) {
	if raw == "" {
		return canonicalPath{}, fmt.Errorf("%s is required", field)
	}
	if !filepath.IsAbs(raw) {
		return canonicalPath{}, fmt.Errorf("%s must be absolute", field)
	}
	info, err := os.Stat(raw)
	if err != nil {
		return canonicalPath{}, fmt.Errorf("%s must exist: %w", field, err)
	}
	if !info.IsDir() {
		return canonicalPath{}, fmt.Errorf("%s must be a directory", field)
	}
	canonical, err := filepath.EvalSymlinks(raw)
	if err != nil {
		return canonicalPath{}, fmt.Errorf("%s must be canonicalizable: %w", field, err)
	}
	canonicalInfo, err := os.Stat(canonical)
	if err != nil {
		return canonicalPath{}, fmt.Errorf("%s canonical path must exist: %w", field, err)
	}
	if !canonicalInfo.IsDir() {
		return canonicalPath{}, fmt.Errorf("%s canonical path must be a directory", field)
	}
	return canonicalPath{
		path: filepath.Clean(canonical),
		key:  canonicalComparisonKey(canonical),
		info: canonicalInfo,
	}, nil
}

func resolveExecutable(raw string, field string) (string, error) {
	return validateExecutable(raw, field, executableLookupEnabled)
}

func resolveAbsoluteExecutable(raw string, field string) (string, error) {
	return validateExecutable(raw, field, executableLookupDisabled)
}

func validateExecutable(raw string, field string, lookupMode executableLookupMode) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("%s is required", field)
	}

	path := raw
	if !filepath.IsAbs(path) {
		if lookupMode == executableLookupDisabled {
			return "", fmt.Errorf("%s must be an absolute path", field)
		}
		if strings.ContainsAny(path, `/\`) {
			return "", fmt.Errorf("%s must be an absolute path or command name", field)
		}
		resolved, err := exec.LookPath(path)
		if err != nil {
			return "", fmt.Errorf("%s command not found: %w", field, err)
		}
		if !filepath.IsAbs(resolved) {
			return "", fmt.Errorf("%s lookup resolved to a relative path", field)
		}
		path = resolved
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%s must be canonicalizable: %w", field, err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("%s must exist: %w", field, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s must not be a directory", field)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s must be a regular file", field)
	}
	if err := validateExecutablePermission(canonical, info, field); err != nil {
		return "", err
	}
	if err := validateExecutableTrust(info, field); err != nil {
		return "", err
	}
	if err := validateExecutableParentTrust(canonical, field); err != nil {
		return "", err
	}
	return filepath.Clean(canonical), nil
}

func validateExecutablePermission(path string, info os.FileInfo, field string) error {
	if runtime.GOOS == "windows" {
		extension := strings.ToLower(filepath.Ext(path))
		if extension != ".exe" {
			return fmt.Errorf("%s must resolve to a .exe file on Windows", field)
		}
		return nil
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s must be executable", field)
	}
	return nil
}

func validateExecutableTrust(info os.FileInfo, field string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s must not be group- or world-writable", field)
	}
	return nil
}

func validateExecutableParentTrust(path string, field string) error {
	if runtime.GOOS == "windows" {
		return nil
	}

	for dir := filepath.Dir(path); ; {
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("%s parent directory must be inspectable: %w", field, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s parent path %q must be a directory", field, dir)
		}
		if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("%s parent directory %q must not be group- or world-writable", field, dir)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

func canonicalComparisonKey(path string) string {
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

func sameCanonicalPath(left canonicalPath, right canonicalPath) bool {
	if left.info != nil && right.info != nil && os.SameFile(left.info, right.info) {
		return true
	}
	return left.key == right.key
}
