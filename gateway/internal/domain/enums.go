package domain

type ContextBlockKind string

const (
	ContextBlockKindApplication ContextBlockKind = "application"
	ContextBlockKindUntrusted   ContextBlockKind = "untrusted"
)

type ChatThreadLifecycle string

const (
	ChatThreadLifecycleNotLoaded          ChatThreadLifecycle = "not_loaded"
	ChatThreadLifecycleIdle               ChatThreadLifecycle = "idle"
	ChatThreadLifecycleActiveRunning      ChatThreadLifecycle = "active_running"
	ChatThreadLifecycleWaitingOnApproval  ChatThreadLifecycle = "waiting_on_approval"
	ChatThreadLifecycleWaitingOnUserInput ChatThreadLifecycle = "waiting_on_user_input"
	ChatThreadLifecycleSystemError        ChatThreadLifecycle = "system_error"
	ChatThreadLifecycleUnknown            ChatThreadLifecycle = "unknown"
)

type ChatTurnLifecycle string

const (
	ChatTurnLifecycleInProgress  ChatTurnLifecycle = "in_progress"
	ChatTurnLifecycleCompleted   ChatTurnLifecycle = "completed"
	ChatTurnLifecycleInterrupted ChatTurnLifecycle = "interrupted"
	ChatTurnLifecycleFailed      ChatTurnLifecycle = "failed"
	ChatTurnLifecycleUnknown     ChatTurnLifecycle = "turn_unknown"
	ChatTurnLifecycleUnavailable ChatTurnLifecycle = "turn_unavailable"
)

type ChatCapabilityState string

const (
	ChatCapabilitySupported   ChatCapabilityState = "supported"
	ChatCapabilityUnsupported ChatCapabilityState = "unsupported"
	ChatCapabilityUnavailable ChatCapabilityState = "unavailable"
	ChatCapabilityUnknown     ChatCapabilityState = "unknown"
	ChatCapabilityNarrowed    ChatCapabilityState = "narrowed"
)

type ChatHistoryDepth string

const (
	ChatHistoryDepthTurnSummary ChatHistoryDepth = "turn_summary"
	ChatHistoryDepthItemLevel   ChatHistoryDepth = "item_level"
)

type ChatHistorySortDirection string

const (
	ChatHistorySortAscending  ChatHistorySortDirection = "ascending"
	ChatHistorySortDescending ChatHistorySortDirection = "descending"
)

type ChatTurnItemsView string

const (
	ChatTurnItemsViewNotLoaded       ChatTurnItemsView = "not_loaded"
	ChatTurnItemsViewSummary         ChatTurnItemsView = "summary"
	ChatTurnItemsViewFullUnsupported ChatTurnItemsView = "full_unsupported"
)

type TaskState string

const (
	TaskStateStarting                 TaskState = "starting"
	TaskStateRunning                  TaskState = "running"
	TaskStateWaitingForPendingRequest TaskState = "waiting_for_pending_request"
	TaskStateInterrupting             TaskState = "interrupting"
	TaskStateCompleted                TaskState = "completed"
	TaskStateFailed                   TaskState = "failed"
	TaskStateInterrupted              TaskState = "interrupted"
)

type TaskLifecycleEventType string

const (
	TaskLifecycleEventTaskStarted   TaskLifecycleEventType = "task_started"
	TaskLifecycleEventThreadStarted TaskLifecycleEventType = "thread_started"
	TaskLifecycleEventTurnStarted   TaskLifecycleEventType = "turn_started"
	TaskLifecycleEventStateChanged  TaskLifecycleEventType = "state_changed"
)

type ReplayNoticeCode string

const (
	ReplayNoticeStartEvicted  ReplayNoticeCode = "replay_start_evicted"
	ReplayNoticeCursorEvicted ReplayNoticeCode = "replay_cursor_evicted"
)

type ToolState string

const (
	ToolStateStarted   ToolState = "started"
	ToolStateRunning   ToolState = "running"
	ToolStateCompleted ToolState = "completed"
	ToolStateFailed    ToolState = "failed"
)

type CommandOutputStream string

const CommandOutputStreamCombined CommandOutputStream = "combined"

type PendingType string

const (
	PendingTypeCommandApproval     PendingType = "command_approval"
	PendingTypeFileChangeApproval  PendingType = "file_change_approval"
	PendingTypePermissionsApproval PendingType = "permissions_approval"
	PendingTypeMcpElicitation      PendingType = "mcp_elicitation"
	PendingTypeToolUserInput       PendingType = "tool_user_input"
)

type PendingResolution string

const (
	PendingResolutionAccepted  PendingResolution = "accepted"
	PendingResolutionDeclined  PendingResolution = "declined"
	PendingResolutionCancelled PendingResolution = "cancelled"
	PendingResolutionGranted   PendingResolution = "granted"
	PendingResolutionDenied    PendingResolution = "denied"
	PendingResolutionAnswered  PendingResolution = "answered"
	PendingResolutionExpired   PendingResolution = "expired"
	PendingResolutionCleared   PendingResolution = "cleared"
	PendingResolutionFailed    PendingResolution = "failed"
)

type TerminalState string

const (
	TerminalStateCompleted   TerminalState = "completed"
	TerminalStateFailed      TerminalState = "failed"
	TerminalStateInterrupted TerminalState = "interrupted"
)

type ApprovalWireDecision string

const (
	ApprovalWireDecisionAccept           ApprovalWireDecision = "accept"
	ApprovalWireDecisionAcceptForSession ApprovalWireDecision = "acceptForSession"
	ApprovalWireDecisionDecline          ApprovalWireDecision = "decline"
	ApprovalWireDecisionCancel           ApprovalWireDecision = "cancel"
)

func (d ApprovalWireDecision) AppServerWireValue() (string, bool) {
	switch d {
	case ApprovalWireDecisionAccept, ApprovalWireDecisionAcceptForSession, ApprovalWireDecisionDecline, ApprovalWireDecisionCancel:
		return string(d), true
	default:
		return "", false
	}
}

type PermissionScope string

const (
	PermissionScopeTurn    PermissionScope = "turn"
	PermissionScopeSession PermissionScope = "session"
)

type ElicitationMode string

const (
	ElicitationModeForm ElicitationMode = "form"
	ElicitationModeURL  ElicitationMode = "url"
)

type McpElicitationAction string

const (
	McpElicitationActionAccept  McpElicitationAction = "accept"
	McpElicitationActionDecline McpElicitationAction = "decline"
	McpElicitationActionCancel  McpElicitationAction = "cancel"
)
