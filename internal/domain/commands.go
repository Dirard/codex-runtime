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
