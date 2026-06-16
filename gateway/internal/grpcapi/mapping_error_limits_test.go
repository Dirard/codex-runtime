package grpcapi

import (
	"strings"
	"testing"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReplayNoticeToProtoWithFailureRejectsOverCapMessage(t *testing.T) {
	notice := domain.ReplayNotice{
		Code:    domain.ReplayNoticeCursorEvicted,
		Message: overCap(domain.MaxOutboundErrorDisplayMessageBytes),
	}

	got, failure := ReplayNoticeToProtoWithFailure(notice)
	assertResourceExhaustedMappingFailure(t, "ReplayNoticeToProtoWithFailure(over-cap message)", got, failure)
	if got, ok := ReplayNoticeToProto(notice); ok || got != nil {
		t.Fatalf("ReplayNoticeToProto(over-cap message) = (%v, %t), want (nil, false)", got, ok)
	}

	streamGot, streamFailure := StreamTaskResponseReplayNoticeToProtoWithFailure(notice)
	assertResourceExhaustedMappingFailure(t, "StreamTaskResponseReplayNoticeToProtoWithFailure(over-cap message)", streamGot, streamFailure)
	if got, ok := StreamTaskResponseReplayNoticeToProto(notice); ok || got != nil {
		t.Fatalf("StreamTaskResponseReplayNoticeToProto(over-cap message) = (%v, %t), want (nil, false)", got, ok)
	}
}

func TestGatewayErrorDetailsRejectsOverCapDisplayMessage(t *testing.T) {
	overCapMessage := overCap(domain.MaxOutboundErrorDisplayMessageBytes)
	details := domain.GatewayErrorDetails{
		Reason:         domain.ReasonInvalidRequest,
		DisplayMessage: overCapMessage,
		TaskID:         "task-1",
	}

	protoDetails := GatewayErrorDetailsToProto(details)
	if protoDetails.GetDisplayMessage() == overCapMessage {
		t.Fatal("GatewayErrorDetailsToProto() serialized over-cap display_message")
	}
	if len(protoDetails.GetDisplayMessage()) > domain.MaxOutboundErrorDisplayMessageBytes {
		t.Fatalf("GatewayErrorDetailsToProto() display_message bytes = %d, want <= %d", len(protoDetails.GetDisplayMessage()), domain.MaxOutboundErrorDisplayMessageBytes)
	}
	if protoDetails.GetReason() != string(domain.ReasonResourceExhausted) {
		t.Fatalf("GatewayErrorDetailsToProto() reason = %q, want %q", protoDetails.GetReason(), domain.ReasonResourceExhausted)
	}

	err := NewStatusError(codes.InvalidArgument, details)
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%v) failed", err)
	}
	if st.Code() != codes.ResourceExhausted {
		t.Fatalf("status code = %s, want %s", st.Code(), codes.ResourceExhausted)
	}
	if st.Message() == overCapMessage {
		t.Fatal("NewStatusError() serialized over-cap message into status text")
	}
	if len(st.Message()) > domain.MaxOutboundErrorDisplayMessageBytes {
		t.Fatalf("status message bytes = %d, want <= %d", len(st.Message()), domain.MaxOutboundErrorDisplayMessageBytes)
	}
	statusDetails := st.Details()
	if len(statusDetails) != 1 {
		t.Fatalf("status details count = %d, want 1", len(statusDetails))
	}
	statusProtoDetails, ok := statusDetails[0].(*pb.GatewayErrorDetails)
	if !ok {
		t.Fatalf("status detail type = %T, want GatewayErrorDetails", statusDetails[0])
	}
	if statusProtoDetails.GetReason() != string(domain.ReasonResourceExhausted) {
		t.Fatalf("status detail reason = %q, want %q", statusProtoDetails.GetReason(), domain.ReasonResourceExhausted)
	}
	if statusProtoDetails.GetDisplayMessage() == overCapMessage {
		t.Fatal("NewStatusError() serialized over-cap display_message into status details")
	}
	if len(statusProtoDetails.GetDisplayMessage()) > domain.MaxOutboundErrorDisplayMessageBytes {
		t.Fatalf("status detail display_message bytes = %d, want <= %d", len(statusProtoDetails.GetDisplayMessage()), domain.MaxOutboundErrorDisplayMessageBytes)
	}
}

func TestGatewayErrorDetailsRejectsOverCapPublicIDs(t *testing.T) {
	overCapID := overCap(domain.MaxPublicIDBytes)
	tests := []struct {
		name   string
		mutate func(*domain.GatewayErrorDetails)
	}{
		{
			name: "task_id",
			mutate: func(details *domain.GatewayErrorDetails) {
				details.TaskID = overCapID
			},
		},
		{
			name: "session_group_id",
			mutate: func(details *domain.GatewayErrorDetails) {
				details.SessionGroupID = overCapID
			},
		},
		{
			name: "client_message_id",
			mutate: func(details *domain.GatewayErrorDetails) {
				details.ClientMessageID = overCapID
			},
		},
		{
			name: "client_response_id",
			mutate: func(details *domain.GatewayErrorDetails) {
				details.ClientResponseID = overCapID
			},
		},
		{
			name: "pending_request_id",
			mutate: func(details *domain.GatewayErrorDetails) {
				details.PendingRequestID = overCapID
			},
		},
		{
			name: "thread_id",
			mutate: func(details *domain.GatewayErrorDetails) {
				details.ThreadID = overCapID
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details := domain.GatewayErrorDetails{
				Reason:           domain.ReasonInvalidRequest,
				DisplayMessage:   "safe display",
				TaskID:           "task-1",
				SessionGroupID:   "sg-1",
				ClientMessageID:  "client-message-1",
				ClientResponseID: "client-response-1",
				PendingRequestID: "pending-1",
				ThreadID:         "thread-1",
			}
			tt.mutate(&details)

			protoDetails := GatewayErrorDetailsToProto(details)
			if protoDetails.GetReason() != string(domain.ReasonResourceExhausted) {
				t.Fatalf("GatewayErrorDetailsToProto() reason = %q, want %q", protoDetails.GetReason(), domain.ReasonResourceExhausted)
			}
			assertGatewayErrorDetailsDoesNotContainPublicID(t, "GatewayErrorDetailsToProto()", protoDetails, overCapID)

			err := NewStatusError(codes.InvalidArgument, details)
			st, ok := status.FromError(err)
			if !ok {
				t.Fatalf("status.FromError(%v) failed", err)
			}
			if st.Code() != codes.ResourceExhausted {
				t.Fatalf("status code = %s, want %s", st.Code(), codes.ResourceExhausted)
			}
			if strings.Contains(st.Message(), overCapID) {
				t.Fatal("NewStatusError() serialized over-cap public id into status text")
			}
			statusDetails := st.Details()
			if len(statusDetails) != 1 {
				t.Fatalf("status details count = %d, want 1", len(statusDetails))
			}
			statusProtoDetails, ok := statusDetails[0].(*pb.GatewayErrorDetails)
			if !ok {
				t.Fatalf("status detail type = %T, want GatewayErrorDetails", statusDetails[0])
			}
			assertGatewayErrorDetailsDoesNotContainPublicID(t, "NewStatusError()", statusProtoDetails, overCapID)
		})
	}
}

func TestGatewayErrorDetailsClearsPaddedPublicIDs(t *testing.T) {
	paddedID := " task-1 "
	details := domain.GatewayErrorDetails{
		Reason:         domain.ReasonInvalidRequest,
		DisplayMessage: "safe display",
		TaskID:         paddedID,
		SessionGroupID: "sg-1",
	}

	protoDetails := GatewayErrorDetailsToProto(details)
	if protoDetails.GetReason() != string(domain.ReasonInternalGatewayError) {
		t.Fatalf("GatewayErrorDetailsToProto() reason = %q, want %q", protoDetails.GetReason(), domain.ReasonInternalGatewayError)
	}
	if protoDetails.GetTaskId() != "" {
		t.Fatalf("GatewayErrorDetailsToProto() task_id = %q, want empty", protoDetails.GetTaskId())
	}
	assertGatewayErrorDetailsDoesNotContainPublicID(t, "GatewayErrorDetailsToProto()", protoDetails, paddedID)

	err := NewStatusError(codes.InvalidArgument, details)
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%v) failed", err)
	}
	if st.Code() != codes.Internal {
		t.Fatalf("status code = %s, want %s", st.Code(), codes.Internal)
	}
	if strings.Contains(st.Message(), paddedID) {
		t.Fatal("NewStatusError() serialized padded public id into status text")
	}
	statusDetails := st.Details()
	if len(statusDetails) != 1 {
		t.Fatalf("status details count = %d, want 1", len(statusDetails))
	}
	statusProtoDetails, ok := statusDetails[0].(*pb.GatewayErrorDetails)
	if !ok {
		t.Fatalf("status detail type = %T, want GatewayErrorDetails", statusDetails[0])
	}
	if statusProtoDetails.GetReason() != string(domain.ReasonInternalGatewayError) {
		t.Fatalf("status detail reason = %q, want %q", statusProtoDetails.GetReason(), domain.ReasonInternalGatewayError)
	}
	if statusProtoDetails.GetTaskId() != "" {
		t.Fatalf("NewStatusError() task_id = %q, want empty", statusProtoDetails.GetTaskId())
	}
	assertGatewayErrorDetailsDoesNotContainPublicID(t, "NewStatusError()", statusProtoDetails, paddedID)
}

func TestGatewayErrorDetailsNormalizesUnknownReason(t *testing.T) {
	details := domain.GatewayErrorDetails{
		Reason: domain.GatewayErrorReason("non_canonical_reason"),
	}

	protoDetails := GatewayErrorDetailsToProto(details)
	if protoDetails.GetReason() != string(domain.ReasonInternalGatewayError) {
		t.Fatalf("GatewayErrorDetailsToProto() reason = %q, want %q", protoDetails.GetReason(), domain.ReasonInternalGatewayError)
	}

	err := NewStatusError(codes.InvalidArgument, details)
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%v) failed", err)
	}
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("status code = %s, want %s", st.Code(), codes.InvalidArgument)
	}
	if st.Message() != string(domain.ReasonInternalGatewayError) {
		t.Fatalf("status message = %q, want %q", st.Message(), domain.ReasonInternalGatewayError)
	}
	statusDetails := st.Details()
	if len(statusDetails) != 1 {
		t.Fatalf("status details count = %d, want 1", len(statusDetails))
	}
	statusProtoDetails, ok := statusDetails[0].(*pb.GatewayErrorDetails)
	if !ok {
		t.Fatalf("status detail type = %T, want GatewayErrorDetails", statusDetails[0])
	}
	if statusProtoDetails.GetReason() != string(domain.ReasonInternalGatewayError) {
		t.Fatalf("status detail reason = %q, want %q", statusProtoDetails.GetReason(), domain.ReasonInternalGatewayError)
	}
}

func assertGatewayErrorDetailsDoesNotContainPublicID(t *testing.T, name string, details *pb.GatewayErrorDetails, publicID string) {
	t.Helper()

	if details.GetTaskId() == publicID ||
		details.GetSessionGroupId() == publicID ||
		details.GetClientMessageId() == publicID ||
		details.GetClientResponseId() == publicID ||
		details.GetPendingRequestId() == publicID ||
		details.GetThreadId() == publicID {
		t.Fatalf("%s serialized public id into details: %v", name, details)
	}
}
