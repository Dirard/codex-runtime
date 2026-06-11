package appserver

import (
	"encoding/json"
	"fmt"

	"github.com/Dirard/codex-runtime/internal/config"
	"github.com/Dirard/codex-runtime/internal/domain"
	"github.com/Dirard/codex-runtime/internal/redact"
)

type ThreadStartParams struct {
	CWD               string `json:"cwd"`
	ApprovalPolicy    string `json:"approvalPolicy"`
	ApprovalsReviewer string `json:"approvalsReviewer"`
	Sandbox           string `json:"sandbox,omitempty"`
	Permissions       string `json:"permissions,omitempty"`
}

type ThreadResumeParams struct {
	ThreadID     string `json:"threadId"`
	ExcludeTurns bool   `json:"excludeTurns"`
}

type UserInputText struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	TextElements []interface{} `json:"text_elements"`
}

type TurnStartParams struct {
	ThreadID            string          `json:"threadId"`
	ClientUserMessageID string          `json:"clientUserMessageId,omitempty"`
	Input               []UserInputText `json:"input"`
}

type TurnInterruptParams struct {
	TurnID string `json:"turnId"`
}

type DirectApprovalResponsePayload struct {
	Decision string `json:"decision"`
}

type PermissionsRequestApprovalResponse struct {
	Scope            string         `json:"scope,omitempty"`
	Permissions      map[string]any `json:"permissions"`
	StrictAutoReview bool           `json:"strictAutoReview,omitempty"`
}

type McpElicitationResponsePayload struct {
	Action  string          `json:"action"`
	Content json.RawMessage `json:"content"`
	Meta    any             `json:"_meta"`
}

type ToolUserInputResponsePayload struct {
	Answers map[string]ToolUserInputAnswerPayload `json:"answers"`
}

type ToolUserInputAnswerPayload struct {
	Answers []string `json:"answers"`
}

func NewThreadStartParams(group config.SessionGroup) (ThreadStartParams, error) {
	cwd := group.CanonicalCWD
	if cwd == "" {
		return ThreadStartParams{}, fmt.Errorf("session group canonical cwd is required")
	}

	params := ThreadStartParams{
		CWD:               cwd,
		ApprovalPolicy:    group.RuntimePolicy.ApprovalPolicy,
		ApprovalsReviewer: config.ApprovalsReviewerUser,
	}

	hasSandbox := group.RuntimePolicy.SandboxMode != ""
	hasPermissions := group.RuntimePolicy.PermissionsProfileID != ""
	if hasSandbox == hasPermissions {
		return ThreadStartParams{}, fmt.Errorf("runtime policy must set exactly one sandbox or permissions field")
	}
	if hasSandbox {
		params.Sandbox = group.RuntimePolicy.SandboxMode
	} else {
		params.Permissions = group.RuntimePolicy.PermissionsProfileID
	}
	return params, nil
}

func NewThreadResumeParams(threadID string) ThreadResumeParams {
	return ThreadResumeParams{
		ThreadID:     threadID,
		ExcludeTurns: true,
	}
}

func NewUserInputText(text string) UserInputText {
	return UserInputText{
		Type:         "text",
		Text:         text,
		TextElements: []interface{}{},
	}
}

func NewTurnStartParams(threadID string, clientUserMessageID string, input []UserInputText) TurnStartParams {
	return TurnStartParams{
		ThreadID:            threadID,
		ClientUserMessageID: clientUserMessageID,
		Input:               input,
	}
}

func NewTurnInterruptParams(turnID string) TurnInterruptParams {
	return TurnInterruptParams{TurnID: turnID}
}

func NewCommandApprovalResponsePayload(decision domain.ApprovalWireDecision) (DirectApprovalResponsePayload, bool) {
	return newDirectApprovalResponsePayload(decision)
}

func NewFileChangeApprovalResponsePayload(decision domain.ApprovalWireDecision) (DirectApprovalResponsePayload, bool) {
	return newDirectApprovalResponsePayload(decision)
}

func newDirectApprovalResponsePayload(decision domain.ApprovalWireDecision) (DirectApprovalResponsePayload, bool) {
	wireDecision, ok := decision.AppServerWireValue()
	if !ok {
		return DirectApprovalResponsePayload{}, false
	}
	return DirectApprovalResponsePayload{Decision: wireDecision}, true
}

func NewPermissionsRequestApprovalResponse(
	permissions map[string]any,
	scope domain.PermissionScope,
	strictAutoReview bool,
) (PermissionsRequestApprovalResponse, bool) {
	if permissions == nil {
		permissions = map[string]any{}
	}

	response := PermissionsRequestApprovalResponse{
		Permissions:      permissions,
		StrictAutoReview: strictAutoReview,
	}
	switch scope {
	case "", domain.PermissionScopeTurn:
	case domain.PermissionScopeSession:
		response.Scope = string(domain.PermissionScopeSession)
	default:
		return PermissionsRequestApprovalResponse{}, false
	}
	if len(permissions) == 0 && (scope == domain.PermissionScopeSession || strictAutoReview) {
		return PermissionsRequestApprovalResponse{}, false
	}
	return response, true
}

func NewMcpElicitationResponsePayload(response domain.McpElicitationPendingResponse) (McpElicitationResponsePayload, bool) {
	switch response.Action {
	case domain.McpElicitationActionAccept:
		content := json.RawMessage(response.ContentJSON)
		if !json.Valid(content) {
			return McpElicitationResponsePayload{}, false
		}
		return McpElicitationResponsePayload{
			Action:  string(domain.McpElicitationActionAccept),
			Content: append(json.RawMessage(nil), content...),
			Meta:    nil,
		}, true
	case domain.McpElicitationActionDecline, domain.McpElicitationActionCancel:
		if response.ContentJSON != "" {
			return McpElicitationResponsePayload{}, false
		}
		return McpElicitationResponsePayload{
			Action:  string(response.Action),
			Content: json.RawMessage("null"),
			Meta:    nil,
		}, true
	default:
		return McpElicitationResponsePayload{}, false
	}
}

func NewMcpElicitationResponsePayloadWithRedaction(
	response domain.McpElicitationPendingResponse,
	registry *redact.Registry,
) (McpElicitationResponsePayload, bool) {
	payload, ok := NewMcpElicitationResponsePayload(response)
	if !ok {
		return McpElicitationResponsePayload{}, false
	}
	if response.Action == domain.McpElicitationActionAccept {
		if err := redact.RegisterSensitiveJSONScalars(registry, payload.Content); err != nil {
			return McpElicitationResponsePayload{}, false
		}
	}
	return payload, true
}

func NewToolUserInputResponsePayload(answers []domain.ToolUserInputAnswer) (ToolUserInputResponsePayload, bool) {
	payload := ToolUserInputResponsePayload{
		Answers: make(map[string]ToolUserInputAnswerPayload, len(answers)),
	}
	for _, answer := range answers {
		if answer.QuestionID == "" {
			return ToolUserInputResponsePayload{}, false
		}
		if _, ok := payload.Answers[answer.QuestionID]; ok {
			return ToolUserInputResponsePayload{}, false
		}
		values := append([]string(nil), answer.Answers...)
		payload.Answers[answer.QuestionID] = ToolUserInputAnswerPayload{Answers: values}
	}
	return payload, true
}

func NewToolUserInputResponsePayloadWithRedaction(
	answers []domain.ToolUserInputAnswer,
	registry *redact.Registry,
) (ToolUserInputResponsePayload, bool) {
	payload, ok := NewToolUserInputResponsePayload(answers)
	if !ok {
		return ToolUserInputResponsePayload{}, false
	}
	if registry != nil {
		for _, answer := range answers {
			registry.AddMany(answer.Answers...)
		}
	}
	return payload, true
}
