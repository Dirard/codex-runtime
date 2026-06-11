package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Dirard/codex-runtime/internal/config"
	"github.com/Dirard/codex-runtime/internal/redact"
)

func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_APP_SERVER_PROCESS") == "1" {
		runAppServerProcessHelper()
		return
	}
	os.Exit(m.Run())
}

func TestResolveExecutableIdentityVerifiesCurrentBinary(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := ResolveExecutableIdentity(executable)
	if err != nil {
		t.Fatalf("ResolveExecutableIdentity() error = %v", err)
	}
	if identity.Path == "" {
		t.Fatal("resolved executable path is empty")
	}
	if err := identity.VerifyUnchanged(); err != nil {
		t.Fatalf("VerifyUnchanged() error = %v", err)
	}
}

func TestExecutableIdentityVerifyUnchangedRejectsFileTrustDrift(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file trust bits do not apply on Windows")
	}
	executable := writeTrustedExecutable(t, t.TempDir())
	identity, err := ResolveExecutableIdentity(executable)
	if err != nil {
		t.Fatalf("ResolveExecutableIdentity() error = %v", err)
	}

	if err := os.Chmod(executable, 0o777); err != nil {
		t.Fatalf("chmod executable: %v", err)
	}

	if err := identity.VerifyUnchanged(); err == nil {
		t.Fatal("VerifyUnchanged() succeeded after executable became group/world writable")
	}
}

func TestExecutableIdentityVerifyUnchangedRejectsExecutableBitDrift(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable bits do not apply on Windows")
	}
	executable := writeTrustedExecutable(t, t.TempDir())
	identity, err := ResolveExecutableIdentity(executable)
	if err != nil {
		t.Fatalf("ResolveExecutableIdentity() error = %v", err)
	}

	if err := os.Chmod(executable, 0o644); err != nil {
		t.Fatalf("chmod executable: %v", err)
	}

	if err := identity.VerifyUnchanged(); err == nil {
		t.Fatal("VerifyUnchanged() succeeded after executable bit was removed")
	}
}

func TestExecutableIdentityVerifyUnchangedRejectsParentTrustDrift(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory trust bits do not apply on Windows")
	}
	parent := t.TempDir()
	executable := writeTrustedExecutable(t, parent)
	identity, err := ResolveExecutableIdentity(executable)
	if err != nil {
		t.Fatalf("ResolveExecutableIdentity() error = %v", err)
	}

	if err := os.Chmod(parent, 0o777); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(parent, 0o700)
	})

	if err := identity.VerifyUnchanged(); err == nil {
		t.Fatal("VerifyUnchanged() succeeded after parent became group/world writable")
	}
}

func TestExecutableIdentityVerifyUnchangedRejectsReplaceAtSamePath(t *testing.T) {
	dir := t.TempDir()
	executable := writeTrustedExecutable(t, dir)
	identity, err := ResolveExecutableIdentity(executable)
	if err != nil {
		t.Fatalf("ResolveExecutableIdentity() error = %v", err)
	}

	replacement := writeTrustedExecutableNamed(t, dir, "replacement"+executableExtension())
	if err := os.Chtimes(replacement, identity.info.ModTime(), identity.info.ModTime()); err != nil {
		t.Fatalf("chtimes replacement: %v", err)
	}
	if err := os.Remove(executable); err != nil {
		t.Fatalf("remove executable: %v", err)
	}
	if err := os.Rename(replacement, executable); err != nil {
		t.Skipf("replace-at-same-path is not supported: %v", err)
	}
	currentInfo, err := os.Stat(executable)
	if err != nil {
		t.Fatalf("stat replacement: %v", err)
	}
	if os.SameFile(identity.info, currentInfo) {
		t.Skip("replace-at-same-path did not change file identity on this filesystem")
	}

	if err := identity.VerifyUnchanged(); err == nil {
		t.Fatal("VerifyUnchanged() succeeded after executable was replaced at the same path")
	}
}

func TestStartProcessConnectionKeepsChildAfterStartupContextCancel(t *testing.T) {
	cfg, session, identity := processTestConfig(t)
	parentEnv := envMapFromOS()
	parentEnv["GO_WANT_APP_SERVER_PROCESS"] = "1"
	stderrChunks := make(chan string, 8)

	ctx, cancel := context.WithCancel(context.Background())
	connection, err := StartProcessConnection(ctx, cfg, session, identity, ProcessOptions{
		ParentEnv: parentEnv,
		StderrSink: func(chunk string) {
			stderrChunks <- chunk
		},
	})
	if err != nil {
		t.Fatalf("StartProcessConnection() error = %v", err)
	}
	defer connection.Close()

	cancel()
	for _, message := range []string{"after-cancel-one", "after-cancel-two"} {
		if err := connection.Notify("stderr/ping", map[string]string{"message": message}); err != nil {
			t.Fatalf("Notify(%s) after startup context cancel error = %v", message, err)
		}
		waitForStderrMessage(t, stderrChunks, message)
	}
}

func TestNewProcessSupervisorWiresCredentialProvider(t *testing.T) {
	cfg, session, _ := processTestConfigWithProvider(t)
	parentEnv := envMapFromOS()
	parentEnv["GO_WANT_APP_SERVER_PROCESS"] = "1"
	stderrChunks := make(chan string, 8)

	supervisor, _, err := NewProcessSupervisor(context.Background(), cfg, session, ProcessOptions{
		ParentEnv: parentEnv,
		StderrSink: func(chunk string) {
			stderrChunks <- chunk
		},
	})
	if err != nil {
		t.Fatalf("NewProcessSupervisor() error = %v", err)
	}
	connection, err := supervisor.Connection(context.Background())
	if err != nil {
		t.Fatalf("Connection() error = %v", err)
	}
	defer connection.Close()

	if err := connection.Notify("auth-refresh/request", nil); err != nil {
		t.Fatalf("Notify(auth-refresh/request) error = %v", err)
	}
	waitForStderrMessage(t, stderrChunks, "auth-refresh-provider-ok")
}

func TestStartProcessConnectionStoresOnlyCredentialProviderEnvSources(t *testing.T) {
	cfg, session, identity := processTestConfigWithProvider(t)
	cfg.CredentialProviders[0].Args = []string{"-test.run=TestCredentialProviderHelperProcess", "--", "env-check"}
	cfg.CredentialProviders[0].EnvSources = []string{"PROVIDER_ALLOWED_ENV"}
	parentEnv := envMapFromOS()
	parentEnv["GO_WANT_APP_SERVER_PROCESS"] = "1"
	parentEnv["PROVIDER_ALLOWED_ENV"] = "allowed"
	parentEnv["UNRELATED_PARENT_SECRET"] = "must-not-reach-provider"
	stderrChunks := make(chan string, 8)

	connection, err := StartProcessConnection(context.Background(), cfg, session, identity, ProcessOptions{
		ParentEnv: parentEnv,
		StderrSink: func(chunk string) {
			stderrChunks <- chunk
		},
	})
	if err != nil {
		t.Fatalf("StartProcessConnection() error = %v", err)
	}
	defer connection.Close()

	if len(connection.providerEnv) != 1 || connection.providerEnv["PROVIDER_ALLOWED_ENV"] != "allowed" {
		t.Fatalf("provider env stored in connection = %#v, want only PROVIDER_ALLOWED_ENV", connection.providerEnv)
	}
	if err := connection.Notify("auth-refresh/request", nil); err != nil {
		t.Fatalf("Notify(auth-refresh/request) error = %v", err)
	}
	waitForStderrMessage(t, stderrChunks, "auth-refresh-provider-ok")
}

func TestDrainStderrRedactsAndBoundsOutput(t *testing.T) {
	registry := redact.NewRegistry()
	registry.Add("stderr-secret-value")
	var chunks []string

	DrainStderr(context.Background(), strings.NewReader("first stderr-secret-value second"), redact.New(redact.WithConnectionRegistry(registry)), 1024, func(chunk string) {
		chunks = append(chunks, chunk)
	})

	output := strings.Join(chunks, "")
	if strings.Contains(output, "stderr-secret-value") {
		t.Fatalf("stderr drain leaked secret: %q", output)
	}
	if !strings.Contains(output, redact.SensitiveValueMarker) {
		t.Fatalf("stderr drain missing redaction marker: %q", output)
	}
}

func TestDrainStderrRedactsBeforeOutputCap(t *testing.T) {
	sensitive := "stderr-secret-value"
	registry := redact.NewRegistry()
	registry.Add(sensitive)
	var chunks []string
	maxBytes := len("prefix stderr")

	DrainStderr(context.Background(), strings.NewReader("prefix "+sensitive+" suffix"), redact.New(redact.WithConnectionRegistry(registry)), maxBytes, func(chunk string) {
		chunks = append(chunks, chunk)
	})

	output := strings.Join(chunks, "")
	if len(output) > maxBytes {
		t.Fatalf("stderr output length = %d, want <= %d", len(output), maxBytes)
	}
	for _, leaked := range []string{sensitive, "stderr", "secret", "value"} {
		if strings.Contains(output, leaked) {
			t.Fatalf("stderr drain leaked sensitive fragment %q in capped output: %q", leaked, output)
		}
	}
}

func TestFirstLineNormalizesVersionOutput(t *testing.T) {
	for name, input := range map[string]string{
		"lf":              "codex 0.137.0\n",
		"crlf":            "codex 0.137.0\r\n",
		"trailing spaces": "codex 0.137.0 \t\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := firstLine(input); got != "codex 0.137.0" {
				t.Fatalf("firstLine() = %q, want %q", got, "codex 0.137.0")
			}
		})
	}
}

func processTestConfig(t *testing.T) (*config.ValidatedConfig, config.SessionGroup, ExecutableIdentity) {
	return processTestConfigWithProviderEnabled(t, false)
}

func processTestConfigWithProvider(t *testing.T) (*config.ValidatedConfig, config.SessionGroup, ExecutableIdentity) {
	return processTestConfigWithProviderEnabled(t, true)
}

func processTestConfigWithProviderEnabled(t *testing.T, includeProvider bool) (*config.ValidatedConfig, config.SessionGroup, ExecutableIdentity) {
	t.Helper()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tokenFile := t.TempDir() + string(os.PathSeparator) + "token.txt"
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	codexHome := t.TempDir()
	raw := config.Config{
		CodexBinary: executable,
		Listen:      config.DefaultListenAddress,
		ClientAuthTokenSource: config.TokenSource{
			File: tokenFile,
		},
		ChildEnvAllowlist: []string{"GO_WANT_APP_SERVER_PROCESS"},
		SessionGroups: []config.SessionGroup{
			{
				SessionGroupID: "sg-1",
				WorkspaceID:    "ws-1",
				CWD:            cwd,
				CodexHome:      codexHome,
				RuntimePolicy: config.RuntimePolicy{
					ApprovalPolicy: config.ApprovalPolicyOnRequest,
					SandboxMode:    config.SandboxReadOnly,
				},
			},
		},
	}
	if includeProvider {
		raw.CredentialProviders = []config.CredentialProvider{
			{
				ProviderID: "provider-1",
				Executable: executable,
				Args:       []string{"-test.run=TestCredentialProviderHelperProcess", "--", "success"},
			},
		}
		raw.SessionGroups[0].CredentialProviderID = "provider-1"
	}
	cfg, err := raw.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	identity, err := ResolveExecutableIdentity(cfg.CodexBinary)
	if err != nil {
		t.Fatalf("ResolveExecutableIdentity() error = %v", err)
	}
	return cfg, cfg.SessionGroups[0], identity
}

func writeTrustedExecutable(t *testing.T, dir string) string {
	t.Helper()
	return writeTrustedExecutableNamed(t, dir, "codex-test"+executableExtension())
}

func writeTrustedExecutableNamed(t *testing.T, dir string, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("test"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatalf("chmod executable: %v", err)
		}
	}
	return path
}

func executableExtension() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func waitForStderrMessage(t *testing.T, chunks <-chan string, message string) {
	t.Helper()

	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	var output strings.Builder
	for {
		select {
		case chunk := <-chunks:
			output.WriteString(chunk)
			if strings.Contains(output.String(), message) {
				return
			}
		case <-timer.C:
			t.Fatalf("stderr did not contain %q; got %q", message, output.String())
		}
	}
}

func runAppServerProcessHelper() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var message rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			os.Exit(2)
		}
		if len(message.ID) > 0 && message.Method == "" {
			if string(message.ID) != "41" || message.Error != nil {
				os.Exit(2)
			}
			var result struct {
				AccessToken      string  `json:"accessToken"`
				ChatGPTAccountID string  `json:"chatgptAccountId"`
				ChatGPTPlanType  *string `json:"chatgptPlanType"`
			}
			if err := json.Unmarshal(message.Result, &result); err != nil ||
				result.AccessToken != "provider-access-token" ||
				result.ChatGPTAccountID != "account-123" ||
				result.ChatGPTPlanType == nil ||
				*result.ChatGPTPlanType != "plus" {
				os.Exit(2)
			}
			if _, err := os.Stderr.WriteString("auth-refresh-provider-ok " + strings.Repeat("x", 64) + "\n"); err != nil {
				os.Exit(2)
			}
			continue
		}
		switch message.Method {
		case "initialize":
			result, err := marshalValue(map[string]any{"codexHome": os.Getenv("CODEX_HOME")})
			if err != nil {
				os.Exit(2)
			}
			response, err := encodeRPCJSONL(rpcMessage{
				JSONRPC: jsonrpcVersion,
				ID:      cloneRaw(message.ID),
				Result:  result,
			})
			if err != nil {
				os.Exit(2)
			}
			if _, err := os.Stdout.Write(response); err != nil {
				os.Exit(2)
			}
		case "initialized":
		case "auth-refresh/request":
			params, err := marshalValue(map[string]any{
				"reason":            "unauthorized",
				"previousAccountId": "previous-account",
			})
			if err != nil {
				os.Exit(2)
			}
			request, err := encodeRPCJSONL(rpcMessage{
				JSONRPC: jsonrpcVersion,
				ID:      json.RawMessage("41"),
				Method:  "account/chatgptAuthTokens/refresh",
				Params:  params,
			})
			if err != nil {
				os.Exit(2)
			}
			if _, err := os.Stdout.Write(request); err != nil {
				os.Exit(2)
			}
		case "stderr/ping":
			var params struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(message.Params, &params); err != nil {
				os.Exit(2)
			}
			if _, err := os.Stderr.WriteString(params.Message + "\n"); err != nil {
				os.Exit(2)
			}
		}
	}
	os.Exit(0)
}

func TestProbeEnvExcludesCodexHomeAndSecretSources(t *testing.T) {
	env := probeEnv(map[string]string{
		"PATH":         "/trusted/bin",
		"TMPDIR":       "/tmp",
		"CODEX_HOME":   "/secret/codex-home",
		"SECRET_TOKEN": "secret-token-value",
		"API_KEY":      "secret-api-key",
	})

	if env["PATH"] != "/trusted/bin" {
		t.Fatalf("probe env PATH = %q, want trusted PATH", env["PATH"])
	}
	for _, name := range []string{"CODEX_HOME", "SECRET_TOKEN", "API_KEY"} {
		if _, ok := env[name]; ok {
			t.Fatalf("probe env included %s", name)
		}
	}
}
