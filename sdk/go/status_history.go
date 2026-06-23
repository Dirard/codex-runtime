package codex

import (
	"context"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
)

// ThreadLifecycle is the friendly chat/thread lifecycle.
type ThreadLifecycle string

const (
	ThreadLifecycleUnknown           ThreadLifecycle = ""
	ThreadLifecycleNotLoaded         ThreadLifecycle = "not_loaded"
	ThreadLifecycleIdle              ThreadLifecycle = "idle"
	ThreadLifecycleActiveRunning     ThreadLifecycle = "active_running"
	ThreadLifecycleWaitingOnApproval ThreadLifecycle = "waiting_on_approval"
	ThreadLifecycleWaitingOnInput    ThreadLifecycle = "waiting_on_user_input"
	ThreadLifecycleSystemError       ThreadLifecycle = "system_error"
)

// RunLifecycle is the friendly run/turn lifecycle.
type RunLifecycle string

const (
	RunLifecycleUnknown     RunLifecycle = ""
	RunLifecycleInProgress  RunLifecycle = "in_progress"
	RunLifecycleCompleted   RunLifecycle = "completed"
	RunLifecycleInterrupted RunLifecycle = "interrupted"
	RunLifecycleFailed      RunLifecycle = "failed"
	RunLifecycleUnavailable RunLifecycle = "unavailable"
)

// CapabilityState is the friendly gateway capability state.
type CapabilityState string

const (
	CapabilityStateUnknown     CapabilityState = ""
	CapabilityStateSupported   CapabilityState = "supported"
	CapabilityStateUnsupported CapabilityState = "unsupported"
	CapabilityStateUnavailable CapabilityState = "unavailable"
	CapabilityStateNarrowed    CapabilityState = "narrowed"
)

// StatusSnapshot is the friendly, protobuf-free chat status shape used by
// common-path SDK code.
type StatusSnapshot struct {
	_ noUnkeyedLiterals

	ChatID          string
	RunID           string
	SessionGroupID  string
	WorkspaceID     string
	ThreadLifecycle ThreadLifecycle
	RunLifecycle    RunLifecycle
	LastEventID     uint64
	Pending         []PendingSummary
	Capabilities    CapabilitySnapshot
}

// PendingSummary identifies an active pending action in a status snapshot.
type PendingSummary struct {
	_ noUnkeyedLiterals

	PendingID string
	Kind      PendingKind
}

// CapabilitySnapshot reports gateway capabilities for the current chat.
type CapabilitySnapshot struct {
	_ noUnkeyedLiterals

	Status      CapabilityState
	History     CapabilityState
	EventStream CapabilityState
	Replay      CapabilityState
	Pending     CapabilityState
	Interrupt   CapabilityState
}

// CachedStatusSnapshot returns the last status observed by this Chat when one is
// available locally.
func (chat *Chat) CachedStatusSnapshot() (StatusSnapshot, bool) {
	status := chat.CachedStatus()
	if status == nil {
		return StatusSnapshot{}, false
	}
	return statusSnapshotFromProto(status), true
}

// GetStatusSnapshot fetches the current chat status as a friendly value.
func (chat *Chat) GetStatusSnapshot(ctx context.Context) (StatusSnapshot, error) {
	status, err := chat.GetStatus(ctx)
	if err != nil {
		return StatusSnapshot{}, err
	}
	return statusSnapshotFromProto(status), nil
}

func statusSnapshotFromProto(status *pb.ChatStatus) StatusSnapshot {
	if status == nil {
		return StatusSnapshot{}
	}
	pending := make([]PendingSummary, 0, len(status.GetActivePendingRequests()))
	for _, item := range status.GetActivePendingRequests() {
		pending = append(pending, PendingSummary{
			PendingID: item.GetPendingRequestId(),
			Kind:      pendingKindFromProto(item.GetPendingType()),
		})
	}
	return StatusSnapshot{
		ChatID:          status.GetChatId(),
		RunID:           status.GetCurrentRunId(),
		SessionGroupID:  status.GetSessionGroupId(),
		WorkspaceID:     status.GetWorkspaceId(),
		ThreadLifecycle: threadLifecycleFromProto(status.GetThreadLifecycle()),
		RunLifecycle:    runLifecycleFromProto(status.GetCurrentRunLifecycle()),
		LastEventID:     status.GetLastEventId(),
		Pending:         pending,
		Capabilities:    capabilitySnapshotFromProto(status.GetCapabilities()),
	}
}

func capabilitySnapshotFromProto(capabilities *pb.ChatCapabilitySet) CapabilitySnapshot {
	if capabilities == nil {
		return CapabilitySnapshot{}
	}
	return CapabilitySnapshot{
		Status:      capabilityStateFromProto(capabilities.GetStatus()),
		History:     capabilityStateFromProto(capabilities.GetHistory()),
		EventStream: capabilityStateFromProto(capabilities.GetEventStream()),
		Replay:      capabilityStateFromProto(capabilities.GetReplay()),
		Pending:     capabilityStateFromProto(capabilities.GetPending()),
		Interrupt:   capabilityStateFromProto(capabilities.GetInterrupt()),
	}
}

// HistoryDepth selects the friendly history detail level.
type HistoryDepth string

const (
	HistoryDepthUnspecified HistoryDepth = ""
	HistoryDepthTurnSummary HistoryDepth = "turn_summary"
	HistoryDepthItemLevel   HistoryDepth = "item_level"
)

// HistorySortDirection selects friendly history page ordering.
type HistorySortDirection string

const (
	HistorySortUnspecified HistorySortDirection = ""
	HistorySortAscending   HistorySortDirection = "ascending"
	HistorySortDescending  HistorySortDirection = "descending"
)

// HistoryPage is the friendly chat history page returned by GetHistoryPage.
type HistoryPage struct {
	_ noUnkeyedLiterals

	ChatID          string
	Turns           []HistoryTurn
	NextCursor      string
	BackwardsCursor string
	ReturnedDepth   HistoryDepth
}

// HistoryTurn summarizes one run/turn in a friendly history page.
type HistoryTurn struct {
	_ noUnkeyedLiterals

	RunID     string
	Lifecycle RunLifecycle
	Summary   string
	Truncated bool
}

// HistoryPageOption configures GetHistoryPage without exposing protobuf enums.
type HistoryPageOption func(*historyPageOptions)

type historyPageOptions struct {
	depth  pb.ChatHistoryDepth
	cursor string
	limit  uint32
	sort   pb.ChatHistorySortDirection
}

// WithHistoryPageDepth requests summary or item-level history.
func WithHistoryPageDepth(depth HistoryDepth) HistoryPageOption {
	return func(opts *historyPageOptions) {
		opts.depth = historyDepthToProto(depth)
	}
}

// WithHistoryPageCursor requests a page after a gateway history cursor.
func WithHistoryPageCursor(cursor string) HistoryPageOption {
	return func(opts *historyPageOptions) {
		opts.cursor = cursor
	}
}

// WithHistoryPageLimit limits the number of history turns returned.
func WithHistoryPageLimit(limit uint32) HistoryPageOption {
	return func(opts *historyPageOptions) {
		opts.limit = limit
	}
}

// WithHistoryPageSort chooses ascending or descending history order.
func WithHistoryPageSort(direction HistorySortDirection) HistoryPageOption {
	return func(opts *historyPageOptions) {
		opts.sort = historySortToProto(direction)
	}
}

// GetHistoryPage fetches chat history through friendly SDK options and values.
func (chat *Chat) GetHistoryPage(ctx context.Context, opts ...HistoryPageOption) (HistoryPage, error) {
	applied := historyPageOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&applied)
		}
	}
	raw, err := chat.GetHistory(
		ctx,
		WithHistoryDepth(applied.depth),
		WithHistoryCursor(applied.cursor),
		WithHistoryLimit(applied.limit),
		WithHistorySortDirection(applied.sort),
	)
	if err != nil {
		return HistoryPage{}, err
	}
	return historyPageFromProto(raw), nil
}

func historyPageFromProto(raw *pb.GetChatHistoryResponse) HistoryPage {
	if raw == nil {
		return HistoryPage{}
	}
	turns := make([]HistoryTurn, 0, len(raw.GetTurns()))
	for _, turn := range raw.GetTurns() {
		turns = append(turns, HistoryTurn{
			RunID:     turn.GetRunId(),
			Lifecycle: runLifecycleFromProto(turn.GetLifecycle()),
			Summary:   turn.GetSummary(),
			Truncated: turn.GetTruncated(),
		})
	}
	return HistoryPage{
		ChatID:          raw.GetChatId(),
		Turns:           turns,
		NextCursor:      raw.GetNextCursor(),
		BackwardsCursor: raw.GetBackwardsCursor(),
		ReturnedDepth:   historyDepthFromProto(raw.GetReturnedDepth()),
	}
}

func historyDepthToProto(depth HistoryDepth) pb.ChatHistoryDepth {
	switch depth {
	case HistoryDepthTurnSummary:
		return pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_TURN_SUMMARY
	case HistoryDepthItemLevel:
		return pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_ITEM_LEVEL
	default:
		return pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_UNSPECIFIED
	}
}

func historyDepthFromProto(depth pb.ChatHistoryDepth) HistoryDepth {
	switch depth {
	case pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_TURN_SUMMARY:
		return HistoryDepthTurnSummary
	case pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_ITEM_LEVEL:
		return HistoryDepthItemLevel
	default:
		return HistoryDepthUnspecified
	}
}

func historySortToProto(direction HistorySortDirection) pb.ChatHistorySortDirection {
	switch direction {
	case HistorySortAscending:
		return pb.ChatHistorySortDirection_CHAT_HISTORY_SORT_DIRECTION_ASCENDING
	case HistorySortDescending:
		return pb.ChatHistorySortDirection_CHAT_HISTORY_SORT_DIRECTION_DESCENDING
	default:
		return pb.ChatHistorySortDirection_CHAT_HISTORY_SORT_DIRECTION_UNSPECIFIED
	}
}

func threadLifecycleFromProto(lifecycle pb.ChatThreadLifecycle) ThreadLifecycle {
	switch lifecycle {
	case pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_NOT_LOADED:
		return ThreadLifecycleNotLoaded
	case pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_IDLE:
		return ThreadLifecycleIdle
	case pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_ACTIVE_RUNNING:
		return ThreadLifecycleActiveRunning
	case pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_WAITING_ON_APPROVAL:
		return ThreadLifecycleWaitingOnApproval
	case pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_WAITING_ON_USER_INPUT:
		return ThreadLifecycleWaitingOnInput
	case pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_SYSTEM_ERROR:
		return ThreadLifecycleSystemError
	default:
		return ThreadLifecycleUnknown
	}
}

func runLifecycleFromProto(lifecycle pb.ChatTurnLifecycle) RunLifecycle {
	switch lifecycle {
	case pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_IN_PROGRESS:
		return RunLifecycleInProgress
	case pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_COMPLETED:
		return RunLifecycleCompleted
	case pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_INTERRUPTED:
		return RunLifecycleInterrupted
	case pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_FAILED:
		return RunLifecycleFailed
	case pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_TURN_UNAVAILABLE:
		return RunLifecycleUnavailable
	default:
		return RunLifecycleUnknown
	}
}

func capabilityStateFromProto(state pb.ChatCapabilityState) CapabilityState {
	switch state {
	case pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED:
		return CapabilityStateSupported
	case pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_UNSUPPORTED:
		return CapabilityStateUnsupported
	case pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_UNAVAILABLE:
		return CapabilityStateUnavailable
	case pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_NARROWED:
		return CapabilityStateNarrowed
	default:
		return CapabilityStateUnknown
	}
}
