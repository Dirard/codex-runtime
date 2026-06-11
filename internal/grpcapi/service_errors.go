package grpcapi

import (
	"context"
	"errors"

	"github.com/Dirard/codex-runtime/internal/domain"
	"google.golang.org/grpc/codes"
)

func statusErrorFromServiceError(err error) error {
	if err == nil {
		return nil
	}
	var requestError *RequestError
	if errors.As(err, &requestError) {
		return StatusErrorFromRequestError(requestError)
	}
	var gatewayError *domain.GatewayError
	if errors.As(err, &gatewayError) {
		return NewStatusError(codeFromDomainGatewayError(gatewayError.Code), gatewayError.Details)
	}
	if errors.Is(err, context.Canceled) {
		return NewStatusError(codes.Canceled, domain.GatewayErrorDetails{
			Reason:         domain.ReasonCallerCanceled,
			DisplayMessage: "caller canceled",
		})
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewStatusError(codes.DeadlineExceeded, domain.GatewayErrorDetails{
			Reason:         domain.ReasonCallerDeadlineExceeded,
			DisplayMessage: "caller deadline exceeded",
		})
	}
	return redactedInternalStatusError()
}

func codeFromDomainGatewayError(code domain.GatewayErrorCode) codes.Code {
	switch code {
	case domain.GatewayErrorCodeInvalidArgument:
		return codes.InvalidArgument
	case domain.GatewayErrorCodeNotFound:
		return codes.NotFound
	case domain.GatewayErrorCodeFailedPrecondition:
		return codes.FailedPrecondition
	case domain.GatewayErrorCodeCanceled:
		return codes.Canceled
	case domain.GatewayErrorCodeDeadlineExceeded:
		return codes.DeadlineExceeded
	case domain.GatewayErrorCodeResourceExhausted:
		return codes.ResourceExhausted
	case domain.GatewayErrorCodeUnavailable:
		return codes.Unavailable
	case domain.GatewayErrorCodeInternal:
		return codes.Internal
	default:
		return codes.Internal
	}
}

func statusErrorFromMappingFailure(failure *MappingFailure) error {
	if failure == nil {
		return nil
	}
	code := codes.Internal
	if failure.Reason == domain.ReasonResourceExhausted {
		code = codes.ResourceExhausted
	}
	return NewStatusError(code, domain.GatewayErrorDetails{
		Reason:         failure.Reason,
		DisplayMessage: failure.Message,
	})
}
