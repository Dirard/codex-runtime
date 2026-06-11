package tasks

import (
	"encoding/json"
	"fmt"

	"github.com/Dirard/codex-runtime/internal/appserver"
	"github.com/Dirard/codex-runtime/internal/domain"
	"github.com/Dirard/codex-runtime/internal/pending"
)

const (
	unknownTurnStatusWarningCode           = "unknown_turn_status"
	unknownTurnStatusWarningMessage        = "turn/completed reported an unknown status; task failed closed"
	unsupportedServerRequestWarningCode    = "unsupported_server_request"
	unsupportedServerRequestWarningMessage = "app-server sent an unsupported request"
	maxDeferredUnsupportedWarnings         = 64
)

func (s *Service) ensureMonitor(connection *appserver.Connection, supervisor AppServerSupervisor) {
	s.mu.Lock()
	if _, exists := s.monitors[connection]; exists {
		s.mu.Unlock()
		return
	}
	s.monitors[connection] = struct{}{}
	s.mu.Unlock()

	go s.monitorConnection(connection, supervisor)
}

func (s *Service) monitorConnection(connection *appserver.Connection, supervisor AppServerSupervisor) {
	notifications := connection.Notifications()
	requests := connection.Requests()
	for notifications != nil || requests != nil {
		select {
		case notification, ok := <-notifications:
			if !ok {
				notifications = nil
				continue
			}
			s.handleNotification(notification, connection)
		case request, ok := <-requests:
			if !ok {
				requests = nil
				continue
			}
			s.handleServerRequest(request, connection)
		}
	}
	s.failTasksForClosedConnection(connection)
	supervisor.MarkClosed(connection)
}

func (s *Service) handleAuthRefreshFailure(failure appserver.AuthRefreshFailure) {
	if failure.Reason == "" {
		failure.Reason = domain.ReasonAuthRefreshUnavailable
	}
	if failure.TaskID == "" {
		s.recordConnectionDiagnostic(connectionDiagnostic{
			SessionGroupID: failure.SessionGroupID,
			Code:           string(failure.Reason),
			Method:         "account/chatgptAuthTokens/refresh",
		})
		return
	}

	var publications []publication
	s.mu.Lock()
	task := s.tasks[failure.TaskID]
	if task == nil || task.sessionGroupID != failure.SessionGroupID || isTerminalState(task.state) {
		s.recordConnectionDiagnosticLocked(connectionDiagnostic{
			SessionGroupID: failure.SessionGroupID,
			TaskID:         failure.TaskID,
			Code:           string(failure.Reason),
			Method:         "account/chatgptAuthTokens/refresh",
		})
		s.mu.Unlock()
		return
	}
	task.state = domain.TaskStateFailed
	task.phase = startPhaseTerminal
	terminal := domain.TaskTerminalEvent{
		TerminalState: domain.TerminalStateFailed,
		ErrorMessage:  string(failure.Reason),
	}
	task.terminal = &terminal
	task.terminalAt = s.now()
	s.clearActiveLocked(task)
	payloads := s.flushTaskStreamsLocked(task)
	payloads = append(payloads, s.clearPendingLocked(task, domain.PendingResolutionFailed)...)
	payloads = append(payloads, terminal)
	publications = s.appendPublicationsLocked(task, payloads, true)
	if task.startEntry != nil && task.startEntry.state == startEntryPending {
		err := authRefreshUnavailable(task.sessionGroupID, task.id, task.clientMessageID)
		task.startEntry.state = startEntryFailed
		task.startEntry.err = err
		task.startEntry.updatedAt = s.now()
		task.startEntry.promise.err = err
		close(task.startEntry.promise.done)
	}
	s.mu.Unlock()
	s.publishPublications(publications)
}

func (s *Service) handleUnsupportedServerRequest(request appserver.UnsupportedServerRequest, connection *appserver.Connection) {
	if request.TaskID == "" && (request.ThreadID == "" || request.TurnID == "") {
		s.recordConnectionDiagnosticForConnection(connectionDiagnostic{
			SessionGroupID: request.SessionGroupID,
			Code:           pending.WarningCodeUnsupportedServerRequest,
			Method:         request.Method,
		}, connection)
		return
	}

	s.mu.Lock()
	session := s.sessions[request.SessionGroupID]
	notification := appserver.Notification{
		TaskID:         request.TaskID,
		SessionGroupID: request.SessionGroupID,
	}
	var task *task
	if request.TaskID != "" {
		candidate := s.taskForNotificationTaskIDLocked(notification, connection)
		if notificationTaskMatchesTurn(candidate, notification, request.ThreadID, request.TurnID) {
			task = candidate
		}
	} else if request.ThreadID != "" && request.TurnID != "" {
		task = s.taskForExactTurnNotificationLocked(notification, connection, request.ThreadID, request.TurnID)
	}
	if session == nil || task == nil || session.activeTaskID != task.id || isTerminalState(task.state) {
		s.mu.Unlock()
		if request.TaskID == "" {
			s.recordConnectionDiagnosticForConnection(connectionDiagnostic{
				SessionGroupID: request.SessionGroupID,
				Code:           pending.WarningCodeUnsupportedServerRequest,
				Method:         request.Method,
			}, connection)
		}
		return
	}
	if task.startEntry != nil && task.startEntry.state == startEntryPending {
		deferUnsupportedServerRequestLocked(task, request.Method)
		s.mu.Unlock()
		return
	}
	taskID := task.id
	event, subscribers := s.appendUnsupportedServerRequestWarningLocked(task, request.Method, 1, false, false)
	s.mu.Unlock()
	s.publishEvent(taskID, subscribers, event, false)
}

func (s *Service) appendUnsupportedServerRequestWarningLocked(
	task *task,
	method string,
	count int,
	overflow bool,
	multipleMethods bool,
) (domain.TaskEvent, []*taskSubscriber) {
	return s.appendEventLocked(task, domain.GatewayWarningEvent{
		Code:    unsupportedServerRequestWarningCode,
		Message: unsupportedServerRequestWarningText(method, count, overflow, multipleMethods),
	})
}

func (s *Service) appendDeferredUnsupportedWarningPublicationsLocked(task *task) []publication {
	if task == nil || task.deferredUnsupportedWarnings == 0 {
		return nil
	}
	warningCount := task.deferredUnsupportedWarnings
	method := task.deferredUnsupportedMethod
	multipleMethods := task.deferredUnsupportedMultiple
	overflow := task.deferredUnsupportedOverflow
	task.deferredUnsupportedWarnings = 0
	task.deferredUnsupportedMethod = ""
	task.deferredUnsupportedMultiple = false
	task.deferredUnsupportedOverflow = false
	event, subscribers := s.appendUnsupportedServerRequestWarningLocked(task, method, warningCount, overflow, multipleMethods)
	return []publication{{
		event:       event,
		taskID:      task.id,
		subscribers: subscribers,
	}}
}

func deferUnsupportedServerRequestLocked(task *task, method string) {
	if task.deferredUnsupportedWarnings < maxDeferredUnsupportedWarnings {
		task.deferredUnsupportedWarnings++
	} else {
		task.deferredUnsupportedOverflow = true
	}
	if task.deferredUnsupportedMethod == "" {
		task.deferredUnsupportedMethod = method
		return
	}
	if method != "" && method != task.deferredUnsupportedMethod {
		task.deferredUnsupportedMultiple = true
	}
}

func unsupportedServerRequestWarningText(method string, count int, overflow bool, multipleMethods bool) string {
	message := unsupportedServerRequestWarningMessage
	if method != "" {
		message += ": " + method
	}
	if count > 1 || overflow {
		countText := fmt.Sprintf("%d total while task start was pending", count)
		if overflow {
			countText = fmt.Sprintf("%d or more total while task start was pending", count)
		}
		if multipleMethods {
			countText += ", multiple methods"
		}
		message += " (" + countText + ")"
	}
	return message
}

func (s *Service) handleNotification(notification appserver.Notification, connection *appserver.Connection) {
	switch notification.Method {
	case "thread/started":
		s.handleThreadStarted(notification, connection)
	case "turn/started":
		s.handleTurnStarted(notification, connection)
	case "turn/completed":
		s.handleTurnCompleted(notification, connection)
	case "serverRequest/resolved":
		s.handleServerRequestResolved(notification, connection)
	default:
		s.handleMappedNotification(notification, connection)
	}
}

func (s *Service) handleThreadStarted(notification appserver.Notification, connection *appserver.Connection) {
	threadID := appserver.ParseThreadID(notification.Params)
	if threadID == "" {
		return
	}
	s.mu.Lock()
	task := s.taskForVerifiedLifecycleNotificationLocked(notification, connection)
	if task == nil || task.threadID != "" || isTerminalState(task.state) {
		s.mu.Unlock()
		return
	}
	task.threadID = threadID
	event, subscribers := s.appendEventLocked(task, domain.TaskLifecycleEvent{
		LifecycleEvent: domain.TaskLifecycleEventThreadStarted,
		State:          task.state,
	})
	s.mu.Unlock()
	s.publishEvent(task.id, subscribers, event, false)
}

func (s *Service) handleTurnStarted(notification appserver.Notification, connection *appserver.Connection) {
	turnID := appserver.ParseTurnID(notification.Params)
	if turnID == "" {
		return
	}
	threadID := appserver.ParseThreadID(notification.Params)
	taskID, ok := s.verifiedLifecycleTaskID(notification, connection, threadID, turnID)
	if !ok {
		return
	}
	if threadID != "" {
		s.confirmThreadStarted(taskID, threadID)
	}
	_, interruptAfterStart, interruptConnection := s.confirmTurnStartedOnce(taskID, turnID)
	if interruptAfterStart {
		s.sendInterruptAfterTurnStart(interruptConnection, taskID, turnID)
	}
}

func (s *Service) handleTurnCompleted(notification appserver.Notification, connection *appserver.Connection) {
	turnID := appserver.ParseTurnID(notification.Params)
	if turnID == "" {
		return
	}
	threadID := appserver.ParseThreadID(notification.Params)
	status := parseTurnStatus(notification.Params)
	s.mu.Lock()
	task := s.taskForTurnCompletedNotificationLocked(notification, connection, threadID, turnID)
	if task == nil || task.turnID != turnID || isTerminalState(task.state) {
		s.mu.Unlock()
		return
	}
	terminal, knownStatus := terminalForTurnStatus(status)
	task.state = stateForTerminal(terminal.TerminalState)
	task.phase = startPhaseTerminal
	task.terminal = &terminal
	task.terminalAt = s.now()
	s.clearActiveLocked(task)
	payloads := s.flushTaskStreamsLocked(task)
	if !knownStatus {
		payloads = append(payloads, domain.GatewayWarningEvent{
			Code:    unknownTurnStatusWarningCode,
			Message: unknownTurnStatusWarningMessage,
		})
	}
	payloads = append(payloads, s.clearPendingLocked(task, domain.PendingResolutionCleared)...)
	payloads = append(payloads, terminal)
	publications := s.appendPublicationsLocked(task, payloads, true)
	s.mu.Unlock()
	s.publishPublications(publications)
}

func (s *Service) verifiedLifecycleTaskID(notification appserver.Notification, connection *appserver.Connection, threadID string, turnID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.taskForVerifiedLifecycleNotificationLocked(notification, connection)
	if task == nil || isTerminalState(task.state) {
		return "", false
	}
	if threadID != "" && task.threadID != "" && task.threadID != threadID {
		return "", false
	}
	if turnID != "" && task.turnID != "" {
		return "", false
	}
	return task.id, true
}

func (s *Service) failTasksForClosedConnection(connection *appserver.Connection) {
	var publications []publication
	s.mu.Lock()
	for _, task := range s.tasks {
		if task.connection != connection || isTerminalState(task.state) {
			continue
		}
		publications = append(publications, s.appendDeferredUnsupportedWarningPublicationsLocked(task)...)
		if task.cancelBeforeTurn && task.turnID == "" && task.startEntry != nil && task.startEntry.state == startEntryPending {
			event, subscribers := s.completePreTurnInterruptLocked(task, task.startEntry.promise)
			publications = append(publications, publication{event: event, taskID: task.id, subscribers: subscribers, closeAfter: true})
			continue
		}
		task.state = domain.TaskStateFailed
		task.phase = startPhaseTerminal
		terminal := domain.TaskTerminalEvent{
			TerminalState: domain.TerminalStateFailed,
			ErrorMessage:  "app-server connection closed",
		}
		task.terminal = &terminal
		task.terminalAt = s.now()
		s.clearActiveLocked(task)
		payloads := s.flushTaskStreamsLocked(task)
		payloads = append(payloads, s.clearPendingLocked(task, domain.PendingResolutionFailed)...)
		payloads = append(payloads, terminal)
		taskPublications := s.appendPublicationsLocked(task, payloads, true)
		if task.startEntry != nil && task.startEntry.state == startEntryPending {
			task.startEntry.state = startEntryFailed
			task.startEntry.err = safeStartError(appserver.ErrDispatcherClosed, task)
			task.startEntry.updatedAt = s.now()
			task.startEntry.promise.err = task.startEntry.err
			close(task.startEntry.promise.done)
		}
		publications = append(publications, taskPublications...)
	}
	delete(s.monitors, connection)
	delete(s.resolvedServerRequests, connection)
	s.mu.Unlock()
	s.publishPublications(publications)
}

type publication struct {
	event       domain.TaskEvent
	taskID      string
	subscribers []*taskSubscriber
	closeAfter  bool
}

func (s *Service) taskForNotificationTaskIDLocked(notification appserver.Notification, connection *appserver.Connection) *task {
	if notification.TaskID != "" {
		task := s.tasks[notification.TaskID]
		if task != nil && task.connection == connection && notificationMatchesSession(task, notification) {
			return task
		}
	}
	return nil
}

func (s *Service) taskForVerifiedLifecycleNotificationLocked(notification appserver.Notification, connection *appserver.Connection) *task {
	if connection == nil || notification.TaskID == "" || notification.SessionGroupID == "" {
		return nil
	}
	task := s.tasks[notification.TaskID]
	if task != nil && task.connection == connection && task.sessionGroupID == notification.SessionGroupID {
		return task
	}
	return nil
}

func (s *Service) taskForTurnCompletedNotificationLocked(notification appserver.Notification, connection *appserver.Connection, threadID string, turnID string) *task {
	if notification.TaskID != "" {
		task := s.taskForNotificationTaskIDLocked(notification, connection)
		if task == nil {
			return nil
		}
		if notificationMatchesTask(task, notification, threadID) {
			return task
		}
		return nil
	}
	if turnID == "" {
		return nil
	}
	var found *task
	for _, task := range s.tasks {
		if task.connection != connection || isTerminalState(task.state) {
			continue
		}
		if task.turnID != turnID || !notificationMatchesTask(task, notification, threadID) {
			continue
		}
		if found != nil {
			return nil
		}
		found = task
	}
	return found
}

func notificationMatchesSession(task *task, notification appserver.Notification) bool {
	if notification.SessionGroupID != "" && task.sessionGroupID != notification.SessionGroupID {
		return false
	}
	return true
}

func notificationMatchesTask(task *task, notification appserver.Notification, threadID string) bool {
	return notificationMatchesSession(task, notification) && notificationMatchesThread(task, threadID)
}

func notificationMatchesThread(task *task, threadID string) bool {
	if threadID != "" && task.threadID != threadID {
		return false
	}
	return true
}

func parseTurnStatus(raw json.RawMessage) string {
	var payload struct {
		Turn struct {
			Status string `json:"status"`
		} `json:"turn"`
		Status string `json:"status"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	if payload.Turn.Status != "" {
		return payload.Turn.Status
	}
	return payload.Status
}

func terminalForTurnStatus(status string) (domain.TaskTerminalEvent, bool) {
	switch status {
	case "completed", "succeeded", "success":
		return domain.TaskTerminalEvent{TerminalState: domain.TerminalStateCompleted}, true
	case "interrupted", "cancelled", "canceled":
		return domain.TaskTerminalEvent{TerminalState: domain.TerminalStateInterrupted}, true
	case "failed":
		return domain.TaskTerminalEvent{
			TerminalState: domain.TerminalStateFailed,
			ErrorMessage:  "turn failed",
		}, true
	default:
		return domain.TaskTerminalEvent{
			TerminalState: domain.TerminalStateFailed,
			ErrorMessage:  "turn failed",
		}, false
	}
}

func stateForTerminal(terminal domain.TerminalState) domain.TaskState {
	switch terminal {
	case domain.TerminalStateCompleted:
		return domain.TaskStateCompleted
	case domain.TerminalStateInterrupted:
		return domain.TaskStateInterrupted
	case domain.TerminalStateFailed:
		return domain.TaskStateFailed
	default:
		return domain.TaskStateFailed
	}
}
