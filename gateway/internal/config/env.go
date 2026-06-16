package config

import (
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

const codexHomeEnvName = "CODEX_HOME"

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var secretEnvNameFragments = []string{
	"token",
	"secret",
	"password",
	"passwd",
	"api_key",
	"apikey",
	"access_key",
	"refresh_token",
	"session",
	"cookie",
	"authorization",
	"credential",
	"client_secret",
}

// ChildEnvPolicy carries the validated parent env names that may be copied into a child Codex process.
type ChildEnvPolicy struct {
	allowlist []string
	forbidden map[string]struct{}
}

func newChildEnvPolicy(allowlist []string, forbidden map[string]struct{}) (ChildEnvPolicy, error) {
	if err := validateChildEnvAllowlist(allowlist, forbidden); err != nil {
		return ChildEnvPolicy{}, err
	}
	return ChildEnvPolicy{
		allowlist: append([]string(nil), allowlist...),
		forbidden: cloneEnvNameSet(forbidden),
	}, nil
}

func validateEnvName(name string) error {
	if !envNamePattern.MatchString(name) {
		return fmt.Errorf("invalid environment variable name %q", name)
	}
	return nil
}

func validateChildEnvAllowlist(names []string, forbidden map[string]struct{}) error {
	seen := map[string]struct{}{}
	for _, name := range names {
		if err := validateEnvName(name); err != nil {
			return err
		}
		if isReservedChildEnvName(name) {
			return fmt.Errorf("child env allowlist name %q is reserved", name)
		}
		key := envNameKey(name)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate child env allowlist name %q", name)
		}
		seen[key] = struct{}{}
		if isSecretLikeEnvName(name) {
			return fmt.Errorf("child env allowlist name %q looks secret-like", name)
		}
		if _, ok := forbidden[key]; ok {
			return fmt.Errorf("child env allowlist name %q matches a configured secret source", name)
		}
	}
	return nil
}

func validateChildEnvNameForCopy(name string, forbidden map[string]struct{}) error {
	if err := validateEnvName(name); err != nil {
		return err
	}
	if isSecretLikeEnvName(name) {
		return fmt.Errorf("child env name %q looks secret-like", name)
	}
	if _, ok := forbidden[envNameKey(name)]; ok {
		return fmt.Errorf("child env name %q matches a configured secret source", name)
	}
	return nil
}

func validateForbiddenEnvSourcesDoNotReachChild(forbidden map[string]struct{}) error {
	childNames := append([]string{codexHomeEnvName}, platformEssentialEnvNames()...)
	for _, name := range childNames {
		if _, ok := forbidden[envNameKey(name)]; ok {
			return fmt.Errorf("configured secret source %q would be copied to the child environment", name)
		}
	}
	return nil
}

func isSecretLikeEnvName(name string) bool {
	lower := strings.ToLower(name)
	for _, fragment := range secretEnvNameFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

func isReservedChildEnvName(name string) bool {
	return strings.EqualFold(name, codexHomeEnvName)
}

func (c *ValidatedConfig) BuildChildEnv(parent map[string]string, session SessionGroup) (map[string]string, error) {
	if c == nil {
		return nil, fmt.Errorf("validated config is nil")
	}
	return buildChildEnv(parent, session, c.childEnvPolicy)
}

func buildChildEnv(parent map[string]string, session SessionGroup, policy ChildEnvPolicy) (map[string]string, error) {
	if session.CanonicalCodexHome == "" {
		return nil, fmt.Errorf("session group canonical codex_home is required")
	}

	result := map[string]string{}
	result[codexHomeEnvName] = session.CanonicalCodexHome

	for _, name := range platformEssentialEnvNames() {
		if isReservedChildEnvName(name) {
			continue
		}
		if err := validateChildEnvNameForCopy(name, policy.forbidden); err != nil {
			return nil, err
		}
		if value, ok := lookupEnvInMap(parent, name); ok {
			result[name] = value
		}
	}
	for _, name := range policy.allowlist {
		if isReservedChildEnvName(name) {
			continue
		}
		if err := validateChildEnvNameForCopy(name, policy.forbidden); err != nil {
			return nil, err
		}
		if value, ok := lookupEnvInMap(parent, name); ok {
			result[name] = value
		}
	}
	return result, nil
}

func cloneEnvNameSet(source map[string]struct{}) map[string]struct{} {
	if len(source) == 0 {
		return nil
	}
	clone := make(map[string]struct{}, len(source))
	for name := range source {
		clone[name] = struct{}{}
	}
	return clone
}

func ChildEnvDiagnosticNames(env map[string]string) []string {
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func platformEssentialEnvNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "PATH", "TEMP", "TMP", "USERPROFILE", "APPDATA", "LOCALAPPDATA"}
	}
	return []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "LANG", "LC_ALL", "LC_CTYPE"}
}

func lookupEnvInMap(parent map[string]string, name string) (string, bool) {
	if runtime.GOOS != "windows" {
		value, ok := parent[name]
		return value, ok
	}
	for key, value := range parent {
		if strings.EqualFold(key, name) {
			return value, true
		}
	}
	return "", false
}

func envNameKey(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}
