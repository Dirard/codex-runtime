package redact

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type PathSanitizer struct {
	cwd     canonicalPath
	home    string
	configs []string
}

type canonicalPath struct {
	path string
	key  string
	info os.FileInfo
}

func NewPathSanitizer(cwd string) (*PathSanitizer, error) {
	canonicalCWD, err := canonicalizeDir(cwd)
	if err != nil {
		return nil, err
	}
	sanitizer := &PathSanitizer{cwd: canonicalCWD}
	if home, err := os.UserHomeDir(); err == nil {
		sanitizer.home = canonicalComparisonKey(home)
	}
	for _, name := range []string{"APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME"} {
		if value := os.Getenv(name); value != "" {
			sanitizer.configs = append(sanitizer.configs, canonicalComparisonKey(value))
		}
	}
	return sanitizer, nil
}

func (s *PathSanitizer) Sanitize(path string) string {
	if s == nil || strings.TrimSpace(path) == "" || strings.ContainsRune(path, '\x00') {
		return PathMarker
	}

	target, err := canonicalizeTarget(path)
	if err != nil {
		return PathMarker
	}
	if sameOrInside(target.key, s.cwd.key) {
		rel, err := filepath.Rel(s.cwd.path, target.path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return PathMarker
		}
		if rel == "." {
			return "."
		}
		return filepath.ToSlash(rel)
	}
	if s.isHomeOrConfig(target.key) {
		return PathMarker
	}
	return PathMarker
}

func (s *PathSanitizer) SanitizeLabel(label string) string {
	label = strings.TrimSpace(label)
	if s == nil || label == "" || strings.ContainsRune(label, '\x00') {
		return PathMarker
	}
	if runtime.GOOS != "windows" && isWindowsAbsoluteLabel(label) {
		return PathMarker
	}
	path := normalizeLabelPath(label)
	if filepath.IsAbs(path) {
		return s.Sanitize(path)
	}
	if unsafePathLabelBeforeJoin(label) {
		return PathMarker
	}
	path = filepath.Join(s.cwd.path, path)
	return s.Sanitize(path)
}

func (s *PathSanitizer) RedactString(text string) string {
	if text == "" {
		return text
	}
	return redactFreeformPaths(text)
}

func (s *PathSanitizer) isHomeOrConfig(key string) bool {
	if s.home != "" && sameOrInside(key, s.home) {
		return true
	}
	for _, config := range s.configs {
		if sameOrInside(key, config) {
			return true
		}
	}
	return false
}

func canonicalizeDir(path string) (canonicalPath, error) {
	if path == "" || !filepath.IsAbs(path) {
		return canonicalPath{}, fmt.Errorf("path must be absolute")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return canonicalPath{}, err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return canonicalPath{}, err
	}
	if !info.IsDir() {
		return canonicalPath{}, fmt.Errorf("path must be a directory")
	}
	return canonicalPath{path: filepath.Clean(canonical), key: canonicalComparisonKey(canonical), info: info}, nil
}

func canonicalizeTarget(path string) (canonicalPath, error) {
	if !filepath.IsAbs(path) {
		return canonicalPath{}, fmt.Errorf("path must be absolute")
	}
	clean := filepath.Clean(path)
	if canonical, err := filepath.EvalSymlinks(clean); err == nil {
		info, statErr := os.Stat(canonical)
		if statErr != nil {
			return canonicalPath{}, statErr
		}
		return canonicalPath{path: filepath.Clean(canonical), key: canonicalComparisonKey(canonical), info: info}, nil
	}

	parent := clean
	var suffix []string
	for {
		info, err := os.Stat(parent)
		if err == nil {
			canonicalParent, err := filepath.EvalSymlinks(parent)
			if err != nil {
				return canonicalPath{}, err
			}
			canonical := filepath.Join(append([]string{canonicalParent}, reverseStrings(suffix)...)...)
			return canonicalPath{path: filepath.Clean(canonical), key: canonicalComparisonKey(canonical), info: info}, nil
		}
		next := filepath.Dir(parent)
		if next == parent {
			return canonicalPath{}, err
		}
		suffix = append(suffix, filepath.Base(parent))
		parent = next
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

func sameOrInside(pathKey string, parentKey string) bool {
	if pathKey == parentKey {
		return true
	}
	separator := string(filepath.Separator)
	if runtime.GOOS == "windows" {
		separator = "/"
	}
	return strings.HasPrefix(pathKey, strings.TrimRight(parentKey, separator)+separator)
}

func normalizeLabelPath(path string) string {
	if runtime.GOOS == "windows" {
		return path
	}
	return strings.ReplaceAll(path, `\`, string(filepath.Separator))
}

func isWindowsAbsoluteLabel(path string) bool {
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, `//`) {
		return true
	}
	if len(path) < 3 || path[1] != ':' {
		return false
	}
	drive := path[0]
	if !((drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z')) {
		return false
	}
	return path[2] == '\\' || path[2] == '/'
}

func unsafePathLabelBeforeJoin(label string) bool {
	normalized := strings.ReplaceAll(label, `\`, "/")
	if strings.HasPrefix(label, "~") ||
		hasWindowsDrivePrefix(label) ||
		strings.HasPrefix(normalized, "//") ||
		strings.HasPrefix(normalized, "/??/") ||
		strings.ContainsAny(label, "?#") ||
		looksLikeURI(label) ||
		looksLikeUserInfoLabel(label) {
		return true
	}
	return false
}

func hasWindowsDrivePrefix(path string) bool {
	if len(path) < 2 || path[1] != ':' {
		return false
	}
	drive := path[0]
	return (drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z')
}

func looksLikeURI(label string) bool {
	parsed, err := url.Parse(label)
	return err == nil && parsed.Scheme != ""
}

func looksLikeUserInfoLabel(label string) bool {
	segmentEnd := strings.IndexAny(label, `/\`)
	if segmentEnd < 0 {
		segmentEnd = len(label)
	}
	firstSegment := label[:segmentEnd]
	at := strings.Index(firstSegment, "@")
	if at <= 0 || at == len(firstSegment)-1 {
		return false
	}
	return strings.Contains(firstSegment[:at], ":") || strings.Contains(firstSegment[at+1:], ".")
}

func reverseStrings(values []string) []string {
	reversed := make([]string, len(values))
	for i := range values {
		reversed[len(values)-1-i] = values[i]
	}
	return reversed
}
