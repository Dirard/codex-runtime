package chatstate

import (
	"time"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

const (
	DefaultReplayMaxEvents = 2000
	DefaultReplayMaxBytes  = 8 * int64(domain.MiB)
	DefaultReplayTTL       = 30 * time.Minute
	DefaultPendingTTL      = 30 * time.Minute
	DefaultDiagnosticsCap  = 256
	DefaultActiveRunsCap   = 1000
	DefaultPendingCap      = 1000
	DefaultIdempotencyCap  = 1000
	DefaultSubscriberQueue = 256
)

type Limits struct {
	ReplayMaxEvents int
	ReplayMaxBytes  int64
	ReplayTTL       time.Duration
	PendingTTL      time.Duration
	DiagnosticsCap  int
	ActiveRunsCap   int
	PendingCap      int
	IdempotencyCap  int
	SubscriberQueue int
}

type StoreOptions struct {
	Epoch  string
	Now    func() time.Time
	Limits Limits
}

type Scope struct {
	SessionGroupID string
	WorkspaceID    string
	ChatID         string
}

type RunScope struct {
	Scope
	RunID string
}

type RunState string

const (
	RunStateStarting     RunState = "starting"
	RunStateRunning      RunState = "running"
	RunStatePending      RunState = "pending"
	RunStateInterrupting RunState = "interrupting"
	RunStateCompleted    RunState = "completed"
	RunStateFailed       RunState = "failed"
	RunStateInterrupted  RunState = "interrupted"
)

type RunRecord struct {
	Scope
	RunID           string
	State           RunState
	IdempotencyKey  string
	StartedAtUnixMS int64
	UpdatedAtUnixMS int64
}

type EventInput struct {
	RunScope
	Kind                      string
	State                     string
	Reason                    domain.GatewayErrorReason
	AssistantDelta            *domain.AssistantDeltaEvent
	AssistantMessageCompleted *domain.AssistantMessageCompletedEvent
	PendingCreated            *domain.ChatPendingRequest
	PendingResolved           *domain.ChatPendingResolved
	Terminal                  *domain.ChatTerminal
	CommandStarted            *domain.CommandStartedEvent
	CommandOutputDelta        *domain.CommandOutputDeltaEvent
	GatewayWarning            *domain.GatewayWarningEvent
	SizeBytes                 int64
}

type EventRecord struct {
	EventID uint64
	Epoch   string
	RunScope
	Kind                      string
	State                     string
	Reason                    domain.GatewayErrorReason
	AssistantDelta            *domain.AssistantDeltaEvent
	AssistantMessageCompleted *domain.AssistantMessageCompletedEvent
	PendingCreated            *domain.ChatPendingRequest
	PendingResolved           *domain.ChatPendingResolved
	Terminal                  *domain.ChatTerminal
	CommandStarted            *domain.CommandStartedEvent
	CommandOutputDelta        *domain.CommandOutputDeltaEvent
	GatewayWarning            *domain.GatewayWarningEvent
	CreatedAtUnixMS           int64
	SizeBytes                 int64
}

type Cursor struct {
	Epoch          string
	SessionGroupID string
	WorkspaceID    string
	ChatID         string
	RunID          string
	AfterEventID   uint64
}

type ReplayResult struct {
	Events                    []EventRecord
	OldestBufferedEventID     uint64
	NewestBufferedEventID     uint64
	FromStartAvailable        bool
	StartEvictedBeforeEventID uint64
}

type ReplayFailureKind string

const (
	ReplayFailureUnavailableAfterRestart ReplayFailureKind = "unavailable_after_restart"
	ReplayFailureCursorEvicted           ReplayFailureKind = "cursor_evicted"
	ReplayFailureOutOfRange              ReplayFailureKind = "out_of_range"
)

type ReplayError struct {
	Kind    ReplayFailureKind
	Cursor  Cursor
	Message string
}

func (e *ReplayError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return string(e.Kind)
}

type PendingStatus string

const (
	PendingStatusActive   PendingStatus = "active"
	PendingStatusClaimed  PendingStatus = "claimed"
	PendingStatusResolved PendingStatus = "resolved"
	PendingStatusExpired  PendingStatus = "expired"
)

type PendingInput struct {
	RunScope
	PendingRequestID string
	TTL              time.Duration
	Reason           domain.GatewayErrorReason
}

type PendingRecord struct {
	RunScope
	PendingRequestID string
	Status           PendingStatus
	Reason           domain.GatewayErrorReason
	CreatedAtUnixMS  int64
	ExpiresAtUnixMS  int64
	ResolvedAtUnixMS int64
}

type Operation string

const (
	OperationStartChatRun     Operation = "start_chat_run"
	OperationRunChatTurn      Operation = "run_chat_turn"
	OperationRespondPending   Operation = "respond_chat_pending"
	OperationInterruptChatRun Operation = "interrupt_chat_run"
)

type IdempotencyScope struct {
	Operation        Operation
	SessionGroupID   string
	WorkspaceID      string
	ChatID           string
	PendingRequestID string
	ClientMessageID  string
	Key              string
}

type IdempotencyState string

const (
	IdempotencyStateInFlight  IdempotencyState = "in_flight"
	IdempotencyStateSucceeded IdempotencyState = "succeeded"
	IdempotencyStateFailed    IdempotencyState = "failed"
)

type IdempotencyResultRef struct {
	ChatID      string
	RunID       string
	Status      string
	LastEventID uint64
	EventCursor string
}

type IdempotencyEntry struct {
	Scope           IdempotencyScope
	State           IdempotencyState
	Result          IdempotencyResultRef
	CreatedAtUnixMS int64
	UpdatedAtUnixMS int64
}

type DiagnosticRecord struct {
	CreatedAtUnixMS int64
	SessionGroupID  string
	WorkspaceID     string
	ChatID          string
	RunID           string
	RequestID       string
	State           string
	Reason          domain.GatewayErrorReason
}
