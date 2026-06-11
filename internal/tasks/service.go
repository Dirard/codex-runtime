package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Dirard/codex-runtime/internal/appserver"
	"github.com/Dirard/codex-runtime/internal/config"
	"github.com/Dirard/codex-runtime/internal/contextpack"
	"github.com/Dirard/codex-runtime/internal/domain"
	"github.com/Dirard/codex-runtime/internal/grpcapi"
	"github.com/Dirard/codex-runtime/internal/pending"
	"github.com/Dirard/codex-runtime/internal/redact"
)

const (
	defaultThreadBindingTTL      = 24 * time.Hour
	defaultThreadBindingCap      = 1_000
	defaultReplayRetentionTTL    = 30 * time.Minute
	defaultReplayRetentionEvents = 2_000
	defaultReplayRetentionBytes  = 8 * 1024 * 1024
	taskIDPrefix                 = "task-"
	startFailureDisplayMessage   = "task start failed"
	preTurnInterruptMessage      = "task was interrupted before turn start"
	appServerCallFailedMessage   = "app-server call failed"
	threadBindingExpiredMessage  = "thread binding expired"
)

type AppServerSupervisor interface {
	Connection(context.Context) (*appserver.Connection, error)
	MarkClosed(*appserver.Connection)
}

type Session struct {
	Group      config.SessionGroup
	Supervisor AppServerSupervisor
}

type Service struct {
	mu sync.Mutex

	sessions               map[string]*sessionRuntime
	tasks                  map[string]*task
	idempotency            map[idempotencyKey]*startEntry
	bindings               map[threadBindingKey]*threadBinding
	tombstones             map[threadBindingKey]*threadBindingTombstone
	monitors               map[*appserver.Connection]struct{}
	resolvedServerRequests map[*appserver.Connection]map[string]struct{}
	connectionDiagnostics  []connectionDiagnostic
	nextTaskSeq            uint64
	now                    func() time.Time

	threadCallTimeout    time.Duration
	turnStartTimeout     time.Duration
	turnInterruptTimeout time.Duration
}

type sessionRuntime struct {
	group        config.SessionGroup
	supervisor   AppServerSupervisor
	activeTaskID string
	bindings     map[string]*threadBinding
}

type threadBindingKey struct {
	sessionGroupID string
	threadID       string
}

type idempotencyKey struct {
	sessionGroupID  string
	clientMessageID string
}

type startEntryState string

const (
	startEntryPending               startEntryState = "start_pending"
	startEntrySucceeded             startEntryState = "start_succeeded"
	startEntryFailed                startEntryState = "start_failed"
	startEntryInterruptedBeforeTurn startEntryState = "start_interrupted_before_turn"
)

type startEntry struct {
	key         idempotencyKey
	fingerprint string
	taskID      string
	state       startEntryState
	promise     *startPromise
	result      domain.StartTaskResponse
	err         error
	updatedAt   time.Time
}

type startPromise struct {
	done   chan struct{}
	result domain.StartTaskResponse
	err    error
}

type startPhase string

const (
	startPhaseClaimed       startPhase = "claimed"
	startPhaseThreadCalling startPhase = "thread_calling"
	startPhaseBeforeTurn    startPhase = "before_turn"
	startPhaseTurnCalling   startPhase = "turn_calling"
	startPhaseStarted       startPhase = "started"
	startPhaseTerminal      startPhase = "terminal"
)

type task struct {
	id               string
	sessionGroupID   string
	workspaceID      string
	clientMessageID  string
	threadID         string
	turnID           string
	state            domain.TaskState
	phase            startPhase
	connection       *appserver.Connection
	connectionCancel context.CancelFunc
	sensitive        *redact.Registry
	pathSanitizer    *redact.PathSanitizer

	cancelBeforeTurn  bool
	interruptSent     bool
	preInterruptState domain.TaskState

	startEntry *startEntry
	terminal   *domain.TaskTerminalEvent

	createdAt  time.Time
	terminalAt time.Time

	nextEventID                 uint64
	events                      []domain.TaskEvent
	retainedEventBytes          int64
	startEvictedBeforeEventID   uint64
	deferredUnsupportedWarnings int
	deferredUnsupportedMethod   string
	deferredUnsupportedMultiple bool
	deferredUnsupportedOverflow bool
	itemBindings                map[string]struct{}
	pending                     *pending.Manager
	fileDiffs                   map[string]domain.FileDiffUpdatedEvent
	streams                     map[taskStreamKey]*redact.Stream
	subscribers                 map[uint64]*taskSubscriber
	nextSubscriberID            uint64
}

type threadBinding struct {
	threadID       string
	sessionGroupID string
	workspaceID    string
	taskID         string
	createdAt      time.Time
	expiresAt      time.Time
}

type threadBindingTombstone struct {
	threadID       string
	sessionGroupID string
	workspaceID    string
	createdAt      time.Time
	expiresAt      time.Time
}

func NewService(sessions []Session) (*Service, error) {
	service := &Service{
		sessions:               make(map[string]*sessionRuntime, len(sessions)),
		tasks:                  map[string]*task{},
		idempotency:            map[idempotencyKey]*startEntry{},
		bindings:               map[threadBindingKey]*threadBinding{},
		tombstones:             map[threadBindingKey]*threadBindingTombstone{},
		monitors:               map[*appserver.Connection]struct{}{},
		resolvedServerRequests: map[*appserver.Connection]map[string]struct{}{},
		now:                    time.Now,
	}
	for _, session := range sessions {
		if session.Group.SessionGroupID == "" {
			return nil, fmt.Errorf("session group id is required")
		}
		if session.Supervisor == nil {
			return nil, fmt.Errorf("session group %q supervisor is required", session.Group.SessionGroupID)
		}
		if _, exists := service.sessions[session.Group.SessionGroupID]; exists {
			return nil, fmt.Errorf("duplicate session group %q", session.Group.SessionGroupID)
		}
		group := session.Group
		service.sessions[group.SessionGroupID] = &sessionRuntime{
			group:      group,
			supervisor: session.Supervisor,
			bindings:   map[string]*threadBinding{},
		}
	}
	return service, nil
}

func (s *Service) configureConnectionHooks(session *sessionRuntime, connection *appserver.Connection) {
	if session == nil || connection == nil {
		return
	}
	sessionGroupID := session.group.SessionGroupID
	activeTaskID := func() string {
		s.mu.Lock()
		defer s.mu.Unlock()
		runtime := s.sessions[sessionGroupID]
		if runtime == nil {
			return ""
		}
		return runtime.activeTaskID
	}
	connection.SetAuthRefreshFailureHooks(activeTaskID, s.handleAuthRefreshFailure)
	connection.SetUnsupportedServerRequestHook(func(request appserver.UnsupportedServerRequest) {
		s.handleUnsupportedServerRequest(request, connection)
	})
}

func (s *Service) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now == nil {
		s.now = time.Now
		return
	}
	s.now = now
}

func (s *Service) StartTask(ctx context.Context, command domain.StartTaskCommand) (domain.StartTaskResponse, error) {
	if command.ClientMessageID == "" {
		return domain.StartTaskResponse{}, invalidArgument(command.SessionGroupID, command.ClientMessageID)
	}
	command, envelope, err := s.prepareStart(command)
	if err != nil {
		return domain.StartTaskResponse{}, err
	}
	fingerprint, err := domain.StartTaskFingerprintV1SHA256Hex(command)
	if err != nil {
		return domain.StartTaskResponse{}, invalidArgument(command.SessionGroupID, command.ClientMessageID)
	}
	if err := callerErrorFromContext(ctx, command.SessionGroupID, command.ClientMessageID); err != nil {
		return domain.StartTaskResponse{}, err
	}

	promise, claimed, err := s.claimStart(command, fingerprint)
	if err != nil {
		return domain.StartTaskResponse{}, err
	}
	if claimed {
		go s.runStart(command, envelope, promise)
	}
	return waitForStart(ctx, promise, command.SessionGroupID, command.ClientMessageID)
}

func (s *Service) prepareStart(command domain.StartTaskCommand) (domain.StartTaskCommand, string, error) {
	s.mu.Lock()
	session, ok := s.sessions[command.SessionGroupID]
	if !ok {
		s.mu.Unlock()
		return domain.StartTaskCommand{}, "", unknownSession(command.SessionGroupID)
	}
	workspaceID := session.group.WorkspaceID
	s.mu.Unlock()

	if command.WorkspaceID != "" && command.WorkspaceID != workspaceID {
		return domain.StartTaskCommand{}, "", workspaceMismatch(command.SessionGroupID)
	}
	command.WorkspaceID = workspaceID
	envelope, err := contextpack.BuildEnvelope(command.Prompt, command.ContextBlocks)
	if err != nil {
		return domain.StartTaskCommand{}, "", invalidContext(command.SessionGroupID, "", command.ClientMessageID, err)
	}
	return command, envelope, nil
}

func (s *Service) claimStart(command domain.StartTaskCommand, fingerprint string) (*startPromise, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[command.SessionGroupID]
	if !ok {
		return nil, false, unknownSession(command.SessionGroupID)
	}
	s.cleanupRetainedLocked()
	key := idempotencyKey{
		sessionGroupID:  command.SessionGroupID,
		clientMessageID: command.ClientMessageID,
	}
	if entry := s.idempotency[key]; entry != nil {
		if entry.fingerprint != fingerprint {
			return nil, false, idempotencyMismatch(command.SessionGroupID, command.ClientMessageID)
		}
		return entry.promise, false, nil
	}
	if session.activeTaskID != "" {
		return nil, false, alreadyRunning(command.SessionGroupID, session.activeTaskID)
	}

	now := s.now()
	taskID := s.nextTaskIDLocked()
	promise := &startPromise{done: make(chan struct{})}
	pathSanitizer, _ := redact.NewPathSanitizer(session.group.CanonicalCWD)
	entry := &startEntry{
		key:         key,
		fingerprint: fingerprint,
		taskID:      taskID,
		state:       startEntryPending,
		promise:     promise,
		updatedAt:   now,
	}
	task := &task{
		id:              taskID,
		sessionGroupID:  command.SessionGroupID,
		workspaceID:     command.WorkspaceID,
		clientMessageID: command.ClientMessageID,
		state:           domain.TaskStateStarting,
		phase:           startPhaseClaimed,
		sensitive:       redact.NewRegistry(),
		pathSanitizer:   pathSanitizer,
		startEntry:      entry,
		createdAt:       now,
		itemBindings:    map[string]struct{}{},
		pending:         pending.NewManager(),
		fileDiffs:       map[string]domain.FileDiffUpdatedEvent{},
		streams:         map[taskStreamKey]*redact.Stream{},
		subscribers:     map[uint64]*taskSubscriber{},
	}
	entry.taskID = task.id
	s.tasks[task.id] = task
	s.idempotency[key] = entry
	session.activeTaskID = task.id
	s.appendEventLocked(task, domain.TaskLifecycleEvent{
		LifecycleEvent: domain.TaskLifecycleEventTaskStarted,
		State:          domain.TaskStateStarting,
	})
	return promise, true, nil
}

func (s *Service) runStart(command domain.StartTaskCommand, envelope string, promise *startPromise) {
	session, task, entry, err := s.startSnapshot(command)
	if err != nil {
		s.completeStartFailure("", nil, nil, promise, err)
		return
	}
	if command.ThreadID != "" {
		if err := s.verifyThreadBinding(command.SessionGroupID, command.WorkspaceID, command.ThreadID); err != nil {
			s.completeStartFailure(task.id, task, entry, promise, err)
			return
		}
	}

	if s.cancelRequestedBeforeThread(task.id) {
		s.completePreTurnInterrupt(task.id, promise)
		return
	}

	connectionCtx, connectionCancel := context.WithCancel(context.Background())
	if s.registerConnectionCancel(task.id, connectionCancel) {
		connectionCancel()
		s.completePreTurnInterrupt(task.id, promise)
		return
	}
	connection, err := session.supervisor.Connection(connectionCtx)
	connectionCanceled := connectionCtx.Err() != nil
	s.clearConnectionCancel(task.id)
	connectionCancel()
	if err != nil {
		if connectionCanceled && s.cancelRequestedBeforeThread(task.id) {
			s.completePreTurnInterrupt(task.id, promise)
			return
		}
		s.completeStartFailure(task.id, task, entry, promise, safeStartError(err, task))
		return
	}
	s.configureConnectionHooks(session, connection)
	s.ensureMonitor(connection, session.supervisor)
	s.setTaskConnection(task.id, connection)

	if s.cancelRequestedBeforeThread(task.id) {
		s.completePreTurnInterrupt(task.id, promise)
		return
	}

	threadID, bindingCreated, err := s.startOrResumeThread(command, session, task.id, connection)
	if err != nil {
		s.completeStartFailure(task.id, task, entry, promise, err)
		return
	}
	s.confirmThreadStarted(task.id, threadID)

	if s.cancelRequestedBeforeTurn(task.id) {
		s.completePreTurnInterrupt(task.id, promise)
		return
	}

	s.setTaskPhase(task.id, startPhaseTurnCalling)
	turnResult, err := connection.StartTurn(context.Background(), appserver.TurnStartCall{
		ThreadID:            threadID,
		ClientUserMessageID: command.ClientMessageID,
		Input: []appserver.UserInputText{
			appserver.NewUserInputText(envelope),
		},
		TaskID:  task.id,
		Timeout: s.turnStartTimeout,
	})
	if err != nil {
		if errors.Is(err, appserver.ErrProtocolMismatch) {
			s.removeThreadBindingForTaskIfCreated(task.id, threadID, bindingCreated)
		}
		s.completeStartFailure(task.id, task, entry, promise, safeStartError(err, task))
		return
	}
	turnID := appserver.ParseTurnID(turnResult)
	if turnID == "" {
		s.removeThreadBindingForTaskIfCreated(task.id, threadID, bindingCreated)
		s.completeStartFailure(task.id, task, entry, promise, protocolMismatch(command.SessionGroupID, task.id))
		return
	}

	response, interruptAfterStart, interruptConnection := s.confirmTurnStartedOnce(task.id, turnID)
	if response.TaskID == "" {
		s.removeThreadBindingForTaskIfCreated(task.id, threadID, bindingCreated)
		s.completeStartFailure(task.id, task, entry, promise, protocolMismatch(command.SessionGroupID, task.id))
		return
	}
	if interruptAfterStart {
		s.sendInterruptAfterTurnStart(interruptConnection, task.id, turnID)
		s.mu.Lock()
		if currentTask := s.tasks[task.id]; currentTask != nil {
			response = statusStartResponseLocked(currentTask)
		}
		s.mu.Unlock()
	}
	s.completeStartSuccess(task.id, promise, response)
}

func (s *Service) confirmTurnStartedOnce(taskID string, turnID string) (domain.StartTaskResponse, bool, *appserver.Connection) {
	s.mu.Lock()
	task := s.tasks[taskID]
	if task == nil {
		s.mu.Unlock()
		return domain.StartTaskResponse{}, false, nil
	}
	if task.turnID != "" {
		if task.turnID != turnID {
			s.mu.Unlock()
			return domain.StartTaskResponse{}, false, nil
		}
		response := statusStartResponseLocked(task)
		s.mu.Unlock()
		return response, false, nil
	}
	task.turnID = turnID
	if task.cancelBeforeTurn {
		task.preInterruptState = domain.TaskStateRunning
		task.state = domain.TaskStateInterrupting
		task.phase = startPhaseStarted
		if !task.interruptSent {
			task.interruptSent = true
			event, subscribers := s.appendEventLocked(task, domain.TaskLifecycleEvent{
				LifecycleEvent: domain.TaskLifecycleEventStateChanged,
				State:          domain.TaskStateInterrupting,
				ReasonCode:     string(domain.ReasonStartInterruptedBeforeTurn),
			})
			response := statusStartResponseLocked(task)
			connection := task.connection
			s.mu.Unlock()
			s.publishEvent(task.id, subscribers, event, false)
			return response, true, connection
		}
		response := statusStartResponseLocked(task)
		connection := task.connection
		s.mu.Unlock()
		return response, false, connection
	}
	task.state = domain.TaskStateRunning
	task.phase = startPhaseStarted
	event, subscribers := s.appendEventLocked(task, domain.TaskLifecycleEvent{
		LifecycleEvent: domain.TaskLifecycleEventTurnStarted,
		State:          domain.TaskStateRunning,
	})
	response := statusStartResponseLocked(task)
	s.mu.Unlock()
	s.publishEvent(task.id, subscribers, event, false)
	return response, false, nil
}

func (s *Service) startSnapshot(command domain.StartTaskCommand) (*sessionRuntime, *task, *startEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[command.SessionGroupID]
	if !ok {
		return nil, nil, nil, unknownSession(command.SessionGroupID)
	}
	key := idempotencyKey{
		sessionGroupID:  command.SessionGroupID,
		clientMessageID: command.ClientMessageID,
	}
	entry := s.idempotency[key]
	if entry == nil {
		return nil, nil, nil, internalError(command.SessionGroupID, "", "")
	}
	task := s.tasks[entry.taskID]
	if task == nil {
		return nil, nil, nil, internalError(command.SessionGroupID, entry.taskID, command.ClientMessageID)
	}
	return session, task, entry, nil
}

func (s *Service) startOrResumeThread(command domain.StartTaskCommand, session *sessionRuntime, taskID string, connection *appserver.Connection) (string, bool, error) {
	s.setTaskPhase(taskID, startPhaseThreadCalling)
	var result json.RawMessage
	var err error
	if command.ThreadID == "" {
		result, err = connection.StartThread(context.Background(), appserver.ThreadStartCall{
			TaskID:  taskID,
			Timeout: s.threadCallTimeout,
		})
	} else {
		result, err = connection.ResumeThread(context.Background(), appserver.ThreadResumeCall{
			ThreadID: command.ThreadID,
			TaskID:   taskID,
			Timeout:  s.threadCallTimeout,
		})
	}
	if err != nil {
		return "", false, safeStartError(err, s.taskByID(taskID))
	}
	threadID := appserver.ParseThreadID(result)
	if threadID == "" {
		return "", false, protocolMismatch(command.SessionGroupID, taskID)
	}
	if command.ThreadID != "" && threadID != command.ThreadID {
		return "", false, protocolMismatch(command.SessionGroupID, taskID)
	}
	bindingCreated := s.upsertThreadBinding(session, threadID, taskID)
	return threadID, bindingCreated, nil
}

func waitForStart(ctx context.Context, promise *startPromise, sessionGroupID string, clientMessageID string) (domain.StartTaskResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-promise.done:
		return promise.result, promise.err
	case <-ctx.Done():
		return domain.StartTaskResponse{}, callerErrorFromContext(ctx, sessionGroupID, clientMessageID)
	}
}

func callerErrorFromContext(ctx context.Context, sessionGroupID string, clientMessageID string) error {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return callerDeadline(sessionGroupID, clientMessageID)
	}
	return callerCanceled(sessionGroupID, clientMessageID)
}

func (s *Service) InterruptTask(ctx context.Context, command domain.InterruptTaskCommand) (domain.InterruptTaskResponse, error) {
	sessionGroupID := ""
	clientMessageID := ""
	switch command.Locator.Kind {
	case domain.TaskLocatorByClientMessage:
		sessionGroupID = command.Locator.ClientMessageLocator.SessionGroupID
		clientMessageID = command.Locator.ClientMessageLocator.ClientMessageID
	case domain.TaskLocatorByThread:
		sessionGroupID = command.Locator.ThreadLocator.SessionGroupID
	case domain.TaskLocatorByTaskID:
	}
	if err := callerErrorFromContext(ctx, sessionGroupID, clientMessageID); err != nil {
		return domain.InterruptTaskResponse{}, err
	}

	target, err := s.interruptTarget(command.Locator)
	if err != nil {
		return domain.InterruptTaskResponse{}, err
	}
	if target.alreadyDone != nil {
		return *target.alreadyDone, nil
	}
	if target.preTurnRecorded != nil {
		return *target.preTurnRecorded, nil
	}
	if target.alreadyInterrupting != nil {
		return *target.alreadyInterrupting, nil
	}

	s.publishEvent(target.taskID, target.stateSubscribers, target.stateEvent, false)
	// After interruptTarget claims the task, the interrupt is task-owned; caller
	// cancellation must not roll back a request that may already be in flight.
	_, callErr := target.connection.InterruptTurn(context.Background(), appserver.TurnInterruptCall{
		TurnID:  target.turnID,
		TaskID:  target.taskID,
		Timeout: s.turnInterruptTimeout,
	})
	if callErr != nil {
		if shouldRollbackInterrupt(callErr) {
			s.rollbackInterrupt(target.taskID)
		}
		return domain.InterruptTaskResponse{}, safeInterruptError(callErr, target.sessionGroupID, target.taskID)
	}
	return s.interruptAccepted(target.taskID), nil
}

func (s *Service) GetTaskStatus(ctx context.Context, command domain.GetTaskStatusCommand) (domain.GetTaskStatusResponse, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupRetainedLocked()

	task, err := s.taskForLocatorLocked(command.Locator)
	if err != nil {
		return domain.GetTaskStatusResponse{}, err
	}
	return statusFromTaskLocked(task), nil
}

func (s *Service) StreamTask(ctx context.Context, command domain.StreamTaskCommand) (grpcapi.TaskStream, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupRetainedLocked()

	task := s.tasks[command.TaskID]
	if task == nil {
		return nil, unknownTask("", command.TaskID, "")
	}
	events, live, subscriberID, err := s.subscribeLocked(task, command)
	if err != nil {
		return nil, err
	}
	return &taskStream{
		replay: events,
		live:   live,
		closeFunc: func() {
			s.unsubscribe(command.TaskID, subscriberID)
		},
	}, nil
}
