package appserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Dirard/codex-runtime/gateway/internal/config"
	"github.com/Dirard/codex-runtime/gateway/internal/redact"
)

func TestInvokeCredentialProviderSuccessRegistersSensitiveValues(t *testing.T) {
	registry := redact.NewRegistry()
	provider := credentialProviderForTest(t, "success")

	response, err := InvokeCredentialProvider(context.Background(), provider, CredentialRefreshRequestV1{
		SchemaVersion:  1,
		SessionGroupID: "sg-1",
		Reason:         "unauthorized",
	}, map[string]string{
		"GO_WANT_HELPER_PROCESS": "1",
		"SYSTEMROOT":             os.Getenv("SYSTEMROOT"),
		"PATH":                   os.Getenv("PATH"),
	}, registry)
	if err != nil {
		t.Fatalf("InvokeCredentialProvider() error = %v", err)
	}
	if response.AccessToken == "" || response.ChatGPTAccountID == "" || response.ChatGPTPlanType == nil {
		t.Fatalf("provider response missing required fields: %#v", response)
	}

	output := registry.Redact("echo " + response.AccessToken + " " + response.ChatGPTAccountID + " " + *response.ChatGPTPlanType)
	if output == "echo "+response.AccessToken+" "+response.ChatGPTAccountID+" "+*response.ChatGPTPlanType {
		t.Fatal("credential provider values were not registered for redaction")
	}
}

func TestInvokeCredentialProviderRejectsMissingCanonicalExecutable(t *testing.T) {
	provider := config.CredentialProvider{
		Executable: credentialProviderTestExecutable(t),
	}

	_, err := InvokeCredentialProvider(context.Background(), provider, CredentialRefreshRequestV1{
		SchemaVersion:  1,
		SessionGroupID: "sg-1",
		Reason:         "unauthorized",
	}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "canonical executable") {
		t.Fatalf("InvokeCredentialProvider() error = %v, want missing canonical executable failure", err)
	}
}

func TestCredentialProviderRejectsMalformedResponses(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "invalid json", raw: `{`},
		{name: "unknown schema", raw: `{"schemaVersion":2,"accessToken":"token","chatgptAccountId":"acct","chatgptPlanType":null}`},
		{name: "missing token", raw: `{"schemaVersion":1,"chatgptAccountId":"acct","chatgptPlanType":null}`},
		{name: "missing plan type", raw: `{"schemaVersion":1,"accessToken":"token","chatgptAccountId":"acct"}`},
		{name: "newline token", raw: "{\"schemaVersion\":1,\"accessToken\":\"token\\nvalue\",\"chatgptAccountId\":\"acct\",\"chatgptPlanType\":null}"},
		{name: "trailing data", raw: `{"schemaVersion":1,"accessToken":"token","chatgptAccountId":"acct","chatgptPlanType":null} true`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseCredentialProviderResponse([]byte(tt.raw)); err == nil {
				t.Fatal("parseCredentialProviderResponse() succeeded, want failure")
			}
		})
	}
}

func TestCredentialProviderAcceptsExplicitNullPlanType(t *testing.T) {
	response, err := parseCredentialProviderResponse([]byte(`{"schemaVersion":1,"accessToken":"token","chatgptAccountId":"acct","chatgptPlanType":null}`))
	if err != nil {
		t.Fatalf("parseCredentialProviderResponse() error = %v", err)
	}
	if response.ChatGPTPlanType != nil {
		t.Fatalf("ChatGPTPlanType = %q, want nil", *response.ChatGPTPlanType)
	}
}

func TestProviderEnvUsesPlatformLookup(t *testing.T) {
	env := providerEnv([]string{"SYSTEMROOT"}, map[string]string{
		"SystemRoot": `C:\Windows`,
	})
	if runtime.GOOS == "windows" {
		if len(env) != 1 || env[0] != `SYSTEMROOT=C:\Windows` {
			t.Fatalf("providerEnv() = %#v, want case-insensitive SYSTEMROOT lookup", env)
		}
		return
	}
	if len(env) != 0 {
		t.Fatalf("providerEnv() = %#v, want exact-case lookup on non-Windows", env)
	}
}

func TestCredentialProviderHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" && !credentialProviderHelperRequested() {
		return
	}
	if len(os.Args) == 0 {
		os.Exit(2)
	}
	result := os.Args[len(os.Args)-1]
	if result == "failure" {
		os.Exit(2)
	}
	if result != "success" && result != "env-check" && result != "slow" {
		os.Exit(2)
	}
	var request CredentialRefreshRequestV1
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil || request.SchemaVersion != 1 || request.SessionGroupID == "" {
		os.Exit(2)
	}
	if result == "env-check" && (os.Getenv("PROVIDER_ALLOWED_ENV") != "allowed" || os.Getenv("UNRELATED_PARENT_SECRET") != "") {
		os.Exit(2)
	}
	if result == "slow" {
		startedFile := os.Getenv("PROVIDER_STARTED_FILE")
		if startedFile == "" {
			os.Exit(2)
		}
		if err := os.WriteFile(startedFile, []byte("started"), 0o600); err != nil {
			os.Exit(2)
		}
		time.Sleep(30 * time.Second)
		os.Exit(2)
	}
	_, _ = os.Stdout.WriteString(`{"schemaVersion":1,"accessToken":"provider-access-token","chatgptAccountId":"account-123","chatgptPlanType":"plus"}`)
	os.Exit(0)
}

func credentialProviderHelperRequested() bool {
	for index, arg := range os.Args {
		if arg == "--" && index+1 < len(os.Args) {
			switch os.Args[index+1] {
			case "success", "failure", "env-check", "slow":
				return true
			}
		}
	}
	return false
}

func credentialProviderForTest(t *testing.T, result string) config.CredentialProvider {
	t.Helper()

	executable := credentialProviderTestExecutable(t)
	return config.CredentialProvider{
		Executable:          executable,
		CanonicalExecutable: executable,
		Args:                []string{"-test.run=TestCredentialProviderHelperProcess", "--", result},
		EnvSources:          []string{"GO_WANT_HELPER_PROCESS", "SYSTEMROOT", "PATH"},
		TimeoutMillis:       int64((5 * time.Second) / time.Millisecond),
		StdoutBytes:         16 * 1024,
		StderrBytes:         8 * 1024,
	}
}

func credentialProviderTestExecutable(t *testing.T) string {
	t.Helper()

	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatalf("resolve canonical test executable: %v", err)
	}
	return executable
}
