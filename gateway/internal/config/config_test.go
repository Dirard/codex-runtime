package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

const testTokenEnv = "CODEX_GATEWAY_TEST_TOKEN"

type testFixture struct {
	binary string
	cwd    string
	home   string
}

type sessionTOMLOptions struct {
	id           string
	workspace    string
	cwd          string
	home         string
	sessionExtra string
	runtimeExtra string
	replayExtra  string
	threadExtra  string
	pendingExtra string
	grpcExtra    string
}

func TestValidConfigLoadsAndBuildsMinimalChildEnv(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)

	cfg, err := loadConfigFromText(t, validConfigTOML(t, fixture, ""))
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if cfg.Listen != "127.0.0.1:0" {
		t.Fatalf("Listen = %q, want %q", cfg.Listen, "127.0.0.1:0")
	}
	assertSameFile(t, cfg.CodexBinary, fixture.binary)

	group, ok := cfg.SessionGroup("sg-1")
	if !ok {
		t.Fatal("SessionGroup(sg-1) not found")
	}
	if group.CanonicalCWD == "" || group.CanonicalCodexHome == "" {
		t.Fatal("canonical cwd and codex_home must be populated")
	}

	childEnv, err := cfg.BuildChildEnv(map[string]string{
		"PATH":             "path-value",
		"GATEWAY_SAFE_ENV": "safe-value",
		"UNSAFE_EXTRA_ENV": "must-not-inherit",
	}, group)
	if err != nil {
		t.Fatalf("BuildChildEnv() error = %v", err)
	}
	if childEnv["CODEX_HOME"] != group.CanonicalCodexHome {
		t.Fatalf("CODEX_HOME = %q, want canonical codex home", childEnv["CODEX_HOME"])
	}
	if childEnv["GATEWAY_SAFE_ENV"] != "safe-value" {
		t.Fatal("allowlisted child env name was not copied")
	}
	if _, ok := childEnv["UNSAFE_EXTRA_ENV"]; ok {
		t.Fatal("child env inherited a non-allowlisted variable")
	}

	diagnostic := strings.Join(ChildEnvDiagnosticNames(childEnv), ",")
	if strings.Contains(diagnostic, "safe-value") || strings.Contains(diagnostic, "must-not-inherit") {
		t.Fatalf("diagnostic env summary leaked an env value: %q", diagnostic)
	}
}

func TestStrictSchemaVerificationConfig(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)

	tests := []struct {
		name string
		toml string
		want bool
	}{
		{
			name: "absent defaults false",
			toml: validConfigTOML(t, fixture, ""),
			want: false,
		},
		{
			name: "explicit true",
			toml: strings.Replace(
				validConfigTOML(t, fixture, ""),
				"listen = \"127.0.0.1:0\"\n",
				"listen = \"127.0.0.1:0\"\nstrict_schema_verification = true\n",
				1,
			),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadConfigFromText(t, tt.toml)
			if err != nil {
				t.Fatalf("LoadFile() error = %v", err)
			}
			if cfg.StrictSchemaVerification != tt.want {
				t.Fatalf("StrictSchemaVerification = %v, want %v", cfg.StrictSchemaVerification, tt.want)
			}
			if cfg.StrictSchemaVerificationEnabled() != tt.want {
				t.Fatalf("StrictSchemaVerificationEnabled() = %v, want %v", cfg.StrictSchemaVerificationEnabled(), tt.want)
			}
		})
	}
}

func TestChatRuntimeEnabledConfig(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)

	tests := []struct {
		name string
		toml string
		want bool
	}{
		{
			name: "absent defaults true",
			toml: validConfigTOML(t, fixture, ""),
			want: true,
		},
		{
			name: "explicit true",
			toml: configTOMLWithChatRuntime(t, fixture, "true"),
			want: true,
		},
		{
			name: "explicit false",
			toml: configTOMLWithChatRuntime(t, fixture, "false"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadConfigFromText(t, tt.toml)
			if err != nil {
				t.Fatalf("LoadFile() error = %v", err)
			}
			if cfg.ChatRuntime.Enabled != tt.want {
				t.Fatalf("ChatRuntime.Enabled = %v, want %v", cfg.ChatRuntime.Enabled, tt.want)
			}
			if cfg.ChatRuntimeEnabled() != tt.want {
				t.Fatalf("ChatRuntimeEnabled() = %v, want %v", cfg.ChatRuntimeEnabled(), tt.want)
			}
		})
	}
}

func TestBuildChildEnvRequiresCanonicalCodexHome(t *testing.T) {
	if _, err := buildChildEnv(map[string]string{}, SessionGroup{CodexHome: "/raw/home"}, ChildEnvPolicy{}); err == nil {
		t.Fatal("BuildChildEnv() succeeded without canonical codex_home")
	}
}

func TestBuildChildEnvDoesNotAllowReservedOverride(t *testing.T) {
	session := SessionGroup{CanonicalCodexHome: "configured-codex-home"}

	childEnv, err := buildChildEnv(map[string]string{
		"CODEX_HOME": "parent-codex-home",
		"Codex_Home": "mixed-parent-codex-home",
	}, session, ChildEnvPolicy{allowlist: []string{"CODEX_HOME", "Codex_Home"}})
	if err != nil {
		t.Fatalf("BuildChildEnv() error = %v", err)
	}
	if childEnv["CODEX_HOME"] != session.CanonicalCodexHome {
		t.Fatalf("CODEX_HOME = %q, want canonical codex home", childEnv["CODEX_HOME"])
	}
	if _, ok := childEnv["Codex_Home"]; ok {
		t.Fatal("mixed-case reserved child env name was copied")
	}
}

func TestBuildChildEnvRejectsUnsafePolicyNames(t *testing.T) {
	session := SessionGroup{CanonicalCodexHome: "configured-codex-home"}

	tests := []struct {
		name   string
		parent map[string]string
		policy ChildEnvPolicy
	}{
		{
			name:   "secret-like allowlist",
			parent: map[string]string{"OPENAI_API_KEY": "must-not-copy"},
			policy: ChildEnvPolicy{allowlist: []string{"OPENAI_API_KEY"}},
		},
		{
			name:   "configured secret source",
			parent: map[string]string{"PROVIDER_GATEWAY_ENV": "must-not-copy"},
			policy: ChildEnvPolicy{
				allowlist: []string{"PROVIDER_GATEWAY_ENV"},
				forbidden: map[string]struct{}{envNameKey("PROVIDER_GATEWAY_ENV"): {}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if childEnv, err := buildChildEnv(tt.parent, session, tt.policy); err == nil {
				t.Fatalf("buildChildEnv() succeeded with env %v, want validation error", ChildEnvDiagnosticNames(childEnv))
			}
		})
	}
}

func TestValidatedConfigBuildChildEnvIgnoresUnvalidatedPublicFields(t *testing.T) {
	cfg := &ValidatedConfig{
		ChildEnvAllowlist: []string{"OPENAI_API_KEY", "PROVIDER_GATEWAY_ENV"},
		CredentialProviders: []CredentialProvider{
			{EnvSources: []string{"PROVIDER_GATEWAY_ENV"}},
		},
	}
	session := SessionGroup{CanonicalCodexHome: "configured-codex-home"}

	childEnv, err := cfg.BuildChildEnv(map[string]string{
		"OPENAI_API_KEY":       "must-not-copy",
		"PROVIDER_GATEWAY_ENV": "must-not-copy",
	}, session)
	if err != nil {
		t.Fatalf("BuildChildEnv() error = %v", err)
	}
	if _, ok := childEnv["OPENAI_API_KEY"]; ok {
		t.Fatal("BuildChildEnv() copied a secret-like public allowlist field")
	}
	if _, ok := childEnv["PROVIDER_GATEWAY_ENV"]; ok {
		t.Fatal("BuildChildEnv() copied an unvalidated provider env source")
	}
}

func TestConfigRejectsUnsafePathsAndDuplicateSessionGroups(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)

	tests := []struct {
		name string
		toml string
	}{
		{
			name: "relative cwd",
			toml: validConfigTOML(t, fixture, sessionGroupTOML(t, sessionTOMLOptions{
				id:        "sg-1",
				workspace: "ws-1",
				cwd:       "relative-workspace",
				home:      fixture.home,
			})),
		},
		{
			name: "duplicate session id",
			toml: validConfigTOML(t, fixture,
				defaultSessionTOML(t, fixture)+
					sessionGroupTOML(t, sessionTOMLOptions{
						id:        "sg-1",
						workspace: "ws-2",
						cwd:       t.TempDir(),
						home:      t.TempDir(),
					})),
		},
		{
			name: "duplicate codex home identity",
			toml: validConfigTOML(t, fixture,
				defaultSessionTOML(t, fixture)+
					sessionGroupTOML(t, sessionTOMLOptions{
						id:        "sg-2",
						workspace: "ws-2",
						cwd:       t.TempDir(),
						home:      fixture.home,
					})),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := loadConfigFromText(t, tt.toml); err == nil {
				t.Fatal("LoadFile() succeeded, want error")
			}
		})
	}
}

func TestConfigRejectsMissingOrBlankWorkspaceID(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing",
			body: sessionGroupWithoutWorkspaceTOML(t, fixture, ""),
		},
		{
			name: "blank",
			body: sessionGroupTOML(t, sessionTOMLOptions{
				id:        "sg-1",
				workspace: "",
				cwd:       fixture.cwd,
				home:      fixture.home,
			}),
		},
		{
			name: "whitespace",
			body: sessionGroupTOML(t, sessionTOMLOptions{
				id:        "sg-1",
				workspace: "   ",
				cwd:       fixture.cwd,
				home:      fixture.home,
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := loadConfigFromText(t, validConfigTOML(t, fixture, tt.body)); err == nil {
				t.Fatal("LoadFile() succeeded, want workspace_id validation error")
			}
		})
	}
}

func TestConfigRejectsInvalidPublicIDs(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)
	overCapID := strings.Repeat("x", domain.MaxPublicIDBytes+1)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "over-cap session group id",
			body: sessionGroupTOML(t, sessionTOMLOptions{
				id:        overCapID,
				workspace: "ws-1",
				cwd:       fixture.cwd,
				home:      fixture.home,
			}),
		},
		{
			name: "over-cap workspace id",
			body: sessionGroupTOML(t, sessionTOMLOptions{
				id:        "sg-1",
				workspace: overCapID,
				cwd:       fixture.cwd,
				home:      fixture.home,
			}),
		},
		{
			name: "whitespace-padded session group id",
			body: sessionGroupTOML(t, sessionTOMLOptions{
				id:        " sg-1 ",
				workspace: "ws-1",
				cwd:       fixture.cwd,
				home:      fixture.home,
			}),
		},
		{
			name: "whitespace-padded workspace id",
			body: sessionGroupTOML(t, sessionTOMLOptions{
				id:        "sg-1",
				workspace: " ws-1 ",
				cwd:       fixture.cwd,
				home:      fixture.home,
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := loadConfigFromText(t, validConfigTOML(t, fixture, tt.body)); err == nil {
				t.Fatal("LoadFile() succeeded, want public id validation error")
			}
		})
	}
}

func TestConfigRejectsDuplicateCodexHomeAliases(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)

	t.Run("symlink alias", func(t *testing.T) {
		aliasHome := filepath.Join(t.TempDir(), "home-alias")
		if err := os.Symlink(fixture.home, aliasHome); err != nil {
			t.Skipf("directory symlink not supported in this environment: %v", err)
		}
		toml := duplicateCodexHomeAliasTOML(t, fixture, aliasHome)
		if _, err := loadConfigFromText(t, toml); err == nil {
			t.Fatal("LoadFile() accepted duplicate codex_home symlink alias")
		}
	})

	if runtime.GOOS == "windows" {
		t.Run("long path prefix alias", func(t *testing.T) {
			aliasHome := `\\?\` + filepath.Clean(fixture.home)
			toml := duplicateCodexHomeAliasTOML(t, fixture, aliasHome)
			if _, err := loadConfigFromText(t, toml); err == nil {
				t.Fatal("LoadFile() accepted duplicate codex_home long-path-prefix alias")
			}
		})

		t.Run("case alias", func(t *testing.T) {
			aliasHome := strings.ToUpper(fixture.home)
			toml := duplicateCodexHomeAliasTOML(t, fixture, aliasHome)
			if _, err := loadConfigFromText(t, toml); err == nil {
				t.Fatal("LoadFile() accepted duplicate codex_home case alias")
			}
		})
	}
}

func TestConfigRejectsExecutableValidationErrors(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)

	if runtime.GOOS == "windows" {
		textFile := writeTestFile(t, "codex-test.txt", 0o755)
		toml := strings.Replace(validConfigTOML(t, fixture, ""), strconv.Quote(fixture.binary), strconv.Quote(textFile), 1)
		if _, err := loadConfigFromText(t, toml); err == nil {
			t.Fatal("LoadFile() accepted a Windows codex_binary without a .exe extension")
		}

		providerTextFile := writeTestFile(t, "provider-test.txt", 0o755)
		toml = validConfigTOML(t, fixture, credentialProviderTOML(t, "provider-1", providerTextFile, []string{"PROVIDER_ENV"})+defaultSessionTOML(t, fixture))
		if _, err := loadConfigFromText(t, toml); err == nil {
			t.Fatal("LoadFile() accepted a Windows credential provider without a .exe extension")
		}
		return
	}

	nonExecutable := writeTestFile(t, "codex-test", 0o644)
	toml := strings.Replace(validConfigTOML(t, fixture, ""), strconv.Quote(fixture.binary), strconv.Quote(nonExecutable), 1)
	if _, err := loadConfigFromText(t, toml); err == nil {
		t.Fatal("LoadFile() accepted a non-executable codex_binary")
	}

	worldWritable := writeTestFile(t, "codex-test", 0o777)
	toml = strings.Replace(validConfigTOML(t, fixture, ""), strconv.Quote(fixture.binary), strconv.Quote(worldWritable), 1)
	if _, err := loadConfigFromText(t, toml); err == nil {
		t.Fatal("LoadFile() accepted a group/world-writable codex_binary")
	}

	unsafeParent := filepath.Join(t.TempDir(), "unsafe-parent")
	if err := os.Mkdir(unsafeParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafeParent, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(unsafeParent, 0o755)
	})

	unsafeParentCodex := writeTestFileInDir(t, unsafeParent, "codex-test", 0o755)
	toml = strings.Replace(validConfigTOML(t, fixture, ""), strconv.Quote(fixture.binary), strconv.Quote(unsafeParentCodex), 1)
	if _, err := loadConfigFromText(t, toml); err == nil {
		t.Fatal("LoadFile() accepted a codex_binary in a writable parent directory")
	}

	providerNonExecutable := writeTestFile(t, "provider-test", 0o644)
	toml = validConfigTOML(t, fixture, credentialProviderTOML(t, "provider-1", providerNonExecutable, []string{"PROVIDER_ENV"})+defaultSessionTOML(t, fixture))
	if _, err := loadConfigFromText(t, toml); err == nil {
		t.Fatal("LoadFile() accepted a non-executable credential provider")
	}

	unsafeParentProvider := writeTestFileInDir(t, unsafeParent, "provider-test", 0o755)
	toml = validConfigTOML(t, fixture, credentialProviderTOML(t, "provider-1", unsafeParentProvider, []string{"PROVIDER_ENV"})+defaultSessionTOML(t, fixture))
	if _, err := loadConfigFromText(t, toml); err == nil {
		t.Fatal("LoadFile() accepted a credential provider in a writable parent directory")
	}
}

func TestConfigRejectsDuplicateKeys(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)

	tests := []struct {
		name string
		toml string
	}{
		{
			name: "root",
			toml: strings.Replace(validConfigTOML(t, fixture, ""), `listen = "127.0.0.1:0"`, `listen = "127.0.0.1:0"`+"\n"+`listen = "localhost:0"`, 1),
		},
		{
			name: "token source",
			toml: strings.Replace(validConfigTOML(t, fixture, ""), "env = "+strconv.Quote(testTokenEnv), "env = "+strconv.Quote(testTokenEnv)+"\n"+"env = "+strconv.Quote(testTokenEnv), 1),
		},
		{
			name: "chat runtime",
			toml: strings.Replace(configTOMLWithChatRuntime(t, fixture, "false"), "enabled = false", "enabled = false\n"+"enabled = true", 1),
		},
		{
			name: "credential provider",
			toml: validConfigTOML(t, fixture, strings.Replace(credentialProviderTOML(t, "provider-1", fixture.binary, []string{"PROVIDER_ENV"}), `provider_id = "provider-1"`, `provider_id = "provider-1"`+"\n"+`provider_id = "provider-2"`, 1)+defaultSessionTOML(t, fixture)),
		},
		{
			name: "session group",
			toml: validConfigTOML(t, fixture, sessionGroupTOML(t, sessionTOMLOptions{
				id:           "sg-1",
				workspace:    "ws-1",
				cwd:          fixture.cwd,
				home:         fixture.home,
				sessionExtra: "cwd = " + strconv.Quote(fixture.cwd) + "\n",
			})),
		},
		{
			name: "nested runtime table",
			toml: validConfigTOML(t, fixture, sessionGroupTOML(t, sessionTOMLOptions{
				id:           "sg-1",
				workspace:    "ws-1",
				cwd:          fixture.cwd,
				home:         fixture.home,
				runtimeExtra: `approval_policy = "on-failure"` + "\n",
			})),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := loadConfigFromText(t, tt.toml); err == nil {
				t.Fatal("LoadFile() succeeded, want duplicate key error")
			}
		})
	}
}

func TestStrictTOMLSubsetParsing(t *testing.T) {
	cfg, err := ParseTOML([]byte(`
# comments and trailing comments are allowed
codex_binary = "C:\\codex\\codex.exe" # comment
listen = "127.0.0.1:0"
child_env_allowlist = ["SAFE_ONE", "SAFE_TWO"]

[chat_runtime]
enabled = false

[client_auth_token_source]
env = "TOKEN_ENV"
`))
	if err != nil {
		t.Fatalf("ParseTOML() error = %v", err)
	}
	if cfg.CodexBinary != `C:\codex\codex.exe` {
		t.Fatalf("CodexBinary = %q, want Windows path", cfg.CodexBinary)
	}
	if got := strings.Join(cfg.ChildEnvAllowlist, ","); got != "SAFE_ONE,SAFE_TWO" {
		t.Fatalf("ChildEnvAllowlist = %q, want SAFE_ONE,SAFE_TWO", got)
	}
	if cfg.ChatRuntime.Enabled {
		t.Fatal("ChatRuntime.Enabled = true, want false")
	}

	rejected := []string{
		`listen = '127.0.0.1:0'`,
		`listen = "\x31"`,
		`listen = """127.0.0.1:0"""`,
		`child_env_allowlist = null`,
		`[[credential_providers]]
provider_id = "provider-1"
executable = "C:\\codex\\provider.exe"
env_sources = null`,
		`[chat_runtime]
enabled = "false"`,
		`[chat_runtime]
unknown = false`,
	}
	for _, text := range rejected {
		t.Run(text, func(t *testing.T) {
			if _, err := ParseTOML([]byte(text)); err == nil {
				t.Fatal("ParseTOML() succeeded for TOML outside the gateway subset")
			}
		})
	}
}

func TestConfigRejectsNonLoopbackListen(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)

	rejected := strings.Replace(validConfigTOML(t, fixture, ""), `listen = "127.0.0.1:0"`, `listen = "0.0.0.0:8080"`, 1)
	if _, err := loadConfigFromText(t, rejected); err == nil {
		t.Fatal("LoadFile() accepted a non-loopback listen address")
	}
}

func TestListenAddressValidation(t *testing.T) {
	for _, address := range []string{"localhost:1", "127.0.0.1:0", "[::1]:65535"} {
		t.Run("accept "+address, func(t *testing.T) {
			if err := ValidateListenAddress(address); err != nil {
				t.Fatalf("ValidateListenAddress(%q) error = %v", address, err)
			}
		})
	}

	for _, address := range []string{"0.0.0.0:1", "[::]:1", ":1", "192.168.1.10:1", "10.0.0.1:1", "8.8.8.8:1", "example.com:1"} {
		t.Run("reject "+address, func(t *testing.T) {
			if err := ValidateListenAddress(address); err == nil {
				t.Fatalf("ValidateListenAddress(%q) succeeded, want error", address)
			}
		})
	}
}

func TestConfigRejectsProfileLikeAndUnsafeRuntimePolicies(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)

	tests := []struct {
		name string
		toml string
	}{
		{
			name: "profile like session field",
			toml: validConfigTOML(t, fixture, sessionGroupTOML(t, sessionTOMLOptions{
				id:           "sg-1",
				workspace:    "ws-1",
				cwd:          fixture.cwd,
				home:         fixture.home,
				sessionExtra: `codex_profile = "trusted-profile"` + "\n",
			})),
		},
		{
			name: "approval never",
			toml: strings.Replace(validConfigTOML(t, fixture, ""), `approval_policy = "on-request"`, `approval_policy = "never"`, 1),
		},
		{
			name: "granular approval policy override",
			toml: validConfigTOML(t, fixture, sessionGroupTOML(t, sessionTOMLOptions{
				id:           "sg-1",
				workspace:    "ws-1",
				cwd:          fixture.cwd,
				home:         fixture.home,
				runtimeExtra: `granular_approval_policy = "auto"` + "\n",
			})),
		},
		{
			name: "auto reviewer",
			toml: strings.Replace(validConfigTOML(t, fixture, ""), `approvals_reviewer = "user"`, `approvals_reviewer = "auto_review"`, 1),
		},
		{
			name: "sandbox and permissions",
			toml: validConfigTOML(t, fixture, sessionGroupTOML(t, sessionTOMLOptions{
				id:           "sg-1",
				workspace:    "ws-1",
				cwd:          fixture.cwd,
				home:         fixture.home,
				runtimeExtra: `permissions_profile_id = "trusted-profile"` + "\n",
			})),
		},
		{
			name: "whitespace permissions profile id",
			toml: validConfigTOML(t, fixture, sessionGroupTOML(t, sessionTOMLOptions{
				id:           "sg-1",
				workspace:    "ws-1",
				cwd:          fixture.cwd,
				home:         fixture.home,
				runtimeExtra: `permissions_profile_id = "   "` + "\n",
			})),
		},
		{
			name: "danger full access without reason",
			toml: strings.Replace(validConfigTOML(t, fixture, ""), `sandbox_mode = "workspace-write"`, `sandbox_mode = "danger-full-access"`, 1),
		},
		{
			name: "danger full access whitespace reason",
			toml: strings.Replace(validConfigTOML(t, fixture, ""), `sandbox_mode = "workspace-write"`, `sandbox_mode = "danger-full-access"`+"\n"+`danger_full_access_reason = "   "`, 1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := loadConfigFromText(t, tt.toml); err == nil {
				t.Fatal("LoadFile() succeeded, want error")
			}
		})
	}
}

func TestConfigRejectsInvalidAuthTokenSources(t *testing.T) {
	fixture := newTestFixture(t)

	envCases := map[string]string{
		"empty":                 "",
		"spaces":                "   ",
		"leading":               " token",
		"trailing":              "token ",
		"inside":                "to ken",
		"multiline":             "line\nbreak",
		"non ascii":             "token-é",
		"control character":     "token\x1fvalue",
		"colon separator":       "token:value",
		"comma separator":       "token,value",
		"quote separator":       `token"value`,
		"backslash separator":   `token\value`,
		"padding in the middle": "token=value",
		"only padding":          "==",
	}
	for name, value := range envCases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(testTokenEnv, value)
			if _, err := loadConfigFromText(t, validConfigTOML(t, fixture, "")); err == nil {
				t.Fatal("LoadFile() succeeded, want token validation error")
			}
		})
	}

	t.Run("missing env", func(t *testing.T) {
		toml := strings.Replace(validConfigTOML(t, fixture, ""), strconv.Quote(testTokenEnv), strconv.Quote("MISSING_GATEWAY_TEST_TOKEN"), 1)
		if _, err := loadConfigFromText(t, toml); err == nil {
			t.Fatal("LoadFile() succeeded, want missing token env error")
		}
	})

	t.Run("file strips one trailing newline", func(t *testing.T) {
		tokenFile := filepath.Join(t.TempDir(), "token.txt")
		if err := os.WriteFile(tokenFile, []byte("file-token\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		toml := strings.Replace(validConfigTOML(t, fixture, ""), "[client_auth_token_source]\nenv = "+strconv.Quote(testTokenEnv), "[client_auth_token_source]\nfile = "+strconv.Quote(tokenFile), 1)
		if _, err := loadConfigFromText(t, toml); err != nil {
			t.Fatalf("LoadFile() error = %v", err)
		}
	})

	t.Run("file rejects nul", func(t *testing.T) {
		tokenFile := filepath.Join(t.TempDir(), "token.txt")
		if err := os.WriteFile(tokenFile, []byte("file\x00token"), 0o600); err != nil {
			t.Fatal(err)
		}
		toml := strings.Replace(validConfigTOML(t, fixture, ""), "[client_auth_token_source]\nenv = "+strconv.Quote(testTokenEnv), "[client_auth_token_source]\nfile = "+strconv.Quote(tokenFile), 1)
		if _, err := loadConfigFromText(t, toml); err == nil {
			t.Fatal("LoadFile() succeeded, want NUL token error")
		}
	})
}

func TestConfigRejectsUnsafeChildEnvAllowlist(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)
	credentialProviderForbiddenTOML := validConfigTOML(t, fixture, credentialProviderTOML(t, "provider-1", fixture.binary, []string{"PROVIDER_GATEWAY_ENV"})+
		strings.Replace(defaultSessionTOML(t, fixture), `workspace_id = "ws-1"`, `workspace_id = "ws-1"`+"\n"+`credential_provider_id = "provider-1"`, 1))
	credentialProviderForbiddenTOML = strings.Replace(credentialProviderForbiddenTOML, `child_env_allowlist = ["GATEWAY_SAFE_ENV"]`, `child_env_allowlist = ["PROVIDER_GATEWAY_ENV"]`, 1)

	tests := []struct {
		name string
		toml string
	}{
		{
			name: "secret like name",
			toml: strings.Replace(validConfigTOML(t, fixture, ""), `child_env_allowlist = ["GATEWAY_SAFE_ENV"]`, `child_env_allowlist = ["OPENAI_API_KEY"]`, 1),
		},
		{
			name: "bearer token source name",
			toml: strings.Replace(validConfigTOML(t, fixture, ""), `child_env_allowlist = ["GATEWAY_SAFE_ENV"]`, `child_env_allowlist = [`+strconv.Quote(testTokenEnv)+`]`, 1),
		},
		{
			name: "reserved codex home",
			toml: strings.Replace(validConfigTOML(t, fixture, ""), `child_env_allowlist = ["GATEWAY_SAFE_ENV"]`, `child_env_allowlist = ["CODEX_HOME"]`, 1),
		},
		{
			name: "reserved codex home mixed case",
			toml: strings.Replace(validConfigTOML(t, fixture, ""), `child_env_allowlist = ["GATEWAY_SAFE_ENV"]`, `child_env_allowlist = ["Codex_Home"]`, 1),
		},
		{
			name: "credential provider secret source name",
			toml: credentialProviderForbiddenTOML,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := loadConfigFromText(t, tt.toml); err == nil {
				t.Fatal("LoadFile() succeeded, want child env validation error")
			}
		})
	}
}

func TestConfigValidatesThreadBindingPendingAndGRPCLimits(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)

	tests := []struct {
		name string
		toml string
	}{
		{
			name: "zero thread bindings",
			toml: validConfigTOML(t, fixture, sessionGroupTOML(t, sessionTOMLOptions{
				id:          "sg-1",
				workspace:   "ws-1",
				cwd:         fixture.cwd,
				home:        fixture.home,
				threadExtra: "max_bindings = 0\n",
			})),
		},
		{
			name: "thread ttl above cap",
			toml: validConfigTOML(t, fixture, sessionGroupTOML(t, sessionTOMLOptions{
				id:          "sg-1",
				workspace:   "ws-1",
				cwd:         fixture.cwd,
				home:        fixture.home,
				threadExtra: "ttl_millis = 604800001\n",
			})),
		},
		{
			name: "grpc outbound above cap",
			toml: validConfigTOML(t, fixture, sessionGroupTOML(t, sessionTOMLOptions{
				id:        "sg-1",
				workspace: "ws-1",
				cwd:       fixture.cwd,
				home:      fixture.home,
				grpcExtra: "outbound_message_bytes = 8388609\n",
			})),
		},
		{
			name: "status safe pending budget violation",
			toml: validConfigTOML(t, fixture, sessionGroupTOML(t, sessionTOMLOptions{
				id:           "sg-1",
				workspace:    "ws-1",
				cwd:          fixture.cwd,
				home:         fixture.home,
				pendingExtra: "max_active_requests = 64\nmax_display_payload_bytes = 65536\nstatus_non_pending_budget_bytes = 262144\n",
				grpcExtra:    "outbound_message_bytes = 4194304\n",
			})),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := loadConfigFromText(t, tt.toml); err == nil {
				t.Fatal("LoadFile() succeeded, want limit validation error")
			}
		})
	}
}

func TestConfigRejectsCredentialProviderValidationErrors(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)

	relativeProvider := validConfigTOML(t, fixture, credentialProviderTOML(t, "provider-1", "relative-provider", []string{"PROVIDER_ENV"})+defaultSessionTOML(t, fixture))
	if _, err := loadConfigFromText(t, relativeProvider); err == nil {
		t.Fatal("LoadFile() accepted a relative credential provider executable")
	}

	unknownProvider := validConfigTOML(t, fixture, strings.Replace(defaultSessionTOML(t, fixture), `workspace_id = "ws-1"`, `workspace_id = "ws-1"`+"\n"+`credential_provider_id = "missing"`, 1))
	if _, err := loadConfigFromText(t, unknownProvider); err == nil {
		t.Fatal("LoadFile() accepted an unknown session credential provider id")
	}

	timeoutOverCap := validConfigTOML(t, fixture, credentialProviderTOML(t, "provider-1", fixture.binary, []string{"PROVIDER_ENV"})+"timeout_millis = 30001\n"+defaultSessionTOML(t, fixture))
	if _, err := loadConfigFromText(t, timeoutOverCap); err == nil {
		t.Fatal("LoadFile() accepted a provider timeout above the hard cap")
	}

	duplicateProviderIDs := validConfigTOML(t, fixture,
		credentialProviderTOML(t, "provider-1", fixture.binary, []string{"PROVIDER_ENV_ONE"})+
			credentialProviderTOML(t, "provider-1", fixture.binary, []string{"PROVIDER_ENV_TWO"})+
			defaultSessionTOML(t, fixture))
	if _, err := loadConfigFromText(t, duplicateProviderIDs); err == nil {
		t.Fatal("LoadFile() accepted duplicate credential provider ids")
	}

	stdoutOverCap := validConfigTOML(t, fixture, credentialProviderTOML(t, "provider-1", fixture.binary, []string{"PROVIDER_ENV"})+"stdout_bytes = 65537\n"+defaultSessionTOML(t, fixture))
	if _, err := loadConfigFromText(t, stdoutOverCap); err == nil {
		t.Fatal("LoadFile() accepted provider stdout cap above the hard cap")
	}

	stderrOverCap := validConfigTOML(t, fixture, credentialProviderTOML(t, "provider-1", fixture.binary, []string{"PROVIDER_ENV"})+"stderr_bytes = 32769\n"+defaultSessionTOML(t, fixture))
	if _, err := loadConfigFromText(t, stderrOverCap); err == nil {
		t.Fatal("LoadFile() accepted provider stderr cap above the hard cap")
	}
}

func TestCredentialProviderWorkdirCanonicalized(t *testing.T) {
	t.Setenv(testTokenEnv, "valid-test-token")
	fixture := newTestFixture(t)

	provider := credentialProviderTOML(t, "provider-1", fixture.binary, []string{"PROVIDER_ENV"}) +
		"workdir = " + strconv.Quote(fixture.cwd) + "\n"
	toml := validConfigTOML(t, fixture,
		provider+
			strings.Replace(defaultSessionTOML(t, fixture), `workspace_id = "ws-1"`, `workspace_id = "ws-1"`+"\n"+`credential_provider_id = "provider-1"`, 1))

	cfg, err := loadConfigFromText(t, toml)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if len(cfg.CredentialProviders) != 1 {
		t.Fatalf("CredentialProviders length = %d, want 1", len(cfg.CredentialProviders))
	}
	if cfg.CredentialProviders[0].CanonicalWorkdir == "" {
		t.Fatal("CanonicalWorkdir is empty")
	}
	assertSameFile(t, cfg.CredentialProviders[0].CanonicalWorkdir, fixture.cwd)
}

func newTestFixture(t *testing.T) testFixture {
	t.Helper()
	return testFixture{
		binary: writeTestExecutable(t),
		cwd:    t.TempDir(),
		home:   t.TempDir(),
	}
}

func writeTestExecutable(t *testing.T) string {
	t.Helper()
	name := "codex-test"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return writeTestFile(t, name, 0o755)
}

func writeTestFile(t *testing.T, name string, mode os.FileMode) string {
	t.Helper()
	return writeTestFileInDir(t, t.TempDir(), name, mode)
}

func writeTestFileInDir(t *testing.T, dir string, name string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("test"), mode); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func loadConfigFromText(t *testing.T, text string) (*ValidatedConfig, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gateway.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return LoadFile(path)
}

func assertSameFile(t *testing.T, got string, want string) {
	t.Helper()
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat got path: %v", err)
	}
	wantInfo, err := os.Stat(want)
	if err != nil {
		t.Fatalf("stat want path: %v", err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("paths do not refer to the same file: got %q, want %q", got, want)
	}
}

func validConfigTOML(t *testing.T, fixture testFixture, body string) string {
	t.Helper()
	if body == "" {
		body = defaultSessionTOML(t, fixture)
	}
	return fmt.Sprintf(`codex_binary = %s
listen = "127.0.0.1:0"
child_env_allowlist = ["GATEWAY_SAFE_ENV"]

[client_auth_token_source]
env = %s

%s`, strconv.Quote(fixture.binary), strconv.Quote(testTokenEnv), body)
}

func configTOMLWithChatRuntime(t *testing.T, fixture testFixture, enabled string) string {
	t.Helper()
	return strings.Replace(
		validConfigTOML(t, fixture, ""),
		"child_env_allowlist = [\"GATEWAY_SAFE_ENV\"]\n",
		"child_env_allowlist = [\"GATEWAY_SAFE_ENV\"]\n\n[chat_runtime]\nenabled = "+enabled+"\n",
		1,
	)
}

func defaultSessionTOML(t *testing.T, fixture testFixture) string {
	t.Helper()
	return sessionGroupTOML(t, sessionTOMLOptions{
		id:        "sg-1",
		workspace: "ws-1",
		cwd:       fixture.cwd,
		home:      fixture.home,
	})
}

func sessionGroupTOML(t *testing.T, options sessionTOMLOptions) string {
	t.Helper()
	if options.id == "" {
		options.id = "sg-1"
	}
	return fmt.Sprintf(`[[session_groups]]
session_group_id = %s
workspace_id = %s
cwd = %s
codex_home = %s
%s
[session_groups.runtime_policy]
approval_policy = "on-request"
approvals_reviewer = "user"
sandbox_mode = "workspace-write"
%s
[session_groups.replay_limits]
max_events = 2000
max_bytes = 8388608
ttl_millis = 1800000
%s
[session_groups.thread_binding_limits]
max_bindings = 1000
ttl_millis = 86400000
%s
[session_groups.pending_limits]
max_active_requests = 32
max_display_payload_bytes = 32768
status_non_pending_budget_bytes = 65536
%s
[session_groups.grpc_limits]
inbound_message_bytes = 4194304
outbound_message_bytes = 4194304
%s
`, strconv.Quote(options.id), strconv.Quote(options.workspace), strconv.Quote(options.cwd), strconv.Quote(options.home), options.sessionExtra, options.runtimeExtra, options.replayExtra, options.threadExtra, options.pendingExtra, options.grpcExtra)
}

func sessionGroupWithoutWorkspaceTOML(t *testing.T, fixture testFixture, runtimeExtra string) string {
	t.Helper()
	return fmt.Sprintf(`[[session_groups]]
session_group_id = "sg-1"
cwd = %s
codex_home = %s

[session_groups.runtime_policy]
approval_policy = "on-request"
approvals_reviewer = "user"
sandbox_mode = "workspace-write"
%s
[session_groups.replay_limits]
max_events = 2000
max_bytes = 8388608
ttl_millis = 1800000

[session_groups.thread_binding_limits]
max_bindings = 1000
ttl_millis = 86400000

[session_groups.pending_limits]
max_active_requests = 32
max_display_payload_bytes = 32768
status_non_pending_budget_bytes = 65536

[session_groups.grpc_limits]
inbound_message_bytes = 4194304
outbound_message_bytes = 4194304
`, strconv.Quote(fixture.cwd), strconv.Quote(fixture.home), runtimeExtra)
}

func credentialProviderTOML(t *testing.T, providerID string, executable string, envSources []string) string {
	t.Helper()
	quotedEnvSources := make([]string, 0, len(envSources))
	for _, envSource := range envSources {
		quotedEnvSources = append(quotedEnvSources, strconv.Quote(envSource))
	}
	return fmt.Sprintf(`[[credential_providers]]
provider_id = %s
executable = %s
env_sources = [%s]
`, strconv.Quote(providerID), strconv.Quote(executable), strings.Join(quotedEnvSources, ", "))
}

func duplicateCodexHomeAliasTOML(t *testing.T, fixture testFixture, aliasHome string) string {
	t.Helper()
	return validConfigTOML(t, fixture,
		defaultSessionTOML(t, fixture)+
			sessionGroupTOML(t, sessionTOMLOptions{
				id:        "sg-2",
				workspace: "ws-2",
				cwd:       t.TempDir(),
				home:      aliasHome,
			}))
}
