package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc/codes"
)

var (
	// ErrActionValidation wraps validation failures for source-backed action
	// builders such as approval, permissions and structured input responses.
	ErrActionValidation = errors.New("codex: invalid action response")
	// ErrInvalidActionResponse reports a response that was nil or not produced
	// by a friendly SDK action builder.
	ErrInvalidActionResponse = errors.New("codex: invalid action response value")
	// ErrUnhandledAction is returned by RunWithHandler when a pending action is
	// delivered but the handler leaves it unresolved.
	ErrUnhandledAction = errors.New("codex: unhandled pending action")
)

// ActionResponse is a validated response produced by a PendingAction helper.
// Pass it to Chat.Respond; do not construct implementations yourself.
type ActionResponse interface {
	actionResponse()
}

type actionResponse struct {
	pendingID string
	kind      PendingKind
	apply     func(*pb.RespondChatPendingRequest)
}

func (*actionResponse) actionResponse() {}

// ActionValidationError describes why an action builder rejected caller input.
// It unwraps to ErrActionValidation for errors.Is checks.
type ActionValidationError struct {
	_ noUnkeyedLiterals

	Message string
}

func (err *ActionValidationError) Error() string {
	if err == nil || err.Message == "" {
		return ErrActionValidation.Error()
	}
	return ErrActionValidation.Error() + ": " + err.Message
}

func (err *ActionValidationError) Unwrap() error { return ErrActionValidation }

// InvalidActionResponseError describes why Chat.Respond rejected an
// ActionResponse value before sending it to the gateway.
type InvalidActionResponseError struct {
	_ noUnkeyedLiterals

	Message string
}

func (err *InvalidActionResponseError) Error() string {
	if err == nil || err.Message == "" {
		return ErrInvalidActionResponse.Error()
	}
	return ErrInvalidActionResponse.Error() + ": " + err.Message
}

func (err *InvalidActionResponseError) Unwrap() error { return ErrInvalidActionResponse }

// Choose returns a response for one source-backed approval decision option.
func (event *ApprovalRequested) Choose(decision ApprovalDecision) (ActionResponse, error) {
	if strings.TrimSpace(decision.ID) == "" {
		return nil, &ActionValidationError{Message: "approval decision id is required"}
	}
	for _, option := range event.decisions {
		if option.ID == decision.ID {
			if !option.Selectable {
				return nil, &ActionValidationError{Message: "approval decision is not selectable"}
			}
			return approvalResponse(event.PendingID(), option.ID), nil
		}
	}
	return nil, &ActionValidationError{Message: "approval decision is not allowed by this pending action"}
}

// Approve chooses the single selectable accept decision for this approval.
func (event *ApprovalRequested) Approve() (ActionResponse, error) {
	return event.chooseByKind(ApprovalDecisionAccept)
}

// Deny chooses the single selectable decline decision for this approval.
func (event *ApprovalRequested) Deny() (ActionResponse, error) {
	return event.chooseByKind(ApprovalDecisionDecline)
}

// Cancel chooses the single selectable cancel decision for this approval.
func (event *ApprovalRequested) Cancel() (ActionResponse, error) {
	return event.chooseByKind(ApprovalDecisionCancel)
}

func (event *ApprovalRequested) chooseByKind(kind ApprovalDecisionKind) (ActionResponse, error) {
	var matched []ApprovalDecision
	for _, decision := range event.decisions {
		if decision.Decision == kind && decision.Selectable {
			matched = append(matched, decision)
		}
	}
	if len(matched) != 1 {
		return nil, &ActionValidationError{Message: fmt.Sprintf("approval decision %q is absent or ambiguous", kind)}
	}
	return approvalResponse(event.PendingID(), matched[0].ID), nil
}

func approvalResponse(pendingID string, decisionID string) ActionResponse {
	return &actionResponse{
		pendingID: pendingID,
		kind:      PendingKindApproval,
		apply: func(req *pb.RespondChatPendingRequest) {
			req.Response = &pb.RespondChatPendingRequest_Approval{
				Approval: &pb.ApprovalPendingResponse{DecisionId: decisionID},
			}
		},
	}
}

// PermissionGrantOption configures a permission grant response.
type PermissionGrantOption func(*permissionGrantOptions)

type permissionGrantOptions struct {
	strictAutoReview bool
}

// WithRelaxedAutoReview disables the default strict auto-review flag for a
// permission grant. Keep the default strict behavior unless your product policy
// has a narrower compensating control.
func WithRelaxedAutoReview() PermissionGrantOption {
	return func(opts *permissionGrantOptions) {
		opts.strictAutoReview = false
	}
}

// GrantTurn grants requested permissions for the current turn with strict auto-review.
func (event *PermissionsRequested) GrantTurn(permissionIDs ...string) (ActionResponse, error) {
	return event.grant(pb.PermissionScope_PERMISSION_SCOPE_TURN, permissionIDs, nil)
}

// GrantSession grants requested permissions for the session with strict auto-review.
func (event *PermissionsRequested) GrantSession(permissionIDs ...string) (ActionResponse, error) {
	return event.grant(pb.PermissionScope_PERMISSION_SCOPE_SESSION, permissionIDs, nil)
}

// GrantTurnWithOptions grants current-turn permissions with explicit grant options.
func (event *PermissionsRequested) GrantTurnWithOptions(permissionIDs []string, opts ...PermissionGrantOption) (ActionResponse, error) {
	return event.grant(pb.PermissionScope_PERMISSION_SCOPE_TURN, permissionIDs, opts)
}

// GrantSessionWithOptions grants session permissions with explicit grant options.
func (event *PermissionsRequested) GrantSessionWithOptions(permissionIDs []string, opts ...PermissionGrantOption) (ActionResponse, error) {
	return event.grant(pb.PermissionScope_PERMISSION_SCOPE_SESSION, permissionIDs, opts)
}

func (event *PermissionsRequested) grant(scope pb.PermissionScope, permissionIDs []string, opts []PermissionGrantOption) (ActionResponse, error) {
	applied := permissionGrantOptions{strictAutoReview: true}
	for _, opt := range opts {
		if opt != nil {
			opt(&applied)
		}
	}
	ids := append([]string(nil), permissionIDs...)
	if len(ids) == 0 {
		for _, permission := range event.permissions {
			if permission.Grantable {
				ids = append(ids, permission.ID)
			}
		}
	}
	allowed := map[string]Permission{}
	for _, permission := range event.permissions {
		allowed[permission.ID] = permission
	}
	for _, id := range ids {
		permission, ok := allowed[id]
		if !ok {
			return nil, &ActionValidationError{Message: "permission is not requested"}
		}
		if !permission.Grantable {
			return nil, &ActionValidationError{Message: "permission is not grantable"}
		}
	}
	return permissionsResponse(event.PendingID(), ids, scope, applied.strictAutoReview), nil
}

// Deny rejects a permissions request without granting any permission ids.
func (event *PermissionsRequested) Deny() ActionResponse {
	return permissionsResponse(event.PendingID(), nil, pb.PermissionScope_PERMISSION_SCOPE_TURN, false)
}

func permissionsResponse(pendingID string, permissionIDs []string, scope pb.PermissionScope, strict bool) ActionResponse {
	return &actionResponse{
		pendingID: pendingID,
		kind:      PendingKindPermissions,
		apply: func(req *pb.RespondChatPendingRequest) {
			req.Response = &pb.RespondChatPendingRequest_Permissions{
				Permissions: &pb.PermissionsPendingResponse{
					PermissionIds:    append([]string(nil), permissionIDs...),
					Scope:            scope,
					StrictAutoReview: strict,
				},
			}
		},
	}
}

// Submit validates and JSON-encodes structured input values for this request.
func (event *StructuredInputRequested) Submit(values map[string]any) (ActionResponse, error) {
	if event.schemaErr != nil {
		return nil, event.schemaErr
	}
	fields, err := event.Fields()
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field.Name] = true
		value, ok := values[field.Name]
		if field.Required && (!ok || value == nil || value == "") {
			return nil, &ActionValidationError{Message: fmt.Sprintf("field %q is required", field.Name)}
		}
		if ok && value != nil {
			if err := validateStructuredInputValue(field, value); err != nil {
				return nil, err
			}
		}
	}
	for name := range values {
		if !allowed[name] {
			return nil, &ActionValidationError{Message: fmt.Sprintf("field %q is not in schema", name)}
		}
	}
	content, err := json.Marshal(values)
	if err != nil {
		return nil, &ActionValidationError{Message: "values are not JSON encodable"}
	}
	return event.SubmitJSON(string(content)), nil
}

func validateStructuredInputValue(field StructuredInputField, value any) error {
	switch field.Type {
	case StructuredInputFieldTypeString:
		if _, ok := value.(string); !ok {
			return &ActionValidationError{Message: fmt.Sprintf("field %q must be a string", field.Name)}
		}
	case StructuredInputFieldTypeNumber:
		if !isStructuredInputNumber(value) {
			return &ActionValidationError{Message: fmt.Sprintf("field %q must be a number", field.Name)}
		}
	case StructuredInputFieldTypeBoolean:
		if _, ok := value.(bool); !ok {
			return &ActionValidationError{Message: fmt.Sprintf("field %q must be a boolean", field.Name)}
		}
	}
	return nil
}

func isStructuredInputNumber(value any) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64, json.Number:
		return true
	default:
		return false
	}
}

// SubmitJSON accepts advanced pre-encoded structured input JSON.
func (event *StructuredInputRequested) SubmitJSON(contentJSON string) ActionResponse {
	return mcpElicitationResponse(event.PendingID(), pb.McpElicitationAction_MCP_ELICITATION_ACTION_ACCEPT, contentJSON)
}

// Cancel rejects this structured input request without submitting values.
func (event *StructuredInputRequested) Cancel() ActionResponse {
	return mcpElicitationResponse(event.PendingID(), pb.McpElicitationAction_MCP_ELICITATION_ACTION_CANCEL, "")
}

func mcpElicitationResponse(pendingID string, action pb.McpElicitationAction, contentJSON string) ActionResponse {
	return &actionResponse{
		pendingID: pendingID,
		kind:      PendingKindStructuredInput,
		apply: func(req *pb.RespondChatPendingRequest) {
			req.Response = &pb.RespondChatPendingRequest_McpElicitation{
				McpElicitation: &pb.McpElicitationPendingResponse{Action: action, ContentJson: contentJSON},
			}
		},
	}
}

// Answer returns a response for source-backed user-input questions.
func (event *UserInputRequested) Answer(answers ...UserInputAnswer) (ActionResponse, error) {
	known := map[string]bool{}
	for _, question := range event.questions {
		known[question.ID] = true
	}
	raw := make([]*pb.ToolUserInputAnswer, 0, len(answers))
	for _, answer := range answers {
		if strings.TrimSpace(answer.QuestionID) == "" {
			return nil, &ActionValidationError{Message: "question id is required"}
		}
		if !known[answer.QuestionID] {
			return nil, &ActionValidationError{Message: "question id is not requested"}
		}
		raw = append(raw, &pb.ToolUserInputAnswer{
			QuestionId: answer.QuestionID,
			Answers:    append([]string(nil), answer.Values...),
		})
	}
	return &actionResponse{
		pendingID: event.PendingID(),
		kind:      PendingKindUserInput,
		apply: func(req *pb.RespondChatPendingRequest) {
			req.Response = &pb.RespondChatPendingRequest_ToolUserInput{
				ToolUserInput: &pb.ToolUserInputPendingResponse{Answers: raw},
			}
		},
	}, nil
}

// Respond sends a friendly pending-action response. The response must come from
// the pending action object that was delivered on the stream.
func (chat *Chat) Respond(ctx context.Context, response ActionResponse, opts ...RequestOption) error {
	action, err := normalizeActionResponse(response)
	if err != nil {
		return err
	}
	if err := validateRespondOptions(opts); err != nil {
		return err
	}
	_, err = chat.respondPending(ctx, action.pendingID, action.apply, opts...)
	return err
}

func normalizeActionResponse(response ActionResponse) (*actionResponse, error) {
	if response == nil {
		return nil, &InvalidActionResponseError{Message: "response is nil"}
	}
	value := reflect.ValueOf(response)
	if value.Kind() == reflect.Ptr && value.IsNil() {
		return nil, &InvalidActionResponseError{Message: "response is typed nil"}
	}
	action, ok := response.(*actionResponse)
	if !ok || action == nil {
		return nil, &InvalidActionResponseError{Message: "response was not created by an SDK action builder"}
	}
	if strings.TrimSpace(action.pendingID) == "" || action.apply == nil {
		return nil, &InvalidActionResponseError{Message: "response is incomplete"}
	}
	return action, nil
}

func validateRespondOptions(opts []RequestOption) error {
	applied := applyRequestOptions(opts)
	if applied.clientMessageID != "" || applied.clientRequestID != "" || len(applied.contextBlocks) != 0 ||
		len(applied.uiCorrelationMetadata) != 0 || len(applied.initialStreamOptions) != 0 {
		return &ActionValidationError{Message: "request option is not valid for Chat.Respond"}
	}
	return nil
}

// RunHandler is callback sugar over EventStream.NextEvent. Specialized
// callbacks consume matching events first; Event receives the remaining command,
// warning, unknown and other progress events.
type RunHandler struct {
	_ noUnkeyedLiterals

	Text     func(context.Context, *Chat, *AssistantTextDelta) error
	Action   func(context.Context, *Chat, PendingAction) (ActionResponse, error)
	Terminal func(context.Context, *Chat, TerminalEvent) error
	Event    func(context.Context, *Chat, StreamEvent) error
}

// HandlerResult captures the chat, terminal event and recovery metadata from a
// RunWithHandler call, including the last safe cursor for explicit resume.
type HandlerResult struct {
	_ noUnkeyedLiterals

	Chat               *Chat
	Terminal           TerminalEvent
	LastEvent          StreamEvent
	LastMeta           EventMeta
	LastSafeResumeMeta EventMeta
	UnhandledAction    PendingAction
}

// RunWithHandler starts a plain chat run and handles its typed event stream with
// callbacks. It returns start failures before callbacks and rejects cursor-moving
// initial stream options with ErrStreamCursorConflict.
func (c *Client) RunWithHandler(ctx context.Context, prompt string, handler RunHandler, opts ...RequestOption) (*HandlerResult, error) {
	if err := rejectCursorMovingInitialOptions(opts); err != nil {
		return nil, err
	}
	chat, events, err := c.Run(ctx, prompt, opts...)
	result := &HandlerResult{Chat: chat}
	if err != nil {
		if events != nil {
			_ = events.Close()
		}
		return result, err
	}
	return runHandlerLoop(ctx, chat, events, handler, result)
}

// RunWithHandler starts a workflow run and handles its typed event stream with
// callbacks.
func (w *Workflow) RunWithHandler(ctx context.Context, prompt string, handler RunHandler, opts ...RequestOption) (*HandlerResult, error) {
	if err := rejectCursorMovingInitialOptions(opts); err != nil {
		return nil, err
	}
	chat, events, err := w.Run(ctx, prompt, opts...)
	result := &HandlerResult{Chat: chat}
	if err != nil {
		if events != nil {
			_ = events.Close()
		}
		return result, err
	}
	return runHandlerLoop(ctx, chat, events, handler, result)
}

// RunWithHandler continues an existing chat and handles its typed event stream
// with callbacks while preserving the compatibility Chat.Run return path.
func (chat *Chat) RunWithHandler(ctx context.Context, prompt string, handler RunHandler, opts ...RequestOption) (*HandlerResult, error) {
	if err := rejectCursorMovingInitialOptions(opts); err != nil {
		return nil, err
	}
	result, events, err := chat.RunWithEvents(ctx, prompt, opts...)
	handlerResult := &HandlerResult{Chat: chat}
	if err != nil {
		if events != nil {
			_ = events.Close()
		}
		return handlerResult, err
	}
	_ = result
	return runHandlerLoop(ctx, chat, events, handler, handlerResult)
}

func runHandlerLoop(ctx context.Context, chat *Chat, events *EventStream, handler RunHandler, result *HandlerResult) (*HandlerResult, error) {
	if events == nil {
		return result, fmt.Errorf("%w: event stream is nil", ErrInvalidConfiguration)
	}
	defer events.Close()
	for {
		event, err := events.NextEvent(ctx)
		if err != nil {
			return result, err
		}
		result.LastEvent = event
		result.LastMeta = event.Meta()
		if _, isPending := event.(PendingAction); event.Meta().CanResumeAfter && !isPending {
			result.LastSafeResumeMeta = event.Meta()
		}
		switch typed := event.(type) {
		case *AssistantTextDelta:
			if handler.Text != nil {
				if err := handler.Text(ctx, chat, typed); err != nil {
					return result, err
				}
				continue
			}
		case PendingAction:
			if handler.Action == nil {
				if handler.Event != nil {
					if err := handler.Event(ctx, chat, event); err != nil {
						return result, err
					}
				}
				result.UnhandledAction = typed
				return result, ErrUnhandledAction
			}
			response, err := handler.Action(ctx, chat, typed)
			if err != nil {
				return result, err
			}
			if response == nil {
				result.UnhandledAction = typed
				return result, ErrUnhandledAction
			}
			if err := chat.Respond(ctx, response); err != nil {
				return result, err
			}
			continue
		case TerminalEvent:
			result.Terminal = typed
			var callbackErr error
			if handler.Terminal != nil {
				callbackErr = handler.Terminal(ctx, chat, typed)
			}
			terminalErr := typed.Result().Err
			if terminalErr != nil && callbackErr != nil {
				return result, errors.Join(terminalErr, callbackErr)
			}
			if terminalErr != nil {
				return result, terminalErr
			}
			return result, callbackErr
		}
		if handler.Event != nil {
			if err := handler.Event(ctx, chat, event); err != nil {
				return result, err
			}
		}
	}
}

func invalidActionResponse(message string) error {
	return statusError(codes.InvalidArgument, "invalid_action_response", message)
}
