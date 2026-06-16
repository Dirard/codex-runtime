package codex

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	testToken          = "test-token"
	testSessionGroupID = "local-session"
	testWorkspaceID    = "workspace-1"
)

func TestRunStartsChatAndSubscribesWithChatFirstIdentity(t *testing.T) {
	fake := &fakeRuntimeClient{
		startResponse: &pb.StartChatRunResponse{
			ChatId:            "thread-123",
			RunId:             "turn-456",
			SessionGroupId:    testSessionGroupID,
			WorkspaceId:       testWorkspaceID,
			Status:            &pb.ChatStatus{ChatId: "thread-123", CurrentRunId: "turn-456"},
			LastEventId:       7,
			EventCursor:       "cursor-7",
			FirstTurnAccepted: true,
		},
		stream: &fakeEventStream{},
	}
	client := newTestClient(t, fake)

	chat, events, err := client.Run(
		context.Background(),
		"hello",
		WithClientMessageID("msg-1"),
		WithIdempotencyKey("idem-1"),
		WithUICorrelationMetadata(map[string]string{"request": "api"}),
	)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if chat.ID != "thread-123" {
		t.Fatalf("chat.ID = %q, want thread-123", chat.ID)
	}
	if events == nil {
		t.Fatal("events stream is nil")
	}
	assertAuthorization(t, fake.startCtx)
	if fake.startRequest.GetContext().GetSessionGroupId() != testSessionGroupID {
		t.Fatalf("session_group_id = %q", fake.startRequest.GetContext().GetSessionGroupId())
	}
	if fake.startRequest.GetContext().GetWorkspaceId() != testWorkspaceID {
		t.Fatalf("workspace_id = %q", fake.startRequest.GetContext().GetWorkspaceId())
	}
	if fake.startRequest.GetPrompt() != "hello" {
		t.Fatalf("prompt = %q", fake.startRequest.GetPrompt())
	}
	if fake.startRequest.GetClientMessageId() != "msg-1" {
		t.Fatalf("client_message_id = %q", fake.startRequest.GetClientMessageId())
	}
	if fake.startRequest.GetIdempotencyKey() != "idem-1" {
		t.Fatalf("idempotency_key = %q", fake.startRequest.GetIdempotencyKey())
	}
	if fake.streamRequest.GetChatId() != "thread-123" {
		t.Fatalf("stream chat_id = %q", fake.streamRequest.GetChatId())
	}
	if got := fake.streamRequest.GetAfterEventCursor(); got != "cursor-7" {
		t.Fatalf("stream cursor = %q, want cursor-7", got)
	}
	assertAuthorization(t, fake.streamCtx)
}

func TestGetChatUsesExistingChatWithoutStart(t *testing.T) {
	fake := &fakeRuntimeClient{
		getChatResponse: &pb.GetChatResponse{
			Chat: &pb.Chat{
				ChatId:         "thread-existing",
				SessionGroupId: testSessionGroupID,
				WorkspaceId:    testWorkspaceID,
			},
			Status: &pb.ChatStatus{ChatId: "thread-existing"},
		},
	}
	client := newTestClient(t, fake)

	chat, err := client.GetChat(context.Background(), "thread-existing")
	if err != nil {
		t.Fatalf("GetChat returned error: %v", err)
	}
	if chat.ID != "thread-existing" {
		t.Fatalf("chat.ID = %q", chat.ID)
	}
	if fake.startRequest != nil {
		t.Fatal("GetChat called StartChatRun")
	}
	if fake.getChatRequest.GetChatId() != "thread-existing" {
		t.Fatalf("get chat_id = %q", fake.getChatRequest.GetChatId())
	}
	assertAuthorization(t, fake.getChatCtx)
}

func TestChatRunUsesExistingChatID(t *testing.T) {
	fake := &fakeRuntimeClient{
		runResponse: &pb.RunChatTurnResponse{
			ChatId:         "thread-existing",
			RunId:          "turn-next",
			SessionGroupId: testSessionGroupID,
			WorkspaceId:    testWorkspaceID,
			Status:         &pb.ChatStatus{ChatId: "thread-existing", CurrentRunId: "turn-next"},
			TurnAccepted:   true,
		},
	}
	client := newTestClient(t, fake)
	chat := &Chat{ID: "thread-existing", client: client}

	result, err := chat.Run(context.Background(), "continue", WithIdempotencyKey("idem-continue"))
	if err != nil {
		t.Fatalf("chat.Run returned error: %v", err)
	}
	if result.RunID != "turn-next" {
		t.Fatalf("run id = %q", result.RunID)
	}
	if fake.runRequest.GetChatId() != "thread-existing" {
		t.Fatalf("run chat_id = %q", fake.runRequest.GetChatId())
	}
	if fake.runRequest.GetIdempotencyKey() != "idem-continue" {
		t.Fatalf("run idempotency_key = %q", fake.runRequest.GetIdempotencyKey())
	}
	if fake.runRequest.GetClientMessageId() == "" {
		t.Fatal("run client_message_id was not generated")
	}
	assertAuthorization(t, fake.runCtx)
}

func TestSideEffectingCallsGenerateRequiredIDs(t *testing.T) {
	fake := &fakeRuntimeClient{
		startResponse: &pb.StartChatRunResponse{
			ChatId:            "thread-new",
			RunId:             "turn-new",
			FirstTurnAccepted: true,
		},
		runResponse: &pb.RunChatTurnResponse{
			ChatId:       "thread-new",
			RunId:        "turn-next",
			TurnAccepted: true,
		},
		pendingResponse: &pb.RespondChatPendingResponse{
			ChatId:           "thread-new",
			RunId:            "turn-next",
			PendingRequestId: "pending-1",
			Accepted:         true,
		},
		interruptResponse: &pb.InterruptChatRunResponse{
			ChatId:        "thread-new",
			RunId:         "turn-next",
			InterruptSent: true,
		},
		stream: &fakeEventStream{},
	}
	client := newTestClient(t, fake)

	chat, events, err := client.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	defer events.Close()
	assertGeneratedID(t, fake.startRequest.GetClientMessageId(), "msg-")
	assertGeneratedID(t, fake.startRequest.GetIdempotencyKey(), "idem-")

	if _, err := chat.Run(context.Background(), "continue"); err != nil {
		t.Fatalf("chat.Run returned error: %v", err)
	}
	assertGeneratedID(t, fake.runRequest.GetClientMessageId(), "msg-")
	assertGeneratedID(t, fake.runRequest.GetIdempotencyKey(), "idem-")

	if _, err := chat.RespondApproval(context.Background(), "pending-1", "accept"); err != nil {
		t.Fatalf("RespondApproval returned error: %v", err)
	}
	assertGeneratedID(t, fake.pendingRequest.GetClientResponseId(), "pending-response-")
	assertGeneratedID(t, fake.pendingRequest.GetIdempotencyKey(), "idem-")

	if _, err := chat.InterruptRun(context.Background(), "turn-next"); err != nil {
		t.Fatalf("InterruptRun returned error: %v", err)
	}
	assertGeneratedID(t, fake.interruptRequest.GetClientRequestId(), "interrupt-")
	assertGeneratedID(t, fake.interruptRequest.GetIdempotencyKey(), "idem-")
}

func TestGetStatusAndHistoryHelpersWireRequestsAndAuth(t *testing.T) {
	fake := &fakeRuntimeClient{
		statusResponse: &pb.GetChatStatusResponse{
			Status: &pb.ChatStatus{ChatId: "thread-existing", CurrentRunId: "turn-active"},
		},
		historyResponse: &pb.GetChatHistoryResponse{
			ChatId: "thread-existing",
			Turns: []*pb.ChatTurnSummary{
				{RunId: "turn-1", Lifecycle: pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_COMPLETED},
			},
			ReturnedDepth: pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_TURN_SUMMARY,
		},
	}
	client := newTestClient(t, fake)
	chat := &Chat{ID: "thread-existing", client: client}

	status, err := chat.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}
	if status.GetCurrentRunId() != "turn-active" {
		t.Fatalf("current run id = %q", status.GetCurrentRunId())
	}
	if got := chat.CachedStatus().GetCurrentRunId(); got != "turn-active" {
		t.Fatalf("cached current run id = %q", got)
	}
	status.CurrentRunId = "mutated"
	if got := chat.CachedStatus().GetCurrentRunId(); got != "turn-active" {
		t.Fatalf("cached status shared mutable response pointer: %q", got)
	}
	if fake.statusRequest.GetChatId() != "thread-existing" {
		t.Fatalf("status chat_id = %q", fake.statusRequest.GetChatId())
	}
	assertAuthorization(t, fake.statusCtx)

	history, err := chat.GetHistory(
		context.Background(),
		WithHistoryDepth(pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_TURN_SUMMARY),
		WithHistoryCursor("cursor-1"),
		WithHistoryLimit(3),
		WithHistorySortDirection(pb.ChatHistorySortDirection_CHAT_HISTORY_SORT_DIRECTION_ASCENDING),
	)
	if err != nil {
		t.Fatalf("GetHistory returned error: %v", err)
	}
	if history.GetChatId() != "thread-existing" {
		t.Fatalf("history chat_id = %q", history.GetChatId())
	}
	if fake.historyRequest.GetRequestedDepth() != pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_TURN_SUMMARY {
		t.Fatalf("history depth = %s", fake.historyRequest.GetRequestedDepth())
	}
	if fake.historyRequest.GetCursor() != "cursor-1" || fake.historyRequest.GetLimit() != 3 {
		t.Fatalf("history cursor/limit = %q/%d", fake.historyRequest.GetCursor(), fake.historyRequest.GetLimit())
	}
	if fake.historyRequest.GetSortDirection() != pb.ChatHistorySortDirection_CHAT_HISTORY_SORT_DIRECTION_ASCENDING {
		t.Fatalf("history sort direction = %s", fake.historyRequest.GetSortDirection())
	}
	assertAuthorization(t, fake.historyCtx)
}

func TestStatusAndHistoryTypedErrors(t *testing.T) {
	statusErr := chatRuntimeStatusError(codes.Unavailable, pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNAVAILABLE, "codex_unavailable", "codex unavailable")
	fake := &fakeRuntimeClient{statusErr: statusErr}
	client := newTestClient(t, fake)
	chat := &Chat{ID: "thread-existing", client: client}

	_, err := chat.GetStatus(context.Background())
	assertSDKError(t, err, codes.Unavailable, "codex_unavailable")

	historyErr := chatRuntimeStatusError(codes.Unimplemented, pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNSUPPORTED, "history_unsupported", "history unsupported")
	fake = &fakeRuntimeClient{historyErr: historyErr}
	client = newTestClient(t, fake)
	chat = &Chat{ID: "thread-existing", client: client}

	_, err = chat.GetHistory(context.Background())
	assertSDKError(t, err, codes.Unimplemented, "history_unsupported")
}

func TestRunRejectsEmptyPromptBeforeRPC(t *testing.T) {
	for _, prompt := range []string{"", "   \t\n"} {
		t.Run(prompt, func(t *testing.T) {
			fake := &fakeRuntimeClient{}
			client := newTestClient(t, fake)

			chat, events, err := client.Run(context.Background(), prompt)
			if err == nil {
				t.Fatal("Run returned nil error")
			}
			if chat != nil || events != nil {
				t.Fatalf("Run returned chat/events on invalid prompt: %v %v", chat, events)
			}
			sdkErr, ok := AsError(err)
			if !ok {
				t.Fatalf("AsError returned false for %T", err)
			}
			if sdkErr.Code != codes.InvalidArgument {
				t.Fatalf("code = %s", sdkErr.Code)
			}
			if fake.startRequest != nil {
				t.Fatal("Run called StartChatRun")
			}
		})
	}
}

func TestChatRunRejectsEmptyPromptBeforeRPC(t *testing.T) {
	for _, prompt := range []string{"", "   \t\n"} {
		t.Run(prompt, func(t *testing.T) {
			fake := &fakeRuntimeClient{}
			client := newTestClient(t, fake)
			chat := &Chat{ID: "thread-existing", client: client}

			result, err := chat.Run(context.Background(), prompt)
			if err == nil {
				t.Fatal("chat.Run returned nil error")
			}
			if result != nil {
				t.Fatalf("chat.Run returned result on invalid prompt: %v", result)
			}
			sdkErr, ok := AsError(err)
			if !ok {
				t.Fatalf("AsError returned false for %T", err)
			}
			if sdkErr.Code != codes.InvalidArgument {
				t.Fatalf("code = %s", sdkErr.Code)
			}
			if fake.runRequest != nil {
				t.Fatal("chat.Run called RunChatTurn")
			}
		})
	}
}

func TestRunRejectsUnacceptedOrUncorrelatedStartResponse(t *testing.T) {
	tests := []struct {
		name     string
		response *pb.StartChatRunResponse
	}{
		{
			name: "first turn not accepted",
			response: &pb.StartChatRunResponse{
				ChatId:            "thread-123",
				RunId:             "turn-456",
				FirstTurnAccepted: false,
			},
		},
		{
			name: "empty chat id",
			response: &pb.StartChatRunResponse{
				RunId:             "turn-456",
				FirstTurnAccepted: true,
			},
		},
		{
			name: "empty run id",
			response: &pb.StartChatRunResponse{
				ChatId:            "thread-123",
				FirstTurnAccepted: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeRuntimeClient{
				startResponse: tt.response,
				stream:        &fakeEventStream{},
			}
			client := newTestClient(t, fake)

			chat, events, err := client.Run(context.Background(), "hello")
			if err == nil {
				t.Fatal("Run returned nil error")
			}
			if chat != nil || events != nil {
				t.Fatalf("Run returned chat/events on invalid gateway response: %v %v", chat, events)
			}
			sdkErr, ok := AsError(err)
			if !ok {
				t.Fatalf("AsError returned false for %T", err)
			}
			if sdkErr.Code != codes.Internal || sdkErr.Reason != "invalid_gateway_response" {
				t.Fatalf("sdk error = %#v", sdkErr)
			}
			if fake.streamRequest != nil {
				t.Fatal("Run subscribed to events after invalid start response")
			}
		})
	}
}

func TestChatRunRejectsUnacceptedOrMismatchedTurnResponse(t *testing.T) {
	tests := []struct {
		name     string
		response *pb.RunChatTurnResponse
	}{
		{
			name: "turn not accepted",
			response: &pb.RunChatTurnResponse{
				ChatId:       "thread-existing",
				RunId:        "turn-next",
				TurnAccepted: false,
			},
		},
		{
			name: "chat id mismatch",
			response: &pb.RunChatTurnResponse{
				ChatId:       "thread-other",
				RunId:        "turn-next",
				TurnAccepted: true,
			},
		},
		{
			name: "empty run id",
			response: &pb.RunChatTurnResponse{
				ChatId:       "thread-existing",
				TurnAccepted: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeRuntimeClient{runResponse: tt.response}
			client := newTestClient(t, fake)
			chat := &Chat{ID: "thread-existing", client: client}

			result, err := chat.Run(context.Background(), "continue")
			if err == nil {
				t.Fatal("chat.Run returned nil error")
			}
			if result != nil {
				t.Fatalf("chat.Run returned result on invalid gateway response: %v", result)
			}
			sdkErr, ok := AsError(err)
			if !ok {
				t.Fatalf("AsError returned false for %T", err)
			}
			if sdkErr.Code != codes.Internal || sdkErr.Reason != "invalid_gateway_response" {
				t.Fatalf("sdk error = %#v", sdkErr)
			}
		})
	}
}

func TestPendingAndInterruptHelpersUseTypedChatOperations(t *testing.T) {
	fake := &fakeRuntimeClient{
		pendingResponse: &pb.RespondChatPendingResponse{
			ChatId:           "thread-existing",
			RunId:            "turn-active",
			PendingRequestId: "pending-1",
			Accepted:         true,
			Status:           &pb.ChatStatus{ChatId: "thread-existing", CurrentRunId: "turn-active"},
		},
		interruptResponse: &pb.InterruptChatRunResponse{
			ChatId:        "thread-existing",
			RunId:         "turn-active",
			InterruptSent: true,
			Status:        &pb.ChatStatus{ChatId: "thread-existing", CurrentRunId: "turn-active"},
		},
	}
	client := newTestClient(t, fake)
	chat := &Chat{
		ID:     "thread-existing",
		client: client,
	}
	chat.setStatus(&pb.ChatStatus{CurrentRunId: "turn-active"})

	if _, err := chat.RespondApproval(context.Background(), "pending-1", "accept", WithClientResponseID("resp-1"), WithIdempotencyKey("idem-pending")); err != nil {
		t.Fatalf("RespondApproval returned error: %v", err)
	}
	if fake.pendingRequest.GetPendingRequestId() != "pending-1" {
		t.Fatalf("pending id = %q", fake.pendingRequest.GetPendingRequestId())
	}
	if fake.pendingRequest.GetApproval().GetDecisionId() != "accept" {
		t.Fatalf("approval decision = %q", fake.pendingRequest.GetApproval().GetDecisionId())
	}
	if fake.pendingRequest.GetIdempotencyKey() != "idem-pending" {
		t.Fatalf("pending idempotency_key = %q", fake.pendingRequest.GetIdempotencyKey())
	}
	assertAuthorization(t, fake.pendingCtx)

	if _, err := chat.Interrupt(context.Background(), WithClientRequestID("interrupt-1"), WithIdempotencyKey("idem-interrupt")); err != nil {
		t.Fatalf("Interrupt returned error: %v", err)
	}
	if fake.interruptRequest.GetRunId() != "turn-active" {
		t.Fatalf("interrupt run_id = %q", fake.interruptRequest.GetRunId())
	}
	if fake.interruptRequest.GetClientRequestId() != "interrupt-1" {
		t.Fatalf("interrupt client_request_id = %q", fake.interruptRequest.GetClientRequestId())
	}
	assertAuthorization(t, fake.interruptCtx)
}

func TestPendingHelperPayloadVariants(t *testing.T) {
	tests := []struct {
		name  string
		call  func(*Chat) (*pb.RespondChatPendingResponse, error)
		check func(*testing.T, *pb.RespondChatPendingRequest)
	}{
		{
			name: "permissions",
			call: func(chat *Chat) (*pb.RespondChatPendingResponse, error) {
				return chat.RespondPermissions(context.Background(), "pending-1", []string{"perm-1", "perm-2"}, pb.PermissionScope_PERMISSION_SCOPE_SESSION, true)
			},
			check: func(t *testing.T, req *pb.RespondChatPendingRequest) {
				t.Helper()
				got := req.GetPermissions()
				if got.GetScope() != pb.PermissionScope_PERMISSION_SCOPE_SESSION || !got.GetStrictAutoReview() {
					t.Fatalf("permissions response = %#v", got)
				}
				if len(got.GetPermissionIds()) != 2 || got.GetPermissionIds()[0] != "perm-1" {
					t.Fatalf("permission ids = %v", got.GetPermissionIds())
				}
			},
		},
		{
			name: "mcp elicitation",
			call: func(chat *Chat) (*pb.RespondChatPendingResponse, error) {
				return chat.RespondMcpElicitation(context.Background(), "pending-1", pb.McpElicitationAction_MCP_ELICITATION_ACTION_ACCEPT, `{"choice":"yes"}`)
			},
			check: func(t *testing.T, req *pb.RespondChatPendingRequest) {
				t.Helper()
				got := req.GetMcpElicitation()
				if got.GetAction() != pb.McpElicitationAction_MCP_ELICITATION_ACTION_ACCEPT || got.GetContentJson() == "" {
					t.Fatalf("mcp response = %#v", got)
				}
			},
		},
		{
			name: "tool user input",
			call: func(chat *Chat) (*pb.RespondChatPendingResponse, error) {
				return chat.RespondToolUserInput(context.Background(), "pending-1", []*pb.ToolUserInputAnswer{
					{QuestionId: "q1", Answers: []string{"answer"}},
				})
			},
			check: func(t *testing.T, req *pb.RespondChatPendingRequest) {
				t.Helper()
				got := req.GetToolUserInput()
				if len(got.GetAnswers()) != 1 || got.GetAnswers()[0].GetQuestionId() != "q1" {
					t.Fatalf("tool input response = %#v", got)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeRuntimeClient{
				pendingResponse: &pb.RespondChatPendingResponse{
					ChatId:           "thread-existing",
					RunId:            "turn-active",
					PendingRequestId: "pending-1",
					Accepted:         true,
				},
			}
			client := newTestClient(t, fake)
			chat := &Chat{ID: "thread-existing", client: client}

			if _, err := tt.call(chat); err != nil {
				t.Fatalf("%s returned error: %v", tt.name, err)
			}
			tt.check(t, fake.pendingRequest)
			assertAuthorization(t, fake.pendingCtx)
		})
	}
}

func TestPendingAndInterruptTypedNegativeErrors(t *testing.T) {
	fake := &fakeRuntimeClient{
		pendingErr: chatRuntimeStatusError(codes.FailedPrecondition, pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_FAILED_PRECONDITION, "pending_stale", "pending stale"),
	}
	client := newTestClient(t, fake)
	chat := &Chat{ID: "thread-existing", client: client}
	chat.setStatus(&pb.ChatStatus{CurrentRunId: "turn-active"})

	_, err := chat.RespondApproval(context.Background(), "pending-stale", "accept")
	assertSDKError(t, err, codes.FailedPrecondition, "pending_stale")

	fake = &fakeRuntimeClient{
		interruptErr: chatRuntimeStatusError(codes.FailedPrecondition, pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_FAILED_PRECONDITION, "no_active_run", "no active run"),
	}
	client = newTestClient(t, fake)
	chat = &Chat{ID: "thread-existing", client: client}
	chat.setStatus(&pb.ChatStatus{CurrentRunId: "turn-active"})

	_, err = chat.Interrupt(context.Background())
	assertSDKError(t, err, codes.FailedPrecondition, "no_active_run")
}

func TestInterruptRunUsesExplicitRunID(t *testing.T) {
	fake := &fakeRuntimeClient{
		interruptResponse: &pb.InterruptChatRunResponse{
			ChatId:        "thread-existing",
			RunId:         "turn-explicit",
			InterruptSent: true,
			Status:        &pb.ChatStatus{ChatId: "thread-existing", CurrentRunId: "turn-explicit"},
		},
	}
	client := newTestClient(t, fake)
	chat := &Chat{ID: "thread-existing", client: client}

	if _, err := chat.InterruptRun(context.Background(), "turn-explicit"); err != nil {
		t.Fatalf("InterruptRun returned error: %v", err)
	}
	if fake.interruptRequest.GetRunId() != "turn-explicit" {
		t.Fatalf("run id = %q", fake.interruptRequest.GetRunId())
	}
}

func TestRuntimeErrorDetailsMapToSDKError(t *testing.T) {
	st := status.New(codes.NotFound, "chat not found")
	st, err := st.WithDetails(&pb.ChatRuntimeErrorDetails{
		Outcome:        pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_NOT_FOUND,
		Reason:         "chat_not_found",
		DisplayMessage: "chat not found",
		ChatId:         "thread-missing",
		Retryable:      false,
	})
	if err != nil {
		t.Fatalf("WithDetails returned error: %v", err)
	}
	fake := &fakeRuntimeClient{getChatErr: st.Err()}
	client := newTestClient(t, fake)

	_, err = client.GetChat(context.Background(), "thread-missing")
	if err == nil {
		t.Fatal("GetChat returned nil error")
	}
	sdkErr, ok := AsError(err)
	if !ok {
		t.Fatalf("AsError returned false for %T", err)
	}
	if sdkErr.Code != codes.NotFound {
		t.Fatalf("code = %s", sdkErr.Code)
	}
	if sdkErr.Reason != "chat_not_found" {
		t.Fatalf("reason = %q", sdkErr.Reason)
	}
	if sdkErr.ChatID != "thread-missing" {
		t.Fatalf("chat id = %q", sdkErr.ChatID)
	}
}

func TestAuthenticatedContextOnlyForwardsBearerMetadata(t *testing.T) {
	fake := &fakeRuntimeClient{}
	client := newTestClient(t, fake)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		authorizationMetadataKey, "Bearer stale",
		authorizationMetadataKey, "Bearer duplicate",
		"cookie", "session=secret",
		"x-api-key", "secret",
		"x-request-id", "request-1",
	))

	authCtx, err := client.authenticatedContext(ctx)
	if err != nil {
		t.Fatalf("authenticatedContext returned error: %v", err)
	}
	md, ok := metadata.FromOutgoingContext(authCtx)
	if !ok {
		t.Fatal("metadata missing")
	}
	if got := md.Get(authorizationMetadataKey); len(got) != 1 || got[0] != bearerPrefix+testToken {
		t.Fatalf("authorization metadata = %v", got)
	}
	for _, key := range []string{"cookie", "x-api-key", "x-request-id"} {
		if got := md.Get(key); len(got) != 0 {
			t.Fatalf("%s metadata leaked: %v", key, got)
		}
	}
	if len(md) != 1 {
		t.Fatalf("metadata keys = %v, want only authorization", md)
	}
}

func TestClientStringRedactsBearerToken(t *testing.T) {
	client := newTestClient(t, &fakeRuntimeClient{})
	for _, rendered := range []string{
		client.String(),
		client.GoString(),
	} {
		if rendered == "" {
			t.Fatal("empty rendered client")
		}
		if contains(rendered, testToken) {
			t.Fatalf("rendered client leaked token: %s", rendered)
		}
	}
}

func TestStreamCloseDoesNotInterrupt(t *testing.T) {
	stream := &fakeEventStream{}
	fake := &fakeRuntimeClient{stream: stream}
	client := newTestClient(t, fake)
	chat := &Chat{ID: "thread-existing", client: client}

	events, err := chat.GetEventsStream(context.Background(), AfterEventID(7))
	if err != nil {
		t.Fatalf("GetEventsStream returned error: %v", err)
	}
	if err := events.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if got := fake.streamRequest.GetAfterEventId(); got != 7 {
		t.Fatalf("after_event_id = %d", got)
	}
	select {
	case <-fake.streamCtx.Done():
	default:
		t.Fatal("stream context was not cancelled")
	}
	if fake.interruptRequest != nil {
		t.Fatal("closing stream called InterruptChatRun")
	}
}

func TestStreamRecvCancellationDoesNotInterrupt(t *testing.T) {
	stream := &fakeEventStream{err: context.Canceled}
	fake := &fakeRuntimeClient{stream: stream}
	client := newTestClient(t, fake)
	chat := &Chat{ID: "thread-existing", client: client}

	events, err := chat.GetEventsStream(context.Background())
	if err != nil {
		t.Fatalf("GetEventsStream returned error: %v", err)
	}
	_, err = events.Recv()
	assertSDKError(t, err, codes.Canceled, "caller_canceled")
	if fake.interruptRequest != nil {
		t.Fatal("stream cancellation called InterruptChatRun")
	}
}

func TestStreamOptionsAndRunFallbackCursor(t *testing.T) {
	fake := &fakeRuntimeClient{stream: &fakeEventStream{}}
	client := newTestClient(t, fake)
	chat := &Chat{ID: "thread-existing", client: client}

	if _, err := chat.GetEventsStream(context.Background(), FromStart(), WithClientSubscriberID("subscriber-1")); err != nil {
		t.Fatalf("GetEventsStream FromStart returned error: %v", err)
	}
	if fake.streamRequest.GetFromStart() == nil {
		t.Fatalf("from_start cursor not set: %#v", fake.streamRequest)
	}
	if fake.streamRequest.GetClientSubscriberId() != "subscriber-1" {
		t.Fatalf("subscriber id = %q", fake.streamRequest.GetClientSubscriberId())
	}

	if _, err := chat.GetEventsStream(context.Background(), AfterEventCursor("cursor-2")); err != nil {
		t.Fatalf("GetEventsStream AfterEventCursor returned error: %v", err)
	}
	if got := fake.streamRequest.GetAfterEventCursor(); got != "cursor-2" {
		t.Fatalf("event cursor = %q", got)
	}

	fake = &fakeRuntimeClient{
		startResponse: &pb.StartChatRunResponse{
			ChatId:            "thread-new",
			RunId:             "turn-new",
			LastEventId:       12,
			FirstTurnAccepted: true,
		},
		stream: &fakeEventStream{},
	}
	client = newTestClient(t, fake)
	if _, _, err := client.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := fake.streamRequest.GetAfterEventId(); got != 12 {
		t.Fatalf("fallback event id = %d", got)
	}
}

func TestErrorMappingMatrix(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		code    codes.Code
		outcome pb.ChatOutcomeCategory
	}{
		{name: "context canceled", err: context.Canceled, code: codes.Canceled, outcome: pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_CANCELLED},
		{name: "deadline", err: context.DeadlineExceeded, code: codes.DeadlineExceeded, outcome: pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_DEADLINE_EXCEEDED},
		{name: "unavailable no details", err: status.Error(codes.Unavailable, "unavailable"), code: codes.Unavailable, outcome: pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNAVAILABLE},
		{name: "unimplemented no details", err: status.Error(codes.Unimplemented, "unsupported"), code: codes.Unimplemented, outcome: pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNSUPPORTED},
		{name: "out of range no details", err: status.Error(codes.OutOfRange, "cursor out of range"), code: codes.OutOfRange, outcome: pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_OUT_OF_RANGE},
		{name: "aborted no details", err: status.Error(codes.Aborted, "conflict"), code: codes.Aborted, outcome: pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_ABORTED},
		{name: "resource exhausted no details", err: status.Error(codes.ResourceExhausted, "too large"), code: codes.ResourceExhausted, outcome: pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNAVAILABLE},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := normalizeError(tt.err)
			sdkErr, ok := AsError(err)
			if !ok {
				t.Fatalf("AsError returned false for %T", err)
			}
			if sdkErr.Code != tt.code || sdkErr.Outcome != tt.outcome {
				t.Fatalf("sdk error = %#v", sdkErr)
			}
		})
	}
}

func TestGatewayErrorDetailsMapToSDKError(t *testing.T) {
	st := status.New(codes.ResourceExhausted, "message too large")
	st, err := st.WithDetails(&pb.GatewayErrorDetails{
		Reason:           "resource_exhausted",
		DisplayMessage:   "message too large",
		SessionGroupId:   testSessionGroupID,
		ThreadId:         "thread-existing",
		PendingRequestId: "pending-1",
		Retryable:        true,
	})
	if err != nil {
		t.Fatalf("WithDetails returned error: %v", err)
	}
	sdkErr, ok := AsError(normalizeError(st.Err()))
	if !ok {
		t.Fatal("AsError returned false")
	}
	if sdkErr.Code != codes.ResourceExhausted {
		t.Fatalf("code = %s", sdkErr.Code)
	}
	if sdkErr.Reason != "resource_exhausted" || sdkErr.ChatID != "thread-existing" || sdkErr.PendingRequestID != "pending-1" || !sdkErr.Retryable {
		t.Fatalf("gateway details not mapped: %#v", sdkErr)
	}
}

func newTestClient(t *testing.T, fake *fakeRuntimeClient) *Client {
	t.Helper()
	client, err := NewWithClient(
		fake,
		WithBearerToken(testToken),
		WithSessionGroupID(testSessionGroupID),
		WithWorkspaceID(testWorkspaceID),
	)
	if err != nil {
		t.Fatalf("NewWithClient returned error: %v", err)
	}
	return client
}

func assertAuthorization(t *testing.T, ctx context.Context) {
	t.Helper()
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("outgoing metadata missing")
	}
	values := md.Get(authorizationMetadataKey)
	if len(values) != 1 {
		t.Fatalf("authorization values = %d, want 1", len(values))
	}
	if values[0] != bearerPrefix+testToken {
		t.Fatalf("authorization value was not bearer token")
	}
}

func assertSDKError(t *testing.T, err error, code codes.Code, reason string) {
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
	if reason != "" && sdkErr.Reason != reason {
		t.Fatalf("reason = %q, want %q", sdkErr.Reason, reason)
	}
}

func assertGeneratedID(t *testing.T, value string, prefix string) {
	t.Helper()
	if !strings.HasPrefix(value, prefix) {
		t.Fatalf("generated id = %q, want prefix %q", value, prefix)
	}
	if len(value) <= len(prefix) {
		t.Fatalf("generated id = %q is too short", value)
	}
}

func chatRuntimeStatusError(code codes.Code, outcome pb.ChatOutcomeCategory, reason string, message string) error {
	st := status.New(code, message)
	st, err := st.WithDetails(&pb.ChatRuntimeErrorDetails{
		Outcome:        outcome,
		Reason:         reason,
		DisplayMessage: message,
		Retryable:      code == codes.Unavailable,
	})
	if err != nil {
		panic(err)
	}
	return st.Err()
}

type fakeRuntimeClient struct {
	startCtx      context.Context
	startRequest  *pb.StartChatRunRequest
	startResponse *pb.StartChatRunResponse
	startErr      error

	getChatCtx      context.Context
	getChatRequest  *pb.GetChatRequest
	getChatResponse *pb.GetChatResponse
	getChatErr      error

	runCtx      context.Context
	runRequest  *pb.RunChatTurnRequest
	runResponse *pb.RunChatTurnResponse
	runErr      error

	statusCtx      context.Context
	statusRequest  *pb.GetChatStatusRequest
	statusResponse *pb.GetChatStatusResponse
	statusErr      error

	historyCtx      context.Context
	historyRequest  *pb.GetChatHistoryRequest
	historyResponse *pb.GetChatHistoryResponse
	historyErr      error

	streamCtx     context.Context
	streamRequest *pb.StreamChatEventsRequest
	stream        pb.ChatRuntimeService_StreamChatEventsClient
	streamErr     error

	pendingCtx      context.Context
	pendingRequest  *pb.RespondChatPendingRequest
	pendingResponse *pb.RespondChatPendingResponse
	pendingErr      error

	interruptCtx      context.Context
	interruptRequest  *pb.InterruptChatRunRequest
	interruptResponse *pb.InterruptChatRunResponse
	interruptErr      error
}

func (f *fakeRuntimeClient) StartChatRun(ctx context.Context, in *pb.StartChatRunRequest, opts ...grpc.CallOption) (*pb.StartChatRunResponse, error) {
	f.startCtx = ctx
	f.startRequest = in
	return f.startResponse, f.startErr
}

func (f *fakeRuntimeClient) GetChat(ctx context.Context, in *pb.GetChatRequest, opts ...grpc.CallOption) (*pb.GetChatResponse, error) {
	f.getChatCtx = ctx
	f.getChatRequest = in
	return f.getChatResponse, f.getChatErr
}

func (f *fakeRuntimeClient) RunChatTurn(ctx context.Context, in *pb.RunChatTurnRequest, opts ...grpc.CallOption) (*pb.RunChatTurnResponse, error) {
	f.runCtx = ctx
	f.runRequest = in
	return f.runResponse, f.runErr
}

func (f *fakeRuntimeClient) GetChatStatus(ctx context.Context, in *pb.GetChatStatusRequest, opts ...grpc.CallOption) (*pb.GetChatStatusResponse, error) {
	f.statusCtx = ctx
	f.statusRequest = in
	return f.statusResponse, f.statusErr
}

func (f *fakeRuntimeClient) GetChatHistory(ctx context.Context, in *pb.GetChatHistoryRequest, opts ...grpc.CallOption) (*pb.GetChatHistoryResponse, error) {
	f.historyCtx = ctx
	f.historyRequest = in
	return f.historyResponse, f.historyErr
}

func (f *fakeRuntimeClient) StreamChatEvents(ctx context.Context, in *pb.StreamChatEventsRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.StreamChatEventsResponse], error) {
	f.streamCtx = ctx
	f.streamRequest = in
	return f.stream, f.streamErr
}

func (f *fakeRuntimeClient) RespondChatPending(ctx context.Context, in *pb.RespondChatPendingRequest, opts ...grpc.CallOption) (*pb.RespondChatPendingResponse, error) {
	f.pendingCtx = ctx
	f.pendingRequest = in
	return f.pendingResponse, f.pendingErr
}

func (f *fakeRuntimeClient) InterruptChatRun(ctx context.Context, in *pb.InterruptChatRunRequest, opts ...grpc.CallOption) (*pb.InterruptChatRunResponse, error) {
	f.interruptCtx = ctx
	f.interruptRequest = in
	return f.interruptResponse, f.interruptErr
}

type fakeEventStream struct {
	grpc.ClientStream
	messages []*pb.StreamChatEventsResponse
	err      error
	closed   bool
}

func (s *fakeEventStream) Recv() (*pb.StreamChatEventsResponse, error) {
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

func (s *fakeEventStream) CloseSend() error {
	s.closed = true
	return nil
}

func TestNormalizeEOFLeavesEOF(t *testing.T) {
	if got := normalizeError(io.EOF); !errors.Is(got, io.EOF) {
		t.Fatalf("normalizeError(io.EOF) = %v", got)
	}
}

func contains(value string, needle string) bool {
	return len(needle) > 0 && len(value) >= len(needle) && strings.Contains(value, needle)
}
