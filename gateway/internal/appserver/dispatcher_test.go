package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dirard/codex-runtime/gateway/internal/config"
	"github.com/Dirard/codex-runtime/gateway/internal/contextpack"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	"github.com/Dirard/codex-runtime/gateway/internal/redact"
	"github.com/Dirard/codex-runtime/gateway/internal/testappserver"
)

func TestConnectionInitializeHandshake(t *testing.T) {
	codexHome := t.TempDir()
	harness := testappserver.New(t, testappserver.Initialize(codexHome)...)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), config.SessionGroup{
		SessionGroupID:     "sg-1",
		CanonicalCodexHome: codexHome,
	}, ConnectionOptions{})
	defer connection.Close()

	if err := connection.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	harness.RequireDone(t)
	requireNoJSONRPCWireField(t, harness.RequireOutboundRequest(t, 0, testappserver.MethodInitialize).Raw)
	requireNoJSONRPCWireField(t, harness.RequireOutboundNotification(t, 1, testappserver.MethodInitialized).Raw)
}

func TestConnectionInitializeRejectsCodexHomeMismatch(t *testing.T) {
	harness := testappserver.New(t,
		testappserver.ExpectRequest(testappserver.MethodInitialize, testappserver.CaptureID(testappserver.MethodInitialize)),
		testappserver.SendResponseFor(testappserver.MethodInitialize, map[string]any{"codexHome": t.TempDir()}),
	)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), config.SessionGroup{
		SessionGroupID:     "sg-1",
		CanonicalCodexHome: t.TempDir(),
	}, ConnectionOptions{})
	defer connection.Close()

	err := connection.Initialize(context.Background())
	var gatewayErr *domain.GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Details.Reason != domain.ReasonCodexHomeMismatch {
		t.Fatalf("Initialize() error = %#v, want codex_home_mismatch", err)
	}
}

func TestConnectionInitializePreservesCallerCancellation(t *testing.T) {
	harness := testappserver.New(t)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), config.SessionGroup{
		SessionGroupID:     "sg-1",
		CanonicalCodexHome: t.TempDir(),
	}, ConnectionOptions{})
	defer connection.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := connection.Initialize(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Initialize() error = %v, want context canceled", err)
	}
	harness.RequireOutboundCount(t, 0)
}

func TestConnectionStartThreadWritesVerifiedPermissionsProfilePayload(t *testing.T) {
	session := config.SessionGroup{
		SessionGroupID: "sg-1",
		CanonicalCWD:   `/work/project`,
		RuntimePolicy: config.RuntimePolicy{
			ApprovalPolicy:       config.ApprovalPolicyUntrusted,
			PermissionsProfileID: "trusted-profile",
		},
	}
	params, err := NewThreadStartParams(session)
	if err != nil {
		t.Fatalf("NewThreadStartParams() error = %v", err)
	}
	harness := testappserver.New(t,
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart), testappserver.WithParams(params)),
		testappserver.SendResponseFor(testappserver.MethodThreadStart, testappserver.ThreadResult("thread-1")),
	)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), session, ConnectionOptions{
		SchemaPolicy: mustValidSchemaPolicy(t),
	})
	defer connection.Close()

	if _, err := connection.StartThread(context.Background(), ThreadStartCall{TaskID: "task-1", Timeout: time.Second}); err != nil {
		t.Fatalf("StartThread() error = %v", err)
	}
	harness.RequireDone(t)
}

func TestConnectionStartThreadAllowsSandboxWhenPermissionsFieldMissing(t *testing.T) {
	session := config.SessionGroup{
		SessionGroupID: "sg-1",
		CanonicalCWD:   `/work/project`,
		RuntimePolicy: config.RuntimePolicy{
			ApprovalPolicy: config.ApprovalPolicyOnRequest,
			SandboxMode:    config.SandboxWorkspaceWrite,
		},
	}
	params, err := NewThreadStartParams(session)
	if err != nil {
		t.Fatalf("NewThreadStartParams() error = %v", err)
	}
	policy := mustSchemaPolicyWith(t, func(metadata *SchemaMetadata) {
		metadata.ThreadStartPermissions = false
	})
	harness := testappserver.New(t,
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart), testappserver.WithParams(params)),
		testappserver.SendResponseFor(testappserver.MethodThreadStart, testappserver.ThreadResult("thread-1")),
	)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), session, ConnectionOptions{
		SchemaPolicy: policy,
	})
	defer connection.Close()

	if _, err := connection.StartThread(context.Background(), ThreadStartCall{TaskID: "task-1", Timeout: time.Second}); err != nil {
		t.Fatalf("StartThread() error = %v", err)
	}
	harness.RequireDone(t)
}

func TestConnectionStartThreadBlocksPermissionsProfileWhenSchemaUnverified(t *testing.T) {
	session := config.SessionGroup{
		SessionGroupID: "sg-1",
		CanonicalCWD:   `/work/project`,
		RuntimePolicy: config.RuntimePolicy{
			ApprovalPolicy:       config.ApprovalPolicyUntrusted,
			PermissionsProfileID: "trusted-profile",
		},
	}
	policy := mustSchemaPolicyWith(t, func(metadata *SchemaMetadata) {
		metadata.ThreadStartPermissions = false
	})
	harness := testappserver.New(t)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), session, ConnectionOptions{
		SchemaPolicy: policy,
	})
	defer connection.Close()

	_, err := connection.StartThread(context.Background(), ThreadStartCall{TaskID: "task-1", Timeout: time.Second})
	requireSchemaUnverified(t, err, "task-1", "sg-1", "")
	harness.RequireOutboundCount(t, 0)
}

func TestConnectionResumeThreadWritesExactPayloadWhenSchemaVerified(t *testing.T) {
	session := config.SessionGroup{SessionGroupID: "sg-1"}
	harness := testappserver.New(t,
		testappserver.ExpectRequest(testappserver.MethodThreadResume, testappserver.CaptureID(testappserver.MethodThreadResume), testappserver.WithParams(NewThreadResumeParams("thread-1"))),
		testappserver.SendResponseFor(testappserver.MethodThreadResume, testappserver.ThreadResumeResult("thread-1")),
	)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), session, ConnectionOptions{
		SchemaPolicy: mustValidSchemaPolicy(t),
	})
	defer connection.Close()

	if _, err := connection.ResumeThread(context.Background(), ThreadResumeCall{ThreadID: "thread-1", TaskID: "task-1", Timeout: time.Second}); err != nil {
		t.Fatalf("ResumeThread() error = %v", err)
	}
	harness.RequireDone(t)
}

func TestConnectionResumeThreadBlocksWhenSchemaUnverified(t *testing.T) {
	session := config.SessionGroup{SessionGroupID: "sg-1"}
	policy := mustSchemaPolicyWith(t, func(metadata *SchemaMetadata) {
		metadata.ThreadResumeInitialTurnsPage = false
	})
	harness := testappserver.New(t)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), session, ConnectionOptions{
		SchemaPolicy: policy,
	})
	defer connection.Close()

	_, err := connection.ResumeThread(context.Background(), ThreadResumeCall{ThreadID: "thread-1", TaskID: "task-1", Timeout: time.Second})
	requireSchemaUnverified(t, err, "task-1", "sg-1", "thread-1")
	harness.RequireOutboundCount(t, 0)
}

func TestDispatcherParsesFragmentedCRLFResponse(t *testing.T) {
	codexHome := t.TempDir()
	harness := testappserver.New(t,
		testappserver.ExpectRequest(testappserver.MethodInitialize, testappserver.CaptureID(testappserver.MethodInitialize)),
		testappserver.SendResponseFor(testappserver.MethodInitialize, map[string]any{"codexHome": codexHome}, testappserver.CRLF(), testappserver.Fragmented(3, 2, 7)),
		testappserver.ExpectNotification(testappserver.MethodInitialized, testappserver.WithoutParams()),
	)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), config.SessionGroup{
		SessionGroupID:     "sg-1",
		CanonicalCodexHome: codexHome,
	}, ConnectionOptions{})
	defer connection.Close()

	if err := connection.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	harness.RequireDone(t)
}

func TestDispatcherTurnStartUsesUserInputTextShape(t *testing.T) {
	harness := testappserver.New(t, testappserver.TurnStart("thread-1", "turn-1")...)
	dispatcher := NewDispatcher(harness.Stdin(), harness.Stdout(), 0)
	defer dispatcher.Close()

	envelope, err := contextpack.BuildEnvelope("Do it", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dispatcher.Call(context.Background(), testappserver.MethodTurnStart, NewTurnStartParams("thread-1", "client-1", []UserInputText{
		NewUserInputText(envelope),
	}), time.Second, CallMetadata{ExpectedThreadID: "thread-1"})
	if err != nil {
		t.Fatalf("Call(turn/start) error = %v", err)
	}
	harness.RequireDone(t)
}

func TestDispatcherCallErrorDoesNotLeakAppServerMessage(t *testing.T) {
	const sensitiveMessage = "api_key=abcdefghijklmnopqrstuvwxyz"
	harness := testappserver.New(t,
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart)),
		testappserver.SendErrorResponseFor(testappserver.MethodThreadStart, -32001, sensitiveMessage),
	)
	dispatcher := NewDispatcher(harness.Stdin(), harness.Stdout(), 0)
	defer dispatcher.Close()

	_, err := dispatcher.Call(context.Background(), testappserver.MethodThreadStart, nil, time.Second, CallMetadata{})
	if err == nil {
		t.Fatal("Call(thread/start) error = nil, want app-server error")
	}
	errorText := err.Error()
	if strings.Contains(errorText, sensitiveMessage) {
		t.Fatalf("Call(thread/start) error leaked app-server message: %q", errorText)
	}
	if !strings.Contains(errorText, testappserver.MethodThreadStart) || !strings.Contains(errorText, "-32001") {
		t.Fatalf("Call(thread/start) error = %q, want method and code", errorText)
	}
	harness.RequireDone(t)
}

func TestAuthRefreshWithoutProviderRespondsError(t *testing.T) {
	failures := make(chan AuthRefreshFailure, 1)
	harness := testappserver.New(t,
		testappserver.SendRequest(41, "account/chatgptAuthTokens/refresh", map[string]any{"reason": "unauthorized"}),
		testappserver.ExpectErrorResponseID(41, authRefreshUnavailableCode, "auth_refresh_unavailable"),
	)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), config.SessionGroup{SessionGroupID: "sg-1"}, ConnectionOptions{
		ActiveTaskID: func() string {
			return "task-1"
		},
		OnAuthRefreshFailure: func(failure AuthRefreshFailure) {
			failures <- failure
		},
	})
	defer connection.Close()

	harness.RequireDone(t)
	harness.RequireOutboundCount(t, 1)
	requireAuthRefreshFailure(t, failures, AuthRefreshFailure{
		SessionGroupID: "sg-1",
		TaskID:         "task-1",
		Reason:         domain.ReasonAuthRefreshUnavailable,
	})
}

func TestAuthRefreshWithProviderRespondsAndRegistersSensitiveValues(t *testing.T) {
	registry := redact.NewRegistry()
	provider := credentialProviderForTest(t, "success")
	harness := testappserver.New(t,
		testappserver.SendRequest(41, "account/chatgptAuthTokens/refresh", map[string]any{"reason": "unauthorized"}),
		testappserver.ExpectResponseID(41, testappserver.WithResult(map[string]any{
			"accessToken":      "provider-access-token",
			"chatgptAccountId": "account-123",
			"chatgptPlanType":  "plus",
		})),
	)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), config.SessionGroup{SessionGroupID: "sg-1"}, ConnectionOptions{
		CredentialProvider: &provider,
		ProviderEnv: map[string]string{
			"GO_WANT_HELPER_PROCESS": "1",
			"SYSTEMROOT":             os.Getenv("SYSTEMROOT"),
			"PATH":                   os.Getenv("PATH"),
		},
		SensitiveRegistry: registry,
	})
	defer connection.Close()

	harness.RequireDone(t)
	if output := registry.Redact("provider-access-token account-123 plus"); output == "provider-access-token account-123 plus" {
		t.Fatal("credential provider response values were not registered before app-server response")
	}
}

func TestAuthRefreshMalformedParamsDoesNotInvokeProvider(t *testing.T) {
	failures := make(chan AuthRefreshFailure, 1)
	provider := credentialProviderForTest(t, "success")
	harness := testappserver.New(t,
		testappserver.SendRequest(41, "account/chatgptAuthTokens/refresh", map[string]any{"previousAccountId": "account-123"}),
		testappserver.ExpectErrorResponseID(41, authRefreshUnavailableCode, "auth_refresh_unavailable"),
	)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), config.SessionGroup{SessionGroupID: "sg-1"}, ConnectionOptions{
		CredentialProvider: &provider,
		ProviderEnv: map[string]string{
			"GO_WANT_HELPER_PROCESS": "1",
			"SYSTEMROOT":             os.Getenv("SYSTEMROOT"),
			"PATH":                   os.Getenv("PATH"),
		},
		OnAuthRefreshFailure: func(failure AuthRefreshFailure) {
			failures <- failure
		},
	})
	defer connection.Close()

	harness.RequireDone(t)
	harness.RequireOutboundCount(t, 1)
	requireAuthRefreshFailure(t, failures, AuthRefreshFailure{
		SessionGroupID: "sg-1",
		Reason:         domain.ReasonAuthRefreshUnavailable,
	})
}

func TestAuthRefreshProviderFailureRunsFailureHook(t *testing.T) {
	failures := make(chan AuthRefreshFailure, 1)
	provider := credentialProviderForTest(t, "failure")
	harness := testappserver.New(t,
		testappserver.SendRequest(41, "account/chatgptAuthTokens/refresh", map[string]any{"reason": "unauthorized"}),
		testappserver.ExpectErrorResponseID(41, authRefreshUnavailableCode, "auth_refresh_unavailable"),
	)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), config.SessionGroup{SessionGroupID: "sg-1"}, ConnectionOptions{
		CredentialProvider: &provider,
		ProviderEnv: map[string]string{
			"GO_WANT_HELPER_PROCESS": "1",
			"SYSTEMROOT":             os.Getenv("SYSTEMROOT"),
			"PATH":                   os.Getenv("PATH"),
		},
		ActiveTaskID: func() string {
			return "task-provider"
		},
		OnAuthRefreshFailure: func(failure AuthRefreshFailure) {
			failures <- failure
		},
	})
	defer connection.Close()

	harness.RequireDone(t)
	harness.RequireOutboundCount(t, 1)
	requireAuthRefreshFailure(t, failures, AuthRefreshFailure{
		SessionGroupID: "sg-1",
		TaskID:         "task-provider",
		Reason:         domain.ReasonAuthRefreshUnavailable,
	})
}

func TestAuthRefreshProviderCancelsOnConnectionClose(t *testing.T) {
	startedFile := filepath.Join(t.TempDir(), "provider-started")
	provider := credentialProviderForTest(t, "slow")
	provider.EnvSources = append(provider.EnvSources, "PROVIDER_STARTED_FILE")
	provider.TimeoutMillis = int64((2 * time.Second) / time.Millisecond)
	stdin := newBlockingWriteCloser()
	stdout, stdoutWriter := io.Pipe()
	defer stdoutWriter.Close()
	connection := NewConnection(stdin, stdout, config.SessionGroup{SessionGroupID: "sg-1"}, ConnectionOptions{
		CredentialProvider: &provider,
		ProviderEnv: map[string]string{
			"GO_WANT_HELPER_PROCESS": "1",
			"PROVIDER_STARTED_FILE":  startedFile,
			"SYSTEMROOT":             os.Getenv("SYSTEMROOT"),
			"PATH":                   os.Getenv("PATH"),
		},
	})
	defer connection.Close()

	done := make(chan struct{})
	go func() {
		connection.handleAuthRefresh(ServerRequest{
			ID:     json.RawMessage("41"),
			Method: "account/chatgptAuthTokens/refresh",
			Params: json.RawMessage(`{"reason":"unauthorized"}`),
		})
		close(done)
	}()
	waitForFile(t, startedFile)

	started := time.Now()
	if err := connection.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-done:
		if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
			t.Fatalf("auth refresh returned after %s, want prompt provider cancellation", elapsed)
		}
	case <-time.After(750 * time.Millisecond):
		t.Errorf("auth refresh did not return promptly after connection close")
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("auth refresh did not return after provider timeout")
		}
	}
}

func TestUnsupportedServerRequestRespondsErrorOnce(t *testing.T) {
	harness := testappserver.New(t,
		testappserver.SendRequest(42, "attestation/generate", map[string]any{"challenge": "ignored"}),
		testappserver.ExpectErrorResponseID(42, unsupportedServerRequestCode, "unsupported_server_request"),
	)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), config.SessionGroup{SessionGroupID: "sg-1"}, ConnectionOptions{})
	defer connection.Close()

	harness.RequireDone(t)
}

func TestUnsupportedServerRequestHookUsesRequestTaskID(t *testing.T) {
	requests := make(chan UnsupportedServerRequest, 1)
	harness := testappserver.New(t,
		testappserver.SendRequest(42, "attestation/generate", map[string]any{
			"taskId": "task-from-request",
			"token":  "unsafe-value",
		}),
		testappserver.ExpectErrorResponseID(42, unsupportedServerRequestCode, "unsupported_server_request"),
	)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), config.SessionGroup{SessionGroupID: "sg-1"}, ConnectionOptions{
		ActiveTaskID: func() string {
			return "fallback-task"
		},
		OnUnsupportedRequest: func(request UnsupportedServerRequest) {
			requests <- request
		},
	})
	defer connection.Close()

	harness.RequireDone(t)
	var got UnsupportedServerRequest
	select {
	case got = <-requests:
	case <-time.After(time.Second):
		t.Fatal("unsupported server request hook was not called")
	}
	want := UnsupportedServerRequest{
		SessionGroupID: "sg-1",
		TaskID:         "task-from-request",
		Method:         "attestation/generate",
	}
	if got != want {
		t.Fatalf("unsupported server request = %#v, want %#v", got, want)
	}
	if strings.Contains(fmt.Sprintf("%#v", got), "unsafe-value") {
		t.Fatalf("unsupported server request hook carried raw params: %#v", got)
	}
}

func TestUnsupportedServerRequestHookCarriesExplicitTurnIDs(t *testing.T) {
	requests := make(chan UnsupportedServerRequest, 1)
	harness := testappserver.New(t,
		testappserver.SendRequest(42, "attestation/generate", map[string]any{
			"threadId":  "thread-1",
			"turnId":    "turn-1",
			"challenge": "unsafe-value",
		}),
		testappserver.ExpectErrorResponseID(42, unsupportedServerRequestCode, "unsupported_server_request"),
	)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), config.SessionGroup{SessionGroupID: "sg-1"}, ConnectionOptions{
		OnUnsupportedRequest: func(request UnsupportedServerRequest) {
			requests <- request
		},
	})
	defer connection.Close()

	harness.RequireDone(t)
	var got UnsupportedServerRequest
	select {
	case got = <-requests:
	case <-time.After(time.Second):
		t.Fatal("unsupported server request hook was not called")
	}
	want := UnsupportedServerRequest{
		SessionGroupID: "sg-1",
		ThreadID:       "thread-1",
		TurnID:         "turn-1",
		Method:         "attestation/generate",
	}
	if got != want {
		t.Fatalf("unsupported server request = %#v, want %#v", got, want)
	}
	if strings.Contains(fmt.Sprintf("%#v", got), "unsafe-value") {
		t.Fatalf("unsupported server request hook carried raw params: %#v", got)
	}
}

func TestUnsupportedServerRequestHookDoesNotUseActiveTaskFallback(t *testing.T) {
	requests := make(chan UnsupportedServerRequest, 1)
	harness := testappserver.New(t,
		testappserver.SendRequest(42, "attestation/generate", map[string]any{
			"challenge": "unsafe-value",
		}),
		testappserver.ExpectErrorResponseID(42, unsupportedServerRequestCode, "unsupported_server_request"),
	)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), config.SessionGroup{SessionGroupID: "sg-1"}, ConnectionOptions{
		ActiveTaskID: func() string {
			return "fallback-task"
		},
		OnUnsupportedRequest: func(request UnsupportedServerRequest) {
			requests <- request
		},
	})
	defer connection.Close()

	harness.RequireDone(t)
	var got UnsupportedServerRequest
	select {
	case got = <-requests:
	case <-time.After(time.Second):
		t.Fatal("unsupported server request hook was not called")
	}
	want := UnsupportedServerRequest{
		SessionGroupID: "sg-1",
		Method:         "attestation/generate",
	}
	if got != want {
		t.Fatalf("unsupported server request = %#v, want %#v", got, want)
	}
	if strings.Contains(fmt.Sprintf("%#v", got), "unsafe-value") {
		t.Fatalf("unsupported server request hook carried raw params: %#v", got)
	}
}

func requireAuthRefreshFailure(t *testing.T, failures <-chan AuthRefreshFailure, want AuthRefreshFailure) {
	t.Helper()

	var got AuthRefreshFailure
	select {
	case got = <-failures:
	case <-time.After(time.Second):
		t.Fatal("auth refresh failure hook was not called")
	}
	if got != want {
		t.Fatalf("auth refresh failure = %#v, want %#v", got, want)
	}
	serialized := fmt.Sprintf("%#v", got)
	for _, unsafe := range []string{"account-123", "provider-access-token", "unauthorized"} {
		if strings.Contains(serialized, unsafe) {
			t.Fatalf("auth refresh failure hook carried unsafe fragment %q: %s", unsafe, serialized)
		}
	}
	select {
	case extra := <-failures:
		t.Fatalf("unexpected extra auth refresh failure hook call: %#v", extra)
	default:
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()

	deadline := time.After(time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("file %s was not created", path)
		case <-ticker.C:
		}
	}
}

func TestDispatcherDetectsEarlyThreadIDConflict(t *testing.T) {
	harness := testappserver.New(t,
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart)),
		testappserver.SendNotification(testappserver.MethodThreadStarted, testappserver.ThreadStartedParams("thread-early")),
		testappserver.SendResponseFor(testappserver.MethodThreadStart, testappserver.ThreadResult("thread-response")),
	)
	dispatcher := NewDispatcher(harness.Stdin(), harness.Stdout(), 0)
	defer dispatcher.Close()

	_, err := dispatcher.Call(context.Background(), testappserver.MethodThreadStart, map[string]any{}, time.Second, CallMetadata{
		TaskID:         "task-1",
		SessionGroupID: "sg-1",
	})
	if !errors.Is(err, ErrProtocolMismatch) {
		t.Fatalf("Call(thread/start) error = %v, want ErrProtocolMismatch", err)
	}
}

func TestDispatcherCorrelatesEarlyThreadStartedNotification(t *testing.T) {
	harness := testappserver.New(t,
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart)),
		testappserver.SendNotification(testappserver.MethodThreadStarted, testappserver.ThreadStartedParams("thread-1")),
		testappserver.SendResponseFor(testappserver.MethodThreadStart, testappserver.ThreadResult("thread-1")),
	)
	dispatcher := NewDispatcher(harness.Stdin(), harness.Stdout(), 0)
	defer dispatcher.Close()

	errCh := make(chan error, 1)
	go func() {
		_, err := dispatcher.Call(context.Background(), testappserver.MethodThreadStart, map[string]any{}, time.Second, CallMetadata{
			TaskID:         "task-1",
			SessionGroupID: "sg-1",
		})
		errCh <- err
	}()

	notification := requireNotification(t, dispatcher.Notifications())
	if notification.Method != testappserver.MethodThreadStarted {
		t.Fatalf("notification method = %q, want %q", notification.Method, testappserver.MethodThreadStarted)
	}
	if notification.TaskID != "task-1" || notification.SessionGroupID != "sg-1" {
		t.Fatalf("notification metadata = task %q session %q, want task-1 sg-1", notification.TaskID, notification.SessionGroupID)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Call(thread/start) error = %v", err)
	}
	harness.RequireDone(t)
}

func TestDispatcherCorrelatesEarlyTurnStartedNotification(t *testing.T) {
	harness := testappserver.New(t,
		testappserver.ExpectRequest(testappserver.MethodTurnStart, testappserver.CaptureID(testappserver.MethodTurnStart)),
		testappserver.SendNotification(testappserver.MethodTurnStarted, testappserver.TurnStartedParams("thread-1", "turn-1")),
		testappserver.SendResponseFor(testappserver.MethodTurnStart, testappserver.TurnResult("turn-1", "running")),
	)
	dispatcher := NewDispatcher(harness.Stdin(), harness.Stdout(), 0)
	defer dispatcher.Close()

	errCh := make(chan error, 1)
	go func() {
		_, err := dispatcher.Call(context.Background(), testappserver.MethodTurnStart, map[string]any{}, time.Second, CallMetadata{
			TaskID:           "task-1",
			SessionGroupID:   "sg-1",
			ExpectedThreadID: "thread-1",
		})
		errCh <- err
	}()

	notification := requireNotification(t, dispatcher.Notifications())
	if notification.Method != testappserver.MethodTurnStarted {
		t.Fatalf("notification method = %q, want %q", notification.Method, testappserver.MethodTurnStarted)
	}
	if notification.TaskID != "task-1" || notification.SessionGroupID != "sg-1" {
		t.Fatalf("notification metadata = task %q session %q, want task-1 sg-1", notification.TaskID, notification.SessionGroupID)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Call(turn/start) error = %v", err)
	}
	harness.RequireDone(t)
}

func TestDispatcherDoesNotBindWrongThreadTurnStartedNotification(t *testing.T) {
	harness := testappserver.New(t,
		testappserver.ExpectRequest(testappserver.MethodTurnStart, testappserver.CaptureID(testappserver.MethodTurnStart)),
		testappserver.SendNotification(testappserver.MethodTurnStarted, testappserver.TurnStartedParams("thread-stale", "turn-stale")),
		testappserver.SendResponseFor(testappserver.MethodTurnStart, testappserver.TurnResult("turn-1", "running")),
	)
	dispatcher := NewDispatcher(harness.Stdin(), harness.Stdout(), 0)
	defer dispatcher.Close()

	errCh := make(chan error, 1)
	go func() {
		_, err := dispatcher.Call(context.Background(), testappserver.MethodTurnStart, map[string]any{
			"threadId": "thread-1",
		}, time.Second, CallMetadata{
			TaskID:           "task-1",
			SessionGroupID:   "sg-1",
			ExpectedThreadID: "thread-1",
		})
		errCh <- err
	}()

	notification := requireNotification(t, dispatcher.Notifications())
	if notification.Method != testappserver.MethodTurnStarted {
		t.Fatalf("notification method = %q, want %q", notification.Method, testappserver.MethodTurnStarted)
	}
	if notification.TaskID != "" || notification.SessionGroupID != "" {
		t.Fatalf("notification metadata = task %q session %q, want empty metadata", notification.TaskID, notification.SessionGroupID)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Call(turn/start) error = %v", err)
	}
	harness.RequireDone(t)
}

func TestConnectionResumeThreadDoesNotBindWrongThreadStartedNotification(t *testing.T) {
	harness := testappserver.New(t,
		testappserver.ExpectRequest(testappserver.MethodThreadResume, testappserver.CaptureID(testappserver.MethodThreadResume), testappserver.WithParams(NewThreadResumeParams("thread-1"))),
		testappserver.SendNotification(testappserver.MethodThreadStarted, testappserver.ThreadStartedParams("thread-stale")),
		testappserver.SendResponseFor(testappserver.MethodThreadResume, testappserver.ThreadResumeResult("thread-1")),
	)
	connection := NewConnection(harness.Stdin(), harness.Stdout(), config.SessionGroup{SessionGroupID: "sg-1"}, ConnectionOptions{
		SchemaPolicy: mustValidSchemaPolicy(t),
	})
	defer connection.Close()

	errCh := make(chan error, 1)
	go func() {
		_, err := connection.ResumeThread(context.Background(), ThreadResumeCall{
			ThreadID: "thread-1",
			TaskID:   "task-1",
			Timeout:  time.Second,
		})
		errCh <- err
	}()

	notification := requireNotification(t, connection.Notifications())
	if notification.Method != testappserver.MethodThreadStarted {
		t.Fatalf("notification method = %q, want %q", notification.Method, testappserver.MethodThreadStarted)
	}
	if notification.TaskID != "" || notification.SessionGroupID != "" {
		t.Fatalf("notification metadata = task %q session %q, want empty metadata", notification.TaskID, notification.SessionGroupID)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("ResumeThread() error = %v", err)
	}
	harness.RequireDone(t)
}

func TestDispatcherTimeoutReleasesRequestID(t *testing.T) {
	harness := testappserver.New(t,
		testappserver.ExpectRequest("slow", testappserver.CaptureID("slow")),
		testappserver.ExpectRequest("next", testappserver.CaptureID("next")),
		testappserver.SendResponseFor("next", map[string]any{"ok": true}),
	)
	dispatcher := NewDispatcher(harness.Stdin(), harness.Stdout(), 0)
	defer dispatcher.Close()

	_, err := dispatcher.Call(context.Background(), "slow", nil, 20*time.Millisecond, CallMetadata{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call(slow) error = %v, want deadline exceeded", err)
	}
	if _, err := dispatcher.Call(context.Background(), "next", nil, time.Second, CallMetadata{}); err != nil {
		t.Fatalf("Call(next) error = %v", err)
	}
	harness.RequireDone(t)
}

func TestDispatcherCallWriteTimeoutClosesConnection(t *testing.T) {
	stdin := newBlockingWriteCloser()
	stdout, stdoutWriter := io.Pipe()
	defer stdoutWriter.Close()
	dispatcher := NewDispatcher(stdin, stdout, 0)

	_, err := dispatcher.Call(context.Background(), "blocked", map[string]any{"ok": true}, 20*time.Millisecond, CallMetadata{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call() error = %v, want deadline exceeded", err)
	}
	stdin.requireClosed(t)
	select {
	case <-dispatcher.Done():
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not close after write timeout")
	}
}

func TestDispatcherRespondWriteTimeoutClosesConnection(t *testing.T) {
	stdin := newBlockingWriteCloser()
	stdout, stdoutWriter := io.Pipe()
	defer stdoutWriter.Close()
	dispatcher := NewDispatcher(stdin, stdout, 0)
	id := json.RawMessage("1")
	dispatcher.mu.Lock()
	dispatcher.serverRequests[idKey(id)] = &serverRequestState{
		request: ServerRequest{ID: id, Method: "blocked"},
	}
	dispatcher.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := dispatcher.Respond(ctx, id, map[string]any{"ok": true})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Respond() error = %v, want deadline exceeded", err)
	}
	stdin.requireClosed(t)
	select {
	case <-dispatcher.Done():
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not close after response write timeout")
	}
}

func TestDispatcherRespondCleansServerRequest(t *testing.T) {
	harness := testappserver.New(t,
		testappserver.SendRequest(42, "item/tool/call", map[string]any{"name": "unsupported"}),
		testappserver.ExpectResponseID(42, testappserver.WithResult(map[string]any{"ok": true})),
	)
	dispatcher := NewDispatcher(harness.Stdin(), harness.Stdout(), 0)
	defer dispatcher.Close()

	var request ServerRequest
	select {
	case request = <-dispatcher.Requests():
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not deliver server request")
	}
	if err := dispatcher.Respond(context.Background(), request.ID, map[string]any{"ok": true}); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	harness.RequireDone(t)

	dispatcher.mu.Lock()
	count := len(dispatcher.serverRequests)
	dispatcher.mu.Unlock()
	if count != 0 {
		t.Fatalf("server request entries = %d, want 0", count)
	}
}

func TestDispatcherResolvedNotificationCleansServerRequest(t *testing.T) {
	stdin := newBlockingWriteCloser()
	stdout, stdoutWriter := io.Pipe()
	defer stdoutWriter.Close()
	dispatcher := NewDispatcher(stdin, stdout, 0)
	defer dispatcher.Close()
	id := json.RawMessage("7")
	dispatcher.mu.Lock()
	dispatcher.serverRequests[idKey(id)] = &serverRequestState{
		request: ServerRequest{ID: id, Method: "item/tool/call"},
	}
	dispatcher.mu.Unlock()

	if err := dispatcher.handleNotification(rpcMessage{
		Method: "serverRequest/resolved",
		Params: json.RawMessage(`{"requestId":7}`),
	}); err != nil {
		t.Fatalf("handleNotification() error = %v", err)
	}
	notification := requireNotification(t, dispatcher.Notifications())
	if !notification.ServerRequestResolvedChecked || !notification.ServerRequestResolvedMatched {
		t.Fatalf("resolved metadata = checked %t matched %t, want checked and matched",
			notification.ServerRequestResolvedChecked, notification.ServerRequestResolvedMatched)
	}
	dispatcher.mu.Lock()
	count := len(dispatcher.serverRequests)
	dispatcher.mu.Unlock()
	if count != 0 {
		t.Fatalf("server request entries = %d, want 0", count)
	}
}

func TestDispatcherResolvedNotificationRequestIDAliasCleansServerRequest(t *testing.T) {
	stdin := newBlockingWriteCloser()
	stdout, stdoutWriter := io.Pipe()
	defer stdoutWriter.Close()
	dispatcher := NewDispatcher(stdin, stdout, 0)
	defer dispatcher.Close()
	id := json.RawMessage("7")
	dispatcher.mu.Lock()
	dispatcher.serverRequests[idKey(id)] = &serverRequestState{
		request: ServerRequest{ID: id, Method: "item/tool/call"},
	}
	dispatcher.mu.Unlock()

	if err := dispatcher.handleNotification(rpcMessage{
		Method: "serverRequest/resolved",
		Params: json.RawMessage(`{"requestID":7}`),
	}); err != nil {
		t.Fatalf("handleNotification() error = %v", err)
	}
	notification := requireNotification(t, dispatcher.Notifications())
	if !notification.ServerRequestResolvedChecked || !notification.ServerRequestResolvedMatched {
		t.Fatalf("resolved metadata = checked %t matched %t, want checked and matched",
			notification.ServerRequestResolvedChecked, notification.ServerRequestResolvedMatched)
	}
	dispatcher.mu.Lock()
	count := len(dispatcher.serverRequests)
	dispatcher.mu.Unlock()
	if count != 0 {
		t.Fatalf("server request entries = %d, want 0", count)
	}
}

func TestDispatcherResolvedNotificationDoesNotCleanRawDifferentServerRequestID(t *testing.T) {
	tests := []struct {
		name           string
		activeID       json.RawMessage
		resolvedParams json.RawMessage
	}{
		{
			name:           "active numeric request with string resolved id",
			activeID:       json.RawMessage(`101`),
			resolvedParams: json.RawMessage(`{"requestId":"101"}`),
		},
		{
			name:           "active string request with numeric resolved id",
			activeID:       json.RawMessage(`"101"`),
			resolvedParams: json.RawMessage(`{"requestId":101}`),
		},
		{
			name:           "active numeric request with string resolved id alias",
			activeID:       json.RawMessage(`101`),
			resolvedParams: json.RawMessage(`{"requestID":"101"}`),
		},
		{
			name:           "active string request with numeric resolved id alias",
			activeID:       json.RawMessage(`"101"`),
			resolvedParams: json.RawMessage(`{"requestID":101}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := testappserver.New(t)
			dispatcher := NewDispatcher(harness.Stdin(), harness.Stdout(), 0)
			defer dispatcher.Close()
			dispatcher.mu.Lock()
			dispatcher.serverRequests[idKey(tt.activeID)] = &serverRequestState{
				request: ServerRequest{ID: tt.activeID, Method: "item/tool/call"},
			}
			dispatcher.mu.Unlock()

			if err := dispatcher.handleNotification(rpcMessage{
				Method: "serverRequest/resolved",
				Params: tt.resolvedParams,
			}); err != nil {
				t.Fatalf("handleNotification() error = %v", err)
			}
			notification := requireNotification(t, dispatcher.Notifications())
			if !notification.ServerRequestResolvedChecked || notification.ServerRequestResolvedMatched {
				t.Fatalf("resolved metadata = checked %t matched %t, want checked without match",
					notification.ServerRequestResolvedChecked, notification.ServerRequestResolvedMatched)
			}
			dispatcher.mu.Lock()
			count := len(dispatcher.serverRequests)
			dispatcher.mu.Unlock()
			if count != 1 {
				t.Fatalf("server request entries = %d, want 1", count)
			}
			if err := dispatcher.Respond(context.Background(), tt.activeID, map[string]any{"ok": true}); err != nil {
				t.Fatalf("Respond() error = %v", err)
			}
			outbound := harness.RequireOutboundCount(t, 1)
			if string(outbound[0].ID) != string(tt.activeID) || outbound[0].Error != nil {
				t.Fatalf("outbound response = %#v, want success response for id %s", outbound[0], tt.activeID)
			}
		})
	}
}

func TestDispatcherResolvedNotificationDoesNotTrimStringServerRequestID(t *testing.T) {
	stdin := newBlockingWriteCloser()
	stdout, stdoutWriter := io.Pipe()
	defer stdoutWriter.Close()
	dispatcher := NewDispatcher(stdin, stdout, 0)
	defer dispatcher.Close()
	id := json.RawMessage(`" 101 "`)
	dispatcher.mu.Lock()
	dispatcher.serverRequests[idKey(id)] = &serverRequestState{
		request: ServerRequest{ID: id, Method: "item/tool/call"},
	}
	dispatcher.mu.Unlock()

	if err := dispatcher.handleNotification(rpcMessage{
		Method: "serverRequest/resolved",
		Params: json.RawMessage(`{"requestId":101}`),
	}); err != nil {
		t.Fatalf("handleNotification() error = %v", err)
	}
	notification := requireNotification(t, dispatcher.Notifications())
	if !notification.ServerRequestResolvedChecked || notification.ServerRequestResolvedMatched {
		t.Fatalf("resolved metadata = checked %t matched %t, want checked without match",
			notification.ServerRequestResolvedChecked, notification.ServerRequestResolvedMatched)
	}
	dispatcher.mu.Lock()
	count := len(dispatcher.serverRequests)
	dispatcher.mu.Unlock()
	if count != 1 {
		t.Fatalf("server request entries = %d, want 1", count)
	}
}

func TestDispatcherResolvedNotificationMarksClosedServerRequest(t *testing.T) {
	stdin := newBlockingWriteCloser()
	stdout, stdoutWriter := io.Pipe()
	defer stdoutWriter.Close()
	dispatcher := NewDispatcher(stdin, stdout, 0)
	defer dispatcher.Close()
	id := json.RawMessage("7")
	dispatcher.mu.Lock()
	dispatcher.serverRequests[idKey(id)] = &serverRequestState{
		request: ServerRequest{ID: id, Method: "item/tool/call"},
	}
	dispatcher.mu.Unlock()
	dispatcher.completeServerRequest(idKey(id))

	if err := dispatcher.handleNotification(rpcMessage{
		Method: "serverRequest/resolved",
		Params: json.RawMessage(`{"requestId":7}`),
	}); err != nil {
		t.Fatalf("handleNotification() error = %v", err)
	}
	notification := requireNotification(t, dispatcher.Notifications())
	if !notification.ServerRequestResolvedChecked || notification.ServerRequestResolvedMatched {
		t.Fatalf("resolved metadata = checked %t matched %t, want checked without match",
			notification.ServerRequestResolvedChecked, notification.ServerRequestResolvedMatched)
	}
}

func TestDispatcherCloseDoesNotPanicWhileNotificationSendIsBlocked(t *testing.T) {
	stdin := newBlockingWriteCloser()
	stdout, stdoutWriter := io.Pipe()
	defer stdoutWriter.Close()
	dispatcher := NewDispatcher(stdin, stdout, 0)

	for i := 0; i < cap(dispatcher.notifications)+1; i++ {
		message, err := newNotification(fmt.Sprintf("test/notification/%d", i), nil)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := encodeRPCJSONL(message)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stdoutWriter.Write(encoded); err != nil {
			t.Fatalf("write notification %d: %v", i, err)
		}
	}

	if err := dispatcher.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-dispatcher.Done():
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not close")
	}
}

func mustValidSchemaPolicy(t *testing.T) SchemaPolicy {
	t.Helper()
	return mustSchemaPolicyWith(t, nil)
}

func mustSchemaPolicyWith(t *testing.T, mutate func(*SchemaMetadata)) SchemaPolicy {
	t.Helper()
	metadata := mustLoadVendoredSchemaMetadata(t)
	if mutate != nil {
		mutate(&metadata)
	}
	policy, err := NewSchemaPolicy(metadata, metadata.TargetCodexVersion, false)
	if err != nil {
		t.Fatalf("NewSchemaPolicy() error = %v", err)
	}
	return policy
}

func requireSchemaUnverified(t *testing.T, err error, taskID string, sessionGroupID string, threadID string) {
	t.Helper()
	var gatewayErr *domain.GatewayError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("error = %#v, want GatewayError", err)
	}
	if gatewayErr.Code != domain.GatewayErrorCodeFailedPrecondition ||
		gatewayErr.Details.Reason != domain.ReasonAppServerSchemaUnverified ||
		gatewayErr.Details.TaskID != taskID ||
		gatewayErr.Details.SessionGroupID != sessionGroupID ||
		gatewayErr.Details.ThreadID != threadID {
		t.Fatalf("GatewayError = %#v, want failed_precondition app_server_schema_unverified for task/session/thread", gatewayErr)
	}
}

func requireNoJSONRPCWireField(t *testing.T, raw []byte) {
	t.Helper()

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("wire message JSON = %q: %v", raw, err)
	}
	if _, ok := fields["jsonrpc"]; ok {
		t.Fatalf("wire message included jsonrpc field: %s", raw)
	}
}

func requireNotification(t *testing.T, notifications <-chan Notification) Notification {
	t.Helper()
	select {
	case notification := <-notifications:
		return notification
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not deliver notification")
		return Notification{}
	}
}

type blockingWriteCloser struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingWriteCloser() *blockingWriteCloser {
	return &blockingWriteCloser{closed: make(chan struct{})}
}

func (w *blockingWriteCloser) Write([]byte) (int, error) {
	<-w.closed
	return 0, io.ErrClosedPipe
}

func (w *blockingWriteCloser) Close() error {
	w.once.Do(func() {
		close(w.closed)
	})
	return nil
}

func (w *blockingWriteCloser) requireClosed(t *testing.T) {
	t.Helper()
	select {
	case <-w.closed:
	case <-time.After(time.Second):
		t.Fatal("stdin was not closed")
	}
}
