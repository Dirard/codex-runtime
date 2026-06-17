package codex

import (
	"context"
	"io"
	"testing"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWorkflowQuickstartSnippet(t *testing.T) {
	ctx := context.Background()
	workflowDir := createWorkflowFixture(t, "workflow-sdk")
	fakeWorkflow := newHappyWorkflowFake()
	client := newWorkflowTestClient(t, fakeWorkflow)

	workflow, err := client.InitWorkflow(ctx, WorkflowDir{
		Namespace: "team-a",
		ID:        "writer",
		Path:      workflowDir,
	})
	if err != nil {
		t.Fatalf("InitWorkflow returned error: %v", err)
	}

	chat, events, err := workflow.Run(ctx, "Draft a concise status update")
	if err != nil {
		t.Fatalf("workflow.Run returned error: %v", err)
	}
	defer events.Close()

	if _, err := chat.Run(ctx, "Make it warmer"); err != nil {
		t.Fatalf("workflow chat.Run returned error: %v", err)
	}
	if _, err := chat.GetHistory(ctx); err != nil {
		t.Fatalf("workflow chat.GetHistory returned error: %v", err)
	}
	if _, err := workflow.Restart(ctx, WithWorkflowForceRestart(true)); err != nil {
		t.Fatalf("workflow.Restart returned error: %v", err)
	}

	if fakeWorkflow.initRequest.GetWorkflowPackage().GetWorkflow().GetNamespace() != "team-a" {
		t.Fatalf("init package namespace = %q", fakeWorkflow.initRequest.GetWorkflowPackage().GetWorkflow().GetNamespace())
	}
	if fakeWorkflow.restartRequest.GetForce() != true {
		t.Fatal("workflow.Restart did not pass force=true")
	}
}

func TestWorkflowFacadeUsesWorkflowScopedRPCsAndStream(t *testing.T) {
	ctx := context.Background()
	workflowDir := createWorkflowFixture(t, "workflow-sdk")
	fakeWorkflow := newHappyWorkflowFake()
	fakeWorkflow.stream = &fakeWorkflowEventStream{
		messages: []*pb.StreamWorkflowChatEventsResponse{
			{
				Payload: &pb.StreamWorkflowChatEventsResponse_Event{
					Event: &pb.ChatEvent{EventId: 9, EventCursor: "cursor-9"},
				},
			},
		},
	}
	client := newWorkflowTestClient(t, fakeWorkflow)

	workflow, err := client.InitWorkflow(
		ctx,
		WorkflowDir{Namespace: "team-a", ID: "writer", Path: workflowDir},
		WithWorkflowClientRequestID("init-1"),
		WithWorkflowIdempotencyKey("idem-init"),
		WithWorkflowMCPReload(true),
	)
	if err != nil {
		t.Fatalf("InitWorkflow returned error: %v", err)
	}
	if workflow.Namespace != "team-a" || workflow.ID != "writer" {
		t.Fatalf("workflow identity = %s/%s", workflow.Namespace, workflow.ID)
	}
	if fakeWorkflow.initRequest.GetClientRequestId() != "init-1" || fakeWorkflow.initRequest.GetIdempotencyKey() != "idem-init" {
		t.Fatalf("init ids = %q/%q", fakeWorkflow.initRequest.GetClientRequestId(), fakeWorkflow.initRequest.GetIdempotencyKey())
	}
	if !fakeWorkflow.initRequest.GetAllowMcpReload() {
		t.Fatal("allow_mcp_reload was not forwarded")
	}
	assertAuthorization(t, fakeWorkflow.initCtx)

	chat, events, err := workflow.Run(
		ctx,
		"Start the workflow chat",
		WithClientMessageID("msg-1"),
		WithIdempotencyKey("idem-run"),
		WithInitialStreamOptions(AfterEventCursor("cursor-explicit"), WithClientSubscriberID("subscriber-1")),
	)
	if err != nil {
		t.Fatalf("workflow.Run returned error: %v", err)
	}
	if fakeWorkflow.startRequest.GetWorkflow().GetNamespace() != "team-a" || fakeWorkflow.startRequest.GetWorkflow().GetWorkflowId() != "writer" {
		t.Fatalf("start workflow selector = %#v", fakeWorkflow.startRequest.GetWorkflow())
	}
	if fakeWorkflow.startRequest.GetClientMessageId() != "msg-1" || fakeWorkflow.startRequest.GetIdempotencyKey() != "idem-run" {
		t.Fatalf("start ids = %q/%q", fakeWorkflow.startRequest.GetClientMessageId(), fakeWorkflow.startRequest.GetIdempotencyKey())
	}
	if fakeWorkflow.streamRequest.GetAfterEventCursor() != "cursor-explicit" || fakeWorkflow.streamRequest.GetClientSubscriberId() != "subscriber-1" {
		t.Fatalf("stream request = %#v", fakeWorkflow.streamRequest)
	}

	event, err := events.Recv()
	if err != nil {
		t.Fatalf("workflow events.Recv returned error: %v", err)
	}
	if event.GetEvent().GetEventCursor() != "cursor-9" {
		t.Fatalf("event cursor = %q", event.GetEvent().GetEventCursor())
	}

	if _, err := chat.Run(ctx, "Continue", WithIdempotencyKey("idem-turn")); err != nil {
		t.Fatalf("workflow chat.Run returned error: %v", err)
	}
	if fakeWorkflow.turnRequest.GetWorkflow().GetWorkflowId() != "writer" || fakeWorkflow.turnRequest.GetChatId() != "workflow-chat-1" {
		t.Fatalf("turn request = %#v", fakeWorkflow.turnRequest)
	}
	if fakeWorkflow.turnRequest.GetIdempotencyKey() != "idem-turn" {
		t.Fatalf("turn idempotency = %q", fakeWorkflow.turnRequest.GetIdempotencyKey())
	}

	history, err := chat.GetHistory(ctx, WithHistoryLimit(2))
	if err != nil {
		t.Fatalf("workflow chat.GetHistory returned error: %v", err)
	}
	if history.GetChatId() != "workflow-chat-1" || fakeWorkflow.historyRequest.GetLimit() != 2 {
		t.Fatalf("history response/request = %#v / %#v", history, fakeWorkflow.historyRequest)
	}

	existing, err := workflow.GetChat(ctx, "workflow-chat-1")
	if err != nil {
		t.Fatalf("workflow.GetChat returned error: %v", err)
	}
	if existing.workflow != workflow {
		t.Fatal("workflow.GetChat did not attach workflow handle")
	}
	fakeWorkflow.getChatRequest = nil
	status, err := existing.GetStatus(ctx)
	if err != nil {
		t.Fatalf("workflow chat.GetStatus returned error: %v", err)
	}
	if status.GetChatId() != "workflow-chat-1" {
		t.Fatalf("workflow chat.GetStatus status chat id = %q", status.GetChatId())
	}
	if fakeWorkflow.getChatRequest.GetWorkflow().GetWorkflowId() != "writer" || fakeWorkflow.getChatRequest.GetChatId() != "workflow-chat-1" {
		t.Fatalf("status request = %#v", fakeWorkflow.getChatRequest)
	}
}

func TestWorkflowPendingAndInterruptUseWorkflowScopedRPCs(t *testing.T) {
	fakeWorkflow := newHappyWorkflowFake()
	client := newWorkflowTestClient(t, fakeWorkflow)
	workflow := newTestWorkflow(client)
	chat := &Chat{ID: "workflow-chat-1", client: client, workflow: workflow}
	chat.setStatus(&pb.ChatStatus{ChatId: "workflow-chat-1", CurrentRunId: "run-1"})

	pending, err := chat.RespondApproval(context.Background(), "pending-1", "approve", WithIdempotencyKey("idem-pending"))
	if err != nil {
		t.Fatalf("RespondApproval returned error: %v", err)
	}
	if pending.GetAccepted() != true || fakeWorkflow.pendingRequest.GetWorkflow().GetWorkflowId() != "writer" {
		t.Fatalf("pending response/request = %#v / %#v", pending, fakeWorkflow.pendingRequest)
	}
	if fakeWorkflow.pendingRequest.GetApproval().GetDecisionId() != "approve" {
		t.Fatalf("approval decision = %q", fakeWorkflow.pendingRequest.GetApproval().GetDecisionId())
	}

	interrupt, err := chat.Interrupt(context.Background(), WithIdempotencyKey("idem-interrupt"))
	if err != nil {
		t.Fatalf("Interrupt returned error: %v", err)
	}
	if !interrupt.GetInterruptSent() || fakeWorkflow.interruptRequest.GetRunId() != "run-1" {
		t.Fatalf("interrupt response/request = %#v / %#v", interrupt, fakeWorkflow.interruptRequest)
	}
}

func TestWorkflowTypedErrorMapping(t *testing.T) {
	ctx := context.Background()
	workflowDir := createWorkflowFixture(t, "workflow-errors")

	client, err := NewWithClient(
		&fakeRuntimeClient{},
		WithBearerToken(testToken),
		WithSessionGroupID(testSessionGroupID),
		WithWorkspaceID(testWorkspaceID),
	)
	if err != nil {
		t.Fatalf("NewWithClient returned error: %v", err)
	}
	_, err = client.InitWorkflow(ctx, WorkflowDir{Namespace: "team-a", ID: "writer", Path: workflowDir})
	assertWorkflowSDKError(t, err, codes.Unavailable, WorkflowErrorGatewayUnavailable)

	fakeWorkflow := newHappyWorkflowFake()
	client = newWorkflowTestClient(t, fakeWorkflow)
	_, err = client.InitWorkflow(ctx, WorkflowDir{Namespace: "team-a", ID: "writer", Path: t.TempDir()})
	assertWorkflowSDKError(t, err, codes.InvalidArgument, WorkflowErrorInvalidWorkflowPackage)
	if fakeWorkflow.initRequest != nil {
		t.Fatal("invalid local package called InitWorkflow RPC")
	}

	workflow := newTestWorkflow(client)
	_, _, err = workflow.Run(ctx, "  \n\t")
	assertWorkflowSDKError(t, err, codes.InvalidArgument, WorkflowErrorEmptyPrompt)

	fakeWorkflow.startErr = workflowStatusError(codes.FailedPrecondition, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_RESTART_REQUIRED, "restart_required", "restart required")
	_, _, err = workflow.Run(ctx, "hello")
	assertWorkflowSDKError(t, err, codes.FailedPrecondition, WorkflowErrorRestartRequired)

	fakeWorkflow.startErr = nil
	fakeWorkflow.stream = &fakeWorkflowEventStream{
		err: workflowStatusError(codes.Aborted, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_REPLAY_UNAVAILABLE, "replay_unavailable", "replay unavailable"),
	}
	_, events, err := workflow.Run(ctx, "hello")
	if err != nil {
		t.Fatalf("workflow.Run returned error before Recv: %v", err)
	}
	_, err = events.Recv()
	assertWorkflowSDKError(t, err, codes.Aborted, WorkflowErrorReplayUnavailable)

	for _, tc := range []struct {
		name string
		code pb.WorkflowErrorCode
		want WorkflowErrorCode
	}{
		{name: "mcp not reachable", code: pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_MCP_NOT_REACHABLE, want: WorkflowErrorMCPNotReachable},
		{name: "mcp unavailable", code: pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_MCP_UNAVAILABLE, want: WorkflowErrorMCPUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeWorkflow := newHappyWorkflowFake()
			fakeWorkflow.initErr = workflowStatusError(codes.Unavailable, tc.code, "mcp_error", "mcp error")
			client := newWorkflowTestClient(t, fakeWorkflow)
			_, err := client.InitWorkflow(ctx, WorkflowDir{Namespace: "team-a", ID: "writer", Path: workflowDir})
			assertWorkflowSDKError(t, err, codes.Unavailable, tc.want)
		})
	}
}

func newWorkflowTestClient(t *testing.T, workflow *fakeWorkflowRuntimeClient) *Client {
	t.Helper()
	client, err := NewWithClients(
		&fakeRuntimeClient{},
		workflow,
		WithBearerToken(testToken),
		WithSessionGroupID(testSessionGroupID),
		WithWorkspaceID(testWorkspaceID),
	)
	if err != nil {
		t.Fatalf("NewWithClients returned error: %v", err)
	}
	return client
}

func newTestWorkflow(client *Client) *Workflow {
	return &Workflow{
		Namespace: "team-a",
		ID:        "writer",
		client:    client,
	}
}

func newHappyWorkflowFake() *fakeWorkflowRuntimeClient {
	return &fakeWorkflowRuntimeClient{
		initResponse: &pb.InitWorkflowResponse{
			Workflow: workflowProto("team-a", "writer"),
			Status:   workflowStatusProto("team-a", "writer"),
			Created:  true,
		},
		getWorkflowResponse: &pb.GetWorkflowResponse{
			Workflow: workflowProto("team-a", "writer"),
			Status:   workflowStatusProto("team-a", "writer"),
		},
		statusResponse: &pb.GetWorkflowStatusResponse{
			Status: workflowStatusProto("team-a", "writer"),
		},
		restartResponse: &pb.RestartWorkflowResponse{
			Status:           workflowStatusProto("team-a", "writer"),
			RestartStarted:   true,
			RestartCompleted: true,
		},
		startResponse: &pb.StartWorkflowChatRunResponse{
			Workflow:          workflowSelectorProto("team-a", "writer"),
			ChatId:            "workflow-chat-1",
			RunId:             "run-1",
			Status:            &pb.ChatStatus{ChatId: "workflow-chat-1", CurrentRunId: "run-1"},
			LastEventId:       8,
			EventCursor:       "cursor-8",
			FirstTurnAccepted: true,
			Capabilities:      &pb.ChatCapabilitySet{Status: pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED},
		},
		turnResponse: &pb.RunWorkflowChatTurnResponse{
			Workflow:     workflowSelectorProto("team-a", "writer"),
			ChatId:       "workflow-chat-1",
			RunId:        "run-2",
			Status:       &pb.ChatStatus{ChatId: "workflow-chat-1", CurrentRunId: "run-2"},
			LastEventId:  10,
			EventCursor:  "cursor-10",
			TurnAccepted: true,
		},
		getChatResponse: &pb.GetWorkflowChatResponse{
			Workflow: workflowSelectorProto("team-a", "writer"),
			Chat:     &pb.Chat{ChatId: "workflow-chat-1", Capabilities: &pb.ChatCapabilitySet{Status: pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED}},
			Status:   &pb.ChatStatus{ChatId: "workflow-chat-1"},
		},
		historyResponse: &pb.GetWorkflowChatHistoryResponse{
			Workflow:      workflowSelectorProto("team-a", "writer"),
			ChatId:        "workflow-chat-1",
			ReturnedDepth: pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_TURN_SUMMARY,
		},
		stream: &fakeWorkflowEventStream{},
		pendingResponse: &pb.RespondWorkflowChatPendingResponse{
			Workflow:         workflowSelectorProto("team-a", "writer"),
			ChatId:           "workflow-chat-1",
			RunId:            "run-1",
			PendingRequestId: "pending-1",
			Accepted:         true,
			Status:           &pb.ChatStatus{ChatId: "workflow-chat-1", CurrentRunId: "run-1"},
		},
		interruptResponse: &pb.InterruptWorkflowChatRunResponse{
			Workflow:      workflowSelectorProto("team-a", "writer"),
			ChatId:        "workflow-chat-1",
			RunId:         "run-1",
			InterruptSent: true,
			Status:        &pb.ChatStatus{ChatId: "workflow-chat-1", CurrentRunId: "run-1"},
		},
	}
}

func workflowProto(namespace string, workflowID string) *pb.Workflow {
	return &pb.Workflow{
		Workflow:                 workflowSelectorProto(namespace, workflowID),
		StorageKey:               namespace + "/" + workflowID,
		ActivePackageFingerprint: "fingerprint-1",
		Capabilities:             &pb.WorkflowCapabilitySet{Init: pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED},
	}
}

func workflowStatusProto(namespace string, workflowID string) *pb.WorkflowStatus {
	return &pb.WorkflowStatus{
		Workflow:                 workflowSelectorProto(namespace, workflowID),
		ActivePackageFingerprint: "fingerprint-1",
		ProcessEpoch:             "epoch-1",
		Capabilities:             &pb.WorkflowCapabilitySet{Run: pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED},
	}
}

func workflowSelectorProto(namespace string, workflowID string) *pb.WorkflowSelector {
	return &pb.WorkflowSelector{Namespace: namespace, WorkflowId: workflowID}
}

func workflowStatusError(code codes.Code, workflowCode pb.WorkflowErrorCode, reason string, message string) error {
	st := status.New(code, message)
	st, err := st.WithDetails(&pb.WorkflowErrorDetails{
		Outcome:        pb.WorkflowOutcomeCategory_WORKFLOW_OUTCOME_CATEGORY_FAILED_PRECONDITION,
		Code:           workflowCode,
		Reason:         reason,
		DisplayMessage: message,
		Retryable:      code == codes.Unavailable,
		NextAction:     "retry when ready",
		Workflow:       workflowSelectorProto("team-a", "writer"),
		SafeMetadata:   map[string]string{"kind": "workflow"},
	})
	if err != nil {
		panic(err)
	}
	return st.Err()
}

func assertWorkflowSDKError(t *testing.T, err error, code codes.Code, workflowCode WorkflowErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatal("error is nil")
	}
	sdkErr, ok := AsError(err)
	if !ok {
		t.Fatalf("AsError returned false for %T", err)
	}
	if sdkErr.Code != code {
		t.Fatalf("code = %s, want %s", sdkErr.Code, code)
	}
	if !sdkErr.IsWorkflow(workflowCode) {
		t.Fatalf("workflow code = %q, want %q (err=%#v)", sdkErr.WorkflowCode, workflowCode, sdkErr)
	}
}

type fakeWorkflowRuntimeClient struct {
	initCtx      context.Context
	initRequest  *pb.InitWorkflowRequest
	initResponse *pb.InitWorkflowResponse
	initErr      error

	getWorkflowCtx      context.Context
	getWorkflowRequest  *pb.GetWorkflowRequest
	getWorkflowResponse *pb.GetWorkflowResponse
	getWorkflowErr      error

	statusCtx      context.Context
	statusRequest  *pb.GetWorkflowStatusRequest
	statusResponse *pb.GetWorkflowStatusResponse
	statusErr      error

	restartCtx      context.Context
	restartRequest  *pb.RestartWorkflowRequest
	restartResponse *pb.RestartWorkflowResponse
	restartErr      error

	deleteCtx      context.Context
	deleteRequest  *pb.DeleteWorkflowRequest
	deleteResponse *pb.DeleteWorkflowResponse
	deleteErr      error

	startCtx      context.Context
	startRequest  *pb.StartWorkflowChatRunRequest
	startResponse *pb.StartWorkflowChatRunResponse
	startErr      error

	turnCtx      context.Context
	turnRequest  *pb.RunWorkflowChatTurnRequest
	turnResponse *pb.RunWorkflowChatTurnResponse
	turnErr      error

	getChatCtx      context.Context
	getChatRequest  *pb.GetWorkflowChatRequest
	getChatResponse *pb.GetWorkflowChatResponse
	getChatErr      error

	historyCtx      context.Context
	historyRequest  *pb.GetWorkflowChatHistoryRequest
	historyResponse *pb.GetWorkflowChatHistoryResponse
	historyErr      error

	streamCtx     context.Context
	streamRequest *pb.StreamWorkflowChatEventsRequest
	stream        pb.WorkflowRuntimeService_StreamWorkflowChatEventsClient
	streamErr     error

	pendingCtx      context.Context
	pendingRequest  *pb.RespondWorkflowChatPendingRequest
	pendingResponse *pb.RespondWorkflowChatPendingResponse
	pendingErr      error

	interruptCtx      context.Context
	interruptRequest  *pb.InterruptWorkflowChatRunRequest
	interruptResponse *pb.InterruptWorkflowChatRunResponse
	interruptErr      error
}

func (f *fakeWorkflowRuntimeClient) InitWorkflow(ctx context.Context, in *pb.InitWorkflowRequest, opts ...grpc.CallOption) (*pb.InitWorkflowResponse, error) {
	f.initCtx = ctx
	f.initRequest = in
	return f.initResponse, f.initErr
}

func (f *fakeWorkflowRuntimeClient) GetWorkflow(ctx context.Context, in *pb.GetWorkflowRequest, opts ...grpc.CallOption) (*pb.GetWorkflowResponse, error) {
	f.getWorkflowCtx = ctx
	f.getWorkflowRequest = in
	return f.getWorkflowResponse, f.getWorkflowErr
}

func (f *fakeWorkflowRuntimeClient) GetWorkflowStatus(ctx context.Context, in *pb.GetWorkflowStatusRequest, opts ...grpc.CallOption) (*pb.GetWorkflowStatusResponse, error) {
	f.statusCtx = ctx
	f.statusRequest = in
	return f.statusResponse, f.statusErr
}

func (f *fakeWorkflowRuntimeClient) RestartWorkflow(ctx context.Context, in *pb.RestartWorkflowRequest, opts ...grpc.CallOption) (*pb.RestartWorkflowResponse, error) {
	f.restartCtx = ctx
	f.restartRequest = in
	return f.restartResponse, f.restartErr
}

func (f *fakeWorkflowRuntimeClient) DeleteWorkflow(ctx context.Context, in *pb.DeleteWorkflowRequest, opts ...grpc.CallOption) (*pb.DeleteWorkflowResponse, error) {
	f.deleteCtx = ctx
	f.deleteRequest = in
	return f.deleteResponse, f.deleteErr
}

func (f *fakeWorkflowRuntimeClient) StartWorkflowChatRun(ctx context.Context, in *pb.StartWorkflowChatRunRequest, opts ...grpc.CallOption) (*pb.StartWorkflowChatRunResponse, error) {
	f.startCtx = ctx
	f.startRequest = in
	return f.startResponse, f.startErr
}

func (f *fakeWorkflowRuntimeClient) RunWorkflowChatTurn(ctx context.Context, in *pb.RunWorkflowChatTurnRequest, opts ...grpc.CallOption) (*pb.RunWorkflowChatTurnResponse, error) {
	f.turnCtx = ctx
	f.turnRequest = in
	return f.turnResponse, f.turnErr
}

func (f *fakeWorkflowRuntimeClient) GetWorkflowChat(ctx context.Context, in *pb.GetWorkflowChatRequest, opts ...grpc.CallOption) (*pb.GetWorkflowChatResponse, error) {
	f.getChatCtx = ctx
	f.getChatRequest = in
	return f.getChatResponse, f.getChatErr
}

func (f *fakeWorkflowRuntimeClient) GetWorkflowChatHistory(ctx context.Context, in *pb.GetWorkflowChatHistoryRequest, opts ...grpc.CallOption) (*pb.GetWorkflowChatHistoryResponse, error) {
	f.historyCtx = ctx
	f.historyRequest = in
	return f.historyResponse, f.historyErr
}

func (f *fakeWorkflowRuntimeClient) StreamWorkflowChatEvents(ctx context.Context, in *pb.StreamWorkflowChatEventsRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.StreamWorkflowChatEventsResponse], error) {
	f.streamCtx = ctx
	f.streamRequest = in
	return f.stream, f.streamErr
}

func (f *fakeWorkflowRuntimeClient) RespondWorkflowChatPending(ctx context.Context, in *pb.RespondWorkflowChatPendingRequest, opts ...grpc.CallOption) (*pb.RespondWorkflowChatPendingResponse, error) {
	f.pendingCtx = ctx
	f.pendingRequest = in
	return f.pendingResponse, f.pendingErr
}

func (f *fakeWorkflowRuntimeClient) InterruptWorkflowChatRun(ctx context.Context, in *pb.InterruptWorkflowChatRunRequest, opts ...grpc.CallOption) (*pb.InterruptWorkflowChatRunResponse, error) {
	f.interruptCtx = ctx
	f.interruptRequest = in
	return f.interruptResponse, f.interruptErr
}

type fakeWorkflowEventStream struct {
	grpc.ClientStream
	messages []*pb.StreamWorkflowChatEventsResponse
	err      error
}

func (s *fakeWorkflowEventStream) Recv() (*pb.StreamWorkflowChatEventsResponse, error) {
	if len(s.messages) == 0 {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}
	next := s.messages[0]
	s.messages = s.messages[1:]
	return next, nil
}
