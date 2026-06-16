package domain

type SessionGroupMetadata struct {
	SessionGroupID           string
	WorkspaceID              string
	GRPCInboundMessageBytes  int
	GRPCOutboundMessageBytes int
}

type ContextBlock struct {
	Kind        ContextBlockKind
	SourceLabel string
	SourceURI   string
	MimeType    string
	Content     string
}

type StartTaskCommand struct {
	SessionGroupID        string
	WorkspaceID           string
	Prompt                string
	ContextBlocks         []ContextBlock
	ThreadID              string
	ClientMessageID       string
	UICorrelationMetadata map[string]string
}

type StartChatRunCommand struct {
	SessionGroupID        string
	WorkspaceID           string
	Prompt                string
	ContextBlocks         []ContextBlock
	ClientMessageID       string
	IdempotencyKey        string
	UICorrelationMetadata map[string]string
}

type StartChatRunResponse struct {
	ChatID            string
	RunID             string
	SessionGroupID    string
	WorkspaceID       string
	LastEventID       uint64
	EventCursor       string
	FirstTurnAccepted bool
	ProcessEpoch      string
}

type GetChatCommand struct {
	SessionGroupID string
	WorkspaceID    string
	ChatID         string
}

type GetChatResponse struct {
	Chat   Chat
	Status ChatStatus
}

type RunChatTurnCommand struct {
	SessionGroupID        string
	WorkspaceID           string
	ChatID                string
	Prompt                string
	ContextBlocks         []ContextBlock
	ClientMessageID       string
	IdempotencyKey        string
	UICorrelationMetadata map[string]string
}

type RunChatTurnResponse struct {
	ChatID         string
	RunID          string
	SessionGroupID string
	WorkspaceID    string
	Status         ChatStatus
	LastEventID    uint64
	EventCursor    string
	TurnAccepted   bool
}

type GetChatStatusCommand struct {
	SessionGroupID string
	WorkspaceID    string
	ChatID         string
}

type GetChatStatusResponse struct {
	Status ChatStatus
}

type GetChatHistoryCommand struct {
	SessionGroupID string
	WorkspaceID    string
	ChatID         string
	RequestedDepth ChatHistoryDepth
	Cursor         string
	Limit          uint32
	SortDirection  ChatHistorySortDirection
}

type GetChatHistoryResponse struct {
	ChatID          string
	Turns           []ChatTurnSummary
	NextCursor      string
	BackwardsCursor string
	ReturnedDepth   ChatHistoryDepth
	Capability      ChatCapabilityState
	Narrowed        *ChatNarrowedOutcome
}

type StreamChatEventsCommand struct {
	SessionGroupID     string
	WorkspaceID        string
	ChatID             string
	CursorKind         StreamCursorKind
	AfterEventID       uint64
	AfterEventCursor   string
	ClientSubscriberID string
}

type RespondChatPendingCommand struct {
	SessionGroupID   string
	WorkspaceID      string
	ChatID           string
	PendingRequestID string
	ClientResponseID string
	IdempotencyKey   string
	Response         PendingResponse
}

type RespondChatPendingResponse struct {
	ChatID           string
	RunID            string
	SessionGroupID   string
	WorkspaceID      string
	PendingRequestID string
	ClientResponseID string
	Accepted         bool
	AlreadyApplied   bool
	LastEventID      uint64
	Status           ChatStatus
}

type InterruptChatRunCommand struct {
	SessionGroupID  string
	WorkspaceID     string
	ChatID          string
	RunID           string
	ClientRequestID string
	IdempotencyKey  string
}

type InterruptChatRunResponse struct {
	ChatID              string
	RunID               string
	SessionGroupID      string
	WorkspaceID         string
	Status              ChatStatus
	InterruptSent       bool
	AlreadyInterrupting bool
	AlreadyTerminal     bool
	LastEventID         uint64
}

type StreamCursorKind string

const (
	StreamCursorFromStart    StreamCursorKind = "from_start"
	StreamCursorAfterEventID StreamCursorKind = "after_event_id"
)

type StreamTaskCommand struct {
	TaskID             string
	CursorKind         StreamCursorKind
	AfterEventID       uint64
	ClientSubscriberID string
}

type ClientMessageTaskLocator struct {
	SessionGroupID  string
	ClientMessageID string
}

type ThreadTaskLocator struct {
	SessionGroupID string
	ThreadID       string
}

type TaskLocatorKind string

const (
	TaskLocatorByTaskID        TaskLocatorKind = "task_id"
	TaskLocatorByClientMessage TaskLocatorKind = "client_message"
	TaskLocatorByThread        TaskLocatorKind = "thread"
)

type TaskLocator struct {
	Kind                 TaskLocatorKind
	TaskID               string
	ClientMessageLocator ClientMessageTaskLocator
	ThreadLocator        ThreadTaskLocator
}

type GetTaskStatusCommand struct {
	Locator TaskLocator
}

type InterruptTaskCommand struct {
	Locator         TaskLocator
	ClientRequestID string
}

type PendingResponse struct {
	Approval       *ApprovalPendingResponse
	Permissions    *PermissionsPendingResponse
	McpElicitation *McpElicitationPendingResponse
	ToolUserInput  *ToolUserInputPendingResponse
}

type ApprovalPendingResponse struct {
	DecisionID string
}

type PermissionsPendingResponse struct {
	PermissionIDs    []string
	Scope            PermissionScope
	StrictAutoReview bool
}

type McpElicitationPendingResponse struct {
	Action      McpElicitationAction
	ContentJSON string
}

type ToolUserInputPendingResponse struct {
	Answers []ToolUserInputAnswer
}

type ToolUserInputAnswer struct {
	QuestionID string
	Answers    []string
}

type RespondPendingRequestCommand struct {
	TaskID           string
	PendingRequestID string
	ClientResponseID string
	Response         PendingResponse
}
