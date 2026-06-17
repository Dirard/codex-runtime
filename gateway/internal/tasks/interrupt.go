package tasks

import (
	"context"
	"errors"

	"github.com/Dirard/codex-runtime/gateway/internal/appserver"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

const (
	interruptRejectedWarningCode    = "interrupt_rejected"
	interruptRejectedWarningMessage = "interrupt request was rejected; task is still running"
)

func (s *Service) interruptTarget(locator domain.TaskLocator) (interruptTarget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupRetainedLocked()

	task, err := s.taskForLocatorLocked(locator)
	if err != nil {
		return interruptTarget{}, err
	}
	response := interruptResponseLocked(task)
	if isTerminalState(task.state) {
		response.AlreadyTerminal = true
		return interruptTarget{alreadyDone: &response}, nil
	}
	if task.state == domain.TaskStateInterrupting {
		response.AlreadyInterrupting = true
		return interruptTarget{alreadyInterrupting: &response}, nil
	}
	if task.turnID == "" {
		task.cancelBeforeTurn = true
		if task.connectionCancel != nil {
			task.connectionCancel()
		}
		response.PreTurnCancelRecorded = true
		return interruptTarget{preTurnRecorded: &response}, nil
	}
	if task.connection == nil || task.threadID == "" {
		return interruptTarget{}, internalError(task.sessionGroupID, task.id, task.clientMessageID)
	}
	task.interruptSent = true
	task.preInterruptState = task.state
	task.state = domain.TaskStateInterrupting
	event, subscribers := s.appendEventLocked(task, domain.TaskLifecycleEvent{
		LifecycleEvent: domain.TaskLifecycleEventStateChanged,
		State:          domain.TaskStateInterrupting,
	})
	response = interruptResponseLocked(task)
	response.InterruptSent = true
	target := interruptTarget{
		taskID:           task.id,
		sessionGroupID:   task.sessionGroupID,
		threadID:         task.threadID,
		turnID:           task.turnID,
		connection:       task.connection,
		stateEvent:       event,
		stateSubscribers: subscribers,
	}
	return target, nil
}

func (s *Service) interruptAccepted(taskID string) domain.InterruptTaskResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[taskID]
	if task == nil {
		return domain.InterruptTaskResponse{}
	}
	response := interruptResponseLocked(task)
	response.InterruptSent = true
	return response
}

func (s *Service) rollbackInterrupt(taskID string) {
	s.mu.Lock()
	task := s.tasks[taskID]
	if task == nil || isTerminalState(task.state) {
		s.mu.Unlock()
		return
	}
	task.interruptSent = false
	task.state = task.preInterruptState
	if task.state == "" || task.state == domain.TaskStateInterrupting {
		task.state = domain.TaskStateRunning
	}
	task.preInterruptState = ""
	event, subscribers := s.appendEventLocked(task, domain.GatewayWarningEvent{
		Code:    interruptRejectedWarningCode,
		Message: interruptRejectedWarningMessage,
	})
	s.mu.Unlock()
	s.publishEvent(task.id, subscribers, event, false)
}

func (s *Service) sendInterruptAfterTurnStart(connection *appserver.Connection, taskID string, threadID string, turnID string) {
	if connection == nil || threadID == "" || turnID == "" {
		return
	}
	_, err := connection.InterruptTurn(context.Background(), appserver.TurnInterruptCall{
		ThreadID: threadID,
		TurnID:   turnID,
		TaskID:   taskID,
		Timeout:  s.turnInterruptTimeout,
	})
	if err != nil && shouldRollbackInterrupt(err) {
		s.rollbackInterrupt(taskID)
	}
}

func shouldRollbackInterrupt(err error) bool {
	return !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, appserver.ErrDispatcherClosed)
}

func interruptResponseLocked(task *task) domain.InterruptTaskResponse {
	return domain.InterruptTaskResponse{
		TaskID:         task.id,
		SessionGroupID: task.sessionGroupID,
		ThreadID:       task.threadID,
		TurnID:         task.turnID,
		State:          task.state,
		LastEventID:    task.nextEventID,
	}
}
