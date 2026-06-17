package grpcapi

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	"github.com/Dirard/codex-runtime/gateway/internal/workflowstorage"
	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestWorkflowInitMaterializesAndNoOpsSameFingerprint(t *testing.T) {
	runtime := &fakeChatRuntimeStartService{}
	service, storageRoot := newTestWorkflowRuntimeService(t, runtime)
	pkg := testWorkflowPackage(t, "team-a", "writer", "name = \"writer\"\n")

	response, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{
		WorkflowPackage: pkg,
		ClientRequestId: "init-1",
		IdempotencyKey:  "idem-1",
		AllowMcpReload:  false,
	})
	if err != nil {
		t.Fatalf("InitWorkflow() error = %v", err)
	}
	if !response.GetCreated() || response.GetUpdated() || response.GetNoOp() {
		t.Fatalf("InitWorkflow() flags created=%v updated=%v no_op=%v, want first materialization", response.GetCreated(), response.GetUpdated(), response.GetNoOp())
	}
	if response.GetStatus().GetLifecycle() != pb.WorkflowLifecycle_WORKFLOW_LIFECYCLE_READY {
		t.Fatalf("workflow lifecycle = %s, want ready", response.GetStatus().GetLifecycle())
	}
	currentConfig := filepath.Join(storageRoot, response.GetWorkflow().GetStorageKey(), "current", "config.toml")
	if _, err := os.Stat(currentConfig); err != nil {
		t.Fatalf("materialized config missing at %s: %v", currentConfig, err)
	}

	record := assertWorkflowRuntimeRecord(t, service, "team-a", "writer")
	if record.runtimeStarts != 1 {
		t.Fatalf("runtimeStarts = %d, want 1", record.runtimeStarts)
	}
	if !strings.HasPrefix(record.root, storageRoot) {
		t.Fatalf("record root %q is outside workflow storage %q", record.root, storageRoot)
	}
	if !strings.HasPrefix(record.runtimeHome, filepath.Join(record.root, "runtime")) || !strings.HasPrefix(record.cwd, filepath.Join(record.root, "runtime")) {
		t.Fatalf("runtime paths home=%q cwd=%q, want under workflow runtime directory", record.runtimeHome, record.cwd)
	}

	second, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{
		WorkflowPackage: pkg,
		ClientRequestId: "init-2",
		IdempotencyKey:  "idem-2",
	})
	if err != nil {
		t.Fatalf("second InitWorkflow() error = %v", err)
	}
	if !second.GetNoOp() || second.GetCreated() || second.GetUpdated() {
		t.Fatalf("second InitWorkflow() flags created=%v updated=%v no_op=%v, want no-op", second.GetCreated(), second.GetUpdated(), second.GetNoOp())
	}
	record = assertWorkflowRuntimeRecord(t, service, "team-a", "writer")
	if record.runtimeStarts != 1 {
		t.Fatalf("runtimeStarts after no-op = %d, want 1", record.runtimeStarts)
	}
}

func TestWorkflowInitConcurrentSamePackageStartsOnce(t *testing.T) {
	service, _ := newTestWorkflowRuntimeService(t, &fakeChatRuntimeStartService{})
	pkg := testWorkflowPackage(t, "team-a", "concurrent", "name = \"concurrent\"\n")

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	created := make(chan bool, workers)
	noOps := make(chan bool, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: pkg})
			if err != nil {
				errs <- err
				return
			}
			created <- response.GetCreated()
			noOps <- response.GetNoOp()
		}()
	}
	wg.Wait()
	close(errs)
	close(created)
	close(noOps)

	for err := range errs {
		t.Fatalf("concurrent InitWorkflow() error = %v", err)
	}
	createdCount := 0
	for value := range created {
		if value {
			createdCount++
		}
	}
	noOpCount := 0
	for value := range noOps {
		if value {
			noOpCount++
		}
	}
	if createdCount != 1 || noOpCount != workers-1 {
		t.Fatalf("created=%d no_op=%d, want one create and %d no-ops", createdCount, noOpCount, workers-1)
	}
	record := assertWorkflowRuntimeRecord(t, service, "team-a", "concurrent")
	if record.runtimeStarts != 1 {
		t.Fatalf("runtimeStarts = %d, want one serialized startup", record.runtimeStarts)
	}
}

func TestWorkflowChatProxyUsesDynamicRegistryAndPreservesChatIdentity(t *testing.T) {
	runtime := &fakeChatRuntimeStartService{}
	launcher := &fakeWorkflowRuntimeLauncher{}
	service, _ := newTestWorkflowRuntimeService(t, runtime, func(options *WorkflowRuntimeServiceOptions) {
		options.Launcher = launcher
	})
	pkg := testWorkflowPackage(t, "team-a", "chat", "name = \"chat\"\n")
	if _, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: pkg}); err != nil {
		t.Fatalf("InitWorkflow() error = %v", err)
	}
	record := assertWorkflowRuntimeRecord(t, service, "team-a", "chat")
	if record.sessionGroupID == "sg-1" || record.workspaceID == "ws-1" {
		t.Fatalf("workflow record used static chat ids: session=%q workspace=%q", record.sessionGroupID, record.workspaceID)
	}
	firstLaunch := launcher.lastEnsure(t)
	if firstLaunch.SessionGroupID != record.sessionGroupID || firstLaunch.WorkspaceID != record.workspaceID || firstLaunch.CWD != record.cwd || firstLaunch.ProcessEpoch != record.processEpoch {
		t.Fatalf("workflow launch = %#v, want dynamic record ids and runtime cwd", firstLaunch)
	}

	status := workflowTestChatStatus(record, testCodexThreadID, "turn-1", domain.ChatTurnLifecycleInProgress)
	runtime.response = domain.StartChatRunResponse{
		ChatID:            testCodexThreadID,
		RunID:             "turn-1",
		SessionGroupID:    record.sessionGroupID,
		WorkspaceID:       record.workspaceID,
		LastEventID:       1,
		EventCursor:       "event-1",
		FirstTurnAccepted: true,
		ProcessEpoch:      record.processEpoch,
	}
	runtime.runResponse = domain.RunChatTurnResponse{
		ChatID:         testCodexThreadID,
		RunID:          "turn-2",
		SessionGroupID: record.sessionGroupID,
		WorkspaceID:    record.workspaceID,
		Status:         workflowTestChatStatus(record, testCodexThreadID, "turn-2", domain.ChatTurnLifecycleInProgress),
		LastEventID:    2,
		EventCursor:    "event-2",
		TurnAccepted:   true,
	}
	runtime.getResponse = domain.GetChatResponse{
		Chat: domain.Chat{
			ChatID:          testCodexThreadID,
			SessionGroupID:  record.sessionGroupID,
			WorkspaceID:     record.workspaceID,
			ThreadLifecycle: domain.ChatThreadLifecycleActiveRunning,
			Capabilities:    testChatCapabilities(),
		},
		Status: status,
	}
	runtime.historyResponse = domain.GetChatHistoryResponse{
		ChatID:        testCodexThreadID,
		ReturnedDepth: domain.ChatHistoryDepthTurnSummary,
		Capability:    domain.ChatCapabilitySupported,
		Turns: []domain.ChatTurnSummary{{
			RunID:     "turn-1",
			Lifecycle: domain.ChatTurnLifecycleCompleted,
			Summary:   "done",
		}},
	}
	runtime.stream = &fakeChatEventStream{messages: []StreamChatEventsMessage{{
		SessionGroupID: record.sessionGroupID,
		Event: &domain.ChatEvent{
			EventID:         1,
			EventCursor:     "event-1",
			ChatID:          testCodexThreadID,
			SessionGroupID:  record.sessionGroupID,
			WorkspaceID:     record.workspaceID,
			RunID:           "turn-2",
			CreatedAtUnixMS: 1,
			Terminal: &domain.ChatTerminal{
				State:          domain.ChatTurnLifecycleCompleted,
				DisplayMessage: "done",
			},
		},
	}}}

	started, err := service.StartWorkflowChatRun(context.Background(), &pb.StartWorkflowChatRunRequest{
		Workflow:        record.selector(),
		Prompt:          "hello",
		ClientMessageId: "client-message-1",
		IdempotencyKey:  "idem-1",
	})
	if err != nil {
		t.Fatalf("StartWorkflowChatRun() error = %v", err)
	}
	if started.GetChatId() != testCodexThreadID || started.GetRunId() != "turn-1" {
		t.Fatalf("StartWorkflowChatRun() = %#v, want workflow chat identity", started)
	}
	if runtime.command.SessionGroupID != record.sessionGroupID || runtime.command.WorkspaceID != record.workspaceID {
		t.Fatalf("StartWorkflowChatRun command session=%q workspace=%q, want workflow registry ids", runtime.command.SessionGroupID, runtime.command.WorkspaceID)
	}

	continued, err := service.RunWorkflowChatTurn(context.Background(), &pb.RunWorkflowChatTurnRequest{
		Workflow:        record.selector(),
		ChatId:          testCodexThreadID,
		Prompt:          "continue",
		ClientMessageId: "client-message-2",
		IdempotencyKey:  "idem-2",
	})
	if err != nil {
		t.Fatalf("RunWorkflowChatTurn() error = %v", err)
	}
	if continued.GetChatId() != testCodexThreadID || runtime.runCommand.ChatID != testCodexThreadID {
		t.Fatalf("RunWorkflowChatTurn chat id response=%q command=%q, want preserved chat id", continued.GetChatId(), runtime.runCommand.ChatID)
	}
	activeRecord := assertWorkflowRuntimeRecord(t, service, "team-a", "chat")
	if !activeRecord.hasActiveRuns() {
		t.Fatal("workflow active run was not tracked before terminal stream event")
	}

	got, err := service.GetWorkflowChat(context.Background(), &pb.GetWorkflowChatRequest{Workflow: record.selector(), ChatId: testCodexThreadID})
	if err != nil {
		t.Fatalf("GetWorkflowChat() error = %v", err)
	}
	if got.GetChat().GetChatId() != testCodexThreadID {
		t.Fatalf("GetWorkflowChat chat_id = %q, want %q", got.GetChat().GetChatId(), testCodexThreadID)
	}

	history, err := service.GetWorkflowChatHistory(context.Background(), &pb.GetWorkflowChatHistoryRequest{Workflow: record.selector(), ChatId: testCodexThreadID})
	if err != nil {
		t.Fatalf("GetWorkflowChatHistory() error = %v", err)
	}
	if history.GetChatId() != testCodexThreadID || len(history.GetTurns()) != 1 {
		t.Fatalf("GetWorkflowChatHistory() chat_id=%q turns=%d, want preserved history", history.GetChatId(), len(history.GetTurns()))
	}

	stream := &fakeStreamWorkflowChatEventsServer{ctx: context.Background()}
	err = service.StreamWorkflowChatEvents(&pb.StreamWorkflowChatEventsRequest{
		Workflow: record.selector(),
		ChatId:   testCodexThreadID,
		Cursor:   &pb.StreamWorkflowChatEventsRequest_FromStart{FromStart: &pb.ChatFromStartCursor{}},
	}, stream)
	if err != nil {
		t.Fatalf("StreamWorkflowChatEvents() error = %v", err)
	}
	if len(stream.sent) != 1 || stream.sent[0].GetEvent().GetChatId() != testCodexThreadID {
		t.Fatalf("stream sent %#v, want one event for workflow chat", stream.sent)
	}
	if runtime.streamCommand.SessionGroupID != record.sessionGroupID || runtime.streamCommand.WorkspaceID != record.workspaceID {
		t.Fatalf("stream command session=%q workspace=%q, want workflow registry ids", runtime.streamCommand.SessionGroupID, runtime.streamCommand.WorkspaceID)
	}
	clearedRecord := assertWorkflowRuntimeRecord(t, service, "team-a", "chat")
	if clearedRecord.hasActiveRuns() {
		t.Fatalf("workflow active runs = %#v, want cleared after terminal stream event", clearedRecord.activeRunSnapshot())
	}
}

func TestWorkflowInitRejectsInvalidPackageBeforeRuntimeOrCommit(t *testing.T) {
	runtime := &fakeChatRuntimeStartService{}
	service, storageRoot := newTestWorkflowRuntimeService(t, runtime)
	pkg := testWorkflowPackage(t, "team-a", "invalid", "name = \"invalid\"\n")
	pkg.Files[0].Sha256 = "bad-hash"

	_, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: pkg})
	details := assertWorkflowRuntimeStatus(t, err, codes.InvalidArgument, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_INVALID_WORKFLOW_PACKAGE)
	if details.GetReason() != "hash_mismatch" {
		t.Fatalf("workflow error reason = %q, want hash_mismatch", details.GetReason())
	}
	if runtime.calls != 0 {
		t.Fatalf("runtime calls = %d, want validation failure before chat runtime", runtime.calls)
	}
	assertWorkflowRootMissing(t, storageRoot, "team-a", "invalid")
}

func TestWorkflowInitRejectsMCPConfigByDefaultBeforeCommit(t *testing.T) {
	runtime := &fakeChatRuntimeStartService{}
	service, storageRoot := newTestWorkflowRuntimeService(t, runtime)
	pkg := testWorkflowPackage(t, "team-a", "mcp", "[mcp_servers.writer]\ncommand = \"writer\"\n")

	_, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: pkg, AllowMcpReload: true})
	details := assertWorkflowRuntimeStatus(t, err, codes.FailedPrecondition, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_MCP_UNAVAILABLE)
	if details.GetReason() != "mcp_reload_unsupported" {
		t.Fatalf("workflow error reason = %q, want mcp_reload_unsupported", details.GetReason())
	}
	if runtime.calls != 0 {
		t.Fatalf("runtime calls = %d, want MCP rejection before chat runtime", runtime.calls)
	}
	if _, ok := service.recordByKey(workflowstorage.SafeStorageKey("team-a", "mcp")); ok {
		t.Fatal("workflow registry record exists after rejected MCP package")
	}
	assertWorkflowRootMissing(t, storageRoot, "team-a", "mcp")
}

func TestWorkflowUpdateStagesConfigAndRestartRequiredBlocksRuns(t *testing.T) {
	runtime := &fakeChatRuntimeStartService{}
	service, storageRoot := newTestWorkflowRuntimeService(t, runtime)
	initial := testWorkflowPackage(t, "team-a", "updater", "name = \"v1\"\n")
	first, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: initial})
	if err != nil {
		t.Fatalf("InitWorkflow(v1) error = %v", err)
	}
	oldFingerprint := first.GetWorkflow().GetActivePackageFingerprint()
	oldEpoch := first.GetStatus().GetProcessEpoch()

	changed := testWorkflowPackage(t, "team-a", "updater", "name = \"v2\"\n")
	update, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: changed})
	if err != nil {
		t.Fatalf("InitWorkflow(v2) error = %v", err)
	}
	if !update.GetUpdated() || !update.GetRestartRequired() {
		t.Fatalf("update flags updated=%v restart_required=%v, want staged restart", update.GetUpdated(), update.GetRestartRequired())
	}
	if update.GetWorkflow().GetStorageKey() != first.GetWorkflow().GetStorageKey() {
		t.Fatalf("storage key changed from %q to %q", first.GetWorkflow().GetStorageKey(), update.GetWorkflow().GetStorageKey())
	}
	if update.GetWorkflow().GetActivePackageFingerprint() != oldFingerprint || update.GetWorkflow().GetPendingPackageFingerprint() != changed.GetPackageFingerprint() {
		t.Fatalf("update fingerprints active=%q pending=%q, want active old %q pending new %q", update.GetWorkflow().GetActivePackageFingerprint(), update.GetWorkflow().GetPendingPackageFingerprint(), oldFingerprint, changed.GetPackageFingerprint())
	}
	storageKey := first.GetWorkflow().GetStorageKey()
	assertFileContains(t, filepath.Join(storageRoot, storageKey, "current", "config.toml"), "v1")
	assertFileContains(t, filepath.Join(storageRoot, storageKey, "pending", "config.toml"), "v2")

	_, err = service.StartWorkflowChatRun(context.Background(), &pb.StartWorkflowChatRunRequest{
		Workflow:        update.GetWorkflow().GetWorkflow(),
		Prompt:          "hello",
		ClientMessageId: "client-message-1",
		IdempotencyKey:  "idem-1",
	})
	assertWorkflowRuntimeStatus(t, err, codes.FailedPrecondition, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_RESTART_REQUIRED)

	restarted, err := service.RestartWorkflow(context.Background(), &pb.RestartWorkflowRequest{Workflow: update.GetWorkflow().GetWorkflow()})
	if err != nil {
		t.Fatalf("RestartWorkflow() error = %v", err)
	}
	if !restarted.GetRestartCompleted() || restarted.GetStatus().GetProcessEpoch() == oldEpoch {
		t.Fatalf("RestartWorkflow() completed=%v epoch=%q old=%q, want new epoch", restarted.GetRestartCompleted(), restarted.GetStatus().GetProcessEpoch(), oldEpoch)
	}
	if restarted.GetStatus().GetActivePackageFingerprint() != changed.GetPackageFingerprint() || restarted.GetStatus().GetRestartRequired() {
		t.Fatalf("restart status active=%q restart_required=%v, want promoted pending", restarted.GetStatus().GetActivePackageFingerprint(), restarted.GetStatus().GetRestartRequired())
	}
	assertFileContains(t, filepath.Join(storageRoot, storageKey, "current", "config.toml"), "v2")
}

func TestWorkflowFailedUpdateKeepsPreviousReadyAndRecordsRedactedError(t *testing.T) {
	service, storageRoot := newTestWorkflowRuntimeService(t, &fakeChatRuntimeStartService{})
	initial := testWorkflowPackage(t, "team-a", "rollback", "name = \"ready\"\n")
	first, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: initial})
	if err != nil {
		t.Fatalf("InitWorkflow(v1) error = %v", err)
	}

	bad := testWorkflowPackage(t, "team-a", "rollback", "name = \"bad\"\n")
	bad.Files[0].Sha256 = "bad-hash"
	_, err = service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: bad})
	assertWorkflowRuntimeStatus(t, err, codes.InvalidArgument, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_INVALID_WORKFLOW_PACKAGE)

	statusResponse, err := service.GetWorkflowStatus(context.Background(), &pb.GetWorkflowStatusRequest{Workflow: first.GetWorkflow().GetWorkflow()})
	if err != nil {
		t.Fatalf("GetWorkflowStatus() error = %v", err)
	}
	status := statusResponse.GetStatus()
	if status.GetLifecycle() != pb.WorkflowLifecycle_WORKFLOW_LIFECYCLE_READY || status.GetActivePackageFingerprint() != first.GetWorkflow().GetActivePackageFingerprint() || status.GetPendingPackageFingerprint() != "" {
		t.Fatalf("status after failed update = %#v, want previous ready revision only", status)
	}
	if status.GetLastError().GetCode() != pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_INVALID_WORKFLOW_PACKAGE || strings.Contains(status.GetLastError().String(), "name =") {
		t.Fatalf("last_error = %#v, want redacted invalid package details", status.GetLastError())
	}
	assertFileContains(t, filepath.Join(storageRoot, first.GetWorkflow().GetStorageKey(), "current", "config.toml"), "ready")
}

func TestWorkflowDeleteRefusesActiveAndForceInterrupts(t *testing.T) {
	runtime := &fakeChatRuntimeStartService{}
	launcher := &fakeWorkflowRuntimeLauncher{}
	service, storageRoot := newTestWorkflowRuntimeService(t, runtime, func(options *WorkflowRuntimeServiceOptions) {
		options.Launcher = launcher
	})
	pkg := testWorkflowPackage(t, "team-a", "delete-active", "name = \"delete\"\n")
	if _, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: pkg}); err != nil {
		t.Fatalf("InitWorkflow() error = %v", err)
	}
	record := assertWorkflowRuntimeRecord(t, service, "team-a", "delete-active")
	runtime.response = domain.StartChatRunResponse{
		ChatID:            testCodexThreadID,
		RunID:             "turn-delete",
		SessionGroupID:    record.sessionGroupID,
		WorkspaceID:       record.workspaceID,
		LastEventID:       1,
		EventCursor:       "cursor-delete",
		FirstTurnAccepted: true,
		ProcessEpoch:      record.processEpoch,
	}
	if _, err := service.StartWorkflowChatRun(context.Background(), &pb.StartWorkflowChatRunRequest{
		Workflow:        record.selector(),
		Prompt:          "hello",
		ClientMessageId: "client-message-delete",
		IdempotencyKey:  "idem-delete",
	}); err != nil {
		t.Fatalf("StartWorkflowChatRun() error = %v", err)
	}

	_, err := service.DeleteWorkflow(context.Background(), &pb.DeleteWorkflowRequest{Workflow: record.selector(), DeleteMaterializedState: true})
	assertWorkflowRuntimeStatus(t, err, codes.FailedPrecondition, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_ACTIVE_WORK_REFUSED)
	if _, ok := service.recordByKey(record.storageKey); !ok {
		t.Fatal("workflow record was deleted after refused active delete")
	}

	deleted, err := service.DeleteWorkflow(context.Background(), &pb.DeleteWorkflowRequest{Workflow: record.selector(), Force: true, DeleteMaterializedState: true, CleanupRuntime: true})
	if err != nil {
		t.Fatalf("force DeleteWorkflow() error = %v", err)
	}
	if !deleted.GetDeleted() || !deleted.GetCleanupScheduled() || runtime.interruptCalls == 0 {
		t.Fatalf("force delete response deleted=%v cleanup=%v interruptCalls=%d, want explicit forced cleanup", deleted.GetDeleted(), deleted.GetCleanupScheduled(), runtime.interruptCalls)
	}
	closed := launcher.lastClose(t)
	if closed.SessionGroupID != record.sessionGroupID || closed.ProcessEpoch != record.processEpoch {
		t.Fatalf("closed workflow runtime = %#v, want deleted record runtime", closed)
	}
	assertWorkflowRootMissing(t, storageRoot, "team-a", "delete-active")
}

func TestWorkflowGracefulRestartDeadlineAndForcePromotesPending(t *testing.T) {
	runtime := &fakeChatRuntimeStartService{}
	service, storageRoot := newTestWorkflowRuntimeService(t, runtime)
	initial := testWorkflowPackage(t, "team-a", "restart-active", "name = \"v1\"\n")
	if _, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: initial}); err != nil {
		t.Fatalf("InitWorkflow(v1) error = %v", err)
	}
	record := assertWorkflowRuntimeRecord(t, service, "team-a", "restart-active")
	runtime.response = domain.StartChatRunResponse{
		ChatID:            testCodexThreadID,
		RunID:             "turn-restart",
		SessionGroupID:    record.sessionGroupID,
		WorkspaceID:       record.workspaceID,
		LastEventID:       1,
		EventCursor:       "cursor-restart",
		FirstTurnAccepted: true,
		ProcessEpoch:      record.processEpoch,
	}
	if _, err := service.StartWorkflowChatRun(context.Background(), &pb.StartWorkflowChatRunRequest{
		Workflow:        record.selector(),
		Prompt:          "hello",
		ClientMessageId: "client-message-restart",
		IdempotencyKey:  "idem-restart",
	}); err != nil {
		t.Fatalf("StartWorkflowChatRun() error = %v", err)
	}
	changed := testWorkflowPackage(t, "team-a", "restart-active", "name = \"v2\"\n")
	if _, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: changed}); err != nil {
		t.Fatalf("InitWorkflow(v2) error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err := service.RestartWorkflow(ctx, &pb.RestartWorkflowRequest{Workflow: record.selector()})
	assertWorkflowRuntimeStatus(t, err, codes.DeadlineExceeded, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_ACTIVE_WORK_REFUSED)

	forced, err := service.RestartWorkflow(context.Background(), &pb.RestartWorkflowRequest{Workflow: record.selector(), Force: true})
	if err != nil {
		t.Fatalf("force RestartWorkflow() error = %v", err)
	}
	if !forced.GetActiveWorkInterrupted() || runtime.interruptCalls == 0 || forced.GetStatus().GetActivePackageFingerprint() != changed.GetPackageFingerprint() {
		t.Fatalf("force restart active_interrupted=%v interruptCalls=%d activeFingerprint=%q, want interrupted promoted pending", forced.GetActiveWorkInterrupted(), runtime.interruptCalls, forced.GetStatus().GetActivePackageFingerprint())
	}
	assertFileContains(t, filepath.Join(storageRoot, record.storageKey, "current", "config.toml"), "v2")
}

func TestWorkflowMCPPolicyRejectsAndReloadFallbackStagesRestart(t *testing.T) {
	service, _ := newTestWorkflowRuntimeService(t, &fakeChatRuntimeStartService{}, func(options *WorkflowRuntimeServiceOptions) {
		options.AllowMCP = true
	})
	initial := testWorkflowPackage(t, "team-a", "mcp-policy", "name = \"base\"\n")
	first, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: initial})
	if err != nil {
		t.Fatalf("InitWorkflow(base) error = %v", err)
	}

	disallowed := testWorkflowPackage(t, "team-a", "mcp-policy", "[mcp_servers.writer]\ncommand = \"../writer\"\n")
	_, err = service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: disallowed, AllowMcpReload: true})
	assertWorkflowRuntimeStatus(t, err, codes.PermissionDenied, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_MCP_UNAVAILABLE)
	statusResponse, err := service.GetWorkflowStatus(context.Background(), &pb.GetWorkflowStatusRequest{Workflow: first.GetWorkflow().GetWorkflow()})
	if err != nil {
		t.Fatalf("GetWorkflowStatus() after rejected MCP policy error = %v", err)
	}
	if statusResponse.GetStatus().GetActivePackageFingerprint() != first.GetWorkflow().GetActivePackageFingerprint() || statusResponse.GetStatus().GetPendingPackageFingerprint() != "" {
		t.Fatalf("status after rejected MCP = %#v, want unchanged active revision", statusResponse.GetStatus())
	}

	allowed := testWorkflowPackage(t, "team-a", "mcp-policy", "[mcp_servers.writer]\ncommand = \"writer\"\n")
	update, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: allowed, AllowMcpReload: true})
	if err != nil {
		t.Fatalf("InitWorkflow(allowed MCP fallback) error = %v", err)
	}
	if !update.GetRestartRequired() || update.GetStatus().GetMcpReloadState() != pb.WorkflowMcpReloadState_WORKFLOW_MCP_RELOAD_STATE_UNSUPPORTED {
		t.Fatalf("MCP fallback restart_required=%v mcp_state=%s, want unsupported restart fallback", update.GetRestartRequired(), update.GetStatus().GetMcpReloadState())
	}
}

func TestWorkflowMCPReloadAppliedWithReloader(t *testing.T) {
	reloader := &fakeWorkflowMCPReloader{}
	service, storageRoot := newTestWorkflowRuntimeService(t, &fakeChatRuntimeStartService{}, func(options *WorkflowRuntimeServiceOptions) {
		options.AllowMCP = true
		options.MCPReloader = reloader
	})
	initial := testWorkflowPackage(t, "team-a", "mcp-reload", "name = \"base\"\n")
	first, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: initial})
	if err != nil {
		t.Fatalf("InitWorkflow(base) error = %v", err)
	}
	allowed := testWorkflowPackage(t, "team-a", "mcp-reload", "[mcp_servers.writer]\ncommand = \"writer\"\n")
	update, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: allowed, AllowMcpReload: true})
	if err != nil {
		t.Fatalf("InitWorkflow(MCP reload) error = %v", err)
	}
	if !update.GetMcpReloadApplied() || update.GetRestartRequired() || update.GetStatus().GetMcpReloadState() != pb.WorkflowMcpReloadState_WORKFLOW_MCP_RELOAD_STATE_APPLIED || reloader.calls != 1 {
		t.Fatalf("MCP reload applied=%v restart_required=%v state=%s calls=%d, want applied without restart", update.GetMcpReloadApplied(), update.GetRestartRequired(), update.GetStatus().GetMcpReloadState(), reloader.calls)
	}
	assertFileContains(t, filepath.Join(storageRoot, first.GetWorkflow().GetStorageKey(), "current", "config.toml"), "mcp_servers.writer")
}

func TestWorkflowMCPReloadFailureReturnsGatewayNextAction(t *testing.T) {
	reloader := &fakeWorkflowMCPReloader{err: status.Error(codes.Unavailable, "mcp helper unavailable")}
	service, _ := newTestWorkflowRuntimeService(t, &fakeChatRuntimeStartService{}, func(options *WorkflowRuntimeServiceOptions) {
		options.AllowMCP = true
		options.MCPReloader = reloader
	})
	initial := testWorkflowPackage(t, "team-a", "mcp-reload-failure", "name = \"base\"\n")
	if _, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: initial}); err != nil {
		t.Fatalf("InitWorkflow(base) error = %v", err)
	}
	allowed := testWorkflowPackage(t, "team-a", "mcp-reload-failure", "[mcp_servers.writer]\ncommand = \"writer\"\n")
	_, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: allowed, AllowMcpReload: true})
	details := assertWorkflowRuntimeStatus(t, err, codes.Unavailable, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_MCP_NOT_REACHABLE)
	if details.GetReason() != "mcp_reload_failed" {
		t.Fatalf("workflow MCP reason = %q, want mcp_reload_failed", details.GetReason())
	}
	nextAction := details.GetNextAction()
	for _, want := range []string{"gateway-side MCP command", "network", "env"} {
		if !strings.Contains(nextAction, want) {
			t.Fatalf("workflow MCP next action = %q, want it to mention %q", nextAction, want)
		}
	}
	if reloader.calls != 1 {
		t.Fatalf("MCP reload calls = %d, want 1", reloader.calls)
	}
}

func TestWorkflowWriterNotesMCPPolicyAllowsOnlyMaterializedHelperPath(t *testing.T) {
	service, storageRoot := newTestWorkflowRuntimeService(t, &fakeChatRuntimeStartService{}, func(options *WorkflowRuntimeServiceOptions) {
		options.AllowMCP = true
	})
	allowed := testWorkflowPackageWithFiles(t, "examples", "writer-notes", "[mcp_servers.writer_notes_search]\ncommand = \"tools/writer_notes_search\"\n", []workflowstorage.PackageFile{
		{Path: "tools/writer_notes_search", Contents: []byte("#!/usr/bin/env sh\nexit 0\n"), Executable: true},
		{Path: "references/harbor-fire-notes.md", Contents: []byte("# Harbor Fire Notes\n")},
	})

	response, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: allowed, AllowMcpReload: true})
	if err != nil {
		t.Fatalf("InitWorkflow(writer_notes_search helper) error = %v", err)
	}
	helperPath := filepath.Join(storageRoot, response.GetWorkflow().GetStorageKey(), "current", "tools", "writer_notes_search")
	assertFileContains(t, helperPath, "exit 0")

	disallowed := testWorkflowPackageWithFiles(t, "examples", "writer-notes-bad", "[mcp_servers.writer_notes_search]\ncommand = \"../writer_notes_search\"\n", []workflowstorage.PackageFile{
		{Path: "references/harbor-fire-notes.md", Contents: []byte("# Harbor Fire Notes\n")},
	})
	_, err = service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: disallowed, AllowMcpReload: true})
	details := assertWorkflowRuntimeStatus(t, err, codes.PermissionDenied, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_MCP_UNAVAILABLE)
	if details.GetReason() != "mcp_stdio_command_rejected" {
		t.Fatalf("workflow policy reason = %q, want mcp_stdio_command_rejected", details.GetReason())
	}
	assertWorkflowRootMissing(t, storageRoot, "examples", "writer-notes-bad")
}

func TestWorkflowStaleEventCursorAfterRestartReturnsReplayUnavailable(t *testing.T) {
	runtime := &fakeChatRuntimeStartService{}
	service, _ := newTestWorkflowRuntimeService(t, runtime)
	initial := testWorkflowPackage(t, "team-a", "stale-cursor", "name = \"v1\"\n")
	if _, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: initial}); err != nil {
		t.Fatalf("InitWorkflow(v1) error = %v", err)
	}
	record := assertWorkflowRuntimeRecord(t, service, "team-a", "stale-cursor")
	runtime.response = domain.StartChatRunResponse{
		ChatID:            testCodexThreadID,
		RunID:             "turn-cursor",
		SessionGroupID:    record.sessionGroupID,
		WorkspaceID:       record.workspaceID,
		LastEventID:       1,
		EventCursor:       "cursor-1",
		FirstTurnAccepted: true,
		ProcessEpoch:      record.processEpoch,
	}
	started, err := service.StartWorkflowChatRun(context.Background(), &pb.StartWorkflowChatRunRequest{
		Workflow:        record.selector(),
		Prompt:          "hello",
		ClientMessageId: "client-message-cursor",
		IdempotencyKey:  "idem-cursor",
	})
	if err != nil {
		t.Fatalf("StartWorkflowChatRun() error = %v", err)
	}
	oldCursor := started.GetEventCursor()
	changed := testWorkflowPackage(t, "team-a", "stale-cursor", "name = \"v2\"\n")
	if _, err := service.InitWorkflow(context.Background(), &pb.InitWorkflowRequest{WorkflowPackage: changed}); err != nil {
		t.Fatalf("InitWorkflow(v2) error = %v", err)
	}
	if _, err := service.RestartWorkflow(context.Background(), &pb.RestartWorkflowRequest{Workflow: record.selector(), Force: true}); err != nil {
		t.Fatalf("force RestartWorkflow() error = %v", err)
	}

	err = service.StreamWorkflowChatEvents(&pb.StreamWorkflowChatEventsRequest{
		Workflow: record.selector(),
		ChatId:   testCodexThreadID,
		Cursor:   &pb.StreamWorkflowChatEventsRequest_AfterEventCursor{AfterEventCursor: oldCursor},
	}, &fakeStreamWorkflowChatEventsServer{ctx: context.Background()})
	assertWorkflowRuntimeStatus(t, err, codes.FailedPrecondition, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_REPLAY_UNAVAILABLE)
}

func newTestWorkflowRuntimeService(t *testing.T, runtime ChatRuntimeService, configure ...func(*WorkflowRuntimeServiceOptions)) (*workflowRuntimeService, string) {
	t.Helper()
	storageRoot := t.TempDir()
	manager, err := workflowstorage.NewManager(storageRoot, workflowstorage.PackageLimits{})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	options := WorkflowRuntimeServiceOptions{
		Enabled: true,
		Storage: manager,
		Runtime: runtime,
	}
	for _, apply := range configure {
		apply(&options)
	}
	service := NewWorkflowRuntimeService(options).(*workflowRuntimeService)
	return service, storageRoot
}

func testWorkflowPackage(t *testing.T, namespace string, workflowID string, config string) *pb.WorkflowPackage {
	t.Helper()
	pkg, err := workflowstorage.NewProtoPackage(namespace, workflowID, []workflowstorage.PackageFile{{
		Path:     "config.toml",
		Contents: []byte(config),
	}})
	if err != nil {
		t.Fatalf("NewProtoPackage() error = %v", err)
	}
	return pkg
}

func testWorkflowPackageWithFiles(t *testing.T, namespace string, workflowID string, config string, files []workflowstorage.PackageFile) *pb.WorkflowPackage {
	t.Helper()
	allFiles := []workflowstorage.PackageFile{{Path: "config.toml", Contents: []byte(config)}}
	allFiles = append(allFiles, files...)
	pkg, err := workflowstorage.NewProtoPackage(namespace, workflowID, allFiles)
	if err != nil {
		t.Fatalf("NewProtoPackage() error = %v", err)
	}
	return pkg
}

func assertWorkflowRuntimeRecord(t *testing.T, service *workflowRuntimeService, namespace string, workflowID string) *workflowRuntimeRecord {
	t.Helper()
	record, ok := service.recordByKey(workflowstorage.SafeStorageKey(namespace, workflowID))
	if !ok {
		t.Fatalf("workflow record %s/%s missing", namespace, workflowID)
	}
	return record
}

func assertWorkflowRootMissing(t *testing.T, storageRoot string, namespace string, workflowID string) {
	t.Helper()
	root := filepath.Join(storageRoot, workflowstorage.SafeStorageKey(namespace, workflowID))
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workflow root %s exists or stat failed with %v, want missing", root, err)
	}
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if !strings.Contains(string(contents), want) {
		t.Fatalf("file %s contents %q do not contain %q", path, string(contents), want)
	}
}

func workflowTestChatStatus(record *workflowRuntimeRecord, chatID string, runID string, runLifecycle domain.ChatTurnLifecycle) domain.ChatStatus {
	return domain.ChatStatus{
		ChatID:              chatID,
		SessionGroupID:      record.sessionGroupID,
		WorkspaceID:         record.workspaceID,
		LookupValid:         true,
		ThreadLifecycle:     domain.ChatThreadLifecycleActiveRunning,
		CurrentRunLifecycle: runLifecycle,
		CurrentRunID:        runID,
		LastRunID:           runID,
		Capabilities:        testChatCapabilities(),
		GatewayLocal: domain.ChatGatewayLocalState{
			Live:            true,
			ReplayAvailable: true,
			ProcessEpoch:    record.processEpoch,
		},
		LastEventID: 1,
	}
}

func assertWorkflowRuntimeStatus(t *testing.T, err error, code codes.Code, workflowCode pb.WorkflowErrorCode) *pb.WorkflowErrorDetails {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s/%s", code, workflowCode)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%#v) ok = false", err)
	}
	if st.Code() != code {
		t.Fatalf("status code = %s, want %s (%v)", st.Code(), code, err)
	}
	for _, detail := range st.Details() {
		if workflowDetails, ok := detail.(*pb.WorkflowErrorDetails); ok {
			if workflowDetails.GetCode() != workflowCode {
				t.Fatalf("workflow error code = %s, want %s", workflowDetails.GetCode(), workflowCode)
			}
			return workflowDetails
		}
	}
	t.Fatalf("workflow error details missing from %v", st.Details())
	return nil
}

type fakeStreamWorkflowChatEventsServer struct {
	ctx  context.Context
	sent []*pb.StreamWorkflowChatEventsResponse
}

func (s *fakeStreamWorkflowChatEventsServer) Send(response *pb.StreamWorkflowChatEventsResponse) error {
	s.sent = append(s.sent, response)
	return nil
}

func (s *fakeStreamWorkflowChatEventsServer) SetHeader(metadata.MD) error {
	return nil
}

func (s *fakeStreamWorkflowChatEventsServer) SendHeader(metadata.MD) error {
	return nil
}

func (s *fakeStreamWorkflowChatEventsServer) SetTrailer(metadata.MD) {}

func (s *fakeStreamWorkflowChatEventsServer) Context() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

func (s *fakeStreamWorkflowChatEventsServer) SendMsg(any) error {
	return nil
}

func (s *fakeStreamWorkflowChatEventsServer) RecvMsg(any) error {
	return io.EOF
}

type fakeWorkflowMCPReloader struct {
	calls   int
	command WorkflowMCPReloadCommand
	err     error
}

func (r *fakeWorkflowMCPReloader) ReloadWorkflowMCP(_ context.Context, command WorkflowMCPReloadCommand) error {
	r.calls++
	r.command = command
	return r.err
}

type fakeWorkflowRuntimeLauncher struct {
	mu      sync.Mutex
	ensures []WorkflowRuntimeLaunch
	closes  []WorkflowRuntimeLaunch
	err     error
}

func (l *fakeWorkflowRuntimeLauncher) EnsureWorkflowRuntime(_ context.Context, launch WorkflowRuntimeLaunch) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensures = append(l.ensures, launch)
	return l.err
}

func (l *fakeWorkflowRuntimeLauncher) CloseWorkflowRuntime(_ context.Context, launch WorkflowRuntimeLaunch) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closes = append(l.closes, launch)
	return l.err
}

func (l *fakeWorkflowRuntimeLauncher) lastEnsure(t *testing.T) WorkflowRuntimeLaunch {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.ensures) == 0 {
		t.Fatal("workflow launcher EnsureWorkflowRuntime was not called")
	}
	return l.ensures[len(l.ensures)-1]
}

func (l *fakeWorkflowRuntimeLauncher) lastClose(t *testing.T) WorkflowRuntimeLaunch {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.closes) == 0 {
		t.Fatal("workflow launcher CloseWorkflowRuntime was not called")
	}
	return l.closes[len(l.closes)-1]
}
