package chatruntime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/Dirard/codex-runtime/gateway/internal/appserver"
	"github.com/Dirard/codex-runtime/gateway/internal/chatstate"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	"github.com/Dirard/codex-runtime/gateway/internal/redact"
)

const (
	chatGatewayWarningCodeAppServerWarning  = "app_server_warning"
	chatGatewayWarningCodeConfigWarning     = "config_warning"
	chatGatewayWarningCodeGuardianWarning   = "guardian_warning"
	chatGatewayWarningCodeModelRerouted     = "model_rerouted"
	chatGatewayWarningCodeModelVerification = "model_verification"
)

type notificationProvider interface {
	Notifications() <-chan appserver.Notification
}

func (s *Service) configureNotificationBridge(connection AppServerClient) {
	provider, ok := connection.(notificationProvider)
	if !ok {
		return
	}
	notifications := provider.Notifications()
	if notifications == nil {
		return
	}
	key, ok := comparableNotificationProvider(provider)
	if !ok {
		return
	}
	s.mu.Lock()
	if _, exists := s.notificationProviders[key]; exists {
		s.mu.Unlock()
		return
	}
	s.notificationProviders[key] = struct{}{}
	s.mu.Unlock()

	go s.consumeNotifications(connection, notifications)
}

func comparableNotificationProvider(provider notificationProvider) (notificationProvider, bool) {
	value := reflect.ValueOf(provider)
	if !value.IsValid() || !value.Type().Comparable() {
		return nil, false
	}
	return provider, true
}

func (s *Service) consumeNotifications(connection AppServerClient, notifications <-chan appserver.Notification) {
	for notification := range notifications {
		s.handleChatNotification(connection, notification)
	}
}

func (s *Service) handleChatNotification(connection AppServerClient, notification appserver.Notification) {
	root := decodeChatNotificationObject(notification.Params)
	sessionGroupID := notification.SessionGroupID
	if sessionGroupID == "" {
		sessionGroupID = connectionSessionGroupID(connection)
	}
	session, ok := s.sessionByID(sessionGroupID)
	if !ok {
		return
	}
	threadID := appserver.ParseThreadID(notification.Params)
	turnID := appserver.ParseTurnID(notification.Params)
	if threadID == "" {
		threadID = firstChatNotificationString(root, "threadId", "thread.id")
	}
	if turnID == "" {
		turnID = firstChatNotificationString(root, "turnId", "turn.id")
	}
	if threadID == "" || turnID == "" {
		return
	}

	scope := chatstate.Scope{SessionGroupID: sessionGroupID, WorkspaceID: session.Group.WorkspaceID, ChatID: threadID}
	active, ok := s.store.ActiveRun(scope)
	if !ok || active.RunID != turnID {
		return
	}
	runScope := chatstate.RunScope{Scope: scope, RunID: turnID}

	switch notification.Method {
	case "item/agentMessage/delta":
		s.appendAssistantDeltaEvent(connection, session, runScope, active.State, root)
	case "item/started":
		s.appendCommandStartedEvent(connection, session, runScope, active.State, root)
	case "item/commandExecution/outputDelta":
		s.appendCommandOutputDeltaEvent(connection, session, runScope, active.State, root)
	case "item/completed":
		s.appendAssistantCompletedEvent(connection, session, runScope, active.State, root)
	case "turn/completed":
		s.appendTurnCompletedEvent(runScope, root)
	case "warning", "guardianWarning", "configWarning", "model/rerouted", "model/verification":
		s.appendGatewayWarningEvent(connection, session, runScope, active.State, notification.Method, root)
	}
}

func (s *Service) appendAssistantDeltaEvent(connection AppServerClient, session Session, runScope chatstate.RunScope, state chatstate.RunState, root map[string]any) {
	rawDelta := firstChatNotificationString(root, "delta", "textDelta", "message.delta")
	delta, truncated := chatNotificationText(connection, session, rawDelta, domain.MaxOutboundAssistantTextBytes)
	if delta == "" {
		return
	}
	_, _ = s.store.AppendEvent(chatstate.EventInput{
		RunScope:       runScope,
		Kind:           "assistant_delta",
		State:          string(state),
		AssistantDelta: &domain.AssistantDeltaEvent{TextDelta: delta, Truncated: truncated},
		SizeBytes:      int64(len(delta) + len(runScope.ChatID) + len(runScope.RunID)),
	})
}

func (s *Service) appendAssistantCompletedEvent(connection AppServerClient, session Session, runScope chatstate.RunScope, state chatstate.RunState, root map[string]any) {
	if firstChatNotificationString(root, "item.type", "type", "itemType") != "agentMessage" {
		return
	}
	rawMessage := firstChatNotificationString(root, "message", "text", "content", "item.message", "item.text", "item.content")
	message, truncated := chatNotificationText(connection, session, rawMessage, domain.MaxOutboundAssistantTextBytes)
	_, _ = s.store.AppendEvent(chatstate.EventInput{
		RunScope:                  runScope,
		Kind:                      "assistant_message_completed",
		State:                     string(state),
		AssistantMessageCompleted: &domain.AssistantMessageCompletedEvent{Message: message, Truncated: truncated},
		SizeBytes:                 int64(len(message) + len(runScope.ChatID) + len(runScope.RunID)),
	})
}

func (s *Service) appendCommandStartedEvent(connection AppServerClient, session Session, runScope chatstate.RunScope, state chatstate.RunState, root map[string]any) {
	if firstChatNotificationString(root, "item.type", "type", "itemType") != "commandExecution" {
		return
	}
	itemID := firstChatNotificationString(root, "item.id", "itemId", "item_id", "id")
	if itemID == "" {
		return
	}
	rawCommand := firstChatNotificationString(root, "item.command", "command", "item.commandDisplay", "commandDisplay")
	command, commandTruncated := chatNotificationText(connection, session, rawCommand, domain.MaxOutboundCommandDisplayBytes)
	workspaceLabel, labelTruncated := chatNotificationText(connection, session, firstChatNotificationString(root, "item.workspaceLabel", "workspaceLabel", "item.cwd", "cwd"), domain.MaxSourceLabelBytes)
	_, _ = commandTruncated, labelTruncated
	_, _ = s.store.AppendEvent(chatstate.EventInput{
		RunScope: runScope,
		Kind:     "command_started",
		State:    string(state),
		CommandStarted: &domain.CommandStartedEvent{
			ItemID:         itemID,
			CommandDisplay: command,
			WorkspaceLabel: workspaceLabel,
		},
		SizeBytes: int64(len(itemID) + len(command) + len(workspaceLabel) + len(runScope.ChatID) + len(runScope.RunID)),
	})
}

func (s *Service) appendCommandOutputDeltaEvent(connection AppServerClient, session Session, runScope chatstate.RunScope, state chatstate.RunState, root map[string]any) {
	itemID := firstChatNotificationString(root, "itemId", "item_id", "item.id", "id")
	if itemID == "" {
		return
	}
	output, truncated := chatNotificationText(connection, session, firstChatNotificationString(root, "delta", "outputDelta", "item.delta"), domain.MaxOutboundCommandOutputDeltaBytes)
	if output == "" {
		return
	}
	_, _ = s.store.AppendEvent(chatstate.EventInput{
		RunScope: runScope,
		Kind:     "command_output_delta",
		State:    string(state),
		CommandOutputDelta: &domain.CommandOutputDeltaEvent{
			ItemID:    itemID,
			Stream:    domain.CommandOutputStreamCombined,
			Delta:     output,
			Truncated: truncated,
		},
		SizeBytes: int64(len(itemID) + len(output) + len(runScope.ChatID) + len(runScope.RunID)),
	})
}

func (s *Service) appendGatewayWarningEvent(connection AppServerClient, session Session, runScope chatstate.RunScope, state chatstate.RunState, method string, root map[string]any) {
	warning, ok := chatGatewayWarningFromNotification(method, root)
	if !ok || warning.Message == "" {
		return
	}
	message, messageTruncated := chatNotificationText(connection, session, warning.Message, domain.MaxOutboundErrorDisplayMessageBytes)
	if message == "" {
		return
	}
	warning.Message = message
	if messageTruncated && warning.LimitReason == "" {
		warning.LimitReason = "message_truncated"
	}
	_, _ = s.store.AppendEvent(chatstate.EventInput{
		RunScope:       runScope,
		Kind:           "gateway_warning",
		State:          string(state),
		GatewayWarning: &warning,
		SizeBytes:      int64(len(warning.Code) + len(warning.Message) + len(warning.RequestType) + len(runScope.ChatID) + len(runScope.RunID)),
	})
}

func (s *Service) appendTurnCompletedEvent(runScope chatstate.RunScope, root map[string]any) {
	state, ok := chatNotificationTerminalState(root)
	if !ok {
		return
	}
	lifecycle := turnLifecycleFromRunState(state)
	terminal := &domain.ChatTerminal{
		State:          lifecycle,
		DisplayMessage: firstChatNotificationString(root, "turn.summary", "summary", "message"),
	}
	_, _ = s.store.AppendEvent(chatstate.EventInput{
		RunScope:  runScope,
		Kind:      "terminal",
		State:     string(state),
		Terminal:  terminal,
		SizeBytes: int64(len(runScope.ChatID) + len(runScope.RunID) + len(terminal.DisplayMessage) + len(lifecycle)),
	})
	_, _ = s.store.CompleteRun(runScope, state)
}

func chatGatewayWarningFromNotification(method string, root map[string]any) (domain.GatewayWarningEvent, bool) {
	warning := domain.GatewayWarningEvent{
		RequestType: method,
	}
	switch method {
	case "warning":
		warning.Code = chatGatewayWarningCodeAppServerWarning
		warning.Message = firstNonEmptyChatString(root, "app-server warning", "message", "warning.message")
	case "guardianWarning":
		warning.Code = chatGatewayWarningCodeGuardianWarning
		warning.Message = firstNonEmptyChatString(root, "guardian warning", "message", "warning.message")
	case "configWarning":
		warning.Code = chatGatewayWarningCodeConfigWarning
		warning.Message = firstNonEmptyChatString(root, "configuration warning", "summary", "message", "details")
	case "model/rerouted":
		warning.Code = chatGatewayWarningCodeModelRerouted
		warning.Message = firstNonEmptyChatString(root, "model request was rerouted", "message", "summary")
		warning.AutoResolution = "continue"
		warning.LimitReason = firstChatNotificationString(root, "reason")
	case "model/verification":
		warning.Code = chatGatewayWarningCodeModelVerification
		warning.Message = firstNonEmptyChatString(root, "model verification update", "message", "summary", "status")
		warning.AutoResolution = "continue"
	default:
		return domain.GatewayWarningEvent{}, false
	}
	return warning, true
}

func firstNonEmptyChatString(root map[string]any, fallback string, paths ...string) string {
	if value := firstChatNotificationString(root, paths...); value != "" {
		return value
	}
	return fallback
}

func chatNotificationTerminalState(root map[string]any) (chatstate.RunState, bool) {
	switch strings.ToLower(firstChatNotificationString(root, "turn.status.type", "turn.status", "status.type", "status")) {
	case "completed":
		return chatstate.RunStateCompleted, true
	case "interrupted", "cancelled", "canceled":
		return chatstate.RunStateInterrupted, true
	case "failed", "failure", "error":
		return chatstate.RunStateFailed, true
	default:
		return "", false
	}
}

func chatNotificationText(connection AppServerClient, session Session, value string, maxBytes int) (string, bool) {
	if value == "" {
		return "", false
	}
	registry := sensitiveRegistry(connection)
	var options []redact.Option
	if registry != nil {
		options = append(options, redact.WithConnectionRegistry(registry))
	}
	if sanitizer, err := redact.NewPathSanitizer(session.Group.CanonicalCWD); err == nil {
		options = append(options, redact.WithPathSanitizer(sanitizer))
	}
	redacted := redact.New(options...).RedactString(value)
	return truncateUTF8BytesWithFlag(redacted, maxBytes)
}

func truncateUTF8BytesWithFlag(value string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value, false
	}
	truncated := value[:maxBytes]
	for !utf8.ValidString(truncated) && len(truncated) > 0 {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated, true
}

func decodeChatNotificationObject(raw json.RawMessage) map[string]any {
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

func firstChatNotificationString(root map[string]any, paths ...string) string {
	for _, path := range paths {
		value := firstChatNotificationValue(root, path)
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

func firstChatNotificationValue(root map[string]any, paths ...string) any {
	for _, path := range paths {
		value, ok := chatNotificationValueAtPath(root, path)
		if ok && value != nil {
			return value
		}
	}
	return nil
}

func chatNotificationValueAtPath(root map[string]any, path string) (any, bool) {
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
