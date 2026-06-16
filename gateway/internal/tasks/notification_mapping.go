package tasks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Dirard/codex-runtime/gateway/internal/appserver"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	"github.com/Dirard/codex-runtime/gateway/internal/redact"
)

const (
	gatewayWarningCodeAppServerError       = "app_server_error"
	gatewayWarningCodeAppServerWarning     = "app_server_warning"
	gatewayWarningCodeConfigWarning        = "config_warning"
	gatewayWarningCodeModelRerouted        = "model_rerouted"
	gatewayWarningCodeModelVerification    = "model_verification"
	gatewayWarningCodeWorldWritableWarning = "world_writable_warning"
	gatewayWarningCodePlanDelta            = "plan_delta"
	gatewayWarningCodeUnsupportedItem      = "unsupported_item"
	gatewayWarningCodeUnknownNotification  = "unknown_notification"
	gatewayDiagnosticCodeStructuredDrop    = "structured_drop"
	gatewayDiagnosticCodeFilesystemChanged = "filesystem_changed"
	gatewayDiagnosticCodeDiagnosticOnly    = "diagnostic_only_notification"
	gatewayDiagnosticCodeUnknownEvent      = "unknown_notification"
)

type warningCorrelationPolicy int

const (
	warningCorrelationConnectionDiagnostic warningCorrelationPolicy = iota
	warningCorrelationExactTurn
)

func (s *Service) handleMappedNotification(notification appserver.Notification, connection *appserver.Connection) {
	root := decodeNotificationObject(notification.Params)
	switch notification.Method {
	case "turn/plan/updated":
		s.publishTurnNotification(notification, connection, appserver.ParseThreadID(notification.Params), appserver.ParseTurnID(notification.Params), planUpdatedEvent(root))
	case "turn/diff/updated":
		s.publishTurnNotification(notification, connection, appserver.ParseThreadID(notification.Params), appserver.ParseTurnID(notification.Params), turnDiffUpdatedEvent(root))
	case "item/started", "item/completed", "item/agentMessage/delta", "item/plan/delta",
		"item/commandExecution/outputDelta", "item/fileChange/patchUpdated", "item/mcpToolCall/progress":
		s.handleItemNotification(notification, connection, root)
	case "error":
		s.publishWarningNotification(notification, connection, appserver.ParseThreadID(notification.Params), appserver.ParseTurnID(notification.Params), warningCorrelationConnectionDiagnostic, domain.GatewayWarningEvent{
			Code:    gatewayWarningCodeAppServerError,
			Message: rawMessageOrFallback(root, "app-server error", "error.message", "message"),
		})
	case "warning":
		s.publishWarningNotification(notification, connection, appserver.ParseThreadID(notification.Params), appserver.ParseTurnID(notification.Params), warningCorrelationConnectionDiagnostic, domain.GatewayWarningEvent{
			Code:    gatewayWarningCodeAppServerWarning,
			Message: rawMessageOrFallback(root, "app-server warning", "message", "warning.message"),
		})
	case "configWarning":
		s.publishWarningNotification(notification, connection, appserver.ParseThreadID(notification.Params), appserver.ParseTurnID(notification.Params), warningCorrelationConnectionDiagnostic, domain.GatewayWarningEvent{
			Code:    gatewayWarningCodeConfigWarning,
			Message: rawMessageOrFallback(root, "configuration warning", "summary", "message", "details"),
		})
	case "model/rerouted":
		s.publishWarningNotification(notification, connection, appserver.ParseThreadID(notification.Params), appserver.ParseTurnID(notification.Params), warningCorrelationExactTurn, domain.GatewayWarningEvent{
			Code:    gatewayWarningCodeModelRerouted,
			Message: rawMessageOrFallback(root, "model request was rerouted", "message", "summary", "reason"),
		})
	case "model/verification":
		s.publishWarningNotification(notification, connection, appserver.ParseThreadID(notification.Params), appserver.ParseTurnID(notification.Params), warningCorrelationExactTurn, domain.GatewayWarningEvent{
			Code:    gatewayWarningCodeModelVerification,
			Message: rawMessageOrFallback(root, "model verification update", "message", "summary", "status"),
		})
	case "windows/worldWritableWarning":
		s.publishWarningNotification(notification, connection, appserver.ParseThreadID(notification.Params), appserver.ParseTurnID(notification.Params), warningCorrelationConnectionDiagnostic, domain.GatewayWarningEvent{
			Code:    gatewayWarningCodeWorldWritableWarning,
			Message: worldWritableWarningMessage(root),
		})
	case "account/updated", "account/rateLimits/updated", "account/login/completed":
		s.recordNotificationDiagnostic(notification, connection, gatewayDiagnosticCodeStructuredDrop)
		return
	case "fs/changed":
		s.recordNotificationDiagnostic(notification, connection, gatewayDiagnosticCodeFilesystemChanged)
		return
	default:
		if redact.StructuredDrop(notification.Method) {
			s.recordNotificationDiagnostic(notification, connection, gatewayDiagnosticCodeStructuredDrop)
			return
		}
		if diagnosticOnlyNotification(notification.Method) {
			s.recordNotificationDiagnostic(notification, connection, gatewayDiagnosticCodeDiagnosticOnly)
			return
		}
		s.publishUnknownNotification(notification, connection, root)
	}
}

func (s *Service) recordNotificationDiagnostic(notification appserver.Notification, connection *appserver.Connection, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordNotificationDiagnosticLocked(notification, connection, code)
}

func (s *Service) recordNotificationDiagnosticLocked(notification appserver.Notification, connection *appserver.Connection, code string) {
	sessionGroupID := notification.SessionGroupID
	if sessionGroupID == "" && connection != nil {
		sessionGroupID = connection.SessionGroupID()
	}
	s.recordConnectionDiagnosticWithRedactorLocked(connectionDiagnostic{
		SessionGroupID: sessionGroupID,
		TaskID:         notification.TaskID,
		Code:           code,
		Method:         notification.Method,
	}, connectionDiagnosticRedactor(connection))
}

func diagnosticOnlyNotification(method string) bool {
	switch method {
	case "thread/status/changed",
		"thread/archived",
		"thread/unarchived",
		"thread/closed",
		"skills/changed",
		"thread/name/updated",
		"thread/goal/updated",
		"thread/goal/cleared",
		"thread/settings/updated",
		"thread/tokenUsage/updated",
		"hook/started",
		"hook/completed",
		"item/autoApprovalReview/started",
		"item/autoApprovalReview/completed",
		"rawResponseItem/completed",
		"command/exec/outputDelta",
		"process/outputDelta",
		"process/exited",
		"item/commandExecution/terminalInteraction",
		"item/fileChange/outputDelta",
		"mcpServer/oauthLogin/completed",
		"mcpServer/startupStatus/updated",
		"app/list/updated",
		"remoteControl/status/changed",
		"externalAgentConfig/import/completed",
		"item/reasoning/summaryTextDelta",
		"item/reasoning/summaryPartAdded",
		"item/reasoning/textDelta",
		"thread/compacted",
		"guardianWarning",
		"deprecationNotice",
		"fuzzyFileSearch/sessionUpdated",
		"fuzzyFileSearch/sessionCompleted",
		"thread/realtime/started",
		"thread/realtime/itemAdded",
		"thread/realtime/transcript/delta",
		"thread/realtime/transcript/done",
		"thread/realtime/outputAudio/delta",
		"thread/realtime/sdp",
		"thread/realtime/error",
		"thread/realtime/closed",
		"windowsSandbox/setupCompleted",
		"serverRequest/resolved":
		return true
	default:
		return false
	}
}

func rawMessageOrFallback(root map[string]any, fallback string, paths ...string) string {
	if message := firstString(root, paths...); message != "" {
		return message
	}
	return fallback
}

func (s *Service) handleItemNotification(notification appserver.Notification, connection *appserver.Connection, root map[string]any) {
	itemID := notificationItemID(root)
	threadID := appserver.ParseThreadID(notification.Params)
	turnID := appserver.ParseTurnID(notification.Params)

	s.mu.Lock()
	task := s.taskForItemNotificationLocked(notification, connection, threadID, turnID, itemID)
	if task == nil || isTerminalState(task.state) {
		s.mu.Unlock()
		return
	}
	payloads := s.itemNotificationPayloadsLocked(task, notification.Method, root, itemID)
	if len(payloads) == 0 {
		s.mu.Unlock()
		return
	}
	publications := s.appendPublicationsLocked(task, payloads, false)
	s.mu.Unlock()
	s.publishPublications(publications)
}

func (s *Service) publishTurnNotification(
	notification appserver.Notification,
	connection *appserver.Connection,
	threadID string,
	turnID string,
	payload domain.TaskEventPayload,
) {
	if payload == nil {
		return
	}

	s.mu.Lock()
	task := s.taskForExactTurnNotificationLocked(notification, connection, threadID, turnID)
	if task == nil || isTerminalState(task.state) {
		s.mu.Unlock()
		return
	}
	payload = s.redactPayloadLocked(task, payload)
	event, subscribers := s.appendEventLocked(task, payload)
	s.mu.Unlock()
	s.publishEvent(task.id, subscribers, event, false)
}

func (s *Service) publishWarningNotification(
	notification appserver.Notification,
	connection *appserver.Connection,
	threadID string,
	turnID string,
	policy warningCorrelationPolicy,
	warning domain.GatewayWarningEvent,
) {
	if warning.Message == "" {
		return
	}

	s.mu.Lock()
	task := s.taskForWarningNotificationLocked(notification, connection, threadID, turnID, policy)
	if task == nil || isTerminalState(task.state) {
		s.recordNotificationDiagnosticLocked(notification, connection, warning.Code)
		s.mu.Unlock()
		return
	}
	warning = s.redactPayloadLocked(task, warning).(domain.GatewayWarningEvent)
	event, subscribers := s.appendEventLocked(task, warning)
	s.mu.Unlock()
	s.publishEvent(task.id, subscribers, event, false)
}

func (s *Service) publishUnknownNotification(notification appserver.Notification, connection *appserver.Connection, root map[string]any) {
	itemID := notificationItemID(root)
	threadID := appserver.ParseThreadID(notification.Params)
	turnID := appserver.ParseTurnID(notification.Params)

	s.mu.Lock()
	task := s.taskForUnknownNotificationLocked(notification, connection, threadID, turnID, itemID)
	if task == nil || isTerminalState(task.state) {
		s.recordNotificationDiagnosticLocked(notification, connection, gatewayDiagnosticCodeUnknownEvent)
		s.mu.Unlock()
		return
	}
	redactor := s.scopedRedactorLocked(task)
	event, subscribers := s.appendEventLocked(task, domain.GatewayWarningEvent{
		Code:           gatewayWarningCodeUnknownNotification,
		Message:        "app-server sent an unsupported notification; payload was suppressed",
		RequestType:    publicTextWithRedactor(redactor, notification.Method, domain.MaxSourceLabelBytes, "unknown"),
		AutoResolution: "ignored",
		LimitReason:    "payload_suppressed",
	})
	s.mu.Unlock()
	s.publishEvent(task.id, subscribers, event, false)
}

func (s *Service) taskForItemNotificationLocked(
	notification appserver.Notification,
	connection *appserver.Connection,
	threadID string,
	turnID string,
	itemID string,
) *task {
	if notification.TaskID != "" {
		task := s.taskForNotificationTaskIDLocked(notification, connection)
		if notificationTaskMatchesTurn(task, notification, threadID, turnID) && task.turnID != "" {
			s.bindItemLocked(task, itemID)
			return task
		}
		return nil
	}
	if threadID != "" && turnID != "" {
		task := s.taskForExactTurnNotificationLocked(notification, connection, threadID, turnID)
		s.bindItemLocked(task, itemID)
		return task
	}
	return s.taskForRegisteredItemLocked(connection, itemID)
}

func (s *Service) taskForUnknownNotificationLocked(
	notification appserver.Notification,
	connection *appserver.Connection,
	threadID string,
	turnID string,
	itemID string,
) *task {
	if notification.TaskID != "" {
		task := s.taskForNotificationTaskIDLocked(notification, connection)
		if notificationTaskMatchesTurn(task, notification, threadID, turnID) {
			return task
		}
		return nil
	}
	if threadID != "" && turnID != "" {
		return s.taskForExactTurnNotificationLocked(notification, connection, threadID, turnID)
	}
	return s.taskForRegisteredItemLocked(connection, itemID)
}

func (s *Service) taskForWarningNotificationLocked(
	notification appserver.Notification,
	connection *appserver.Connection,
	threadID string,
	turnID string,
	policy warningCorrelationPolicy,
) *task {
	if policy == warningCorrelationExactTurn {
		return s.taskForExactWarningNotificationLocked(notification, connection, threadID, turnID)
	}
	if notification.TaskID != "" {
		task := s.taskForNotificationTaskIDLocked(notification, connection)
		if notificationTaskMatchesTurn(task, notification, threadID, turnID) {
			return task
		}
		return nil
	}
	if threadID != "" && turnID != "" {
		return s.taskForExactTurnNotificationLocked(notification, connection, threadID, turnID)
	}
	if threadID != "" {
		return s.taskForThreadNotificationLocked(notification, connection, threadID)
	}
	return s.activeTaskForConnectionLocked(connection)
}

func (s *Service) taskForExactWarningNotificationLocked(
	notification appserver.Notification,
	connection *appserver.Connection,
	threadID string,
	turnID string,
) *task {
	if notification.TaskID != "" {
		task := s.taskForNotificationTaskIDLocked(notification, connection)
		if task == nil || turnID == "" || task.turnID != turnID {
			return nil
		}
		if !notificationMatchesTask(task, notification, threadID) {
			return nil
		}
		return task
	}
	if threadID != "" && turnID != "" {
		return s.taskForExactTurnNotificationLocked(notification, connection, threadID, turnID)
	}
	return nil
}

func notificationTaskMatchesTurn(task *task, notification appserver.Notification, threadID string, turnID string) bool {
	if task == nil || !notificationMatchesTask(task, notification, threadID) {
		return false
	}
	if turnID != "" && task.turnID != turnID {
		return false
	}
	return true
}

func (s *Service) taskForExactTurnNotificationLocked(
	notification appserver.Notification,
	connection *appserver.Connection,
	threadID string,
	turnID string,
) *task {
	if connection == nil || threadID == "" || turnID == "" {
		return nil
	}
	var found *task
	for _, task := range s.tasks {
		if task.connection != connection || isTerminalState(task.state) {
			continue
		}
		if task.threadID != threadID || task.turnID != turnID || !notificationMatchesSession(task, notification) {
			continue
		}
		if found != nil {
			return nil
		}
		found = task
	}
	return found
}

func (s *Service) taskForThreadNotificationLocked(notification appserver.Notification, connection *appserver.Connection, threadID string) *task {
	if connection == nil || threadID == "" {
		return nil
	}
	var found *task
	for _, task := range s.tasks {
		if task.connection != connection || isTerminalState(task.state) {
			continue
		}
		if task.threadID != threadID || !notificationMatchesSession(task, notification) {
			continue
		}
		if found != nil {
			return nil
		}
		found = task
	}
	return found
}

func (s *Service) activeTaskForConnectionLocked(connection *appserver.Connection) *task {
	if connection == nil {
		return nil
	}
	var found *task
	for _, task := range s.tasks {
		if task.connection != connection || isTerminalState(task.state) {
			continue
		}
		if found != nil {
			return nil
		}
		found = task
	}
	return found
}

func (s *Service) taskForRegisteredItemLocked(connection *appserver.Connection, itemID string) *task {
	if connection == nil || itemID == "" {
		return nil
	}
	var found *task
	for _, task := range s.tasks {
		if task.connection != connection || isTerminalState(task.state) || task.turnID == "" {
			continue
		}
		if _, ok := task.itemBindings[itemID]; !ok {
			continue
		}
		if found != nil {
			return nil
		}
		found = task
	}
	return found
}

func (s *Service) bindItemLocked(task *task, itemID string) {
	if task == nil || itemID == "" {
		return
	}
	if task.itemBindings == nil {
		task.itemBindings = map[string]struct{}{}
	}
	task.itemBindings[itemID] = struct{}{}
}

func (s *Service) itemNotificationPayloadsLocked(task *task, method string, root map[string]any, itemID string) []domain.TaskEventPayload {
	redactor := s.scopedRedactorLocked(task)
	switch method {
	case "item/started":
		payload := itemStartedPayload(root, itemID, redactor, task.pathSanitizer)
		if fileDiff, ok := payload.(domain.FileDiffUpdatedEvent); ok && itemID != "" {
			if task.fileDiffs == nil {
				task.fileDiffs = map[string]domain.FileDiffUpdatedEvent{}
			}
			task.fileDiffs[itemID] = fileDiff
		}
		return compactPayloads(payload)
	case "item/completed":
		payloads := s.flushItemStreamsLocked(task, itemID)
		return append(payloads, itemCompletedPayload(root, itemID, redactor, task.pathSanitizer))
	case "item/agentMessage/delta":
		delta, truncated := s.writeStreamDeltaLocked(
			task,
			taskStreamKey{kind: assistantDeltaStream, itemID: itemID},
			firstString(root, "delta", "textDelta", "message.delta"),
			domain.MaxOutboundAssistantTextBytes,
		)
		if delta == "" {
			return nil
		}
		return []domain.TaskEventPayload{domain.AssistantDeltaEvent{TextDelta: delta, Truncated: truncated}}
	case "item/plan/delta":
		delta := publicTextWithRedactor(redactor, firstString(root, "delta", "textDelta"), domain.MaxOutboundErrorDisplayMessageBytes, "plan updated")
		return []domain.TaskEventPayload{domain.GatewayWarningEvent{Code: gatewayWarningCodePlanDelta, Message: delta}}
	case "item/commandExecution/outputDelta":
		delta, truncated := s.writeStreamDeltaLocked(
			task,
			taskStreamKey{kind: commandOutputStream, itemID: itemID},
			firstString(root, "delta", "output", "chunk"),
			domain.MaxOutboundCommandOutputDeltaBytes,
		)
		if delta == "" {
			return nil
		}
		return []domain.TaskEventPayload{domain.CommandOutputDeltaEvent{
			ItemID:    itemID,
			Stream:    domain.CommandOutputStreamCombined,
			Delta:     delta,
			Truncated: truncated,
		}}
	case "item/fileChange/patchUpdated":
		payload := fileDiffUpdatedEvent(root, itemID, redactor, task.pathSanitizer)
		if task.fileDiffs == nil {
			task.fileDiffs = map[string]domain.FileDiffUpdatedEvent{}
		}
		if itemID != "" {
			task.fileDiffs[itemID] = payload
		}
		return compactPayloads(payload)
	case "item/mcpToolCall/progress":
		return []domain.TaskEventPayload{domain.ToolProgressEvent{
			ItemID:   itemID,
			ToolName: toolName(root, "mcpToolCall", redactor),
			State:    domain.ToolStateRunning,
			Summary:  publicTextWithRedactor(redactor, firstString(root, "progress", "message", "summary"), domain.MaxOutboundErrorDisplayMessageBytes, "tool progress"),
		}}
	default:
		return nil
	}
}

func compactPayloads(payload domain.TaskEventPayload) []domain.TaskEventPayload {
	if payload == nil {
		return nil
	}
	return []domain.TaskEventPayload{payload}
}

func (s *Service) redactPayloadLocked(task *task, payload domain.TaskEventPayload) domain.TaskEventPayload {
	redactor := s.scopedRedactorLocked(task)
	switch typed := payload.(type) {
	case domain.PlanUpdatedEvent:
		typed.Explanation = publicTextWithRedactor(redactor, typed.Explanation, domain.MaxOutboundErrorDisplayMessageBytes, "")
		for i, step := range typed.Steps {
			typed.Steps[i] = domain.PlanStep{
				Step:   publicTextWithRedactor(redactor, step.Step, domain.MaxOutboundErrorDisplayMessageBytes, ""),
				Status: publicTextWithRedactor(redactor, step.Status, domain.MaxSourceLabelBytes, ""),
			}
		}
		return typed
	case domain.TurnDiffUpdatedEvent:
		typed.DiffSummary, typed.Truncated = publicTextWithTruncationAndRedactor(redactor, typed.DiffSummary, domain.MaxOutboundDiffDisplayBytes)
		var unifiedTruncated bool
		typed.DiffUnified, unifiedTruncated = publicTextWithTruncationAndRedactor(redactor, typed.DiffUnified, domain.MaxOutboundDiffDisplayBytes)
		typed.Truncated = typed.Truncated || unifiedTruncated
		return typed
	case domain.GatewayWarningEvent:
		typed.Message = publicTextWithRedactor(redactor, typed.Message, domain.MaxOutboundErrorDisplayMessageBytes, "")
		typed.RequestType = publicTextWithRedactor(redactor, typed.RequestType, domain.MaxSourceLabelBytes, "")
		typed.AutoResolution = publicTextWithRedactor(redactor, typed.AutoResolution, domain.MaxSourceLabelBytes, "")
		typed.LimitReason = publicTextWithRedactor(redactor, typed.LimitReason, domain.MaxSourceLabelBytes, "")
		return typed
	default:
		return payload
	}
}

func itemStartedPayload(root map[string]any, itemID string, redactor *redact.Redactor, pathSanitizer *redact.PathSanitizer) domain.TaskEventPayload {
	switch itemType(root) {
	case "commandExecution":
		return domain.CommandStartedEvent{
			ItemID:         itemID,
			CommandDisplay: commandDisplay(root, redactor),
			WorkspaceLabel: pathLabel(pathSanitizer, redactor, firstString(root, "workspaceLabel", "cwd", "workdir"), ""),
		}
	case "fileChange":
		return fileDiffUpdatedEvent(root, itemID, redactor, pathSanitizer)
	case "mcpToolCall", "collabToolCall":
		return domain.ToolProgressEvent{
			ItemID:   itemID,
			ToolName: toolName(root, itemType(root), redactor),
			State:    domain.ToolStateStarted,
			Summary:  publicTextWithRedactor(redactor, firstString(root, "summary", "message"), domain.MaxOutboundErrorDisplayMessageBytes, ""),
		}
	default:
		return domain.GatewayWarningEvent{
			Code:    gatewayWarningCodeUnsupportedItem,
			Message: publicTextWithRedactor(redactor, fmt.Sprintf("unsupported item type: %s", itemType(root)), domain.MaxOutboundErrorDisplayMessageBytes, "unsupported item"),
		}
	}
}

func itemCompletedPayload(root map[string]any, itemID string, redactor *redact.Redactor, pathSanitizer *redact.PathSanitizer) domain.TaskEventPayload {
	switch itemType(root) {
	case "agentMessage":
		message, truncated := publicTextWithTruncationAndRedactor(redactor, firstString(root, "message", "text", "content", "item.message", "item.text", "item.content"), domain.MaxOutboundAssistantTextBytes)
		return domain.AssistantMessageCompletedEvent{Message: message, Truncated: truncated}
	case "commandExecution", "mcpToolCall", "collabToolCall":
		return domain.ToolProgressEvent{
			ItemID:   itemID,
			ToolName: toolName(root, itemType(root), redactor),
			State:    toolState(root, domain.ToolStateCompleted),
			Summary:  publicTextWithRedactor(redactor, firstString(root, "summary", "message", "result"), domain.MaxOutboundErrorDisplayMessageBytes, ""),
		}
	case "fileChange":
		return fileDiffUpdatedEvent(root, itemID, redactor, pathSanitizer)
	default:
		return domain.GatewayWarningEvent{
			Code:    gatewayWarningCodeUnsupportedItem,
			Message: publicTextWithRedactor(redactor, fmt.Sprintf("unsupported item type: %s", itemType(root)), domain.MaxOutboundErrorDisplayMessageBytes, "unsupported item"),
		}
	}
}

func planUpdatedEvent(root map[string]any) domain.PlanUpdatedEvent {
	stepsValue := firstValue(root, "plan", "steps")
	steps := make([]domain.PlanStep, 0)
	if rawSteps, ok := stepsValue.([]any); ok {
		for _, rawStep := range rawSteps {
			if len(steps) >= domain.MaxPlanSteps {
				break
			}
			stepMap, ok := rawStep.(map[string]any)
			if !ok {
				continue
			}
			step := firstString(stepMap, "step", "text", "message")
			status := firstString(stepMap, "status")
			if step != "" || status != "" {
				steps = append(steps, domain.PlanStep{Step: step, Status: status})
			}
		}
	}
	return domain.PlanUpdatedEvent{
		Explanation: firstString(root, "explanation", "message"),
		Steps:       steps,
	}
}

func turnDiffUpdatedEvent(root map[string]any) domain.TurnDiffUpdatedEvent {
	return domain.TurnDiffUpdatedEvent{
		DiffSummary: firstString(root, "diffSummary", "summary", "diff.summary"),
		DiffUnified: firstString(root, "diffUnified", "unifiedDiff", "diff.unified", "diff"),
	}
}

func fileDiffUpdatedEvent(root map[string]any, itemID string, redactor *redact.Redactor, pathSanitizer *redact.PathSanitizer) domain.FileDiffUpdatedEvent {
	summary, summaryTruncated := publicTextWithTruncationAndRedactor(redactor, firstString(root, "diffSummary", "summary", "diff.summary"), domain.MaxOutboundDiffDisplayBytes)
	unified, unifiedTruncated := publicTextWithTruncationAndRedactor(redactor, firstString(root, "diffUnified", "unifiedDiff", "patch", "diff.unified", "diff"), domain.MaxOutboundDiffDisplayBytes)
	return domain.FileDiffUpdatedEvent{
		ItemID:      itemID,
		FileLabel:   fileLabel(root, redactor, pathSanitizer),
		ChangeKind:  publicTextWithRedactor(redactor, firstString(root, "changeKind", "action", "change.kind"), domain.MaxSourceLabelBytes, ""),
		DiffSummary: summary,
		DiffUnified: unified,
		Truncated:   summaryTruncated || unifiedTruncated,
	}
}

func commandDisplay(root map[string]any, redactor *redact.Redactor) string {
	if display := firstString(root, "commandDisplay", "item.commandDisplay"); display != "" {
		return publicTextWithRedactor(redactor, display, domain.MaxOutboundCommandDisplayBytes, "command")
	}
	if command := firstValue(root, "command", "item.command"); command != nil {
		return publicTextWithRedactor(redactor, commandValueDisplay(command), domain.MaxOutboundCommandDisplayBytes, "command")
	}
	return "command"
}

func commandValueDisplay(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, part := range typed {
			parts = append(parts, fmt.Sprint(part))
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(typed)
	}
}

func fileLabel(root map[string]any, redactor *redact.Redactor, pathSanitizer *redact.PathSanitizer) string {
	if label := firstString(root, "fileLabel", "path", "filePath", "change.path", "item.path"); label != "" {
		return pathLabel(pathSanitizer, redactor, label, redact.PathMarker)
	}
	if changes, ok := firstValue(root, "changes").([]any); ok {
		for _, change := range changes {
			changeMap, ok := change.(map[string]any)
			if !ok {
				continue
			}
			if label := firstString(changeMap, "fileLabel", "path", "filePath"); label != "" {
				return pathLabel(pathSanitizer, redactor, label, redact.PathMarker)
			}
		}
	}
	return "file"
}

func pathLabel(pathSanitizer *redact.PathSanitizer, redactor *redact.Redactor, label string, fallback string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return fallback
	}
	if pathSanitizer != nil {
		label = pathSanitizer.SanitizeLabel(label)
	} else {
		label = redact.PathMarker
	}
	return publicTextWithRedactor(redactor, label, domain.MaxSourceLabelBytes, fallback)
}

func toolName(root map[string]any, fallback string, redactor *redact.Redactor) string {
	name := firstString(root, "toolName", "name", "serverName", "item.toolName", "item.name")
	if name == "" {
		name = fallback
	}
	return publicTextWithRedactor(redactor, name, domain.MaxSourceLabelBytes, "tool")
}

func itemType(root map[string]any) string {
	return firstString(root, "item.type", "type", "itemType")
}

func notificationItemID(root map[string]any) string {
	return firstString(root, "itemId", "itemID", "item.id", "id")
}

func toolState(root map[string]any, defaultState domain.ToolState) domain.ToolState {
	switch strings.ToLower(firstString(root, "status", "item.status", "state")) {
	case "failed", "failure", "error", "cancelled", "canceled":
		return domain.ToolStateFailed
	case "running", "in_progress", "progress":
		return domain.ToolStateRunning
	case "completed", "complete", "succeeded", "success":
		return domain.ToolStateCompleted
	default:
		return defaultState
	}
}

func worldWritableWarningMessage(root map[string]any) string {
	extraCount := firstString(root, "extraCount")
	failedScan := firstString(root, "failedScan")
	switch {
	case extraCount != "" && failedScan != "":
		return publicText("world-writable path warning; extra paths: "+extraCount+"; failed scan: "+failedScan, domain.MaxOutboundErrorDisplayMessageBytes, "world-writable path warning")
	case extraCount != "":
		return publicText("world-writable path warning; extra paths: "+extraCount, domain.MaxOutboundErrorDisplayMessageBytes, "world-writable path warning")
	default:
		return "world-writable path warning"
	}
}

func decodeNotificationObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return map[string]any{}
	}
	return value
}

func firstString(root map[string]any, paths ...string) string {
	for _, path := range paths {
		value := firstValue(root, path)
		switch typed := value.(type) {
		case string:
			if typed != "" {
				return typed
			}
		case json.Number:
			return typed.String()
		case bool:
			return fmt.Sprint(typed)
		case nil:
		default:
			if encoded, err := json.Marshal(typed); err == nil {
				return string(encoded)
			}
		}
	}
	return ""
}

func firstValue(root map[string]any, paths ...string) any {
	for _, path := range paths {
		value, ok := valueAtPath(root, path)
		if ok && value != nil {
			return value
		}
	}
	return nil
}

func valueAtPath(root map[string]any, path string) (any, bool) {
	var current any = root
	for _, segment := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func publicText(value string, maxBytes int, fallback string) string {
	text, _ := publicTextWithTruncation(value, maxBytes)
	if text == "" {
		return fallback
	}
	return text
}

func publicTextWithTruncation(value string, maxBytes int) (string, bool) {
	return publicTextWithTruncationAndRedactor(nil, value, maxBytes)
}

func publicTextWithRedactor(redactor *redact.Redactor, value string, maxBytes int, fallback string) string {
	text, _ := publicTextWithTruncationAndRedactor(redactor, value, maxBytes)
	if text == "" {
		return fallback
	}
	return text
}

func publicTextWithTruncationAndRedactor(redactor *redact.Redactor, value string, maxBytes int) (string, bool) {
	if redactor == nil {
		redactor = redact.New()
	}
	redacted := redactor.RedactString(value)
	return truncateUTF8Bytes(redacted, maxBytes)
}

func redactedJSON(raw json.RawMessage, maxBytes int, redactor *redact.Redactor) (string, bool) {
	if len(raw) == 0 {
		return "{}", false
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return "{}", false
	}
	return publicTextWithTruncationAndRedactor(redactor, compact.String(), maxBytes)
}

func truncateUTF8Bytes(value string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value, false
	}
	truncated := value[:maxBytes]
	for !utf8.ValidString(truncated) && len(truncated) > 0 {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated, true
}
