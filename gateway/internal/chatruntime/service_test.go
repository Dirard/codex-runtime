package chatruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Dirard/codex-runtime/gateway/internal/appserver"
	"github.com/Dirard/codex-runtime/gateway/internal/chatstate"
	"github.com/Dirard/codex-runtime/gateway/internal/config"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	"github.com/Dirard/codex-runtime/gateway/internal/grpcapi"
	"github.com/Dirard/codex-runtime/gateway/internal/pending"
)

func TestStartChatRunRejectsEmptyPromptBeforeCodexCalls(t *testing.T) {
	provider := &fakeConnectionProvider{client: &fakeAppServerClient{}}
	service := newTestService(t, provider)
	command := testStartChatRunCommand("idem-1")
	command.Prompt = "   "
	_, err := service.StartChatRun(context.Background(), command)
	assertGatewayError(t, err, domain.GatewayErrorCodeInvalidArgument, domain.ReasonInvalidRequest)
	if provider.calls != 0 || provider.client.threadCalls != 0 || provider.client.turnCalls != 0 {
		t.Fatalf("Codex calls = provider %d thread %d turn %d, want 0", provider.calls, provider.client.threadCalls, provider.client.turnCalls)
	}
}

func TestStartChatRunReleasesIdempotencyWhenSupervisorFailsBeforeSideEffect(t *testing.T) {
	client := &fakeAppServerClient{
		threadResults: []json.RawMessage{json.RawMessage(`{"threadId":"thread-1"}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turnId":"turn-1"}`)},
	}
	provider := &fakeConnectionProvider{
		client: client,
		err: &domain.GatewayError{
			Code: domain.GatewayErrorCodeUnavailable,
			Details: domain.GatewayErrorDetails{
				Reason:         domain.ReasonAppServerRestartBackoff,
				DisplayMessage: "app-server restart cooldown is active",
			},
		},
	}
	service := newTestService(t, provider)
	command := testStartChatRunCommand("idem-1")
	_, err := service.StartChatRun(context.Background(), command)
	assertGatewayError(t, err, domain.GatewayErrorCodeUnavailable, domain.ReasonAppServerRestartBackoff)
	if client.threadCalls != 0 || client.turnCalls != 0 {
		t.Fatalf("Codex calls after supervisor failure = thread %d turn %d, want 0", client.threadCalls, client.turnCalls)
	}

	provider.err = nil
	response, err := service.StartChatRun(context.Background(), command)
	if err != nil {
		t.Fatalf("StartChatRun() after pre-side-effect failure error = %v", err)
	}
	if response.ChatID != "thread-1" || response.RunID != "turn-1" || !response.FirstTurnAccepted {
		t.Fatalf("StartChatRun() response = %#v, want accepted thread/run", response)
	}
}

func TestStartChatRunReturnsCodexThreadAndTurnOnlyAfterFirstTurnAccepted(t *testing.T) {
	client := &fakeAppServerClient{
		threadResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1"}}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turn":{"id":"turn-1"}}`)},
	}
	provider := &fakeConnectionProvider{client: client}
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	service := newTestServiceWithStore(t, provider, store)

	response, err := service.StartChatRun(context.Background(), testStartChatRunCommand("idem-1"))
	if err != nil {
		t.Fatalf("StartChatRun() error = %v", err)
	}
	if response.ChatID != "thread-1" || response.RunID != "turn-1" || response.SessionGroupID != "sg-1" || response.WorkspaceID != "ws-1" {
		t.Fatalf("StartChatRun() response ids = %#v, want Codex thread and turn ids", response)
	}
	if !response.FirstTurnAccepted || response.LastEventID == 0 || response.EventCursor == "" || response.ProcessEpoch != "epoch-1" {
		t.Fatalf("StartChatRun() response metadata = %#v, want accepted first turn and event cursor", response)
	}
	active, ok := store.ActiveRun(chatstate.Scope{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"})
	if !ok || active.RunID != "turn-1" || active.State != chatstate.RunStateRunning {
		t.Fatalf("ActiveRun() = (%#v, %t), want running turn", active, ok)
	}
}

func TestStartChatRunActiveRunCapacityExhaustedBeforeCodexSideEffects(t *testing.T) {
	client := &fakeAppServerClient{
		threadResults: []json.RawMessage{json.RawMessage(`{"threadId":"thread-1"}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turnId":"turn-1"}`)},
	}
	provider := &fakeConnectionProvider{client: client}
	store := chatstate.NewStore(chatstate.StoreOptions{
		Epoch: "epoch-1",
		Limits: chatstate.Limits{
			ActiveRunsCap: 1,
		},
	})
	existing := chatstate.RunScope{
		Scope: chatstate.Scope{
			SessionGroupID: "sg-1",
			WorkspaceID:    "ws-1",
			ChatID:         "thread-existing",
		},
		RunID: "turn-existing",
	}
	if _, err := store.StartRun(existing, "idem-existing"); err != nil {
		t.Fatalf("StartRun(existing) error = %v", err)
	}
	service := newTestServiceWithStore(t, provider, store)
	command := testStartChatRunCommand("idem-1")

	_, err := service.StartChatRun(context.Background(), command)
	assertGatewayError(t, err, domain.GatewayErrorCodeResourceExhausted, domain.ReasonResourceExhausted)
	if provider.calls != 0 || client.threadCalls != 0 || client.turnCalls != 0 {
		t.Fatalf("Codex calls after capacity exhaustion = provider %d thread %d turn %d, want 0", provider.calls, client.threadCalls, client.turnCalls)
	}

	if _, err := store.CompleteRun(existing, chatstate.RunStateCompleted); err != nil {
		t.Fatalf("CompleteRun(existing) error = %v", err)
	}
	response, err := service.StartChatRun(context.Background(), command)
	if err != nil {
		t.Fatalf("StartChatRun() after capacity release error = %v", err)
	}
	if response.ChatID != "thread-1" || response.RunID != "turn-1" || !response.FirstTurnAccepted {
		t.Fatalf("StartChatRun() after capacity release = %#v, want accepted thread/run", response)
	}
	if provider.calls != 1 || client.threadCalls != 1 || client.turnCalls != 1 {
		t.Fatalf("Codex calls after retry = provider %d thread %d turn %d, want one accepted sequence", provider.calls, client.threadCalls, client.turnCalls)
	}
}

func TestStartChatRunRetryAfterTurnFailureDoesNotDuplicateCodexCalls(t *testing.T) {
	client := &fakeAppServerClient{
		threadResults: []json.RawMessage{json.RawMessage(`{"threadId":"thread-1"}`)},
		turnErrs:      []error{errors.New("transport closed after write")},
	}
	provider := &fakeConnectionProvider{client: client}
	service := newTestService(t, provider)
	command := testStartChatRunCommand("idem-1")

	_, err := service.StartChatRun(context.Background(), command)
	assertGatewayError(t, err, domain.GatewayErrorCodeUnavailable, domain.ReasonDispatcherUnavailable)
	_, err = service.StartChatRun(context.Background(), command)
	assertGatewayError(t, err, domain.GatewayErrorCodeUnknown, domain.ReasonIdempotencyResultUnavailable)
	if client.threadCalls != 1 || client.turnCalls != 1 {
		t.Fatalf("Codex calls = thread %d turn %d, want no duplicate after uncertain failure", client.threadCalls, client.turnCalls)
	}
}

func TestStartChatRunSuccessfulRetryReturnsSafeResultWithoutDuplicate(t *testing.T) {
	client := &fakeAppServerClient{
		threadResults: []json.RawMessage{json.RawMessage(`{"threadId":"thread-1"}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turnId":"turn-1"}`)},
	}
	provider := &fakeConnectionProvider{client: client}
	service := newTestService(t, provider)
	command := testStartChatRunCommand("idem-1")

	first, err := service.StartChatRun(context.Background(), command)
	if err != nil {
		t.Fatalf("first StartChatRun() error = %v", err)
	}
	second, err := service.StartChatRun(context.Background(), command)
	if err != nil {
		t.Fatalf("second StartChatRun() error = %v", err)
	}
	if second.ChatID != first.ChatID || second.RunID != first.RunID || second.LastEventID != first.LastEventID || second.EventCursor != first.EventCursor {
		t.Fatalf("second StartChatRun() = %#v, want prior safe result %#v", second, first)
	}
	if client.threadCalls != 1 || client.turnCalls != 1 {
		t.Fatalf("Codex calls = thread %d turn %d, want one start sequence", client.threadCalls, client.turnCalls)
	}
}

func TestStartChatRunSameKeyDifferentClientMessageIDConflictsWithoutDuplicate(t *testing.T) {
	client := &fakeAppServerClient{
		threadResults: []json.RawMessage{
			json.RawMessage(`{"threadId":"thread-1"}`),
		},
		turnResults: []json.RawMessage{
			json.RawMessage(`{"turnId":"turn-1"}`),
		},
	}
	provider := &fakeConnectionProvider{client: client}
	service := newTestService(t, provider)
	command := testStartChatRunCommand("idem-1")

	if _, err := service.StartChatRun(context.Background(), command); err != nil {
		t.Fatalf("first StartChatRun() error = %v", err)
	}
	next := command
	next.ClientMessageID = "client-message-2"
	_, err := service.StartChatRun(context.Background(), next)
	assertGatewayError(t, err, domain.GatewayErrorCodeAborted, domain.ReasonIdempotencyScopeMismatch)
	if client.threadCalls != 1 || client.turnCalls != 1 {
		t.Fatalf("Codex calls = thread %d turn %d, want no duplicate after idempotency conflict", client.threadCalls, client.turnCalls)
	}
}

func TestStartChatRunSameRawIdempotencyKeyAcrossSessionsIsIndependent(t *testing.T) {
	firstClient := &fakeAppServerClient{
		threadResults: []json.RawMessage{json.RawMessage(`{"threadId":"thread-1"}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turnId":"turn-1"}`)},
	}
	secondClient := &fakeAppServerClient{
		threadResults: []json.RawMessage{json.RawMessage(`{"threadId":"thread-2"}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turnId":"turn-2"}`)},
	}
	service, err := NewService([]Session{
		{Group: testSessionGroup("sg-1", "ws-1"), ConnectionProvider: &fakeConnectionProvider{client: firstClient}},
		{Group: testSessionGroup("sg-2", "ws-2"), ConnectionProvider: &fakeConnectionProvider{client: secondClient}},
	}, ServiceOptions{Store: chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if _, err := service.StartChatRun(context.Background(), testStartChatRunCommand("idem-1")); err != nil {
		t.Fatalf("first StartChatRun() error = %v", err)
	}
	next := testStartChatRunCommand("idem-1")
	next.SessionGroupID = "sg-2"
	next.WorkspaceID = "ws-2"
	response, err := service.StartChatRun(context.Background(), next)
	if err != nil {
		t.Fatalf("second StartChatRun() in another session error = %v", err)
	}
	if response.ChatID != "thread-2" || response.RunID != "turn-2" {
		t.Fatalf("second StartChatRun() response = %#v, want second session thread/run", response)
	}
	if secondClient.threadCalls != 1 || secondClient.turnCalls != 1 {
		t.Fatalf("second session Codex calls = thread %d turn %d, want one start sequence", secondClient.threadCalls, secondClient.turnCalls)
	}
}

func TestStartChatRunErrorsDoNotEchoPrompt(t *testing.T) {
	client := &fakeAppServerClient{
		threadResults: []json.RawMessage{json.RawMessage(`{"threadId":"thread-1"}`)},
		turnErrs:      []error{errors.New("redaction-sentinel prompt should not leave fake transport")},
	}
	service := newTestService(t, &fakeConnectionProvider{client: client})
	command := testStartChatRunCommand("idem-1")
	command.Prompt = "redaction-sentinel prompt"

	_, err := service.StartChatRun(context.Background(), command)
	if err == nil {
		t.Fatal("StartChatRun() error = nil, want sanitized failure")
	}
	if strings.Contains(err.Error(), "redaction-sentinel") {
		t.Fatalf("StartChatRun() error leaked prompt: %q", err.Error())
	}
}

func TestGetChatReadsCodexThreadWithoutResumeOrTurnStart(t *testing.T) {
	client := &fakeAppServerClient{
		readResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`)},
	}
	service := newTestService(t, &fakeConnectionProvider{client: client})

	response, err := service.GetChat(context.Background(), domain.GetChatCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
	})
	if err != nil {
		t.Fatalf("GetChat() error = %v", err)
	}
	if response.Chat.ChatID != "thread-1" || response.Chat.ThreadLifecycle != domain.ChatThreadLifecycleIdle || response.Chat.CreatedAtUnixMS != 10000 {
		t.Fatalf("GetChat() response = %#v, want Codex thread metadata", response)
	}
	if client.readCalls != 1 || client.resumeCalls != 0 || client.threadCalls != 0 || client.turnCalls != 0 {
		t.Fatalf("Codex calls = read %d resume %d thread %d turn %d, want read-only lookup", client.readCalls, client.resumeCalls, client.threadCalls, client.turnCalls)
	}
	if len(client.readInputs) != 1 || client.readInputs[0].IncludeTurns {
		t.Fatalf("ReadThread inputs = %#v, want lightweight GetChat lookup without turns", client.readInputs)
	}
}

func TestGetChatUnknownCodexThreadReturnsNotFound(t *testing.T) {
	client := &fakeAppServerClient{
		readErrs: []error{errors.New("thread not loaded: thread-1")},
	}
	service := newTestService(t, &fakeConnectionProvider{client: client})

	_, err := service.GetChat(context.Background(), domain.GetChatCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
	})
	assertGatewayError(t, err, domain.GatewayErrorCodeNotFound, domain.ReasonUnknownChat)
}

func TestGetChatUnavailableCodexReadReturnsUnavailable(t *testing.T) {
	client := &fakeAppServerClient{
		readErrs: []error{errors.New("transport closed")},
	}
	service := newTestService(t, &fakeConnectionProvider{client: client})

	_, err := service.GetChat(context.Background(), domain.GetChatCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
	})
	assertGatewayError(t, err, domain.GatewayErrorCodeUnavailable, domain.ReasonDispatcherUnavailable)
	if client.readCalls != 1 || client.resumeCalls != 0 || client.turnCalls != 0 {
		t.Fatalf("Codex calls = read %d resume %d turn %d, want read failure only", client.readCalls, client.resumeCalls, client.turnCalls)
	}
}

func TestGetChatRejectsInvalidUnreadableMismatchedAndUnauthorizedThread(t *testing.T) {
	tests := []struct {
		name       string
		command    domain.GetChatCommand
		client     *fakeAppServerClient
		wantCode   domain.GatewayErrorCode
		wantReason domain.GatewayErrorReason
		wantReads  int
	}{
		{
			name:       "missing chat id",
			command:    domain.GetChatCommand{SessionGroupID: "sg-1", WorkspaceID: "ws-1"},
			client:     &fakeAppServerClient{},
			wantCode:   domain.GatewayErrorCodeInvalidArgument,
			wantReason: domain.ReasonInvalidRequest,
		},
		{
			name:       "invalid codex thread id",
			command:    domain.GetChatCommand{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"},
			client:     &fakeAppServerClient{readErrs: []error{errors.New("invalid thread id")}},
			wantCode:   domain.GatewayErrorCodeInvalidArgument,
			wantReason: domain.ReasonInvalidLocator,
			wantReads:  1,
		},
		{
			name:       "malformed codex read payload",
			command:    domain.GetChatCommand{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"},
			client:     &fakeAppServerClient{readResults: []json.RawMessage{json.RawMessage(`{`)}},
			wantCode:   domain.GatewayErrorCodeUnavailable,
			wantReason: domain.ReasonDispatcherUnavailable,
			wantReads:  1,
		},
		{
			name:       "codex thread id mismatch",
			command:    domain.GetChatCommand{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"},
			client:     &fakeAppServerClient{readResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-2","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`)}},
			wantCode:   domain.GatewayErrorCodeUnavailable,
			wantReason: domain.ReasonDispatcherUnavailable,
			wantReads:  1,
		},
		{
			name:       "codex thread outside workspace",
			command:    domain.GetChatCommand{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"},
			client:     &fakeAppServerClient{readResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/other-workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`)}},
			wantCode:   domain.GatewayErrorCodePermissionDenied,
			wantReason: domain.ReasonWorkspaceMismatch,
			wantReads:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeConnectionProvider{client: tt.client}
			service := newTestService(t, provider)

			_, err := service.GetChat(context.Background(), tt.command)
			assertGatewayError(t, err, tt.wantCode, tt.wantReason)
			if tt.client.readCalls != tt.wantReads {
				t.Fatalf("ReadThread calls = %d, want %d", tt.client.readCalls, tt.wantReads)
			}
			if tt.client.resumeCalls != 0 || tt.client.threadCalls != 0 || tt.client.turnCalls != 0 || tt.client.interruptCalls != 0 || tt.client.respondCalls != 0 {
				t.Fatalf("unexpected side-effect calls: thread=%d resume=%d turn=%d interrupt=%d respond=%d", tt.client.threadCalls, tt.client.resumeCalls, tt.client.turnCalls, tt.client.interruptCalls, tt.client.respondCalls)
			}
		})
	}
}

func TestGetChatStatusMapsCodexAndGatewayLifecycle(t *testing.T) {
	tests := []struct {
		name              string
		threadJSON        string
		setupStore        func(t *testing.T, store *chatstate.Store)
		wantThread        domain.ChatThreadLifecycle
		wantRun           domain.ChatTurnLifecycle
		wantCurrentRunID  string
		wantLive          bool
		wantReplay        bool
		wantReplayMissing bool
		wantActivePending int
	}{
		{
			name:              "codex idle without local active run",
			threadJSON:        `{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`,
			wantThread:        domain.ChatThreadLifecycleIdle,
			wantRun:           domain.ChatTurnLifecycleUnknown,
			wantReplayMissing: true,
		},
		{
			name:       "codex active with local active run",
			threadJSON: `{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"active"},"turns":[]}}`,
			setupStore: func(t *testing.T, store *chatstate.Store) {
				startActiveRun(t, store)
			},
			wantThread:       domain.ChatThreadLifecycleActiveRunning,
			wantRun:          domain.ChatTurnLifecycleInProgress,
			wantCurrentRunID: "turn-1",
			wantLive:         true,
			wantReplay:       true,
		},
		{
			name:              "codex system error",
			threadJSON:        `{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"systemError"},"turns":[]}}`,
			wantThread:        domain.ChatThreadLifecycleSystemError,
			wantRun:           domain.ChatTurnLifecycleUnknown,
			wantReplayMissing: true,
		},
		{
			name:              "unknown codex status",
			threadJSON:        `{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"turns":[]}}`,
			wantThread:        domain.ChatThreadLifecycleUnknown,
			wantRun:           domain.ChatTurnLifecycleUnknown,
			wantReplayMissing: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
			if tt.setupStore != nil {
				tt.setupStore(t, store)
			}
			client := &fakeAppServerClient{
				readResults: []json.RawMessage{json.RawMessage(tt.threadJSON)},
			}
			service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)

			response, err := service.GetChatStatus(context.Background(), domain.GetChatStatusCommand{
				SessionGroupID: "sg-1",
				WorkspaceID:    "ws-1",
				ChatID:         "thread-1",
			})
			if err != nil {
				t.Fatalf("GetChatStatus() error = %v", err)
			}
			status := response.Status
			if status.ThreadLifecycle != tt.wantThread || status.CurrentRunLifecycle != tt.wantRun || status.CurrentRunID != tt.wantCurrentRunID {
				t.Fatalf("GetChatStatus() status = %#v, want thread %q run %q current %q", status, tt.wantThread, tt.wantRun, tt.wantCurrentRunID)
			}
			if status.GatewayLocal.Live != tt.wantLive || status.GatewayLocal.ReplayAvailable != tt.wantReplay || status.GatewayLocal.ReplayUnavailable != tt.wantReplayMissing {
				t.Fatalf("GetChatStatus() local = %#v, want live=%t replay=%t missing=%t", status.GatewayLocal, tt.wantLive, tt.wantReplay, tt.wantReplayMissing)
			}
			if len(status.ActivePending) != tt.wantActivePending {
				t.Fatalf("GetChatStatus() active pending = %#v, want %d", status.ActivePending, tt.wantActivePending)
			}
			if client.readCalls != 1 || client.resumeCalls != 0 || client.turnCalls != 0 {
				t.Fatalf("Codex calls = read %d resume %d turn %d, want read-only status lookup", client.readCalls, client.resumeCalls, client.turnCalls)
			}
			if len(client.readInputs) != 1 || !client.readInputs[0].IncludeTurns {
				t.Fatalf("ReadThread inputs = %#v, want GetChatStatus lookup with turns", client.readInputs)
			}
		})
	}
}

func TestGetChatStatusReadsCodexTurnsForLastRunLifecycle(t *testing.T) {
	tests := []struct {
		name             string
		threadJSON       string
		wantThread       domain.ChatThreadLifecycle
		wantRun          domain.ChatTurnLifecycle
		wantCurrentRunID string
	}{
		{
			name:       "completed latest turn",
			threadJSON: `{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[{"id":"turn-0","status":"completed"},{"id":"turn-1","status":"completed"}]}}`,
			wantThread: domain.ChatThreadLifecycleIdle,
			wantRun:    domain.ChatTurnLifecycleCompleted,
		},
		{
			name:       "failed latest turn",
			threadJSON: `{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"systemError"},"turns":[{"id":"turn-0","status":"completed"},{"id":"turn-1","status":"failed"}]}}`,
			wantThread: domain.ChatThreadLifecycleSystemError,
			wantRun:    domain.ChatTurnLifecycleFailed,
		},
		{
			name:       "interrupted latest turn",
			threadJSON: `{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[{"id":"turn-0","status":"completed"},{"id":"turn-1","status":"interrupted"}]}}`,
			wantThread: domain.ChatThreadLifecycleIdle,
			wantRun:    domain.ChatTurnLifecycleInterrupted,
		},
		{
			name:             "in progress latest turn",
			threadJSON:       `{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"active"},"turns":[{"id":"turn-0","status":"completed"},{"id":"turn-1","status":"inProgress"}]}}`,
			wantThread:       domain.ChatThreadLifecycleActiveRunning,
			wantRun:          domain.ChatTurnLifecycleInProgress,
			wantCurrentRunID: "turn-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeAppServerClient{readResults: []json.RawMessage{json.RawMessage(tt.threadJSON)}}
			service := newTestService(t, &fakeConnectionProvider{client: client})

			response, err := service.GetChatStatus(context.Background(), domain.GetChatStatusCommand{
				SessionGroupID: "sg-1",
				WorkspaceID:    "ws-1",
				ChatID:         "thread-1",
			})
			if err != nil {
				t.Fatalf("GetChatStatus() error = %v", err)
			}
			status := response.Status
			if status.ThreadLifecycle != tt.wantThread || status.CurrentRunLifecycle != tt.wantRun || status.CurrentRunID != tt.wantCurrentRunID || status.LastRunID != "turn-1" {
				t.Fatalf("GetChatStatus() status = %#v, want thread %q run %q current %q last turn-1", status, tt.wantThread, tt.wantRun, tt.wantCurrentRunID)
			}
			if len(client.readInputs) != 1 || !client.readInputs[0].IncludeTurns {
				t.Fatalf("ReadThread inputs = %#v, want turns included for status", client.readInputs)
			}
		})
	}
}

func TestGetChatStatusReconcilesLocalActiveRunFromCodexTerminalTurn(t *testing.T) {
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	scope := startActiveRun(t, store)
	client := &fakeAppServerClient{
		readResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[{"id":"turn-1","status":"completed"}]}}`)},
	}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)

	response, err := service.GetChatStatus(context.Background(), domain.GetChatStatusCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
	})
	if err != nil {
		t.Fatalf("GetChatStatus() error = %v", err)
	}
	status := response.Status
	if status.ThreadLifecycle != domain.ChatThreadLifecycleIdle || status.CurrentRunLifecycle != domain.ChatTurnLifecycleCompleted || status.LastRunID != "turn-1" || status.CurrentRunID != "" {
		t.Fatalf("GetChatStatus() status = %#v, want Codex terminal turn after local reconciliation", status)
	}
	if _, ok := store.ActiveRun(scope.Scope); ok {
		t.Fatal("ActiveRun() still present after Codex terminal reconciliation, want cleared")
	}
}

func TestGetChatStatusRejectsInvalidUnknownUnavailableAndMismatchedState(t *testing.T) {
	tests := []struct {
		name       string
		command    domain.GetChatStatusCommand
		client     *fakeAppServerClient
		wantCode   domain.GatewayErrorCode
		wantReason domain.GatewayErrorReason
		wantReads  int
	}{
		{
			name:       "invalid chat id",
			command:    domain.GetChatStatusCommand{SessionGroupID: "sg-1", WorkspaceID: "ws-1"},
			client:     &fakeAppServerClient{},
			wantCode:   domain.GatewayErrorCodeInvalidArgument,
			wantReason: domain.ReasonInvalidRequest,
		},
		{
			name:       "unknown codex thread",
			command:    domain.GetChatStatusCommand{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"},
			client:     &fakeAppServerClient{readErrs: []error{errors.New("thread not loaded: thread-1")}},
			wantCode:   domain.GatewayErrorCodeNotFound,
			wantReason: domain.ReasonUnknownChat,
			wantReads:  1,
		},
		{
			name:       "codex read unavailable",
			command:    domain.GetChatStatusCommand{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"},
			client:     &fakeAppServerClient{readErrs: []error{errors.New("dispatcher stopped")}},
			wantCode:   domain.GatewayErrorCodeUnavailable,
			wantReason: domain.ReasonDispatcherUnavailable,
			wantReads:  1,
		},
		{
			name:       "codex thread id mismatch",
			command:    domain.GetChatStatusCommand{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"},
			client:     &fakeAppServerClient{readResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-2","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`)}},
			wantCode:   domain.GatewayErrorCodeUnavailable,
			wantReason: domain.ReasonDispatcherUnavailable,
			wantReads:  1,
		},
		{
			name:       "codex thread outside workspace",
			command:    domain.GetChatStatusCommand{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"},
			client:     &fakeAppServerClient{readResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/other-workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`)}},
			wantCode:   domain.GatewayErrorCodePermissionDenied,
			wantReason: domain.ReasonWorkspaceMismatch,
			wantReads:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeConnectionProvider{client: tt.client}
			service := newTestService(t, provider)

			_, err := service.GetChatStatus(context.Background(), tt.command)
			assertGatewayError(t, err, tt.wantCode, tt.wantReason)
			if tt.client.readCalls != tt.wantReads {
				t.Fatalf("ReadThread calls = %d, want %d", tt.client.readCalls, tt.wantReads)
			}
			if tt.client.resumeCalls != 0 || tt.client.turnCalls != 0 || tt.client.interruptCalls != 0 {
				t.Fatalf("unexpected side-effect calls: resume=%d turn=%d interrupt=%d", tt.client.resumeCalls, tt.client.turnCalls, tt.client.interruptCalls)
			}
		})
	}
}

func TestRunChatTurnResumesExistingThreadThenStartsTurn(t *testing.T) {
	client := &fakeAppServerClient{
		resumeResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turn":{"id":"turn-2"}}`)},
	}
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)

	response, err := service.RunChatTurn(context.Background(), testRunChatTurnCommand("idem-2"))
	if err != nil {
		t.Fatalf("RunChatTurn() error = %v", err)
	}
	if response.ChatID != "thread-1" || response.RunID != "turn-2" || !response.TurnAccepted {
		t.Fatalf("RunChatTurn() response = %#v, want accepted existing-thread turn", response)
	}
	if client.resumeCalls != 1 || client.turnCalls != 1 || client.threadCalls != 0 || client.readCalls != 0 {
		t.Fatalf("Codex calls = resume %d turn %d thread %d read %d, want resume+turn only", client.resumeCalls, client.turnCalls, client.threadCalls, client.readCalls)
	}
	active, ok := store.ActiveRun(chatstate.Scope{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"})
	if !ok || active.RunID != "turn-2" || active.State != chatstate.RunStateRunning {
		t.Fatalf("ActiveRun() = (%#v, %t), want running turn-2", active, ok)
	}
}

func TestRunChatTurnUnknownCodexThreadDoesNotStartTurn(t *testing.T) {
	client := &fakeAppServerClient{
		resumeErrs: []error{errors.New("thread not loaded: thread-1")},
	}
	service := newTestService(t, &fakeConnectionProvider{client: client})

	_, err := service.RunChatTurn(context.Background(), testRunChatTurnCommand("idem-2"))
	assertGatewayError(t, err, domain.GatewayErrorCodeNotFound, domain.ReasonUnknownChat)
	if client.resumeCalls != 1 || client.turnCalls != 0 {
		t.Fatalf("Codex calls = resume %d turn %d, want no turn/start for unknown thread", client.resumeCalls, client.turnCalls)
	}
}

func TestRunChatTurnSuccessfulRetryWhileActiveReturnsSafeResultWithoutDuplicate(t *testing.T) {
	client := &fakeAppServerClient{
		resumeResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turn":{"id":"turn-2"}}`)},
	}
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)
	command := testRunChatTurnCommand("idem-2")

	first, err := service.RunChatTurn(context.Background(), command)
	if err != nil {
		t.Fatalf("first RunChatTurn() error = %v", err)
	}
	second, err := service.RunChatTurn(context.Background(), command)
	if err != nil {
		t.Fatalf("second RunChatTurn() error = %v", err)
	}
	if second.ChatID != first.ChatID || second.RunID != first.RunID || second.LastEventID != first.LastEventID || second.EventCursor != first.EventCursor {
		t.Fatalf("second RunChatTurn() = %#v, want prior safe result %#v", second, first)
	}
	if client.resumeCalls != 1 || client.turnCalls != 1 {
		t.Fatalf("Codex calls = resume %d turn %d, want one existing-chat turn sequence", client.resumeCalls, client.turnCalls)
	}
}

func TestRunChatTurnCodexActiveThreadDoesNotStartTurn(t *testing.T) {
	client := &fakeAppServerClient{
		resumeResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"active"},"turns":[]}}`)},
	}
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)

	_, err := service.RunChatTurn(context.Background(), testRunChatTurnCommand("idem-2"))
	assertGatewayError(t, err, domain.GatewayErrorCodeFailedPrecondition, domain.ReasonAlreadyRunning)
	if client.resumeCalls != 1 || client.turnCalls != 0 || client.threadCalls != 0 || client.readCalls != 0 {
		t.Fatalf("Codex calls after active conflict = resume %d turn %d thread %d read %d, want resume proof and no turn/start", client.resumeCalls, client.turnCalls, client.threadCalls, client.readCalls)
	}
}

func TestRunChatTurnUnknownThreadStateDoesNotClearLocalActiveRun(t *testing.T) {
	client := &fakeAppServerClient{
		resumeResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"turns":[]}}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turn":{"id":"turn-2"}}`)},
	}
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	staleScope := chatstate.RunScope{
		Scope: chatstate.Scope{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"},
		RunID: "turn-stale",
	}
	if _, err := store.StartRun(staleScope, "idem-stale"); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)

	_, err := service.RunChatTurn(context.Background(), testRunChatTurnCommand("idem-2"))
	assertGatewayError(t, err, domain.GatewayErrorCodeUnavailable, domain.ReasonChatStateUnavailable)
	if client.resumeCalls != 1 || client.turnCalls != 0 {
		t.Fatalf("Codex calls = resume %d turn %d, want no turn/start for unknown thread state", client.resumeCalls, client.turnCalls)
	}
	active, ok := store.ActiveRun(staleScope.Scope)
	if !ok || active.RunID != "turn-stale" {
		t.Fatalf("ActiveRun() = (%#v, %t), want stale run preserved until Codex proves terminal state", active, ok)
	}
}

func TestRunChatTurnStaleLocalActiveRunReconcilesFromCodexIdle(t *testing.T) {
	client := &fakeAppServerClient{
		resumeResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turn":{"id":"turn-2"}}`)},
	}
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	if _, err := store.StartRun(chatstate.RunScope{
		Scope: chatstate.Scope{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"},
		RunID: "turn-stale",
	}, "idem-stale"); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)

	response, err := service.RunChatTurn(context.Background(), testRunChatTurnCommand("idem-2"))
	if err != nil {
		t.Fatalf("RunChatTurn() error = %v", err)
	}
	if response.RunID != "turn-2" {
		t.Fatalf("RunChatTurn() response = %#v, want new Codex turn after stale local reconciliation", response)
	}
	active, ok := store.ActiveRun(chatstate.Scope{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"})
	if !ok || active.RunID != "turn-2" {
		t.Fatalf("ActiveRun() = (%#v, %t), want current turn-2 active", active, ok)
	}
}

func TestRunChatTurnWorkspaceMismatchDoesNotStartTurn(t *testing.T) {
	client := &fakeAppServerClient{
		resumeResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/other-workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turn":{"id":"turn-2"}}`)},
	}
	service := newTestService(t, &fakeConnectionProvider{client: client})

	_, err := service.RunChatTurn(context.Background(), testRunChatTurnCommand("idem-2"))
	assertGatewayError(t, err, domain.GatewayErrorCodePermissionDenied, domain.ReasonWorkspaceMismatch)
	if client.resumeCalls != 1 || client.turnCalls != 0 {
		t.Fatalf("Codex calls = resume %d turn %d, want resume ownership proof and no turn/start", client.resumeCalls, client.turnCalls)
	}
}

func TestRunChatTurnSameKeyDifferentClientMessageIDConflictsWithoutDuplicate(t *testing.T) {
	client := &fakeAppServerClient{
		resumeResults: []json.RawMessage{
			json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`),
		},
		turnResults: []json.RawMessage{
			json.RawMessage(`{"turn":{"id":"turn-2"}}`),
		},
	}
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)
	command := testRunChatTurnCommand("idem-2")

	if _, err := service.RunChatTurn(context.Background(), command); err != nil {
		t.Fatalf("first RunChatTurn() error = %v", err)
	}
	if _, err := store.CompleteRun(chatstate.RunScope{
		Scope: chatstate.Scope{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"},
		RunID: "turn-2",
	}, chatstate.RunStateCompleted); err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}
	next := command
	next.ClientMessageID = "client-message-2"
	_, err := service.RunChatTurn(context.Background(), next)
	assertGatewayError(t, err, domain.GatewayErrorCodeAborted, domain.ReasonIdempotencyScopeMismatch)
	if client.resumeCalls != 1 || client.turnCalls != 1 {
		t.Fatalf("Codex calls = resume %d turn %d, want no duplicate after idempotency conflict", client.resumeCalls, client.turnCalls)
	}
}

func TestGetChatHistoryWorkspaceMismatchDoesNotListTurns(t *testing.T) {
	client := &fakeAppServerClient{
		readResults:  []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/other-workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`)},
		turnsResults: []json.RawMessage{json.RawMessage(`{"data":[{"id":"turn-1","status":"completed","itemsView":"summary"}]}`)},
	}
	service := newTestService(t, &fakeConnectionProvider{client: client})

	_, err := service.GetChatHistory(context.Background(), domain.GetChatHistoryCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
		RequestedDepth: domain.ChatHistoryDepthTurnSummary,
		Limit:          2,
	})
	assertGatewayError(t, err, domain.GatewayErrorCodePermissionDenied, domain.ReasonWorkspaceMismatch)
	if client.readCalls != 1 || client.turnsCalls != 0 {
		t.Fatalf("Codex calls = read %d turns %d, want ownership read and no turns/list", client.readCalls, client.turnsCalls)
	}
}

func TestGetChatHistoryEphemeralThreadReturnsUnavailableWithoutListingTurns(t *testing.T) {
	client := &fakeAppServerClient{
		readResults:  []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":true,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`)},
		turnsResults: []json.RawMessage{json.RawMessage(`{"data":[{"id":"turn-1","status":"completed","itemsView":"summary"}]}`)},
	}
	service := newTestService(t, &fakeConnectionProvider{client: client})

	response, err := service.GetChatHistory(context.Background(), domain.GetChatHistoryCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
		RequestedDepth: domain.ChatHistoryDepthTurnSummary,
		Limit:          2,
	})
	if err != nil {
		t.Fatalf("GetChatHistory() error = %v", err)
	}
	if response.Capability != domain.ChatCapabilityUnavailable || response.Narrowed == nil || response.Narrowed.Reason != domain.ReasonHistoryUnavailable {
		t.Fatalf("GetChatHistory() response = %#v, want unavailable history outcome", response)
	}
	if client.readCalls != 1 || client.turnsCalls != 0 {
		t.Fatalf("Codex calls = read %d turns %d, want no turns/list for ephemeral thread", client.readCalls, client.turnsCalls)
	}
}

func TestGetChatHistoryUsesCodexTurnSummariesAndNarrowsItemLevel(t *testing.T) {
	client := &fakeAppServerClient{
		readResults:  []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`)},
		turnsResults: []json.RawMessage{json.RawMessage(`{"data":[{"id":"turn-2","status":"completed","itemsView":"summary","startedAt":10,"completedAt":12,"durationMs":2000},{"id":"turn-1","status":"interrupted","itemsView":"notLoaded"}],"nextCursor":"next","backwardsCursor":"back"}`)},
	}
	service := newTestService(t, &fakeConnectionProvider{client: client})

	response, err := service.GetChatHistory(context.Background(), domain.GetChatHistoryCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
		RequestedDepth: domain.ChatHistoryDepthItemLevel,
		Limit:          2,
	})
	if err != nil {
		t.Fatalf("GetChatHistory() error = %v", err)
	}
	if response.ChatID != "thread-1" || len(response.Turns) != 2 || response.Turns[0].RunID != "turn-2" || response.Turns[0].ItemsView != domain.ChatTurnItemsViewSummary {
		t.Fatalf("GetChatHistory() response = %#v, want Codex turn summaries", response)
	}
	if response.ReturnedDepth != domain.ChatHistoryDepthTurnSummary || response.Capability != domain.ChatCapabilityNarrowed || response.Narrowed == nil {
		t.Fatalf("GetChatHistory() narrowing = %#v, want item-level narrowed to turn summary", response)
	}
	if client.turnsCalls != 1 || client.readCalls != 1 || client.resumeCalls != 0 || client.turnCalls != 0 {
		t.Fatalf("Codex calls = turns %d read %d resume %d turn %d, want ownership read + turns/list", client.turnsCalls, client.readCalls, client.resumeCalls, client.turnCalls)
	}
}

func TestGetChatHistoryWrapsCursorWithChatScope(t *testing.T) {
	client := &fakeAppServerClient{
		readResults: []json.RawMessage{
			json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`),
			json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`),
			json.RawMessage(`{"thread":{"id":"thread-2","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`),
			json.RawMessage(`{"thread":{"id":"thread-2","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`),
		},
		turnsResults: []json.RawMessage{
			json.RawMessage(`{"data":[{"id":"turn-2","status":"completed","itemsView":"summary"}],"nextCursor":"codex-next"}`),
			json.RawMessage(`{"data":[]}`),
		},
	}
	service := newTestService(t, &fakeConnectionProvider{client: client})

	first, err := service.GetChatHistory(context.Background(), domain.GetChatHistoryCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
		RequestedDepth: domain.ChatHistoryDepthTurnSummary,
		Limit:          1,
	})
	if err != nil {
		t.Fatalf("first GetChatHistory() error = %v", err)
	}
	if first.NextCursor == "" || first.NextCursor == "codex-next" {
		t.Fatalf("GetChatHistory() next cursor = %q, want opaque gateway cursor", first.NextCursor)
	}
	if _, err := service.GetChatHistory(context.Background(), domain.GetChatHistoryCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
		RequestedDepth: domain.ChatHistoryDepthTurnSummary,
		Cursor:         first.NextCursor,
		Limit:          1,
	}); err != nil {
		t.Fatalf("second GetChatHistory() error = %v", err)
	}
	if len(client.turnsInputs) != 2 || client.turnsInputs[1].Cursor != "codex-next" {
		t.Fatalf("turns/list inputs = %#v, want decoded Codex cursor on second call", client.turnsInputs)
	}

	_, err = service.GetChatHistory(context.Background(), domain.GetChatHistoryCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-2",
		RequestedDepth: domain.ChatHistoryDepthTurnSummary,
		Cursor:         first.NextCursor,
		Limit:          1,
	})
	assertGatewayError(t, err, domain.GatewayErrorCodeOutOfRange, domain.ReasonReplayOutOfRange)
	if client.turnsCalls != 2 {
		t.Fatalf("turns/list calls = %d, want cross-chat cursor rejection before list", client.turnsCalls)
	}

	tamperedCursor := tamperHistoryCursorChatID(t, first.NextCursor, "thread-2")
	_, err = service.GetChatHistory(context.Background(), domain.GetChatHistoryCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-2",
		RequestedDepth: domain.ChatHistoryDepthTurnSummary,
		Cursor:         tamperedCursor,
		Limit:          1,
	})
	assertGatewayError(t, err, domain.GatewayErrorCodeInvalidArgument, domain.ReasonInvalidCursor)
	if client.turnsCalls != 2 {
		t.Fatalf("turns/list calls = %d, want tampered cursor rejection before list", client.turnsCalls)
	}
}

func TestGetChatHistoryDoesNotLeakCodexTurnErrorMessage(t *testing.T) {
	client := &fakeAppServerClient{
		readResults:  []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`)},
		turnsResults: []json.RawMessage{json.RawMessage(`{"data":[{"id":"turn-1","status":"failed","itemsView":"summary","error":{"message":"redaction-sentinel adapter detail"}}]}`)},
	}
	service := newTestService(t, &fakeConnectionProvider{client: client})

	response, err := service.GetChatHistory(context.Background(), domain.GetChatHistoryCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
		RequestedDepth: domain.ChatHistoryDepthTurnSummary,
		Limit:          1,
	})
	if err != nil {
		t.Fatalf("GetChatHistory() error = %v", err)
	}
	if len(response.Turns) != 1 {
		t.Fatalf("GetChatHistory() turns = %d, want 1", len(response.Turns))
	}
	if response.Turns[0].Error != nil && strings.Contains(response.Turns[0].Error.DisplayMessage, "redaction-sentinel") {
		t.Fatalf("GetChatHistory() leaked raw Codex turn error: %#v", response.Turns[0].Error)
	}
}

func TestPostRestartKeepsCodexBackedLookupsButLosesProcessLocalState(t *testing.T) {
	client := &fakeAppServerClient{
		readResults: []json.RawMessage{
			json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`),
			json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`),
			json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`),
		},
		turnsResults: []json.RawMessage{json.RawMessage(`{"data":[{"id":"turn-1","status":"completed","itemsView":"summary"}]}`)},
	}
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-after-restart"})
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)

	chat, err := service.GetChat(context.Background(), domain.GetChatCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
	})
	if err != nil {
		t.Fatalf("GetChat() after restart error = %v", err)
	}
	if chat.Chat.ChatID != "thread-1" || chat.Status.ThreadLifecycle != domain.ChatThreadLifecycleIdle {
		t.Fatalf("GetChat() after restart = %#v, want Codex-backed idle chat", chat)
	}
	status, err := service.GetChatStatus(context.Background(), domain.GetChatStatusCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
	})
	if err != nil {
		t.Fatalf("GetChatStatus() after restart error = %v", err)
	}
	if status.Status.ThreadLifecycle != domain.ChatThreadLifecycleIdle || !status.Status.GatewayLocal.ReplayUnavailable {
		t.Fatalf("GetChatStatus() after restart = %#v, want Codex-backed status with process-local replay unavailable", status)
	}
	history, err := service.GetChatHistory(context.Background(), domain.GetChatHistoryCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
		RequestedDepth: domain.ChatHistoryDepthTurnSummary,
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("GetChatHistory() after restart error = %v", err)
	}
	if len(history.Turns) != 1 || history.Turns[0].RunID != "turn-1" {
		t.Fatalf("GetChatHistory() after restart = %#v, want Codex-backed turn summary", history)
	}
	stream, err := service.StreamChatEvents(context.Background(), domain.StreamChatEventsCommand{
		SessionGroupID:   "sg-1",
		WorkspaceID:      "ws-1",
		ChatID:           "thread-1",
		CursorKind:       domain.StreamCursorAfterEventID,
		AfterEventCursor: eventCursor("epoch-before-restart", "thread-1", "turn-1", 1),
	})
	if err != nil {
		t.Fatalf("StreamChatEvents() after restart error = %v", err)
	}
	defer func() {
		_ = stream.Close()
	}()
	message, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("StreamChatEvents().Next() after restart error = %v", err)
	}
	if message.ReplayNotice == nil || message.ReplayNotice.Code != domain.ChatReplayNoticeUnavailableAfterRestart {
		t.Fatalf("StreamChatEvents() after restart message = %#v, want replay-unavailable notice", message)
	}
	_, err = service.RespondChatPending(context.Background(), domain.RespondChatPendingCommand{
		SessionGroupID:   "sg-1",
		WorkspaceID:      "ws-1",
		ChatID:           "thread-1",
		PendingRequestID: "pending-before-restart",
		ClientResponseID: "response-after-restart",
		IdempotencyKey:   "idem-before-restart",
		Response: domain.PendingResponse{
			Approval: &domain.ApprovalPendingResponse{DecisionID: "decision-decline"},
		},
	})
	assertGatewayError(t, err, domain.GatewayErrorCodeUnavailable, domain.ReasonPendingUnavailableAfterRestart)
	if client.readCalls != 3 || client.turnsCalls != 1 || client.respondCalls != 0 {
		t.Fatalf("Codex calls after restart = read %d turns %d respond %d, want read-only recovery and no stale pending write", client.readCalls, client.turnsCalls, client.respondCalls)
	}
}

func TestPostRestartIdempotencyKeyOnlyDoesNotReplaySideEffectingChatResults(t *testing.T) {
	beforeClient := &fakeAppServerClient{
		threadResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-before"}}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turn":{"id":"turn-before"}}`)},
	}
	before := newTestServiceWithStore(t, &fakeConnectionProvider{client: beforeClient}, chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-before-restart"}))
	startCommand := testStartChatRunCommand("idem-start-restart")
	beforeStart, err := before.StartChatRun(context.Background(), startCommand)
	if err != nil {
		t.Fatalf("before restart StartChatRun() error = %v", err)
	}
	if beforeStart.ChatID != "thread-before" || beforeStart.RunID != "turn-before" {
		t.Fatalf("before restart StartChatRun() = %#v, want initial Codex ids", beforeStart)
	}

	afterStartClient := &fakeAppServerClient{
		threadResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-after"}}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turn":{"id":"turn-after"}}`)},
	}
	afterStart := newTestServiceWithStore(t, &fakeConnectionProvider{client: afterStartClient}, chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-after-restart"}))
	afterStartResponse, err := afterStart.StartChatRun(context.Background(), startCommand)
	if err != nil {
		t.Fatalf("after restart StartChatRun() error = %v", err)
	}
	if afterStartResponse.ChatID != "thread-after" || afterStartResponse.RunID != "turn-after" {
		t.Fatalf("after restart StartChatRun() = %#v, want fresh Codex side effect instead of stale idempotency replay", afterStartResponse)
	}
	if afterStartClient.threadCalls != 1 || afterStartClient.turnCalls != 1 {
		t.Fatalf("after restart StartChatRun Codex calls = thread %d turn %d, want fresh start sequence", afterStartClient.threadCalls, afterStartClient.turnCalls)
	}

	afterTurnClient := &fakeAppServerClient{
		resumeResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[]}}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turn":{"id":"turn-after"}}`)},
	}
	afterTurn := newTestServiceWithStore(t, &fakeConnectionProvider{client: afterTurnClient}, chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-after-restart"}))
	runCommand := testRunChatTurnCommand("idem-turn-restart")
	turnResponse, err := afterTurn.RunChatTurn(context.Background(), runCommand)
	if err != nil {
		t.Fatalf("after restart RunChatTurn() error = %v", err)
	}
	if turnResponse.RunID != "turn-after" {
		t.Fatalf("after restart RunChatTurn() = %#v, want fresh Codex turn instead of stale idempotency replay", turnResponse)
	}
	if afterTurnClient.resumeCalls != 1 || afterTurnClient.turnCalls != 1 {
		t.Fatalf("after restart RunChatTurn Codex calls = resume %d turn %d, want fresh existing-chat turn sequence", afterTurnClient.resumeCalls, afterTurnClient.turnCalls)
	}

	afterInterruptClient := &fakeAppServerClient{interruptResults: []json.RawMessage{json.RawMessage(`{}`)}}
	afterInterruptProvider := &fakeConnectionProvider{client: afterInterruptClient}
	afterInterrupt := newTestServiceWithStore(t, afterInterruptProvider, chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-after-restart"}))
	_, err = afterInterrupt.InterruptChatRun(context.Background(), domain.InterruptChatRunCommand{
		SessionGroupID:  "sg-1",
		WorkspaceID:     "ws-1",
		ChatID:          "thread-1",
		RunID:           "turn-before",
		ClientRequestID: "interrupt-after-restart",
		IdempotencyKey:  "idem-interrupt-restart",
	})
	assertGatewayError(t, err, domain.GatewayErrorCodeFailedPrecondition, domain.ReasonInvalidRequest)
	if afterInterruptProvider.calls != 0 || afterInterruptClient.interruptCalls != 0 {
		t.Fatalf("after restart InterruptChatRun Codex calls = provider %d interrupt %d, want key-only request rejected without stale replay", afterInterruptProvider.calls, afterInterruptClient.interruptCalls)
	}
}

func TestStreamChatEventsReplaysCurrentRunStatusEvent(t *testing.T) {
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	scope := chatstate.RunScope{
		Scope: chatstate.Scope{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"},
		RunID: "turn-1",
	}
	if _, err := store.StartRun(scope, "idem-1"); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := store.AppendEvent(chatstate.EventInput{
		RunScope:  scope,
		Kind:      "status",
		State:     string(chatstate.RunStateRunning),
		SizeBytes: 1,
	}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: &fakeAppServerClient{}}, store)

	stream, err := service.StreamChatEvents(context.Background(), domain.StreamChatEventsCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
		CursorKind:     domain.StreamCursorFromStart,
	})
	if err != nil {
		t.Fatalf("StreamChatEvents() error = %v", err)
	}
	defer func() {
		_ = stream.Close()
	}()
	message, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("StreamChatEvents().Next() error = %v", err)
	}
	if message.Event == nil || message.Event.EventID != 1 || message.Event.EventCursor == "" {
		t.Fatalf("stream message = %#v, want replayed status event with opaque cursor", message)
	}
	parsed, ok := parseEventCursor(message.Event.EventCursor)
	if !ok || parsed.Epoch != "epoch-1" || parsed.ChatID != "thread-1" || parsed.RunID != "turn-1" || parsed.EventID != 1 {
		t.Fatalf("event cursor parsed = %#v ok=%t, want scoped event cursor", parsed, ok)
	}
	if message.Event.StatusUpdated == nil || message.Event.StatusUpdated.CurrentRunID != "turn-1" {
		t.Fatalf("stream status = %#v, want current turn status", message.Event.StatusUpdated)
	}
}

func TestChatNotificationBridgeStreamsAssistantAndTerminalEvents(t *testing.T) {
	notifications := make(chan appserver.Notification, 8)
	client := &fakeAppServerClient{
		notifications: notifications,
		threadResults: []json.RawMessage{json.RawMessage(`{"thread":{"id":"thread-1"}}`)},
		turnResults:   []json.RawMessage{json.RawMessage(`{"turn":{"id":"turn-1"}}`)},
	}
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)

	response, err := service.StartChatRun(context.Background(), testStartChatRunCommand("idem-1"))
	if err != nil {
		t.Fatalf("StartChatRun() error = %v", err)
	}
	stream, err := service.StreamChatEvents(context.Background(), domain.StreamChatEventsCommand{
		SessionGroupID:     "sg-1",
		WorkspaceID:        "ws-1",
		ChatID:             response.ChatID,
		CursorKind:         domain.StreamCursorAfterEventID,
		AfterEventCursor:   response.EventCursor,
		ClientSubscriberID: "subscriber-1",
	})
	if err != nil {
		t.Fatalf("StreamChatEvents() error = %v", err)
	}
	defer func() {
		_ = stream.Close()
	}()
	next := func() grpcapi.StreamChatEventsMessage {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		message, err := stream.Next(ctx)
		if err != nil {
			t.Fatalf("StreamChatEvents().Next() error = %v", err)
		}
		return message
	}

	notifications <- appserver.Notification{
		Method: "item/agentMessage/delta",
		Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","delta":"hel"}`),
	}
	delta := next()
	if delta.Event == nil || delta.Event.AssistantDelta == nil || delta.Event.AssistantDelta.TextDelta != "hel" {
		t.Fatalf("delta event = %#v, want assistant delta", delta.Event)
	}

	notifications <- appserver.Notification{
		Method: "item/completed",
		Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","completedAtMs":1,"item":{"id":"item-1","type":"agentMessage","text":"hello"}}`),
	}
	completed := next()
	if completed.Event == nil || completed.Event.AssistantMessageCompleted == nil || completed.Event.AssistantMessageCompleted.Message != "hello" {
		t.Fatalf("completed event = %#v, want assistant completed", completed.Event)
	}

	notifications <- appserver.Notification{
		Method: "turn/completed",
		Params: json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`),
	}
	terminal := next()
	if terminal.Event == nil || terminal.Event.Terminal == nil || terminal.Event.Terminal.State != domain.ChatTurnLifecycleCompleted {
		t.Fatalf("terminal event = %#v, want completed terminal", terminal.Event)
	}
	if _, ok := store.ActiveRun(chatstate.Scope{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"}); ok {
		t.Fatal("ActiveRun() still present after turn/completed notification")
	}
}

func TestStreamChatEventsRejectsEventCursorForAnotherChat(t *testing.T) {
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	cursor := eventCursor("epoch-1", "thread-1", "turn-1", 1)
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: &fakeAppServerClient{}}, store)

	_, err := service.StreamChatEvents(context.Background(), domain.StreamChatEventsCommand{
		SessionGroupID:   "sg-1",
		WorkspaceID:      "ws-1",
		ChatID:           "thread-2",
		CursorKind:       domain.StreamCursorAfterEventID,
		AfterEventCursor: cursor,
	})
	assertGatewayError(t, err, domain.GatewayErrorCodeOutOfRange, domain.ReasonReplayOutOfRange)
}

func TestStreamChatEventsReplaysAfterValidEventCursor(t *testing.T) {
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	scope := startActiveRun(t, store)
	first, err := store.AppendEvent(chatstate.EventInput{
		RunScope:  scope,
		Kind:      "status",
		State:     string(chatstate.RunStateRunning),
		SizeBytes: 1,
	})
	if err != nil {
		t.Fatalf("AppendEvent(first) error = %v", err)
	}
	second, err := store.AppendEvent(chatstate.EventInput{
		RunScope:  scope,
		Kind:      "status",
		State:     string(chatstate.RunStateInterrupting),
		SizeBytes: 1,
	})
	if err != nil {
		t.Fatalf("AppendEvent(second) error = %v", err)
	}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: &fakeAppServerClient{}}, store)

	stream, err := service.StreamChatEvents(context.Background(), domain.StreamChatEventsCommand{
		SessionGroupID:     "sg-1",
		WorkspaceID:        "ws-1",
		ChatID:             "thread-1",
		CursorKind:         domain.StreamCursorAfterEventID,
		AfterEventCursor:   eventCursor("epoch-1", "thread-1", "turn-1", first.EventID),
		ClientSubscriberID: "subscriber-1",
	})
	if err != nil {
		t.Fatalf("StreamChatEvents() error = %v", err)
	}
	defer func() {
		_ = stream.Close()
	}()
	message, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("StreamChatEvents().Next() error = %v", err)
	}
	if message.Event == nil || message.Event.EventID != second.EventID || message.Event.RunID != "turn-1" || message.Event.EventCursor == "" {
		t.Fatalf("stream message = %#v, want replayed second event only", message)
	}
	if message.Event.StatusUpdated == nil || message.Event.StatusUpdated.CurrentRunLifecycle != domain.ChatTurnLifecycleInProgress {
		t.Fatalf("stream status = %#v, want typed status for replayed event", message.Event.StatusUpdated)
	}
}

func TestStreamChatEventsReturnsReplayUnavailableAfterRestartEpoch(t *testing.T) {
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-2"})
	startActiveRun(t, store)
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: &fakeAppServerClient{}}, store)

	stream, err := service.StreamChatEvents(context.Background(), domain.StreamChatEventsCommand{
		SessionGroupID:   "sg-1",
		WorkspaceID:      "ws-1",
		ChatID:           "thread-1",
		CursorKind:       domain.StreamCursorAfterEventID,
		AfterEventCursor: eventCursor("epoch-1", "thread-1", "turn-1", 1),
	})
	if err != nil {
		t.Fatalf("StreamChatEvents() error = %v", err)
	}
	defer func() {
		_ = stream.Close()
	}()
	message, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("StreamChatEvents().Next() error = %v", err)
	}
	if message.ReplayNotice == nil || message.ReplayNotice.Code != domain.ChatReplayNoticeUnavailableAfterRestart || message.ReplayNotice.ProcessEpoch != "epoch-2" {
		t.Fatalf("stream message = %#v, want unavailable-after-restart replay notice", message)
	}
}

func TestStreamChatEventsReturnsCursorEvictedNotice(t *testing.T) {
	store := chatstate.NewStore(chatstate.StoreOptions{
		Epoch: "epoch-1",
		Limits: chatstate.Limits{
			ReplayMaxEvents: 1,
			ReplayMaxBytes:  1024,
			ReplayTTL:       time.Hour,
		},
	})
	scope := startActiveRun(t, store)
	first, err := store.AppendEvent(chatstate.EventInput{RunScope: scope, Kind: "status", State: string(chatstate.RunStateRunning), SizeBytes: 1})
	if err != nil {
		t.Fatalf("AppendEvent(first) error = %v", err)
	}
	if _, err := store.AppendEvent(chatstate.EventInput{RunScope: scope, Kind: "status", State: string(chatstate.RunStatePending), SizeBytes: 1}); err != nil {
		t.Fatalf("AppendEvent(second) error = %v", err)
	}
	if _, err := store.AppendEvent(chatstate.EventInput{RunScope: scope, Kind: "status", State: string(chatstate.RunStateInterrupting), SizeBytes: 1}); err != nil {
		t.Fatalf("AppendEvent(third) error = %v", err)
	}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: &fakeAppServerClient{}}, store)

	stream, err := service.StreamChatEvents(context.Background(), domain.StreamChatEventsCommand{
		SessionGroupID:   "sg-1",
		WorkspaceID:      "ws-1",
		ChatID:           "thread-1",
		CursorKind:       domain.StreamCursorAfterEventID,
		AfterEventCursor: eventCursor("epoch-1", "thread-1", "turn-1", first.EventID),
	})
	if err != nil {
		t.Fatalf("StreamChatEvents() error = %v", err)
	}
	defer func() {
		_ = stream.Close()
	}()
	message, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("StreamChatEvents().Next() error = %v", err)
	}
	if message.ReplayNotice == nil || message.ReplayNotice.Code != domain.ChatReplayNoticeCursorEvicted {
		t.Fatalf("stream message = %#v, want cursor-evicted replay notice", message)
	}
}

func TestChatPendingHookClaimsActiveChatRunRequestAndStreamsPendingEvent(t *testing.T) {
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	scope := startActiveRun(t, store)
	client := &fakeAppServerClient{}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)
	service.configureConnection(client)
	if client.forwardedHook == nil {
		t.Fatal("forwarded hook = nil, want configured chat pending hook")
	}

	claimed := client.forwardedHook(testCommandApprovalServerRequest("req-1"))
	if !claimed {
		t.Fatal("forwarded hook claimed = false, want active chat pending request claimed")
	}
	status := service.statusForRun(scope, chatstate.RunStatePending, 1)
	if len(status.ActivePending) != 1 || status.ActivePending[0].PendingRequestID != "pending-1" || status.ActivePending[0].ChatID != "thread-1" || status.ActivePending[0].RunID != "turn-1" {
		t.Fatalf("status active pending = %#v, want pending-1 for thread-1/turn-1", status.ActivePending)
	}

	stream, err := service.StreamChatEvents(context.Background(), domain.StreamChatEventsCommand{
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "thread-1",
		CursorKind:     domain.StreamCursorFromStart,
	})
	if err != nil {
		t.Fatalf("StreamChatEvents() error = %v", err)
	}
	defer func() {
		_ = stream.Close()
	}()
	message, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("StreamChatEvents().Next() error = %v", err)
	}
	if message.Event == nil || message.Event.PendingCreated == nil || message.Event.PendingCreated.PendingRequestID != "pending-1" {
		t.Fatalf("stream event = %#v, want pending created payload", message.Event)
	}
	if message.Event.StatusUpdated != nil {
		t.Fatalf("stream event status payload = %#v, want concrete pending-created payload", message.Event.StatusUpdated)
	}
}

func TestRespondChatPendingWritesCodexResponseAndIdempotentRetryDoesNotWriteTwice(t *testing.T) {
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	scope := startActiveRun(t, store)
	client := &fakeAppServerClient{}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)
	service.configureConnection(client)
	if !client.forwardedHook(testCommandApprovalServerRequest("req-1")) {
		t.Fatal("pending hook did not claim command approval request")
	}

	command := domain.RespondChatPendingCommand{
		SessionGroupID:   "sg-1",
		WorkspaceID:      "ws-1",
		ChatID:           "thread-1",
		PendingRequestID: "pending-1",
		ClientResponseID: "response-1",
		IdempotencyKey:   "idem-response-1",
		Response: domain.PendingResponse{
			Approval: &domain.ApprovalPendingResponse{DecisionID: "decision-decline"},
		},
	}
	first, err := service.RespondChatPending(context.Background(), command)
	if err != nil {
		t.Fatalf("RespondChatPending() error = %v", err)
	}
	if !first.Accepted || first.AlreadyApplied || first.Status.CurrentRunLifecycle != domain.ChatTurnLifecycleInProgress {
		t.Fatalf("first RespondChatPending() = %#v, want accepted running status", first)
	}
	if client.respondCalls != 1 || len(client.respondPayloads) != 1 {
		t.Fatalf("respond calls = %d payloads = %d, want one app-server response", client.respondCalls, len(client.respondPayloads))
	}
	if got, want := mustJSON(t, client.respondPayloads[0]), `{"decision":"decline"}`; got != want {
		t.Fatalf("app-server response payload = %s, want %s", got, want)
	}
	active, ok := store.ActiveRun(scope.Scope)
	if !ok || active.State != chatstate.RunStateRunning {
		t.Fatalf("ActiveRun() after response = (%#v, %t), want running active run", active, ok)
	}
	service.mu.Lock()
	pendingRecords := len(service.pendingRecords)
	service.mu.Unlock()
	if pendingRecords != 0 {
		t.Fatalf("pendingRecords len after accepted response = %d, want 0", pendingRecords)
	}

	second, err := service.RespondChatPending(context.Background(), command)
	if err != nil {
		t.Fatalf("second RespondChatPending() error = %v", err)
	}
	if !second.AlreadyApplied || second.LastEventID != first.LastEventID {
		t.Fatalf("second RespondChatPending() = %#v, want idempotent prior result %#v", second, first)
	}
	if client.respondCalls != 1 {
		t.Fatalf("respond calls after retry = %d, want no duplicate app-server response", client.respondCalls)
	}
}

func TestRespondChatPendingRejectsExpiredPendingBeforeCodexWrite(t *testing.T) {
	now := time.Unix(1000, 0)
	store := chatstate.NewStore(chatstate.StoreOptions{
		Epoch: "epoch-1",
		Now: func() time.Time {
			return now
		},
		Limits: chatstate.Limits{PendingTTL: time.Second},
	})
	scope := startActiveRun(t, store)
	client := &fakeAppServerClient{}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)
	service.configureConnection(client)
	if !client.forwardedHook(testCommandApprovalServerRequest("req-1")) {
		t.Fatal("pending hook did not claim command approval request")
	}

	now = now.Add(2 * time.Second)
	_, err := service.RespondChatPending(context.Background(), domain.RespondChatPendingCommand{
		SessionGroupID:   "sg-1",
		WorkspaceID:      "ws-1",
		ChatID:           "thread-1",
		PendingRequestID: "pending-1",
		ClientResponseID: "response-1",
		IdempotencyKey:   "idem-response-1",
		Response: domain.PendingResponse{
			Approval: &domain.ApprovalPendingResponse{DecisionID: "decision-decline"},
		},
	})
	assertGatewayError(t, err, domain.GatewayErrorCodeUnavailable, domain.ReasonPendingUnavailableAfterRestart)
	if client.respondCalls != 0 {
		t.Fatalf("respond calls = %d, want no app-server write for expired pending", client.respondCalls)
	}
	status := service.statusForRun(scope, chatstate.RunStatePending, 1)
	if len(status.ActivePending) != 0 {
		t.Fatalf("status active pending after expiry = %#v, want none", status.ActivePending)
	}
	service.mu.Lock()
	pendingRecords := len(service.pendingRecords)
	service.mu.Unlock()
	if pendingRecords != 0 {
		t.Fatalf("pendingRecords len after expired response = %d, want 0", pendingRecords)
	}
}

func TestRespondChatPendingRejectsExpiredWrongTypeBeforeTypeDisclosureAndCleansUp(t *testing.T) {
	now := time.Unix(1000, 0)
	store := chatstate.NewStore(chatstate.StoreOptions{
		Epoch: "epoch-1",
		Now: func() time.Time {
			return now
		},
		Limits: chatstate.Limits{PendingTTL: time.Second},
	})
	startActiveRun(t, store)
	client := &fakeAppServerClient{}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)
	service.configureConnection(client)
	if !client.forwardedHook(testCommandApprovalServerRequest("req-1")) {
		t.Fatal("pending hook did not claim command approval request")
	}

	now = now.Add(2 * time.Second)
	_, err := service.RespondChatPending(context.Background(), domain.RespondChatPendingCommand{
		SessionGroupID:   "sg-1",
		WorkspaceID:      "ws-1",
		ChatID:           "thread-1",
		PendingRequestID: "pending-1",
		ClientResponseID: "response-1",
		IdempotencyKey:   "idem-response-1",
		Response: domain.PendingResponse{
			McpElicitation: &domain.McpElicitationPendingResponse{Action: domain.McpElicitationActionCancel},
		},
	})
	assertGatewayError(t, err, domain.GatewayErrorCodeUnavailable, domain.ReasonPendingUnavailableAfterRestart)
	if client.respondCalls != 0 {
		t.Fatalf("respond calls = %d, want no app-server write for expired pending", client.respondCalls)
	}
	service.mu.Lock()
	pendingRecords := len(service.pendingRecords)
	service.mu.Unlock()
	if pendingRecords != 0 {
		t.Fatalf("pendingRecords len after expired wrong-type response = %d, want 0", pendingRecords)
	}
}

func TestChatPendingHookCleansExpiredRawPendingOnNextPendingTrigger(t *testing.T) {
	now := time.Unix(1000, 0)
	store := chatstate.NewStore(chatstate.StoreOptions{
		Epoch: "epoch-1",
		Now: func() time.Time {
			return now
		},
		Limits: chatstate.Limits{PendingTTL: time.Second},
	})
	startActiveRun(t, store)
	client := &fakeAppServerClient{}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)
	service.configureConnection(client)
	if !client.forwardedHook(testCommandApprovalServerRequest("req-1")) {
		t.Fatal("pending hook did not claim first command approval request")
	}
	service.mu.Lock()
	pendingRecords := len(service.pendingRecords)
	service.mu.Unlock()
	if pendingRecords != 1 {
		t.Fatalf("pendingRecords len after first pending = %d, want 1", pendingRecords)
	}

	now = now.Add(2 * time.Second)
	if !client.forwardedHook(testCommandApprovalServerRequest("req-2")) {
		t.Fatal("pending hook did not claim second command approval request")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.pendingRecords) != 1 {
		t.Fatalf("pendingRecords len after cleanup trigger = %d, want only the fresh pending record", len(service.pendingRecords))
	}
	for _, tracked := range service.pendingRecords {
		if tracked == nil || tracked.record == nil || tracked.record.Pending.PendingRequestID != "pending-2" {
			t.Fatalf("pendingRecords after cleanup trigger = %#v, want pending-2 only", service.pendingRecords)
		}
	}
}

func TestRespondChatPendingSameIdempotencyKeyForDifferentPendingIDConflictsWithoutCodexWrite(t *testing.T) {
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	startActiveRun(t, store)
	client := &fakeAppServerClient{}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)
	service.configureConnection(client)
	if !client.forwardedHook(testCommandApprovalServerRequest("req-1")) {
		t.Fatal("pending hook did not claim first command approval request")
	}
	if !client.forwardedHook(testCommandApprovalServerRequest("req-2")) {
		t.Fatal("pending hook did not claim second command approval request")
	}
	first := domain.RespondChatPendingCommand{
		SessionGroupID:   "sg-1",
		WorkspaceID:      "ws-1",
		ChatID:           "thread-1",
		PendingRequestID: "pending-1",
		ClientResponseID: "response-1",
		IdempotencyKey:   "idem-response-1",
		Response: domain.PendingResponse{
			Approval: &domain.ApprovalPendingResponse{DecisionID: "decision-decline"},
		},
	}
	if _, err := service.RespondChatPending(context.Background(), first); err != nil {
		t.Fatalf("first RespondChatPending() error = %v", err)
	}
	second := first
	second.PendingRequestID = "pending-2"

	_, err := service.RespondChatPending(context.Background(), second)
	assertGatewayError(t, err, domain.GatewayErrorCodeAborted, domain.ReasonIdempotencyScopeMismatch)
	if client.respondCalls != 1 {
		t.Fatalf("respond calls = %d, want no second app-server write after idempotency scope mismatch", client.respondCalls)
	}
}

func TestRespondChatPendingAfterRunGoneReturnsUnavailableWithoutCodexWrite(t *testing.T) {
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	scope := startActiveRun(t, store)
	client := &fakeAppServerClient{}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)
	service.configureConnection(client)
	if !client.forwardedHook(testCommandApprovalServerRequest("req-1")) {
		t.Fatal("pending hook did not claim command approval request")
	}
	if _, err := store.CompleteRun(scope, chatstate.RunStateFailed); err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}

	_, err := service.RespondChatPending(context.Background(), domain.RespondChatPendingCommand{
		SessionGroupID:   "sg-1",
		WorkspaceID:      "ws-1",
		ChatID:           "thread-1",
		PendingRequestID: "pending-1",
		ClientResponseID: "response-1",
		IdempotencyKey:   "idem-response-1",
		Response: domain.PendingResponse{
			Approval: &domain.ApprovalPendingResponse{DecisionID: "decision-decline"},
		},
	})
	assertGatewayError(t, err, domain.GatewayErrorCodeUnavailable, domain.ReasonPendingUnavailableAfterRestart)
	if client.respondCalls != 0 {
		t.Fatalf("respond calls = %d, want no app-server write after active run is gone", client.respondCalls)
	}
}

func TestRespondChatPendingAfterInterruptPreservesInterruptingState(t *testing.T) {
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	scope := startActiveRun(t, store)
	client := &fakeAppServerClient{
		interruptResults: []json.RawMessage{json.RawMessage(`{}`)},
	}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)
	service.configureConnection(client)
	if !client.forwardedHook(testCommandApprovalServerRequest("req-1")) {
		t.Fatal("pending hook did not claim command approval request")
	}
	if _, err := service.InterruptChatRun(context.Background(), domain.InterruptChatRunCommand{
		SessionGroupID:  "sg-1",
		WorkspaceID:     "ws-1",
		ChatID:          "thread-1",
		RunID:           "turn-1",
		ClientRequestID: "interrupt-1",
		IdempotencyKey:  "idem-interrupt-1",
	}); err != nil {
		t.Fatalf("InterruptChatRun() error = %v", err)
	}

	response, err := service.RespondChatPending(context.Background(), domain.RespondChatPendingCommand{
		SessionGroupID:   "sg-1",
		WorkspaceID:      "ws-1",
		ChatID:           "thread-1",
		PendingRequestID: "pending-1",
		ClientResponseID: "response-1",
		IdempotencyKey:   "idem-response-1",
		Response: domain.PendingResponse{
			Approval: &domain.ApprovalPendingResponse{DecisionID: "decision-decline"},
		},
	})
	if err != nil {
		t.Fatalf("RespondChatPending() error = %v", err)
	}
	if !response.Accepted || response.Status.CurrentRunLifecycle != domain.ChatTurnLifecycleInProgress {
		t.Fatalf("RespondChatPending() = %#v, want accepted in-progress response", response)
	}
	active, ok := store.ActiveRun(scope.Scope)
	if !ok || active.State != chatstate.RunStateInterrupting {
		t.Fatalf("ActiveRun() after pending response following interrupt = (%#v, %t), want interrupting", active, ok)
	}
	if client.respondCalls != 1 || client.interruptCalls != 1 {
		t.Fatalf("respond/interrupt calls = %d/%d, want one each", client.respondCalls, client.interruptCalls)
	}
}

func TestRespondChatPendingWriteInterleavingDoesNotOverwriteInterruptingState(t *testing.T) {
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	scope := startActiveRun(t, store)
	client := &fakeAppServerClient{}
	client.respondHook = func() {
		if _, err := store.UpdateRunState(scope, chatstate.RunStateInterrupting); err != nil {
			t.Fatalf("UpdateRunState(interrupting) during response write error = %v", err)
		}
	}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)
	service.configureConnection(client)
	if !client.forwardedHook(testCommandApprovalServerRequest("req-1")) {
		t.Fatal("pending hook did not claim command approval request")
	}

	response, err := service.RespondChatPending(context.Background(), domain.RespondChatPendingCommand{
		SessionGroupID:   "sg-1",
		WorkspaceID:      "ws-1",
		ChatID:           "thread-1",
		PendingRequestID: "pending-1",
		ClientResponseID: "response-1",
		IdempotencyKey:   "idem-response-1",
		Response: domain.PendingResponse{
			Approval: &domain.ApprovalPendingResponse{DecisionID: "decision-decline"},
		},
	})
	if err != nil {
		t.Fatalf("RespondChatPending() error = %v", err)
	}
	if !response.Accepted || response.Status.CurrentRunLifecycle != domain.ChatTurnLifecycleInProgress {
		t.Fatalf("RespondChatPending() = %#v, want accepted in-progress response", response)
	}
	active, ok := store.ActiveRun(scope.Scope)
	if !ok || active.State != chatstate.RunStateInterrupting {
		t.Fatalf("ActiveRun() after pending response write interleaving = (%#v, %t), want interrupting", active, ok)
	}
}

func TestInterruptChatRunSendsCodexInterruptAndKeepsRunNonTerminal(t *testing.T) {
	store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
	scope := startActiveRun(t, store)
	client := &fakeAppServerClient{
		interruptResults: []json.RawMessage{json.RawMessage(`{}`)},
	}
	service := newTestServiceWithStore(t, &fakeConnectionProvider{client: client}, store)
	command := domain.InterruptChatRunCommand{
		SessionGroupID:  "sg-1",
		WorkspaceID:     "ws-1",
		ChatID:          "thread-1",
		RunID:           "turn-1",
		ClientRequestID: "interrupt-1",
		IdempotencyKey:  "idem-interrupt-1",
	}

	first, err := service.InterruptChatRun(context.Background(), command)
	if err != nil {
		t.Fatalf("InterruptChatRun() error = %v", err)
	}
	if !first.InterruptSent || first.AlreadyInterrupting || first.Status.CurrentRunLifecycle != domain.ChatTurnLifecycleInProgress {
		t.Fatalf("InterruptChatRun() = %#v, want interrupt sent and in-progress status", first)
	}
	if client.interruptCalls != 1 || len(client.interruptInputs) != 1 || client.interruptInputs[0].TurnID != "turn-1" {
		t.Fatalf("interrupt calls = %d inputs = %#v, want one turn interrupt", client.interruptCalls, client.interruptInputs)
	}
	active, ok := store.ActiveRun(scope.Scope)
	if !ok || active.State != chatstate.RunStateInterrupting {
		t.Fatalf("ActiveRun() after interrupt = (%#v, %t), want interrupting active run", active, ok)
	}

	second, err := service.InterruptChatRun(context.Background(), command)
	if err != nil {
		t.Fatalf("second InterruptChatRun() error = %v", err)
	}
	if !second.InterruptSent || second.LastEventID != first.LastEventID {
		t.Fatalf("second InterruptChatRun() = %#v, want idempotent prior result %#v", second, first)
	}
	if client.interruptCalls != 1 {
		t.Fatalf("interrupt calls after retry = %d, want no duplicate interrupt", client.interruptCalls)
	}
}

func TestInterruptChatRunRejectsNoActiveMismatchAndAlreadyInterruptingBeforeCodexCall(t *testing.T) {
	tests := []struct {
		name               string
		setupStore         func(t *testing.T, store *chatstate.Store)
		runID              string
		wantAlreadyRunning bool
		wantError          bool
	}{
		{
			name:      "no active run before turn exists",
			runID:     "turn-1",
			wantError: true,
		},
		{
			name: "run id mismatch",
			setupStore: func(t *testing.T, store *chatstate.Store) {
				startActiveRun(t, store)
			},
			runID:     "turn-other",
			wantError: true,
		},
		{
			name: "already interrupting",
			setupStore: func(t *testing.T, store *chatstate.Store) {
				scope := startActiveRun(t, store)
				if _, err := store.UpdateRunState(scope, chatstate.RunStateInterrupting); err != nil {
					t.Fatalf("UpdateRunState(interrupting) error = %v", err)
				}
			},
			runID:              "turn-1",
			wantAlreadyRunning: true,
		},
		{
			name: "completed run is no longer interruptible",
			setupStore: func(t *testing.T, store *chatstate.Store) {
				scope := startActiveRun(t, store)
				if _, err := store.CompleteRun(scope, chatstate.RunStateCompleted); err != nil {
					t.Fatalf("CompleteRun(completed) error = %v", err)
				}
			},
			runID:     "turn-1",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})
			if tt.setupStore != nil {
				tt.setupStore(t, store)
			}
			client := &fakeAppServerClient{interruptResults: []json.RawMessage{json.RawMessage(`{}`)}}
			provider := &fakeConnectionProvider{client: client}
			service := newTestServiceWithStore(t, provider, store)

			response, err := service.InterruptChatRun(context.Background(), domain.InterruptChatRunCommand{
				SessionGroupID:  "sg-1",
				WorkspaceID:     "ws-1",
				ChatID:          "thread-1",
				RunID:           tt.runID,
				ClientRequestID: "interrupt-1",
				IdempotencyKey:  "idem-interrupt-1",
			})
			if tt.wantError {
				assertGatewayError(t, err, domain.GatewayErrorCodeFailedPrecondition, domain.ReasonInvalidRequest)
			} else if err != nil {
				t.Fatalf("InterruptChatRun() error = %v", err)
			} else if !response.AlreadyInterrupting || response.InterruptSent {
				t.Fatalf("InterruptChatRun() = %#v, want already-interrupting without new interrupt", response)
			}
			if provider.calls != 0 || client.interruptCalls != 0 {
				t.Fatalf("Codex interrupt path called provider=%d interrupt=%d, want no Codex side effect", provider.calls, client.interruptCalls)
			}
		})
	}
}

func TestExistingChatOperationsRejectWorkspaceMismatchBeforeRuntimeStateOrCodexSideEffects(t *testing.T) {
	tests := []struct {
		name string
		call func(*Service) error
	}{
		{
			name: "get chat",
			call: func(service *Service) error {
				_, err := service.GetChat(context.Background(), domain.GetChatCommand{
					SessionGroupID: "sg-1",
					WorkspaceID:    "ws-other",
					ChatID:         "thread-1",
				})
				return err
			},
		},
		{
			name: "run chat turn",
			call: func(service *Service) error {
				command := testRunChatTurnCommand("idem-scope")
				command.WorkspaceID = "ws-other"
				_, err := service.RunChatTurn(context.Background(), command)
				return err
			},
		},
		{
			name: "get chat status",
			call: func(service *Service) error {
				_, err := service.GetChatStatus(context.Background(), domain.GetChatStatusCommand{
					SessionGroupID: "sg-1",
					WorkspaceID:    "ws-other",
					ChatID:         "thread-1",
				})
				return err
			},
		},
		{
			name: "get chat history",
			call: func(service *Service) error {
				_, err := service.GetChatHistory(context.Background(), domain.GetChatHistoryCommand{
					SessionGroupID: "sg-1",
					WorkspaceID:    "ws-other",
					ChatID:         "thread-1",
					RequestedDepth: domain.ChatHistoryDepthTurnSummary,
					Limit:          1,
				})
				return err
			},
		},
		{
			name: "stream chat events",
			call: func(service *Service) error {
				stream, err := service.StreamChatEvents(context.Background(), domain.StreamChatEventsCommand{
					SessionGroupID: "sg-1",
					WorkspaceID:    "ws-other",
					ChatID:         "thread-1",
					CursorKind:     domain.StreamCursorFromStart,
				})
				if stream != nil {
					_ = stream.Close()
				}
				return err
			},
		},
		{
			name: "respond chat pending",
			call: func(service *Service) error {
				_, err := service.RespondChatPending(context.Background(), domain.RespondChatPendingCommand{
					SessionGroupID:   "sg-1",
					WorkspaceID:      "ws-other",
					ChatID:           "thread-1",
					PendingRequestID: "pending-1",
					ClientResponseID: "response-1",
					IdempotencyKey:   "idem-response-scope",
					Response: domain.PendingResponse{
						Approval: &domain.ApprovalPendingResponse{DecisionID: "decision-decline"},
					},
				})
				return err
			},
		},
		{
			name: "interrupt chat run",
			call: func(service *Service) error {
				_, err := service.InterruptChatRun(context.Background(), domain.InterruptChatRunCommand{
					SessionGroupID:  "sg-1",
					WorkspaceID:     "ws-other",
					ChatID:          "thread-1",
					RunID:           "turn-1",
					ClientRequestID: "interrupt-scope",
					IdempotencyKey:  "idem-interrupt-scope",
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeAppServerClient{interruptResults: []json.RawMessage{json.RawMessage(`{}`)}}
			provider := &fakeConnectionProvider{client: client}
			service := newTestService(t, provider)

			err := tt.call(service)
			assertGatewayError(t, err, domain.GatewayErrorCodeInvalidArgument, domain.ReasonWorkspaceMismatch)
			if provider.calls != 0 || client.threadCalls != 0 || client.resumeCalls != 0 || client.readCalls != 0 || client.turnsCalls != 0 || client.turnCalls != 0 || client.interruptCalls != 0 || client.respondCalls != 0 {
				t.Fatalf("unexpected calls after explicit workspace mismatch: provider=%d thread=%d resume=%d read=%d turns=%d turn=%d interrupt=%d respond=%d",
					provider.calls, client.threadCalls, client.resumeCalls, client.readCalls, client.turnsCalls, client.turnCalls, client.interruptCalls, client.respondCalls)
			}
		})
	}
}

func TestCodexBackedExistingChatOperationsRejectThreadOutsideWorkspaceBeforeSideEffects(t *testing.T) {
	foreignThread := json.RawMessage(`{"thread":{"id":"thread-1","cwd":"D:/other-workspace","preview":"hello","ephemeral":false,"createdAt":10,"updatedAt":20,"status":{"type":"idle"},"turns":[{"id":"turn-1","status":"completed"}]}}`)
	tests := []struct {
		name        string
		client      *fakeAppServerClient
		call        func(*Service) error
		wantReads   int
		wantResume  int
		wantTurns   int
		wantTurnRun int
	}{
		{
			name:      "get chat",
			client:    &fakeAppServerClient{readResults: []json.RawMessage{foreignThread}},
			wantReads: 1,
			call: func(service *Service) error {
				_, err := service.GetChat(context.Background(), domain.GetChatCommand{
					SessionGroupID: "sg-1",
					WorkspaceID:    "ws-1",
					ChatID:         "thread-1",
				})
				return err
			},
		},
		{
			name:      "get chat status",
			client:    &fakeAppServerClient{readResults: []json.RawMessage{foreignThread}},
			wantReads: 1,
			call: func(service *Service) error {
				_, err := service.GetChatStatus(context.Background(), domain.GetChatStatusCommand{
					SessionGroupID: "sg-1",
					WorkspaceID:    "ws-1",
					ChatID:         "thread-1",
				})
				return err
			},
		},
		{
			name:      "get chat history",
			client:    &fakeAppServerClient{readResults: []json.RawMessage{foreignThread}, turnsResults: []json.RawMessage{json.RawMessage(`{"data":[{"id":"turn-1","status":"completed","itemsView":"summary"}]}`)}},
			wantReads: 1,
			call: func(service *Service) error {
				_, err := service.GetChatHistory(context.Background(), domain.GetChatHistoryCommand{
					SessionGroupID: "sg-1",
					WorkspaceID:    "ws-1",
					ChatID:         "thread-1",
					RequestedDepth: domain.ChatHistoryDepthTurnSummary,
					Limit:          1,
				})
				return err
			},
		},
		{
			name:       "run chat turn",
			client:     &fakeAppServerClient{resumeResults: []json.RawMessage{foreignThread}, turnResults: []json.RawMessage{json.RawMessage(`{"turn":{"id":"turn-2"}}`)}},
			wantResume: 1,
			call: func(service *Service) error {
				_, err := service.RunChatTurn(context.Background(), testRunChatTurnCommand("idem-foreign-workspace"))
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newTestService(t, &fakeConnectionProvider{client: tt.client})

			err := tt.call(service)
			assertGatewayError(t, err, domain.GatewayErrorCodePermissionDenied, domain.ReasonWorkspaceMismatch)
			if tt.client.readCalls != tt.wantReads || tt.client.resumeCalls != tt.wantResume || tt.client.turnsCalls != tt.wantTurns || tt.client.turnCalls != tt.wantTurnRun {
				t.Fatalf("Codex calls = read %d resume %d turns %d turn %d, want read %d resume %d turns %d turn %d",
					tt.client.readCalls, tt.client.resumeCalls, tt.client.turnsCalls, tt.client.turnCalls,
					tt.wantReads, tt.wantResume, tt.wantTurns, tt.wantTurnRun)
			}
			if tt.client.threadCalls != 0 || tt.client.interruptCalls != 0 || tt.client.respondCalls != 0 {
				t.Fatalf("unexpected side-effect calls after foreign Codex thread: thread=%d interrupt=%d respond=%d", tt.client.threadCalls, tt.client.interruptCalls, tt.client.respondCalls)
			}
		})
	}
}

func tamperHistoryCursorChatID(t *testing.T, cursor string, chatID string) string {
	t.Helper()
	var signed signedCursorEnvelope
	if !decodeCursorEnvelope(cursor, &signed) {
		t.Fatalf("decode signed history cursor failed")
	}
	var payload historyCursorEnvelope
	if err := json.Unmarshal(signed.Payload, &payload); err != nil {
		t.Fatalf("decode history cursor payload: %v", err)
	}
	payload.ChatID = chatID
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode tampered history cursor payload: %v", err)
	}
	signed.Payload = rawPayload
	return encodeCursorEnvelope(signed)
}

type fakeConnectionProvider struct {
	client *fakeAppServerClient
	err    error
	calls  int
}

func (p *fakeConnectionProvider) Connection(context.Context) (AppServerClient, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return p.client, nil
}

type fakeAppServerClient struct {
	sessionGroupID       string
	forwardedHook        appserver.ForwardedServerRequestHook
	threadResults        []json.RawMessage
	threadErrs           []error
	resumeResults        []json.RawMessage
	resumeErrs           []error
	readResults          []json.RawMessage
	readErrs             []error
	turnsResults         []json.RawMessage
	turnsErrs            []error
	turnResults          []json.RawMessage
	turnErrs             []error
	interruptResults     []json.RawMessage
	interruptErrs        []error
	respondErrs          []error
	respondErrorErrs     []error
	respondHook          func()
	notifications        chan appserver.Notification
	threadCalls          int
	resumeCalls          int
	readCalls            int
	turnsCalls           int
	turnCalls            int
	interruptCalls       int
	respondCalls         int
	respondErrorCalls    int
	turnInputs           [][]appserver.UserInputText
	readInputs           []appserver.ThreadReadCall
	turnsInputs          []appserver.ThreadTurnsListCall
	interruptInputs      []appserver.TurnInterruptCall
	respondRequests      []appserver.ServerRequest
	respondPayloads      []any
	respondErrorRequests []appserver.ServerRequest
	respondErrorCodes    []int
	respondErrorMessages []string
}

func (c *fakeAppServerClient) StartThread(context.Context, appserver.ThreadStartCall) (json.RawMessage, error) {
	c.threadCalls++
	index := c.threadCalls - 1
	if index < len(c.threadErrs) && c.threadErrs[index] != nil {
		return nil, c.threadErrs[index]
	}
	if index < len(c.threadResults) {
		return c.threadResults[index], nil
	}
	return nil, errors.New("unexpected thread/start call")
}

func (c *fakeAppServerClient) ResumeThread(context.Context, appserver.ThreadResumeCall) (json.RawMessage, error) {
	c.resumeCalls++
	index := c.resumeCalls - 1
	if index < len(c.resumeErrs) && c.resumeErrs[index] != nil {
		return nil, c.resumeErrs[index]
	}
	if index < len(c.resumeResults) {
		return c.resumeResults[index], nil
	}
	return nil, errors.New("unexpected thread/resume call")
}

func (c *fakeAppServerClient) ReadThread(_ context.Context, call appserver.ThreadReadCall) (json.RawMessage, error) {
	c.readCalls++
	c.readInputs = append(c.readInputs, call)
	index := c.readCalls - 1
	if index < len(c.readErrs) && c.readErrs[index] != nil {
		return nil, c.readErrs[index]
	}
	if index < len(c.readResults) {
		return c.readResults[index], nil
	}
	return nil, errors.New("unexpected thread/read call")
}

func (c *fakeAppServerClient) ListThreadTurns(_ context.Context, call appserver.ThreadTurnsListCall) (json.RawMessage, error) {
	c.turnsCalls++
	c.turnsInputs = append(c.turnsInputs, call)
	index := c.turnsCalls - 1
	if index < len(c.turnsErrs) && c.turnsErrs[index] != nil {
		return nil, c.turnsErrs[index]
	}
	if index < len(c.turnsResults) {
		return c.turnsResults[index], nil
	}
	return nil, errors.New("unexpected thread/turns/list call")
}

func (c *fakeAppServerClient) StartTurn(_ context.Context, call appserver.TurnStartCall) (json.RawMessage, error) {
	c.turnCalls++
	c.turnInputs = append(c.turnInputs, append([]appserver.UserInputText(nil), call.Input...))
	index := c.turnCalls - 1
	if index < len(c.turnErrs) && c.turnErrs[index] != nil {
		return nil, c.turnErrs[index]
	}
	if index < len(c.turnResults) {
		return c.turnResults[index], nil
	}
	return nil, errors.New("unexpected turn/start call")
}

func (c *fakeAppServerClient) InterruptTurn(_ context.Context, call appserver.TurnInterruptCall) (json.RawMessage, error) {
	c.interruptCalls++
	c.interruptInputs = append(c.interruptInputs, call)
	index := c.interruptCalls - 1
	if index < len(c.interruptErrs) && c.interruptErrs[index] != nil {
		return nil, c.interruptErrs[index]
	}
	if index < len(c.interruptResults) {
		return c.interruptResults[index], nil
	}
	return nil, errors.New("unexpected turn/interrupt call")
}

func (c *fakeAppServerClient) RespondServerRequest(_ context.Context, request appserver.ServerRequest, payload any, _ time.Duration) error {
	c.respondCalls++
	c.respondRequests = append(c.respondRequests, request)
	c.respondPayloads = append(c.respondPayloads, payload)
	if c.respondHook != nil {
		c.respondHook()
	}
	index := c.respondCalls - 1
	if index < len(c.respondErrs) && c.respondErrs[index] != nil {
		return c.respondErrs[index]
	}
	return nil
}

func (c *fakeAppServerClient) RespondServerRequestError(_ context.Context, request appserver.ServerRequest, code int, message string, _ time.Duration) error {
	c.respondErrorCalls++
	c.respondErrorRequests = append(c.respondErrorRequests, request)
	c.respondErrorCodes = append(c.respondErrorCodes, code)
	c.respondErrorMessages = append(c.respondErrorMessages, message)
	index := c.respondErrorCalls - 1
	if index < len(c.respondErrorErrs) && c.respondErrorErrs[index] != nil {
		return c.respondErrorErrs[index]
	}
	return nil
}

func (c *fakeAppServerClient) SetForwardedServerRequestHook(hook appserver.ForwardedServerRequestHook) {
	c.forwardedHook = hook
}

func (c *fakeAppServerClient) SessionGroupID() string {
	if c.sessionGroupID != "" {
		return c.sessionGroupID
	}
	return "sg-1"
}

func (c *fakeAppServerClient) Notifications() <-chan appserver.Notification {
	return c.notifications
}

func newTestService(t *testing.T, provider *fakeConnectionProvider) *Service {
	t.Helper()
	return newTestServiceWithStore(t, provider, chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"}))
}

func newTestServiceWithStore(t *testing.T, provider *fakeConnectionProvider, store *chatstate.Store) *Service {
	t.Helper()
	service, err := NewService([]Session{
		{Group: testSessionGroup("sg-1", "ws-1"), ConnectionProvider: provider},
	}, ServiceOptions{Store: store})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func testStartChatRunCommand(idempotencyKey string) domain.StartChatRunCommand {
	return domain.StartChatRunCommand{
		SessionGroupID:  "sg-1",
		WorkspaceID:     "ws-1",
		Prompt:          "hello",
		ClientMessageID: "client-message-1",
		IdempotencyKey:  idempotencyKey,
	}
}

func testRunChatTurnCommand(idempotencyKey string) domain.RunChatTurnCommand {
	return domain.RunChatTurnCommand{
		SessionGroupID:  "sg-1",
		WorkspaceID:     "ws-1",
		ChatID:          "thread-1",
		Prompt:          "continue",
		ClientMessageID: "client-message-1",
		IdempotencyKey:  idempotencyKey,
	}
}

func testSessionGroup(sessionGroupID string, workspaceID string) config.SessionGroup {
	return config.SessionGroup{
		SessionGroupID: sessionGroupID,
		WorkspaceID:    workspaceID,
		CWD:            "D:/workspace",
		CanonicalCWD:   "D:/workspace",
	}
}

func startActiveRun(t *testing.T, store *chatstate.Store) chatstate.RunScope {
	t.Helper()
	scope := chatstate.RunScope{
		Scope: chatstate.Scope{SessionGroupID: "sg-1", WorkspaceID: "ws-1", ChatID: "thread-1"},
		RunID: "turn-1",
	}
	if _, err := store.StartRun(scope, "idem-run-1"); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := store.UpdateRunState(scope, chatstate.RunStateRunning); err != nil {
		t.Fatalf("UpdateRunState() error = %v", err)
	}
	return scope
}

func testCommandApprovalServerRequest(id string) appserver.ServerRequest {
	return appserver.ServerRequest{
		ID:     json.RawMessage(`"` + id + `"`),
		Method: pending.MethodCommandApproval,
		Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","command":"echo ok","cwd":"D:/workspace"}`),
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal(%T) error = %v", value, err)
	}
	return string(raw)
}

func assertGatewayError(t *testing.T, err error, code domain.GatewayErrorCode, reason domain.GatewayErrorReason) {
	t.Helper()
	var gatewayErr *domain.GatewayError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("error = %#v, want GatewayError", err)
	}
	if gatewayErr.Code != code || gatewayErr.Details.Reason != reason {
		t.Fatalf("GatewayError = (%q, %q), want (%q, %q)", gatewayErr.Code, gatewayErr.Details.Reason, code, reason)
	}
}
