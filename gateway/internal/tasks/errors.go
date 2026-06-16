package tasks

import (
	"errors"

	"github.com/Dirard/codex-runtime/gateway/internal/contextpack"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

func unknownSession(sessionGroupID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeNotFound,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonUnknownSessionGroup,
			DisplayMessage: "unknown session group",
			SessionGroupID: sessionGroupID,
		},
	}
}

func unknownTask(sessionGroupID string, taskID string, clientMessageID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeNotFound,
		Details: domain.GatewayErrorDetails{
			Reason:          domain.ReasonUnknownTask,
			DisplayMessage:  "unknown task",
			TaskID:          taskID,
			SessionGroupID:  sessionGroupID,
			ClientMessageID: clientMessageID,
		},
	}
}

func unknownPendingRequest(sessionGroupID string, taskID string, pendingRequestID string, clientResponseID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeNotFound,
		Details: domain.GatewayErrorDetails{
			Reason:           domain.ReasonUnknownPendingRequest,
			DisplayMessage:   "unknown pending request",
			TaskID:           taskID,
			SessionGroupID:   sessionGroupID,
			PendingRequestID: pendingRequestID,
			ClientResponseID: clientResponseID,
		},
	}
}

func unknownThreadBinding(sessionGroupID string, threadID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeNotFound,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonUnknownThreadBinding,
			DisplayMessage: "unknown thread binding",
			SessionGroupID: sessionGroupID,
			ThreadID:       threadID,
		},
	}
}

func expiredThreadBinding(sessionGroupID string, threadID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeNotFound,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonThreadBindingExpired,
			DisplayMessage: threadBindingExpiredMessage,
			SessionGroupID: sessionGroupID,
			ThreadID:       threadID,
		},
	}
}

func alreadyRunning(sessionGroupID string, taskID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeFailedPrecondition,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonAlreadyRunning,
			DisplayMessage: "a task is already running in this session group",
			TaskID:         taskID,
			SessionGroupID: sessionGroupID,
		},
	}
}

func idempotencyMismatch(sessionGroupID string, clientMessageID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeFailedPrecondition,
		Details: domain.GatewayErrorDetails{
			Reason:          domain.ReasonIdempotencyFingerprintMismatch,
			DisplayMessage:  "client_message_id was reused with different task input",
			SessionGroupID:  sessionGroupID,
			ClientMessageID: clientMessageID,
		},
	}
}

func workspaceMismatch(sessionGroupID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeInvalidArgument,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonWorkspaceMismatch,
			DisplayMessage: "workspace does not match session group",
			SessionGroupID: sessionGroupID,
		},
	}
}

func startInterruptedBeforeTurn(task *task) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeCanceled,
		Details: domain.GatewayErrorDetails{
			Reason:          domain.ReasonStartInterruptedBeforeTurn,
			DisplayMessage:  preTurnInterruptMessage,
			TaskID:          task.id,
			SessionGroupID:  task.sessionGroupID,
			ClientMessageID: task.clientMessageID,
		},
	}
}

func callerCanceled(sessionGroupID string, clientMessageID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeCanceled,
		Details: domain.GatewayErrorDetails{
			Reason:          domain.ReasonCallerCanceled,
			DisplayMessage:  "caller canceled",
			SessionGroupID:  sessionGroupID,
			ClientMessageID: clientMessageID,
		},
	}
}

func callerDeadline(sessionGroupID string, clientMessageID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeDeadlineExceeded,
		Details: domain.GatewayErrorDetails{
			Reason:          domain.ReasonCallerDeadlineExceeded,
			DisplayMessage:  "caller deadline exceeded",
			SessionGroupID:  sessionGroupID,
			ClientMessageID: clientMessageID,
		},
	}
}

func authRefreshUnavailable(sessionGroupID string, taskID string, clientMessageID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeUnavailable,
		Details: domain.GatewayErrorDetails{
			Reason:          domain.ReasonAuthRefreshUnavailable,
			DisplayMessage:  "auth refresh unavailable",
			TaskID:          taskID,
			SessionGroupID:  sessionGroupID,
			ClientMessageID: clientMessageID,
		},
	}
}

func invalidContext(sessionGroupID string, taskID string, clientMessageID string, err error) error {
	reason := domain.ReasonInvalidRequest
	var validationErr *contextpack.ValidationError
	if errors.As(err, &validationErr) {
		reason = validationErr.Reason
	}
	code := domain.GatewayErrorCodeInvalidArgument
	if reason == domain.ReasonRequestTooLarge {
		code = domain.GatewayErrorCodeResourceExhausted
	}
	return &domain.GatewayError{
		Code: code,
		Details: domain.GatewayErrorDetails{
			Reason:          reason,
			DisplayMessage:  "context envelope validation failed",
			TaskID:          taskID,
			SessionGroupID:  sessionGroupID,
			ClientMessageID: clientMessageID,
		},
	}
}

func protocolMismatch(sessionGroupID string, taskID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeUnavailable,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonDispatcherUnavailable,
			DisplayMessage: "app-server protocol mismatch",
			TaskID:         taskID,
			SessionGroupID: sessionGroupID,
			Retryable:      true,
		},
	}
}

func invalidCursor(sessionGroupID string, taskID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeInvalidArgument,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonInvalidCursor,
			DisplayMessage: "invalid stream cursor",
			TaskID:         taskID,
			SessionGroupID: sessionGroupID,
		},
	}
}

func responseTypeMismatch(sessionGroupID string, taskID string, pendingRequestID string, clientResponseID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeInvalidArgument,
		Details: domain.GatewayErrorDetails{
			Reason:           domain.ReasonResponseTypeMismatch,
			DisplayMessage:   "pending response type does not match request",
			TaskID:           taskID,
			SessionGroupID:   sessionGroupID,
			PendingRequestID: pendingRequestID,
			ClientResponseID: clientResponseID,
		},
	}
}

func pendingResponseFingerprintMismatch(sessionGroupID string, taskID string, pendingRequestID string, clientResponseID string) error {
	return pendingFailedPrecondition(sessionGroupID, taskID, pendingRequestID, clientResponseID, domain.ReasonPendingResponseFingerprintMismatch, "pending response fingerprint mismatch")
}

func pendingRequestAlreadyResolved(sessionGroupID string, taskID string, pendingRequestID string, clientResponseID string) error {
	return pendingFailedPrecondition(sessionGroupID, taskID, pendingRequestID, clientResponseID, domain.ReasonPendingRequestAlreadyResolved, "pending request is already resolved")
}

func invalidPendingResponse(sessionGroupID string, taskID string, pendingRequestID string, clientResponseID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeInvalidArgument,
		Details: domain.GatewayErrorDetails{
			Reason:           domain.ReasonInvalidRequest,
			DisplayMessage:   "pending response is invalid",
			TaskID:           taskID,
			SessionGroupID:   sessionGroupID,
			PendingRequestID: pendingRequestID,
			ClientResponseID: clientResponseID,
		},
	}
}

func dispatcherUnavailable(sessionGroupID string, taskID string, pendingRequestID string, clientResponseID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeUnavailable,
		Details: domain.GatewayErrorDetails{
			Reason:           domain.ReasonDispatcherUnavailable,
			DisplayMessage:   appServerCallFailedMessage,
			TaskID:           taskID,
			SessionGroupID:   sessionGroupID,
			PendingRequestID: pendingRequestID,
			ClientResponseID: clientResponseID,
			Retryable:        true,
		},
	}
}

func pendingFailedPrecondition(
	sessionGroupID string,
	taskID string,
	pendingRequestID string,
	clientResponseID string,
	reason domain.GatewayErrorReason,
	message string,
) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeFailedPrecondition,
		Details: domain.GatewayErrorDetails{
			Reason:           reason,
			DisplayMessage:   message,
			TaskID:           taskID,
			SessionGroupID:   sessionGroupID,
			PendingRequestID: pendingRequestID,
			ClientResponseID: clientResponseID,
		},
	}
}

func ambiguousLocator(sessionGroupID string, threadID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeFailedPrecondition,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonAmbiguousLocator,
			DisplayMessage: "task locator is ambiguous",
			SessionGroupID: sessionGroupID,
			ThreadID:       threadID,
		},
	}
}

func invalidLocator() error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeInvalidArgument,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonInvalidLocator,
			DisplayMessage: "task locator is invalid",
		},
	}
}

func invalidArgument(sessionGroupID string, clientMessageID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeInvalidArgument,
		Details: domain.GatewayErrorDetails{
			Reason:          domain.ReasonInvalidRequest,
			DisplayMessage:  "start task request is invalid",
			SessionGroupID:  sessionGroupID,
			ClientMessageID: clientMessageID,
		},
	}
}

func internalError(sessionGroupID string, taskID string, clientMessageID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeInternal,
		Details: domain.GatewayErrorDetails{
			Reason:          domain.ReasonInternalGatewayError,
			DisplayMessage:  "internal gateway error",
			TaskID:          taskID,
			SessionGroupID:  sessionGroupID,
			ClientMessageID: clientMessageID,
		},
	}
}
