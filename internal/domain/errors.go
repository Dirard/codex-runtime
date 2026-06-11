package domain

type GatewayErrorReason string

const (
	ReasonInvalidRequest                     GatewayErrorReason = "invalid_request"
	ReasonUnauthenticated                    GatewayErrorReason = "unauthenticated"
	ReasonInvalidLocator                     GatewayErrorReason = "invalid_locator"
	ReasonWorkspaceMismatch                  GatewayErrorReason = "workspace_mismatch"
	ReasonMutuallyExclusiveCursors           GatewayErrorReason = "mutually_exclusive_cursors"
	ReasonInvalidCursor                      GatewayErrorReason = "invalid_cursor"
	ReasonResponseTypeMismatch               GatewayErrorReason = "response_type_mismatch"
	ReasonPendingResponseFingerprintMismatch GatewayErrorReason = "pending_response_fingerprint_mismatch"
	ReasonPendingRequestAlreadyResolved      GatewayErrorReason = "pending_request_already_resolved"
	ReasonInvalidEnum                        GatewayErrorReason = "invalid_enum"
	ReasonRequestTooLarge                    GatewayErrorReason = "request_too_large"
	ReasonUnknownSessionGroup                GatewayErrorReason = "unknown_session_group"
	ReasonUnknownTask                        GatewayErrorReason = "unknown_task"
	ReasonUnknownPendingRequest              GatewayErrorReason = "unknown_pending_request"
	ReasonUnknownThreadBinding               GatewayErrorReason = "unknown_thread_binding"
	ReasonThreadBindingExpired               GatewayErrorReason = "thread_binding_expired"
	ReasonAmbiguousLocator                   GatewayErrorReason = "ambiguous_locator"
	ReasonAlreadyRunning                     GatewayErrorReason = "already_running"
	ReasonIdempotencyFingerprintMismatch     GatewayErrorReason = "idempotency_fingerprint_mismatch"
	ReasonCodexHomeMismatch                  GatewayErrorReason = "codex_home_mismatch"
	ReasonAuthRefreshUnavailable             GatewayErrorReason = "auth_refresh_unavailable"
	ReasonAppServerSchemaUnverified          GatewayErrorReason = "app_server_schema_unverified"
	ReasonUnsafeRuntimePolicy                GatewayErrorReason = "unsafe_runtime_policy"
	ReasonUnsafeListenConfig                 GatewayErrorReason = "unsafe_listen_config"
	ReasonStartInterruptedBeforeTurn         GatewayErrorReason = "start_interrupted_before_turn"
	ReasonCallerCanceled                     GatewayErrorReason = "caller_canceled"
	ReasonCallerDeadlineExceeded             GatewayErrorReason = "caller_deadline_exceeded"
	ReasonResourceExhausted                  GatewayErrorReason = "resource_exhausted"
	ReasonDispatcherUnavailable              GatewayErrorReason = "dispatcher_unavailable"
	ReasonAppServerExited                    GatewayErrorReason = "app_server_exited"
	ReasonAppServerRestartBackoff            GatewayErrorReason = "app_server_restart_backoff"
	ReasonInternalGatewayError               GatewayErrorReason = "internal_gateway_error"
)

var CanonicalGatewayErrorReasons = []GatewayErrorReason{
	ReasonInvalidRequest,
	ReasonUnauthenticated,
	ReasonInvalidLocator,
	ReasonWorkspaceMismatch,
	ReasonMutuallyExclusiveCursors,
	ReasonInvalidCursor,
	ReasonResponseTypeMismatch,
	ReasonPendingResponseFingerprintMismatch,
	ReasonPendingRequestAlreadyResolved,
	ReasonInvalidEnum,
	ReasonRequestTooLarge,
	ReasonUnknownSessionGroup,
	ReasonUnknownTask,
	ReasonUnknownPendingRequest,
	ReasonUnknownThreadBinding,
	ReasonThreadBindingExpired,
	ReasonAmbiguousLocator,
	ReasonAlreadyRunning,
	ReasonIdempotencyFingerprintMismatch,
	ReasonCodexHomeMismatch,
	ReasonAuthRefreshUnavailable,
	ReasonAppServerSchemaUnverified,
	ReasonUnsafeRuntimePolicy,
	ReasonUnsafeListenConfig,
	ReasonStartInterruptedBeforeTurn,
	ReasonCallerCanceled,
	ReasonCallerDeadlineExceeded,
	ReasonResourceExhausted,
	ReasonDispatcherUnavailable,
	ReasonAppServerExited,
	ReasonAppServerRestartBackoff,
	ReasonInternalGatewayError,
}

type GatewayErrorDetails struct {
	Reason           GatewayErrorReason
	DisplayMessage   string
	TaskID           string
	SessionGroupID   string
	ClientMessageID  string
	ClientResponseID string
	PendingRequestID string
	ThreadID         string
	Retryable        bool
}

type GatewayErrorCode string

const (
	GatewayErrorCodeInvalidArgument    GatewayErrorCode = "invalid_argument"
	GatewayErrorCodeNotFound           GatewayErrorCode = "not_found"
	GatewayErrorCodeFailedPrecondition GatewayErrorCode = "failed_precondition"
	GatewayErrorCodeCanceled           GatewayErrorCode = "canceled"
	GatewayErrorCodeDeadlineExceeded   GatewayErrorCode = "deadline_exceeded"
	GatewayErrorCodeResourceExhausted  GatewayErrorCode = "resource_exhausted"
	GatewayErrorCodeUnavailable        GatewayErrorCode = "unavailable"
	GatewayErrorCodeInternal           GatewayErrorCode = "internal"
)

type GatewayError struct {
	Code    GatewayErrorCode
	Details GatewayErrorDetails
}

func (e *GatewayError) Error() string {
	if e == nil {
		return ""
	}
	if e.Details.DisplayMessage != "" {
		return e.Details.DisplayMessage
	}
	if e.Details.Reason != "" {
		return string(e.Details.Reason)
	}
	return string(e.Code)
}
