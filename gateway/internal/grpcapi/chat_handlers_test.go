package grpcapi

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const testCodexThreadID = "018f3f7a-9a31-7c21-8a4e-7f5b3c2d1a09"

func TestStartChatRunHandlerReturnsAcceptedCodexThreadAndTurn(t *testing.T) {
	runtime := &fakeChatRuntimeStartService{
		response: domain.StartChatRunResponse{
			ChatID:            testCodexThreadID,
			RunID:             "turn-1",
			SessionGroupID:    "sg-1",
			WorkspaceID:       "ws-1",
			LastEventID:       7,
			EventCursor:       "epoch-1:7",
			FirstTurnAccepted: true,
			ProcessEpoch:      "epoch-1",
		},
	}
	service := NewChatRuntimeService(ChatRuntimeServiceOptions{
		Enabled:             true,
		MaxRecvMessageBytes: 1024 * 1024,
		MaxSendMessageBytes: 1024 * 1024,
		SessionGroups: testResolver{
			"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
		},
		Runtime: runtime,
	}).(*chatRuntimeService)

	response, err := service.StartChatRun(context.Background(), validStartChatRunHandlerRequest())
	if err != nil {
		t.Fatalf("StartChatRun() error = %v", err)
	}
	if response.GetChatId() != testCodexThreadID || response.GetRunId() != "turn-1" || !response.GetFirstTurnAccepted() {
		t.Fatalf("StartChatRun() response = %#v, want accepted thread/run", response)
	}
	if response.GetStatus().GetChatId() != testCodexThreadID ||
		response.GetStatus().GetCurrentRunId() != "turn-1" ||
		response.GetStatus().GetCurrentRunLifecycle() != pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_IN_PROGRESS ||
		response.GetStatus().GetGatewayLocal().GetProcessEpoch() != "epoch-1" {
		t.Fatalf("StartChatRun() status = %#v, want running chat status", response.GetStatus())
	}
	if runtime.calls != 1 {
		t.Fatalf("runtime calls = %d, want 1", runtime.calls)
	}
}

func TestStartChatRunHandlerRejectsInvalidPromptBeforeRuntime(t *testing.T) {
	runtime := &fakeChatRuntimeStartService{}
	service := NewChatRuntimeService(ChatRuntimeServiceOptions{
		Enabled:             true,
		MaxRecvMessageBytes: 1024 * 1024,
		MaxSendMessageBytes: 1024 * 1024,
		SessionGroups: testResolver{
			"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
		},
		Runtime: runtime,
	}).(*chatRuntimeService)
	req := validStartChatRunHandlerRequest()
	req.Prompt = " "

	_, err := service.StartChatRun(context.Background(), req)
	assertChatRuntimeStatus(t, err, codes.InvalidArgument, domain.ReasonInvalidRequest)
	if runtime.calls != 0 {
		t.Fatalf("runtime calls = %d, want 0", runtime.calls)
	}
}

func TestStartChatRunHandlerMapsRuntimeErrorToChatDetailsWithoutPromptLeak(t *testing.T) {
	service := NewChatRuntimeService(ChatRuntimeServiceOptions{
		Enabled:             true,
		MaxRecvMessageBytes: 1024 * 1024,
		MaxSendMessageBytes: 1024 * 1024,
		SessionGroups: testResolver{
			"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
		},
		Runtime: &fakeChatRuntimeStartService{
			err: &domain.GatewayError{
				Code: domain.GatewayErrorCodeUnknown,
				Details: domain.GatewayErrorDetails{
					Reason:          domain.ReasonIdempotencyResultUnavailable,
					DisplayMessage:  "idempotent result is not available in this gateway process",
					SessionGroupID:  "sg-1",
					ClientMessageID: "client-message-1",
					Retryable:       true,
				},
			},
		},
	}).(*chatRuntimeService)
	req := validStartChatRunHandlerRequest()
	req.Prompt = "redaction-sentinel prompt"

	_, err := service.StartChatRun(context.Background(), req)
	details := assertChatRuntimeStatus(t, err, codes.Unknown, domain.ReasonIdempotencyResultUnavailable)
	if !details.GetRetryable() || details.GetSessionGroupId() != "sg-1" {
		t.Fatalf("ChatRuntimeErrorDetails = %#v, want retryable sg-1 detail", details)
	}
	if strings.Contains(err.Error(), "redaction-sentinel") {
		t.Fatalf("StartChatRun() error leaked prompt: %q", err.Error())
	}
}

func TestRunChatTurnHandlerReturnsAcceptedExistingThreadTurn(t *testing.T) {
	runtime := &fakeChatRuntimeStartService{
		runResponse: domain.RunChatTurnResponse{
			ChatID:         testCodexThreadID,
			RunID:          "turn-2",
			SessionGroupID: "sg-1",
			WorkspaceID:    "ws-1",
			Status: domain.ChatStatus{
				ChatID:              testCodexThreadID,
				SessionGroupID:      "sg-1",
				WorkspaceID:         "ws-1",
				LookupValid:         true,
				ThreadLifecycle:     domain.ChatThreadLifecycleActiveRunning,
				CurrentRunLifecycle: domain.ChatTurnLifecycleInProgress,
				CurrentRunID:        "turn-2",
				LastRunID:           "turn-2",
				Capabilities:        testChatCapabilities(),
				GatewayLocal: domain.ChatGatewayLocalState{
					Live:            true,
					ReplayAvailable: true,
					ProcessEpoch:    "epoch-1",
				},
			},
			LastEventID:  8,
			EventCursor:  "epoch-1:8",
			TurnAccepted: true,
		},
	}
	service := newTestChatRuntimeService(runtime)

	response, err := service.RunChatTurn(context.Background(), validRunChatTurnHandlerRequest())
	if err != nil {
		t.Fatalf("RunChatTurn() error = %v", err)
	}
	if response.GetChatId() != testCodexThreadID || response.GetRunId() != "turn-2" || !response.GetTurnAccepted() {
		t.Fatalf("RunChatTurn() response = %#v, want accepted existing-thread turn", response)
	}
	if runtime.runCalls != 1 || runtime.runCommand.ChatID != testCodexThreadID {
		t.Fatalf("runtime RunChatTurn calls = %d command = %#v, want chat_id command", runtime.runCalls, runtime.runCommand)
	}
}

func TestGetChatHistoryHandlerMapsNarrowedTurnSummary(t *testing.T) {
	runtime := &fakeChatRuntimeStartService{
		historyResponse: domain.GetChatHistoryResponse{
			ChatID: testCodexThreadID,
			Turns: []domain.ChatTurnSummary{
				{RunID: "turn-2", Lifecycle: domain.ChatTurnLifecycleCompleted, ItemsView: domain.ChatTurnItemsViewSummary},
			},
			ReturnedDepth: domain.ChatHistoryDepthTurnSummary,
			Capability:    domain.ChatCapabilityNarrowed,
			Narrowed: &domain.ChatNarrowedOutcome{
				Reason:         domain.ReasonChatRuntimeNotImplemented,
				DisplayMessage: "item-level chat history is not available",
			},
		},
	}
	service := newTestChatRuntimeService(runtime)

	response, err := service.GetChatHistory(context.Background(), &pb.GetChatHistoryRequest{
		Context: &pb.ChatRuntimeContext{
			SessionGroupId: "sg-1",
			WorkspaceId:    "ws-1",
		},
		ChatId:         testCodexThreadID,
		RequestedDepth: pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_ITEM_LEVEL,
	})
	if err != nil {
		t.Fatalf("GetChatHistory() error = %v", err)
	}
	if len(response.GetTurns()) != 1 || response.GetTurns()[0].GetRunId() != "turn-2" || response.GetCapability() != pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_NARROWED || response.GetNarrowed() == nil {
		t.Fatalf("GetChatHistory() response = %#v, want narrowed turn summary", response)
	}
	if runtime.historyCalls != 1 || runtime.historyCommand.RequestedDepth != domain.ChatHistoryDepthItemLevel {
		t.Fatalf("runtime GetChatHistory calls = %d command = %#v, want item-level request", runtime.historyCalls, runtime.historyCommand)
	}
}

func TestStreamChatEventsHandlerSendsStatusEventAndClosesRuntimeStream(t *testing.T) {
	runtimeStream := &fakeChatEventStream{
		messages: []StreamChatEventsMessage{
			{
				SessionGroupID: "sg-1",
				Event: &domain.ChatEvent{
					EventID:         1,
					EventCursor:     "epoch-1:turn-1:1",
					ChatID:          testCodexThreadID,
					SessionGroupID:  "sg-1",
					WorkspaceID:     "ws-1",
					RunID:           "turn-1",
					CreatedAtUnixMS: 10,
					StatusUpdated: &domain.ChatStatus{
						ChatID:              testCodexThreadID,
						SessionGroupID:      "sg-1",
						WorkspaceID:         "ws-1",
						LookupValid:         true,
						ThreadLifecycle:     domain.ChatThreadLifecycleActiveRunning,
						CurrentRunLifecycle: domain.ChatTurnLifecycleInProgress,
						CurrentRunID:        "turn-1",
						LastRunID:           "turn-1",
						Capabilities:        testChatCapabilities(),
						GatewayLocal: domain.ChatGatewayLocalState{
							Live:            true,
							ReplayAvailable: true,
							ProcessEpoch:    "epoch-1",
						},
						LastEventID: 1,
					},
				},
			},
		},
	}
	runtime := &fakeChatRuntimeStartService{stream: runtimeStream}
	service := newTestChatRuntimeService(runtime)
	serverStream := &fakeStreamChatEventsServer{ctx: context.Background()}

	err := service.StreamChatEvents(&pb.StreamChatEventsRequest{
		Context: &pb.ChatRuntimeContext{
			SessionGroupId: "sg-1",
			WorkspaceId:    "ws-1",
		},
		ChatId: testCodexThreadID,
		Cursor: &pb.StreamChatEventsRequest_FromStart{
			FromStart: &pb.ChatFromStartCursor{},
		},
		ClientSubscriberId: "subscriber-1",
	}, serverStream)
	if err != nil {
		t.Fatalf("StreamChatEvents() error = %v", err)
	}
	if runtime.streamCalls != 1 || runtime.streamCommand.ChatID != testCodexThreadID || runtime.streamCommand.CursorKind != domain.StreamCursorFromStart {
		t.Fatalf("runtime StreamChatEvents calls = %d command = %#v, want from-start chat", runtime.streamCalls, runtime.streamCommand)
	}
	if len(serverStream.sent) != 1 || serverStream.sent[0].GetEvent().GetStatusUpdated().GetStatus().GetCurrentRunId() != "turn-1" {
		t.Fatalf("sent stream responses = %#v, want one status update event", serverStream.sent)
	}
	if !runtimeStream.closed {
		t.Fatal("runtime stream closed = false, want close after handler EOF")
	}
}

func TestGetChatHistoryHandlerRejectsOversizedLimitBeforeRuntime(t *testing.T) {
	runtime := &fakeChatRuntimeStartService{}
	service := newTestChatRuntimeService(runtime)

	_, err := service.GetChatHistory(context.Background(), &pb.GetChatHistoryRequest{
		Context: &pb.ChatRuntimeContext{
			SessionGroupId: "sg-1",
			WorkspaceId:    "ws-1",
		},
		ChatId: testCodexThreadID,
		Limit:  domain.MaxChatHistoryLimit + 1,
	})
	assertChatRuntimeStatus(t, err, codes.ResourceExhausted, domain.ReasonResourceExhausted)
	if runtime.historyCalls != 0 {
		t.Fatalf("runtime GetChatHistory calls = %d, want validation failure before runtime", runtime.historyCalls)
	}
}

func TestChatRuntimeHandlersRejectInvalidSessionWorkspaceBeforeRuntime(t *testing.T) {
	tests := []struct {
		name         string
		call         func(service *chatRuntimeService, runtimeContext *pb.ChatRuntimeContext) error
		runtimeCalls func(runtime *fakeChatRuntimeStartService) int
	}{
		{
			name: "StartChatRun",
			call: func(service *chatRuntimeService, runtimeContext *pb.ChatRuntimeContext) error {
				req := validStartChatRunHandlerRequest()
				req.Context = runtimeContext
				_, err := service.StartChatRun(context.Background(), req)
				return err
			},
			runtimeCalls: func(runtime *fakeChatRuntimeStartService) int { return runtime.calls },
		},
		{
			name: "GetChat",
			call: func(service *chatRuntimeService, runtimeContext *pb.ChatRuntimeContext) error {
				req := validGetChatRequest()
				req.Context = runtimeContext
				_, err := service.GetChat(context.Background(), req)
				return err
			},
			runtimeCalls: func(runtime *fakeChatRuntimeStartService) int { return runtime.getCalls },
		},
		{
			name: "RunChatTurn",
			call: func(service *chatRuntimeService, runtimeContext *pb.ChatRuntimeContext) error {
				req := validRunChatTurnHandlerRequest()
				req.Context = runtimeContext
				_, err := service.RunChatTurn(context.Background(), req)
				return err
			},
			runtimeCalls: func(runtime *fakeChatRuntimeStartService) int { return runtime.runCalls },
		},
		{
			name: "GetChatStatus",
			call: func(service *chatRuntimeService, runtimeContext *pb.ChatRuntimeContext) error {
				req := validGetChatStatusRequest()
				req.Context = runtimeContext
				_, err := service.GetChatStatus(context.Background(), req)
				return err
			},
			runtimeCalls: func(runtime *fakeChatRuntimeStartService) int { return runtime.statusCalls },
		},
		{
			name: "GetChatHistory",
			call: func(service *chatRuntimeService, runtimeContext *pb.ChatRuntimeContext) error {
				req := validGetChatHistoryRequest()
				req.Context = runtimeContext
				_, err := service.GetChatHistory(context.Background(), req)
				return err
			},
			runtimeCalls: func(runtime *fakeChatRuntimeStartService) int { return runtime.historyCalls },
		},
		{
			name: "StreamChatEvents",
			call: func(service *chatRuntimeService, runtimeContext *pb.ChatRuntimeContext) error {
				req := validStreamChatEventsRequest()
				req.Context = runtimeContext
				return service.StreamChatEvents(req, &fakeStreamChatEventsServer{ctx: context.Background()})
			},
			runtimeCalls: func(runtime *fakeChatRuntimeStartService) int { return runtime.streamCalls },
		},
		{
			name: "RespondChatPending",
			call: func(service *chatRuntimeService, runtimeContext *pb.ChatRuntimeContext) error {
				req := validRespondChatPendingRequest()
				req.Context = runtimeContext
				_, err := service.RespondChatPending(context.Background(), req)
				return err
			},
			runtimeCalls: func(runtime *fakeChatRuntimeStartService) int { return runtime.pendingCalls },
		},
		{
			name: "InterruptChatRun",
			call: func(service *chatRuntimeService, runtimeContext *pb.ChatRuntimeContext) error {
				req := validInterruptChatRunRequest()
				req.Context = runtimeContext
				_, err := service.InterruptChatRun(context.Background(), req)
				return err
			},
			runtimeCalls: func(runtime *fakeChatRuntimeStartService) int { return runtime.interruptCalls },
		},
	}
	invalidContexts := []struct {
		name    string
		context *pb.ChatRuntimeContext
		code    codes.Code
		reason  domain.GatewayErrorReason
	}{
		{
			name: "unknown session",
			context: &pb.ChatRuntimeContext{
				SessionGroupId: "sg-unknown",
				WorkspaceId:    "ws-1",
			},
			code:   codes.NotFound,
			reason: domain.ReasonUnknownSessionGroup,
		},
		{
			name: "workspace mismatch",
			context: &pb.ChatRuntimeContext{
				SessionGroupId: "sg-1",
				WorkspaceId:    "ws-other",
			},
			code:   codes.InvalidArgument,
			reason: domain.ReasonWorkspaceMismatch,
		},
	}

	for _, tt := range tests {
		for _, invalid := range invalidContexts {
			t.Run(tt.name+"/"+invalid.name, func(t *testing.T) {
				runtime := &fakeChatRuntimeStartService{}
				service := newTestChatRuntimeService(runtime)

				err := tt.call(service, invalid.context)

				assertChatRuntimeStatus(t, err, invalid.code, invalid.reason)
				if got := tt.runtimeCalls(runtime); got != 0 {
					t.Fatalf("%s runtime calls = %d, want validation failure before runtime", tt.name, got)
				}
			})
		}
	}
}

type fakeChatRuntimeStartService struct {
	response         domain.StartChatRunResponse
	getResponse      domain.GetChatResponse
	runResponse      domain.RunChatTurnResponse
	statusResponse   domain.GetChatStatusResponse
	historyResponse  domain.GetChatHistoryResponse
	pendingResponse  domain.RespondChatPendingResponse
	interruptResult  domain.InterruptChatRunResponse
	stream           ChatEventStream
	streamErr        error
	err              error
	calls            int
	getCalls         int
	runCalls         int
	statusCalls      int
	historyCalls     int
	streamCalls      int
	pendingCalls     int
	interruptCalls   int
	command          domain.StartChatRunCommand
	getCommand       domain.GetChatCommand
	runCommand       domain.RunChatTurnCommand
	statusCommand    domain.GetChatStatusCommand
	historyCommand   domain.GetChatHistoryCommand
	streamCommand    domain.StreamChatEventsCommand
	pendingCommand   domain.RespondChatPendingCommand
	interruptCommand domain.InterruptChatRunCommand
}

func (s *fakeChatRuntimeStartService) StartChatRun(ctx context.Context, command domain.StartChatRunCommand) (domain.StartChatRunResponse, error) {
	s.calls++
	s.command = command
	if s.err != nil {
		return domain.StartChatRunResponse{}, s.err
	}
	return s.response, nil
}

func (s *fakeChatRuntimeStartService) GetChat(_ context.Context, command domain.GetChatCommand) (domain.GetChatResponse, error) {
	s.getCalls++
	s.getCommand = command
	if s.err != nil {
		return domain.GetChatResponse{}, s.err
	}
	return s.getResponse, nil
}

func (s *fakeChatRuntimeStartService) RunChatTurn(_ context.Context, command domain.RunChatTurnCommand) (domain.RunChatTurnResponse, error) {
	s.runCalls++
	s.runCommand = command
	if s.err != nil {
		return domain.RunChatTurnResponse{}, s.err
	}
	return s.runResponse, nil
}

func (s *fakeChatRuntimeStartService) GetChatStatus(_ context.Context, command domain.GetChatStatusCommand) (domain.GetChatStatusResponse, error) {
	s.statusCalls++
	s.statusCommand = command
	if s.err != nil {
		return domain.GetChatStatusResponse{}, s.err
	}
	return s.statusResponse, nil
}

func (s *fakeChatRuntimeStartService) GetChatHistory(_ context.Context, command domain.GetChatHistoryCommand) (domain.GetChatHistoryResponse, error) {
	s.historyCalls++
	s.historyCommand = command
	if s.err != nil {
		return domain.GetChatHistoryResponse{}, s.err
	}
	return s.historyResponse, nil
}

func (s *fakeChatRuntimeStartService) StreamChatEvents(_ context.Context, command domain.StreamChatEventsCommand) (ChatEventStream, error) {
	s.streamCalls++
	s.streamCommand = command
	if s.streamErr != nil {
		return nil, s.streamErr
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.stream != nil {
		return s.stream, nil
	}
	return &fakeChatEventStream{}, nil
}

func (s *fakeChatRuntimeStartService) RespondChatPending(_ context.Context, command domain.RespondChatPendingCommand) (domain.RespondChatPendingResponse, error) {
	s.pendingCalls++
	s.pendingCommand = command
	if s.err != nil {
		return domain.RespondChatPendingResponse{}, s.err
	}
	return s.pendingResponse, nil
}

func (s *fakeChatRuntimeStartService) InterruptChatRun(_ context.Context, command domain.InterruptChatRunCommand) (domain.InterruptChatRunResponse, error) {
	s.interruptCalls++
	s.interruptCommand = command
	if s.err != nil {
		return domain.InterruptChatRunResponse{}, s.err
	}
	return s.interruptResult, nil
}

type fakeChatEventStream struct {
	messages []StreamChatEventsMessage
	index    int
	closed   bool
}

func (s *fakeChatEventStream) Next(context.Context) (StreamChatEventsMessage, error) {
	if s.index >= len(s.messages) {
		return StreamChatEventsMessage{}, io.EOF
	}
	message := s.messages[s.index]
	s.index++
	return message, nil
}

func (s *fakeChatEventStream) Close() error {
	s.closed = true
	return nil
}

type fakeStreamChatEventsServer struct {
	ctx  context.Context
	sent []*pb.StreamChatEventsResponse
}

func (s *fakeStreamChatEventsServer) Send(response *pb.StreamChatEventsResponse) error {
	s.sent = append(s.sent, response)
	return nil
}

func (s *fakeStreamChatEventsServer) SetHeader(metadata.MD) error {
	return nil
}

func (s *fakeStreamChatEventsServer) SendHeader(metadata.MD) error {
	return nil
}

func (s *fakeStreamChatEventsServer) SetTrailer(metadata.MD) {}

func (s *fakeStreamChatEventsServer) Context() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

func (s *fakeStreamChatEventsServer) SendMsg(any) error {
	return nil
}

func (s *fakeStreamChatEventsServer) RecvMsg(any) error {
	return io.EOF
}

func validStartChatRunHandlerRequest() *pb.StartChatRunRequest {
	return &pb.StartChatRunRequest{
		Context: &pb.ChatRuntimeContext{
			SessionGroupId: "sg-1",
			WorkspaceId:    "ws-1",
		},
		Prompt:          "hello",
		ClientMessageId: "client-message-1",
		IdempotencyKey:  "idem-1",
	}
}

func validRunChatTurnHandlerRequest() *pb.RunChatTurnRequest {
	return &pb.RunChatTurnRequest{
		Context: &pb.ChatRuntimeContext{
			SessionGroupId: "sg-1",
			WorkspaceId:    "ws-1",
		},
		ChatId:          testCodexThreadID,
		Prompt:          "continue",
		ClientMessageId: "client-message-1",
		IdempotencyKey:  "idem-2",
	}
}

func newTestChatRuntimeService(runtime ChatRuntimeService) *chatRuntimeService {
	return NewChatRuntimeService(ChatRuntimeServiceOptions{
		Enabled:             true,
		MaxRecvMessageBytes: 1024 * 1024,
		MaxSendMessageBytes: 1024 * 1024,
		SessionGroups: testResolver{
			"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
		},
		Runtime: runtime,
	}).(*chatRuntimeService)
}

func testChatCapabilities() domain.ChatCapabilitySet {
	return domain.ChatCapabilitySet{
		Status:      domain.ChatCapabilitySupported,
		History:     domain.ChatCapabilitySupported,
		EventStream: domain.ChatCapabilityNarrowed,
		Replay:      domain.ChatCapabilityNarrowed,
		Pending:     domain.ChatCapabilitySupported,
		Interrupt:   domain.ChatCapabilitySupported,
	}
}

func assertChatRuntimeStatus(t *testing.T, err error, code codes.Code, reason domain.GatewayErrorReason) *pb.ChatRuntimeErrorDetails {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s/%s", code, reason)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%#v) ok = false", err)
	}
	if st.Code() != code {
		t.Fatalf("status code = %s, want %s", st.Code(), code)
	}
	for _, detail := range st.Details() {
		if chatDetails, ok := detail.(*pb.ChatRuntimeErrorDetails); ok {
			if chatDetails.GetReason() != string(reason) {
				t.Fatalf("ChatRuntimeErrorDetails reason = %q, want %q", chatDetails.GetReason(), reason)
			}
			return chatDetails
		}
	}
	t.Fatalf("status details = %#v, want ChatRuntimeErrorDetails", st.Details())
	return nil
}

var _ ChatRuntimeService = (*fakeChatRuntimeStartService)(nil)
