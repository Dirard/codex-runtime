package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	codex "github.com/Dirard/codex-runtime/sdk/go"
	"google.golang.org/grpc/codes"
)

func TestValidateLocalGatewayAddrAcceptsLoopback(t *testing.T) {
	for _, target := range []string{
		"127.0.0.1:5575",
		"localhost:5575",
		"[::1]:5575",
		"dns:///localhost:5575",
		"passthrough:///127.0.0.1:5575",
	} {
		t.Run(target, func(t *testing.T) {
			if err := validateLocalGatewayAddr(target); err != nil {
				t.Fatalf("validateLocalGatewayAddr returned error: %v", err)
			}
		})
	}
}

func TestValidateLocalGatewayAddrRejectsRemote(t *testing.T) {
	for _, target := range []string{
		"192.0.2.10:5575",
		"example.com:5575",
		"dns:///example.com:5575",
	} {
		t.Run(target, func(t *testing.T) {
			if err := validateLocalGatewayAddr(target); err == nil {
				t.Fatal("validateLocalGatewayAddr returned nil")
			}
		})
	}
}

func TestServerConfigRequiresTokenSource(t *testing.T) {
	cfg := serverConfig{
		gatewayAddr:    "127.0.0.1:5575",
		sessionGroupID: "workflow-smoke-session",
		workspaceID:    "workflow-smoke-workspace",
		namespace:      "examples",
		workflowID:     "writer-notes",
		workflowDir:    ".\\examples\\workflows\\writer-notes",
	}
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "CODEX_RUNTIME_TOKEN_SOURCE") {
		t.Fatalf("serverConfig.validate() error = %v, want token-source error", err)
	}
}

func TestReadTokenSourceTrimsFileContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.token")
	if err := os.WriteFile(path, []byte("local-test-token\n"), 0o600); err != nil {
		t.Fatalf("write token source: %v", err)
	}
	token, err := readTokenSource(path)
	if err != nil {
		t.Fatalf("readTokenSource() error = %v", err)
	}
	if token != "local-test-token" {
		t.Fatalf("token = %q, want trimmed token", token)
	}
}

func TestSafeHTTPErrorIncludesTypedWorkflowNextAction(t *testing.T) {
	err := &codex.Error{
		Code:         codes.FailedPrecondition,
		WorkflowCode: codex.WorkflowErrorRestartRequired,
		Reason:       "restart_required",
		NextAction:   "restart the workflow",
	}
	if status := safeHTTPStatus(err); status != 409 {
		t.Fatalf("safeHTTPStatus() = %d, want 409", status)
	}
	message := safeError(err)
	for _, want := range []string{"workflow_code=RestartRequired", "reason=restart_required", "next_action=restart the workflow"} {
		if !strings.Contains(message, want) {
			t.Fatalf("safeError() = %q, want %q", message, want)
		}
	}
}
