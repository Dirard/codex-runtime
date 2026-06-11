package pending

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Dirard/codex-runtime/internal/appserver"
	"github.com/Dirard/codex-runtime/internal/domain"
	"github.com/Dirard/codex-runtime/internal/redact"
)

type ValidatedResponse struct {
	Payload    any
	Resolution domain.PendingResolution
}

func ValidateResponse(record *Record, response domain.PendingResponse, registry *redact.Registry) (ValidatedResponse, error) {
	if record == nil {
		return ValidatedResponse{}, fmt.Errorf("pending record is required")
	}
	switch record.Pending.PendingType {
	case domain.PendingTypeCommandApproval:
		return validateApprovalResponse(record, response, appserver.NewCommandApprovalResponsePayload)
	case domain.PendingTypeFileChangeApproval:
		return validateApprovalResponse(record, response, appserver.NewFileChangeApprovalResponsePayload)
	case domain.PendingTypePermissionsApproval:
		return validatePermissionsResponse(record, response)
	case domain.PendingTypeMcpElicitation:
		return validateMcpElicitationResponse(record, response, registry)
	case domain.PendingTypeToolUserInput:
		return validateToolUserInputResponse(record, response, registry)
	default:
		return ValidatedResponse{}, fmt.Errorf("unsupported pending type %q", record.Pending.PendingType)
	}
}

func ResponseMatchesType(pendingType domain.PendingType, response domain.PendingResponse) bool {
	switch pendingType {
	case domain.PendingTypeCommandApproval, domain.PendingTypeFileChangeApproval:
		return response.Approval != nil &&
			response.Permissions == nil &&
			response.McpElicitation == nil &&
			response.ToolUserInput == nil
	case domain.PendingTypePermissionsApproval:
		return response.Approval == nil &&
			response.Permissions != nil &&
			response.McpElicitation == nil &&
			response.ToolUserInput == nil
	case domain.PendingTypeMcpElicitation:
		return response.Approval == nil &&
			response.Permissions == nil &&
			response.McpElicitation != nil &&
			response.ToolUserInput == nil
	case domain.PendingTypeToolUserInput:
		return response.Approval == nil &&
			response.Permissions == nil &&
			response.McpElicitation == nil &&
			response.ToolUserInput != nil
	default:
		return false
	}
}

func AutoResolutionPayload(pendingType domain.PendingType) (any, bool, int, string) {
	switch pendingType {
	case domain.PendingTypeCommandApproval:
		payload, _ := appserver.NewCommandApprovalResponsePayload(domain.ApprovalWireDecisionDecline)
		return payload, false, 0, ""
	case domain.PendingTypeFileChangeApproval:
		payload, _ := appserver.NewFileChangeApprovalResponsePayload(domain.ApprovalWireDecisionDecline)
		return payload, false, 0, ""
	case domain.PendingTypePermissionsApproval:
		payload, _ := appserver.NewPermissionsRequestApprovalResponse(nil, "", false)
		return payload, false, 0, ""
	case domain.PendingTypeMcpElicitation:
		payload, _ := appserver.NewMcpElicitationResponsePayload(domain.McpElicitationPendingResponse{
			Action: domain.McpElicitationActionDecline,
		})
		return payload, false, 0, ""
	case domain.PendingTypeToolUserInput:
		return nil, true, ToolUserInputOverLimitCode, ToolUserInputOverLimitMessage
	default:
		payload, _ := appserver.NewCommandApprovalResponsePayload(domain.ApprovalWireDecisionDecline)
		return payload, false, 0, ""
	}
}

func validateApprovalResponse(
	record *Record,
	response domain.PendingResponse,
	builder func(domain.ApprovalWireDecision) (appserver.DirectApprovalResponsePayload, bool),
) (ValidatedResponse, error) {
	if !ResponseMatchesType(record.Pending.PendingType, response) {
		return ValidatedResponse{}, fmt.Errorf("response type mismatch")
	}
	option := record.ApprovalOptions[response.Approval.DecisionID]
	if !option.Selectable || option.WireDecision == "" {
		return ValidatedResponse{}, fmt.Errorf("approval decision is not selectable")
	}
	payload, ok := builder(option.WireDecision)
	if !ok {
		return ValidatedResponse{}, fmt.Errorf("approval decision cannot be mapped")
	}
	return ValidatedResponse{
		Payload:    payload,
		Resolution: approvalResolution(option.WireDecision),
	}, nil
}

func validatePermissionsResponse(record *Record, response domain.PendingResponse) (ValidatedResponse, error) {
	if !ResponseMatchesType(record.Pending.PendingType, response) {
		return ValidatedResponse{}, fmt.Errorf("response type mismatch")
	}
	selected := response.Permissions.PermissionIDs
	granted := map[string]any{}
	seen := map[string]struct{}{}
	for _, permissionID := range selected {
		if _, ok := seen[permissionID]; ok {
			return ValidatedResponse{}, fmt.Errorf("permission id is duplicated")
		}
		seen[permissionID] = struct{}{}
		grant := record.PermissionGrants[permissionID]
		if !grant.Grantable {
			return ValidatedResponse{}, fmt.Errorf("permission id is not grantable")
		}
		mergePermissionGrant(granted, grant)
	}
	payload, ok := appserver.NewPermissionsRequestApprovalResponse(granted, response.Permissions.Scope, response.Permissions.StrictAutoReview)
	if !ok {
		return ValidatedResponse{}, fmt.Errorf("permissions response cannot be mapped")
	}
	resolution := domain.PendingResolutionDenied
	if len(selected) > 0 {
		resolution = domain.PendingResolutionGranted
	}
	return ValidatedResponse{
		Payload:    payload,
		Resolution: resolution,
	}, nil
}

func validateMcpElicitationResponse(record *Record, response domain.PendingResponse, registry *redact.Registry) (ValidatedResponse, error) {
	if !ResponseMatchesType(domain.PendingTypeMcpElicitation, response) {
		return ValidatedResponse{}, fmt.Errorf("response type mismatch")
	}
	payload, ok := appserver.NewMcpElicitationResponsePayloadWithRedaction(*response.McpElicitation, registry)
	if !ok {
		return ValidatedResponse{}, fmt.Errorf("mcp elicitation response cannot be mapped")
	}
	if response.McpElicitation.Action == domain.McpElicitationActionAccept {
		registerMcpSchemaSensitiveValues(registry, payload.Content, record.McpSensitiveFields)
	}
	return ValidatedResponse{
		Payload:    payload,
		Resolution: mcpResolution(response.McpElicitation.Action),
	}, nil
}

func validateToolUserInputResponse(record *Record, response domain.PendingResponse, registry *redact.Registry) (ValidatedResponse, error) {
	if !ResponseMatchesType(record.Pending.PendingType, response) {
		return ValidatedResponse{}, fmt.Errorf("response type mismatch")
	}
	seen := map[string]struct{}{}
	for _, answer := range response.ToolUserInput.Answers {
		if _, ok := seen[answer.QuestionID]; ok {
			return ValidatedResponse{}, fmt.Errorf("tool user input question id is duplicated")
		}
		seen[answer.QuestionID] = struct{}{}
		question, ok := record.ToolQuestions[answer.QuestionID]
		if !ok {
			return ValidatedResponse{}, fmt.Errorf("unknown tool user input question id")
		}
		if len(question.AllowedValues) > 0 && !question.IsOther {
			for _, value := range answer.Answers {
				if _, ok := question.AllowedValues[value]; !ok {
					return ValidatedResponse{}, fmt.Errorf("tool user input answer value is not allowed")
				}
			}
		}
		if question.IsSecret && registry != nil {
			registry.AddMany(answer.Answers...)
		}
	}
	payload, ok := appserver.NewToolUserInputResponsePayload(response.ToolUserInput.Answers)
	if !ok {
		return ValidatedResponse{}, fmt.Errorf("tool user input response cannot be mapped")
	}
	return ValidatedResponse{
		Payload:    payload,
		Resolution: domain.PendingResolutionAnswered,
	}, nil
}

func mergePermissionGrant(target map[string]any, grant PermissionGrant) {
	section := grant.Section
	field := grant.Field
	if section == "" {
		section = grant.Kind
	}
	if field == "" {
		target[section] = cloneJSONValue(grant.Value)
		return
	}
	sectionMap, ok := target[section].(map[string]any)
	if !ok {
		sectionMap = map[string]any{}
		target[section] = sectionMap
	}
	switch existing := sectionMap[field].(type) {
	case nil:
		if field == "entries" || field == "read" || field == "write" {
			sectionMap[field] = []any{cloneJSONValue(grant.Value)}
		} else {
			sectionMap[field] = cloneJSONValue(grant.Value)
		}
	case []any:
		sectionMap[field] = append(existing, cloneJSONValue(grant.Value))
	default:
		sectionMap[field] = cloneJSONValue(grant.Value)
	}
}

func approvalResolution(decision domain.ApprovalWireDecision) domain.PendingResolution {
	switch decision {
	case domain.ApprovalWireDecisionAccept, domain.ApprovalWireDecisionAcceptForSession:
		return domain.PendingResolutionAccepted
	case domain.ApprovalWireDecisionDecline:
		return domain.PendingResolutionDeclined
	case domain.ApprovalWireDecisionCancel:
		return domain.PendingResolutionCancelled
	default:
		return domain.PendingResolutionFailed
	}
}

func mcpResolution(action domain.McpElicitationAction) domain.PendingResolution {
	switch action {
	case domain.McpElicitationActionAccept:
		return domain.PendingResolutionAccepted
	case domain.McpElicitationActionDecline:
		return domain.PendingResolutionDeclined
	case domain.McpElicitationActionCancel:
		return domain.PendingResolutionCancelled
	default:
		return domain.PendingResolutionFailed
	}
}

func registerMcpSchemaSensitiveValues(registry *redact.Registry, raw json.RawMessage, sensitiveFields map[string]struct{}) {
	if registry == nil || len(sensitiveFields) == 0 || len(raw) == 0 {
		return
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return
	}
	registerMcpSchemaSensitiveValue(registry, value, sensitiveFields)
}

func registerMcpSchemaSensitiveValue(registry *redact.Registry, value any, sensitiveFields map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if _, ok := sensitiveFields[strings.ToLower(key)]; ok {
				registerAllScalarValues(registry, child)
				continue
			}
			registerMcpSchemaSensitiveValue(registry, child, sensitiveFields)
		}
	case []any:
		for _, child := range typed {
			registerMcpSchemaSensitiveValue(registry, child, sensitiveFields)
		}
	}
}

func registerAllScalarValues(registry *redact.Registry, value any) {
	switch typed := value.(type) {
	case string:
		registry.Add(typed)
	case json.Number:
		registry.Add(typed.String())
	case float64:
		registry.Add(fmt.Sprint(typed))
	case bool:
		registry.Add(fmt.Sprint(typed))
	case map[string]any:
		for _, child := range typed {
			registerAllScalarValues(registry, child)
		}
	case []any:
		for _, child := range typed {
			registerAllScalarValues(registry, child)
		}
	}
}
