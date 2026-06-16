package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

const idempotencyFingerprintSchemaVersionV1 = 1

type startTaskFingerprintPayloadV1 struct {
	SchemaVersion  int                              `json:"schema_version"`
	SessionGroupID string                           `json:"session_group_id"`
	WorkspaceID    *string                          `json:"workspace_id"`
	ThreadID       *string                          `json:"thread_id"`
	UserPrompt     string                           `json:"user_prompt"`
	ContextBlocks  []startTaskContextBlockPayloadV1 `json:"context_blocks"`
}

type startTaskContextBlockPayloadV1 struct {
	Kind        ContextBlockKind `json:"kind"`
	SourceLabel string           `json:"source_label"`
	SourceURI   *string          `json:"source_uri"`
	MimeType    *string          `json:"mime_type"`
	Content     string           `json:"content"`
}

type pendingResponseFingerprintPayloadV1 struct {
	SchemaVersion    int         `json:"schema_version"`
	TaskID           string      `json:"task_id"`
	PendingRequestID string      `json:"pending_request_id"`
	PendingType      PendingType `json:"pending_type"`
	Response         any         `json:"response"`
}

type pendingApprovalResponsePayloadV1 struct {
	DecisionID string `json:"decision_id"`
}

type pendingPermissionsResponsePayloadV1 struct {
	PermissionIDs    []string         `json:"permission_ids"`
	Scope            *PermissionScope `json:"scope"`
	StrictAutoReview bool             `json:"strict_auto_review"`
}

type pendingMcpElicitationResponsePayloadV1 struct {
	Action  McpElicitationAction `json:"action"`
	Content any                  `json:"content"`
}

type pendingToolUserInputResponsePayloadV1 struct {
	Answers map[string]pendingToolUserInputAnswerPayloadV1 `json:"answers"`
}

type pendingToolUserInputAnswerPayloadV1 struct {
	Answers []string `json:"answers"`
}

func StartTaskFingerprintV1CanonicalJSON(command StartTaskCommand) ([]byte, error) {
	contextBlocks := make([]startTaskContextBlockPayloadV1, 0, len(command.ContextBlocks))
	for _, block := range command.ContextBlocks {
		contextBlocks = append(contextBlocks, startTaskContextBlockPayloadV1{
			Kind:        block.Kind,
			SourceLabel: block.SourceLabel,
			SourceURI:   nullableString(block.SourceURI),
			MimeType:    nullableString(block.MimeType),
			Content:     block.Content,
		})
	}

	return canonicalJSON(startTaskFingerprintPayloadV1{
		SchemaVersion:  idempotencyFingerprintSchemaVersionV1,
		SessionGroupID: command.SessionGroupID,
		WorkspaceID:    nullableString(command.WorkspaceID),
		ThreadID:       nullableString(command.ThreadID),
		UserPrompt:     command.Prompt,
		ContextBlocks:  contextBlocks,
	})
}

func StartTaskFingerprintV1SHA256Hex(command StartTaskCommand) (string, error) {
	canonicalJSON, err := StartTaskFingerprintV1CanonicalJSON(command)
	if err != nil {
		return "", err
	}
	return sha256Hex(canonicalJSON), nil
}

func PendingResponseFingerprintV1CanonicalJSON(
	taskID string,
	pendingRequestID string,
	pendingType PendingType,
	response PendingResponse,
) ([]byte, error) {
	payload, err := pendingResponsePayloadV1(pendingType, response)
	if err != nil {
		return nil, err
	}
	return canonicalJSON(pendingResponseFingerprintPayloadV1{
		SchemaVersion:    idempotencyFingerprintSchemaVersionV1,
		TaskID:           taskID,
		PendingRequestID: pendingRequestID,
		PendingType:      pendingType,
		Response:         payload,
	})
}

func PendingResponseFingerprintV1SHA256Hex(
	taskID string,
	pendingRequestID string,
	pendingType PendingType,
	response PendingResponse,
) (string, error) {
	canonicalJSON, err := PendingResponseFingerprintV1CanonicalJSON(taskID, pendingRequestID, pendingType, response)
	if err != nil {
		return "", err
	}
	return sha256Hex(canonicalJSON), nil
}

func pendingResponsePayloadV1(pendingType PendingType, response PendingResponse) (any, error) {
	switch pendingType {
	case PendingTypeCommandApproval, PendingTypeFileChangeApproval:
		if response.Approval == nil {
			return nil, fmt.Errorf("%s fingerprint requires approval response", pendingType)
		}
		return pendingApprovalResponsePayloadV1{DecisionID: response.Approval.DecisionID}, nil
	case PendingTypePermissionsApproval:
		if response.Permissions == nil {
			return nil, fmt.Errorf("%s fingerprint requires permissions response", pendingType)
		}
		permissionIDs := append([]string(nil), response.Permissions.PermissionIDs...)
		sort.Strings(permissionIDs)

		var scope *PermissionScope
		switch response.Permissions.Scope {
		case "":
		case PermissionScopeTurn, PermissionScopeSession:
			scope = &response.Permissions.Scope
		default:
			return nil, fmt.Errorf("unsupported permission scope %q", response.Permissions.Scope)
		}
		return pendingPermissionsResponsePayloadV1{
			PermissionIDs:    permissionIDs,
			Scope:            scope,
			StrictAutoReview: response.Permissions.StrictAutoReview,
		}, nil
	case PendingTypeMcpElicitation:
		if response.McpElicitation == nil {
			return nil, fmt.Errorf("%s fingerprint requires mcp elicitation response", pendingType)
		}
		return pendingMcpElicitationPayloadV1(*response.McpElicitation)
	case PendingTypeToolUserInput:
		if response.ToolUserInput == nil {
			return nil, fmt.Errorf("%s fingerprint requires tool user input response", pendingType)
		}
		return pendingToolUserInputPayloadV1(*response.ToolUserInput)
	default:
		return nil, fmt.Errorf("unsupported pending response type %q", pendingType)
	}
}

func pendingMcpElicitationPayloadV1(response McpElicitationPendingResponse) (pendingMcpElicitationResponsePayloadV1, error) {
	switch response.Action {
	case McpElicitationActionAccept:
		var content any
		decoder := json.NewDecoder(bytes.NewReader([]byte(response.ContentJSON)))
		decoder.UseNumber()
		if err := decoder.Decode(&content); err != nil {
			return pendingMcpElicitationResponsePayloadV1{}, fmt.Errorf("mcp elicitation accept content must be valid JSON: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return pendingMcpElicitationResponsePayloadV1{}, fmt.Errorf("mcp elicitation accept content must contain exactly one JSON value")
		}
		return pendingMcpElicitationResponsePayloadV1{
			Action:  McpElicitationActionAccept,
			Content: content,
		}, nil
	case McpElicitationActionDecline, McpElicitationActionCancel:
		if response.ContentJSON != "" {
			return pendingMcpElicitationResponsePayloadV1{}, fmt.Errorf("mcp elicitation %s response must not include content", response.Action)
		}
		return pendingMcpElicitationResponsePayloadV1{
			Action:  response.Action,
			Content: nil,
		}, nil
	default:
		return pendingMcpElicitationResponsePayloadV1{}, fmt.Errorf("unsupported mcp elicitation action %q", response.Action)
	}
}

func pendingToolUserInputPayloadV1(response ToolUserInputPendingResponse) (pendingToolUserInputResponsePayloadV1, error) {
	payload := pendingToolUserInputResponsePayloadV1{
		Answers: make(map[string]pendingToolUserInputAnswerPayloadV1, len(response.Answers)),
	}
	for _, answer := range response.Answers {
		if _, exists := payload.Answers[answer.QuestionID]; exists {
			return pendingToolUserInputResponsePayloadV1{}, fmt.Errorf("duplicate tool user input question id %q", answer.QuestionID)
		}
		payload.Answers[answer.QuestionID] = pendingToolUserInputAnswerPayloadV1{
			Answers: append([]string(nil), answer.Answers...),
		}
	}
	return payload, nil
}

func canonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	encoded := bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))
	return append([]byte(nil), encoded...), nil
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	copied := value
	return &copied
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
