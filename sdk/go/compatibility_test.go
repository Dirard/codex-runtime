package codex

import (
	"context"
	"testing"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
)

func TestCompatibilityRawAliasesAndSignaturesCompile(t *testing.T) {
	var rawEvent Event = pb.StreamChatEventsResponse{}
	rawEvent.Payload = &pb.StreamChatEventsResponse_Event{
		Event: &pb.ChatEvent{EventId: 1, EventCursor: "cursor-1"},
	}
	var rawPayload EventPayload = pb.ChatEvent{EventId: 2}
	var pending PendingRequest = pb.ChatPendingRequest{PendingRequestId: "pending-1"}
	var pendingResponse PendingResponse = pb.RespondChatPendingResponse{PendingRequestId: "pending-1"}
	var interrupt InterruptResponse = pb.InterruptChatRunResponse{RunId: "run-1"}
	var capability CapabilitySet = pb.ChatCapabilitySet{EventStream: pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED}

	if rawEvent.GetEvent().GetEventCursor() != "cursor-1" {
		t.Fatal("raw Event alias did not expose protobuf payload")
	}
	if rawPayload.GetEventId() != 2 || pending.GetPendingRequestId() != "pending-1" ||
		pendingResponse.GetPendingRequestId() != "pending-1" || interrupt.GetRunId() != "run-1" ||
		capability.GetEventStream() != pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED {
		t.Fatalf("raw aliases no longer behave as protobuf aliases")
	}

	var _ func(*EventStream) (*pb.StreamChatEventsResponse, error) = (*EventStream).Recv
	var _ func(*Workflow, context.Context, string, ...RequestOption) (*Chat, *EventStream, error) = (*Workflow).Run
	var _ func(*Chat, context.Context, string, ...RequestOption) (*RunResult, error) = (*Chat).Run
	var _ func(*Chat, context.Context, ...StreamOption) (*EventStream, error) = (*Chat).GetEventsStream
	var _ func(*Chat, context.Context, string, string, ...RequestOption) (*pb.RespondChatPendingResponse, error) = (*Chat).RespondApproval
	var _ func(*Chat, context.Context, string, []string, pb.PermissionScope, bool, ...RequestOption) (*pb.RespondChatPendingResponse, error) = (*Chat).RespondPermissions
	var _ func(*Chat, context.Context, string, pb.McpElicitationAction, string, ...RequestOption) (*pb.RespondChatPendingResponse, error) = (*Chat).RespondMcpElicitation
	var _ func(*Chat, context.Context, string, []*pb.ToolUserInputAnswer, ...RequestOption) (*pb.RespondChatPendingResponse, error) = (*Chat).RespondToolUserInput
	var _ func(*Chat, context.Context, ...RequestOption) (*pb.InterruptChatRunResponse, error) = (*Chat).Interrupt
	var _ func(*Chat, context.Context, string, ...RequestOption) (*pb.InterruptChatRunResponse, error) = (*Chat).InterruptRun
}

func TestCompatibilityEventStreamRecvReturnsRawProtobuf(t *testing.T) {
	want := &pb.StreamChatEventsResponse{
		Payload: &pb.StreamChatEventsResponse_Event{
			Event: &pb.ChatEvent{
				EventId:     7,
				EventCursor: "cursor-7",
				ChatId:      "thread-1",
				RunId:       "run-1",
			},
		},
	}
	events := &EventStream{stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{want}}}

	got, err := events.Recv()
	if err != nil {
		t.Fatalf("Recv returned error: %v", err)
	}
	if got != want {
		t.Fatalf("Recv returned %#v, want the raw protobuf message pointer %#v", got, want)
	}
}

func TestCompatibilityChatRunResultCarriesCursorFields(t *testing.T) {
	fake := &fakeRuntimeClient{
		runResponse: &pb.RunChatTurnResponse{
			ChatId:       "thread-existing",
			RunId:        "turn-next",
			LastEventId:  42,
			EventCursor:  "opaque-cursor-42",
			TurnAccepted: true,
		},
	}
	client := newTestClient(t, fake)
	chat := &Chat{ID: "thread-existing", client: client}

	result, err := chat.Run(context.Background(), "continue")
	if err != nil {
		t.Fatalf("chat.Run returned error: %v", err)
	}
	if result.ChatID != "thread-existing" || result.RunID != "turn-next" {
		t.Fatalf("result identity = %s/%s", result.ChatID, result.RunID)
	}
	if result.LastEventID != 42 || result.EventCursor != "opaque-cursor-42" {
		t.Fatalf("result cursor fields = %d/%q", result.LastEventID, result.EventCursor)
	}
}
