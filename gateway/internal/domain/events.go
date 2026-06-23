package domain

type StartTaskResponse struct {
	TaskID         string
	ThreadID       string
	TurnID         string
	SessionGroupID string
	State          TaskState
	LastEventID    uint64
}

type Chat struct {
	ChatID          string
	SessionGroupID  string
	WorkspaceID     string
	ThreadLifecycle ChatThreadLifecycle
	CreatedAtUnixMS int64
	UpdatedAtUnixMS int64
	Preview         string
	Ephemeral       bool
	Capabilities    ChatCapabilitySet
}

type ChatStatus struct {
	ChatID              string
	SessionGroupID      string
	WorkspaceID         string
	LookupValid         bool
	ThreadLifecycle     ChatThreadLifecycle
	CurrentRunLifecycle ChatTurnLifecycle
	CurrentRunID        string
	LastRunID           string
	Capabilities        ChatCapabilitySet
	GatewayLocal        ChatGatewayLocalState
	ActivePending       []ChatPendingRequest
	Terminal            *ChatTerminal
	LastEventID         uint64
}

type ChatCapabilitySet struct {
	Status      ChatCapabilityState
	History     ChatCapabilityState
	EventStream ChatCapabilityState
	Replay      ChatCapabilityState
	Pending     ChatCapabilityState
	Interrupt   ChatCapabilityState
}

type ChatGatewayLocalState struct {
	Live              bool
	ReplayAvailable   bool
	ReplayUnavailable bool
	ProcessEpoch      string
}

type ChatTurnSummary struct {
	RunID             string
	Lifecycle         ChatTurnLifecycle
	ItemsView         ChatTurnItemsView
	StartedAtUnixMS   int64
	CompletedAtUnixMS int64
	DurationMS        int64
	Summary           string
	Truncated         bool
	Error             *ChatErrorSummary
}

type ChatErrorSummary struct {
	Code           string
	DisplayMessage string
	Retryable      bool
}

type ChatNarrowedOutcome struct {
	Reason         GatewayErrorReason
	DisplayMessage string
	Retryable      bool
}

type ChatPendingRequest struct {
	PendingRequestID string
	ChatID           string
	RunID            string
	PendingType      PendingType
	CreatedAtUnixMS  int64
	ItemID           string
	Display          PendingRequestDisplay
}

type ChatTerminal struct {
	State          ChatTurnLifecycle
	Reason         GatewayErrorReason
	DisplayMessage string
}

type ChatEvent struct {
	EventID                   uint64
	EventCursor               string
	ChatID                    string
	SessionGroupID            string
	WorkspaceID               string
	RunID                     string
	CreatedAtUnixMS           int64
	StatusUpdated             *ChatStatus
	AssistantDelta            *AssistantDeltaEvent
	AssistantMessageCompleted *AssistantMessageCompletedEvent
	PendingCreated            *ChatPendingRequest
	PendingResolved           *ChatPendingResolved
	Terminal                  *ChatTerminal
	CommandStarted            *CommandStartedEvent
	CommandOutputDelta        *CommandOutputDeltaEvent
	GatewayWarning            *GatewayWarningEvent
}

type ChatPendingResolved struct {
	PendingRequestID string
	PendingType      PendingType
	Resolution       PendingResolution
	DisplayMessage   string
}

type ChatReplayNotice struct {
	Code                      ChatReplayNoticeCode
	Message                   string
	OldestBufferedEventID     uint64
	NewestBufferedEventID     uint64
	FromStartAvailable        bool
	StartEvictedBeforeEventID uint64
	ProcessEpoch              string
}

type ChatReplayNoticeCode string

const (
	ChatReplayNoticeStartEvicted            ChatReplayNoticeCode = "start_evicted"
	ChatReplayNoticeCursorEvicted           ChatReplayNoticeCode = "cursor_evicted"
	ChatReplayNoticeUnavailableAfterRestart ChatReplayNoticeCode = "unavailable_after_restart"
	ChatReplayNoticeNarrowedToLive          ChatReplayNoticeCode = "narrowed_to_live"
)

type ReplayNotice struct {
	Code                      ReplayNoticeCode
	Message                   string
	OldestBufferedEventID     uint64
	NewestBufferedEventID     uint64
	FromStartAvailable        bool
	StartEvictedBeforeEventID uint64
}

type TaskEvent struct {
	EventID         uint64
	TaskID          string
	SessionGroupID  string
	WorkspaceID     string
	ThreadID        string
	TurnID          string
	CreatedAtUnixMS int64
	Payload         TaskEventPayload
}

type TaskEventPayload interface {
	taskEventPayload()
}

type TaskLifecycleEvent struct {
	LifecycleEvent TaskLifecycleEventType
	State          TaskState
	ReasonCode     string
	DisplayMessage string
}

func (TaskLifecycleEvent) taskEventPayload() {}

type AssistantDeltaEvent struct {
	TextDelta string
	Truncated bool
}

func (AssistantDeltaEvent) taskEventPayload() {}

type AssistantMessageCompletedEvent struct {
	Message   string
	Truncated bool
}

func (AssistantMessageCompletedEvent) taskEventPayload() {}

type PlanUpdatedEvent struct {
	Explanation string
	Steps       []PlanStep
}

func (PlanUpdatedEvent) taskEventPayload() {}

type PlanStep struct {
	Step   string
	Status string
}

type ToolProgressEvent struct {
	ItemID   string
	ToolName string
	State    ToolState
	Summary  string
}

func (ToolProgressEvent) taskEventPayload() {}

type CommandStartedEvent struct {
	ItemID         string
	CommandDisplay string
	WorkspaceLabel string
}

func (CommandStartedEvent) taskEventPayload() {}

type CommandOutputDeltaEvent struct {
	ItemID    string
	Stream    CommandOutputStream
	Delta     string
	Truncated bool
}

func (CommandOutputDeltaEvent) taskEventPayload() {}

type FileDiffUpdatedEvent struct {
	ItemID      string
	FileLabel   string
	ChangeKind  string
	DiffSummary string
	DiffUnified string
	Truncated   bool
}

func (FileDiffUpdatedEvent) taskEventPayload() {}

type TurnDiffUpdatedEvent struct {
	DiffSummary string
	DiffUnified string
	Truncated   bool
}

func (TurnDiffUpdatedEvent) taskEventPayload() {}

type PendingRequestCreatedEvent struct {
	PendingRequestID string
	PendingType      PendingType
	Display          PendingRequestDisplay
}

func (PendingRequestCreatedEvent) taskEventPayload() {}

type PendingRequestResolvedEvent struct {
	PendingRequestID string
	PendingType      PendingType
	Resolution       PendingResolution
	DisplayMessage   string
}

func (PendingRequestResolvedEvent) taskEventPayload() {}

type TaskTerminalEvent struct {
	TerminalState TerminalState
	ResultSummary string
	ErrorMessage  string
}

func (TaskTerminalEvent) taskEventPayload() {}

type GatewayWarningEvent struct {
	Code           string
	Message        string
	RequestType    string
	AutoResolution string
	LimitReason    string
}

func (GatewayWarningEvent) taskEventPayload() {}

type PendingRequest struct {
	PendingRequestID string
	TaskID           string
	PendingType      PendingType
	CreatedAtUnixMS  int64
	ThreadID         string
	TurnID           string
	ItemID           string
	Display          PendingRequestDisplay
}

type PendingRequestDisplay interface {
	pendingRequestDisplay()
}

type CommandApprovalDisplay struct {
	CommandDisplay   string
	WorkspaceLabel   string
	Reason           string
	ApprovalSecurity *ApprovalSecurityMetadata
	DecisionOptions  []ApprovalDecisionOption
}

func (CommandApprovalDisplay) pendingRequestDisplay() {}

type FileChangeApprovalDisplay struct {
	FileLabel       string
	ChangeKind      string
	DiffSummary     string
	DiffUnified     string
	DiffUnavailable bool
	GrantRoot       *FileGrantRootDisplay
	DecisionOptions []ApprovalDecisionOption
}

func (FileChangeApprovalDisplay) pendingRequestDisplay() {}

type PermissionsApprovalDisplay struct {
	RequestedPermissions []PermissionAtom
	RecommendedScope     PermissionScope
	Reason               string
}

func (PermissionsApprovalDisplay) pendingRequestDisplay() {}

type McpElicitationDisplay struct {
	Mode           ElicitationMode
	Message        string
	FormSchemaJSON string
	URL            string
	SubmitLabel    string
}

func (McpElicitationDisplay) pendingRequestDisplay() {}

type ToolUserInputDisplay struct {
	Questions []ToolUserInputQuestion
}

func (ToolUserInputDisplay) pendingRequestDisplay() {}

type PermissionAtom struct {
	PermissionID      string
	Kind              string
	DisplayLabel      string
	ScopeLabel        string
	Grantable         bool
	UngrantableReason string
}

type ToolUserInputQuestion struct {
	ID       string
	Header   string
	Question string
	IsOther  bool
	IsSecret bool
	Options  []string
}

type ApprovalSecurityMetadata struct {
	HasPrivilegeExpansion           bool
	NetworkContext                  *NetworkContextDisplay
	AdditionalNetwork               *AdditionalNetworkDisplay
	AdditionalFilesystemEntries     []AdditionalFilesystemEntry
	ExecPolicyAmendmentSummary      *ExecPolicyAmendmentSummary
	NetworkPolicyAmendmentSummaries []NetworkPolicyAmendmentSummary
	BlockingReason                  string
}

type NetworkContextDisplay struct {
	HostLabel string
	Protocol  string
}

type AdditionalNetworkDisplay struct {
	Enabled bool
}

type AdditionalFilesystemEntry struct {
	EntryID            string
	Access             string
	PathLabel          string
	Approvable         bool
	UnapprovableReason string
}

type ExecPolicyAmendmentSummary struct {
	CommandDisplay string
	Truncated      bool
}

type NetworkPolicyAmendmentSummary struct {
	HostLabel  string
	Action     string
	Approvable bool
}

type FileGrantRootDisplay struct {
	Present            bool
	RootLabel          string
	UnderConfiguredCWD bool
	Approvable         bool
	UnapprovableReason string
}

type ApprovalDecisionOption struct {
	DecisionID        string
	WireDecision      ApprovalWireDecision
	DisplayLabel      string
	Summary           string
	Selectable        bool
	UnsupportedReason string
}

type GetTaskStatusResponse struct {
	TaskID                    string
	State                     TaskState
	SessionGroupID            string
	WorkspaceID               string
	ThreadID                  string
	TurnID                    string
	LastEventID               uint64
	OldestBufferedEventID     uint64
	NewestBufferedEventID     uint64
	FromStartAvailable        bool
	StartEvictedBeforeEventID uint64
	ActivePendingRequests     []PendingRequest
	Terminal                  *TaskTerminalEvent
}

type RespondPendingRequestResponse struct {
	TaskID           string
	SessionGroupID   string
	PendingRequestID string
	ClientResponseID string
	Accepted         bool
	AlreadyApplied   bool
	ResolvedEventID  uint64
}

type InterruptTaskResponse struct {
	TaskID                string
	SessionGroupID        string
	ThreadID              string
	TurnID                string
	State                 TaskState
	InterruptSent         bool
	PreTurnCancelRecorded bool
	AlreadyInterrupting   bool
	AlreadyTerminal       bool
	LastEventID           uint64
}
