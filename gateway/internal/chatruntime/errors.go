package chatruntime

import (
	"context"
	"errors"
	"strings"

	"github.com/Dirard/codex-runtime/gateway/internal/appserver"
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

func invalidRequest(sessionGroupID string, clientMessageID string, message string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeInvalidArgument,
		Details: domain.GatewayErrorDetails{
			Reason:          domain.ReasonInvalidRequest,
			DisplayMessage:  message,
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

func unknownChat(sessionGroupID string, chatID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeNotFound,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonUnknownChat,
			DisplayMessage: "unknown chat",
			SessionGroupID: sessionGroupID,
			ThreadID:       chatID,
		},
	}
}

func cursorOutOfRange(sessionGroupID string, chatID string, message string) error {
	if message == "" {
		message = "cursor is outside this chat"
	}
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeOutOfRange,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonReplayOutOfRange,
			DisplayMessage: message,
			SessionGroupID: sessionGroupID,
			ThreadID:       chatID,
		},
	}
}

func invalidCursor(sessionGroupID string, chatID string, message string) error {
	if message == "" {
		message = "cursor is invalid"
	}
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeInvalidArgument,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonInvalidCursor,
			DisplayMessage: message,
			SessionGroupID: sessionGroupID,
			ThreadID:       chatID,
		},
	}
}

func historyUnavailable(sessionGroupID string, chatID string, message string) error {
	if message == "" {
		message = "chat history is unavailable"
	}
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeUnavailable,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonHistoryUnavailable,
			DisplayMessage: message,
			SessionGroupID: sessionGroupID,
			ThreadID:       chatID,
			Retryable:      true,
		},
	}
}

func chatStateUnavailable(sessionGroupID string, chatID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeUnavailable,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonChatStateUnavailable,
			DisplayMessage: "chat state is unavailable from Codex",
			SessionGroupID: sessionGroupID,
			ThreadID:       chatID,
			Retryable:      true,
		},
	}
}

func invalidContext(sessionGroupID string, clientMessageID string, err error) error {
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
			SessionGroupID:  sessionGroupID,
			ClientMessageID: clientMessageID,
		},
	}
}

func callerErrorFromContext(ctx context.Context, sessionGroupID string, clientMessageID string) error {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.Canceled) {
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
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
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
	return nil
}

func idempotencyResultUnavailable(sessionGroupID string, clientMessageID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeUnknown,
		Details: domain.GatewayErrorDetails{
			Reason:          domain.ReasonIdempotencyResultUnavailable,
			DisplayMessage:  "idempotent result is not available in this gateway process",
			SessionGroupID:  sessionGroupID,
			ClientMessageID: clientMessageID,
			Retryable:       true,
		},
	}
}

func protocolMismatch(sessionGroupID string, chatID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeUnavailable,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonDispatcherUnavailable,
			DisplayMessage: "app-server protocol mismatch",
			SessionGroupID: sessionGroupID,
			ThreadID:       chatID,
			Retryable:      true,
		},
	}
}

func appServerCallError(err error, sessionGroupID string, clientMessageID string, chatID string) error {
	if err == nil {
		return nil
	}
	var gatewayErr *domain.GatewayError
	if errors.As(err, &gatewayErr) {
		return gatewayErr
	}
	if errors.Is(err, context.Canceled) {
		return &domain.GatewayError{
			Code: domain.GatewayErrorCodeCanceled,
			Details: domain.GatewayErrorDetails{
				Reason:          domain.ReasonCallerCanceled,
				DisplayMessage:  "caller canceled",
				SessionGroupID:  sessionGroupID,
				ClientMessageID: clientMessageID,
				ThreadID:        chatID,
			},
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &domain.GatewayError{
			Code: domain.GatewayErrorCodeDeadlineExceeded,
			Details: domain.GatewayErrorDetails{
				Reason:          domain.ReasonCallerDeadlineExceeded,
				DisplayMessage:  "caller deadline exceeded",
				SessionGroupID:  sessionGroupID,
				ClientMessageID: clientMessageID,
				ThreadID:        chatID,
			},
		}
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "thread not loaded") || strings.Contains(message, "no rollout found for thread id") {
		return unknownChat(sessionGroupID, chatID)
	}
	if strings.Contains(message, "invalid thread id") {
		return &domain.GatewayError{
			Code: domain.GatewayErrorCodeInvalidArgument,
			Details: domain.GatewayErrorDetails{
				Reason:          domain.ReasonInvalidLocator,
				DisplayMessage:  "chat_id is invalid",
				SessionGroupID:  sessionGroupID,
				ClientMessageID: clientMessageID,
				ThreadID:        chatID,
			},
		}
	}
	if strings.Contains(message, "ephemeral threads do not support thread/turns/list") ||
		strings.Contains(message, "thread/turns/list is unavailable before first user message") ||
		strings.Contains(message, "not materialized yet") {
		return historyUnavailable(sessionGroupID, chatID, "chat history is unavailable from Codex for this thread")
	}
	reason := domain.ReasonDispatcherUnavailable
	if errors.Is(err, appserver.ErrDispatcherClosed) {
		reason = domain.ReasonDispatcherUnavailable
	}
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeUnavailable,
		Details: domain.GatewayErrorDetails{
			Reason:          reason,
			DisplayMessage:  "app-server call failed",
			SessionGroupID:  sessionGroupID,
			ClientMessageID: clientMessageID,
			ThreadID:        chatID,
			Retryable:       true,
		},
	}
}

func retryableFreshConnectionStartError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, appserver.ErrDispatcherClosed) {
		return true
	}
	var gatewayErr *domain.GatewayError
	if errors.As(err, &gatewayErr) {
		return gatewayErr.Details.Retryable && gatewayErr.Details.Reason == domain.ReasonDispatcherUnavailable
	}
	return false
}
