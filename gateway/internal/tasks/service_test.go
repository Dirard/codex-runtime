package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dirard/codex-runtime/gateway/internal/appserver"
	"github.com/Dirard/codex-runtime/gateway/internal/config"
	"github.com/Dirard/codex-runtime/gateway/internal/contextpack"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	"github.com/Dirard/codex-runtime/gateway/internal/grpcapi"
	"github.com/Dirard/codex-runtime/gateway/internal/pending"
	"github.com/Dirard/codex-runtime/gateway/internal/redact"
	"github.com/Dirard/codex-runtime/gateway/internal/testappserver"
)

func TestPromptOnlyTaskStartsThroughAppServer(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	if response.TaskID == "" || response.ThreadID != "thread-1" || response.TurnID != "turn-1" ||
		response.SessionGroupID != "sg-1" || response.State != domain.TaskStateRunning || response.LastEventID == 0 {
		t.Fatalf("StartTask() response = %#v", response)
	}
	harness.RequireDone(t)
}

func TestAuthRefreshUnavailableFailsActiveTask(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	authRefreshGate := make(chan struct{})
	var releaseAuthRefreshGate sync.Once
	releaseAuthRefresh := func() {
		releaseAuthRefreshGate.Do(func() {
			close(authRefreshGate)
		})
	}
	defer releaseAuthRefresh()
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-1")...)
	steps = append(steps,
		testappserver.WaitForSignal("auth refresh after active task start", authRefreshGate),
		testappserver.SendRequest("auth-1", "account/chatgptAuthTokens/refresh", map[string]any{
			"reason":            "unauthorized",
			"previousAccountId": "account-1",
		}),
		testappserver.ExpectErrorResponseID("auth-1", -32001, "auth_refresh_unavailable"),
	)
	service, harness := newHarnessService(t, group, steps...)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	releaseAuthRefresh()
	status := waitStatusState(t, service, response.TaskID, domain.TaskStateFailed)
	if status.Terminal == nil || status.Terminal.TerminalState != domain.TerminalStateFailed ||
		status.Terminal.ErrorMessage != string(domain.ReasonAuthRefreshUnavailable) {
		t.Fatalf("status after auth refresh failure = %#v", status)
	}
	events := readStreamEvents(t, service, response.TaskID, int(status.LastEventID))
	terminal, ok := events[len(events)-1].Payload.(domain.TaskTerminalEvent)
	if !ok || terminal.TerminalState != domain.TerminalStateFailed ||
		terminal.ErrorMessage != string(domain.ReasonAuthRefreshUnavailable) {
		t.Fatalf("last event after auth refresh failure = %#v", events[len(events)-1])
	}
	harness.RequireDone(t)
}

func TestUncorrelatedAuthRefreshFailureRecordsDiagnosticOnly(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}

	service.handleAuthRefreshFailure(appserver.AuthRefreshFailure{
		SessionGroupID: group.SessionGroupID,
		Reason:         domain.ReasonAuthRefreshUnavailable,
	})

	diagnostics := service.connectionDiagnosticsSnapshot()
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want one auth refresh diagnostic", diagnostics)
	}
	if diagnostics[0].Code != string(domain.ReasonAuthRefreshUnavailable) ||
		diagnostics[0].Method != "account/chatgptAuthTokens/refresh" ||
		diagnostics[0].TaskID != "" {
		t.Fatalf("auth refresh diagnostic = %#v", diagnostics[0])
	}
}

func TestUnsupportedServerRequestPublishesExplicitTaskWarning(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-1")...)
	steps = append(steps,
		testappserver.SendRequest(101, "attestation/generate", map[string]any{
			"taskId":    "task-1",
			"challenge": "raw challenge should not appear",
		}),
		testappserver.ExpectErrorResponseID(101, -32002, "unsupported_server_request"),
	)
	service, harness := newHarnessService(t, group, steps...)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	events := readStreamEvents(t, service, response.TaskID, int(response.LastEventID)+1)
	warning, ok := events[len(events)-1].Payload.(domain.GatewayWarningEvent)
	if !ok || warning.Code != unsupportedServerRequestWarningCode {
		t.Fatalf("last event = %#v, want unsupported request warning", events[len(events)-1])
	}
	if !strings.Contains(warning.Message, "attestation/generate") {
		t.Fatalf("unsupported request warning message = %q, want method detail", warning.Message)
	}
	if strings.Contains(warning.Message, "raw challenge") {
		t.Fatalf("unsupported request warning leaked params: %#v", warning)
	}
	harness.RequireDone(t)
	if diagnostics := service.connectionDiagnosticsSnapshot(); len(diagnostics) != 0 {
		t.Fatalf("explicit unsupported request recorded diagnostics: %#v", diagnostics)
	}
}

func TestUncorrelatedUnsupportedServerRequestWithActiveTaskRecordsDiagnosticOnly(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-1")...)
	steps = append(steps,
		testappserver.SendRequest(101, "attestation/generate", map[string]any{
			"challenge": "raw challenge should not appear",
		}),
		testappserver.ExpectErrorResponseID(101, -32002, "unsupported_server_request"),
	)
	service, harness := newHarnessService(t, group, steps...)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	diagnostics := waitConnectionDiagnosticCount(t, service, 1)
	if diagnostics[0].Code != pending.WarningCodeUnsupportedServerRequest ||
		diagnostics[0].Method != "attestation/generate" ||
		diagnostics[0].TaskID != "" {
		t.Fatalf("unsupported diagnostic = %#v", diagnostics[0])
	}
	harness.RequireDone(t)
	status := waitPendingCount(t, service, response.TaskID, 0)
	events := readStreamEvents(t, service, response.TaskID, int(status.LastEventID))
	for _, event := range events {
		if warning, ok := event.Payload.(domain.GatewayWarningEvent); ok &&
			warning.Code == unsupportedServerRequestWarningCode {
			t.Fatalf("uncorrelated unsupported request emitted task warning: %#v", event)
		}
	}
	raw, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatalf("json.Marshal(diagnostics) error = %v", err)
	}
	if strings.Contains(string(raw), "raw challenge") {
		t.Fatalf("connection diagnostics leaked raw params: %s", raw)
	}
}

func TestPrefilteredUnsupportedServerRequestPublishesExplicitTurnWarning(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-1")...)
	steps = append(steps,
		testappserver.SendRequest(101, "attestation/generate", map[string]any{
			"threadId":  "thread-1",
			"turnId":    "turn-1",
			"challenge": "raw challenge should not appear",
		}),
		testappserver.ExpectErrorResponseID(101, -32002, "unsupported_server_request"),
	)
	service, harness := newHarnessService(t, group, steps...)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	events := readStreamEvents(t, service, response.TaskID, int(response.LastEventID)+1)
	warning, ok := events[len(events)-1].Payload.(domain.GatewayWarningEvent)
	if !ok || warning.Code != unsupportedServerRequestWarningCode {
		t.Fatalf("last event = %#v, want unsupported request warning", events[len(events)-1])
	}
	if !strings.Contains(warning.Message, "attestation/generate") {
		t.Fatalf("unsupported request warning message = %q, want method detail", warning.Message)
	}
	if strings.Contains(warning.Message, "raw challenge") {
		t.Fatalf("unsupported request warning leaked params: %#v", warning)
	}
	harness.RequireDone(t)
	if diagnostics := service.connectionDiagnosticsSnapshot(); len(diagnostics) != 0 {
		t.Fatalf("explicit turn unsupported request recorded diagnostics: %#v", diagnostics)
	}
}

func TestStartTaskRequiresClientMessageID(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	supervisor := &countingSupervisor{}
	service, err := NewService([]Session{{Group: group, Supervisor: supervisor}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.StartTask(context.Background(), startCommand(group, "", "Do it"))
	assertGatewayReason(t, err, domain.ReasonInvalidRequest)
	if got := supervisor.calls.Load(); got != 0 {
		t.Fatalf("supervisor calls = %d, want 0", got)
	}
}

func TestStartTaskNormalizesWorkspaceForIdempotency(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...)...,
	)
	command := startCommand(group, "client-1", "Do it")
	command.WorkspaceID = ""

	response, err := service.StartTask(context.Background(), command)
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	retry, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("retry StartTask() error = %v", err)
	}
	if response != retry {
		t.Fatalf("retry response = %#v, want %#v", retry, response)
	}
	status := waitStatusByClient(t, service, group.SessionGroupID, "client-1", domain.TaskStateRunning)
	if status.WorkspaceID != group.WorkspaceID {
		t.Fatalf("status workspace = %q, want %q", status.WorkspaceID, group.WorkspaceID)
	}
	harness.RequireDone(t)
}

func TestStartTaskRejectsWorkspaceMismatchBeforeAppServerCall(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	supervisor := &countingSupervisor{}
	service, err := NewService([]Session{{Group: group, Supervisor: supervisor}})
	if err != nil {
		t.Fatal(err)
	}
	command := startCommand(group, "client-1", "Do it")
	command.WorkspaceID = "other-workspace"

	_, err = service.StartTask(context.Background(), command)
	assertGatewayReason(t, err, domain.ReasonWorkspaceMismatch)
	if got := supervisor.calls.Load(); got != 0 {
		t.Fatalf("supervisor calls = %d, want 0", got)
	}
}

func TestStartTaskRejectsInvalidContextBeforeAppServerCall(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	supervisor := &countingSupervisor{}
	service, err := NewService([]Session{{Group: group, Supervisor: supervisor}})
	if err != nil {
		t.Fatal(err)
	}
	command := startCommandWithContext(group, "client-1", "Do it", []domain.ContextBlock{{
		Kind:        domain.ContextBlockKindUntrusted,
		SourceLabel: "local",
		SourceURI:   "file:///tmp/context.txt",
		Content:     "must not be sent",
	}})

	_, err = service.StartTask(context.Background(), command)
	assertGatewayReason(t, err, domain.ReasonInvalidRequest)
	if got := supervisor.calls.Load(); got != 0 {
		t.Fatalf("supervisor calls = %d, want 0", got)
	}
}

func TestContextTaskUsesEnvelopeUserInputTextShape(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	contextBlock := domain.ContextBlock{
		Kind:        domain.ContextBlockKindUntrusted,
		SourceLabel: "ticket",
		SourceURI:   "https://example.invalid/ticket",
		MimeType:    "text/plain",
		Content:     "The reported symptom is a blank screen.",
	}
	checkTurnStart := testappserver.CheckMessage(func(message testappserver.Message) error {
		var params struct {
			ThreadID string `json:"threadId"`
			Input    []struct {
				Type         string `json:"type"`
				Text         string `json:"text"`
				TextElements []any  `json:"text_elements"`
			} `json:"input"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return err
		}
		if params.ThreadID != "thread-1" || len(params.Input) != 1 {
			return errors.New("turn/start input identity is invalid")
		}
		input := params.Input[0]
		if input.Type != "text" || len(input.TextElements) != 0 {
			return errors.New("turn/start input is not UserInput.Text")
		}
		if !strings.HasPrefix(input.Text, contextpack.Header+"\n") {
			return errors.New("turn/start text is missing the context envelope header")
		}
		if !strings.Contains(input.Text, `"userPrompt":"Use the attached context."`) ||
			!strings.Contains(input.Text, `"sourceLabel":"ticket"`) ||
			!strings.Contains(input.Text, `"kind":"untrusted"`) {
			return errors.New("turn/start text is missing the context envelope payload")
		}
		return nil
	})
	service, harness := newHarnessService(t, group,
		append(testappserver.ThreadStart("thread-1"),
			testappserver.ExpectRequest(testappserver.MethodTurnStart, testappserver.CaptureID(testappserver.MethodTurnStart), checkTurnStart),
			testappserver.SendNotification(testappserver.MethodTurnStarted, testappserver.TurnStartedParams("thread-1", "turn-1")),
			testappserver.SendResponseFor(testappserver.MethodTurnStart, testappserver.TurnResult("turn-1", "running")),
		)...,
	)

	_, err := service.StartTask(context.Background(), startCommandWithContext(group, "client-1", "Use the attached context.", []domain.ContextBlock{contextBlock}))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	harness.RequireDone(t)
}

func TestUncorrelatedLifecycleNotificationsDoNotChangeTaskIdentity(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	supervisor := &countingSupervisor{}
	service, err := NewService([]Session{{Group: group, Supervisor: supervisor}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := &task{
		id:              "task-1",
		sessionGroupID:  group.SessionGroupID,
		workspaceID:     group.WorkspaceID,
		clientMessageID: "client-1",
		state:           domain.TaskStateStarting,
		phase:           startPhaseThreadCalling,
		connection:      connection,
		createdAt:       time.Now(),
		subscribers:     map[uint64]*taskSubscriber{},
	}
	service.mu.Lock()
	service.tasks[task.id] = task
	service.sessions[group.SessionGroupID].activeTaskID = task.id
	service.appendEventLocked(task, domain.TaskLifecycleEvent{
		LifecycleEvent: domain.TaskLifecycleEventTaskStarted,
		State:          domain.TaskStateStarting,
	})
	service.mu.Unlock()

	service.handleNotification(appserver.Notification{
		Method: testappserver.MethodThreadStarted,
		Params: mustRawMessage(t, testappserver.ThreadStartedParams("stale-thread")),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: testappserver.MethodTurnStarted,
		Params: mustRawMessage(t, testappserver.TurnStartedParams("stale-thread", "stale-turn")),
	}, connection)

	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: task.id},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.ThreadID != "" || status.TurnID != "" || status.LastEventID != 1 {
		t.Fatalf("status identity = %#v", status)
	}
	for _, event := range readStreamEvents(t, service, task.id, 1) {
		if event.ThreadID == "stale-thread" || event.TurnID == "stale-turn" {
			t.Fatalf("stream event used stale identity: %#v", event)
		}
	}
	if got := supervisor.calls.Load(); got != 0 {
		t.Fatalf("supervisor calls = %d, want 0", got)
	}
}

func TestVerifiedLifecycleNotificationsUpdateStatusAndStream(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := &task{
		id:              "task-1",
		sessionGroupID:  group.SessionGroupID,
		workspaceID:     group.WorkspaceID,
		clientMessageID: "client-1",
		state:           domain.TaskStateStarting,
		phase:           startPhaseThreadCalling,
		connection:      connection,
		createdAt:       time.Now(),
		subscribers:     map[uint64]*taskSubscriber{},
	}
	service.mu.Lock()
	service.tasks[task.id] = task
	service.sessions[group.SessionGroupID].activeTaskID = task.id
	service.appendEventLocked(task, domain.TaskLifecycleEvent{
		LifecycleEvent: domain.TaskLifecycleEventTaskStarted,
		State:          domain.TaskStateStarting,
	})
	service.mu.Unlock()

	service.handleNotification(appserver.Notification{
		Method:         testappserver.MethodThreadStarted,
		Params:         mustRawMessage(t, testappserver.ThreadStartedParams("thread-1")),
		TaskID:         task.id,
		SessionGroupID: group.SessionGroupID,
	}, connection)
	service.handleNotification(appserver.Notification{
		Method:         testappserver.MethodTurnStarted,
		Params:         mustRawMessage(t, testappserver.TurnStartedParams("thread-1", "turn-1")),
		TaskID:         task.id,
		SessionGroupID: group.SessionGroupID,
	}, connection)

	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: task.id},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.State != domain.TaskStateRunning || status.ThreadID != "thread-1" || status.TurnID != "turn-1" || status.LastEventID != 3 {
		t.Fatalf("status after verified lifecycle notifications = %#v", status)
	}
	events := readStreamEvents(t, service, task.id, 3)
	threadEvent, ok := events[1].Payload.(domain.TaskLifecycleEvent)
	if !ok || threadEvent.LifecycleEvent != domain.TaskLifecycleEventThreadStarted || events[1].ThreadID != "thread-1" {
		t.Fatalf("thread event = %#v", events[1])
	}
	turnEvent, ok := events[2].Payload.(domain.TaskLifecycleEvent)
	if !ok || turnEvent.LifecycleEvent != domain.TaskLifecycleEventTurnStarted || events[2].TurnID != "turn-1" {
		t.Fatalf("turn event = %#v", events[2])
	}
}

func TestNotificationMappingPublishesRepresentativeTaskEvents(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := addManualRunningTask(t, service, group, connection, "task-1", "thread-1", "turn-1")

	service.handleNotification(appserver.Notification{
		Method: "turn/plan/updated",
		Params: mustRawMessage(t, map[string]any{
			"threadId":    "thread-1",
			"turnId":      "turn-1",
			"explanation": "working",
			"plan":        []map[string]any{{"step": "edit", "status": "in_progress"}},
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/started",
		Params: mustRawMessage(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"item":     map[string]any{"id": "cmd-1", "type": "commandExecution"},
			"command":  []any{"echo", "hello"},
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/commandExecution/outputDelta",
		Params: mustRawMessage(t, map[string]any{
			"itemId": "cmd-1",
			"delta":  "hello\n",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/agentMessage/delta",
		Params: mustRawMessage(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"itemId":   "agent-1",
			"delta":    "partial",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/completed",
		Params: mustRawMessage(t, map[string]any{
			"itemId":  "agent-1",
			"item":    map[string]any{"type": "agentMessage"},
			"message": "final answer",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "turn/diff/updated",
		Params: mustRawMessage(t, map[string]any{
			"threadId":    "thread-1",
			"turnId":      "turn-1",
			"diffSummary": "1 file changed",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/mcpToolCall/progress",
		Params: mustRawMessage(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"itemId":   "tool-1",
			"toolName": "lookup",
			"progress": "running",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "model/rerouted",
		Params: mustRawMessage(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"message":  "using fallback model",
		}),
	}, connection)

	events := readStreamEvents(t, service, task.id, 9)
	if _, ok := events[1].Payload.(domain.PlanUpdatedEvent); !ok {
		t.Fatalf("event[1] = %T, want PlanUpdatedEvent", events[1].Payload)
	}
	commandStarted, ok := events[2].Payload.(domain.CommandStartedEvent)
	if !ok || commandStarted.ItemID != "cmd-1" || commandStarted.CommandDisplay != "echo hello" {
		t.Fatalf("command event = %#v", events[2])
	}
	if _, ok := events[3].Payload.(domain.CommandOutputDeltaEvent); !ok {
		t.Fatalf("event[3] = %T, want CommandOutputDeltaEvent", events[3].Payload)
	}
	if _, ok := events[4].Payload.(domain.AssistantDeltaEvent); !ok {
		t.Fatalf("event[4] = %T, want AssistantDeltaEvent", events[4].Payload)
	}
	if _, ok := events[5].Payload.(domain.AssistantMessageCompletedEvent); !ok {
		t.Fatalf("event[5] = %T, want AssistantMessageCompletedEvent", events[5].Payload)
	}
	if _, ok := events[6].Payload.(domain.TurnDiffUpdatedEvent); !ok {
		t.Fatalf("event[6] = %T, want TurnDiffUpdatedEvent", events[6].Payload)
	}
	if _, ok := events[7].Payload.(domain.ToolProgressEvent); !ok {
		t.Fatalf("event[7] = %T, want ToolProgressEvent", events[7].Payload)
	}
	warning, ok := events[8].Payload.(domain.GatewayWarningEvent)
	if !ok || warning.Code != gatewayWarningCodeModelRerouted {
		t.Fatalf("event[8] = %#v, want model reroute warning", events[8])
	}
}

func TestTurnAndWarningNotificationsUseScopedRedactor(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := addManualRunningTask(t, service, group, connection, "task-1", "thread-1", "turn-1")
	task.sensitive.Add("scoped-secret-value")

	service.handleNotification(appserver.Notification{
		Method: "turn/plan/updated",
		Params: mustRawMessage(t, map[string]any{
			"threadId":    "thread-1",
			"turnId":      "turn-1",
			"explanation": "working scoped-secret-value",
			"plan": []map[string]any{{
				"step":   "edit scoped-secret-value",
				"status": "status scoped-secret-value",
			}},
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "turn/diff/updated",
		Params: mustRawMessage(t, map[string]any{
			"threadId":     "thread-1",
			"turnId":       "turn-1",
			"diffSummary":  "1 file changed scoped-secret-value",
			"diffUnified":  "+ scoped-secret-value",
			"unifiedExtra": "ignored",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "model/rerouted",
		Params: mustRawMessage(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"message":  "using fallback model scoped-secret-value",
		}),
	}, connection)

	events := readStreamEvents(t, service, task.id, 4)
	plan, ok := events[1].Payload.(domain.PlanUpdatedEvent)
	if !ok {
		t.Fatalf("event[1] = %T, want PlanUpdatedEvent", events[1].Payload)
	}
	if strings.Contains(plan.Explanation, "scoped-secret-value") ||
		strings.Contains(plan.Steps[0].Step, "scoped-secret-value") ||
		strings.Contains(plan.Steps[0].Status, "scoped-secret-value") ||
		!strings.Contains(plan.Explanation, redact.SensitiveValueMarker) ||
		!strings.Contains(plan.Steps[0].Step, redact.SensitiveValueMarker) ||
		!strings.Contains(plan.Steps[0].Status, redact.SensitiveValueMarker) {
		t.Fatalf("plan event was not scoped-redacted: %#v", plan)
	}
	diff, ok := events[2].Payload.(domain.TurnDiffUpdatedEvent)
	if !ok {
		t.Fatalf("event[2] = %T, want TurnDiffUpdatedEvent", events[2].Payload)
	}
	if strings.Contains(diff.DiffSummary, "scoped-secret-value") ||
		strings.Contains(diff.DiffUnified, "scoped-secret-value") ||
		!strings.Contains(diff.DiffSummary, redact.SensitiveValueMarker) ||
		!strings.Contains(diff.DiffUnified, redact.SensitiveValueMarker) {
		t.Fatalf("diff event was not scoped-redacted: %#v", diff)
	}
	warning, ok := events[3].Payload.(domain.GatewayWarningEvent)
	if !ok || warning.Code != gatewayWarningCodeModelRerouted {
		t.Fatalf("event[3] = %#v, want model reroute warning", events[3])
	}
	if strings.Contains(warning.Message, "scoped-secret-value") ||
		!strings.Contains(warning.Message, redact.SensitiveValueMarker) {
		t.Fatalf("warning event was not scoped-redacted: %#v", warning)
	}
}

func TestTurnAndWarningNotificationsRedactScopedSecretsBeforeTruncation(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := addManualRunningTask(t, service, group, connection, "task-1", "thread-1", "turn-1")
	secret := "boundary-secret-value-that-must-not-leak"
	task.sensitive.Add(secret)

	boundaryValue := func(limit int) string {
		return strings.Repeat("x", limit-12) + secret + " after"
	}
	errorBoundary := boundaryValue(domain.MaxOutboundErrorDisplayMessageBytes)
	diffBoundary := boundaryValue(domain.MaxOutboundDiffDisplayBytes)
	sourceBoundary := boundaryValue(domain.MaxSourceLabelBytes)

	service.handleNotification(appserver.Notification{
		Method: "turn/plan/updated",
		Params: mustRawMessage(t, map[string]any{
			"threadId":    "thread-1",
			"turnId":      "turn-1",
			"explanation": errorBoundary,
			"plan": []map[string]any{{
				"step":   "safe step",
				"status": sourceBoundary,
			}},
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "turn/diff/updated",
		Params: mustRawMessage(t, map[string]any{
			"threadId":    "thread-1",
			"turnId":      "turn-1",
			"diffSummary": diffBoundary,
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "model/rerouted",
		Params: mustRawMessage(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"message":  errorBoundary,
		}),
	}, connection)

	events := readStreamEvents(t, service, task.id, 4)
	plan, ok := events[1].Payload.(domain.PlanUpdatedEvent)
	if !ok {
		t.Fatalf("event[1] = %T, want PlanUpdatedEvent", events[1].Payload)
	}
	diff, ok := events[2].Payload.(domain.TurnDiffUpdatedEvent)
	if !ok {
		t.Fatalf("event[2] = %T, want TurnDiffUpdatedEvent", events[2].Payload)
	}
	warning, ok := events[3].Payload.(domain.GatewayWarningEvent)
	if !ok || warning.Code != gatewayWarningCodeModelRerouted {
		t.Fatalf("event[3] = %#v, want model reroute warning", events[3])
	}

	assertNoBoundarySecretFragment := func(field string, value string, limit int) {
		t.Helper()
		if len(value) > limit {
			t.Fatalf("%s length = %d, want <= %d", field, len(value), limit)
		}
		for _, fragment := range []string{secret, secret[:12], secret[len(secret)-12:]} {
			if strings.Contains(value, fragment) {
				t.Fatalf("%s leaked sensitive fragment %q in %q", field, fragment, value)
			}
		}
	}
	assertNoBoundarySecretFragment("plan explanation", plan.Explanation, domain.MaxOutboundErrorDisplayMessageBytes)
	assertNoBoundarySecretFragment("plan status", plan.Steps[0].Status, domain.MaxSourceLabelBytes)
	assertNoBoundarySecretFragment("diff summary", diff.DiffSummary, domain.MaxOutboundDiffDisplayBytes)
	assertNoBoundarySecretFragment("warning message", warning.Message, domain.MaxOutboundErrorDisplayMessageBytes)
	if !diff.Truncated {
		t.Fatalf("diff summary truncated = false, want true")
	}
}

func TestContentNotificationWithoutCorrelationDoesNotAttachToSingleActiveTask(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := addManualRunningTask(t, service, group, connection, "task-1", "thread-1", "turn-1")

	service.handleNotification(appserver.Notification{
		Method: "item/agentMessage/delta",
		Params: mustRawMessage(t, map[string]any{
			"itemId": "unbound-agent",
			"delta":  "must not attach by active-task fallback",
		}),
	}, connection)

	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: task.id},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.LastEventID != 1 {
		t.Fatalf("status after uncorrelated content notification = %#v", status)
	}
}

func TestModelWarningWithoutTurnCorrelationDoesNotAttachToActiveTask(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := addManualRunningTask(t, service, group, connection, "task-1", "thread-1", "turn-1")

	service.handleNotification(appserver.Notification{
		Method: "model/rerouted",
		Params: mustRawMessage(t, map[string]any{
			"message": "must not attach by active-task fallback",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method:         "model/verification",
		Params:         mustRawMessage(t, map[string]any{"message": "missing turn id"}),
		TaskID:         task.id,
		SessionGroupID: group.SessionGroupID,
	}, connection)

	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: task.id},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.LastEventID != 1 {
		t.Fatalf("status after uncorrelated model warning = %#v", status)
	}
}

func TestFileChangeStructuredPathLabelsUseWorkspaceRelativeSanitizer(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := addManualRunningTask(t, service, group, connection, "task-1", "thread-1", "turn-1")
	insidePath := filepath.Join(group.CanonicalCWD, "nested", "file.txt")
	outsidePath := filepath.Join(t.TempDir(), "private-note.txt")

	service.handleNotification(appserver.Notification{
		Method: "item/started",
		Params: mustRawMessage(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"item": map[string]any{
				"id":   "file-1",
				"type": "fileChange",
			},
			"path":        insidePath,
			"diffSummary": "1 file changed",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/fileChange/patchUpdated",
		Params: mustRawMessage(t, map[string]any{
			"itemId":      "file-1",
			"path":        outsidePath,
			"diffSummary": "1 file changed",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/fileChange/patchUpdated",
		Params: mustRawMessage(t, map[string]any{
			"itemId":      "file-1",
			"path":        filepath.Join("nested", "relative.txt"),
			"diffSummary": "1 file changed",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/fileChange/patchUpdated",
		Params: mustRawMessage(t, map[string]any{
			"itemId":      "file-1",
			"path":        filepath.Join("..", "outside", "private-note.txt"),
			"diffSummary": "1 file changed",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/fileChange/patchUpdated",
		Params: mustRawMessage(t, map[string]any{
			"itemId":      "file-1",
			"path":        `..\outside\private-note.txt`,
			"diffSummary": "1 file changed",
		}),
	}, connection)

	events := readStreamEvents(t, service, task.id, 6)
	inside, ok := events[1].Payload.(domain.FileDiffUpdatedEvent)
	if !ok {
		t.Fatalf("event[1] = %T, want FileDiffUpdatedEvent", events[1].Payload)
	}
	if inside.FileLabel != "nested/file.txt" {
		t.Fatalf("inside file label = %q, want %q", inside.FileLabel, "nested/file.txt")
	}
	outside, ok := events[2].Payload.(domain.FileDiffUpdatedEvent)
	if !ok {
		t.Fatalf("event[2] = %T, want FileDiffUpdatedEvent", events[2].Payload)
	}
	if outside.FileLabel != redact.PathMarker {
		t.Fatalf("outside file label = %q, want %q", outside.FileLabel, redact.PathMarker)
	}
	insideRelative, ok := events[3].Payload.(domain.FileDiffUpdatedEvent)
	if !ok {
		t.Fatalf("event[3] = %T, want FileDiffUpdatedEvent", events[3].Payload)
	}
	if insideRelative.FileLabel != "nested/relative.txt" {
		t.Fatalf("inside relative file label = %q, want %q", insideRelative.FileLabel, "nested/relative.txt")
	}
	outsideRelative, ok := events[4].Payload.(domain.FileDiffUpdatedEvent)
	if !ok {
		t.Fatalf("event[4] = %T, want FileDiffUpdatedEvent", events[4].Payload)
	}
	if outsideRelative.FileLabel != redact.PathMarker {
		t.Fatalf("outside relative file label = %q, want %q", outsideRelative.FileLabel, redact.PathMarker)
	}
	outsideWindowsRelative, ok := events[5].Payload.(domain.FileDiffUpdatedEvent)
	if !ok {
		t.Fatalf("event[5] = %T, want FileDiffUpdatedEvent", events[5].Payload)
	}
	if outsideWindowsRelative.FileLabel != redact.PathMarker {
		t.Fatalf("outside windows relative file label = %q, want %q", outsideWindowsRelative.FileLabel, redact.PathMarker)
	}
}

func TestCommandWorkspaceLabelsUseWorkspaceRelativeSanitizer(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := addManualRunningTask(t, service, group, connection, "task-1", "thread-1", "turn-1")

	service.handleNotification(appserver.Notification{
		Method: "item/started",
		Params: mustRawMessage(t, map[string]any{
			"threadId":       "thread-1",
			"turnId":         "turn-1",
			"item":           map[string]any{"id": "cmd-1", "type": "commandExecution"},
			"command":        []any{"echo", "hello"},
			"workspaceLabel": filepath.Join("nested", "workspace"),
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/started",
		Params: mustRawMessage(t, map[string]any{
			"threadId":       "thread-1",
			"turnId":         "turn-1",
			"item":           map[string]any{"id": "cmd-2", "type": "commandExecution"},
			"command":        []any{"echo", "hello"},
			"workspaceLabel": filepath.Join("..", "outside", "private-note.txt"),
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/started",
		Params: mustRawMessage(t, map[string]any{
			"threadId":       "thread-1",
			"turnId":         "turn-1",
			"item":           map[string]any{"id": "cmd-3", "type": "commandExecution"},
			"command":        []any{"echo", "hello"},
			"workspaceLabel": `..\outside\private-note.txt`,
		}),
	}, connection)

	events := readStreamEvents(t, service, task.id, 4)
	inside, ok := events[1].Payload.(domain.CommandStartedEvent)
	if !ok {
		t.Fatalf("event[1] = %T, want CommandStartedEvent", events[1].Payload)
	}
	if inside.WorkspaceLabel != "nested/workspace" {
		t.Fatalf("inside workspace label = %q, want %q", inside.WorkspaceLabel, "nested/workspace")
	}
	outsideRelative, ok := events[2].Payload.(domain.CommandStartedEvent)
	if !ok {
		t.Fatalf("event[2] = %T, want CommandStartedEvent", events[2].Payload)
	}
	if outsideRelative.WorkspaceLabel != redact.PathMarker {
		t.Fatalf("outside relative workspace label = %q, want %q", outsideRelative.WorkspaceLabel, redact.PathMarker)
	}
	outsideWindowsRelative, ok := events[3].Payload.(domain.CommandStartedEvent)
	if !ok {
		t.Fatalf("event[3] = %T, want CommandStartedEvent", events[3].Payload)
	}
	if outsideWindowsRelative.WorkspaceLabel != redact.PathMarker {
		t.Fatalf("outside windows relative workspace label = %q, want %q", outsideWindowsRelative.WorkspaceLabel, redact.PathMarker)
	}
}

func TestStructuredDropsAndUnknownFallbackDoNotExposeRawPayloads(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := addManualRunningTask(t, service, group, connection, "task-1", "thread-1", "turn-1")

	service.handleNotification(appserver.Notification{
		Method: "account/updated",
		Params: mustRawMessage(t, map[string]any{"authMode": "chatgpt", "planType": "plus"}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "fs/changed",
		Params: mustRawMessage(t, map[string]any{"changedPaths": []string{`C:\Users\me\private-note.txt`}}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "thread/status/changed",
		Params: mustRawMessage(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"path":     `C:\Users\me\private-note.txt`,
		}),
	}, connection)
	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: task.id},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.LastEventID != 1 {
		t.Fatalf("structured drop status = %#v", status)
	}
	diagnostics := service.connectionDiagnosticsSnapshot()
	if len(diagnostics) != 3 {
		t.Fatalf("structured drop diagnostics = %#v, want account, fs, and diagnostic-only diagnostics", diagnostics)
	}
	if diagnostics[0].Code != gatewayDiagnosticCodeStructuredDrop || diagnostics[0].Method != "account/updated" ||
		diagnostics[1].Code != gatewayDiagnosticCodeFilesystemChanged || diagnostics[1].Method != "fs/changed" ||
		diagnostics[2].Code != gatewayDiagnosticCodeDiagnosticOnly || diagnostics[2].Method != "thread/status/changed" {
		t.Fatalf("structured drop diagnostics = %#v", diagnostics)
	}
	rawDiagnostics, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatalf("json.Marshal(diagnostics) error = %v", err)
	}
	for _, forbidden := range []string{"chatgpt", "plus", "private-note.txt"} {
		if strings.Contains(string(rawDiagnostics), forbidden) {
			t.Fatalf("structured drop diagnostics leaked raw payload fragment %q: %s", forbidden, rawDiagnostics)
		}
	}

	service.handleNotification(appserver.Notification{
		Method: "vendor/custom",
		Params: mustRawMessage(t, map[string]any{
			"threadId":   "thread-1",
			"turnId":     "turn-1",
			"credential": "redaction-sentinel-credential",
			"path":       `C:\Users\me\private-note.txt`,
		}),
	}, connection)
	events := readStreamEvents(t, service, task.id, 2)
	warning, ok := events[1].Payload.(domain.GatewayWarningEvent)
	if !ok {
		t.Fatalf("event[1] = %T, want GatewayWarningEvent", events[1].Payload)
	}
	if warning.Code != gatewayWarningCodeUnknownNotification || warning.RequestType != "vendor/custom" || warning.LimitReason != "payload_suppressed" {
		t.Fatalf("unknown notification warning = %#v, want typed suppressed-payload warning", warning)
	}
	raw, err := json.Marshal(events[1])
	if err != nil {
		t.Fatalf("json.Marshal(event) error = %v", err)
	}
	if strings.Contains(string(raw), "redaction-sentinel-credential") || strings.Contains(string(raw), "private-note.txt") {
		t.Fatalf("unknown notification warning leaked raw payload: %s", raw)
	}
}

func TestScopedStreamingRedactionCarriesAssistantAndCommandDeltas(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := addManualRunningTask(t, service, group, connection, "task-1", "thread-1", "turn-1")
	task.sensitive.Add("split-secret-value")

	service.handleNotification(appserver.Notification{
		Method: "item/agentMessage/delta",
		Params: mustRawMessage(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"itemId":   "agent-1",
			"delta":    "split-",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/agentMessage/delta",
		Params: mustRawMessage(t, map[string]any{
			"itemId": "agent-1",
			"delta":  "secret-value",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/started",
		Params: mustRawMessage(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"item":     map[string]any{"id": "cmd-1", "type": "commandExecution"},
			"command":  []any{"echo", "safe"},
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/commandExecution/outputDelta",
		Params: mustRawMessage(t, map[string]any{
			"itemId": "cmd-1",
			"delta":  "split-",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: "item/commandExecution/outputDelta",
		Params: mustRawMessage(t, map[string]any{
			"itemId": "cmd-1",
			"delta":  "secret-value",
		}),
	}, connection)

	events := readStreamEvents(t, service, task.id, 4)
	assistant, ok := events[1].Payload.(domain.AssistantDeltaEvent)
	if !ok {
		t.Fatalf("event[1] = %T, want AssistantDeltaEvent", events[1].Payload)
	}
	if assistant.TextDelta != redact.SensitiveValueMarker {
		t.Fatalf("assistant delta = %q, want sensitive marker", assistant.TextDelta)
	}
	command, ok := events[3].Payload.(domain.CommandOutputDeltaEvent)
	if !ok {
		t.Fatalf("event[3] = %T, want CommandOutputDeltaEvent", events[3].Payload)
	}
	if command.Delta != redact.SensitiveValueMarker {
		t.Fatalf("command delta = %q, want sensitive marker", command.Delta)
	}
	for _, event := range events {
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("json.Marshal(event) error = %v", err)
		}
		if strings.Contains(string(raw), "split-secret-value") {
			t.Fatalf("event leaked registered secret: %s", raw)
		}
	}
}

func TestUnknownNotificationWarningSuppressesConnectionScopedPayload(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	registry := redact.NewRegistry()
	registry.Add("redaction-sentinel-credential")
	harness := testappserver.New(t)
	connection := appserver.NewConnection(harness.Stdin(), harness.Stdout(), group, appserver.ConnectionOptions{
		SensitiveRegistry: registry,
	})
	defer connection.Close()
	task := addManualRunningTask(t, service, group, connection, "task-1", "thread-1", "turn-1")

	service.handleNotification(appserver.Notification{
		Method: "vendor/custom",
		Params: mustRawMessage(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"message":  "echo redaction-sentinel-credential",
		}),
	}, connection)

	events := readStreamEvents(t, service, task.id, 2)
	warning, ok := events[1].Payload.(domain.GatewayWarningEvent)
	if !ok {
		t.Fatalf("event[1] = %T, want GatewayWarningEvent", events[1].Payload)
	}
	if warning.Code != gatewayWarningCodeUnknownNotification || warning.RequestType != "vendor/custom" || warning.LimitReason != "payload_suppressed" {
		t.Fatalf("unknown notification warning = %#v, want typed suppressed-payload warning", warning)
	}
	raw, err := json.Marshal(events[1])
	if err != nil {
		t.Fatalf("json.Marshal(event) error = %v", err)
	}
	if strings.Contains(string(raw), "redaction-sentinel-credential") || strings.Contains(string(raw), redact.SensitiveValueMarker) {
		t.Fatalf("unknown notification warning leaked or encoded payload detail: %s", raw)
	}
}

func TestUncorrelatedNotificationsRecordRedactedDiagnosticsOnly(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	registry := redact.NewRegistry()
	registry.Add("connection-redaction-method")
	harness := testappserver.New(t)
	connection := appserver.NewConnection(harness.Stdin(), harness.Stdout(), group, appserver.ConnectionOptions{
		SensitiveRegistry: registry,
	})
	defer connection.Close()

	service.handleNotification(appserver.Notification{
		Method:         "error",
		SessionGroupID: group.SessionGroupID,
		Params: mustRawMessage(t, map[string]any{
			"message": "failed with redaction-sentinel-credential at C:/Users/me/.codex/local-settings.toml",
		}),
	}, connection)
	service.handleNotification(appserver.Notification{
		Method:         "vendor/connection-redaction-method",
		SessionGroupID: group.SessionGroupID,
		Params: mustRawMessage(t, map[string]any{
			"credential": "redaction-sentinel-credential",
			"path":       `C:\Users\me\.codex\local-settings.toml`,
		}),
	}, connection)

	diagnostics := service.connectionDiagnosticsSnapshot()
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v, want warning and unknown diagnostics", diagnostics)
	}
	if diagnostics[0].Code != gatewayWarningCodeAppServerError || diagnostics[0].Method != "error" ||
		diagnostics[1].Code != gatewayDiagnosticCodeUnknownEvent {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if strings.Contains(diagnostics[1].Method, "connection-redaction-method") ||
		!strings.Contains(diagnostics[1].Method, redact.SensitiveValueMarker) {
		t.Fatalf("unknown diagnostic method = %q, want connection registry redaction", diagnostics[1].Method)
	}
	raw, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatalf("json.Marshal(diagnostics) error = %v", err)
	}
	for _, forbidden := range []string{"connection-redaction-method", "redaction-sentinel-credential", "local-settings.toml", "credential"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("diagnostics leaked raw payload fragment %q: %s", forbidden, raw)
		}
	}
}

func TestPendingCommandApprovalRespondsAndRetriesIdempotently(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendCommandApprovalRequest(101, "thread-1", "turn-1", "item-command"),
			testappserver.ExpectResponseID(101, testappserver.WithResult(map[string]any{"decision": "accept"})),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Approve command"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 1)
	pendingRequest := status.ActivePendingRequests[0]
	if pendingRequest.PendingType != domain.PendingTypeCommandApproval || status.State != domain.TaskStateWaitingForPendingRequest {
		t.Fatalf("pending status = %#v", status)
	}

	command := domain.RespondPendingRequestCommand{
		TaskID:           response.TaskID,
		PendingRequestID: pendingRequest.PendingRequestID,
		ClientResponseID: "response-1",
		Response: domain.PendingResponse{
			Approval: &domain.ApprovalPendingResponse{DecisionID: "decision-accept"},
		},
	}
	first, err := service.RespondPendingRequest(context.Background(), command)
	if err != nil {
		t.Fatalf("RespondPendingRequest() error = %v", err)
	}
	second, err := service.RespondPendingRequest(context.Background(), command)
	if err != nil {
		t.Fatalf("RespondPendingRequest() retry error = %v", err)
	}
	if !first.Accepted || first.AlreadyApplied || !second.Accepted || !second.AlreadyApplied ||
		first.ResolvedEventID == 0 || first.ResolvedEventID != second.ResolvedEventID {
		t.Fatalf("responses = %#v then %#v", first, second)
	}
	harness.RequireDone(t)
}

func TestServerRequestResolvedClearsInFlightPendingResponse(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	serverRequestID := " 101 "
	jsonrpcID := json.RawMessage(`" 101 "`)
	record := &pending.Record{
		Method:             pending.MethodCommandApproval,
		AppServerRequestID: pending.NormalizeServerRequestID("", jsonrpcID),
		JSONRPCID:          jsonrpcID,
		Pending: domain.PendingRequest{
			TaskID:          "task-1",
			PendingType:     domain.PendingTypeCommandApproval,
			CreatedAtUnixMS: 123,
			Display:         domain.CommandApprovalDisplay{CommandDisplay: "echo hello"},
		},
		Responses: map[string]*pending.ResponseEntry{},
	}
	command := domain.RespondPendingRequestCommand{
		TaskID:           "task-1",
		PendingRequestID: "pending-1",
		ClientResponseID: "response-1",
		Response: domain.PendingResponse{
			Approval: &domain.ApprovalPendingResponse{DecisionID: "decision-accept"},
		},
	}
	var entry *pending.ResponseEntry

	service.mu.Lock()
	task := &task{
		id:             "task-1",
		sessionGroupID: group.SessionGroupID,
		workspaceID:    group.WorkspaceID,
		threadID:       "thread-1",
		turnID:         "turn-1",
		state:          domain.TaskStateWaitingForPendingRequest,
		connection:     connection,
		createdAt:      service.now(),
		pending:        pending.NewManager(),
		fileDiffs:      map[string]domain.FileDiffUpdatedEvent{},
		streams:        map[taskStreamKey]*redact.Stream{},
		subscribers:    map[uint64]*taskSubscriber{},
	}
	service.tasks[task.id] = task
	task.pending.Add(record)
	command.PendingRequestID = record.Pending.PendingRequestID
	fingerprint, err := domain.PendingResponseFingerprintV1SHA256Hex(
		command.TaskID,
		command.PendingRequestID,
		domain.PendingTypeCommandApproval,
		command.Response,
	)
	if err != nil {
		service.mu.Unlock()
		t.Fatal(err)
	}
	entry = &pending.ResponseEntry{
		ClientResponseID: command.ClientResponseID,
		Fingerprint:      fingerprint,
		State:            pending.ResponseStateResponding,
		Done:             make(chan struct{}),
	}
	record.Responses[command.ClientResponseID] = entry
	record.InFlightClientResponseID = command.ClientResponseID
	service.appendEventLocked(task, domain.PendingRequestCreatedEvent{
		PendingRequestID: record.Pending.PendingRequestID,
		PendingType:      record.Pending.PendingType,
		Display:          record.Pending.Display,
	})
	service.mu.Unlock()

	service.handleServerRequestResolved(appserver.Notification{
		Method:                       "serverRequest/resolved",
		Params:                       mustRawMessage(t, map[string]any{"requestId": serverRequestID}),
		ServerRequestResolvedChecked: true,
		ServerRequestResolvedMatched: true,
	}, connection)

	select {
	case <-entry.Done:
	default:
		t.Fatal("in-flight response entry was not completed by serverRequest/resolved")
	}
	response, err := service.completePendingResponse(
		command.TaskID,
		command.PendingRequestID,
		command.ClientResponseID,
		entry,
		domain.PendingResolutionAccepted,
		appserver.ErrDispatcherClosed,
	)
	if err != nil {
		t.Fatalf("completePendingResponse() error = %v", err)
	}
	if !response.Accepted || !response.AlreadyApplied || response.ResolvedEventID == 0 {
		t.Fatalf("completePendingResponse() response = %#v, want already-applied accepted response", response)
	}
	retry, err := service.RespondPendingRequest(context.Background(), command)
	if err != nil {
		t.Fatalf("RespondPendingRequest() retry error = %v", err)
	}
	if !retry.Accepted || !retry.AlreadyApplied || retry.ResolvedEventID != response.ResolvedEventID {
		t.Fatalf("retry response = %#v, want same already-applied cleared event as %#v", retry, response)
	}

	status := waitPendingCount(t, service, command.TaskID, 0)
	if status.State != domain.TaskStateRunning {
		t.Fatalf("status = %#v, want running with no active pending requests", status)
	}
	events := readStreamEvents(t, service, command.TaskID, int(status.LastEventID))
	clearedEvents := 0
	for _, event := range events {
		resolved, ok := event.Payload.(domain.PendingRequestResolvedEvent)
		if !ok {
			continue
		}
		if resolved.Resolution == domain.PendingResolutionFailed {
			t.Fatalf("resolved event = %#v, did not expect failed resolution", resolved)
		}
		if resolved.PendingRequestID == command.PendingRequestID && resolved.Resolution == domain.PendingResolutionCleared {
			clearedEvents++
		}
	}
	if clearedEvents != 1 {
		t.Fatalf("cleared resolved event count = %d, want 1 in events %#v", clearedEvents, events)
	}
}

func TestClearPendingDoesNotDuplicateLateInFlightResponseResolution(t *testing.T) {
	tests := []struct {
		name     string
		writeErr error
	}{
		{
			name:     "late write error",
			writeErr: appserver.ErrDispatcherClosed,
		},
		{
			name: "late write success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			group := testSessionGroup(t, "sg-1", "ws-1")
			service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
			if err != nil {
				t.Fatal(err)
			}
			connection := &appserver.Connection{}
			record := &pending.Record{
				Method:             pending.MethodCommandApproval,
				AppServerRequestID: "server-request-1",
				JSONRPCID:          json.RawMessage(`101`),
				Pending: domain.PendingRequest{
					TaskID:          "task-1",
					PendingType:     domain.PendingTypeCommandApproval,
					CreatedAtUnixMS: 123,
					Display:         domain.CommandApprovalDisplay{CommandDisplay: "echo hello"},
				},
				Responses: map[string]*pending.ResponseEntry{},
			}
			command := domain.RespondPendingRequestCommand{
				TaskID:           "task-1",
				PendingRequestID: "pending-1",
				ClientResponseID: "response-1",
				Response: domain.PendingResponse{
					Approval: &domain.ApprovalPendingResponse{DecisionID: "decision-accept"},
				},
			}
			entry := &pending.ResponseEntry{
				ClientResponseID: command.ClientResponseID,
				State:            pending.ResponseStateResponding,
				Done:             make(chan struct{}),
			}

			service.mu.Lock()
			task := &task{
				id:             "task-1",
				sessionGroupID: group.SessionGroupID,
				workspaceID:    group.WorkspaceID,
				threadID:       "thread-1",
				turnID:         "turn-1",
				state:          domain.TaskStateWaitingForPendingRequest,
				connection:     connection,
				createdAt:      service.now(),
				pending:        pending.NewManager(),
				fileDiffs:      map[string]domain.FileDiffUpdatedEvent{},
				streams:        map[taskStreamKey]*redact.Stream{},
				subscribers:    map[uint64]*taskSubscriber{},
			}
			service.tasks[task.id] = task
			task.pending.Add(record)
			command.PendingRequestID = record.Pending.PendingRequestID
			record.Responses[command.ClientResponseID] = entry
			record.InFlightClientResponseID = command.ClientResponseID
			service.appendEventLocked(task, domain.PendingRequestCreatedEvent{
				PendingRequestID: record.Pending.PendingRequestID,
				PendingType:      record.Pending.PendingType,
				Display:          record.Pending.Display,
			})
			payloads := service.clearPendingLocked(task, domain.PendingResolutionCleared)
			publications := service.appendPublicationsLocked(task, payloads, false)
			service.mu.Unlock()

			if len(publications) != 1 {
				t.Fatalf("clear pending publications = %d, want 1", len(publications))
			}
			resolvedEventID := publications[0].event.EventID
			response, err := service.completePendingResponse(
				command.TaskID,
				command.PendingRequestID,
				command.ClientResponseID,
				entry,
				domain.PendingResolutionAccepted,
				tt.writeErr,
			)
			if err != nil {
				t.Fatalf("completePendingResponse() error = %v", err)
			}
			if !response.Accepted || !response.AlreadyApplied || response.ResolvedEventID != resolvedEventID {
				t.Fatalf("completePendingResponse() response = %#v, want already-applied event %d", response, resolvedEventID)
			}
			select {
			case <-entry.Done:
			default:
				t.Fatal("in-flight response entry was not completed")
			}

			status := waitPendingCount(t, service, command.TaskID, 0)
			if status.State != domain.TaskStateRunning {
				t.Fatalf("status = %#v, want running with no active pending requests", status)
			}
			events := readStreamEvents(t, service, command.TaskID, int(status.LastEventID))
			resolvedEvents := 0
			for _, event := range events {
				resolved, ok := event.Payload.(domain.PendingRequestResolvedEvent)
				if !ok || resolved.PendingRequestID != command.PendingRequestID {
					continue
				}
				resolvedEvents++
				if resolved.Resolution != domain.PendingResolutionCleared {
					t.Fatalf("resolved event = %#v, want only cleared resolution", resolved)
				}
			}
			if resolvedEvents != 1 {
				t.Fatalf("resolved event count = %d, want 1 in events %#v", resolvedEvents, events)
			}
		})
	}
}

func TestPendingApprovalSecurityHostLabelsAreOpaqueInStatusAndReplay(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendRequest(101, testappserver.MethodCommandApprovalRequest, map[string]any{
				"threadId": "thread-1",
				"turnId":   "turn-1",
				"item": map[string]any{
					"id":   "item-command",
					"type": "commandExecution",
				},
				"command": []string{"echo", "hello"},
				"networkApprovalContext": map[string]any{
					"origin":   "https://10.0.0.5:8443/private",
					"protocol": "https",
				},
				"proposedNetworkPolicyAmendments": []map[string]any{
					{
						"host":   "db.internal.local",
						"action": "allow",
					},
				},
			}),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Approve command"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 1)
	display, ok := status.ActivePendingRequests[0].Display.(domain.CommandApprovalDisplay)
	if !ok {
		t.Fatalf("pending display = %T, want CommandApprovalDisplay", status.ActivePendingRequests[0].Display)
	}
	if display.ApprovalSecurity == nil || display.ApprovalSecurity.NetworkContext == nil {
		t.Fatalf("approval security = %#v, want network context", display.ApprovalSecurity)
	}
	if display.ApprovalSecurity.NetworkContext.HostLabel != "network" {
		t.Fatalf("network host label = %q, want opaque network label", display.ApprovalSecurity.NetworkContext.HostLabel)
	}
	if len(display.ApprovalSecurity.NetworkPolicyAmendmentSummaries) != 1 ||
		display.ApprovalSecurity.NetworkPolicyAmendmentSummaries[0].HostLabel != "network" {
		t.Fatalf("network amendments = %#v, want opaque host label", display.ApprovalSecurity.NetworkPolicyAmendmentSummaries)
	}

	events := readStreamEvents(t, service, response.TaskID, int(response.LastEventID)+1)
	rawStatus, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal(status) error = %v", err)
	}
	rawReplay, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("json.Marshal(events) error = %v", err)
	}
	for _, forbidden := range []string{"10.0.0.5", "https://10.0.0.5:8443/private", "db.internal.local"} {
		if strings.Contains(string(rawStatus), forbidden) {
			t.Fatalf("status leaked raw network target %q: %s", forbidden, rawStatus)
		}
		if strings.Contains(string(rawReplay), forbidden) {
			t.Fatalf("replay leaked raw network target %q: %s", forbidden, rawReplay)
		}
	}
	harness.RequireDone(t)
}

func TestPendingResponseTypeMismatchIsInvalidArgument(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendCommandApprovalRequest(101, "thread-1", "turn-1", "item-command"),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Approve command"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 1)
	pendingID := status.ActivePendingRequests[0].PendingRequestID

	_, err = service.RespondPendingRequest(context.Background(), domain.RespondPendingRequestCommand{
		TaskID:           response.TaskID,
		PendingRequestID: pendingID,
		ClientResponseID: "response-1",
		Response: domain.PendingResponse{
			Permissions: &domain.PermissionsPendingResponse{Scope: domain.PermissionScopeTurn},
		},
	})
	assertGatewayCodeReason(t, err, domain.GatewayErrorCodeInvalidArgument, domain.ReasonResponseTypeMismatch)
	harness.RequireDone(t)
}

func TestPendingResponseRejectsFingerprintMismatchAndAlreadyResolved(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendCommandApprovalRequest(101, "thread-1", "turn-1", "item-command"),
			testappserver.ExpectResponseID(101, testappserver.WithResult(map[string]any{"decision": "decline"})),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Approve command"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 1)
	pendingID := status.ActivePendingRequests[0].PendingRequestID

	decline := domain.RespondPendingRequestCommand{
		TaskID:           response.TaskID,
		PendingRequestID: pendingID,
		ClientResponseID: "response-1",
		Response: domain.PendingResponse{
			Approval: &domain.ApprovalPendingResponse{DecisionID: "decision-decline"},
		},
	}
	changed := decline
	changed.Response.Approval = &domain.ApprovalPendingResponse{DecisionID: "decision-cancel"}
	if _, err := service.RespondPendingRequest(context.Background(), decline); err != nil {
		t.Fatalf("RespondPendingRequest() error = %v", err)
	}
	if _, err := service.RespondPendingRequest(context.Background(), changed); err == nil {
		t.Fatal("RespondPendingRequest() accepted changed retry")
	} else {
		assertGatewayReason(t, err, domain.ReasonPendingResponseFingerprintMismatch)
	}
	late := decline
	late.ClientResponseID = "response-2"
	if _, err := service.RespondPendingRequest(context.Background(), late); err == nil {
		t.Fatal("RespondPendingRequest() accepted late response")
	} else {
		assertGatewayReason(t, err, domain.ReasonPendingRequestAlreadyResolved)
	}
	harness.RequireDone(t)
}

func TestPendingPermissionsGrantSubsetWireShape(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendPermissionsRequest(103, "thread-1", "turn-1", "permissions-1"),
			testappserver.ExpectResponseID(103, testappserver.WithResult(map[string]any{
				"scope":            "session",
				"permissions":      map[string]any{"network": map[string]any{"enabled": true}},
				"strictAutoReview": true,
			})),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Grant permission"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 1)
	pendingRequest := status.ActivePendingRequests[0]
	display, ok := pendingRequest.Display.(domain.PermissionsApprovalDisplay)
	if !ok || len(display.RequestedPermissions) != 1 {
		t.Fatalf("permissions display = %#v", pendingRequest.Display)
	}

	_, err = service.RespondPendingRequest(context.Background(), domain.RespondPendingRequestCommand{
		TaskID:           response.TaskID,
		PendingRequestID: pendingRequest.PendingRequestID,
		ClientResponseID: "response-1",
		Response: domain.PendingResponse{
			Permissions: &domain.PermissionsPendingResponse{
				PermissionIDs:    []string{display.RequestedPermissions[0].PermissionID},
				Scope:            domain.PermissionScopeSession,
				StrictAutoReview: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("RespondPendingRequest() error = %v", err)
	}
	harness.RequireDone(t)
}

func TestPendingServerRequestResolvedClearsActivePending(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendCommandApprovalRequest(101, "thread-1", "turn-1", "item-command"),
			testappserver.SendNotification("serverRequest/resolved", map[string]any{"requestId": 101}),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Approve command"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	events := readStreamEvents(t, service, response.TaskID, 5)
	created, ok := events[3].Payload.(domain.PendingRequestCreatedEvent)
	if !ok {
		t.Fatalf("event[3] = %T, want PendingRequestCreatedEvent", events[3].Payload)
	}
	resolved, ok := events[4].Payload.(domain.PendingRequestResolvedEvent)
	if !ok || resolved.PendingRequestID != created.PendingRequestID || resolved.Resolution != domain.PendingResolutionCleared {
		t.Fatalf("resolved event = %#v, created = %#v", events[4].Payload, created)
	}
	status := waitPendingCount(t, service, response.TaskID, 0)
	if status.State != domain.TaskStateRunning {
		t.Fatalf("status state after resolved = %s, want running", status.State)
	}
	harness.RequireDone(t)
}

func TestPendingServerRequestResolvedClearsPaddedStringRequestID(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	serverRequestID := " 101 "
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendCommandApprovalRequest(serverRequestID, "thread-1", "turn-1", "item-command"),
			testappserver.SendNotification("serverRequest/resolved", map[string]any{"requestId": serverRequestID}),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Approve command"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	events := readStreamEvents(t, service, response.TaskID, 5)
	createdID := ""
	for _, event := range events {
		if created, ok := event.Payload.(domain.PendingRequestCreatedEvent); ok {
			createdID = created.PendingRequestID
		}
	}
	if createdID == "" {
		t.Fatalf("events = %#v, want PendingRequestCreatedEvent", events)
	}
	clearedEvents := 0
	for _, event := range events {
		resolved, ok := event.Payload.(domain.PendingRequestResolvedEvent)
		if !ok {
			continue
		}
		if resolved.Resolution == domain.PendingResolutionFailed {
			t.Fatalf("resolved event = %#v, did not expect failed resolution", resolved)
		}
		if resolved.PendingRequestID == createdID && resolved.Resolution == domain.PendingResolutionCleared {
			clearedEvents++
		}
	}
	if clearedEvents != 1 {
		t.Fatalf("cleared resolved event count = %d, want 1 in events %#v", clearedEvents, events)
	}
	status := waitPendingCount(t, service, response.TaskID, 0)
	if status.State != domain.TaskStateRunning {
		t.Fatalf("status state after resolved = %s, want running", status.State)
	}
	harness.RequireDone(t)
}

func TestPendingServerRequestResolvedClearsExactRawIDBeforeLogicalFallback(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := &task{
		id:             "task-1",
		sessionGroupID: group.SessionGroupID,
		workspaceID:    group.WorkspaceID,
		threadID:       "thread-1",
		turnID:         "turn-1",
		state:          domain.TaskStateWaitingForPendingRequest,
		connection:     connection,
		createdAt:      service.now(),
		pending:        pending.NewManager(),
		fileDiffs:      map[string]domain.FileDiffUpdatedEvent{},
		streams:        map[taskStreamKey]*redact.Stream{},
		subscribers:    map[uint64]*taskSubscriber{},
	}
	numericRecord := serviceTestPendingRecord(task.id, json.RawMessage(`101`))
	stringRecord := serviceTestPendingRecord(task.id, json.RawMessage(`"101"`))
	task.pending.Add(numericRecord)
	task.pending.Add(stringRecord)

	service.mu.Lock()
	service.tasks[task.id] = task
	service.mu.Unlock()

	service.handleServerRequestResolved(appserver.Notification{
		Method:                       "serverRequest/resolved",
		Params:                       json.RawMessage(`{"requestId":101}`),
		ServerRequestResolvedChecked: true,
		ServerRequestResolvedMatched: true,
	}, connection)
	service.mu.Lock()
	if numericRecord.Active || !stringRecord.Active || task.pending.ActiveCount() != 1 || task.state != domain.TaskStateWaitingForPendingRequest {
		service.mu.Unlock()
		t.Fatalf(
			"numeric resolved id cleared wrong pending state: numeric_active=%t string_active=%t active_count=%d state=%s",
			numericRecord.Active,
			stringRecord.Active,
			task.pending.ActiveCount(),
			task.state,
		)
	}
	service.mu.Unlock()

	service.handleServerRequestResolved(appserver.Notification{
		Method:                       "serverRequest/resolved",
		Params:                       json.RawMessage(`{"requestId":"101"}`),
		ServerRequestResolvedChecked: true,
		ServerRequestResolvedMatched: true,
	}, connection)
	service.mu.Lock()
	if stringRecord.Active || task.pending.ActiveCount() != 0 || task.state != domain.TaskStateRunning {
		service.mu.Unlock()
		t.Fatalf(
			"string resolved id did not clear string pending state: string_active=%t active_count=%d state=%s",
			stringRecord.Active,
			task.pending.ActiveCount(),
			task.state,
		)
	}
	service.mu.Unlock()
}

func TestPendingServerRequestResolvedCheckedMissDoesNotUseLogicalFallback(t *testing.T) {
	tests := []struct {
		name       string
		activeID   json.RawMessage
		resolvedID json.RawMessage
		params     json.RawMessage
	}{
		{
			name:       "string resolved id does not clear numeric request",
			activeID:   json.RawMessage(`101`),
			resolvedID: json.RawMessage(`"101"`),
			params:     json.RawMessage(`{"requestId":"101"}`),
		},
		{
			name:       "numeric resolved id does not clear string request",
			activeID:   json.RawMessage(`"101"`),
			resolvedID: json.RawMessage(`101`),
			params:     json.RawMessage(`{"requestId":101}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			group := testSessionGroup(t, "sg-1", "ws-1")
			service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
			if err != nil {
				t.Fatal(err)
			}
			connection := &appserver.Connection{}
			task := &task{
				id:             "task-1",
				sessionGroupID: group.SessionGroupID,
				workspaceID:    group.WorkspaceID,
				threadID:       "thread-1",
				turnID:         "turn-1",
				state:          domain.TaskStateWaitingForPendingRequest,
				connection:     connection,
				createdAt:      service.now(),
				pending:        pending.NewManager(),
				fileDiffs:      map[string]domain.FileDiffUpdatedEvent{},
				streams:        map[taskStreamKey]*redact.Stream{},
				subscribers:    map[uint64]*taskSubscriber{},
			}
			record := serviceTestPendingRecord(task.id, tt.activeID)
			task.pending.Add(record)

			service.mu.Lock()
			service.tasks[task.id] = task
			service.mu.Unlock()

			service.handleServerRequestResolved(appserver.Notification{
				Method:                       "serverRequest/resolved",
				Params:                       tt.params,
				ServerRequestResolvedChecked: true,
				ServerRequestResolvedMatched: false,
			}, connection)

			service.mu.Lock()
			active := record.Active
			activeCount := task.pending.ActiveCount()
			state := task.state
			eventCount := len(task.events)
			backlogCount := len(service.resolvedServerRequests[connection])
			service.mu.Unlock()
			if !active || activeCount != 1 || state != domain.TaskStateWaitingForPendingRequest {
				t.Fatalf(
					"checked raw miss changed pending state for resolved id %s: active=%t active_count=%d state=%s",
					string(tt.resolvedID),
					active,
					activeCount,
					state,
				)
			}
			if eventCount != 0 {
				t.Fatalf("checked raw miss emitted %d events, want none", eventCount)
			}
			if backlogCount != 0 {
				t.Fatalf("checked raw miss backlogged %d resolved ids, want none", backlogCount)
			}
		})
	}
}

func TestPendingServerRequestResolvedDuringInFlightResponseClearsWithoutBacklog(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	harness := testappserver.New(t)
	connection := newHarnessConnection(group, harness)
	defer connection.Close()

	task := &task{
		id:             "task-1",
		sessionGroupID: group.SessionGroupID,
		workspaceID:    group.WorkspaceID,
		threadID:       "thread-1",
		turnID:         "turn-1",
		state:          domain.TaskStateWaitingForPendingRequest,
		phase:          startPhaseStarted,
		connection:     connection,
		createdAt:      service.now(),
		pending:        pending.NewManager(),
		subscribers:    map[uint64]*taskSubscriber{},
		itemBindings:   map[string]struct{}{},
		fileDiffs:      map[string]domain.FileDiffUpdatedEvent{},
	}
	record := &pending.Record{
		Method:             pending.MethodCommandApproval,
		AppServerRequestID: "server-request-1",
		JSONRPCID:          json.RawMessage(`"server-request-1"`),
		Pending: domain.PendingRequest{
			TaskID:      task.id,
			PendingType: domain.PendingTypeCommandApproval,
			Display:     domain.CommandApprovalDisplay{CommandDisplay: "echo hello"},
		},
		Responses: map[string]*pending.ResponseEntry{},
	}
	task.pending.Add(record)
	record.InFlightClientResponseID = "response-1"
	service.mu.Lock()
	service.tasks[task.id] = task
	service.sessions[group.SessionGroupID].activeTaskID = task.id
	service.mu.Unlock()

	service.handleServerRequestResolved(appserver.Notification{
		Method:                       "serverRequest/resolved",
		Params:                       mustRawMessage(t, map[string]any{"requestId": "server-request-1"}),
		ServerRequestResolvedChecked: true,
		ServerRequestResolvedMatched: true,
	}, connection)

	service.mu.Lock()
	if record.Active || task.pending.ActiveCount() != 0 || task.state != domain.TaskStateRunning {
		service.mu.Unlock()
		t.Fatalf("in-flight record was not cleared: active=%t active_count=%d state=%s", record.Active, task.pending.ActiveCount(), task.state)
	}
	if ids := service.resolvedServerRequests[connection]; len(ids) != 0 {
		service.mu.Unlock()
		t.Fatalf("in-flight resolved notification was backlogged: %#v", ids)
	}
	service.mu.Unlock()

	build, taskID, _, _, _, ok := service.tryCreatePendingRequest(appserver.ServerRequest{
		ID:     json.RawMessage(`"server-request-1"`),
		Method: pending.MethodCommandApproval,
		Params: mustRawMessage(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"item": map[string]any{
				"id":   "item-command-2",
				"type": "commandExecution",
			},
			"command": []string{"echo", "again"},
		}),
	}, connection)
	if !ok {
		t.Fatalf("tryCreatePendingRequest() ok = false, build = %#v, taskID = %q", build, taskID)
	}
	if taskID != task.id || !build.Record.Active || task.pending.ActiveCount() != 1 {
		t.Fatalf("reused request id was auto-cleared: taskID=%q active=%t active_count=%d", taskID, build.Record.Active, task.pending.ActiveCount())
	}
}

func TestPendingResolvedBacklogDoesNotClearRawDifferentRequestID(t *testing.T) {
	tests := []struct {
		name            string
		staleResolvedID json.RawMessage
		futureRequestID json.RawMessage
	}{
		{
			name:            "stale string resolved id does not clear numeric request",
			staleResolvedID: json.RawMessage(`"101"`),
			futureRequestID: json.RawMessage(`101`),
		},
		{
			name:            "stale numeric resolved id does not clear string request",
			staleResolvedID: json.RawMessage(`101`),
			futureRequestID: json.RawMessage(`"101"`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			group := testSessionGroup(t, "sg-1", "ws-1")
			service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
			if err != nil {
				t.Fatal(err)
			}
			connection := &appserver.Connection{}
			task := &task{
				id:             "task-1",
				sessionGroupID: group.SessionGroupID,
				workspaceID:    group.WorkspaceID,
				threadID:       "thread-1",
				turnID:         "turn-1",
				state:          domain.TaskStateRunning,
				phase:          startPhaseStarted,
				connection:     connection,
				createdAt:      service.now(),
				pending:        pending.NewManager(),
				subscribers:    map[uint64]*taskSubscriber{},
				itemBindings:   map[string]struct{}{},
				fileDiffs:      map[string]domain.FileDiffUpdatedEvent{},
				streams:        map[taskStreamKey]*redact.Stream{},
			}
			service.mu.Lock()
			service.tasks[task.id] = task
			service.sessions[group.SessionGroupID].activeTaskID = task.id
			service.markResolvedServerRequestLocked(connection, tt.staleResolvedID)
			service.mu.Unlock()

			build, taskID, _, _, _, ok := service.tryCreatePendingRequest(appserver.ServerRequest{
				ID:     tt.futureRequestID,
				Method: pending.MethodCommandApproval,
				Params: mustRawMessage(t, map[string]any{
					"threadId": "thread-1",
					"turnId":   "turn-1",
					"item": map[string]any{
						"id":   "item-command",
						"type": "commandExecution",
					},
					"command": []string{"echo", "again"},
				}),
			}, connection)
			if !ok {
				t.Fatalf("tryCreatePendingRequest() ok = false, build = %#v, taskID = %q", build, taskID)
			}

			service.mu.Lock()
			activeCount := task.pending.ActiveCount()
			state := task.state
			backlogCount := len(service.resolvedServerRequests[connection])
			service.mu.Unlock()
			if taskID != task.id || !build.Record.Active || activeCount != 1 || state != domain.TaskStateWaitingForPendingRequest {
				t.Fatalf(
					"raw-different backlog cleared pending: taskID=%q active=%t active_count=%d state=%s",
					taskID,
					build.Record.Active,
					activeCount,
					state,
				)
			}
			if backlogCount != 1 {
				t.Fatalf("resolved backlog count = %d, want stale id retained", backlogCount)
			}
		})
	}
}

func serviceTestPendingRecord(taskID string, jsonrpcID json.RawMessage) *pending.Record {
	return &pending.Record{
		Method:             pending.MethodCommandApproval,
		AppServerRequestID: pending.NormalizeServerRequestID("", jsonrpcID),
		JSONRPCID:          jsonrpcID,
		Pending: domain.PendingRequest{
			TaskID:      taskID,
			PendingType: domain.PendingTypeCommandApproval,
			Display:     domain.CommandApprovalDisplay{CommandDisplay: "echo hello"},
		},
		Responses: map[string]*pending.ResponseEntry{},
	}
}

func TestPendingServerRequestResolvedAfterClosedRequestDoesNotBacklog(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	harness := testappserver.New(t)
	connection := newHarnessConnection(group, harness)
	defer connection.Close()

	task := &task{
		id:             "task-1",
		sessionGroupID: group.SessionGroupID,
		workspaceID:    group.WorkspaceID,
		threadID:       "thread-1",
		turnID:         "turn-1",
		state:          domain.TaskStateRunning,
		phase:          startPhaseStarted,
		connection:     connection,
		createdAt:      service.now(),
		pending:        pending.NewManager(),
		subscribers:    map[uint64]*taskSubscriber{},
		itemBindings:   map[string]struct{}{},
		fileDiffs:      map[string]domain.FileDiffUpdatedEvent{},
	}
	record := &pending.Record{
		Method:             pending.MethodCommandApproval,
		AppServerRequestID: "server-request-1",
		JSONRPCID:          json.RawMessage(`"server-request-1"`),
		Pending: domain.PendingRequest{
			TaskID:      task.id,
			PendingType: domain.PendingTypeCommandApproval,
			Display:     domain.CommandApprovalDisplay{CommandDisplay: "echo hello"},
		},
		Responses: map[string]*pending.ResponseEntry{},
	}
	task.pending.Add(record)
	task.pending.MarkResolved(record)
	service.mu.Lock()
	service.tasks[task.id] = task
	service.sessions[group.SessionGroupID].activeTaskID = task.id
	service.mu.Unlock()

	service.handleServerRequestResolved(appserver.Notification{
		Method:                       "serverRequest/resolved",
		Params:                       mustRawMessage(t, map[string]any{"requestId": "server-request-1"}),
		ServerRequestResolvedChecked: true,
	}, connection)

	service.mu.Lock()
	if ids := service.resolvedServerRequests[connection]; len(ids) != 0 {
		service.mu.Unlock()
		t.Fatalf("late resolved notification was backlogged: %#v", ids)
	}
	service.mu.Unlock()

	build, taskID, _, _, _, ok := service.tryCreatePendingRequest(appserver.ServerRequest{
		ID:     json.RawMessage(`"server-request-1"`),
		Method: pending.MethodCommandApproval,
		Params: mustRawMessage(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"item": map[string]any{
				"id":   "item-command-2",
				"type": "commandExecution",
			},
			"command": []string{"echo", "again"},
		}),
	}, connection)
	if !ok {
		t.Fatalf("tryCreatePendingRequest() ok = false, build = %#v, taskID = %q", build, taskID)
	}
	if taskID != task.id || !build.Record.Active || task.pending.ActiveCount() != 1 {
		t.Fatalf("reused request id was auto-cleared: taskID=%q active=%t active_count=%d", taskID, build.Record.Active, task.pending.ActiveCount())
	}
}

func TestPendingFileApprovalMarksDiffUnavailableWhenCachedDiffMissing(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendFileApprovalRequest(104, "thread-1", "turn-1", "file-change-1"),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Approve file"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 1)
	display, ok := status.ActivePendingRequests[0].Display.(domain.FileChangeApprovalDisplay)
	if !ok {
		t.Fatalf("file pending display = %T, want FileChangeApprovalDisplay", status.ActivePendingRequests[0].Display)
	}
	if !display.DiffUnavailable || display.DiffSummary != "" || display.DiffUnified != "" {
		t.Fatalf("file diff display = %#v, want explicit unavailable empty diff", display)
	}
	harness.RequireDone(t)
}

func TestPendingToolUserInputSecretAnswerIsRedactedAfterWrite(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	secret := "super-secret-answer"
	toolRequest := map[string]any{
		"threadId":  "thread-1",
		"turnId":    "turn-1",
		"requestId": "tool-input-1",
		"questions": []map[string]any{{
			"id":       "secret",
			"label":    "Secret",
			"question": "Enter secret",
			"isSecret": true,
		}},
	}
	echoParams := map[string]any{
		"threadId": "thread-1",
		"turnId":   "turn-1",
		"itemId":   "assistant-1",
		"delta":    "echo " + secret,
	}
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendRequest(105, testappserver.MethodToolUserInputRequest, toolRequest),
			testappserver.ExpectResponseID(105, testappserver.WithResult(map[string]any{
				"answers": map[string]any{
					"secret": map[string]any{"answers": []string{secret}},
				},
			})),
			testappserver.SendNotification("item/agentMessage/delta", echoParams),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Ask for input"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 1)
	pendingID := status.ActivePendingRequests[0].PendingRequestID
	_, err = service.RespondPendingRequest(context.Background(), domain.RespondPendingRequestCommand{
		TaskID:           response.TaskID,
		PendingRequestID: pendingID,
		ClientResponseID: "response-1",
		Response: domain.PendingResponse{
			ToolUserInput: &domain.ToolUserInputPendingResponse{
				Answers: []domain.ToolUserInputAnswer{{
					QuestionID: "secret",
					Answers:    []string{secret},
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("RespondPendingRequest() error = %v", err)
	}

	events := readStreamEvents(t, service, response.TaskID, 6)
	for _, event := range events {
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("json.Marshal(event) error = %v", err)
		}
		if strings.Contains(string(raw), secret) {
			t.Fatalf("event leaked secret answer: %s", raw)
		}
	}
	harness.RequireDone(t)
}

func TestPendingToolUserInputOptionValuesRoundTrip(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendToolUserInputRequest(105, "thread-1", "turn-1", "tool-input"),
			testappserver.ExpectResponseID(105, testappserver.WithResult(map[string]any{
				"answers": map[string]any{
					"q1": map[string]any{"answers": []string{"one"}},
				},
			})),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Ask for input"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 1)
	pendingRequest := status.ActivePendingRequests[0]
	display, ok := pendingRequest.Display.(domain.ToolUserInputDisplay)
	if !ok || len(display.Questions) != 1 {
		t.Fatalf("pending display = %#v, want one ToolUserInput question", pendingRequest.Display)
	}
	if strings.Join(display.Questions[0].Options, ",") != "one,two" {
		t.Fatalf("tool options = %#v, want sendable value tokens", display.Questions[0].Options)
	}

	_, err = service.RespondPendingRequest(context.Background(), domain.RespondPendingRequestCommand{
		TaskID:           response.TaskID,
		PendingRequestID: pendingRequest.PendingRequestID,
		ClientResponseID: "response-1",
		Response: domain.PendingResponse{
			ToolUserInput: &domain.ToolUserInputPendingResponse{
				Answers: []domain.ToolUserInputAnswer{{
					QuestionID: "q1",
					Answers:    []string{"one"},
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("RespondPendingRequest() error = %v", err)
	}
	harness.RequireDone(t)
}

func TestPendingMcpElicitationAcceptContentIsRedactedAfterWrite(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	secret := "mcp-secret-value"
	mcpRequest := map[string]any{
		"threadId":  "thread-1",
		"turnId":    "turn-1",
		"requestId": "mcp-request",
		"message":   "fill the form",
		"schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"password": map[string]any{
					"type":   "string",
					"format": "password",
				},
			},
		},
	}
	echoParams := map[string]any{
		"threadId": "thread-1",
		"turnId":   "turn-1",
		"itemId":   "mcp-echo",
		"delta":    "echo " + secret,
	}
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendRequest(106, testappserver.MethodMcpElicitationRequest, mcpRequest),
			testappserver.ExpectResponseID(106, testappserver.WithResult(map[string]any{
				"action":  "accept",
				"content": map[string]any{"password": secret},
				"_meta":   nil,
			})),
			testappserver.SendNotification("item/agentMessage/delta", echoParams),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Ask MCP"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 1)
	pendingID := status.ActivePendingRequests[0].PendingRequestID
	_, err = service.RespondPendingRequest(context.Background(), domain.RespondPendingRequestCommand{
		TaskID:           response.TaskID,
		PendingRequestID: pendingID,
		ClientResponseID: "response-1",
		Response: domain.PendingResponse{
			McpElicitation: &domain.McpElicitationPendingResponse{
				Action:      domain.McpElicitationActionAccept,
				ContentJSON: fmt.Sprintf(`{"password":%q}`, secret),
			},
		},
	})
	if err != nil {
		t.Fatalf("RespondPendingRequest() error = %v", err)
	}

	events := readStreamEvents(t, service, response.TaskID, 6)
	for _, event := range events {
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("json.Marshal(event) error = %v", err)
		}
		if strings.Contains(string(raw), secret) {
			t.Fatalf("event leaked MCP content: %s", raw)
		}
	}
	harness.RequireDone(t)
}

func TestPendingMcpElicitationSchemaSensitiveNumericContentIsRedactedExactly(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	secretNumber := json.Number("9007199254740993")
	mcpRequest := map[string]any{
		"threadId":  "thread-1",
		"turnId":    "turn-1",
		"requestId": "mcp-request",
		"message":   "fill the form",
		"schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"token": map[string]any{
					"type":     "number",
					"x-secret": true,
				},
			},
		},
	}
	echoParams := map[string]any{
		"threadId": "thread-1",
		"turnId":   "turn-1",
		"itemId":   "mcp-echo",
		"delta":    "echo " + secretNumber.String(),
	}
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendRequest(107, testappserver.MethodMcpElicitationRequest, mcpRequest),
			testappserver.ExpectResponseID(107, testappserver.WithResult(map[string]any{
				"action":  "accept",
				"content": map[string]any{"token": secretNumber},
				"_meta":   nil,
			})),
			testappserver.SendNotification("item/agentMessage/delta", echoParams),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Ask MCP"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 1)
	pendingID := status.ActivePendingRequests[0].PendingRequestID
	_, err = service.RespondPendingRequest(context.Background(), domain.RespondPendingRequestCommand{
		TaskID:           response.TaskID,
		PendingRequestID: pendingID,
		ClientResponseID: "response-1",
		Response: domain.PendingResponse{
			McpElicitation: &domain.McpElicitationPendingResponse{
				Action:      domain.McpElicitationActionAccept,
				ContentJSON: `{"token":9007199254740993}`,
			},
		},
	})
	if err != nil {
		t.Fatalf("RespondPendingRequest() error = %v", err)
	}

	events := readStreamEvents(t, service, response.TaskID, 6)
	for _, event := range events {
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("json.Marshal(event) error = %v", err)
		}
		if strings.Contains(string(raw), secretNumber.String()) {
			t.Fatalf("event leaked schema-marked numeric MCP content: %s", raw)
		}
	}
	harness.RequireDone(t)
}

func TestAcceptedPendingDisplayWithinHardCapSerializesInTaskStatus(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	group.PendingLimits.MaxActiveRequests = 1
	group.PendingLimits.MaxDisplayPayloadBytes = domain.MaxOutboundPendingDisplayPayloadBytes
	group.PendingLimits.StatusNonPendingBudgetBytes = 64 * domain.KiB
	group.GRPCLimits.OutboundMessageBytes = 4 * domain.MiB
	diff := strings.Repeat("x", 40*domain.KiB)
	fileRequest := map[string]any{
		"threadId":    "thread-1",
		"turnId":      "turn-1",
		"requestId":   "file-change-large",
		"fileLabel":   "README.md",
		"changeKind":  "modify",
		"diffUnified": diff,
	}
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendRequest(111, testappserver.MethodFileApprovalRequest, fileRequest),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Approve file"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 1)
	display, ok := status.ActivePendingRequests[0].Display.(domain.FileChangeApprovalDisplay)
	if !ok {
		t.Fatalf("pending display = %T, want FileChangeApprovalDisplay", status.ActivePendingRequests[0].Display)
	}
	if display.DiffUnified != diff {
		t.Fatalf("diff bytes = %d, want %d", len(display.DiffUnified), len(diff))
	}
	if _, failure := grpcapi.GetTaskStatusResponseToProtoWithFailure(status); failure != nil {
		t.Fatalf("GetTaskStatusResponseToProtoWithFailure() failure = %#v", failure)
	}
	if protoStatus, ok := grpcapi.GetTaskStatusResponseToProto(status); !ok || protoStatus == nil {
		t.Fatalf("GetTaskStatusResponseToProto() = (%v, %t), want success", protoStatus, ok)
	}
	harness.RequireDone(t)
}

func TestPendingOverLimitAutoResolvesWithoutCreatingPublicPending(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	group.PendingLimits.MaxActiveRequests = 1
	group.PendingLimits.MaxDisplayPayloadBytes = domain.MaxOutboundPendingDisplayPayloadBytes
	group.PendingLimits.StatusNonPendingBudgetBytes = 64 * domain.KiB
	group.GRPCLimits.OutboundMessageBytes = 4 * domain.MiB
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendCommandApprovalRequest(101, "thread-1", "turn-1", "item-command-1"),
			testappserver.SendFileApprovalRequest(102, "thread-1", "turn-1", "item-file-2"),
			testappserver.ExpectResponseID(102, testappserver.WithResult(map[string]any{"decision": "decline"})),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Approve command"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 1)
	if status.ActivePendingRequests[0].ItemID != "item-command-1" {
		t.Fatalf("active pending = %#v", status.ActivePendingRequests)
	}
	events := readStreamEvents(t, service, response.TaskID, 5)
	warning, ok := events[4].Payload.(domain.GatewayWarningEvent)
	if !ok || warning.Code != pending.WarningCodeOverLimit ||
		warning.RequestType != pending.RequestTypeFileApproval ||
		warning.AutoResolution != pending.AutoResolutionDecline ||
		warning.LimitReason != pending.LimitReasonStatusBudgetExceeded {
		t.Fatalf("over-limit warning = %#v", events[4].Payload)
	}
	harness.RequireDone(t)
}

func TestPendingToolUserInputDuplicateQuestionIDsErrorsWithoutPublicPending(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	toolRequest := map[string]any{
		"threadId":  "thread-1",
		"turnId":    "turn-1",
		"requestId": "tool-input-duplicate",
		"questions": []map[string]any{
			{"id": "q1", "question": "Pick one"},
			{"id": "q1", "question": "Pick again"},
		},
	}
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendRequest(112, testappserver.MethodToolUserInputRequest, toolRequest),
			testappserver.ExpectErrorResponseID(112, pending.ToolUserInputOverLimitCode, pending.ToolUserInputOverLimitMessage),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Ask for input"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 0)
	if len(status.ActivePendingRequests) != 0 {
		t.Fatalf("duplicate tool input created public pending: %#v", status.ActivePendingRequests)
	}
	events := readStreamEvents(t, service, response.TaskID, 4)
	warning, ok := events[3].Payload.(domain.GatewayWarningEvent)
	if !ok || warning.Code != pending.WarningCodeOverLimit ||
		warning.RequestType != pending.RequestTypeToolUserInput ||
		warning.AutoResolution != pending.AutoResolutionJSONRPCError ||
		warning.LimitReason != pending.LimitReasonControlsTooLarge {
		t.Fatalf("duplicate tool input warning = %#v", events[3].Payload)
	}
	harness.RequireDone(t)
}

func TestPendingMcpOversizedSchemaAutoDeclinesWithoutPublicPending(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	mcpRequest := map[string]any{
		"threadId":  "thread-1",
		"turnId":    "turn-1",
		"requestId": "mcp-oversized",
		"message":   "fill the form",
		"schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"note": map[string]any{
					"type":        "string",
					"description": strings.Repeat("x", domain.MaxOutboundMcpFormSchemaBytes),
				},
			},
		},
	}
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendRequest(108, testappserver.MethodMcpElicitationRequest, mcpRequest),
			testappserver.ExpectResponseID(108, testappserver.WithResult(map[string]any{
				"action":  "decline",
				"content": nil,
				"_meta":   nil,
			})),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Ask MCP"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 0)
	if len(status.ActivePendingRequests) != 0 {
		t.Fatalf("oversized schema created public pending: %#v", status.ActivePendingRequests)
	}
	events := readStreamEvents(t, service, response.TaskID, 4)
	warning, ok := events[3].Payload.(domain.GatewayWarningEvent)
	if !ok || warning.Code != pending.WarningCodeOverLimit ||
		warning.RequestType != pending.RequestTypeMcpElicitation ||
		warning.AutoResolution != pending.AutoResolutionDecline ||
		warning.LimitReason != pending.LimitReasonDisplayPayloadTooLarge {
		t.Fatalf("oversized schema warning = %#v", events[3].Payload)
	}
	harness.RequireDone(t)
}

func TestPendingMcpFileURLAutoDeclinesWithoutPublicPending(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	unsafeURL := "file:///C:/Users/alice/.ssh/config"
	mcpRequest := map[string]any{
		"threadId":  "thread-1",
		"turnId":    "turn-1",
		"requestId": "mcp-file-url",
		"message":   "open this URL",
		"url":       unsafeURL,
	}
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendRequest(109, testappserver.MethodMcpElicitationRequest, mcpRequest),
			testappserver.ExpectResponseID(109, testappserver.WithResult(map[string]any{
				"action":  "decline",
				"content": nil,
				"_meta":   nil,
			})),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Ask MCP"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	status := waitPendingCount(t, service, response.TaskID, 0)
	if len(status.ActivePendingRequests) != 0 {
		t.Fatalf("file URL created public pending: %#v", status.ActivePendingRequests)
	}
	events := readStreamEvents(t, service, response.TaskID, 4)
	warning, ok := events[3].Payload.(domain.GatewayWarningEvent)
	if !ok || warning.Code != pending.WarningCodeOverLimit ||
		warning.RequestType != pending.RequestTypeMcpElicitation ||
		warning.AutoResolution != pending.AutoResolutionDecline ||
		warning.LimitReason != pending.LimitReasonDisplayPayloadTooLarge {
		t.Fatalf("file URL warning = %#v", events[3].Payload)
	}
	assertPublicTaskDataDoesNotContain(t, unsafeURL, status, events)
	assertPublicTaskDataDoesNotContain(t, "C:/Users/alice", status, events)
	assertPublicTaskDataDoesNotContain(t, ".ssh/config", status, events)
	harness.RequireDone(t)
}

func TestUncorrelatedOverLimitRequestWithActiveTaskRecordsDiagnosticOnly(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	mcpRequest := map[string]any{
		"requestId": "mcp-uncorrelated",
		"message":   "fill the form",
		"schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"note": map[string]any{
					"type":        "string",
					"description": strings.Repeat("private", domain.MaxOutboundMcpFormSchemaBytes),
				},
			},
		},
	}
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendRequest(110, testappserver.MethodMcpElicitationRequest, mcpRequest),
			testappserver.ExpectResponseID(110, testappserver.WithResult(map[string]any{
				"action":  "decline",
				"content": nil,
				"_meta":   nil,
			})),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Ask MCP"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	diagnostics := waitConnectionDiagnosticCount(t, service, 1)
	if diagnostics[0].Code != pending.WarningCodeOverLimit ||
		diagnostics[0].RequestType != pending.RequestTypeMcpElicitation ||
		diagnostics[0].AutoResolution != pending.AutoResolutionDecline ||
		diagnostics[0].LimitReason != pending.LimitReasonStatusBudgetExceeded ||
		diagnostics[0].TaskID != "" {
		t.Fatalf("over-limit diagnostic = %#v", diagnostics[0])
	}
	harness.RequireDone(t)
	status := waitPendingCount(t, service, response.TaskID, 0)
	events := readStreamEvents(t, service, response.TaskID, int(status.LastEventID))
	for _, event := range events {
		if warning, ok := event.Payload.(domain.GatewayWarningEvent); ok &&
			warning.Code == pending.WarningCodeOverLimit {
			t.Fatalf("uncorrelated over-limit request emitted task warning: %#v", event)
		}
	}
	raw, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatalf("json.Marshal(diagnostics) error = %v", err)
	}
	if strings.Contains(string(raw), "private") {
		t.Fatalf("connection diagnostics leaked raw params: %s", raw)
	}
}

func TestForwardedUnsupportedServerRequestErrorsWithoutPending(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendRequest(111, "vendor/customRequest", map[string]any{
				"threadId": "thread-1",
				"turnId":   "turn-1",
			}),
			testappserver.ExpectErrorResponseID(111, -32002, "unsupported_server_request"),
		)...,
	)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Unsupported request"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	events := readStreamEvents(t, service, response.TaskID, 4)
	warning, ok := events[3].Payload.(domain.GatewayWarningEvent)
	if !ok || warning.Code != "unsupported_server_request" {
		t.Fatalf("unsupported warning = %#v", events[3].Payload)
	}
	if !strings.Contains(warning.Message, "vendor/customRequest") {
		t.Fatalf("unsupported warning message = %q, want method detail", warning.Message)
	}
	status := waitPendingCount(t, service, response.TaskID, 0)
	if len(status.ActivePendingRequests) != 0 {
		t.Fatalf("unsupported request created pending: %#v", status.ActivePendingRequests)
	}
	harness.RequireDone(t)
}

func TestUncorrelatedOverLimitAndUnsupportedRequestsRecordConnectionDiagnostics(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	steps := append([]testappserver.Step{}, testappserver.Initialize(group.CanonicalCodexHome)...)
	steps = append(steps,
		testappserver.SendCommandApprovalRequest(301, "thread-unowned", "turn-unowned", "item-unowned"),
		testappserver.ExpectResponseID(301, testappserver.WithResult(map[string]any{"decision": "decline"})),
		testappserver.SendRequest(302, "vendor/customRequest", map[string]any{
			"secret": "raw params must not be diagnosed",
		}),
		testappserver.ExpectErrorResponseID(302, -32002, "unsupported_server_request"),
	)
	harness := testappserver.New(t, steps...)
	supervisor := &countingSupervisor{}
	service, err := NewService([]Session{{Group: group, Supervisor: supervisor}})
	if err != nil {
		t.Fatal(err)
	}
	connection := newHarnessConnection(group, harness)
	if err := connection.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	service.configureConnectionHooks(service.sessions[group.SessionGroupID], connection)
	service.ensureMonitor(connection, supervisor)

	diagnostics := waitConnectionDiagnosticCount(t, service, 2)
	if diagnostics[0].Code != pending.WarningCodeOverLimit ||
		diagnostics[0].RequestType != pending.RequestTypeCommandApproval ||
		diagnostics[0].AutoResolution != pending.AutoResolutionDecline ||
		diagnostics[0].LimitReason != pending.LimitReasonStatusBudgetExceeded {
		t.Fatalf("over-limit diagnostic = %#v", diagnostics[0])
	}
	if diagnostics[1].Code != pending.WarningCodeUnsupportedServerRequest ||
		diagnostics[1].Method != "vendor/customRequest" {
		t.Fatalf("unsupported diagnostic = %#v", diagnostics[1])
	}
	raw, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatalf("json.Marshal(diagnostics) error = %v", err)
	}
	if strings.Contains(string(raw), "raw params") {
		t.Fatalf("connection diagnostics leaked raw params: %s", raw)
	}
	harness.RequireDone(t)
}

func TestDeferredUnsupportedServerRequestsCoalesceWhileStartTaskIsPending(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	steps := []testappserver.Step{
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart)),
	}
	for i := 0; i < maxDeferredUnsupportedWarnings+5; i++ {
		id := 400 + i
		steps = append(steps,
			testappserver.SendRequest(id, "attestation/generate", map[string]any{
				"taskId":    "task-1",
				"challenge": "raw challenge should not appear",
			}),
			testappserver.ExpectErrorResponseID(id, -32002, "unsupported_server_request"),
		)
	}
	steps = append(steps,
		testappserver.SendNotification(testappserver.MethodThreadStarted, testappserver.ThreadStartedParams("thread-1")),
		testappserver.SendResponseFor(testappserver.MethodThreadStart, testappserver.ThreadResult("thread-1")),
	)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-1")...)
	service, harness := newHarnessService(t, group, steps...)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	events := readStreamEvents(t, service, response.TaskID, 4)
	warningCount := 0
	var warning domain.GatewayWarningEvent
	for _, event := range events {
		if payload, ok := event.Payload.(domain.GatewayWarningEvent); ok && payload.Code == unsupportedServerRequestWarningCode {
			warningCount++
			warning = payload
		}
	}
	if warningCount != 1 {
		t.Fatalf("unsupported warning count = %d, want 1; events = %#v", warningCount, events)
	}
	if !strings.Contains(warning.Message, "attestation/generate") ||
		!strings.Contains(warning.Message, fmt.Sprintf("%d or more", maxDeferredUnsupportedWarnings)) {
		t.Fatalf("deferred unsupported warning message = %q", warning.Message)
	}
	if strings.Contains(warning.Message, "raw challenge") {
		t.Fatalf("deferred unsupported warning leaked params: %#v", warning)
	}
	harness.RequireDone(t)
}

func TestTurnCompletedRequiresMatchingThread(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := &task{
		id:              "task-1",
		sessionGroupID:  group.SessionGroupID,
		workspaceID:     group.WorkspaceID,
		clientMessageID: "client-1",
		threadID:        "thread-1",
		turnID:          "turn-1",
		state:           domain.TaskStateRunning,
		phase:           startPhaseStarted,
		connection:      connection,
		createdAt:       time.Now(),
		subscribers:     map[uint64]*taskSubscriber{},
	}
	service.mu.Lock()
	service.tasks[task.id] = task
	service.sessions[group.SessionGroupID].activeTaskID = task.id
	service.appendEventLocked(task, domain.TaskLifecycleEvent{
		LifecycleEvent: domain.TaskLifecycleEventTaskStarted,
		State:          domain.TaskStateRunning,
	})
	service.mu.Unlock()

	wrongThreadCompleted := mustRawMessage(t, testappserver.TurnCompletedParams("wrong-thread", "turn-1", "completed"))
	service.handleNotification(appserver.Notification{
		Method:         testappserver.MethodTurnCompleted,
		Params:         wrongThreadCompleted,
		TaskID:         task.id,
		SessionGroupID: group.SessionGroupID,
	}, connection)
	service.handleNotification(appserver.Notification{
		Method: testappserver.MethodTurnCompleted,
		Params: wrongThreadCompleted,
	}, connection)

	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: task.id},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.State != domain.TaskStateRunning || status.LastEventID != 1 || status.Terminal != nil {
		t.Fatalf("status after wrong-thread turn/completed = %#v", status)
	}
}

func TestTurnCompletedWithWrongTaskIDDoesNotFallbackToThreadAndTurn(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := &task{
		id:              "task-1",
		sessionGroupID:  group.SessionGroupID,
		workspaceID:     group.WorkspaceID,
		clientMessageID: "client-1",
		threadID:        "thread-1",
		turnID:          "turn-1",
		state:           domain.TaskStateRunning,
		phase:           startPhaseStarted,
		connection:      connection,
		createdAt:       time.Now(),
		subscribers:     map[uint64]*taskSubscriber{},
	}
	service.mu.Lock()
	service.tasks[task.id] = task
	service.sessions[group.SessionGroupID].activeTaskID = task.id
	service.appendEventLocked(task, domain.TaskLifecycleEvent{
		LifecycleEvent: domain.TaskLifecycleEventTaskStarted,
		State:          domain.TaskStateRunning,
	})
	service.mu.Unlock()

	service.handleNotification(appserver.Notification{
		Method:         testappserver.MethodTurnCompleted,
		Params:         mustRawMessage(t, testappserver.TurnCompletedParams("thread-1", "turn-1", "completed")),
		TaskID:         "wrong-task",
		SessionGroupID: group.SessionGroupID,
	}, connection)

	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: task.id},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.State != domain.TaskStateRunning || status.LastEventID != 1 || status.Terminal != nil {
		t.Fatalf("status after wrong-task turn/completed = %#v", status)
	}
}

func TestUnknownTurnCompletedStatusPublishesWarningBeforeTerminal(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := addManualRunningTask(t, service, group, connection, "task-1", "thread-1", "turn-1")

	service.handleNotification(appserver.Notification{
		Method:         testappserver.MethodTurnCompleted,
		Params:         mustRawMessage(t, testappserver.TurnCompletedParams("thread-1", "turn-1", "mystery")),
		TaskID:         task.id,
		SessionGroupID: group.SessionGroupID,
	}, connection)

	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: task.id},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.State != domain.TaskStateFailed || status.Terminal == nil ||
		status.Terminal.TerminalState != domain.TerminalStateFailed {
		t.Fatalf("status after unknown turn/completed = %#v", status)
	}
	events := readStreamEvents(t, service, task.id, int(status.LastEventID))
	warning, ok := events[len(events)-2].Payload.(domain.GatewayWarningEvent)
	if !ok || warning.Code != unknownTurnStatusWarningCode {
		t.Fatalf("penultimate event = %#v, want unknown status warning", events[len(events)-2])
	}
	terminal, ok := events[len(events)-1].Payload.(domain.TaskTerminalEvent)
	if !ok || terminal.TerminalState != domain.TerminalStateFailed {
		t.Fatalf("last event = %#v, want failed terminal", events[len(events)-1])
	}
	if events[len(events)-2].EventID >= events[len(events)-1].EventID {
		t.Fatalf("warning event id %d is not before terminal event id %d", events[len(events)-2].EventID, events[len(events)-1].EventID)
	}
}

func TestTurnCompletedFailedStatusIsKnownTerminal(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := &appserver.Connection{}
	task := addManualRunningTask(t, service, group, connection, "task-1", "thread-1", "turn-1")

	service.handleNotification(appserver.Notification{
		Method:         testappserver.MethodTurnCompleted,
		Params:         mustRawMessage(t, testappserver.TurnCompletedParams("thread-1", "turn-1", "failed")),
		TaskID:         task.id,
		SessionGroupID: group.SessionGroupID,
	}, connection)

	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: task.id},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.State != domain.TaskStateFailed || status.Terminal == nil ||
		status.Terminal.TerminalState != domain.TerminalStateFailed {
		t.Fatalf("status after failed turn/completed = %#v", status)
	}
	events := readStreamEvents(t, service, task.id, int(status.LastEventID))
	for _, event := range events {
		if warning, ok := event.Payload.(domain.GatewayWarningEvent); ok && warning.Code == unknownTurnStatusWarningCode {
			t.Fatalf("failed status emitted unknown status warning: %#v", event)
		}
	}
}

func TestIdempotentStartWaitsForSameFullResultBeforeAlreadyRunning(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart)),
		testappserver.Delay(50*time.Millisecond),
		testappserver.SendNotification(testappserver.MethodThreadStarted, testappserver.ThreadStartedParams("thread-1")),
		testappserver.SendResponseFor(testappserver.MethodThreadStart, testappserver.ThreadResult("thread-1")),
		testappserver.TurnStart("thread-1", "turn-1")[0],
		testappserver.TurnStart("thread-1", "turn-1")[1],
		testappserver.TurnStart("thread-1", "turn-1")[2],
	)

	command := startCommand(group, "client-1", "Do it")
	first := startAsync(service, command)
	harness.RequireOutboundRequest(t, 2, testappserver.MethodThreadStart)

	duplicate := startAsync(service, command)
	firstResponse, firstErr := first.wait(t)
	duplicateResponse, duplicateErr := duplicate.wait(t)
	if firstErr != nil || duplicateErr != nil {
		t.Fatalf("StartTask() errors = %v / %v", firstErr, duplicateErr)
	}
	if firstResponse != duplicateResponse {
		t.Fatalf("duplicate response = %#v, want %#v", duplicateResponse, firstResponse)
	}
	_, err := service.StartTask(context.Background(), startCommand(group, "client-2", "Another task"))
	assertGatewayReason(t, err, domain.ReasonAlreadyRunning)
}

func TestFingerprintMismatchIsSafeAndDoesNotOverwriteEntry(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart)),
		testappserver.Delay(50*time.Millisecond),
		testappserver.SendNotification(testappserver.MethodThreadStarted, testappserver.ThreadStartedParams("thread-1")),
		testappserver.SendResponseFor(testappserver.MethodThreadStart, testappserver.ThreadResult("thread-1")),
		testappserver.TurnStart("thread-1", "turn-1")[0],
		testappserver.TurnStart("thread-1", "turn-1")[1],
		testappserver.TurnStart("thread-1", "turn-1")[2],
	)

	original := startCommand(group, "client-1", "safe prompt")
	first := startAsync(service, original)
	harness.RequireOutboundRequest(t, 2, testappserver.MethodThreadStart)

	changed := startCommandWithContext(group, "client-1", "redaction-sentinel prompt changed", []domain.ContextBlock{{
		Kind:        domain.ContextBlockKindApplication,
		SourceLabel: "source",
		Content:     "redaction-sentinel context changed",
	}})
	_, err := service.StartTask(context.Background(), changed)
	assertGatewayReason(t, err, domain.ReasonIdempotencyFingerprintMismatch)
	if strings.Contains(err.Error(), "redaction-sentinel prompt") || strings.Contains(err.Error(), "redaction-sentinel context") {
		t.Fatalf("idempotency mismatch leaked request content: %q", err.Error())
	}
	if _, err := first.wait(t); err != nil {
		t.Fatalf("original StartTask() error = %v", err)
	}
}

func TestResumeUsesBindingAndExcludeTurns(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	checkResume := testappserver.CheckMessage(func(message testappserver.Message) error {
		var params map[string]any
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return err
		}
		if params["threadId"] != "thread-1" || params["excludeTurns"] != true {
			return errors.New("thread/resume params did not use excludeTurns true")
		}
		return nil
	})
	service, harness := newHarnessService(t, group,
		append(append(testappserver.ThreadStart("thread-1"), testappserver.TurnStart("thread-1", "turn-1")...),
			testappserver.SendNotification(testappserver.MethodTurnCompleted, testappserver.TurnCompletedParams("thread-1", "turn-1", "completed")),
			testappserver.ExpectRequest(testappserver.MethodThreadResume, testappserver.CaptureID(testappserver.MethodThreadResume), checkResume),
			testappserver.SendNotification(testappserver.MethodThreadStarted, testappserver.ThreadStartedParams("thread-1")),
			testappserver.SendResponseFor(testappserver.MethodThreadResume, testappserver.ThreadResumeResult("thread-1")),
			testappserver.TurnStart("thread-1", "turn-2")[0],
			testappserver.TurnStart("thread-1", "turn-2")[1],
			testappserver.TurnStart("thread-1", "turn-2")[2],
		)...,
	)

	first, err := service.StartTask(context.Background(), startCommand(group, "client-1", "First"))
	if err != nil {
		t.Fatalf("first StartTask() error = %v", err)
	}
	waitStatusState(t, service, first.TaskID, domain.TaskStateCompleted)

	resume := startCommand(group, "client-2", "Continue")
	resume.ThreadID = "thread-1"
	response, err := service.StartTask(context.Background(), resume)
	if err != nil {
		t.Fatalf("resume StartTask() error = %v", err)
	}
	if response.ThreadID != "thread-1" || response.TurnID != "turn-2" {
		t.Fatalf("resume response = %#v", response)
	}
	harness.RequireDone(t)
}

func TestThreadLocatorPrefersActiveTaskOverRetainedTerminalTask(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-1")...)
	steps = append(steps, testappserver.SendNotification(
		testappserver.MethodTurnCompleted,
		testappserver.TurnCompletedParams("thread-1", "turn-1", "completed"),
	))
	steps = append(steps, testappserver.ThreadResume("thread-1")...)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-2")...)
	service, harness := newHarnessService(t, group, steps...)

	first, err := service.StartTask(context.Background(), startCommand(group, "client-1", "First"))
	if err != nil {
		t.Fatalf("first StartTask() error = %v", err)
	}
	waitStatusState(t, service, first.TaskID, domain.TaskStateCompleted)

	resume := startCommand(group, "client-2", "Continue")
	resume.ThreadID = "thread-1"
	second, err := service.StartTask(context.Background(), resume)
	if err != nil {
		t.Fatalf("resume StartTask() error = %v", err)
	}
	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{
			Kind: domain.TaskLocatorByThread,
			ThreadLocator: domain.ThreadTaskLocator{
				SessionGroupID: group.SessionGroupID,
				ThreadID:       "thread-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus(thread locator) error = %v", err)
	}
	if status.TaskID != second.TaskID || status.State != domain.TaskStateRunning {
		t.Fatalf("thread locator status = %#v, want active task %#v", status, second)
	}
	harness.RequireDone(t)
}

func TestThreadBindingsAreScopedPerSessionGroup(t *testing.T) {
	groupA := testSessionGroup(t, "sg-1", "ws-1")
	groupB := testSessionGroup(t, "sg-2", "ws-2")
	const sharedThreadID = "thread-shared"

	stepsA := append([]testappserver.Step{}, testappserver.ThreadStart(sharedThreadID)...)
	stepsA = append(stepsA, testappserver.TurnStart(sharedThreadID, "turn-a1")...)
	stepsA = append(stepsA, testappserver.SendNotification(
		testappserver.MethodTurnCompleted,
		testappserver.TurnCompletedParams(sharedThreadID, "turn-a1", "completed"),
	))
	stepsA = append(stepsA, testappserver.ThreadResume(sharedThreadID)...)
	stepsA = append(stepsA, testappserver.TurnStart(sharedThreadID, "turn-a2")...)
	supervisorA, harnessA := newHarnessSupervisor(t, groupA, stepsA...)

	stepsB := append([]testappserver.Step{}, testappserver.ThreadStart(sharedThreadID)...)
	stepsB = append(stepsB, testappserver.TurnStart(sharedThreadID, "turn-b1")...)
	stepsB = append(stepsB, testappserver.SendNotification(
		testappserver.MethodTurnCompleted,
		testappserver.TurnCompletedParams(sharedThreadID, "turn-b1", "completed"),
	))
	stepsB = append(stepsB, testappserver.ThreadResume(sharedThreadID)...)
	stepsB = append(stepsB, testappserver.TurnStart(sharedThreadID, "turn-b2")...)
	supervisorB, harnessB := newHarnessSupervisor(t, groupB, stepsB...)

	service, err := NewService([]Session{
		{Group: groupA, Supervisor: supervisorA},
		{Group: groupB, Supervisor: supervisorB},
	})
	if err != nil {
		t.Fatal(err)
	}

	firstA, err := service.StartTask(context.Background(), startCommand(groupA, "client-a1", "First A"))
	if err != nil {
		t.Fatalf("first group A StartTask() error = %v", err)
	}
	waitStatusState(t, service, firstA.TaskID, domain.TaskStateCompleted)
	firstB, err := service.StartTask(context.Background(), startCommand(groupB, "client-b1", "First B"))
	if err != nil {
		t.Fatalf("first group B StartTask() error = %v", err)
	}
	waitStatusState(t, service, firstB.TaskID, domain.TaskStateCompleted)

	resumeA := startCommand(groupA, "client-a2", "Continue A")
	resumeA.ThreadID = sharedThreadID
	responseA, err := service.StartTask(context.Background(), resumeA)
	if err != nil {
		t.Fatalf("resume group A StartTask() error = %v", err)
	}
	if responseA.SessionGroupID != groupA.SessionGroupID || responseA.ThreadID != sharedThreadID || responseA.TurnID != "turn-a2" {
		t.Fatalf("resume group A response = %#v", responseA)
	}

	resumeB := startCommand(groupB, "client-b2", "Continue B")
	resumeB.ThreadID = sharedThreadID
	responseB, err := service.StartTask(context.Background(), resumeB)
	if err != nil {
		t.Fatalf("resume group B StartTask() error = %v", err)
	}
	if responseB.SessionGroupID != groupB.SessionGroupID || responseB.ThreadID != sharedThreadID || responseB.TurnID != "turn-b2" {
		t.Fatalf("resume group B response = %#v", responseB)
	}

	harnessA.RequireDone(t)
	harnessB.RequireDone(t)
}

func TestResumeBindingFailuresDoNotCallAppServer(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	otherGroup := testSessionGroup(t, "sg-2", "ws-2")
	supervisor := &countingSupervisor{}
	service, err := NewService([]Session{
		{Group: group, Supervisor: supervisor},
		{Group: otherGroup, Supervisor: supervisor},
	})
	if err != nil {
		t.Fatal(err)
	}

	unknown := startCommand(group, "client-unknown", "resume")
	unknown.ThreadID = "missing-thread"
	_, err = service.StartTask(context.Background(), unknown)
	assertGatewayReason(t, err, domain.ReasonUnknownThreadBinding)

	service.mu.Lock()
	threadKey := threadBindingMapKey(group.SessionGroupID, "thread-1")
	service.bindings[threadKey] = &threadBinding{
		threadID:       "thread-1",
		sessionGroupID: group.SessionGroupID,
		workspaceID:    group.WorkspaceID,
		taskID:         "task-old",
		createdAt:      time.Now(),
		expiresAt:      time.Now().Add(time.Hour),
	}
	service.sessions[group.SessionGroupID].bindings["thread-1"] = service.bindings[threadKey]
	expiredKey := threadBindingMapKey(group.SessionGroupID, "expired-thread")
	service.bindings[expiredKey] = &threadBinding{
		threadID:       "expired-thread",
		sessionGroupID: group.SessionGroupID,
		workspaceID:    group.WorkspaceID,
		taskID:         "task-old",
		createdAt:      time.Now().Add(-time.Hour),
		expiresAt:      time.Now().Add(-time.Second),
	}
	service.sessions[group.SessionGroupID].bindings["expired-thread"] = service.bindings[expiredKey]
	service.mu.Unlock()

	crossSession := startCommand(otherGroup, "client-cross", "resume")
	crossSession.ThreadID = "thread-1"
	_, err = service.StartTask(context.Background(), crossSession)
	assertGatewayReason(t, err, domain.ReasonUnknownThreadBinding)

	expired := startCommand(group, "client-expired", "resume")
	expired.ThreadID = "expired-thread"
	_, err = service.StartTask(context.Background(), expired)
	assertGatewayReason(t, err, domain.ReasonThreadBindingExpired)

	if got := supervisor.calls.Load(); got != 0 {
		t.Fatalf("supervisor calls = %d, want 0", got)
	}
}

func TestTurnStartProtocolMismatchRemovesNewThreadBinding(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	harness := testappserver.New(t,
		append(append(testappserver.Initialize(group.CanonicalCodexHome), testappserver.ThreadStart("thread-1")...),
			testappserver.ExpectRequest(testappserver.MethodTurnStart, testappserver.CaptureID(testappserver.MethodTurnStart)),
			testappserver.SendNotification(testappserver.MethodTurnStarted, testappserver.TurnStartedParams("thread-1", "turn-provisional")),
			testappserver.SendResponseFor(testappserver.MethodTurnStart, testappserver.TurnResult("turn-confirmed", "running")),
		)...,
	)
	supervisor := &oneShotHarnessSupervisor{group: group, harness: harness}
	service, err := NewService([]Session{{Group: group, Supervisor: supervisor}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	assertGatewayReason(t, err, domain.ReasonDispatcherUnavailable)

	resume := startCommand(group, "client-2", "Continue")
	resume.ThreadID = "thread-1"
	_, err = service.StartTask(context.Background(), resume)
	assertGatewayReasonOneOf(t, err, domain.ReasonThreadBindingExpired, domain.ReasonUnknownThreadBinding)
	if got := supervisor.calls.Load(); got != 1 {
		t.Fatalf("supervisor calls = %d, want 1", got)
	}
	harness.RequireDone(t)
}

func TestCapEvictedThreadBindingReturnsExpiredWithoutAppServerCall(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	group.ThreadBindingLimits.MaxBindings = 1
	supervisor := &countingSupervisor{}
	service, err := NewService([]Session{{Group: group, Supervisor: supervisor}})
	if err != nil {
		t.Fatal(err)
	}
	var nowMillis atomic.Int64
	base := time.Unix(1000, 0)
	nowMillis.Store(base.UnixMilli())
	service.SetClock(func() time.Time {
		return time.UnixMilli(nowMillis.Load())
	})
	session := service.sessions[group.SessionGroupID]
	service.upsertThreadBinding(session, "thread-old", "task-old")
	nowMillis.Store(base.Add(time.Minute).UnixMilli())
	service.upsertThreadBinding(session, "thread-new", "task-new")

	resume := startCommand(group, "client-resume", "Continue")
	resume.ThreadID = "thread-old"
	_, err = service.StartTask(context.Background(), resume)
	assertGatewayReason(t, err, domain.ReasonThreadBindingExpired)
	if got := supervisor.calls.Load(); got != 0 {
		t.Fatalf("supervisor calls = %d, want 0", got)
	}
}

func TestPartialStartFailureClearsActiveAndRetainsSafeResult(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	steps := []testappserver.Step{
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID("failed-thread")),
		testappserver.SendErrorResponseFor("failed-thread", -32000, "raw prompt should not leak"),
	}
	steps = append(steps, testappserver.ThreadStart("thread-2")...)
	steps = append(steps, testappserver.TurnStart("thread-2", "turn-2")...)
	service, harness := newHarnessService(t, group, steps...)

	failedCommand := startCommand(group, "client-1", "raw prompt should not leak")
	_, err := service.StartTask(context.Background(), failedCommand)
	assertGatewayReason(t, err, domain.ReasonDispatcherUnavailable)
	if strings.Contains(err.Error(), "raw prompt") {
		t.Fatalf("start failure leaked prompt: %q", err.Error())
	}

	_, err = service.StartTask(context.Background(), failedCommand)
	assertGatewayReason(t, err, domain.ReasonDispatcherUnavailable)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-2", "second"))
	if err != nil {
		t.Fatalf("second StartTask() error = %v", err)
	}
	if response.ThreadID != "thread-2" || response.TurnID != "turn-2" {
		t.Fatalf("second response = %#v", response)
	}
	harness.RequireDone(t)
}

func TestTerminalNotificationBeforeLateTurnStartFailureDoesNotOverwriteTask(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps,
		testappserver.ExpectRequest(testappserver.MethodTurnStart, testappserver.CaptureID(testappserver.MethodTurnStart)),
		testappserver.SendNotification(testappserver.MethodTurnStarted, testappserver.TurnStartedParams("thread-1", "turn-1")),
		testappserver.SendNotification(testappserver.MethodTurnCompleted, testappserver.TurnCompletedParams("thread-1", "turn-1", "completed")),
		testappserver.Delay(80*time.Millisecond),
		testappserver.SendErrorResponseFor(testappserver.MethodTurnStart, -32000, "late start failure"),
	)
	service, harness := newHarnessService(t, group, steps...)

	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	if response.State != domain.TaskStateCompleted || response.ThreadID != "thread-1" || response.TurnID != "turn-1" {
		t.Fatalf("StartTask() response = %#v", response)
	}
	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.State != domain.TaskStateCompleted || status.Terminal == nil || status.Terminal.TerminalState != domain.TerminalStateCompleted {
		t.Fatalf("status after late turn/start failure = %#v", status)
	}
	events := readStreamEvents(t, service, response.TaskID, int(status.LastEventID))
	terminalEvents := 0
	for _, event := range events {
		if terminal, ok := event.Payload.(domain.TaskTerminalEvent); ok {
			terminalEvents++
			if terminal.TerminalState != domain.TerminalStateCompleted {
				t.Fatalf("terminal event was overwritten: %#v", terminal)
			}
		}
	}
	if terminalEvents != 1 {
		t.Fatalf("terminal event count = %d, want 1", terminalEvents)
	}
	harness.RequireDone(t)
}

func TestStartCallTimeoutClosesOldConnectionBeforeNextTask(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	firstHarness := testappserver.New(t,
		append(testappserver.Initialize(group.CanonicalCodexHome),
			testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart)),
			testappserver.Delay(80*time.Millisecond),
		)...,
	)
	secondSteps := append([]testappserver.Step{}, testappserver.Initialize(group.CanonicalCodexHome)...)
	secondSteps = append(secondSteps, testappserver.ThreadStart("thread-2")...)
	secondSteps = append(secondSteps, testappserver.TurnStart("thread-2", "turn-2")...)
	secondHarness := testappserver.New(t, secondSteps...)
	supervisor := newSequenceHarnessSupervisor(group, firstHarness, secondHarness)
	service, err := NewService([]Session{{Group: group, Supervisor: supervisor}})
	if err != nil {
		t.Fatal(err)
	}
	service.threadCallTimeout = 20 * time.Millisecond

	_, err = service.StartTask(context.Background(), startCommand(group, "client-1", "first"))
	assertGatewayReason(t, err, domain.ReasonDispatcherUnavailable)
	firstConnection := supervisor.connectionAt(t, 0)
	select {
	case <-firstConnection.Done():
	default:
		t.Fatal("first connection was not closed after uncertain thread/start timeout")
	}
	if got := supervisor.waitClosed(t, 1); got != 1 {
		t.Fatalf("closed connections = %d, want 1", got)
	}

	response, err := service.StartTask(context.Background(), startCommand(group, "client-2", "second"))
	if err != nil {
		t.Fatalf("second StartTask() error = %v", err)
	}
	if response.ThreadID != "thread-2" || response.TurnID != "turn-2" {
		t.Fatalf("second response = %#v", response)
	}
	before := response.LastEventID
	service.handleNotification(appserver.Notification{
		Method: "item/agentMessage/delta",
		Params: mustRawMessage(t, map[string]any{
			"threadId": "thread-2",
			"turnId":   "turn-2",
			"itemId":   "late-old",
			"delta":    "must not attach from old connection",
		}),
	}, firstConnection)
	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.LastEventID != before {
		t.Fatalf("late old connection notification changed new task status: before=%d status=%#v", before, status)
	}
	if got := supervisor.calls.Load(); got != 2 {
		t.Fatalf("supervisor calls = %d, want 2", got)
	}
	firstHarness.RequireDone(t)
	secondHarness.RequireDone(t)
}

func TestRetainedTerminalTaskAndIdempotencyExpireAfterReplayTTL(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	group.ReplayLimits.TTLMillis = int64(time.Minute / time.Millisecond)
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-1")...)
	steps = append(steps, testappserver.SendNotification(
		testappserver.MethodTurnCompleted,
		testappserver.TurnCompletedParams("thread-1", "turn-1", "completed"),
	))
	steps = append(steps, testappserver.ThreadStart("thread-2")...)
	steps = append(steps, testappserver.TurnStart("thread-2", "turn-2")...)
	service, harness := newHarnessService(t, group, steps...)
	var nowMillis atomic.Int64
	base := time.Unix(2000, 0)
	nowMillis.Store(base.UnixMilli())
	service.SetClock(func() time.Time {
		return time.UnixMilli(nowMillis.Load())
	})
	command := startCommand(group, "client-1", "Do it")

	first, err := service.StartTask(context.Background(), command)
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	waitStatusState(t, service, first.TaskID, domain.TaskStateCompleted)
	nowMillis.Store(base.Add(time.Minute - time.Second).UnixMilli())
	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: first.TaskID},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() before TTL error = %v", err)
	}
	if status.State != domain.TaskStateCompleted {
		t.Fatalf("status before TTL = %#v", status)
	}
	retry, err := service.StartTask(context.Background(), command)
	if err != nil {
		t.Fatalf("idempotent StartTask() before TTL error = %v", err)
	}
	if retry != first {
		t.Fatalf("idempotent response before TTL = %#v, want %#v", retry, first)
	}

	nowMillis.Store(base.Add(time.Minute + time.Second).UnixMilli())
	_, err = service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: first.TaskID},
	})
	assertGatewayReason(t, err, domain.ReasonUnknownTask)
	second, err := service.StartTask(context.Background(), command)
	if err != nil {
		t.Fatalf("StartTask() after TTL error = %v", err)
	}
	if second.TaskID == first.TaskID || second.ThreadID != "thread-2" || second.TurnID != "turn-2" {
		t.Fatalf("StartTask() after TTL response = %#v, first = %#v", second, first)
	}
	harness.RequireDone(t)
}

func TestCallerCancellationAfterClaimDoesNotStopOwnedStart(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart)),
		testappserver.Delay(80*time.Millisecond),
		testappserver.SendNotification(testappserver.MethodThreadStarted, testappserver.ThreadStartedParams("thread-1")),
		testappserver.SendResponseFor(testappserver.MethodThreadStart, testappserver.ThreadResult("thread-1")),
		testappserver.TurnStart("thread-1", "turn-1")[0],
		testappserver.TurnStart("thread-1", "turn-1")[1],
		testappserver.TurnStart("thread-1", "turn-1")[2],
	)
	ctx, cancel := context.WithCancel(context.Background())
	result := startAsyncContext(service, ctx, startCommand(group, "client-1", "Do it"))
	harness.RequireOutboundRequest(t, 2, testappserver.MethodThreadStart)
	cancel()

	_, err := result.wait(t)
	assertGatewayReason(t, err, domain.ReasonCallerCanceled)
	status := waitStatusByClient(t, service, group.SessionGroupID, "client-1", domain.TaskStateRunning)
	if status.TaskID == "" || status.ThreadID != "thread-1" || status.TurnID != "turn-1" {
		t.Fatalf("recovered status = %#v", status)
	}
}

func TestStartTaskDoneContextBeforeClaimDoesNotCreateTask(t *testing.T) {
	tests := []struct {
		name   string
		ctx    func() (context.Context, context.CancelFunc)
		reason domain.GatewayErrorReason
	}{
		{
			name: "canceled",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			reason: domain.ReasonCallerCanceled,
		},
		{
			name: "deadline",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			reason: domain.ReasonCallerDeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			group := testSessionGroup(t, "sg-1", "ws-1")
			supervisor := &countingSupervisor{}
			service, err := NewService([]Session{{Group: group, Supervisor: supervisor}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := tt.ctx()
			defer cancel()
			clientMessageID := "client-" + tt.name

			_, err = service.StartTask(ctx, startCommand(group, clientMessageID, "Do it"))
			assertGatewayReason(t, err, tt.reason)
			_, err = service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
				Locator: domain.TaskLocator{
					Kind: domain.TaskLocatorByClientMessage,
					ClientMessageLocator: domain.ClientMessageTaskLocator{
						SessionGroupID:  group.SessionGroupID,
						ClientMessageID: clientMessageID,
					},
				},
			})
			assertGatewayReason(t, err, domain.ReasonUnknownTask)
			if got := supervisor.calls.Load(); got != 0 {
				t.Fatalf("supervisor calls = %d, want 0", got)
			}
		})
	}
}

func TestInterruptActiveTurnIsSentOnceAndIdempotent(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-1")...)
	steps = append(steps, testappserver.TurnInterrupt("turn-1")...)
	service, harness := newHarnessService(t, group, steps...)
	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}

	interrupt, err := service.InterruptTask(context.Background(), domain.InterruptTaskCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	if err != nil {
		t.Fatalf("InterruptTask() error = %v", err)
	}
	if !interrupt.InterruptSent || interrupt.State != domain.TaskStateInterrupting {
		t.Fatalf("InterruptTask() response = %#v", interrupt)
	}
	again, err := service.InterruptTask(context.Background(), domain.InterruptTaskCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	if err != nil {
		t.Fatalf("second InterruptTask() error = %v", err)
	}
	if !again.AlreadyInterrupting || again.InterruptSent {
		t.Fatalf("second InterruptTask() response = %#v", again)
	}
	harness.RequireDone(t)
	messages := harness.OutboundMessages()
	interruptCount := 0
	for _, message := range messages {
		if message.Method == testappserver.MethodTurnInterrupt {
			interruptCount++
		}
	}
	if interruptCount != 1 {
		t.Fatalf("turn/interrupt calls = %d, want 1", interruptCount)
	}
}

func TestInterruptDoneContextBeforeClaimDoesNotMutateTask(t *testing.T) {
	tests := []struct {
		name   string
		ctx    func() (context.Context, context.CancelFunc)
		reason domain.GatewayErrorReason
	}{
		{
			name: "canceled",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			reason: domain.ReasonCallerCanceled,
		},
		{
			name: "deadline",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			reason: domain.ReasonCallerDeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			group := testSessionGroup(t, "sg-1", "ws-1")
			service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
			if err != nil {
				t.Fatal(err)
			}
			connection := &appserver.Connection{}
			task := addManualRunningTask(t, service, group, connection, "task-1", "thread-1", "turn-1")
			ctx, cancel := tt.ctx()
			defer cancel()

			_, err = service.InterruptTask(ctx, domain.InterruptTaskCommand{
				Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: task.id},
			})
			assertGatewayReason(t, err, tt.reason)
			status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
				Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: task.id},
			})
			if err != nil {
				t.Fatalf("GetTaskStatus() error = %v", err)
			}
			if status.State != domain.TaskStateRunning || status.LastEventID != 1 {
				t.Fatalf("status after pre-claim canceled interrupt = %#v", status)
			}
		})
	}
}

func TestInterruptCallerCancellationAfterClaimKeepsInterruptingAndSendsOnce(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-1")...)
	steps = append(steps,
		testappserver.ExpectRequest(testappserver.MethodTurnInterrupt, testappserver.CaptureID(testappserver.MethodTurnInterrupt)),
		testappserver.Delay(80*time.Millisecond),
		testappserver.SendResponseFor(testappserver.MethodTurnInterrupt, testappserver.TurnResult("turn-1", "interrupted")),
	)
	service, harness := newHarnessService(t, group, steps...)
	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	first := interruptAsyncContext(service, ctx, domain.InterruptTaskCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	harness.RequireOutboundRequest(t, 4, testappserver.MethodTurnInterrupt)
	cancel()

	again, err := service.InterruptTask(context.Background(), domain.InterruptTaskCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	if err != nil {
		t.Fatalf("second InterruptTask() error = %v", err)
	}
	if !again.AlreadyInterrupting || again.InterruptSent {
		t.Fatalf("second InterruptTask() response = %#v", again)
	}
	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.State != domain.TaskStateInterrupting {
		t.Fatalf("status after canceled caller interrupt = %#v", status)
	}
	if _, err := first.wait(t); err != nil {
		t.Fatalf("first InterruptTask() error = %v", err)
	}
	harness.RequireDone(t)
	if got := countOutboundMethod(harness.OutboundMessages(), testappserver.MethodTurnInterrupt); got != 1 {
		t.Fatalf("turn/interrupt calls = %d, want 1", got)
	}
}

func TestInterruptFailureRollsBackRunningTaskWithWarningEvent(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-1")...)
	steps = append(steps,
		testappserver.ExpectRequest(testappserver.MethodTurnInterrupt, testappserver.CaptureID(testappserver.MethodTurnInterrupt)),
		testappserver.SendErrorResponseFor(testappserver.MethodTurnInterrupt, -32000, "rejected"),
	)
	service, harness := newHarnessService(t, group, steps...)
	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}

	_, err = service.InterruptTask(context.Background(), domain.InterruptTaskCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	assertGatewayReason(t, err, domain.ReasonDispatcherUnavailable)
	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.State != domain.TaskStateRunning {
		t.Fatalf("status after rejected interrupt = %#v", status)
	}
	events := readStreamEvents(t, service, response.TaskID, 5)
	warning, ok := events[len(events)-1].Payload.(domain.GatewayWarningEvent)
	if !ok {
		t.Fatalf("last event payload = %T, want GatewayWarningEvent", events[len(events)-1].Payload)
	}
	if warning.Code != interruptRejectedWarningCode || warning.Message == "" {
		t.Fatalf("warning event = %#v", warning)
	}
	harness.RequireDone(t)
}

func TestInterruptTimeoutClosesConnectionWithoutRejectedRollback(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-1")...)
	steps = append(steps,
		testappserver.ExpectRequest(testappserver.MethodTurnInterrupt, testappserver.CaptureID(testappserver.MethodTurnInterrupt)),
		testappserver.Delay(80*time.Millisecond),
		testappserver.SendResponseForIgnoringWriteError(testappserver.MethodTurnInterrupt, testappserver.TurnResult("turn-1", "interrupted")),
	)
	service, harness := newHarnessService(t, group, steps...)
	service.turnInterruptTimeout = 20 * time.Millisecond
	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}

	_, err = service.InterruptTask(context.Background(), domain.InterruptTaskCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	assertGatewayReason(t, err, domain.ReasonDispatcherUnavailable)
	status := waitStatusState(t, service, response.TaskID, domain.TaskStateFailed)
	events := readStreamEvents(t, service, response.TaskID, int(status.LastEventID))
	for _, event := range events {
		if warning, ok := event.Payload.(domain.GatewayWarningEvent); ok && warning.Code == interruptRejectedWarningCode {
			t.Fatalf("interrupt timeout emitted rejected warning: %#v", event)
		}
	}
	if got := countOutboundMethod(harness.OutboundMessages(), testappserver.MethodTurnInterrupt); got != 1 {
		t.Fatalf("turn/interrupt calls = %d, want 1", got)
	}
	harness.RequireDone(t)
}

func TestRejectedInterruptLiveStreamPublishesInterruptingBeforeWarning(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-1")...)
	steps = append(steps,
		testappserver.ExpectRequest(testappserver.MethodTurnInterrupt, testappserver.CaptureID(testappserver.MethodTurnInterrupt)),
		testappserver.SendErrorResponseFor(testappserver.MethodTurnInterrupt, -32000, "rejected"),
	)
	service, harness := newHarnessService(t, group, steps...)
	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	stream := openTaskStream(t, service, domain.StreamTaskCommand{
		TaskID:       response.TaskID,
		CursorKind:   domain.StreamCursorAfterEventID,
		AfterEventID: response.LastEventID,
	})
	defer closeTaskStream(t, stream)

	_, err = service.InterruptTask(context.Background(), domain.InterruptTaskCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	assertGatewayReason(t, err, domain.ReasonDispatcherUnavailable)

	interrupting := nextStreamMessage(t, stream)
	if interrupting.Event == nil {
		t.Fatalf("first live message = %#v, want interrupting event", interrupting)
	}
	lifecycle, ok := interrupting.Event.Payload.(domain.TaskLifecycleEvent)
	if !ok || lifecycle.LifecycleEvent != domain.TaskLifecycleEventStateChanged ||
		lifecycle.State != domain.TaskStateInterrupting {
		t.Fatalf("first live event = %#v, want interrupting state change", interrupting.Event)
	}
	warningMessage := nextStreamMessage(t, stream)
	if warningMessage.Event == nil {
		t.Fatalf("second live message = %#v, want warning event", warningMessage)
	}
	warning, ok := warningMessage.Event.Payload.(domain.GatewayWarningEvent)
	if !ok || warning.Code != interruptRejectedWarningCode {
		t.Fatalf("second live event = %#v, want rejected interrupt warning", warningMessage.Event)
	}
	if interrupting.Event.EventID >= warningMessage.Event.EventID {
		t.Fatalf("live event ids out of order: interrupting=%d warning=%d", interrupting.Event.EventID, warningMessage.Event.EventID)
	}
	harness.RequireDone(t)
}

func TestRepeatedRejectedInterruptEventsStayBoundedByReplayLimits(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	group.ReplayLimits.MaxEvents = 3
	group.ReplayLimits.MaxBytes = 1 << 20
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-1")...)
	for i := 0; i < 5; i++ {
		steps = append(steps,
			testappserver.ExpectRequest(testappserver.MethodTurnInterrupt, testappserver.CaptureID(testappserver.MethodTurnInterrupt)),
			testappserver.SendErrorResponseFor(testappserver.MethodTurnInterrupt, -32000, "rejected"),
		)
	}
	service, harness := newHarnessService(t, group, steps...)
	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}

	for i := 0; i < 5; i++ {
		_, err = service.InterruptTask(context.Background(), domain.InterruptTaskCommand{
			Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
		})
		assertGatewayReason(t, err, domain.ReasonDispatcherUnavailable)
	}

	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.FromStartAvailable || status.OldestBufferedEventID == 0 {
		t.Fatalf("status replay availability = %#v", status)
	}
	if status.StartEvictedBeforeEventID != status.OldestBufferedEventID-1 {
		t.Fatalf("status eviction boundary = %#v", status)
	}
	if status.NewestBufferedEventID != status.LastEventID {
		t.Fatalf("status newest buffered = %#v", status)
	}
	events := readStreamEvents(t, service, response.TaskID, group.ReplayLimits.MaxEvents)
	if events[0].EventID != status.OldestBufferedEventID || events[len(events)-1].EventID != status.NewestBufferedEventID {
		t.Fatalf("stream events = %#v, status = %#v", events, status)
	}
	service.mu.Lock()
	retained := len(service.tasks[response.TaskID].events)
	service.mu.Unlock()
	if retained != group.ReplayLimits.MaxEvents {
		t.Fatalf("retained event count = %d, want %d", retained, group.ReplayLimits.MaxEvents)
	}
	harness.RequireDone(t)
}

func TestReplayNoticesAfterEvictionAreStreamLocal(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	group.ReplayLimits.MaxEvents = 3
	group.ReplayLimits.MaxBytes = 1 << 20
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	task := addManualRunningTask(t, service, group, &appserver.Connection{}, "task-1", "thread-1", "turn-1")
	for i := 0; i < 5; i++ {
		appendGatewayWarningForTest(service, task.id, "warning")
	}
	appendTerminalForTest(service, task.id, domain.TerminalStateCompleted)

	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: task.id},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.FromStartAvailable || status.OldestBufferedEventID <= 1 || status.NewestBufferedEventID != status.LastEventID {
		t.Fatalf("status replay metadata = %#v", status)
	}

	fromStart := openTaskStream(t, service, domain.StreamTaskCommand{TaskID: task.id, CursorKind: domain.StreamCursorFromStart})
	defer closeTaskStream(t, fromStart)
	first := nextStreamMessage(t, fromStart)
	if first.Event != nil || first.ReplayNotice == nil || first.ReplayNotice.Code != domain.ReplayNoticeStartEvicted {
		t.Fatalf("first from_start message = %#v", first)
	}
	if first.ReplayNotice.OldestBufferedEventID != status.OldestBufferedEventID ||
		first.ReplayNotice.NewestBufferedEventID != status.NewestBufferedEventID ||
		first.ReplayNotice.FromStartAvailable {
		t.Fatalf("from_start replay notice = %#v, status = %#v", first.ReplayNotice, status)
	}
	for eventID := status.OldestBufferedEventID; eventID <= status.NewestBufferedEventID; eventID++ {
		message := nextStreamMessage(t, fromStart)
		if message.Event == nil || message.Event.EventID != eventID {
			t.Fatalf("from_start replay event = %#v, want event id %d", message, eventID)
		}
	}
	assertStreamEOF(t, fromStart)

	afterEvicted := openTaskStream(t, service, domain.StreamTaskCommand{
		TaskID:       task.id,
		CursorKind:   domain.StreamCursorAfterEventID,
		AfterEventID: 1,
	})
	defer closeTaskStream(t, afterEvicted)
	cursorNotice := nextStreamMessage(t, afterEvicted)
	if cursorNotice.Event != nil || cursorNotice.ReplayNotice == nil || cursorNotice.ReplayNotice.Code != domain.ReplayNoticeCursorEvicted {
		t.Fatalf("first after_event_id message = %#v", cursorNotice)
	}
	cursorReplay := nextStreamMessage(t, afterEvicted)
	if cursorReplay.Event == nil || cursorReplay.Event.EventID != status.OldestBufferedEventID {
		t.Fatalf("cursor replay event = %#v, want oldest event id %d", cursorReplay, status.OldestBufferedEventID)
	}

	_, err = service.StreamTask(context.Background(), domain.StreamTaskCommand{
		TaskID:       task.id,
		CursorKind:   domain.StreamCursorAfterEventID,
		AfterEventID: status.NewestBufferedEventID + 1,
	})
	assertGatewayReason(t, err, domain.ReasonInvalidCursor)
	afterStatus, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: task.id},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() after streams error = %v", err)
	}
	if afterStatus.LastEventID != status.LastEventID || afterStatus.NewestBufferedEventID != status.NewestBufferedEventID {
		t.Fatalf("replay notices changed status: before %#v after %#v", status, afterStatus)
	}
}

func TestSlowSubscriberDisconnectsAndCanReplayWithoutTaskLogLoss(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	group.ReplayLimits.MaxEvents = streamSubscriberQueue + 10
	group.ReplayLimits.MaxBytes = 1 << 20
	service, err := NewService([]Session{{Group: group, Supervisor: &countingSupervisor{}}})
	if err != nil {
		t.Fatal(err)
	}
	task := addManualRunningTask(t, service, group, &appserver.Connection{}, "task-1", "thread-1", "turn-1")
	stream := openTaskStream(t, service, domain.StreamTaskCommand{TaskID: task.id, CursorKind: domain.StreamCursorFromStart})
	defer closeTaskStream(t, stream)
	initial := nextStreamMessage(t, stream)
	if initial.Event == nil || initial.Event.EventID != 1 {
		t.Fatalf("initial stream message = %#v", initial)
	}

	for i := 0; i < streamSubscriberQueue+1; i++ {
		appendGatewayWarningForTest(service, task.id, "slow-subscriber-test")
	}
	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: task.id},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.LastEventID != uint64(streamSubscriberQueue+2) {
		t.Fatalf("status after slow subscriber fill = %#v", status)
	}

	lastSeen := initial.Event.EventID
	for {
		message, err := nextStreamMessageAllowEOF(stream)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream Next() error = %v", err)
		}
		if message.Event == nil {
			t.Fatalf("stream message = %#v, want event", message)
		}
		lastSeen = message.Event.EventID
	}
	if lastSeen >= status.LastEventID {
		t.Fatalf("slow subscriber was not disconnected before newest event: lastSeen=%d status=%#v", lastSeen, status)
	}

	replay := openTaskStream(t, service, domain.StreamTaskCommand{
		TaskID:       task.id,
		CursorKind:   domain.StreamCursorAfterEventID,
		AfterEventID: lastSeen,
	})
	defer closeTaskStream(t, replay)
	replayed := nextStreamMessage(t, replay)
	if replayed.Event == nil || replayed.Event.EventID != status.LastEventID {
		t.Fatalf("replayed event = %#v, want last event id %d", replayed, status.LastEventID)
	}
}

func TestStartTaskCancelDuringTurnStartRejectedInterruptReturnsRunning(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	turnStart := testappserver.TurnStart("thread-1", "turn-1")
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps,
		turnStart[0],
		testappserver.Delay(80*time.Millisecond),
		testappserver.SendResponseFor(testappserver.MethodTurnStart, testappserver.TurnResult("turn-1", "running")),
		testappserver.ExpectRequest(testappserver.MethodTurnInterrupt, testappserver.CaptureID(testappserver.MethodTurnInterrupt)),
		testappserver.SendErrorResponseFor(testappserver.MethodTurnInterrupt, -32000, "rejected"),
	)
	service, harness := newHarnessService(t, group, steps...)
	command := startCommand(group, "client-1", "Do it")
	result := startAsync(service, command)
	harness.RequireOutboundRequest(t, 3, testappserver.MethodTurnStart)

	interrupt, err := service.InterruptTask(context.Background(), domain.InterruptTaskCommand{
		Locator: domain.TaskLocator{
			Kind: domain.TaskLocatorByClientMessage,
			ClientMessageLocator: domain.ClientMessageTaskLocator{
				SessionGroupID:  group.SessionGroupID,
				ClientMessageID: "client-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("InterruptTask() error = %v", err)
	}
	if !interrupt.PreTurnCancelRecorded || interrupt.TaskID == "" {
		t.Fatalf("InterruptTask() response = %#v", interrupt)
	}

	response, err := result.wait(t)
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	if response.State != domain.TaskStateRunning || response.ThreadID != "thread-1" || response.TurnID != "turn-1" {
		t.Fatalf("StartTask() response after rejected interrupt = %#v", response)
	}
	retry, err := service.StartTask(context.Background(), command)
	if err != nil {
		t.Fatalf("idempotent StartTask() error = %v", err)
	}
	if retry != response {
		t.Fatalf("idempotent response = %#v, want %#v", retry, response)
	}
	status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	if err != nil {
		t.Fatalf("GetTaskStatus() error = %v", err)
	}
	if status.State != domain.TaskStateRunning || status.LastEventID != response.LastEventID || status.TurnID != response.TurnID {
		t.Fatalf("status after rejected interrupt = %#v, start response = %#v", status, response)
	}
	events := readStreamEvents(t, service, response.TaskID, int(status.LastEventID))
	warning, ok := events[len(events)-1].Payload.(domain.GatewayWarningEvent)
	if !ok {
		t.Fatalf("last event payload = %T, want GatewayWarningEvent", events[len(events)-1].Payload)
	}
	if warning.Code != interruptRejectedWarningCode || warning.Message == "" {
		t.Fatalf("warning event = %#v", warning)
	}
	harness.RequireDone(t)
}

func TestInterruptTerminalTaskReturnsAlreadyTerminal(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps, testappserver.TurnStart("thread-1", "turn-1")...)
	steps = append(steps, testappserver.TurnInterrupt("turn-1")...)
	steps = append(steps, testappserver.SendNotification(
		testappserver.MethodTurnCompleted,
		testappserver.TurnCompletedParams("thread-1", "turn-1", "interrupted"),
	))
	service, harness := newHarnessService(t, group, steps...)
	response, err := service.StartTask(context.Background(), startCommand(group, "client-1", "Do it"))
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	_, err = service.InterruptTask(context.Background(), domain.InterruptTaskCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	if err != nil {
		t.Fatalf("InterruptTask() error = %v", err)
	}
	waitStatusState(t, service, response.TaskID, domain.TaskStateInterrupted)

	terminal, err := service.InterruptTask(context.Background(), domain.InterruptTaskCommand{
		Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: response.TaskID},
	})
	if err != nil {
		t.Fatalf("terminal InterruptTask() error = %v", err)
	}
	if !terminal.AlreadyTerminal || terminal.InterruptSent {
		t.Fatalf("terminal InterruptTask() response = %#v", terminal)
	}
	harness.RequireDone(t)
}

func TestPreTurnInterruptPreventsTurnStartAndStatusIsRecoverable(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart)),
		testappserver.Delay(80*time.Millisecond),
		testappserver.SendNotification(testappserver.MethodThreadStarted, testappserver.ThreadStartedParams("thread-1")),
		testappserver.SendResponseFor(testappserver.MethodThreadStart, testappserver.ThreadResult("thread-1")),
	)

	result := startAsync(service, startCommand(group, "client-1", "Do it"))
	harness.RequireOutboundRequest(t, 2, testappserver.MethodThreadStart)

	interrupt, err := service.InterruptTask(context.Background(), domain.InterruptTaskCommand{
		Locator: domain.TaskLocator{
			Kind: domain.TaskLocatorByClientMessage,
			ClientMessageLocator: domain.ClientMessageTaskLocator{
				SessionGroupID:  group.SessionGroupID,
				ClientMessageID: "client-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("InterruptTask() error = %v", err)
	}
	if !interrupt.PreTurnCancelRecorded || interrupt.TaskID == "" {
		t.Fatalf("InterruptTask() response = %#v", interrupt)
	}

	_, err = result.wait(t)
	assertGatewayReason(t, err, domain.ReasonStartInterruptedBeforeTurn)
	status := waitStatusByClient(t, service, group.SessionGroupID, "client-1", domain.TaskStateInterrupted)
	if status.TaskID != interrupt.TaskID || status.ThreadID != "thread-1" || status.TurnID != "" {
		t.Fatalf("interrupted status = %#v, interrupt = %#v", status, interrupt)
	}
	harness.RequireDone(t)
	for _, message := range harness.OutboundMessages() {
		if message.Method == testappserver.MethodTurnStart {
			t.Fatalf("turn/start was sent after pre-turn interrupt: %#v", message)
		}
	}
}

func TestPreTurnInterruptCancelsBlockedConnectionAcquisition(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	supervisor := &blockingConnectionSupervisor{entered: make(chan struct{}, 1)}
	service, err := NewService([]Session{{Group: group, Supervisor: supervisor}})
	if err != nil {
		t.Fatal(err)
	}

	command := startCommand(group, "client-1", "Do it")
	result := startAsync(service, command)
	select {
	case <-supervisor.entered:
	case <-time.After(time.Second):
		t.Fatal("StartTask() did not enter Connection()")
	}

	interrupt, err := service.InterruptTask(context.Background(), domain.InterruptTaskCommand{
		Locator: domain.TaskLocator{
			Kind: domain.TaskLocatorByClientMessage,
			ClientMessageLocator: domain.ClientMessageTaskLocator{
				SessionGroupID:  group.SessionGroupID,
				ClientMessageID: "client-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("InterruptTask() error = %v", err)
	}
	if !interrupt.PreTurnCancelRecorded || interrupt.TaskID == "" {
		t.Fatalf("InterruptTask() response = %#v", interrupt)
	}

	_, err = result.wait(t)
	assertGatewayReason(t, err, domain.ReasonStartInterruptedBeforeTurn)
	status := waitStatusByClient(t, service, group.SessionGroupID, "client-1", domain.TaskStateInterrupted)
	if status.TaskID != interrupt.TaskID || status.ThreadID != "" || status.TurnID != "" {
		t.Fatalf("interrupted status = %#v, interrupt = %#v", status, interrupt)
	}

	_, err = service.StartTask(context.Background(), startCommand(group, "client-2", "Second"))
	assertGatewayReason(t, err, domain.ReasonDispatcherUnavailable)
	if got := supervisor.calls.Load(); got != 2 {
		t.Fatalf("supervisor calls = %d, want 2", got)
	}
}

func TestPreTurnInterruptWinsWhenThreadStartFails(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart)),
		testappserver.Delay(80*time.Millisecond),
		testappserver.SendErrorResponseFor(testappserver.MethodThreadStart, -32000, "failed"),
	)
	command := startCommand(group, "client-1", "Do it")
	result := startAsync(service, command)
	harness.RequireOutboundRequest(t, 2, testappserver.MethodThreadStart)

	interrupt, err := service.InterruptTask(context.Background(), domain.InterruptTaskCommand{
		Locator: domain.TaskLocator{
			Kind: domain.TaskLocatorByClientMessage,
			ClientMessageLocator: domain.ClientMessageTaskLocator{
				SessionGroupID:  group.SessionGroupID,
				ClientMessageID: "client-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("InterruptTask() error = %v", err)
	}
	if !interrupt.PreTurnCancelRecorded || interrupt.TaskID == "" {
		t.Fatalf("InterruptTask() response = %#v", interrupt)
	}

	_, err = result.wait(t)
	assertGatewayReason(t, err, domain.ReasonStartInterruptedBeforeTurn)
	_, err = service.StartTask(context.Background(), command)
	assertGatewayReason(t, err, domain.ReasonStartInterruptedBeforeTurn)
	status := waitStatusByClient(t, service, group.SessionGroupID, "client-1", domain.TaskStateInterrupted)
	if status.ThreadID != "" || status.TurnID != "" {
		t.Fatalf("interrupted status = %#v", status)
	}
	harness.RequireDone(t)
	for _, message := range harness.OutboundMessages() {
		if message.Method == testappserver.MethodTurnStart {
			t.Fatalf("turn/start was sent after pre-turn interrupt: %#v", message)
		}
	}
}

func TestStatusDoesNotBlockBehindInFlightStart(t *testing.T) {
	group := testSessionGroup(t, "sg-1", "ws-1")
	service, harness := newHarnessService(t, group,
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart)),
		testappserver.Delay(120*time.Millisecond),
		testappserver.SendNotification(testappserver.MethodThreadStarted, testappserver.ThreadStartedParams("thread-1")),
		testappserver.SendResponseFor(testappserver.MethodThreadStart, testappserver.ThreadResult("thread-1")),
		testappserver.TurnStart("thread-1", "turn-1")[0],
		testappserver.TurnStart("thread-1", "turn-1")[1],
		testappserver.TurnStart("thread-1", "turn-1")[2],
	)

	result := startAsync(service, startCommand(group, "client-1", "Do it"))
	harness.RequireOutboundRequest(t, 2, testappserver.MethodThreadStart)

	done := make(chan error, 1)
	go func() {
		_, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
			Locator: domain.TaskLocator{
				Kind: domain.TaskLocatorByClientMessage,
				ClientMessageLocator: domain.ClientMessageTaskLocator{
					SessionGroupID:  group.SessionGroupID,
					ClientMessageID: "client-1",
				},
			},
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GetTaskStatus() error = %v", err)
		}
	case <-time.After(30 * time.Millisecond):
		t.Fatal("GetTaskStatus() blocked behind in-flight start")
	}
	if _, err := result.wait(t); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
}

type asyncStart struct {
	response domain.StartTaskResponse
	err      error
	done     chan struct{}
}

func startAsync(service *Service, command domain.StartTaskCommand) *asyncStart {
	return startAsyncContext(service, context.Background(), command)
}

func startAsyncContext(service *Service, ctx context.Context, command domain.StartTaskCommand) *asyncStart {
	result := &asyncStart{done: make(chan struct{})}
	go func() {
		result.response, result.err = service.StartTask(ctx, command)
		close(result.done)
	}()
	return result
}

func (s *asyncStart) wait(t *testing.T) (domain.StartTaskResponse, error) {
	t.Helper()
	select {
	case <-s.done:
		return s.response, s.err
	case <-time.After(2 * time.Second):
		t.Fatal("StartTask() did not finish")
		return domain.StartTaskResponse{}, nil
	}
}

type asyncInterrupt struct {
	response domain.InterruptTaskResponse
	err      error
	done     chan struct{}
}

func interruptAsyncContext(service *Service, ctx context.Context, command domain.InterruptTaskCommand) *asyncInterrupt {
	result := &asyncInterrupt{done: make(chan struct{})}
	go func() {
		result.response, result.err = service.InterruptTask(ctx, command)
		close(result.done)
	}()
	return result
}

func (s *asyncInterrupt) wait(t *testing.T) (domain.InterruptTaskResponse, error) {
	t.Helper()
	select {
	case <-s.done:
		return s.response, s.err
	case <-time.After(2 * time.Second):
		t.Fatal("InterruptTask() did not finish")
		return domain.InterruptTaskResponse{}, nil
	}
}

func readStreamEvents(t *testing.T, service *Service, taskID string, count int) []domain.TaskEvent {
	t.Helper()
	stream := openTaskStream(t, service, domain.StreamTaskCommand{
		TaskID:     taskID,
		CursorKind: domain.StreamCursorFromStart,
	})
	defer closeTaskStream(t, stream)
	events := make([]domain.TaskEvent, 0, count)
	for len(events) < count {
		message := nextStreamMessage(t, stream)
		if message.ReplayNotice != nil {
			continue
		}
		if message.Event == nil {
			t.Fatal("stream message event = nil")
		}
		events = append(events, *message.Event)
	}
	return events
}

func openTaskStream(t *testing.T, service *Service, command domain.StreamTaskCommand) grpcapi.TaskStream {
	t.Helper()
	stream, err := service.StreamTask(context.Background(), command)
	if err != nil {
		t.Fatalf("StreamTask() error = %v", err)
	}
	return stream
}

func closeTaskStream(t *testing.T, stream grpcapi.TaskStream) {
	t.Helper()
	if stream == nil {
		return
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func nextStreamMessage(t *testing.T, stream grpcapi.TaskStream) grpcapi.StreamTaskMessage {
	t.Helper()
	message, err := nextStreamMessageAllowEOF(stream)
	if err != nil {
		t.Fatalf("stream Next() error = %v", err)
	}
	return message
}

func nextStreamMessageAllowEOF(stream grpcapi.TaskStream) (grpcapi.StreamTaskMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return stream.Next(ctx)
}

func assertStreamEOF(t *testing.T, stream grpcapi.TaskStream) {
	t.Helper()
	_, err := nextStreamMessageAllowEOF(stream)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("stream Next() error = %v, want EOF", err)
	}
}

func mustRawMessage(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return raw
}

func addManualRunningTask(
	t *testing.T,
	service *Service,
	group config.SessionGroup,
	connection *appserver.Connection,
	taskID string,
	threadID string,
	turnID string,
) *task {
	t.Helper()
	pathSanitizer, _ := redact.NewPathSanitizer(group.CanonicalCWD)
	task := &task{
		id:              taskID,
		sessionGroupID:  group.SessionGroupID,
		workspaceID:     group.WorkspaceID,
		clientMessageID: taskID + "-client",
		threadID:        threadID,
		turnID:          turnID,
		state:           domain.TaskStateRunning,
		phase:           startPhaseStarted,
		connection:      connection,
		sensitive:       redact.NewRegistry(),
		pathSanitizer:   pathSanitizer,
		createdAt:       time.Now(),
		itemBindings:    map[string]struct{}{},
		streams:         map[taskStreamKey]*redact.Stream{},
		subscribers:     map[uint64]*taskSubscriber{},
	}
	service.mu.Lock()
	service.tasks[task.id] = task
	service.sessions[group.SessionGroupID].activeTaskID = task.id
	service.appendEventLocked(task, domain.TaskLifecycleEvent{
		LifecycleEvent: domain.TaskLifecycleEventTaskStarted,
		State:          domain.TaskStateRunning,
	})
	service.mu.Unlock()
	return task
}

func appendGatewayWarningForTest(service *Service, taskID string, code string) domain.TaskEvent {
	service.mu.Lock()
	task := service.tasks[taskID]
	event, subscribers := service.appendEventLocked(task, domain.GatewayWarningEvent{
		Code:    code,
		Message: "test warning",
	})
	service.mu.Unlock()
	service.publishEvent(taskID, subscribers, event, false)
	return event
}

func appendTerminalForTest(service *Service, taskID string, terminalState domain.TerminalState) domain.TaskEvent {
	service.mu.Lock()
	task := service.tasks[taskID]
	terminal := domain.TaskTerminalEvent{TerminalState: terminalState}
	task.state = stateForTerminal(terminalState)
	task.phase = startPhaseTerminal
	task.terminal = &terminal
	task.terminalAt = service.now()
	service.clearActiveLocked(task)
	payloads := append(service.flushTaskStreamsLocked(task), terminal)
	publications := service.appendPublicationsLocked(task, payloads, true)
	service.mu.Unlock()
	service.publishPublications(publications)
	return publications[len(publications)-1].event
}

type countingSupervisor struct {
	calls atomic.Int32
}

func (s *countingSupervisor) Connection(context.Context) (*appserver.Connection, error) {
	s.calls.Add(1)
	return nil, errors.New("unexpected connection")
}

func (s *countingSupervisor) MarkClosed(*appserver.Connection) {}

type oneShotHarnessSupervisor struct {
	group   config.SessionGroup
	harness *testappserver.Harness
	calls   atomic.Int32
}

func (s *oneShotHarnessSupervisor) Connection(ctx context.Context) (*appserver.Connection, error) {
	if s.calls.Add(1) != 1 {
		return nil, errors.New("unexpected connection")
	}
	connection := newHarnessConnection(s.group, s.harness)
	if err := connection.Initialize(ctx); err != nil {
		return nil, err
	}
	return connection, nil
}

func (s *oneShotHarnessSupervisor) MarkClosed(*appserver.Connection) {}

type sequenceHarnessSupervisor struct {
	group     config.SessionGroup
	harnesses []*testappserver.Harness
	calls     atomic.Int32
	closed    atomic.Int32
	conns     atomic.Value
}

func newSequenceHarnessSupervisor(group config.SessionGroup, harnesses ...*testappserver.Harness) *sequenceHarnessSupervisor {
	supervisor := &sequenceHarnessSupervisor{group: group, harnesses: harnesses}
	supervisor.conns.Store([]*appserver.Connection(nil))
	return supervisor
}

func (s *sequenceHarnessSupervisor) Connection(ctx context.Context) (*appserver.Connection, error) {
	index := int(s.calls.Add(1)) - 1
	if index < 0 || index >= len(s.harnesses) {
		return nil, errors.New("unexpected connection")
	}
	connection := newHarnessConnection(s.group, s.harnesses[index])
	connections := append([]*appserver.Connection(nil), s.conns.Load().([]*appserver.Connection)...)
	connections = append(connections, connection)
	s.conns.Store(connections)
	if err := connection.Initialize(ctx); err != nil {
		return nil, err
	}
	return connection, nil
}

func (s *sequenceHarnessSupervisor) MarkClosed(*appserver.Connection) {
	s.closed.Add(1)
}

func (s *sequenceHarnessSupervisor) connectionAt(t *testing.T, index int) *appserver.Connection {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		connections := s.conns.Load().([]*appserver.Connection)
		if index < len(connections) {
			return connections[index]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("connection %d was not created", index)
	return nil
}

func (s *sequenceHarnessSupervisor) waitClosed(t *testing.T, want int32) int32 {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := s.closed.Load(); got >= want {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	return s.closed.Load()
}

type blockingConnectionSupervisor struct {
	calls   atomic.Int32
	entered chan struct{}
}

func (s *blockingConnectionSupervisor) Connection(ctx context.Context) (*appserver.Connection, error) {
	if s.calls.Add(1) == 1 {
		select {
		case s.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, errors.New("connection unavailable")
}

func (s *blockingConnectionSupervisor) MarkClosed(*appserver.Connection) {}

func newHarnessService(t *testing.T, group config.SessionGroup, steps ...testappserver.Step) (*Service, *testappserver.Harness) {
	t.Helper()
	supervisor, harness := newHarnessSupervisor(t, group, steps...)
	service, err := NewService([]Session{{Group: group, Supervisor: supervisor}})
	if err != nil {
		t.Fatal(err)
	}
	return service, harness
}

func newHarnessSupervisor(t *testing.T, group config.SessionGroup, steps ...testappserver.Step) (*appserver.Supervisor, *testappserver.Harness) {
	t.Helper()
	script := append([]testappserver.Step{}, testappserver.Initialize(group.CanonicalCodexHome)...)
	script = append(script, steps...)
	harness := testappserver.New(t, script...)
	supervisor := appserver.NewSupervisor(group.SessionGroupID, func(ctx context.Context) (*appserver.Connection, error) {
		connection := newHarnessConnection(group, harness)
		if err := connection.Initialize(ctx); err != nil {
			return nil, err
		}
		return connection, nil
	})
	return supervisor, harness
}

func newHarnessConnection(group config.SessionGroup, harness *testappserver.Harness) *appserver.Connection {
	return appserver.NewConnection(harness.Stdin(), harness.Stdout(), group, appserver.ConnectionOptions{
		ForwardRequests: true,
		SchemaPolicy: appserver.SchemaPolicy{Metadata: appserver.SchemaMetadata{
			ThreadStartPermissions:              true,
			ThreadResumeExcludeTurns:            true,
			ThreadResumeInitialTurnsPage:        true,
			ThreadResumeActivePermissionProfile: true,
		}},
	})
}

func testSessionGroup(t *testing.T, sessionGroupID string, workspaceID string) config.SessionGroup {
	t.Helper()
	return config.SessionGroup{
		SessionGroupID:     sessionGroupID,
		WorkspaceID:        workspaceID,
		CanonicalCWD:       t.TempDir(),
		CanonicalCodexHome: t.TempDir(),
		RuntimePolicy: config.RuntimePolicy{
			ApprovalPolicy: config.ApprovalPolicyOnRequest,
			SandboxMode:    config.SandboxWorkspaceWrite,
		},
		ThreadBindingLimits: config.ThreadBindingLimits{
			MaxBindings: 1000,
			TTLMillis:   int64((24 * time.Hour) / time.Millisecond),
		},
	}
}

func startCommand(group config.SessionGroup, clientMessageID string, prompt string) domain.StartTaskCommand {
	return startCommandWithContext(group, clientMessageID, prompt, nil)
}

func startCommandWithContext(group config.SessionGroup, clientMessageID string, prompt string, blocks []domain.ContextBlock) domain.StartTaskCommand {
	return domain.StartTaskCommand{
		SessionGroupID:  group.SessionGroupID,
		WorkspaceID:     group.WorkspaceID,
		Prompt:          prompt,
		ContextBlocks:   blocks,
		ClientMessageID: clientMessageID,
	}
}

func waitStatusState(t *testing.T, service *Service, taskID string, state domain.TaskState) domain.GetTaskStatusResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
			Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: taskID},
		})
		if err == nil && status.State == state {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("task %q did not reach state %q", taskID, state)
	return domain.GetTaskStatusResponse{}
}

func waitPendingCount(t *testing.T, service *Service, taskID string, count int) domain.GetTaskStatusResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
			Locator: domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: taskID},
		})
		if err == nil && len(status.ActivePendingRequests) == count {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("task %q did not reach pending count %d", taskID, count)
	return domain.GetTaskStatusResponse{}
}

func waitConnectionDiagnosticCount(t *testing.T, service *Service, count int) []connectionDiagnostic {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		diagnostics := service.connectionDiagnosticsSnapshot()
		if len(diagnostics) >= count {
			return diagnostics
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("connection diagnostics did not reach count %d", count)
	return nil
}

func waitStatusByClient(t *testing.T, service *Service, sessionGroupID string, clientMessageID string, state domain.TaskState) domain.GetTaskStatusResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err := service.GetTaskStatus(context.Background(), domain.GetTaskStatusCommand{
			Locator: domain.TaskLocator{
				Kind: domain.TaskLocatorByClientMessage,
				ClientMessageLocator: domain.ClientMessageTaskLocator{
					SessionGroupID:  sessionGroupID,
					ClientMessageID: clientMessageID,
				},
			},
		})
		if err == nil && status.State == state {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("client message %q did not reach state %q", clientMessageID, state)
	return domain.GetTaskStatusResponse{}
}

func countOutboundMethod(messages []testappserver.Message, method string) int {
	count := 0
	for _, message := range messages {
		if message.Method == method {
			count++
		}
	}
	return count
}

func assertPublicTaskDataDoesNotContain(t *testing.T, fragment string, status domain.GetTaskStatusResponse, events []domain.TaskEvent) {
	t.Helper()
	raw, err := json.Marshal(struct {
		Status domain.GetTaskStatusResponse `json:"status"`
		Events []domain.TaskEvent           `json:"events"`
	}{
		Status: status,
		Events: events,
	})
	if err != nil {
		t.Fatalf("json.Marshal(public task data) error = %v", err)
	}
	if strings.Contains(string(raw), fragment) {
		t.Fatalf("public task data leaked %q: %s", fragment, raw)
	}
}

func assertGatewayReason(t *testing.T, err error, reason domain.GatewayErrorReason) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want reason %s", reason)
	}
	var gatewayErr *domain.GatewayError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("error = %T %v, want GatewayError", err, err)
	}
	if gatewayErr.Details.Reason != reason {
		t.Fatalf("error reason = %q, want %q (error %v)", gatewayErr.Details.Reason, reason, err)
	}
}

func assertGatewayCodeReason(t *testing.T, err error, code domain.GatewayErrorCode, reason domain.GatewayErrorReason) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want code %s reason %s", code, reason)
	}
	var gatewayErr *domain.GatewayError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("error = %T %v, want GatewayError", err, err)
	}
	if gatewayErr.Code != code || gatewayErr.Details.Reason != reason {
		t.Fatalf("gateway error = code %q reason %q, want code %q reason %q", gatewayErr.Code, gatewayErr.Details.Reason, code, reason)
	}
}

func assertGatewayReasonOneOf(t *testing.T, err error, reasons ...domain.GatewayErrorReason) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want one of %v", reasons)
	}
	var gatewayErr *domain.GatewayError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("error = %T %v, want GatewayError", err, err)
	}
	for _, reason := range reasons {
		if gatewayErr.Details.Reason == reason {
			return
		}
	}
	t.Fatalf("error reason = %q, want one of %v (error %v)", gatewayErr.Details.Reason, reasons, err)
}
