package chatruntime

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Dirard/codex-runtime/gateway/internal/appserver"
	"github.com/Dirard/codex-runtime/gateway/internal/chatstate"
	"github.com/Dirard/codex-runtime/gateway/internal/config"
	"github.com/Dirard/codex-runtime/gateway/internal/contextpack"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

const (
	defaultThreadCallTimeout      = 30 * time.Second
	defaultTurnStartTimeout       = 30 * time.Second
	defaultPendingResponseTimeout = 10 * time.Second
	defaultTurnInterruptTimeout   = 10 * time.Second

	startChatFreshConnectionAttempts   = 3
	startChatFreshConnectionRetryDelay = 200 * time.Millisecond
	startChatTurnRecoveryAttempts      = 3
	startChatTurnRecoveryDelay         = 200 * time.Millisecond
)

type AppServerClient interface {
	StartThread(context.Context, appserver.ThreadStartCall) (json.RawMessage, error)
	ResumeThread(context.Context, appserver.ThreadResumeCall) (json.RawMessage, error)
	ReadThread(context.Context, appserver.ThreadReadCall) (json.RawMessage, error)
	ListThreadTurns(context.Context, appserver.ThreadTurnsListCall) (json.RawMessage, error)
	StartTurn(context.Context, appserver.TurnStartCall) (json.RawMessage, error)
	InterruptTurn(context.Context, appserver.TurnInterruptCall) (json.RawMessage, error)
	RespondServerRequest(context.Context, appserver.ServerRequest, any, time.Duration) error
}

type ConnectionProvider interface {
	Connection(context.Context) (AppServerClient, error)
}

type AppServerSupervisor interface {
	Connection(context.Context) (*appserver.Connection, error)
}

type appServerConnectionProvider struct {
	supervisor AppServerSupervisor
}

func NewAppServerConnectionProvider(supervisor AppServerSupervisor) ConnectionProvider {
	return appServerConnectionProvider{supervisor: supervisor}
}

func (p appServerConnectionProvider) Connection(ctx context.Context) (AppServerClient, error) {
	if p.supervisor == nil {
		return nil, fmt.Errorf("app-server supervisor is required")
	}
	return p.supervisor.Connection(ctx)
}

type Session struct {
	Group              config.SessionGroup
	ConnectionProvider ConnectionProvider
}

type ServiceOptions struct {
	Store                  *chatstate.Store
	ThreadCallTimeout      time.Duration
	TurnStartTimeout       time.Duration
	PendingResponseTimeout time.Duration
	TurnInterruptTimeout   time.Duration
}

type Service struct {
	mu                     sync.Mutex
	sessionsMu             sync.RWMutex
	sessions               map[string]Session
	store                  *chatstate.Store
	pendingRecords         map[string]*chatPendingRecord
	notificationProviders  map[notificationProvider]struct{}
	nextPendingSeq         uint64
	threadCallTimeout      time.Duration
	turnStartTimeout       time.Duration
	pendingResponseTimeout time.Duration
	turnInterruptTimeout   time.Duration
	cursorSigningKey       []byte
}

func NewService(sessions []Session, options ServiceOptions) (*Service, error) {
	if len(sessions) == 0 {
		return nil, fmt.Errorf("at least one chat runtime session is required")
	}
	store := options.Store
	if store == nil {
		store = chatstate.NewStore(chatstate.StoreOptions{})
	}
	threadCallTimeout := options.ThreadCallTimeout
	if threadCallTimeout <= 0 {
		threadCallTimeout = defaultThreadCallTimeout
	}
	turnStartTimeout := options.TurnStartTimeout
	if turnStartTimeout <= 0 {
		turnStartTimeout = defaultTurnStartTimeout
	}
	pendingResponseTimeout := options.PendingResponseTimeout
	if pendingResponseTimeout <= 0 {
		pendingResponseTimeout = defaultPendingResponseTimeout
	}
	turnInterruptTimeout := options.TurnInterruptTimeout
	if turnInterruptTimeout <= 0 {
		turnInterruptTimeout = defaultTurnInterruptTimeout
	}
	cursorSigningKey, err := newCursorSigningKey()
	if err != nil {
		return nil, fmt.Errorf("history cursor signing key unavailable")
	}
	service := &Service{
		sessions:               make(map[string]Session, len(sessions)),
		store:                  store,
		pendingRecords:         map[string]*chatPendingRecord{},
		notificationProviders:  map[notificationProvider]struct{}{},
		threadCallTimeout:      threadCallTimeout,
		turnStartTimeout:       turnStartTimeout,
		pendingResponseTimeout: pendingResponseTimeout,
		turnInterruptTimeout:   turnInterruptTimeout,
		cursorSigningKey:       cursorSigningKey,
	}
	for _, session := range sessions {
		if err := validateSession(session); err != nil {
			return nil, err
		}
		if _, exists := service.sessions[session.Group.SessionGroupID]; exists {
			return nil, fmt.Errorf("duplicate session group %q", session.Group.SessionGroupID)
		}
		service.sessions[session.Group.SessionGroupID] = session
	}
	return service, nil
}

func (s *Service) RegisterSession(session Session) error {
	if s == nil {
		return fmt.Errorf("chat runtime service is required")
	}
	if err := validateSession(session); err != nil {
		return err
	}
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	if _, exists := s.sessions[session.Group.SessionGroupID]; exists {
		return fmt.Errorf("duplicate session group %q", session.Group.SessionGroupID)
	}
	s.sessions[session.Group.SessionGroupID] = session
	return nil
}

func (s *Service) UnregisterSession(sessionGroupID string) bool {
	if s == nil || sessionGroupID == "" {
		return false
	}
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	if _, exists := s.sessions[sessionGroupID]; !exists {
		return false
	}
	delete(s.sessions, sessionGroupID)
	return true
}

func validateSession(session Session) error {
	if session.Group.SessionGroupID == "" {
		return fmt.Errorf("session group id is required")
	}
	if session.Group.WorkspaceID == "" {
		return fmt.Errorf("workspace id is required for session group %q", session.Group.SessionGroupID)
	}
	if session.ConnectionProvider == nil {
		return fmt.Errorf("connection provider is required for session group %q", session.Group.SessionGroupID)
	}
	return nil
}

func waitForFreshConnectionStartRetry(ctx context.Context) error {
	timer := time.NewTimer(startChatFreshConnectionRetryDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitForStartChatTurnRecovery(ctx context.Context) error {
	timer := time.NewTimer(startChatTurnRecoveryDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func runStateFromCodexTurnStatus(status string) (chatstate.RunState, bool) {
	switch turnLifecycleFromCodex(status) {
	case domain.ChatTurnLifecycleInProgress:
		return chatstate.RunStateRunning, true
	case domain.ChatTurnLifecycleCompleted:
		return chatstate.RunStateCompleted, true
	case domain.ChatTurnLifecycleInterrupted:
		return chatstate.RunStateInterrupted, true
	case domain.ChatTurnLifecycleFailed:
		return chatstate.RunStateFailed, true
	default:
		return "", false
	}
}

func terminalRunState(state chatstate.RunState) bool {
	switch state {
	case chatstate.RunStateCompleted, chatstate.RunStateFailed, chatstate.RunStateInterrupted:
		return true
	default:
		return false
	}
}

type startTurnRecovery struct {
	runID          string
	state          chatstate.RunState
	foundTurn      bool
	confirmedEmpty bool
}

func (s *Service) recoverStartedTurnAfterStartTurnError(ctx context.Context, session Session, chatID string) startTurnRecovery {
	confirmedEmpty := false
	for attempt := 0; attempt < startChatTurnRecoveryAttempts; attempt++ {
		if attempt > 0 {
			if err := waitForStartChatTurnRecovery(ctx); err != nil {
				return startTurnRecovery{}
			}
		}
		thread, err := s.readCodexThread(ctx, session, chatID, true)
		if err != nil {
			continue
		}
		if len(thread.Turns) == 0 {
			confirmedEmpty = true
			continue
		}
		latest := thread.Turns[len(thread.Turns)-1]
		if latest.ID == "" {
			continue
		}
		state, ok := runStateFromCodexTurnStatus(latest.Status)
		if !ok {
			continue
		}
		return startTurnRecovery{runID: latest.ID, state: state, foundTurn: true}
	}
	return startTurnRecovery{confirmedEmpty: confirmedEmpty}
}

func (s *Service) sessionByID(sessionGroupID string) (Session, bool) {
	if s == nil {
		return Session{}, false
	}
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	session, ok := s.sessions[sessionGroupID]
	return session, ok
}

func (s *Service) StartChatRun(ctx context.Context, command domain.StartChatRunCommand) (domain.StartChatRunResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	command, session, envelope, err := s.prepareStartChatRun(command)
	if err != nil {
		return domain.StartChatRunResponse{}, err
	}
	if err := callerErrorFromContext(ctx, command.SessionGroupID, command.ClientMessageID); err != nil {
		return domain.StartChatRunResponse{}, err
	}

	idempotencyScope := startChatRunIdempotencyScope(command)
	entry, reused, err := s.store.ReserveIdempotency(idempotencyScope)
	if err != nil {
		return domain.StartChatRunResponse{}, err
	}
	if reused {
		return responseFromIdempotency(command, entry, s.store.Epoch())
	}

	if err := s.store.ReserveActiveRunCapacity(idempotencyScope); err != nil {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.StartChatRunResponse{}, err
	}
	releaseActiveRunReservation := true
	defer func() {
		if releaseActiveRunReservation {
			_ = s.store.ReleaseActiveRunCapacity(idempotencyScope)
		}
	}()

	var connection AppServerClient
	var threadResult json.RawMessage
	threadStartAttempted := false
	for attempt := 0; attempt < startChatFreshConnectionAttempts; attempt++ {
		connection, err = session.ConnectionProvider.Connection(ctx)
		if err != nil {
			if attempt+1 < startChatFreshConnectionAttempts && retryableFreshConnectionStartError(err) {
				if err := waitForFreshConnectionStartRetry(ctx); err != nil {
					if !threadStartAttempted {
						_ = s.store.ReleaseIdempotency(idempotencyScope)
					}
					return domain.StartChatRunResponse{}, appServerCallError(err, command.SessionGroupID, command.ClientMessageID, "")
				}
				continue
			}
			if !threadStartAttempted {
				_ = s.store.ReleaseIdempotency(idempotencyScope)
			}
			return domain.StartChatRunResponse{}, appServerCallError(err, command.SessionGroupID, command.ClientMessageID, "")
		}
		if connection == nil {
			if !threadStartAttempted {
				_ = s.store.ReleaseIdempotency(idempotencyScope)
			}
			return domain.StartChatRunResponse{}, &domain.GatewayError{
				Code: domain.GatewayErrorCodeInternal,
				Details: domain.GatewayErrorDetails{
					Reason:          domain.ReasonInternalGatewayError,
					DisplayMessage:  "internal gateway error",
					SessionGroupID:  command.SessionGroupID,
					ClientMessageID: command.ClientMessageID,
				},
			}
		}
		s.configureConnection(connection)
		if err := callerErrorFromContext(ctx, command.SessionGroupID, command.ClientMessageID); err != nil {
			if !threadStartAttempted {
				_ = s.store.ReleaseIdempotency(idempotencyScope)
			}
			return domain.StartChatRunResponse{}, err
		}

		threadStartAttempted = true
		threadResult, err = connection.StartThread(ctx, appserver.ThreadStartCall{
			Timeout: s.threadCallTimeout,
		})
		if err == nil {
			break
		}
		if attempt+1 < startChatFreshConnectionAttempts && retryableFreshConnectionStartError(err) {
			if err := waitForFreshConnectionStartRetry(ctx); err != nil {
				if !threadStartAttempted {
					_ = s.store.ReleaseIdempotency(idempotencyScope)
				}
				return domain.StartChatRunResponse{}, appServerCallError(err, command.SessionGroupID, command.ClientMessageID, "")
			}
			continue
		}
		return domain.StartChatRunResponse{}, appServerCallError(err, command.SessionGroupID, command.ClientMessageID, "")
	}
	chatID := appserver.ParseThreadID(threadResult)
	if chatID == "" {
		return domain.StartChatRunResponse{}, protocolMismatch(command.SessionGroupID, "")
	}
	turnResult, err := connection.StartTurn(ctx, appserver.TurnStartCall{
		ThreadID:            chatID,
		ClientUserMessageID: command.ClientMessageID,
		Input: []appserver.UserInputText{
			appserver.NewUserInputText(envelope),
		},
		Timeout: s.turnStartTimeout,
	})
	var runID string
	runState := chatstate.RunStateRunning
	if err != nil {
		startTurnErr := err
		recovery := s.recoverStartedTurnAfterStartTurnError(ctx, session, chatID)
		if !recovery.foundTurn && recovery.confirmedEmpty {
			retryConnection, retryErr := session.ConnectionProvider.Connection(ctx)
			if retryErr != nil {
				startTurnErr = retryErr
			} else if retryConnection == nil {
				return domain.StartChatRunResponse{}, internalGatewayError(command.SessionGroupID, command.ClientMessageID, chatID)
			} else {
				s.configureConnection(retryConnection)
				if err := callerErrorFromContext(ctx, command.SessionGroupID, command.ClientMessageID); err != nil {
					return domain.StartChatRunResponse{}, err
				}
				turnResult, retryErr = retryConnection.StartTurn(ctx, appserver.TurnStartCall{
					ThreadID:            chatID,
					ClientUserMessageID: command.ClientMessageID,
					Input: []appserver.UserInputText{
						appserver.NewUserInputText(envelope),
					},
					Timeout: s.turnStartTimeout,
				})
				if retryErr != nil {
					startTurnErr = retryErr
					recovery = s.recoverStartedTurnAfterStartTurnError(ctx, session, chatID)
				} else {
					runID = appserver.ParseTurnID(turnResult)
					if runID == "" {
						return domain.StartChatRunResponse{}, protocolMismatch(command.SessionGroupID, chatID)
					}
				}
			}
		}
		if runID == "" {
			if !recovery.foundTurn {
				return domain.StartChatRunResponse{}, appServerCallError(startTurnErr, command.SessionGroupID, command.ClientMessageID, chatID)
			}
			runID = recovery.runID
			runState = recovery.state
		}
	} else {
		runID = appserver.ParseTurnID(turnResult)
		if runID == "" {
			return domain.StartChatRunResponse{}, protocolMismatch(command.SessionGroupID, chatID)
		}
	}

	runScope := chatstate.RunScope{
		Scope: chatstate.Scope{
			SessionGroupID: command.SessionGroupID,
			WorkspaceID:    command.WorkspaceID,
			ChatID:         chatID,
		},
		RunID: runID,
	}
	if _, err := s.store.StartRunWithReservation(idempotencyScope, runScope, command.IdempotencyKey); err != nil {
		return domain.StartChatRunResponse{}, err
	}
	releaseActiveRunReservation = false
	if _, err := s.store.UpdateRunState(runScope, runState); err != nil {
		return domain.StartChatRunResponse{}, err
	}
	event, err := s.store.AppendEvent(chatstate.EventInput{
		RunScope:  runScope,
		Kind:      "status",
		State:     string(runState),
		SizeBytes: int64(len(chatID) + len(runID)),
	})
	if err != nil {
		return domain.StartChatRunResponse{}, err
	}
	if terminalRunState(runState) {
		if _, err := s.store.CompleteRun(runScope, runState); err != nil {
			return domain.StartChatRunResponse{}, err
		}
	}
	response := domain.StartChatRunResponse{
		ChatID:            chatID,
		RunID:             runID,
		SessionGroupID:    command.SessionGroupID,
		WorkspaceID:       command.WorkspaceID,
		LastEventID:       event.EventID,
		EventCursor:       eventCursor(event.Epoch, chatID, runID, event.EventID),
		FirstTurnAccepted: true,
		ProcessEpoch:      event.Epoch,
	}
	if _, err := s.store.CompleteIdempotency(idempotencyScope, chatstate.IdempotencyResultRef{
		ChatID:      response.ChatID,
		RunID:       response.RunID,
		Status:      string(runState),
		LastEventID: response.LastEventID,
		EventCursor: response.EventCursor,
	}); err != nil {
		return domain.StartChatRunResponse{}, err
	}
	return response, nil
}

func (s *Service) GetChat(ctx context.Context, command domain.GetChatCommand) (domain.GetChatResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	command, session, err := s.prepareChatLookup(command.SessionGroupID, command.WorkspaceID, command.ChatID)
	if err != nil {
		return domain.GetChatResponse{}, err
	}
	if err := callerErrorFromContext(ctx, command.SessionGroupID, ""); err != nil {
		return domain.GetChatResponse{}, err
	}
	thread, err := s.readCodexThread(ctx, session, command.ChatID, false)
	if err != nil {
		return domain.GetChatResponse{}, err
	}
	if err := s.reconcileLocalActiveRunFromThread(chatstate.Scope{SessionGroupID: command.SessionGroupID, WorkspaceID: command.WorkspaceID, ChatID: command.ChatID}, thread); err != nil {
		return domain.GetChatResponse{}, err
	}
	status := s.statusFromThread(command.SessionGroupID, command.WorkspaceID, thread)
	return domain.GetChatResponse{
		Chat:   chatFromThread(command.SessionGroupID, command.WorkspaceID, thread),
		Status: status,
	}, nil
}

func (s *Service) RunChatTurn(ctx context.Context, command domain.RunChatTurnCommand) (domain.RunChatTurnResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	command, session, envelope, err := s.prepareRunChatTurn(command)
	if err != nil {
		return domain.RunChatTurnResponse{}, err
	}
	if err := callerErrorFromContext(ctx, command.SessionGroupID, command.ClientMessageID); err != nil {
		return domain.RunChatTurnResponse{}, err
	}
	scope := chatstate.Scope{
		SessionGroupID: command.SessionGroupID,
		WorkspaceID:    command.WorkspaceID,
		ChatID:         command.ChatID,
	}
	idempotencyScope := runChatTurnIdempotencyScope(command)
	entry, reused, err := s.store.ReserveIdempotency(idempotencyScope)
	if err != nil {
		return domain.RunChatTurnResponse{}, err
	}
	if reused {
		return runChatTurnResponseFromIdempotency(command, entry, s.store.Epoch())
	}

	connection, err := session.ConnectionProvider.Connection(ctx)
	if err != nil {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.RunChatTurnResponse{}, appServerCallError(err, command.SessionGroupID, command.ClientMessageID, command.ChatID)
	}
	if connection == nil {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.RunChatTurnResponse{}, &domain.GatewayError{
			Code: domain.GatewayErrorCodeInternal,
			Details: domain.GatewayErrorDetails{
				Reason:          domain.ReasonInternalGatewayError,
				DisplayMessage:  "internal gateway error",
				SessionGroupID:  command.SessionGroupID,
				ClientMessageID: command.ClientMessageID,
				ThreadID:        command.ChatID,
			},
		}
	}
	s.configureConnection(connection)
	if err := callerErrorFromContext(ctx, command.SessionGroupID, command.ClientMessageID); err != nil {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.RunChatTurnResponse{}, err
	}
	resumeResult, err := connection.ResumeThread(ctx, appserver.ThreadResumeCall{
		ThreadID: command.ChatID,
		Timeout:  s.threadCallTimeout,
	})
	if err != nil {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.RunChatTurnResponse{}, appServerCallError(err, command.SessionGroupID, command.ClientMessageID, command.ChatID)
	}
	thread, err := appserver.ParseThreadView(resumeResult)
	if err != nil || thread.ID != command.ChatID {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.RunChatTurnResponse{}, protocolMismatch(command.SessionGroupID, command.ChatID)
	}
	if err := verifyThreadWorkspace(session, thread); err != nil {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.RunChatTurnResponse{}, err
	}
	lifecycle := threadLifecycleFromCodex(thread.Status)
	if lifecycle == domain.ChatThreadLifecycleActiveRunning {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.RunChatTurnResponse{}, alreadyRunning(command.SessionGroupID, command.WorkspaceID, command.ChatID)
	}
	if lifecycle == domain.ChatThreadLifecycleUnknown {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.RunChatTurnResponse{}, chatStateUnavailable(command.SessionGroupID, command.ChatID)
	}
	if err := s.reconcileLocalActiveRunFromThread(scope, thread); err != nil {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.RunChatTurnResponse{}, err
	}

	provisionalRunID := provisionalRunID(command.IdempotencyKey)
	provisionalScope := chatstate.RunScope{Scope: scope, RunID: provisionalRunID}
	if _, err := s.store.StartRun(provisionalScope, command.IdempotencyKey); err != nil {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.RunChatTurnResponse{}, err
	}
	releaseBeforeCodexSideEffect := true
	defer func() {
		if releaseBeforeCodexSideEffect {
			_, _ = s.store.CompleteRun(provisionalScope, chatstate.RunStateFailed)
			_ = s.store.ReleaseIdempotency(idempotencyScope)
		}
	}()

	releaseBeforeCodexSideEffect = false
	turnResult, err := connection.StartTurn(ctx, appserver.TurnStartCall{
		ThreadID:            command.ChatID,
		ClientUserMessageID: command.ClientMessageID,
		Input: []appserver.UserInputText{
			appserver.NewUserInputText(envelope),
		},
		Timeout: s.turnStartTimeout,
	})
	if err != nil {
		_, _ = s.store.CompleteRun(provisionalScope, chatstate.RunStateFailed)
		return domain.RunChatTurnResponse{}, appServerCallError(err, command.SessionGroupID, command.ClientMessageID, command.ChatID)
	}
	runID := appserver.ParseTurnID(turnResult)
	if runID == "" {
		_, _ = s.store.CompleteRun(provisionalScope, chatstate.RunStateFailed)
		return domain.RunChatTurnResponse{}, protocolMismatch(command.SessionGroupID, command.ChatID)
	}
	runScope := chatstate.RunScope{Scope: scope, RunID: runID}
	if _, err := s.store.BindRunID(provisionalScope, runID); err != nil {
		return domain.RunChatTurnResponse{}, err
	}
	if _, err := s.store.UpdateRunState(runScope, chatstate.RunStateRunning); err != nil {
		return domain.RunChatTurnResponse{}, err
	}
	event, err := s.store.AppendEvent(chatstate.EventInput{
		RunScope:  runScope,
		Kind:      "status",
		State:     string(chatstate.RunStateRunning),
		SizeBytes: int64(len(command.ChatID) + len(runID)),
	})
	if err != nil {
		return domain.RunChatTurnResponse{}, err
	}
	status := s.statusFromThread(command.SessionGroupID, command.WorkspaceID, thread)
	status.ThreadLifecycle = domain.ChatThreadLifecycleActiveRunning
	status.CurrentRunLifecycle = domain.ChatTurnLifecycleInProgress
	status.CurrentRunID = runID
	status.LastRunID = runID
	status.LastEventID = event.EventID
	status.GatewayLocal.Live = true
	status.GatewayLocal.ReplayAvailable = true
	status.GatewayLocal.ReplayUnavailable = false
	status.GatewayLocal.ProcessEpoch = event.Epoch
	response := domain.RunChatTurnResponse{
		ChatID:         command.ChatID,
		RunID:          runID,
		SessionGroupID: command.SessionGroupID,
		WorkspaceID:    command.WorkspaceID,
		Status:         status,
		LastEventID:    event.EventID,
		EventCursor:    eventCursor(event.Epoch, command.ChatID, runID, event.EventID),
		TurnAccepted:   true,
	}
	if _, err := s.store.CompleteIdempotency(idempotencyScope, chatstate.IdempotencyResultRef{
		ChatID:      response.ChatID,
		RunID:       response.RunID,
		Status:      string(chatstate.RunStateRunning),
		LastEventID: response.LastEventID,
		EventCursor: response.EventCursor,
	}); err != nil {
		return domain.RunChatTurnResponse{}, err
	}
	return response, nil
}

func (s *Service) GetChatStatus(ctx context.Context, command domain.GetChatStatusCommand) (domain.GetChatStatusResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lookup, session, err := s.prepareChatLookup(command.SessionGroupID, command.WorkspaceID, command.ChatID)
	if err != nil {
		return domain.GetChatStatusResponse{}, err
	}
	if err := callerErrorFromContext(ctx, lookup.SessionGroupID, ""); err != nil {
		return domain.GetChatStatusResponse{}, err
	}
	thread, err := s.readCodexThread(ctx, session, lookup.ChatID, true)
	if err != nil {
		return domain.GetChatStatusResponse{}, err
	}
	if thread.ID != lookup.ChatID {
		return domain.GetChatStatusResponse{}, protocolMismatch(lookup.SessionGroupID, lookup.ChatID)
	}
	if err := s.reconcileLocalActiveRunFromThread(chatstate.Scope{SessionGroupID: lookup.SessionGroupID, WorkspaceID: lookup.WorkspaceID, ChatID: lookup.ChatID}, thread); err != nil {
		return domain.GetChatStatusResponse{}, err
	}
	return domain.GetChatStatusResponse{Status: s.statusFromThread(lookup.SessionGroupID, lookup.WorkspaceID, thread)}, nil
}

func (s *Service) GetChatHistory(ctx context.Context, command domain.GetChatHistoryCommand) (domain.GetChatHistoryResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lookup, session, err := s.prepareChatLookup(command.SessionGroupID, command.WorkspaceID, command.ChatID)
	if err != nil {
		return domain.GetChatHistoryResponse{}, err
	}
	if command.RequestedDepth == "" {
		command.RequestedDepth = domain.ChatHistoryDepthTurnSummary
	}
	if command.RequestedDepth != domain.ChatHistoryDepthTurnSummary && command.RequestedDepth != domain.ChatHistoryDepthItemLevel {
		return domain.GetChatHistoryResponse{}, invalidRequest(command.SessionGroupID, "", "requested_depth is invalid")
	}
	if command.SortDirection == "" {
		command.SortDirection = domain.ChatHistorySortDescending
	}
	if err := callerErrorFromContext(ctx, lookup.SessionGroupID, ""); err != nil {
		return domain.GetChatHistoryResponse{}, err
	}
	connection, err := session.ConnectionProvider.Connection(ctx)
	if err != nil {
		return domain.GetChatHistoryResponse{}, appServerCallError(err, lookup.SessionGroupID, "", lookup.ChatID)
	}
	if connection == nil {
		return domain.GetChatHistoryResponse{}, &domain.GatewayError{
			Code: domain.GatewayErrorCodeInternal,
			Details: domain.GatewayErrorDetails{
				Reason:         domain.ReasonInternalGatewayError,
				DisplayMessage: "internal gateway error",
				SessionGroupID: lookup.SessionGroupID,
				ThreadID:       lookup.ChatID,
			},
		}
	}
	s.configureConnection(connection)
	rawThread, err := connection.ReadThread(ctx, appserver.ThreadReadCall{
		ThreadID:     lookup.ChatID,
		IncludeTurns: false,
		Timeout:      s.threadCallTimeout,
	})
	if err != nil {
		return domain.GetChatHistoryResponse{}, appServerCallError(err, lookup.SessionGroupID, "", lookup.ChatID)
	}
	thread, err := appserver.ParseThreadView(rawThread)
	if err != nil {
		return domain.GetChatHistoryResponse{}, protocolMismatch(lookup.SessionGroupID, lookup.ChatID)
	}
	if thread.ID != lookup.ChatID {
		return domain.GetChatHistoryResponse{}, protocolMismatch(lookup.SessionGroupID, lookup.ChatID)
	}
	if err := verifyThreadWorkspace(session, thread); err != nil {
		return domain.GetChatHistoryResponse{}, err
	}
	if err := s.reconcileLocalActiveRunFromThread(chatstate.Scope{SessionGroupID: lookup.SessionGroupID, WorkspaceID: lookup.WorkspaceID, ChatID: lookup.ChatID}, thread); err != nil {
		return domain.GetChatHistoryResponse{}, err
	}
	if thread.Ephemeral || threadLifecycleFromCodex(thread.Status) == domain.ChatThreadLifecycleNotLoaded {
		return chatHistoryUnavailableResponse(lookup, "chat history is unavailable from Codex for this thread"), nil
	}
	sortDirection := "desc"
	if command.SortDirection == domain.ChatHistorySortAscending {
		sortDirection = "asc"
	}
	codexCursor := ""
	if command.Cursor != "" {
		codexCursor, err = s.decodeHistoryCursor(lookup, command, command.Cursor)
		if err != nil {
			return domain.GetChatHistoryResponse{}, err
		}
	}
	pageResult, err := connection.ListThreadTurns(ctx, appserver.ThreadTurnsListCall{
		ThreadID:      lookup.ChatID,
		Cursor:        codexCursor,
		Limit:         command.Limit,
		SortDirection: sortDirection,
		ItemsView:     "summary",
		Timeout:       s.threadCallTimeout,
	})
	if err != nil {
		return domain.GetChatHistoryResponse{}, appServerCallError(err, lookup.SessionGroupID, "", lookup.ChatID)
	}
	page, err := appserver.ParseThreadTurnsPage(pageResult)
	if err != nil {
		return domain.GetChatHistoryResponse{}, protocolMismatch(lookup.SessionGroupID, lookup.ChatID)
	}
	response := domain.GetChatHistoryResponse{
		ChatID:          lookup.ChatID,
		Turns:           turnSummariesFromViews(page.Turns),
		NextCursor:      s.encodeHistoryCursor(lookup, command, page.NextCursor),
		BackwardsCursor: s.encodeHistoryCursor(lookup, command, page.BackwardsCursor),
		ReturnedDepth:   domain.ChatHistoryDepthTurnSummary,
		Capability:      domain.ChatCapabilitySupported,
	}
	if command.RequestedDepth == domain.ChatHistoryDepthItemLevel {
		response.Capability = domain.ChatCapabilityNarrowed
		response.Narrowed = &domain.ChatNarrowedOutcome{
			Reason:         domain.ReasonChatRuntimeNotImplemented,
			DisplayMessage: "item-level chat history is not available from this Codex app-server method",
		}
	}
	return response, nil
}

func chatHistoryUnavailableResponse(lookup domain.GetChatCommand, message string) domain.GetChatHistoryResponse {
	return domain.GetChatHistoryResponse{
		ChatID:        lookup.ChatID,
		ReturnedDepth: domain.ChatHistoryDepthTurnSummary,
		Capability:    domain.ChatCapabilityUnavailable,
		Narrowed: &domain.ChatNarrowedOutcome{
			Reason:         domain.ReasonHistoryUnavailable,
			DisplayMessage: message,
			Retryable:      true,
		},
	}
}

func (s *Service) prepareStartChatRun(command domain.StartChatRunCommand) (domain.StartChatRunCommand, Session, string, error) {
	session, ok := s.sessionByID(command.SessionGroupID)
	if !ok {
		return domain.StartChatRunCommand{}, Session{}, "", unknownSession(command.SessionGroupID)
	}
	if command.WorkspaceID != "" && command.WorkspaceID != session.Group.WorkspaceID {
		return domain.StartChatRunCommand{}, Session{}, "", workspaceMismatch(command.SessionGroupID)
	}
	command.WorkspaceID = session.Group.WorkspaceID
	if strings.TrimSpace(command.Prompt) == "" {
		return domain.StartChatRunCommand{}, Session{}, "", invalidRequest(command.SessionGroupID, command.ClientMessageID, "prompt is required")
	}
	if command.ClientMessageID == "" {
		return domain.StartChatRunCommand{}, Session{}, "", invalidRequest(command.SessionGroupID, "", "client_message_id is required")
	}
	if command.IdempotencyKey == "" {
		return domain.StartChatRunCommand{}, Session{}, "", invalidRequest(command.SessionGroupID, command.ClientMessageID, "idempotency_key is required")
	}
	envelope, err := contextpack.BuildEnvelope(command.Prompt, command.ContextBlocks)
	if err != nil {
		return domain.StartChatRunCommand{}, Session{}, "", invalidContext(command.SessionGroupID, command.ClientMessageID, err)
	}
	return command, session, envelope, nil
}

func (s *Service) prepareChatLookup(sessionGroupID string, workspaceID string, chatID string) (domain.GetChatCommand, Session, error) {
	session, ok := s.sessionByID(sessionGroupID)
	if !ok {
		return domain.GetChatCommand{}, Session{}, unknownSession(sessionGroupID)
	}
	if workspaceID != "" && workspaceID != session.Group.WorkspaceID {
		return domain.GetChatCommand{}, Session{}, workspaceMismatch(sessionGroupID)
	}
	if chatID == "" {
		return domain.GetChatCommand{}, Session{}, invalidRequest(sessionGroupID, "", "chat_id is required")
	}
	return domain.GetChatCommand{
		SessionGroupID: sessionGroupID,
		WorkspaceID:    session.Group.WorkspaceID,
		ChatID:         chatID,
	}, session, nil
}

func (s *Service) prepareRunChatTurn(command domain.RunChatTurnCommand) (domain.RunChatTurnCommand, Session, string, error) {
	lookup, session, err := s.prepareChatLookup(command.SessionGroupID, command.WorkspaceID, command.ChatID)
	if err != nil {
		return domain.RunChatTurnCommand{}, Session{}, "", err
	}
	command.WorkspaceID = lookup.WorkspaceID
	if strings.TrimSpace(command.Prompt) == "" {
		return domain.RunChatTurnCommand{}, Session{}, "", invalidRequest(command.SessionGroupID, command.ClientMessageID, "prompt is required")
	}
	if command.ClientMessageID == "" {
		return domain.RunChatTurnCommand{}, Session{}, "", invalidRequest(command.SessionGroupID, "", "client_message_id is required")
	}
	if command.IdempotencyKey == "" {
		return domain.RunChatTurnCommand{}, Session{}, "", invalidRequest(command.SessionGroupID, command.ClientMessageID, "idempotency_key is required")
	}
	envelope, err := contextpack.BuildEnvelope(command.Prompt, command.ContextBlocks)
	if err != nil {
		return domain.RunChatTurnCommand{}, Session{}, "", invalidContext(command.SessionGroupID, command.ClientMessageID, err)
	}
	return command, session, envelope, nil
}

func startChatRunIdempotencyScope(command domain.StartChatRunCommand) chatstate.IdempotencyScope {
	return chatstate.IdempotencyScope{
		Operation:       chatstate.OperationStartChatRun,
		SessionGroupID:  command.SessionGroupID,
		WorkspaceID:     command.WorkspaceID,
		ClientMessageID: command.ClientMessageID,
		Key:             command.IdempotencyKey,
	}
}

func runChatTurnIdempotencyScope(command domain.RunChatTurnCommand) chatstate.IdempotencyScope {
	return chatstate.IdempotencyScope{
		Operation:       chatstate.OperationRunChatTurn,
		SessionGroupID:  command.SessionGroupID,
		WorkspaceID:     command.WorkspaceID,
		ChatID:          command.ChatID,
		ClientMessageID: command.ClientMessageID,
		Key:             command.IdempotencyKey,
	}
}

func responseFromIdempotency(command domain.StartChatRunCommand, entry chatstate.IdempotencyEntry, epoch string) (domain.StartChatRunResponse, error) {
	if entry.State != chatstate.IdempotencyStateSucceeded {
		return domain.StartChatRunResponse{}, idempotencyResultUnavailable(command.SessionGroupID, command.ClientMessageID)
	}
	if entry.Result.ChatID == "" || entry.Result.RunID == "" {
		return domain.StartChatRunResponse{}, idempotencyResultUnavailable(command.SessionGroupID, command.ClientMessageID)
	}
	return domain.StartChatRunResponse{
		ChatID:            entry.Result.ChatID,
		RunID:             entry.Result.RunID,
		SessionGroupID:    command.SessionGroupID,
		WorkspaceID:       command.WorkspaceID,
		LastEventID:       entry.Result.LastEventID,
		EventCursor:       entry.Result.EventCursor,
		FirstTurnAccepted: true,
		ProcessEpoch:      epoch,
	}, nil
}

func runChatTurnResponseFromIdempotency(command domain.RunChatTurnCommand, entry chatstate.IdempotencyEntry, epoch string) (domain.RunChatTurnResponse, error) {
	if entry.State != chatstate.IdempotencyStateSucceeded || entry.Result.ChatID == "" || entry.Result.RunID == "" {
		return domain.RunChatTurnResponse{}, idempotencyResultUnavailable(command.SessionGroupID, command.ClientMessageID)
	}
	status := baseChatStatus(command.SessionGroupID, command.WorkspaceID, command.ChatID, epoch)
	status.ThreadLifecycle = domain.ChatThreadLifecycleActiveRunning
	status.CurrentRunLifecycle = domain.ChatTurnLifecycleInProgress
	status.CurrentRunID = entry.Result.RunID
	status.LastRunID = entry.Result.RunID
	status.LastEventID = entry.Result.LastEventID
	status.GatewayLocal.Live = true
	status.GatewayLocal.ReplayAvailable = true
	status.GatewayLocal.ReplayUnavailable = false
	return domain.RunChatTurnResponse{
		ChatID:         entry.Result.ChatID,
		RunID:          entry.Result.RunID,
		SessionGroupID: command.SessionGroupID,
		WorkspaceID:    command.WorkspaceID,
		Status:         status,
		LastEventID:    entry.Result.LastEventID,
		EventCursor:    entry.Result.EventCursor,
		TurnAccepted:   true,
	}, nil
}

func (s *Service) readCodexThread(ctx context.Context, session Session, chatID string, includeTurns bool) (appserver.ThreadView, error) {
	connection, err := session.ConnectionProvider.Connection(ctx)
	if err != nil {
		return appserver.ThreadView{}, appServerCallError(err, session.Group.SessionGroupID, "", chatID)
	}
	if connection == nil {
		return appserver.ThreadView{}, &domain.GatewayError{
			Code: domain.GatewayErrorCodeInternal,
			Details: domain.GatewayErrorDetails{
				Reason:         domain.ReasonInternalGatewayError,
				DisplayMessage: "internal gateway error",
				SessionGroupID: session.Group.SessionGroupID,
				ThreadID:       chatID,
			},
		}
	}
	s.configureConnection(connection)
	raw, err := connection.ReadThread(ctx, appserver.ThreadReadCall{
		ThreadID:     chatID,
		IncludeTurns: includeTurns,
		Timeout:      s.threadCallTimeout,
	})
	if err != nil {
		return appserver.ThreadView{}, appServerCallError(err, session.Group.SessionGroupID, "", chatID)
	}
	thread, err := appserver.ParseThreadView(raw)
	if err != nil {
		return appserver.ThreadView{}, protocolMismatch(session.Group.SessionGroupID, chatID)
	}
	if thread.ID != chatID {
		return appserver.ThreadView{}, protocolMismatch(session.Group.SessionGroupID, chatID)
	}
	if err := verifyThreadWorkspace(session, thread); err != nil {
		return appserver.ThreadView{}, err
	}
	return thread, nil
}

func chatFromThread(sessionGroupID string, workspaceID string, thread appserver.ThreadView) domain.Chat {
	return domain.Chat{
		ChatID:          thread.ID,
		SessionGroupID:  sessionGroupID,
		WorkspaceID:     workspaceID,
		ThreadLifecycle: threadLifecycleFromCodex(thread.Status),
		CreatedAtUnixMS: thread.CreatedAtUnixMS,
		UpdatedAtUnixMS: thread.UpdatedAtUnixMS,
		Preview:         thread.Preview,
		Ephemeral:       thread.Ephemeral,
		Capabilities:    defaultCapabilities(),
	}
}

func (s *Service) statusFromThread(sessionGroupID string, workspaceID string, thread appserver.ThreadView) domain.ChatStatus {
	status := baseChatStatus(sessionGroupID, workspaceID, thread.ID, s.store.Epoch())
	status.ThreadLifecycle = threadLifecycleFromCodex(thread.Status)
	applyLatestTurnStatus(&status, thread)
	if active, ok := s.store.ActiveRun(chatstate.Scope{SessionGroupID: sessionGroupID, WorkspaceID: workspaceID, ChatID: thread.ID}); ok {
		runScope := chatstate.RunScope{Scope: chatstate.Scope{SessionGroupID: sessionGroupID, WorkspaceID: workspaceID, ChatID: thread.ID}, RunID: active.RunID}
		status.ThreadLifecycle = domain.ChatThreadLifecycleActiveRunning
		status.CurrentRunLifecycle = turnLifecycleFromRunState(active.State)
		if !strings.HasPrefix(active.RunID, "pending:") {
			status.CurrentRunID = active.RunID
			status.LastRunID = active.RunID
		}
		status.GatewayLocal.Live = true
		status.GatewayLocal.ReplayAvailable = true
		status.GatewayLocal.ReplayUnavailable = false
		status.ActivePending = s.activePendingRequests(runScope)
	}
	return status
}

func applyLatestTurnStatus(status *domain.ChatStatus, thread appserver.ThreadView) {
	if len(thread.Turns) == 0 {
		return
	}
	latest := thread.Turns[len(thread.Turns)-1]
	if latest.ID == "" {
		return
	}
	status.LastRunID = latest.ID
	status.CurrentRunLifecycle = turnLifecycleFromCodex(latest.Status)
	if status.CurrentRunLifecycle == domain.ChatTurnLifecycleInProgress {
		status.CurrentRunID = latest.ID
	}
}

func (s *Service) reconcileLocalActiveRunFromThread(scope chatstate.Scope, thread appserver.ThreadView) error {
	lifecycle := threadLifecycleFromCodex(thread.Status)
	if lifecycle == domain.ChatThreadLifecycleActiveRunning {
		return nil
	}
	active, ok := s.store.ActiveRun(scope)
	if !ok {
		return nil
	}
	terminalState := chatstate.RunStateCompleted
	switch lifecycle {
	case domain.ChatThreadLifecycleSystemError, domain.ChatThreadLifecycleNotLoaded:
		terminalState = chatstate.RunStateFailed
	case domain.ChatThreadLifecycleUnknown:
		return nil
	}
	runScope := chatstate.RunScope{Scope: scope, RunID: active.RunID}
	_, _ = s.store.AppendEvent(chatstate.EventInput{
		RunScope:  runScope,
		Kind:      "status",
		State:     string(terminalState),
		SizeBytes: int64(len(scope.ChatID) + len(active.RunID)),
	})
	_, err := s.store.CompleteRun(runScope, terminalState)
	return err
}

func baseChatStatus(sessionGroupID string, workspaceID string, chatID string, epoch string) domain.ChatStatus {
	return domain.ChatStatus{
		ChatID:              chatID,
		SessionGroupID:      sessionGroupID,
		WorkspaceID:         workspaceID,
		LookupValid:         true,
		ThreadLifecycle:     domain.ChatThreadLifecycleUnknown,
		CurrentRunLifecycle: domain.ChatTurnLifecycleUnknown,
		Capabilities:        defaultCapabilities(),
		GatewayLocal: domain.ChatGatewayLocalState{
			ReplayUnavailable: true,
			ProcessEpoch:      epoch,
		},
	}
}

func defaultCapabilities() domain.ChatCapabilitySet {
	return domain.ChatCapabilitySet{
		Status:      domain.ChatCapabilitySupported,
		History:     domain.ChatCapabilitySupported,
		EventStream: domain.ChatCapabilityNarrowed,
		Replay:      domain.ChatCapabilityNarrowed,
		Pending:     domain.ChatCapabilitySupported,
		Interrupt:   domain.ChatCapabilitySupported,
	}
}

func turnSummariesFromViews(turns []appserver.TurnView) []domain.ChatTurnSummary {
	summaries := make([]domain.ChatTurnSummary, 0, len(turns))
	for _, turn := range turns {
		summary := domain.ChatTurnSummary{
			RunID:             turn.ID,
			Lifecycle:         turnLifecycleFromCodex(turn.Status),
			ItemsView:         turnItemsViewFromCodex(turn.ItemsView),
			StartedAtUnixMS:   turn.StartedAtUnixMS,
			CompletedAtUnixMS: turn.CompletedAtUnixMS,
			DurationMS:        turn.DurationMS,
		}
		if turn.ErrorMessage != "" {
			summary.Error = &domain.ChatErrorSummary{DisplayMessage: turn.ErrorMessage}
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

func threadLifecycleFromCodex(status string) domain.ChatThreadLifecycle {
	switch status {
	case "notLoaded":
		return domain.ChatThreadLifecycleNotLoaded
	case "idle":
		return domain.ChatThreadLifecycleIdle
	case "active":
		return domain.ChatThreadLifecycleActiveRunning
	case "systemError":
		return domain.ChatThreadLifecycleSystemError
	default:
		return domain.ChatThreadLifecycleUnknown
	}
}

func turnLifecycleFromCodex(status string) domain.ChatTurnLifecycle {
	switch status {
	case "inProgress":
		return domain.ChatTurnLifecycleInProgress
	case "completed":
		return domain.ChatTurnLifecycleCompleted
	case "interrupted":
		return domain.ChatTurnLifecycleInterrupted
	case "failed":
		return domain.ChatTurnLifecycleFailed
	default:
		return domain.ChatTurnLifecycleUnknown
	}
}

func turnLifecycleFromRunState(state chatstate.RunState) domain.ChatTurnLifecycle {
	switch state {
	case chatstate.RunStateRunning, chatstate.RunStateStarting, chatstate.RunStatePending, chatstate.RunStateInterrupting:
		return domain.ChatTurnLifecycleInProgress
	case chatstate.RunStateCompleted:
		return domain.ChatTurnLifecycleCompleted
	case chatstate.RunStateInterrupted:
		return domain.ChatTurnLifecycleInterrupted
	case chatstate.RunStateFailed:
		return domain.ChatTurnLifecycleFailed
	default:
		return domain.ChatTurnLifecycleUnknown
	}
}

func turnItemsViewFromCodex(view string) domain.ChatTurnItemsView {
	switch view {
	case "notLoaded":
		return domain.ChatTurnItemsViewNotLoaded
	case "summary":
		return domain.ChatTurnItemsViewSummary
	case "full":
		return domain.ChatTurnItemsViewFullUnsupported
	default:
		return domain.ChatTurnItemsViewNotLoaded
	}
}

func alreadyRunning(sessionGroupID string, workspaceID string, chatID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeFailedPrecondition,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonAlreadyRunning,
			DisplayMessage: "chat already has active run",
			SessionGroupID: sessionGroupID,
			ThreadID:       chatID,
		},
	}
}

func verifyThreadWorkspace(session Session, thread appserver.ThreadView) error {
	if session.Group.CanonicalCWD == "" || thread.CWD == "" {
		return protocolMismatch(session.Group.SessionGroupID, thread.ID)
	}
	if workspacePathKey(session.Group.CanonicalCWD) == workspacePathKey(thread.CWD) {
		return nil
	}
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodePermissionDenied,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonWorkspaceMismatch,
			DisplayMessage: "chat is outside the authorized workspace",
			SessionGroupID: session.Group.SessionGroupID,
			ThreadID:       thread.ID,
		},
	}
}

func workspacePathKey(path string) string {
	cleaned := filepath.Clean(path)
	if runtime.GOOS != "windows" {
		return cleaned
	}
	normalized := cleaned
	if strings.HasPrefix(normalized, `\\?\UNC\`) {
		normalized = `\\` + strings.TrimPrefix(normalized, `\\?\UNC\`)
	} else {
		normalized = strings.TrimPrefix(normalized, `\\?\`)
	}
	normalized = strings.TrimPrefix(normalized, `\??\`)
	normalized = filepath.ToSlash(normalized)
	normalized = strings.TrimRight(normalized, "/")
	return strings.ToLower(normalized)
}

func provisionalRunID(idempotencyKey string) string {
	if idempotencyKey == "" {
		return "pending:run"
	}
	return "pending:" + idempotencyKey
}

type eventCursorEnvelope struct {
	Epoch   string `json:"epoch"`
	ChatID  string `json:"chat_id"`
	RunID   string `json:"run_id"`
	EventID uint64 `json:"event_id"`
}

type historyCursorEnvelope struct {
	SessionGroupID string                          `json:"session_group_id"`
	WorkspaceID    string                          `json:"workspace_id"`
	ChatID         string                          `json:"chat_id"`
	Depth          domain.ChatHistoryDepth         `json:"depth"`
	SortDirection  domain.ChatHistorySortDirection `json:"sort_direction"`
	Cursor         string                          `json:"cursor"`
}

type signedCursorEnvelope struct {
	Version   int             `json:"v"`
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"sig"`
}

func eventCursor(epoch string, chatID string, runID string, eventID uint64) string {
	if epoch == "" || chatID == "" || runID == "" || eventID == 0 {
		return ""
	}
	return encodeCursorEnvelope(eventCursorEnvelope{
		Epoch:   epoch,
		ChatID:  chatID,
		RunID:   runID,
		EventID: eventID,
	})
}

func parseEventCursor(cursor string) (eventCursorEnvelope, bool) {
	var envelope eventCursorEnvelope
	if !decodeCursorEnvelope(cursor, &envelope) {
		return eventCursorEnvelope{}, false
	}
	if envelope.Epoch == "" || envelope.ChatID == "" || envelope.RunID == "" || envelope.EventID == 0 {
		return eventCursorEnvelope{}, false
	}
	return envelope, true
}

func (s *Service) encodeHistoryCursor(lookup domain.GetChatCommand, command domain.GetChatHistoryCommand, cursor string) string {
	if lookup.ChatID == "" || cursor == "" {
		return ""
	}
	payload, err := json.Marshal(historyCursorEnvelope{
		SessionGroupID: lookup.SessionGroupID,
		WorkspaceID:    lookup.WorkspaceID,
		ChatID:         lookup.ChatID,
		Depth:          command.RequestedDepth,
		SortDirection:  command.SortDirection,
		Cursor:         cursor,
	})
	if err != nil {
		return ""
	}
	return encodeCursorEnvelope(signedCursorEnvelope{
		Version:   1,
		Payload:   payload,
		Signature: s.signCursorPayload(payload),
	})
}

func (s *Service) decodeHistoryCursor(lookup domain.GetChatCommand, command domain.GetChatHistoryCommand, cursor string) (string, error) {
	var signed signedCursorEnvelope
	if !decodeCursorEnvelope(cursor, &signed) || signed.Version != 1 || len(signed.Payload) == 0 || signed.Signature == "" {
		return "", invalidCursor(lookup.SessionGroupID, lookup.ChatID, "history cursor is invalid")
	}
	if !s.verifyCursorPayload(signed.Payload, signed.Signature) {
		return "", invalidCursor(lookup.SessionGroupID, lookup.ChatID, "history cursor is invalid")
	}
	var envelope historyCursorEnvelope
	if json.Unmarshal(signed.Payload, &envelope) != nil || envelope.ChatID == "" || envelope.Cursor == "" {
		return "", invalidCursor(lookup.SessionGroupID, lookup.ChatID, "history cursor is invalid")
	}
	if envelope.SessionGroupID != lookup.SessionGroupID || envelope.WorkspaceID != lookup.WorkspaceID || envelope.ChatID != lookup.ChatID {
		return "", cursorOutOfRange(lookup.SessionGroupID, lookup.ChatID, "history cursor is outside this chat")
	}
	if envelope.Depth != command.RequestedDepth || envelope.SortDirection != command.SortDirection {
		return "", cursorOutOfRange(lookup.SessionGroupID, lookup.ChatID, "history cursor is outside this history request")
	}
	return envelope.Cursor, nil
}

func newCursorSigningKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *Service) signCursorPayload(payload []byte) string {
	mac := hmac.New(sha256.New, s.cursorSigningKey)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) verifyCursorPayload(payload []byte, signature string) bool {
	actual, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, s.cursorSigningKey)
	_, _ = mac.Write(payload)
	return hmac.Equal(actual, mac.Sum(nil))
}

func encodeCursorEnvelope(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursorEnvelope(cursor string, value any) bool {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, value) == nil
}
