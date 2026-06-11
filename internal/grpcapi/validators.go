package grpcapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"github.com/Dirard/codex-runtime/internal/domain"
)

type SessionGroupResolver interface {
	ResolveSessionGroup(sessionGroupID string) (domain.SessionGroupMetadata, bool)
}

type SessionGroupResolverFunc func(sessionGroupID string) (domain.SessionGroupMetadata, bool)

func (f SessionGroupResolverFunc) ResolveSessionGroup(sessionGroupID string) (domain.SessionGroupMetadata, bool) {
	return f(sessionGroupID)
}

func ValidateStartTask(req *pb.StartTaskRequest, resolver SessionGroupResolver) (domain.StartTaskCommand, *RequestError) {
	if req == nil {
		return domain.StartTaskCommand{}, invalidArgument(domain.ReasonInvalidRequest, "start task request is required")
	}
	sessionGroupID, err := validateRequiredPublicID(req.GetSessionGroupId(), "session_group_id", domain.ReasonInvalidRequest)
	if err != nil {
		return domain.StartTaskCommand{}, err
	}
	clientMessageID, err := validateRequiredPublicID(req.GetClientMessageId(), "client_message_id", domain.ReasonInvalidRequest)
	if err != nil {
		return domain.StartTaskCommand{}, err
	}
	threadID, err := validateOptionalPublicID(req.GetThreadId(), "thread_id")
	if err != nil {
		return domain.StartTaskCommand{}, err
	}
	workspaceID, err := validateOptionalPublicID(req.GetWorkspaceId(), "workspace_id")
	if err != nil {
		return domain.StartTaskCommand{}, err
	}
	metadata, ok := resolveSessionGroup(resolver, sessionGroupID)
	if !ok {
		return domain.StartTaskCommand{}, notFound(domain.ReasonUnknownSessionGroup, "unknown session group")
	}
	if err := validateResolvedPublicID(metadata.WorkspaceID, "resolved workspace_id"); err != nil {
		return domain.StartTaskCommand{}, err
	}
	if err := validateInboundMessageForSession(req, metadata); err != nil {
		return domain.StartTaskCommand{}, err
	}
	if workspaceID != "" && workspaceID != metadata.WorkspaceID {
		return domain.StartTaskCommand{}, &RequestError{
			Code: invalidArgument(domain.ReasonWorkspaceMismatch, "workspace_id does not match session group").Code,
			Details: domain.GatewayErrorDetails{
				Reason:          domain.ReasonWorkspaceMismatch,
				DisplayMessage:  "workspace_id does not match session group",
				SessionGroupID:  sessionGroupID,
				ClientMessageID: clientMessageID,
			},
		}
	}
	if byteLen(req.GetPrompt()) > domain.MaxPromptBytes {
		return domain.StartTaskCommand{}, resourceExhausted("prompt exceeds the maximum size")
	}
	if len(req.GetContextBlocks()) > domain.MaxContextBlocks {
		return domain.StartTaskCommand{}, resourceExhausted("too many context blocks")
	}

	contextBlocks := make([]domain.ContextBlock, 0, len(req.GetContextBlocks()))
	totalContextBytes := 0
	for _, block := range req.GetContextBlocks() {
		domainBlock, blockBytes, err := validateContextBlock(block)
		if err != nil {
			return domain.StartTaskCommand{}, err
		}
		totalContextBytes += blockBytes
		if totalContextBytes > domain.MaxTotalContextBytes {
			return domain.StartTaskCommand{}, resourceExhausted("total context exceeds the maximum size")
		}
		contextBlocks = append(contextBlocks, domainBlock)
	}

	metadataMap, err := validateUICorrelationMetadata(req.GetUiCorrelationMetadata())
	if err != nil {
		return domain.StartTaskCommand{}, err
	}

	return domain.StartTaskCommand{
		SessionGroupID:        sessionGroupID,
		WorkspaceID:           metadata.WorkspaceID,
		Prompt:                req.GetPrompt(),
		ContextBlocks:         contextBlocks,
		ThreadID:              threadID,
		ClientMessageID:       clientMessageID,
		UICorrelationMetadata: metadataMap,
	}, nil
}

func resolveSessionGroup(resolver SessionGroupResolver, sessionGroupID string) (domain.SessionGroupMetadata, bool) {
	if resolver == nil {
		return domain.SessionGroupMetadata{}, false
	}
	return resolver.ResolveSessionGroup(sessionGroupID)
}

func validateRequiredPublicID(rawID string, field string, reason domain.GatewayErrorReason) (string, *RequestError) {
	if err := validatePublicIDCap(rawID, field); err != nil {
		return "", err
	}
	id := strings.TrimSpace(rawID)
	if id == "" {
		return "", invalidArgument(reason, field+" is required")
	}
	if id != rawID {
		return "", invalidArgument(reason, field+" must not have leading or trailing whitespace")
	}
	if err := validatePublicIDCap(id, field); err != nil {
		return "", err
	}
	return rawID, nil
}

func validateOptionalPublicID(rawID string, field string) (string, *RequestError) {
	if err := validatePublicIDCap(rawID, field); err != nil {
		return "", err
	}
	if rawID == "" {
		return "", nil
	}
	id := strings.TrimSpace(rawID)
	if id == "" || id != rawID {
		return "", invalidArgument(domain.ReasonInvalidRequest, field+" must not have leading or trailing whitespace")
	}
	if err := validatePublicIDCap(id, field); err != nil {
		return "", err
	}
	return rawID, nil
}

func validatePublicIDCap(id string, field string) *RequestError {
	if byteLen(id) > domain.MaxPublicIDBytes {
		return resourceExhausted(field + " exceeds the maximum size")
	}
	return nil
}

func validateResolvedPublicID(id string, field string) *RequestError {
	if err := validatePublicIDCap(id, field); err != nil {
		return err
	}
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" || trimmedID != id {
		return invalidArgument(domain.ReasonInvalidRequest, field+" is invalid")
	}
	if err := validatePublicIDCap(trimmedID, field); err != nil {
		return err
	}
	return nil
}

func validateContextBlock(block *pb.ContextBlock) (domain.ContextBlock, int, *RequestError) {
	if block == nil {
		return domain.ContextBlock{}, 0, invalidArgument(domain.ReasonInvalidRequest, "context block is required")
	}
	kind, ok := ContextBlockKindFromProto(block.GetKind())
	if !ok {
		return domain.ContextBlock{}, 0, invalidArgument(domain.ReasonInvalidEnum, "context block kind is invalid")
	}
	content := block.GetContent()
	if byteLen(content) > domain.MaxContextBlockContentBytes {
		return domain.ContextBlock{}, 0, resourceExhausted("context block content exceeds the maximum size")
	}
	if err := validateSourceContentLines(content); err != nil {
		return domain.ContextBlock{}, 0, err
	}
	rawSourceLabel := block.GetSourceLabel()
	if byteLen(rawSourceLabel) > domain.MaxSourceLabelBytes {
		return domain.ContextBlock{}, 0, resourceExhausted("context block source_label exceeds the maximum size")
	}
	sourceLabel := strings.TrimSpace(rawSourceLabel)
	if sourceLabel == "" {
		return domain.ContextBlock{}, 0, invalidArgument(domain.ReasonInvalidRequest, "context block source_label is required")
	}
	if byteLen(sourceLabel) > domain.MaxSourceLabelBytes {
		return domain.ContextBlock{}, 0, resourceExhausted("context block source_label exceeds the maximum size")
	}
	if byteLen(block.GetSourceUri()) > domain.MaxSourceURIBytes {
		return domain.ContextBlock{}, 0, resourceExhausted("context block source_uri exceeds the maximum size")
	}
	if block.GetSourceUri() != "" && !isSafeSourceURI(block.GetSourceUri()) {
		return domain.ContextBlock{}, 0, invalidArgument(domain.ReasonInvalidRequest, "context block source_uri is invalid")
	}
	if byteLen(block.GetMimeType()) > domain.MaxMimeTypeBytes {
		return domain.ContextBlock{}, 0, resourceExhausted("context block mime_type exceeds the maximum size")
	}
	return domain.ContextBlock{
		Kind:        kind,
		SourceLabel: sourceLabel,
		SourceURI:   block.GetSourceUri(),
		MimeType:    block.GetMimeType(),
		Content:     content,
	}, byteLen(content), nil
}

func isSafeSourceURI(rawURI string) bool {
	if strings.ContainsFunc(rawURI, unicode.IsSpace) {
		return false
	}
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return false
	}
	return true
}

func validateUICorrelationMetadata(metadata map[string]string) (map[string]string, *RequestError) {
	if len(metadata) > domain.MaxUICorrelationMetadataEntries {
		return nil, resourceExhausted("ui correlation metadata has too many entries")
	}
	validated := make(map[string]string, len(metadata))
	for key, value := range metadata {
		if strings.TrimSpace(key) == "" {
			return nil, invalidArgument(domain.ReasonInvalidRequest, "ui correlation metadata key is required")
		}
		if byteLen(key) > domain.MaxUICorrelationMetadataKey {
			return nil, resourceExhausted("ui correlation metadata key exceeds the maximum size")
		}
		if byteLen(value) > domain.MaxUICorrelationMetadataValue {
			return nil, resourceExhausted("ui correlation metadata value exceeds the maximum size")
		}
		validated[key] = value
	}
	return validated, nil
}

func ValidateStreamTask(req *pb.StreamTaskRequest) (domain.StreamTaskCommand, *RequestError) {
	if req == nil {
		return domain.StreamTaskCommand{}, invalidArgument(domain.ReasonInvalidRequest, "stream task request is required")
	}
	taskID, err := validateRequiredPublicID(req.GetTaskId(), "task_id", domain.ReasonInvalidLocator)
	if err != nil {
		return domain.StreamTaskCommand{}, err
	}
	clientSubscriberID, err := validateOptionalPublicID(req.GetClientSubscriberId(), "client_subscriber_id")
	if err != nil {
		return domain.StreamTaskCommand{}, err
	}
	command := domain.StreamTaskCommand{
		TaskID:             taskID,
		ClientSubscriberID: clientSubscriberID,
	}
	switch cursor := req.GetCursor().(type) {
	case *pb.StreamTaskRequest_FromStart:
		if cursor.FromStart == nil {
			return domain.StreamTaskCommand{}, invalidArgument(domain.ReasonInvalidCursor, "from_start cursor is required")
		}
		command.CursorKind = domain.StreamCursorFromStart
		return command, nil
	case *pb.StreamTaskRequest_AfterEventId:
		command.CursorKind = domain.StreamCursorAfterEventID
		command.AfterEventID = cursor.AfterEventId
		return command, nil
	case nil:
		return domain.StreamTaskCommand{}, invalidArgument(domain.ReasonInvalidCursor, "stream cursor is required")
	default:
		return domain.StreamTaskCommand{}, invalidArgument(domain.ReasonInvalidCursor, "stream cursor is invalid")
	}
}

func ValidateGetTaskStatus(req *pb.GetTaskStatusRequest) (domain.GetTaskStatusCommand, *RequestError) {
	if req == nil {
		return domain.GetTaskStatusCommand{}, invalidArgument(domain.ReasonInvalidRequest, "get status request is required")
	}
	locator, err := taskLocatorFromProto(req.GetLocator(), true)
	if err != nil {
		return domain.GetTaskStatusCommand{}, err
	}
	return domain.GetTaskStatusCommand{Locator: locator}, nil
}

func ValidateInterruptTask(req *pb.InterruptTaskRequest) (domain.InterruptTaskCommand, *RequestError) {
	if req == nil {
		return domain.InterruptTaskCommand{}, invalidArgument(domain.ReasonInvalidRequest, "interrupt task request is required")
	}
	locator, err := interruptLocatorFromProto(req.GetLocator())
	if err != nil {
		return domain.InterruptTaskCommand{}, err
	}
	clientRequestID, err := validateOptionalPublicID(req.GetClientRequestId(), "client_request_id")
	if err != nil {
		return domain.InterruptTaskCommand{}, err
	}
	return domain.InterruptTaskCommand{
		Locator:         locator,
		ClientRequestID: clientRequestID,
	}, nil
}

func ValidateRespondPendingRequest(req *pb.RespondPendingRequestRequest) (domain.RespondPendingRequestCommand, *RequestError) {
	if req == nil {
		return domain.RespondPendingRequestCommand{}, invalidArgument(domain.ReasonInvalidRequest, "pending response request is required")
	}
	taskID, err := validateRequiredPublicID(req.GetTaskId(), "task_id", domain.ReasonInvalidLocator)
	if err != nil {
		return domain.RespondPendingRequestCommand{}, err
	}
	pendingRequestID, err := validateRequiredPublicID(req.GetPendingRequestId(), "pending_request_id", domain.ReasonInvalidLocator)
	if err != nil {
		return domain.RespondPendingRequestCommand{}, err
	}
	clientResponseID, err := validateRequiredPublicID(req.GetClientResponseId(), "client_response_id", domain.ReasonInvalidRequest)
	if err != nil {
		return domain.RespondPendingRequestCommand{}, err
	}
	response, err := pendingResponseFromProto(req.GetResponse())
	if err != nil {
		return domain.RespondPendingRequestCommand{}, err
	}
	return domain.RespondPendingRequestCommand{
		TaskID:           taskID,
		PendingRequestID: pendingRequestID,
		ClientResponseID: clientResponseID,
		Response:         response,
	}, nil
}

func taskLocatorFromProto(locator any, allowThreadLocator bool) (domain.TaskLocator, *RequestError) {
	switch typed := locator.(type) {
	case *pb.GetTaskStatusRequest_TaskId:
		taskID, err := validateRequiredPublicID(typed.TaskId, "task_id", domain.ReasonInvalidLocator)
		if err != nil {
			return domain.TaskLocator{}, err
		}
		return domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: taskID}, nil
	case *pb.GetTaskStatusRequest_ClientMessageLocator:
		clientLocator, err := clientMessageLocatorFromProto(typed.ClientMessageLocator)
		if err != nil {
			return domain.TaskLocator{}, err
		}
		return domain.TaskLocator{Kind: domain.TaskLocatorByClientMessage, ClientMessageLocator: clientLocator}, nil
	case *pb.GetTaskStatusRequest_ThreadLocator:
		if !allowThreadLocator {
			return domain.TaskLocator{}, invalidArgument(domain.ReasonInvalidLocator, "thread locator is not allowed")
		}
		threadLocator, err := threadLocatorFromProto(typed.ThreadLocator)
		if err != nil {
			return domain.TaskLocator{}, err
		}
		return domain.TaskLocator{Kind: domain.TaskLocatorByThread, ThreadLocator: threadLocator}, nil
	case nil:
		return domain.TaskLocator{}, invalidArgument(domain.ReasonInvalidLocator, "task locator is required")
	default:
		return domain.TaskLocator{}, invalidArgument(domain.ReasonInvalidLocator, "task locator is invalid")
	}
}

func interruptLocatorFromProto(locator any) (domain.TaskLocator, *RequestError) {
	switch typed := locator.(type) {
	case *pb.InterruptTaskRequest_TaskId:
		taskID, err := validateRequiredPublicID(typed.TaskId, "task_id", domain.ReasonInvalidLocator)
		if err != nil {
			return domain.TaskLocator{}, err
		}
		return domain.TaskLocator{Kind: domain.TaskLocatorByTaskID, TaskID: taskID}, nil
	case *pb.InterruptTaskRequest_ClientMessageLocator:
		clientLocator, err := clientMessageLocatorFromProto(typed.ClientMessageLocator)
		if err != nil {
			return domain.TaskLocator{}, err
		}
		return domain.TaskLocator{Kind: domain.TaskLocatorByClientMessage, ClientMessageLocator: clientLocator}, nil
	case nil:
		return domain.TaskLocator{}, invalidArgument(domain.ReasonInvalidLocator, "interrupt locator is required")
	default:
		return domain.TaskLocator{}, invalidArgument(domain.ReasonInvalidLocator, "interrupt locator is invalid")
	}
}

func clientMessageLocatorFromProto(locator *pb.ClientMessageTaskLocator) (domain.ClientMessageTaskLocator, *RequestError) {
	if locator == nil {
		return domain.ClientMessageTaskLocator{}, invalidArgument(domain.ReasonInvalidLocator, "client message locator is required")
	}
	sessionGroupID, err := validateRequiredPublicID(locator.GetSessionGroupId(), "session_group_id", domain.ReasonInvalidLocator)
	if err != nil {
		return domain.ClientMessageTaskLocator{}, err
	}
	clientMessageID, err := validateRequiredPublicID(locator.GetClientMessageId(), "client_message_id", domain.ReasonInvalidLocator)
	if err != nil {
		return domain.ClientMessageTaskLocator{}, err
	}
	return domain.ClientMessageTaskLocator{
		SessionGroupID:  sessionGroupID,
		ClientMessageID: clientMessageID,
	}, nil
}

func threadLocatorFromProto(locator *pb.ThreadTaskLocator) (domain.ThreadTaskLocator, *RequestError) {
	if locator == nil {
		return domain.ThreadTaskLocator{}, invalidArgument(domain.ReasonInvalidLocator, "thread locator is required")
	}
	sessionGroupID, err := validateRequiredPublicID(locator.GetSessionGroupId(), "session_group_id", domain.ReasonInvalidLocator)
	if err != nil {
		return domain.ThreadTaskLocator{}, err
	}
	threadID, err := validateRequiredPublicID(locator.GetThreadId(), "thread_id", domain.ReasonInvalidLocator)
	if err != nil {
		return domain.ThreadTaskLocator{}, err
	}
	return domain.ThreadTaskLocator{
		SessionGroupID: sessionGroupID,
		ThreadID:       threadID,
	}, nil
}

func pendingResponseFromProto(response any) (domain.PendingResponse, *RequestError) {
	switch typed := response.(type) {
	case *pb.RespondPendingRequestRequest_Approval:
		if typed.Approval == nil {
			return domain.PendingResponse{}, invalidArgument(domain.ReasonInvalidRequest, "approval decision_id is required")
		}
		decisionID, err := validateRequiredPublicID(typed.Approval.GetDecisionId(), "approval decision_id", domain.ReasonInvalidRequest)
		if err != nil {
			return domain.PendingResponse{}, err
		}
		return domain.PendingResponse{Approval: &domain.ApprovalPendingResponse{DecisionID: decisionID}}, nil
	case *pb.RespondPendingRequestRequest_Permissions:
		return permissionsResponseFromProto(typed.Permissions)
	case *pb.RespondPendingRequestRequest_McpElicitation:
		return mcpElicitationResponseFromProto(typed.McpElicitation)
	case *pb.RespondPendingRequestRequest_ToolUserInput:
		return toolUserInputResponseFromProto(typed.ToolUserInput)
	case nil:
		return domain.PendingResponse{}, invalidArgument(domain.ReasonResponseTypeMismatch, "pending response payload is required")
	default:
		return domain.PendingResponse{}, invalidArgument(domain.ReasonResponseTypeMismatch, "pending response payload is invalid")
	}
}

func permissionsResponseFromProto(response *pb.PermissionsPendingResponse) (domain.PendingResponse, *RequestError) {
	if response == nil {
		return domain.PendingResponse{}, invalidArgument(domain.ReasonResponseTypeMismatch, "permissions response is required")
	}
	scope, ok := PermissionScopeFromProto(response.GetScope())
	if response.GetScope() != pb.PermissionScope_PERMISSION_SCOPE_UNSPECIFIED && !ok {
		return domain.PendingResponse{}, invalidArgument(domain.ReasonInvalidEnum, "permission scope is invalid")
	}
	if len(response.GetPermissionIds()) > domain.MaxPermissionAtoms {
		return domain.PendingResponse{}, resourceExhausted("too many permission ids")
	}
	ids := make([]string, 0, len(response.GetPermissionIds()))
	seenIDs := make(map[string]struct{}, len(response.GetPermissionIds()))
	for _, rawID := range response.GetPermissionIds() {
		id, err := validateRequiredPublicID(rawID, "permission_id", domain.ReasonInvalidRequest)
		if err != nil {
			return domain.PendingResponse{}, err
		}
		if _, ok := seenIDs[id]; ok {
			return domain.PendingResponse{}, invalidArgument(domain.ReasonInvalidRequest, "permission_id is duplicated")
		}
		seenIDs[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 && (scope != "" || response.GetStrictAutoReview()) {
		return domain.PendingResponse{}, invalidArgument(domain.ReasonInvalidRequest, "permissions denial must not include scope or strict_auto_review")
	}
	return domain.PendingResponse{Permissions: &domain.PermissionsPendingResponse{
		PermissionIDs:    ids,
		Scope:            scope,
		StrictAutoReview: response.GetStrictAutoReview(),
	}}, nil
}

func mcpElicitationResponseFromProto(response *pb.McpElicitationPendingResponse) (domain.PendingResponse, *RequestError) {
	if response == nil {
		return domain.PendingResponse{}, invalidArgument(domain.ReasonResponseTypeMismatch, "mcp elicitation response is required")
	}
	action, ok := McpElicitationActionFromProto(response.GetAction())
	if !ok {
		return domain.PendingResponse{}, invalidArgument(domain.ReasonInvalidEnum, "mcp elicitation action is invalid")
	}
	contentJSON := response.GetContentJson()
	if byteLen(contentJSON) > domain.MaxMcpElicitationContentJSONBytes {
		return domain.PendingResponse{}, resourceExhausted("mcp elicitation content exceeds the maximum size")
	}
	switch action {
	case domain.McpElicitationActionAccept:
		if err := validateCompleteJSONDepth(contentJSON, domain.MaxMcpElicitationContentJSONDepth); err != nil {
			return domain.PendingResponse{}, err
		}
	case domain.McpElicitationActionDecline, domain.McpElicitationActionCancel:
		if contentJSON != "" {
			return domain.PendingResponse{}, invalidArgument(domain.ReasonInvalidRequest, "mcp elicitation content_json must be empty unless action is accept")
		}
	}
	return domain.PendingResponse{McpElicitation: &domain.McpElicitationPendingResponse{
		Action:      action,
		ContentJSON: contentJSON,
	}}, nil
}

func toolUserInputResponseFromProto(response *pb.ToolUserInputPendingResponse) (domain.PendingResponse, *RequestError) {
	if response == nil {
		return domain.PendingResponse{}, invalidArgument(domain.ReasonResponseTypeMismatch, "tool user input response is required")
	}
	if len(response.GetAnswers()) > domain.MaxToolUserInputQuestions {
		return domain.PendingResponse{}, resourceExhausted("too many tool user input answers")
	}
	answers := make([]domain.ToolUserInputAnswer, 0, len(response.GetAnswers()))
	seenQuestionIDs := make(map[string]struct{}, len(response.GetAnswers()))
	totalBytes := 0
	for _, answer := range response.GetAnswers() {
		questionID, err := validateRequiredPublicID(answer.GetQuestionId(), "tool user input question_id", domain.ReasonInvalidRequest)
		if err != nil {
			return domain.PendingResponse{}, err
		}
		if _, ok := seenQuestionIDs[questionID]; ok {
			return domain.PendingResponse{}, invalidArgument(domain.ReasonInvalidRequest, "tool user input question_id is duplicated")
		}
		seenQuestionIDs[questionID] = struct{}{}
		if len(answer.GetAnswers()) > domain.MaxToolUserInputAnswersPerQuestion {
			return domain.PendingResponse{}, resourceExhausted("too many answers for tool user input question")
		}
		values := make([]string, 0, len(answer.GetAnswers()))
		for _, value := range answer.GetAnswers() {
			valueBytes := byteLen(value)
			if valueBytes > domain.MaxToolUserInputAnswerValueBytes {
				return domain.PendingResponse{}, resourceExhausted("tool user input answer exceeds the maximum size")
			}
			totalBytes += valueBytes
			if totalBytes > domain.MaxToolUserInputTotalAnswerBytes {
				return domain.PendingResponse{}, resourceExhausted("tool user input answers exceed the maximum total size")
			}
			values = append(values, value)
		}
		answers = append(answers, domain.ToolUserInputAnswer{
			QuestionID: questionID,
			Answers:    values,
		})
	}
	return domain.PendingResponse{ToolUserInput: &domain.ToolUserInputPendingResponse{Answers: answers}}, nil
}

func validateSourceContentLines(content string) *RequestError {
	lineStart := 0
	for index := 0; index < len(content); index++ {
		if content[index] != '\n' {
			continue
		}
		lineEnd := index
		if lineEnd > lineStart && content[lineEnd-1] == '\r' {
			lineEnd--
		}
		if lineEnd-lineStart > domain.MaxContextSourceLineBytes {
			return resourceExhausted("context block source content line exceeds the maximum size")
		}
		lineStart = index + 1
	}
	lineEnd := len(content)
	if lineEnd > lineStart && content[lineEnd-1] == '\r' {
		lineEnd--
	}
	if lineEnd-lineStart > domain.MaxContextSourceLineBytes {
		return resourceExhausted("context block source content line exceeds the maximum size")
	}
	return nil
}

func validateCompleteJSONDepth(rawJSON string, maxDepth int) *RequestError {
	if strings.TrimSpace(rawJSON) == "" {
		return invalidArgument(domain.ReasonInvalidRequest, "mcp elicitation accepted content_json is required")
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(rawJSON))
	if err := decoder.Decode(&value); err != nil {
		return invalidArgument(domain.ReasonInvalidRequest, "mcp elicitation content_json must be valid JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return invalidArgument(domain.ReasonInvalidRequest, "mcp elicitation content_json must contain exactly one JSON value")
	}
	if jsonDepth(value) > maxDepth {
		return resourceExhausted("mcp elicitation content_json exceeds the maximum depth")
	}
	return nil
}

func jsonDepth(value any) int {
	switch typed := value.(type) {
	case map[string]any:
		maxChild := 0
		for _, child := range typed {
			maxChild = max(maxChild, jsonDepth(child))
		}
		return maxChild + 1
	case []any:
		maxChild := 0
		for _, child := range typed {
			maxChild = max(maxChild, jsonDepth(child))
		}
		return maxChild + 1
	default:
		return 1
	}
}

func byteLen(value string) int {
	if !utf8.ValidString(value) {
		return len([]rune(value))
	}
	return len(value)
}
