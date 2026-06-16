package tasks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Dirard/codex-runtime/gateway/internal/appserver"
	"github.com/Dirard/codex-runtime/gateway/internal/config"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

type interruptTarget struct {
	taskID              string
	sessionGroupID      string
	turnID              string
	connection          *appserver.Connection
	stateEvent          domain.TaskEvent
	stateSubscribers    []*taskSubscriber
	alreadyDone         *domain.InterruptTaskResponse
	alreadyInterrupting *domain.InterruptTaskResponse
	preTurnRecorded     *domain.InterruptTaskResponse
}

func (s *Service) nextTaskIDLocked() string {
	s.nextTaskSeq++
	return fmt.Sprintf("%s%d", taskIDPrefix, s.nextTaskSeq)
}

func (s *Service) setTaskConnection(taskID string, connection *appserver.Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task := s.tasks[taskID]; task != nil {
		task.connection = connection
	}
}

func (s *Service) taskByID(taskID string) *task {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tasks[taskID]
}

func (s *Service) setTaskPhase(taskID string, phase startPhase) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task := s.tasks[taskID]; task != nil && !isTerminalState(task.state) {
		task.phase = phase
	}
}

func (s *Service) cancelRequestedBeforeThread(taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[taskID]
	if task == nil {
		return false
	}
	return task.cancelBeforeTurn && task.threadID == ""
}

// registerConnectionCancel returns true if a pre-thread interrupt already won
// before the connection-acquisition cancel function could be stored.
func (s *Service) registerConnectionCancel(taskID string, cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[taskID]
	if task == nil {
		return false
	}
	if task.cancelBeforeTurn && task.threadID == "" {
		return true
	}
	task.connectionCancel = cancel
	return false
}

func (s *Service) clearConnectionCancel(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task := s.tasks[taskID]; task != nil {
		task.connectionCancel = nil
	}
}

func (s *Service) cancelRequestedBeforeTurn(taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[taskID]
	if task == nil {
		return false
	}
	task.phase = startPhaseBeforeTurn
	return task.cancelBeforeTurn
}

func (s *Service) confirmThreadStarted(taskID string, threadID string) {
	s.mu.Lock()
	task := s.tasks[taskID]
	var event domain.TaskEvent
	var subscribers []*taskSubscriber
	if task != nil && task.threadID == "" && !isTerminalState(task.state) {
		task.threadID = threadID
		event, subscribers = s.appendEventLocked(task, domain.TaskLifecycleEvent{
			LifecycleEvent: domain.TaskLifecycleEventThreadStarted,
			State:          task.state,
		})
	}
	s.mu.Unlock()
	s.publishEvent(taskID, subscribers, event, false)
}

func (s *Service) completeStartSuccess(taskID string, promise *startPromise, response domain.StartTaskResponse) {
	var publications []publication
	s.mu.Lock()
	task := s.tasks[taskID]
	if task != nil && task.startEntry != nil && task.startEntry.state != startEntryPending {
		s.mu.Unlock()
		return
	}
	if task != nil && task.startEntry != nil {
		task.startEntry.state = startEntrySucceeded
		task.startEntry.result = response
		task.startEntry.err = nil
		task.startEntry.updatedAt = s.now()
	}
	promise.result = response
	promise.err = nil
	close(promise.done)
	publications = append(publications, s.appendDeferredUnsupportedWarningPublicationsLocked(task)...)
	s.mu.Unlock()
	s.publishPublications(publications)
}

func (s *Service) completeStartFailure(taskID string, taskSnapshot *task, entry *startEntry, promise *startPromise, err error) {
	s.mu.Lock()
	task := taskSnapshot
	if task == nil && taskID != "" {
		task = s.tasks[taskID]
	}
	if task != nil {
		entry = task.startEntry
		if entry != nil && entry.state != startEntryPending {
			s.mu.Unlock()
			return
		}
		if isTerminalState(task.state) {
			s.completeStartPromiseForTerminalTaskLocked(task, entry, promise, err)
			s.mu.Unlock()
			return
		}
		if task.cancelBeforeTurn && task.turnID == "" {
			event, subscribers := s.completePreTurnInterruptLocked(task, promise)
			s.mu.Unlock()
			s.publishEvent(task.id, subscribers, event, true)
			return
		}
		task.phase = startPhaseTerminal
		task.state = domain.TaskStateFailed
		terminal := domain.TaskTerminalEvent{
			TerminalState: domain.TerminalStateFailed,
			ErrorMessage:  startFailureDisplayMessage,
		}
		task.terminal = &terminal
		task.terminalAt = s.now()
		s.clearActiveLocked(task)
		payloads := s.flushTaskStreamsLocked(task)
		payloads = append(payloads, s.clearPendingLocked(task, domain.PendingResolutionFailed)...)
		payloads = append(payloads, terminal)
		publications := s.appendPublicationsLocked(task, payloads, true)
		if entry != nil {
			entry.state = startEntryFailed
			entry.err = err
			entry.updatedAt = s.now()
		}
		promise.err = err
		close(promise.done)
		s.mu.Unlock()
		s.publishPublications(publications)
		return
	}
	if entry != nil {
		if entry.state != startEntryPending {
			s.mu.Unlock()
			return
		}
		entry.state = startEntryFailed
		entry.err = err
		entry.updatedAt = s.now()
	}
	promise.err = err
	close(promise.done)
	s.mu.Unlock()
}

func (s *Service) completeStartPromiseForTerminalTaskLocked(task *task, entry *startEntry, promise *startPromise, err error) {
	if task.threadID != "" && task.turnID != "" {
		response := statusStartResponseLocked(task)
		if entry != nil {
			entry.state = startEntrySucceeded
			entry.result = response
			entry.err = nil
			entry.updatedAt = s.now()
		}
		promise.result = response
		promise.err = nil
		close(promise.done)
		return
	}

	safeErr := safeStartError(err, task)
	if entry != nil {
		entry.state = startEntryFailed
		entry.err = safeErr
		entry.updatedAt = s.now()
	}
	promise.err = safeErr
	close(promise.done)
}

func (s *Service) completePreTurnInterrupt(taskID string, promise *startPromise) {
	s.mu.Lock()
	task := s.tasks[taskID]
	if task == nil {
		promise.err = internalError("", taskID, "")
		close(promise.done)
		s.mu.Unlock()
		return
	}
	if task.startEntry != nil && task.startEntry.state != startEntryPending {
		s.mu.Unlock()
		return
	}
	event, subscribers := s.completePreTurnInterruptLocked(task, promise)
	s.mu.Unlock()
	s.publishEvent(task.id, subscribers, event, true)
}

func (s *Service) completePreTurnInterruptLocked(task *task, promise *startPromise) (domain.TaskEvent, []*taskSubscriber) {
	task.phase = startPhaseTerminal
	task.state = domain.TaskStateInterrupted
	task.connectionCancel = nil
	terminal := domain.TaskTerminalEvent{
		TerminalState: domain.TerminalStateInterrupted,
		ResultSummary: preTurnInterruptMessage,
	}
	task.terminal = &terminal
	task.terminalAt = s.now()
	s.clearActiveLocked(task)
	event, subscribers := s.appendEventLocked(task, terminal)
	err := startInterruptedBeforeTurn(task)
	if task.startEntry != nil {
		task.startEntry.state = startEntryInterruptedBeforeTurn
		task.startEntry.err = err
		task.startEntry.updatedAt = s.now()
	}
	promise.err = err
	close(promise.done)
	return event, subscribers
}

func threadBindingMapKey(sessionGroupID string, threadID string) threadBindingKey {
	return threadBindingKey{sessionGroupID: sessionGroupID, threadID: threadID}
}

func (s *Service) clearActiveLocked(task *task) {
	session := s.sessions[task.sessionGroupID]
	if session != nil && session.activeTaskID == task.id {
		session.activeTaskID = ""
	}
}

func (s *Service) verifyThreadBinding(sessionGroupID string, workspaceID string, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupRetainedLocked()

	binding := s.bindings[threadBindingMapKey(sessionGroupID, threadID)]
	if binding == nil {
		if s.bindingTombstoneMatchesLocked(sessionGroupID, workspaceID, threadID) {
			return expiredThreadBinding(sessionGroupID, threadID)
		}
		return unknownThreadBinding(sessionGroupID, threadID)
	}
	if binding.sessionGroupID != sessionGroupID || binding.workspaceID != workspaceID {
		return unknownThreadBinding(sessionGroupID, threadID)
	}
	if s.now().After(binding.expiresAt) {
		s.removeBindingLocked(binding)
		return expiredThreadBinding(sessionGroupID, threadID)
	}
	return nil
}

func (s *Service) upsertThreadBinding(session *sessionRuntime, threadID string, taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	ttl := bindingTTL(session.group)
	key := threadBindingMapKey(session.group.SessionGroupID, threadID)
	binding := s.bindings[key]
	created := false
	if binding == nil {
		binding = &threadBinding{
			threadID:       threadID,
			sessionGroupID: session.group.SessionGroupID,
			workspaceID:    session.group.WorkspaceID,
			createdAt:      now,
		}
		s.bindings[key] = binding
		session.bindings[threadID] = binding
		delete(s.tombstones, key)
		created = true
	}
	binding.taskID = taskID
	binding.expiresAt = now.Add(ttl)
	s.enforceBindingCapLocked(session)
	return created
}

func (s *Service) enforceBindingCapLocked(session *sessionRuntime) {
	capacity := bindingCap(session.group)
	for len(session.bindings) > capacity {
		var oldest *threadBinding
		for _, binding := range session.bindings {
			if s.bindingIsActiveLocked(binding) {
				continue
			}
			if oldest == nil || binding.createdAt.Before(oldest.createdAt) {
				oldest = binding
			}
		}
		if oldest == nil {
			return
		}
		s.removeBindingLocked(oldest)
	}
}

func (s *Service) bindingIsActiveLocked(binding *threadBinding) bool {
	session := s.sessions[binding.sessionGroupID]
	if session == nil || session.activeTaskID == "" {
		return false
	}
	task := s.tasks[session.activeTaskID]
	return task != nil && task.threadID == binding.threadID && !isTerminalState(task.state)
}

func (s *Service) removeBindingLocked(binding *threadBinding) {
	s.tombstoneBindingLocked(binding)
	delete(s.bindings, threadBindingMapKey(binding.sessionGroupID, binding.threadID))
	if session := s.sessions[binding.sessionGroupID]; session != nil {
		delete(session.bindings, binding.threadID)
	}
}

func (s *Service) removeThreadBindingForTaskIfCreated(taskID string, threadID string, created bool) {
	if !created || threadID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[taskID]
	if task == nil {
		return
	}
	binding := s.bindings[threadBindingMapKey(task.sessionGroupID, threadID)]
	if binding != nil && binding.taskID == taskID {
		s.removeBindingLocked(binding)
	}
}

func (s *Service) tombstoneBindingLocked(binding *threadBinding) {
	now := s.now()
	s.tombstones[threadBindingMapKey(binding.sessionGroupID, binding.threadID)] = &threadBindingTombstone{
		threadID:       binding.threadID,
		sessionGroupID: binding.sessionGroupID,
		workspaceID:    binding.workspaceID,
		createdAt:      now,
		expiresAt:      now.Add(s.bindingTombstoneTTLLocked(binding.sessionGroupID)),
	}
	s.cleanupBindingTombstonesLocked()
}

func (s *Service) bindingTombstoneMatchesLocked(sessionGroupID string, workspaceID string, threadID string) bool {
	key := threadBindingMapKey(sessionGroupID, threadID)
	tombstone := s.tombstones[key]
	if tombstone == nil || tombstone.sessionGroupID != sessionGroupID || tombstone.workspaceID != workspaceID {
		return false
	}
	if s.now().After(tombstone.expiresAt) {
		delete(s.tombstones, key)
		return false
	}
	return true
}

func (s *Service) cleanupBindingTombstonesLocked() {
	now := s.now()
	for key, tombstone := range s.tombstones {
		if now.After(tombstone.expiresAt) {
			delete(s.tombstones, key)
		}
	}
	for sessionGroupID := range s.sessions {
		s.enforceBindingTombstoneCapLocked(sessionGroupID)
	}
}

func (s *Service) enforceBindingTombstoneCapLocked(sessionGroupID string) {
	capacity := defaultThreadBindingCap
	if session := s.sessions[sessionGroupID]; session != nil {
		capacity = bindingCap(session.group)
	}
	for s.bindingTombstoneCountLocked(sessionGroupID) > capacity {
		var oldest *threadBindingTombstone
		for _, tombstone := range s.tombstones {
			if tombstone.sessionGroupID != sessionGroupID {
				continue
			}
			if oldest == nil || tombstone.createdAt.Before(oldest.createdAt) {
				oldest = tombstone
			}
		}
		if oldest == nil {
			return
		}
		delete(s.tombstones, threadBindingMapKey(oldest.sessionGroupID, oldest.threadID))
	}
}

func (s *Service) bindingTombstoneCountLocked(sessionGroupID string) int {
	count := 0
	for _, tombstone := range s.tombstones {
		if tombstone.sessionGroupID == sessionGroupID {
			count++
		}
	}
	return count
}

func (s *Service) bindingTombstoneTTLLocked(sessionGroupID string) time.Duration {
	session := s.sessions[sessionGroupID]
	if session == nil {
		return defaultThreadBindingTTL
	}
	return bindingTTL(session.group)
}

func bindingTTL(group config.SessionGroup) time.Duration {
	if group.ThreadBindingLimits.TTLMillis <= 0 {
		return defaultThreadBindingTTL
	}
	return time.Duration(group.ThreadBindingLimits.TTLMillis) * time.Millisecond
}

func bindingCap(group config.SessionGroup) int {
	if group.ThreadBindingLimits.MaxBindings <= 0 {
		return defaultThreadBindingCap
	}
	return group.ThreadBindingLimits.MaxBindings
}

func (s *Service) cleanupRetainedLocked() {
	now := s.now()
	for taskID, task := range s.tasks {
		if !isTerminalState(task.state) {
			continue
		}
		retainedAt := task.terminalAt
		if retainedAt.IsZero() {
			retainedAt = task.createdAt
		}
		if !now.After(retainedAt.Add(s.replayRetentionTTLLocked(task.sessionGroupID))) {
			continue
		}
		delete(s.tasks, taskID)
		if task.startEntry != nil {
			delete(s.idempotency, task.startEntry.key)
		}
	}
	for key, entry := range s.idempotency {
		if entry.state == startEntryPending {
			continue
		}
		if _, exists := s.tasks[entry.taskID]; exists {
			continue
		}
		if now.After(entry.updatedAt.Add(s.replayRetentionTTLLocked(key.sessionGroupID))) {
			delete(s.idempotency, key)
		}
	}
	s.cleanupBindingTombstonesLocked()
}

func (s *Service) replayRetentionTTLLocked(sessionGroupID string) time.Duration {
	session := s.sessions[sessionGroupID]
	if session == nil {
		return defaultReplayRetentionTTL
	}
	return replayRetentionTTL(session.group)
}

func replayRetentionTTL(group config.SessionGroup) time.Duration {
	if group.ReplayLimits.TTLMillis <= 0 {
		return defaultReplayRetentionTTL
	}
	return time.Duration(group.ReplayLimits.TTLMillis) * time.Millisecond
}

func (s *Service) replayRetentionEventCapLocked(sessionGroupID string) int {
	session := s.sessions[sessionGroupID]
	if session == nil {
		return defaultReplayRetentionEvents
	}
	return replayRetentionEventCap(session.group)
}

func replayRetentionEventCap(group config.SessionGroup) int {
	if group.ReplayLimits.MaxEvents <= 0 {
		return defaultReplayRetentionEvents
	}
	return group.ReplayLimits.MaxEvents
}

func (s *Service) replayRetentionByteCapLocked(sessionGroupID string) int64 {
	session := s.sessions[sessionGroupID]
	if session == nil {
		return defaultReplayRetentionBytes
	}
	return replayRetentionByteCap(session.group)
}

func replayRetentionByteCap(group config.SessionGroup) int64 {
	if group.ReplayLimits.MaxBytes <= 0 {
		return defaultReplayRetentionBytes
	}
	return group.ReplayLimits.MaxBytes
}

func (s *Service) taskForLocatorLocked(locator domain.TaskLocator) (*task, error) {
	switch locator.Kind {
	case domain.TaskLocatorByTaskID:
		task := s.tasks[locator.TaskID]
		if task == nil {
			return nil, unknownTask("", locator.TaskID, "")
		}
		return task, nil
	case domain.TaskLocatorByClientMessage:
		entry := s.idempotency[idempotencyKey{
			sessionGroupID:  locator.ClientMessageLocator.SessionGroupID,
			clientMessageID: locator.ClientMessageLocator.ClientMessageID,
		}]
		if entry == nil {
			return nil, unknownTask(locator.ClientMessageLocator.SessionGroupID, "", locator.ClientMessageLocator.ClientMessageID)
		}
		task := s.tasks[entry.taskID]
		if task == nil {
			return nil, unknownTask(locator.ClientMessageLocator.SessionGroupID, entry.taskID, locator.ClientMessageLocator.ClientMessageID)
		}
		return task, nil
	case domain.TaskLocatorByThread:
		var active *task
		var newestTerminal *task
		for _, task := range s.tasks {
			if task.sessionGroupID != locator.ThreadLocator.SessionGroupID || task.threadID != locator.ThreadLocator.ThreadID {
				continue
			}
			if !isTerminalState(task.state) {
				if active != nil {
					return nil, ambiguousLocator(locator.ThreadLocator.SessionGroupID, locator.ThreadLocator.ThreadID)
				}
				active = task
				continue
			}
			if newestTerminal == nil || taskRetainedAtLocked(task).After(taskRetainedAtLocked(newestTerminal)) {
				newestTerminal = task
			}
		}
		if active != nil {
			return active, nil
		}
		if newestTerminal == nil {
			return nil, unknownTask(locator.ThreadLocator.SessionGroupID, "", "")
		}
		return newestTerminal, nil
	default:
		return nil, invalidLocator()
	}
}

func taskRetainedAtLocked(task *task) time.Time {
	if task.terminalAt.IsZero() {
		return task.createdAt
	}
	return task.terminalAt
}

func statusFromTaskLocked(task *task) domain.GetTaskStatusResponse {
	response := domain.GetTaskStatusResponse{
		TaskID:                    task.id,
		State:                     task.state,
		SessionGroupID:            task.sessionGroupID,
		WorkspaceID:               task.workspaceID,
		ThreadID:                  task.threadID,
		TurnID:                    task.turnID,
		LastEventID:               task.nextEventID,
		FromStartAvailable:        task.startEvictedBeforeEventID == 0,
		StartEvictedBeforeEventID: task.startEvictedBeforeEventID,
		Terminal:                  copyTerminal(task.terminal),
	}
	if task.pending != nil {
		response.ActivePendingRequests = task.pending.Active()
	}
	if len(task.events) > 0 {
		response.OldestBufferedEventID = task.events[0].EventID
		response.NewestBufferedEventID = task.events[len(task.events)-1].EventID
	}
	return response
}

func statusStartResponseLocked(task *task) domain.StartTaskResponse {
	return domain.StartTaskResponse{
		TaskID:         task.id,
		ThreadID:       task.threadID,
		TurnID:         task.turnID,
		SessionGroupID: task.sessionGroupID,
		State:          task.state,
		LastEventID:    task.nextEventID,
	}
}

func copyTerminal(terminal *domain.TaskTerminalEvent) *domain.TaskTerminalEvent {
	if terminal == nil {
		return nil
	}
	copied := *terminal
	return &copied
}

func isTerminalState(state domain.TaskState) bool {
	switch state {
	case domain.TaskStateCompleted, domain.TaskStateFailed, domain.TaskStateInterrupted:
		return true
	default:
		return false
	}
}

func safeStartError(err error, task *task) error {
	if task == nil {
		return err
	}
	var gatewayErr *domain.GatewayError
	if errors.As(err, &gatewayErr) {
		details := gatewayErr.Details
		details.TaskID = task.id
		details.SessionGroupID = task.sessionGroupID
		details.ClientMessageID = task.clientMessageID
		return &domain.GatewayError{Code: gatewayErr.Code, Details: details}
	}
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeUnavailable,
		Details: domain.GatewayErrorDetails{
			Reason:          domain.ReasonDispatcherUnavailable,
			DisplayMessage:  appServerCallFailedMessage,
			TaskID:          task.id,
			SessionGroupID:  task.sessionGroupID,
			ClientMessageID: task.clientMessageID,
			Retryable:       true,
		},
	}
}

func safeInterruptError(err error, sessionGroupID string, taskID string) error {
	var gatewayErr *domain.GatewayError
	if errors.As(err, &gatewayErr) {
		details := gatewayErr.Details
		details.TaskID = taskID
		details.SessionGroupID = sessionGroupID
		return &domain.GatewayError{Code: gatewayErr.Code, Details: details}
	}
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeUnavailable,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonDispatcherUnavailable,
			DisplayMessage: appServerCallFailedMessage,
			TaskID:         taskID,
			SessionGroupID: sessionGroupID,
			Retryable:      true,
		},
	}
}
