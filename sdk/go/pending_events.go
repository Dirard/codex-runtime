package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
)

// PendingKind is the friendly category for pending action events.
type PendingKind string

const (
	PendingKindUnknown         PendingKind = ""
	PendingKindApproval        PendingKind = "approval"
	PendingKindPermissions     PendingKind = "permissions"
	PendingKindStructuredInput PendingKind = "structured_input"
	PendingKindUserInput       PendingKind = "user_input"
)

// PendingResolution reports how a pending action was resolved.
type PendingResolution string

const (
	PendingResolutionUnknown  PendingResolution = ""
	PendingResolutionAccepted PendingResolution = "accepted"
	PendingResolutionDeclined PendingResolution = "declined"
	PendingResolutionCanceled PendingResolution = "canceled"
	PendingResolutionGranted  PendingResolution = "granted"
	PendingResolutionDenied   PendingResolution = "denied"
	PendingResolutionAnswered PendingResolution = "answered"
	PendingResolutionExpired  PendingResolution = "expired"
	PendingResolutionCleared  PendingResolution = "cleared"
	PendingResolutionFailed   PendingResolution = "failed"
)

// PendingAction is implemented by friendly pending events that require an
// explicit product decision before the run can continue.
type PendingAction interface {
	Meta() EventMeta
	Raw() RawEvent
	PendingID() string
	PendingKind() PendingKind
	Display() ActionDisplay
	pendingAction()
}

// ActionDisplay is a safe UI summary for a pending action. Title and Summary are
// always populated by source data or SDK fallback.
type ActionDisplay struct {
	_ noUnkeyedLiterals

	Title     string
	Summary   string
	Details   []string
	Truncated bool
	Redacted  bool
}

type pendingBase struct {
	baseEvent
	pendingID   string
	pendingKind PendingKind
	display     ActionDisplay
}

func (event pendingBase) PendingID() string        { return event.pendingID }
func (event pendingBase) PendingKind() PendingKind { return event.pendingKind }
func (event pendingBase) Display() ActionDisplay   { return event.display }
func (event pendingBase) pendingAction()           {}

// ApprovalSubject distinguishes command approval from file-change approval.
type ApprovalSubject string

const (
	ApprovalSubjectUnknown    ApprovalSubject = ""
	ApprovalSubjectCommand    ApprovalSubject = "command"
	ApprovalSubjectFileChange ApprovalSubject = "file_change"
)

// ApprovalDecisionKind is the semantic meaning of a source-backed approval
// decision option.
type ApprovalDecisionKind string

const (
	ApprovalDecisionUnknown          ApprovalDecisionKind = ""
	ApprovalDecisionAccept           ApprovalDecisionKind = "accept"
	ApprovalDecisionAcceptForSession ApprovalDecisionKind = "accept_for_session"
	ApprovalDecisionDecline          ApprovalDecisionKind = "decline"
	ApprovalDecisionCancel           ApprovalDecisionKind = "cancel"
)

// ApprovalDecision is one source-backed selectable or disabled approval option.
type ApprovalDecision struct {
	_ noUnkeyedLiterals

	ID                string
	Decision          ApprovalDecisionKind
	Label             string
	Summary           string
	Selectable        bool
	UnsupportedReason string
}

// ApprovalRequested asks the product to accept, decline or cancel a command or
// file-change request after policy checks.
type ApprovalRequested struct {
	pendingBase
	subject   ApprovalSubject
	decisions []ApprovalDecision
	command   CommandApproval
	file      FileChangeApproval
}

func (event *ApprovalRequested) Subject() ApprovalSubject { return event.subject }
func (event *ApprovalRequested) Decisions() []ApprovalDecision {
	return append([]ApprovalDecision(nil), event.decisions...)
}
func (event *ApprovalRequested) Command() CommandApproval       { return event.command }
func (event *ApprovalRequested) FileChange() FileChangeApproval { return event.file }

// ApprovalSecurity summarizes source-backed security context for command
// approvals.
type ApprovalSecurity struct {
	_ noUnkeyedLiterals

	HasPrivilegeExpansion bool
	NetworkHostLabel      string
	NetworkProtocol       string
	AdditionalNetwork     bool
	FilesystemEntries     []ApprovalFilesystemEntry
	ExecPolicyCommand     string
	ExecPolicyTruncated   bool
	NetworkPolicy         []ApprovalNetworkPolicy
	BlockingReason        string
}

// ApprovalFilesystemEntry describes one filesystem permission expansion in a
// command approval.
type ApprovalFilesystemEntry struct {
	_ noUnkeyedLiterals

	ID                 string
	Access             string
	PathLabel          string
	Approvable         bool
	UnapprovableReason string
}

// ApprovalNetworkPolicy describes one source-backed network policy amendment.
type ApprovalNetworkPolicy struct {
	_ noUnkeyedLiterals

	HostLabel  string
	Action     string
	Approvable bool
}

// CommandApproval is the command-specific detail for an approval request.
type CommandApproval struct {
	_ noUnkeyedLiterals

	CommandDisplay string
	WorkspaceLabel string
	Reason         string
	Security       ApprovalSecurity
}

// FileGrantRoot describes a source-backed file-change grant root.
type FileGrantRoot struct {
	_ noUnkeyedLiterals

	Present            bool
	RootLabel          string
	UnderConfiguredCWD bool
	Approvable         bool
	UnapprovableReason string
}

// FileChangeApproval is the file-change detail for an approval request.
type FileChangeApproval struct {
	_ noUnkeyedLiterals

	FileLabel       string
	ChangeKind      string
	DiffSummary     string
	GrantRoot       FileGrantRoot
	DiffUnavailable bool
}

// PermissionScope is the lifetime requested for a permission grant.
type PermissionScope string

const (
	PermissionScopeUnknown PermissionScope = ""
	PermissionScopeTurn    PermissionScope = "turn"
	PermissionScopeSession PermissionScope = "session"
)

// Permission is one source-backed permission requested by the runtime.
type Permission struct {
	_ noUnkeyedLiterals

	ID                string
	Kind              string
	Label             string
	ScopeLabel        string
	Grantable         bool
	UngrantableReason string
}

// PermissionsRequested asks the product to grant or deny runtime permissions.
// Grant helpers default to strict auto-review.
type PermissionsRequested struct {
	pendingBase
	permissions      []Permission
	recommendedScope PermissionScope
	reason           string
}

func (event *PermissionsRequested) RequestedPermissions() []Permission {
	return append([]Permission(nil), event.permissions...)
}
func (event *PermissionsRequested) RecommendedScope() PermissionScope { return event.recommendedScope }
func (event *PermissionsRequested) Reason() string                    { return event.reason }

// StructuredInputMode is the supported structured input presentation mode.
type StructuredInputMode string

const (
	StructuredInputModeUnknown StructuredInputMode = ""
	StructuredInputModeForm    StructuredInputMode = "form"
	StructuredInputModeURL     StructuredInputMode = "url"
)

// StructuredInputFieldType is the supported primitive type for form fields.
type StructuredInputFieldType string

const (
	StructuredInputFieldTypeString  StructuredInputFieldType = "string"
	StructuredInputFieldTypeNumber  StructuredInputFieldType = "number"
	StructuredInputFieldTypeBoolean StructuredInputFieldType = "boolean"
)

// StructuredInputField is one source-backed field in a structured input form.
type StructuredInputField struct {
	_ noUnkeyedLiterals

	Name        string
	Type        StructuredInputFieldType
	Required    bool
	Default     any
	Description string
}

// ErrUnsupportedSchema wraps structured input schemas that the friendly form
// helper cannot represent.
var ErrUnsupportedSchema = errors.New("codex: unsupported structured input schema")

// UnsupportedSchemaError describes the unsupported part of a structured input
// schema and unwraps to ErrUnsupportedSchema.
type UnsupportedSchemaError struct {
	_ noUnkeyedLiterals

	Message string
}

func (err *UnsupportedSchemaError) Error() string {
	if err == nil || err.Message == "" {
		return ErrUnsupportedSchema.Error()
	}
	return ErrUnsupportedSchema.Error() + ": " + err.Message
}

func (err *UnsupportedSchemaError) Unwrap() error { return ErrUnsupportedSchema }

// StructuredInputRequested asks the product to submit or cancel a source-backed
// structured input request.
type StructuredInputRequested struct {
	pendingBase
	mode        StructuredInputMode
	message     string
	formSchema  string
	url         string
	submitLabel string
	schemaErr   error
	fields      []StructuredInputField
}

func (event *StructuredInputRequested) Mode() StructuredInputMode { return event.mode }
func (event *StructuredInputRequested) Message() string           { return event.message }
func (event *StructuredInputRequested) URL() string               { return event.url }
func (event *StructuredInputRequested) SubmitLabel() string       { return event.submitLabel }
func (event *StructuredInputRequested) SchemaError() error        { return event.schemaErr }
func (event *StructuredInputRequested) Fields() ([]StructuredInputField, error) {
	if event.schemaErr != nil {
		return nil, event.schemaErr
	}
	return append([]StructuredInputField(nil), event.fields...), nil
}

// UserInputQuestion is one source-backed user-input question.
type UserInputQuestion struct {
	_ noUnkeyedLiterals

	ID       string
	Header   string
	Question string
	Secret   bool
	Other    bool
	Options  []UserInputOption
}

// UserInputOption is one selectable option for a user-input question.
type UserInputOption struct {
	_ noUnkeyedLiterals

	Value string
	Label string
}

// UserInputAnswer is the friendly response value for a user-input question.
type UserInputAnswer struct {
	_ noUnkeyedLiterals

	QuestionID string
	Values     []string
}

// UserInputRequested asks the product to answer source-backed user-input
// questions.
type UserInputRequested struct {
	pendingBase
	questions []UserInputQuestion
}

func (event *UserInputRequested) Questions() []UserInputQuestion {
	return append([]UserInputQuestion(nil), event.questions...)
}

func decodePendingCreated(meta EventMeta, envelope *pb.StreamChatEventsResponse, pending *pb.ChatPendingRequest) (StreamEvent, error) {
	if pending == nil {
		return nil, pendingDecodeError(meta, envelope, pending, "pending request is nil")
	}
	if strings.TrimSpace(pending.GetPendingRequestId()) == "" {
		return nil, pendingDecodeError(meta, envelope, pending, "pending request id is required")
	}
	if pending.GetChatId() != "" && pending.GetChatId() != meta.ChatID {
		return nil, pendingDecodeError(meta, envelope, pending, "pending request chat id mismatch")
	}
	if pending.GetRunId() != "" && pending.GetRunId() != meta.RunID {
		return nil, pendingDecodeError(meta, envelope, pending, "pending request run id mismatch")
	}
	kind := pendingKindFromProto(pending.GetPendingType())
	displayKind := pendingKindFromDisplay(pending.GetDisplay().GetPayload())
	if kind != PendingKindUnknown && displayKind != PendingKindUnknown && kind != displayKind {
		return nil, pendingDecodeError(meta, envelope, pending, "pending request type/display mismatch")
	}
	display := displayFromPending(pending)
	base := pendingBase{
		baseEvent:   newBase(eventKindForPending(kind), meta, envelope, true),
		pendingID:   pending.GetPendingRequestId(),
		pendingKind: kind,
		display:     display,
	}
	switch displayPayload := pending.GetDisplay().GetPayload().(type) {
	case *pb.PendingRequestDisplay_CommandApproval:
		return &ApprovalRequested{
			pendingBase: base,
			subject:     ApprovalSubjectCommand,
			decisions:   decisionsFromProto(displayPayload.CommandApproval.GetDecisionOptions()),
			command: CommandApproval{
				CommandDisplay: displayPayload.CommandApproval.GetCommandDisplay(),
				WorkspaceLabel: displayPayload.CommandApproval.GetWorkspaceLabel(),
				Reason:         displayPayload.CommandApproval.GetReason(),
				Security:       approvalSecurityFromProto(displayPayload.CommandApproval.GetApprovalSecurity()),
			},
		}, nil
	case *pb.PendingRequestDisplay_FileChangeApproval:
		return &ApprovalRequested{
			pendingBase: base,
			subject:     ApprovalSubjectFileChange,
			decisions:   decisionsFromProto(displayPayload.FileChangeApproval.GetDecisionOptions()),
			file: FileChangeApproval{
				FileLabel:       displayPayload.FileChangeApproval.GetFileLabel(),
				ChangeKind:      displayPayload.FileChangeApproval.GetChangeKind(),
				DiffSummary:     displayPayload.FileChangeApproval.GetDiffSummary(),
				GrantRoot:       fileGrantRootFromProto(displayPayload.FileChangeApproval.GetGrantRoot()),
				DiffUnavailable: displayPayload.FileChangeApproval.GetDiffUnavailable(),
			},
		}, nil
	case *pb.PendingRequestDisplay_PermissionsApproval:
		return &PermissionsRequested{
			pendingBase:      base,
			permissions:      permissionsFromProto(displayPayload.PermissionsApproval.GetRequestedPermissions()),
			recommendedScope: permissionScopeFromProto(displayPayload.PermissionsApproval.GetRecommendedScope()),
			reason:           displayPayload.PermissionsApproval.GetReason(),
		}, nil
	case *pb.PendingRequestDisplay_McpElicitation:
		fields, schemaErr := fieldsFromSchema(displayPayload.McpElicitation.GetFormSchemaJson())
		return &StructuredInputRequested{
			pendingBase: base,
			mode:        structuredInputModeFromProto(displayPayload.McpElicitation.GetMode()),
			message:     displayPayload.McpElicitation.GetMessage(),
			formSchema:  displayPayload.McpElicitation.GetFormSchemaJson(),
			url:         displayPayload.McpElicitation.GetUrl(),
			submitLabel: displayPayload.McpElicitation.GetSubmitLabel(),
			schemaErr:   schemaErr,
			fields:      fields,
		}, nil
	case *pb.PendingRequestDisplay_ToolUserInput:
		return &UserInputRequested{
			pendingBase: base,
			questions:   questionsFromProto(displayPayload.ToolUserInput.GetQuestions()),
		}, nil
	default:
		return &UnknownEvent{baseEvent: base.baseEvent, name: "pending_unknown"}, nil
	}
}

func pendingDecodeError(meta EventMeta, envelope *pb.StreamChatEventsResponse, pending *pb.ChatPendingRequest, message string) error {
	hasPayload := false
	if pending != nil && pending.GetDisplay().GetPayload() != nil {
		hasPayload = true
	}
	return &EventDecodeError{
		Message:  message,
		meta:     meta,
		raw:      newRawEvent(EventKindUnknown, EventSourceChatStream, meta.ID, meta.Cursor, hasPayload, envelope),
		position: EventPosition{ID: meta.ID, Cursor: meta.Cursor},
	}
}

func eventKindForPending(kind PendingKind) EventKind {
	switch kind {
	case PendingKindApproval:
		return EventKindApprovalRequested
	case PendingKindPermissions:
		return EventKindPermissionsRequested
	case PendingKindStructuredInput:
		return EventKindStructuredInputRequested
	case PendingKindUserInput:
		return EventKindUserInputRequested
	default:
		return EventKindUnknown
	}
}

func pendingKindFromProto(kind pb.PendingType) PendingKind {
	switch kind {
	case pb.PendingType_PENDING_TYPE_COMMAND_APPROVAL, pb.PendingType_PENDING_TYPE_FILE_CHANGE_APPROVAL:
		return PendingKindApproval
	case pb.PendingType_PENDING_TYPE_PERMISSIONS_APPROVAL:
		return PendingKindPermissions
	case pb.PendingType_PENDING_TYPE_MCP_ELICITATION:
		return PendingKindStructuredInput
	case pb.PendingType_PENDING_TYPE_TOOL_USER_INPUT:
		return PendingKindUserInput
	default:
		return PendingKindUnknown
	}
}

func pendingKindFromDisplay(payload any) PendingKind {
	switch payload.(type) {
	case *pb.PendingRequestDisplay_CommandApproval, *pb.PendingRequestDisplay_FileChangeApproval:
		return PendingKindApproval
	case *pb.PendingRequestDisplay_PermissionsApproval:
		return PendingKindPermissions
	case *pb.PendingRequestDisplay_McpElicitation:
		return PendingKindStructuredInput
	case *pb.PendingRequestDisplay_ToolUserInput:
		return PendingKindUserInput
	default:
		return PendingKindUnknown
	}
}

func pendingResolutionFromProto(resolution pb.PendingResolution) PendingResolution {
	switch resolution {
	case pb.PendingResolution_PENDING_RESOLUTION_ACCEPTED:
		return PendingResolutionAccepted
	case pb.PendingResolution_PENDING_RESOLUTION_DECLINED:
		return PendingResolutionDeclined
	case pb.PendingResolution_PENDING_RESOLUTION_CANCELLED:
		return PendingResolutionCanceled
	case pb.PendingResolution_PENDING_RESOLUTION_GRANTED:
		return PendingResolutionGranted
	case pb.PendingResolution_PENDING_RESOLUTION_DENIED:
		return PendingResolutionDenied
	case pb.PendingResolution_PENDING_RESOLUTION_ANSWERED:
		return PendingResolutionAnswered
	case pb.PendingResolution_PENDING_RESOLUTION_EXPIRED:
		return PendingResolutionExpired
	case pb.PendingResolution_PENDING_RESOLUTION_CLEARED:
		return PendingResolutionCleared
	case pb.PendingResolution_PENDING_RESOLUTION_FAILED:
		return PendingResolutionFailed
	default:
		return PendingResolutionUnknown
	}
}

func approvalSecurityFromProto(security *pb.ApprovalSecurityMetadata) ApprovalSecurity {
	if security == nil {
		return ApprovalSecurity{}
	}
	filesystemEntries := make([]ApprovalFilesystemEntry, 0, len(security.GetAdditionalFilesystemEntries()))
	for _, entry := range security.GetAdditionalFilesystemEntries() {
		filesystemEntries = append(filesystemEntries, ApprovalFilesystemEntry{
			ID:                 entry.GetEntryId(),
			Access:             entry.GetAccess(),
			PathLabel:          entry.GetPathLabel(),
			Approvable:         entry.GetApprovable(),
			UnapprovableReason: entry.GetUnapprovableReason(),
		})
	}
	networkPolicy := make([]ApprovalNetworkPolicy, 0, len(security.GetNetworkPolicyAmendmentSummaries()))
	for _, summary := range security.GetNetworkPolicyAmendmentSummaries() {
		networkPolicy = append(networkPolicy, ApprovalNetworkPolicy{
			HostLabel:  summary.GetHostLabel(),
			Action:     summary.GetAction(),
			Approvable: summary.GetApprovable(),
		})
	}
	return ApprovalSecurity{
		HasPrivilegeExpansion: security.GetHasPrivilegeExpansion(),
		NetworkHostLabel:      security.GetNetworkContext().GetHostLabel(),
		NetworkProtocol:       security.GetNetworkContext().GetProtocol(),
		AdditionalNetwork:     security.GetAdditionalNetwork().GetEnabled(),
		FilesystemEntries:     filesystemEntries,
		ExecPolicyCommand:     security.GetExecpolicyAmendmentSummary().GetCommandDisplay(),
		ExecPolicyTruncated:   security.GetExecpolicyAmendmentSummary().GetTruncated(),
		NetworkPolicy:         networkPolicy,
		BlockingReason:        security.GetBlockingReason(),
	}
}

func fileGrantRootFromProto(root *pb.FileGrantRootDisplay) FileGrantRoot {
	if root == nil {
		return FileGrantRoot{}
	}
	return FileGrantRoot{
		Present:            root.GetPresent(),
		RootLabel:          root.GetRootLabel(),
		UnderConfiguredCWD: root.GetUnderConfiguredCwd(),
		Approvable:         root.GetApprovable(),
		UnapprovableReason: root.GetUnapprovableReason(),
	}
}

func displayFromPending(pending *pb.ChatPendingRequest) ActionDisplay {
	title := "Action requested"
	summary := "Codex is waiting for input."
	details := []string{}
	switch payload := pending.GetDisplay().GetPayload().(type) {
	case *pb.PendingRequestDisplay_CommandApproval:
		title = "Approve command"
		summary = nonEmpty(payload.CommandApproval.GetCommandDisplay(), "Command approval requested")
		details = appendNonEmpty(details, payload.CommandApproval.GetWorkspaceLabel(), payload.CommandApproval.GetReason())
	case *pb.PendingRequestDisplay_FileChangeApproval:
		title = "Approve file change"
		summary = nonEmpty(payload.FileChangeApproval.GetFileLabel(), "File change approval requested")
		details = appendNonEmpty(details, payload.FileChangeApproval.GetChangeKind(), payload.FileChangeApproval.GetDiffSummary())
	case *pb.PendingRequestDisplay_PermissionsApproval:
		title = "Grant permissions"
		summary = nonEmpty(payload.PermissionsApproval.GetReason(), "Permissions requested")
	case *pb.PendingRequestDisplay_McpElicitation:
		title = "Provide structured input"
		summary = nonEmpty(payload.McpElicitation.GetMessage(), "Structured input requested")
	case *pb.PendingRequestDisplay_ToolUserInput:
		title = "Answer questions"
		summary = "User input requested"
	}
	return ActionDisplay{Title: title, Summary: summary, Details: details}
}

func decisionsFromProto(options []*pb.ApprovalDecisionOption) []ApprovalDecision {
	out := make([]ApprovalDecision, 0, len(options))
	for _, option := range options {
		out = append(out, ApprovalDecision{
			ID:                option.GetDecisionId(),
			Decision:          approvalDecisionFromProto(option.GetWireDecision()),
			Label:             option.GetDisplayLabel(),
			Summary:           option.GetSummary(),
			Selectable:        option.GetSelectable(),
			UnsupportedReason: option.GetUnsupportedReason(),
		})
	}
	return out
}

func approvalDecisionFromProto(decision pb.ApprovalWireDecision) ApprovalDecisionKind {
	switch decision {
	case pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT:
		return ApprovalDecisionAccept
	case pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT_FOR_SESSION:
		return ApprovalDecisionAcceptForSession
	case pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_DECLINE:
		return ApprovalDecisionDecline
	case pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_CANCEL:
		return ApprovalDecisionCancel
	default:
		return ApprovalDecisionUnknown
	}
}

func permissionsFromProto(values []*pb.PermissionAtom) []Permission {
	out := make([]Permission, 0, len(values))
	for _, value := range values {
		out = append(out, Permission{
			ID:                value.GetPermissionId(),
			Kind:              value.GetKind(),
			Label:             value.GetDisplayLabel(),
			ScopeLabel:        value.GetScopeLabel(),
			Grantable:         value.GetGrantable(),
			UngrantableReason: value.GetUngrantableReason(),
		})
	}
	return out
}

func permissionScopeFromProto(scope pb.PermissionScope) PermissionScope {
	switch scope {
	case pb.PermissionScope_PERMISSION_SCOPE_TURN:
		return PermissionScopeTurn
	case pb.PermissionScope_PERMISSION_SCOPE_SESSION:
		return PermissionScopeSession
	default:
		return PermissionScopeUnknown
	}
}

func structuredInputModeFromProto(mode pb.ElicitationMode) StructuredInputMode {
	switch mode {
	case pb.ElicitationMode_ELICITATION_MODE_FORM:
		return StructuredInputModeForm
	case pb.ElicitationMode_ELICITATION_MODE_URL:
		return StructuredInputModeURL
	default:
		return StructuredInputModeUnknown
	}
}

func fieldsFromSchema(schema string) ([]StructuredInputField, error) {
	if strings.TrimSpace(schema) == "" {
		return nil, nil
	}
	var root struct {
		Type       string                     `json:"type"`
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(schema), &root); err != nil {
		return nil, &UnsupportedSchemaError{Message: "invalid JSON schema"}
	}
	if root.Type != "" && root.Type != "object" {
		return nil, &UnsupportedSchemaError{Message: "only object schemas are supported"}
	}
	required := make(map[string]bool, len(root.Required))
	for _, name := range root.Required {
		required[name] = true
	}
	names := make([]string, 0, len(root.Properties))
	for name := range root.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	fields := make([]StructuredInputField, 0, len(names))
	for _, name := range names {
		var property struct {
			Type        string `json:"type"`
			Description string `json:"description"`
			Default     any    `json:"default"`
		}
		if err := json.Unmarshal(root.Properties[name], &property); err != nil {
			return nil, &UnsupportedSchemaError{Message: fmt.Sprintf("field %q has invalid schema", name)}
		}
		fieldType := structuredInputFieldType(property.Type)
		if fieldType == "" {
			return nil, &UnsupportedSchemaError{Message: fmt.Sprintf("field %q has unsupported type %q", name, property.Type)}
		}
		fields = append(fields, StructuredInputField{
			Name:        name,
			Type:        fieldType,
			Required:    required[name],
			Default:     property.Default,
			Description: property.Description,
		})
	}
	return fields, nil
}

func structuredInputFieldType(value string) StructuredInputFieldType {
	switch value {
	case "string", "":
		return StructuredInputFieldTypeString
	case "number", "integer":
		return StructuredInputFieldTypeNumber
	case "boolean":
		return StructuredInputFieldTypeBoolean
	default:
		return ""
	}
}

func questionsFromProto(values []*pb.ToolUserInputQuestion) []UserInputQuestion {
	out := make([]UserInputQuestion, 0, len(values))
	for _, value := range values {
		options := make([]UserInputOption, 0, len(value.GetOptions()))
		for _, option := range value.GetOptions() {
			options = append(options, UserInputOption{Value: option, Label: option})
		}
		out = append(out, UserInputQuestion{
			ID:       value.GetId(),
			Header:   value.GetHeader(),
			Question: value.GetQuestion(),
			Secret:   value.GetIsSecret(),
			Other:    value.GetIsOther(),
			Options:  options,
		})
	}
	return out
}

func appendNonEmpty(values []string, candidates ...string) []string {
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) != "" {
			values = append(values, candidate)
		}
	}
	return values
}

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
