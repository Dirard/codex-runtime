package grpcapi

import (
	"fmt"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RequestError struct {
	Code    codes.Code
	Details domain.GatewayErrorDetails
}

func (e *RequestError) Error() string {
	if e == nil {
		return ""
	}
	if e.Details.DisplayMessage != "" {
		return e.Details.DisplayMessage
	}
	return string(e.Details.Reason)
}

func invalidArgument(reason domain.GatewayErrorReason, message string) *RequestError {
	return &RequestError{
		Code: codes.InvalidArgument,
		Details: domain.GatewayErrorDetails{
			Reason:         reason,
			DisplayMessage: message,
		},
	}
}

func resourceExhausted(message string) *RequestError {
	return &RequestError{
		Code: codes.ResourceExhausted,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonResourceExhausted,
			DisplayMessage: message,
		},
	}
}

func notFound(reason domain.GatewayErrorReason, message string) *RequestError {
	return &RequestError{
		Code: codes.NotFound,
		Details: domain.GatewayErrorDetails{
			Reason:         reason,
			DisplayMessage: message,
		},
	}
}

func internalGatewayRequestError() *RequestError {
	return &RequestError{
		Code: codes.Internal,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonInternalGatewayError,
			DisplayMessage: "internal gateway error",
		},
	}
}

func GatewayErrorDetailsToProto(details domain.GatewayErrorDetails) *pb.GatewayErrorDetails {
	if failure := gatewayErrorDetailsMappingFailure(details); failure != nil {
		details = safeGatewayErrorDetails(details, failure)
	}
	details.Reason = canonicalGatewayErrorReason(details.Reason)
	return &pb.GatewayErrorDetails{
		Reason:           string(details.Reason),
		DisplayMessage:   details.DisplayMessage,
		TaskId:           details.TaskID,
		SessionGroupId:   details.SessionGroupID,
		ClientMessageId:  details.ClientMessageID,
		ClientResponseId: details.ClientResponseID,
		PendingRequestId: details.PendingRequestID,
		ThreadId:         details.ThreadID,
		Retryable:        details.Retryable,
	}
}

func GatewayErrorDetailsFromProto(details *pb.GatewayErrorDetails) domain.GatewayErrorDetails {
	if details == nil {
		return domain.GatewayErrorDetails{}
	}
	return domain.GatewayErrorDetails{
		Reason:           domain.GatewayErrorReason(details.GetReason()),
		DisplayMessage:   details.GetDisplayMessage(),
		TaskID:           details.GetTaskId(),
		SessionGroupID:   details.GetSessionGroupId(),
		ClientMessageID:  details.GetClientMessageId(),
		ClientResponseID: details.GetClientResponseId(),
		PendingRequestID: details.GetPendingRequestId(),
		ThreadID:         details.GetThreadId(),
		Retryable:        details.GetRetryable(),
	}
}

func NewStatusError(code codes.Code, details domain.GatewayErrorDetails) error {
	if failure := gatewayErrorDetailsMappingFailure(details); failure != nil {
		if failure.Reason == domain.ReasonResourceExhausted {
			code = codes.ResourceExhausted
		} else {
			code = codes.Internal
		}
		details = safeGatewayErrorDetails(details, failure)
	}
	details.Reason = canonicalGatewayErrorReason(details.Reason)
	message := details.DisplayMessage
	if message == "" {
		message = string(details.Reason)
	}
	st := status.New(code, message)
	stWithDetails, err := st.WithDetails(GatewayErrorDetailsToProto(details))
	if err != nil {
		return status.Error(codes.Internal, fmt.Sprintf("failed to attach gateway error details: %v", err))
	}
	return stWithDetails.Err()
}

func StatusErrorFromRequestError(err *RequestError) error {
	if err == nil {
		return nil
	}
	return NewStatusError(err.Code, err.Details)
}

func canonicalGatewayErrorReason(reason domain.GatewayErrorReason) domain.GatewayErrorReason {
	for _, canonicalReason := range domain.CanonicalGatewayErrorReasons {
		if reason == canonicalReason {
			return reason
		}
	}
	return domain.ReasonInternalGatewayError
}
