package chatruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/Dirard/codex-runtime/gateway/internal/appserver"
	"github.com/Dirard/codex-runtime/gateway/internal/chatstate"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	"github.com/Dirard/codex-runtime/gateway/internal/pending"
	"github.com/Dirard/codex-runtime/gateway/internal/redact"
)

type forwardedServerRequestHookSetter interface {
	SetForwardedServerRequestHook(appserver.ForwardedServerRequestHook)
}

type sensitiveRegistryProvider interface {
	SensitiveRegistry() *redact.Registry
}

type chatPendingRecord struct {
	runScope   chatstate.RunScope
	record     *pending.Record
	request    appserver.ServerRequest
	connection AppServerClient
	active     bool
	inFlight   string
	responses  map[string]*chatPendingResponseEntry
}

type chatPendingResponseEntry struct {
	clientResponseID string
	idempotencyKey   string
	runScope         chatstate.RunScope
	state            pending.ResponseState
	done             chan struct{}
	response         domain.RespondChatPendingResponse
	err              error
}

type chatPendingResponseClaim struct {
	connection AppServerClient
	request    appserver.ServerRequest
	payload    any
	resolution domain.PendingResolution
	entry      *chatPendingResponseEntry
	wait       *chatPendingResponseEntry
}

func (s *Service) configureConnection(connection AppServerClient) {
	setter, ok := connection.(forwardedServerRequestHookSetter)
	if ok {
		setter.SetForwardedServerRequestHook(func(request appserver.ServerRequest) bool {
			return s.handleForwardedServerRequest(connection, request)
		})
	}
	s.configureNotificationBridge(connection)
}

func (s *Service) handleForwardedServerRequest(connection AppServerClient, request appserver.ServerRequest) bool {
	if _, ok := pending.MethodPendingType(request.Method); !ok {
		return false
	}
	s.cleanupStalePendingRecords()
	sessionGroupID := connectionSessionGroupID(connection)
	if sessionGroupID == "" {
		return false
	}
	session, ok := s.sessions[sessionGroupID]
	if !ok {
		return false
	}
	chatID := appserver.ParseThreadID(request.Params)
	runID := appserver.ParseTurnID(request.Params)
	if chatID == "" || runID == "" {
		return false
	}
	scope := chatstate.Scope{
		SessionGroupID: sessionGroupID,
		WorkspaceID:    session.Group.WorkspaceID,
		ChatID:         chatID,
	}
	active, ok := s.store.ActiveRun(scope)
	if !ok || active.RunID != runID || active.State == chatstate.RunStateInterrupting {
		return false
	}

	build, err := pending.Build(request.Method, request.Params, request.ID, pending.BuildInput{
		ThreadID:          chatID,
		TurnID:            runID,
		CreatedAtUnixMS:   nowUnixMilli(),
		RedactString:      chatPendingRedactFunc(connection, session),
		SanitizePathLabel: chatPendingPathFunc(session),
	})
	if err != nil {
		s.autoResolveChatPending(connection, request, build)
		return true
	}
	record := build.Record
	if record == nil {
		s.autoResolveChatPending(connection, request, build)
		return true
	}

	runScope := chatstate.RunScope{Scope: scope, RunID: runID}
	pendingRequestID := s.nextPendingRequestID()
	record.Pending.PendingRequestID = pendingRequestID
	record.Pending.ThreadID = chatID
	record.Pending.TurnID = runID
	record.Active = true
	if record.Responses == nil {
		record.Responses = map[string]*pending.ResponseEntry{}
	}
	chatPending := chatPendingRequestFromRecord(record)
	if _, _, claimed, err := s.store.RegisterPendingForActiveRun(chatstate.PendingInput{
		RunScope:         runScope,
		PendingRequestID: pendingRequestID,
	}); err != nil {
		s.autoResolveChatPending(connection, request, build)
		return true
	} else if !claimed {
		return false
	}
	_, _ = s.store.AppendEvent(chatstate.EventInput{
		RunScope:       runScope,
		Kind:           "pending_created",
		State:          string(chatstate.RunStatePending),
		PendingCreated: chatPending,
		SizeBytes:      chatPendingEventSize(chatPending),
	})

	s.mu.Lock()
	s.pendingRecords[chatPendingKey(runScope, pendingRequestID)] = &chatPendingRecord{
		runScope:   runScope,
		record:     record,
		request:    cloneServerRequest(request),
		connection: connection,
		active:     true,
		responses:  map[string]*chatPendingResponseEntry{},
	}
	s.mu.Unlock()
	return true
}

func (s *Service) RespondChatPending(ctx context.Context, command domain.RespondChatPendingCommand) (domain.RespondChatPendingResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	command, _, err := s.prepareRespondChatPending(command)
	if err != nil {
		return domain.RespondChatPendingResponse{}, err
	}
	if err := callerErrorFromContext(ctx, command.SessionGroupID, command.ClientResponseID); err != nil {
		return domain.RespondChatPendingResponse{}, err
	}
	idempotencyScope := respondChatPendingIdempotencyScope(command)
	entry, reused, err := s.store.ReserveIdempotency(idempotencyScope)
	if err != nil {
		return domain.RespondChatPendingResponse{}, err
	}
	if reused {
		return s.respondChatPendingResponseFromIdempotency(command, entry)
	}

	claim, err := s.claimChatPendingResponse(command)
	if err != nil {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.RespondChatPendingResponse{}, err
	}
	if claim.wait != nil {
		response, err := waitForChatPendingResponse(ctx, claim.wait)
		if err == nil {
			_, _ = s.store.CompleteIdempotency(idempotencyScope, chatstate.IdempotencyResultRef{
				ChatID:      response.ChatID,
				RunID:       response.RunID,
				Status:      s.respondPendingResultState(response),
				LastEventID: response.LastEventID,
			})
		}
		return response, err
	}

	writeErr := claim.connection.RespondServerRequest(context.Background(), claim.request, claim.payload, s.pendingResponseTimeout)
	response, err := s.completeChatPendingResponse(command, claim.entry, claim.resolution, writeErr)
	if err != nil {
		return domain.RespondChatPendingResponse{}, err
	}
	if _, err := s.store.CompleteIdempotency(idempotencyScope, chatstate.IdempotencyResultRef{
		ChatID:      response.ChatID,
		RunID:       response.RunID,
		Status:      s.respondPendingResultState(response),
		LastEventID: response.LastEventID,
	}); err != nil {
		return domain.RespondChatPendingResponse{}, err
	}
	return response, nil
}

func (s *Service) InterruptChatRun(ctx context.Context, command domain.InterruptChatRunCommand) (domain.InterruptChatRunResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	command, session, err := s.prepareInterruptChatRun(command)
	if err != nil {
		return domain.InterruptChatRunResponse{}, err
	}
	if err := callerErrorFromContext(ctx, command.SessionGroupID, command.ClientRequestID); err != nil {
		return domain.InterruptChatRunResponse{}, err
	}
	idempotencyScope := interruptChatRunIdempotencyScope(command)
	entry, reused, err := s.store.ReserveIdempotency(idempotencyScope)
	if err != nil {
		return domain.InterruptChatRunResponse{}, err
	}
	if reused {
		return s.interruptChatRunResponseFromIdempotency(command, entry)
	}

	scope := chatstate.Scope{
		SessionGroupID: command.SessionGroupID,
		WorkspaceID:    command.WorkspaceID,
		ChatID:         command.ChatID,
	}
	active, ok := s.store.ActiveRun(scope)
	if !ok {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.InterruptChatRunResponse{}, noActiveRun(command.SessionGroupID, command.ChatID, command.ClientRequestID)
	}
	if active.RunID != command.RunID {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.InterruptChatRunResponse{}, runMismatch(command.SessionGroupID, command.ChatID, command.RunID, command.ClientRequestID)
	}
	runScope := chatstate.RunScope{Scope: scope, RunID: command.RunID}
	if active.State == chatstate.RunStateInterrupting {
		response := domain.InterruptChatRunResponse{
			ChatID:              command.ChatID,
			RunID:               command.RunID,
			SessionGroupID:      command.SessionGroupID,
			WorkspaceID:         command.WorkspaceID,
			Status:              s.statusForRun(runScope, chatstate.RunStateInterrupting, 0),
			AlreadyInterrupting: true,
		}
		_, _ = s.store.CompleteIdempotency(idempotencyScope, chatstate.IdempotencyResultRef{
			ChatID: response.ChatID,
			RunID:  response.RunID,
			Status: string(chatstate.RunStateInterrupting),
		})
		return response, nil
	}

	connection, err := session.ConnectionProvider.Connection(ctx)
	if err != nil {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.InterruptChatRunResponse{}, appServerCallError(err, command.SessionGroupID, command.ClientRequestID, command.ChatID)
	}
	if connection == nil {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.InterruptChatRunResponse{}, internalGatewayError(command.SessionGroupID, command.ClientRequestID, command.ChatID)
	}
	s.configureConnection(connection)
	if err := callerErrorFromContext(ctx, command.SessionGroupID, command.ClientRequestID); err != nil {
		_ = s.store.ReleaseIdempotency(idempotencyScope)
		return domain.InterruptChatRunResponse{}, err
	}
	if _, err := connection.InterruptTurn(ctx, appserver.TurnInterruptCall{
		TurnID:  command.RunID,
		Timeout: s.turnInterruptTimeout,
	}); err != nil {
		return domain.InterruptChatRunResponse{}, appServerCallError(err, command.SessionGroupID, command.ClientRequestID, command.ChatID)
	}
	_, _ = s.store.UpdateRunState(runScope, chatstate.RunStateInterrupting)
	event, err := s.store.AppendEvent(chatstate.EventInput{
		RunScope:  runScope,
		Kind:      "status",
		State:     string(chatstate.RunStateInterrupting),
		SizeBytes: int64(len(command.ChatID) + len(command.RunID)),
	})
	if err != nil {
		return domain.InterruptChatRunResponse{}, err
	}
	response := domain.InterruptChatRunResponse{
		ChatID:         command.ChatID,
		RunID:          command.RunID,
		SessionGroupID: command.SessionGroupID,
		WorkspaceID:    command.WorkspaceID,
		Status:         s.statusForRun(runScope, chatstate.RunStateInterrupting, event.EventID),
		InterruptSent:  true,
		LastEventID:    event.EventID,
	}
	if _, err := s.store.CompleteIdempotency(idempotencyScope, chatstate.IdempotencyResultRef{
		ChatID:      response.ChatID,
		RunID:       response.RunID,
		Status:      string(chatstate.RunStateInterrupting),
		LastEventID: response.LastEventID,
	}); err != nil {
		return domain.InterruptChatRunResponse{}, err
	}
	return response, nil
}

func (s *Service) prepareRespondChatPending(command domain.RespondChatPendingCommand) (domain.RespondChatPendingCommand, Session, error) {
	lookup, session, err := s.prepareChatLookup(command.SessionGroupID, command.WorkspaceID, command.ChatID)
	if err != nil {
		return domain.RespondChatPendingCommand{}, Session{}, err
	}
	command.SessionGroupID = lookup.SessionGroupID
	command.WorkspaceID = lookup.WorkspaceID
	if command.PendingRequestID == "" {
		return domain.RespondChatPendingCommand{}, Session{}, invalidRequest(command.SessionGroupID, command.ClientResponseID, "pending_request_id is required")
	}
	if command.ClientResponseID == "" {
		return domain.RespondChatPendingCommand{}, Session{}, invalidRequest(command.SessionGroupID, "", "client_response_id is required")
	}
	if command.IdempotencyKey == "" {
		return domain.RespondChatPendingCommand{}, Session{}, invalidRequest(command.SessionGroupID, command.ClientResponseID, "idempotency_key is required")
	}
	return command, session, nil
}

func (s *Service) prepareInterruptChatRun(command domain.InterruptChatRunCommand) (domain.InterruptChatRunCommand, Session, error) {
	lookup, session, err := s.prepareChatLookup(command.SessionGroupID, command.WorkspaceID, command.ChatID)
	if err != nil {
		return domain.InterruptChatRunCommand{}, Session{}, err
	}
	command.SessionGroupID = lookup.SessionGroupID
	command.WorkspaceID = lookup.WorkspaceID
	if command.RunID == "" {
		return domain.InterruptChatRunCommand{}, Session{}, invalidRequest(command.SessionGroupID, command.ClientRequestID, "run_id is required")
	}
	if command.ClientRequestID == "" {
		return domain.InterruptChatRunCommand{}, Session{}, invalidRequest(command.SessionGroupID, "", "client_request_id is required")
	}
	if command.IdempotencyKey == "" {
		return domain.InterruptChatRunCommand{}, Session{}, invalidRequest(command.SessionGroupID, command.ClientRequestID, "idempotency_key is required")
	}
	return command, session, nil
}

func (s *Service) claimChatPendingResponse(command domain.RespondChatPendingCommand) (chatPendingResponseClaim, error) {
	active, ok := s.store.ActiveRun(chatstate.Scope{
		SessionGroupID: command.SessionGroupID,
		WorkspaceID:    command.WorkspaceID,
		ChatID:         command.ChatID,
	})
	if !ok {
		return chatPendingResponseClaim{}, pendingUnavailableAfterRestart(command.SessionGroupID, command.ChatID, command.PendingRequestID, command.ClientResponseID)
	}
	runScope := chatstate.RunScope{
		Scope: chatstate.Scope{
			SessionGroupID: command.SessionGroupID,
			WorkspaceID:    command.WorkspaceID,
			ChatID:         command.ChatID,
		},
		RunID: active.RunID,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	tracked := s.pendingRecords[chatPendingKey(runScope, command.PendingRequestID)]
	if tracked == nil {
		return chatPendingResponseClaim{}, unknownPending(command.SessionGroupID, command.ChatID, command.PendingRequestID, command.ClientResponseID)
	}
	record := tracked.record
	if record == nil {
		return chatPendingResponseClaim{}, unknownPending(command.SessionGroupID, command.ChatID, command.PendingRequestID, command.ClientResponseID)
	}
	if existing := tracked.responses[command.ClientResponseID]; existing != nil {
		if existing.idempotencyKey != command.IdempotencyKey {
			return chatPendingResponseClaim{}, pendingResponseScopeMismatch(command.SessionGroupID, command.ChatID, command.PendingRequestID, command.ClientResponseID)
		}
		if existing.state == pending.ResponseStateResponding {
			return chatPendingResponseClaim{wait: existing}, nil
		}
		if existing.err != nil {
			return chatPendingResponseClaim{}, existing.err
		}
		return chatPendingResponseClaim{wait: existing}, nil
	}
	if !tracked.active || tracked.inFlight != "" {
		return chatPendingResponseClaim{}, pendingAlreadyResolved(command.SessionGroupID, command.ChatID, command.PendingRequestID, command.ClientResponseID)
	}
	if _, err := s.store.ClaimPendingResolution(tracked.runScope, command.PendingRequestID); err != nil {
		delete(s.pendingRecords, chatPendingKey(tracked.runScope, command.PendingRequestID))
		return chatPendingResponseClaim{}, err
	}
	if !pending.ResponseMatchesType(record.Pending.PendingType, command.Response) {
		_ = s.store.ReleasePendingResolutionClaim(tracked.runScope, command.PendingRequestID)
		return chatPendingResponseClaim{}, responseTypeMismatch(command.SessionGroupID, command.ChatID, command.PendingRequestID, command.ClientResponseID)
	}
	validated, err := pending.ValidateResponse(record, command.Response, sensitiveRegistry(tracked.connection))
	if err != nil {
		_ = s.store.ReleasePendingResolutionClaim(tracked.runScope, command.PendingRequestID)
		return chatPendingResponseClaim{}, invalidPendingResponse(command.SessionGroupID, command.ChatID, command.PendingRequestID, command.ClientResponseID)
	}
	entry := &chatPendingResponseEntry{
		clientResponseID: command.ClientResponseID,
		idempotencyKey:   command.IdempotencyKey,
		runScope:         tracked.runScope,
		state:            pending.ResponseStateResponding,
		done:             make(chan struct{}),
	}
	tracked.responses[command.ClientResponseID] = entry
	tracked.inFlight = command.ClientResponseID
	return chatPendingResponseClaim{
		connection: tracked.connection,
		request:    cloneServerRequest(tracked.request),
		payload:    validated.Payload,
		resolution: validated.Resolution,
		entry:      entry,
	}, nil
}

func (s *Service) completeChatPendingResponse(
	command domain.RespondChatPendingCommand,
	entry *chatPendingResponseEntry,
	resolution domain.PendingResolution,
	writeErr error,
) (domain.RespondChatPendingResponse, error) {
	runScope := entry.runScope
	key := chatPendingKey(runScope, command.PendingRequestID)
	s.mu.Lock()
	tracked := s.pendingRecords[key]
	if tracked == nil || tracked.record == nil {
		s.mu.Unlock()
		return domain.RespondChatPendingResponse{}, unknownPending(command.SessionGroupID, command.ChatID, command.PendingRequestID, command.ClientResponseID)
	}
	tracked.inFlight = ""
	if writeErr != nil {
		responseErr := dispatcherUnavailable(command.SessionGroupID, command.ChatID, command.PendingRequestID, command.ClientResponseID)
		entry.state = pending.ResponseStateFailed
		entry.err = responseErr
		close(entry.done)
		s.mu.Unlock()
		_ = s.store.ReleasePendingResolutionClaim(runScope, command.PendingRequestID)
		return domain.RespondChatPendingResponse{}, responseErr
	}
	pendingType := tracked.record.Pending.PendingType
	delete(s.pendingRecords, key)
	s.mu.Unlock()

	if _, err := s.store.ResolvePending(runScope, command.PendingRequestID); err != nil {
		s.mu.Lock()
		entry.state = pending.ResponseStateFailed
		entry.err = err
		close(entry.done)
		s.mu.Unlock()
		return domain.RespondChatPendingResponse{}, err
	}
	restored, err := s.store.RestoreRunStateAfterPending(runScope)
	if err != nil {
		return domain.RespondChatPendingResponse{}, err
	}
	nextState := restored.State
	resolved := &domain.ChatPendingResolved{
		PendingRequestID: command.PendingRequestID,
		PendingType:      pendingType,
		Resolution:       resolution,
	}
	event, err := s.store.AppendEvent(chatstate.EventInput{
		RunScope:        runScope,
		Kind:            "pending_resolved",
		State:           string(nextState),
		PendingResolved: resolved,
		SizeBytes:       int64(len(command.ChatID) + len(command.PendingRequestID) + 64),
	})
	if err != nil {
		return domain.RespondChatPendingResponse{}, err
	}
	response := domain.RespondChatPendingResponse{
		ChatID:           command.ChatID,
		RunID:            runScope.RunID,
		SessionGroupID:   command.SessionGroupID,
		WorkspaceID:      command.WorkspaceID,
		PendingRequestID: command.PendingRequestID,
		ClientResponseID: command.ClientResponseID,
		Accepted:         true,
		LastEventID:      event.EventID,
		Status:           s.statusForRun(runScope, nextState, event.EventID),
	}
	s.mu.Lock()
	entry.state = pending.ResponseStateAccepted
	entry.response = response
	close(entry.done)
	s.mu.Unlock()
	return response, nil
}

func waitForChatPendingResponse(ctx context.Context, entry *chatPendingResponseEntry) (domain.RespondChatPendingResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-entry.done:
		if entry.err != nil {
			return domain.RespondChatPendingResponse{}, entry.err
		}
		response := entry.response
		response.AlreadyApplied = true
		return response, nil
	case <-ctx.Done():
		return domain.RespondChatPendingResponse{}, callerErrorFromContext(ctx, "", "")
	}
}

func (s *Service) respondChatPendingResponseFromIdempotency(command domain.RespondChatPendingCommand, entry chatstate.IdempotencyEntry) (domain.RespondChatPendingResponse, error) {
	if entry.State != chatstate.IdempotencyStateSucceeded || entry.Result.ChatID == "" || entry.Result.RunID == "" {
		return domain.RespondChatPendingResponse{}, idempotencyResultUnavailable(command.SessionGroupID, command.ClientResponseID)
	}
	runScope := chatstate.RunScope{
		Scope: chatstate.Scope{
			SessionGroupID: command.SessionGroupID,
			WorkspaceID:    command.WorkspaceID,
			ChatID:         command.ChatID,
		},
		RunID: entry.Result.RunID,
	}
	state := chatstate.RunState(entry.Result.Status)
	if state == "" {
		state = chatstate.RunStateRunning
	}
	return domain.RespondChatPendingResponse{
		ChatID:           entry.Result.ChatID,
		RunID:            entry.Result.RunID,
		SessionGroupID:   command.SessionGroupID,
		WorkspaceID:      command.WorkspaceID,
		PendingRequestID: command.PendingRequestID,
		ClientResponseID: command.ClientResponseID,
		Accepted:         true,
		AlreadyApplied:   true,
		LastEventID:      entry.Result.LastEventID,
		Status:           s.statusForRun(runScope, state, entry.Result.LastEventID),
	}, nil
}

func (s *Service) interruptChatRunResponseFromIdempotency(command domain.InterruptChatRunCommand, entry chatstate.IdempotencyEntry) (domain.InterruptChatRunResponse, error) {
	if entry.State != chatstate.IdempotencyStateSucceeded || entry.Result.ChatID == "" || entry.Result.RunID == "" {
		return domain.InterruptChatRunResponse{}, idempotencyResultUnavailable(command.SessionGroupID, command.ClientRequestID)
	}
	runScope := chatstate.RunScope{
		Scope: chatstate.Scope{
			SessionGroupID: command.SessionGroupID,
			WorkspaceID:    command.WorkspaceID,
			ChatID:         command.ChatID,
		},
		RunID: entry.Result.RunID,
	}
	return domain.InterruptChatRunResponse{
		ChatID:         entry.Result.ChatID,
		RunID:          entry.Result.RunID,
		SessionGroupID: command.SessionGroupID,
		WorkspaceID:    command.WorkspaceID,
		Status:         s.statusForRun(runScope, chatstate.RunStateInterrupting, entry.Result.LastEventID),
		InterruptSent:  true,
		LastEventID:    entry.Result.LastEventID,
	}, nil
}

func respondChatPendingIdempotencyScope(command domain.RespondChatPendingCommand) chatstate.IdempotencyScope {
	return chatstate.IdempotencyScope{
		Operation:        chatstate.OperationRespondPending,
		SessionGroupID:   command.SessionGroupID,
		WorkspaceID:      command.WorkspaceID,
		ChatID:           command.ChatID,
		PendingRequestID: command.PendingRequestID,
		ClientMessageID:  command.ClientResponseID,
		Key:              command.IdempotencyKey,
	}
}

func interruptChatRunIdempotencyScope(command domain.InterruptChatRunCommand) chatstate.IdempotencyScope {
	return chatstate.IdempotencyScope{
		Operation:       chatstate.OperationInterruptChatRun,
		SessionGroupID:  command.SessionGroupID,
		WorkspaceID:     command.WorkspaceID,
		ChatID:          command.ChatID,
		ClientMessageID: command.ClientRequestID,
		Key:             command.IdempotencyKey,
	}
}

func (s *Service) statusForRun(scope chatstate.RunScope, state chatstate.RunState, lastEventID uint64) domain.ChatStatus {
	status := baseChatStatus(scope.SessionGroupID, scope.WorkspaceID, scope.ChatID, s.store.Epoch())
	status.ThreadLifecycle = domain.ChatThreadLifecycleActiveRunning
	status.CurrentRunLifecycle = turnLifecycleFromRunState(state)
	status.CurrentRunID = scope.RunID
	status.LastRunID = scope.RunID
	status.LastEventID = lastEventID
	status.GatewayLocal.Live = true
	status.GatewayLocal.ReplayAvailable = true
	status.GatewayLocal.ReplayUnavailable = false
	status.ActivePending = s.activePendingRequests(scope)
	return status
}

func (s *Service) activePendingRequests(scope chatstate.RunScope) []domain.ChatPendingRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupStalePendingRecordsLocked()
	var active []domain.ChatPendingRequest
	for key, tracked := range s.pendingRecords {
		if tracked == nil || !tracked.active || tracked.runScope != scope || tracked.record == nil {
			continue
		}
		if _, ok := s.store.Pending(tracked.runScope, tracked.record.Pending.PendingRequestID); !ok {
			delete(s.pendingRecords, key)
			continue
		}
		active = append(active, *chatPendingRequestFromRecord(tracked.record))
	}
	return active
}

func (s *Service) cleanupStalePendingRecords() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupStalePendingRecordsLocked()
}

func (s *Service) cleanupStalePendingRecordsLocked() {
	for key, tracked := range s.pendingRecords {
		if tracked == nil || tracked.record == nil {
			delete(s.pendingRecords, key)
			continue
		}
		if _, ok := s.store.Pending(tracked.runScope, tracked.record.Pending.PendingRequestID); !ok {
			delete(s.pendingRecords, key)
		}
	}
}

func (s *Service) respondPendingResultState(response domain.RespondChatPendingResponse) string {
	scope := chatstate.Scope{
		SessionGroupID: response.SessionGroupID,
		WorkspaceID:    response.WorkspaceID,
		ChatID:         response.ChatID,
	}
	if active, ok := s.store.ActiveRun(scope); ok && active.RunID == response.RunID {
		return string(active.State)
	}
	return string(chatstate.RunStateRunning)
}

func (s *Service) nextPendingRequestID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextPendingSeq++
	return "pending-" + strconv.FormatUint(s.nextPendingSeq, 10)
}

func chatPendingRequestFromRecord(record *pending.Record) *domain.ChatPendingRequest {
	if record == nil {
		return nil
	}
	return &domain.ChatPendingRequest{
		PendingRequestID: record.Pending.PendingRequestID,
		ChatID:           record.Pending.ThreadID,
		RunID:            record.Pending.TurnID,
		PendingType:      record.Pending.PendingType,
		CreatedAtUnixMS:  record.Pending.CreatedAtUnixMS,
		ItemID:           record.Pending.ItemID,
		Display:          record.Pending.Display,
	}
}

func chatPendingKey(scope chatstate.RunScope, pendingRequestID string) string {
	return scope.SessionGroupID + "\x00" + scope.WorkspaceID + "\x00" + scope.ChatID + "\x00" + scope.RunID + "\x00" + pendingRequestID
}

func chatPendingEventSize(request *domain.ChatPendingRequest) int64 {
	if request == nil {
		return 0
	}
	return int64(len(request.PendingRequestID) + len(request.ChatID) + len(request.RunID) + len(request.ItemID) + 256)
}

func connectionSessionGroupID(connection AppServerClient) string {
	provider, ok := connection.(interface{ SessionGroupID() string })
	if !ok {
		return ""
	}
	return provider.SessionGroupID()
}

func sensitiveRegistry(connection AppServerClient) *redact.Registry {
	provider, ok := connection.(sensitiveRegistryProvider)
	if !ok {
		return nil
	}
	return provider.SensitiveRegistry()
}

func chatPendingRedactFunc(connection AppServerClient, session Session) func(string, int, string) string {
	registry := sensitiveRegistry(connection)
	var options []redact.Option
	if registry != nil {
		options = append(options, redact.WithConnectionRegistry(registry))
	}
	if sanitizer, err := redact.NewPathSanitizer(session.Group.CanonicalCWD); err == nil {
		options = append(options, redact.WithPathSanitizer(sanitizer))
	}
	redactor := redact.New(options...)
	return func(value string, maxBytes int, fallback string) string {
		value = redactor.RedactString(value)
		if value == "" {
			return fallback
		}
		return truncateUTF8Bytes(value, maxBytes)
	}
}

func chatPendingPathFunc(session Session) func(string) (string, bool) {
	sanitizer, err := redact.NewPathSanitizer(session.Group.CanonicalCWD)
	if err != nil {
		return func(string) (string, bool) {
			return redact.PathMarker, false
		}
	}
	return func(path string) (string, bool) {
		label := sanitizer.SanitizeLabel(path)
		return label, label != "" && label != redact.PathMarker
	}
}

func truncateUTF8Bytes(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	for maxBytes > 0 && !utf8.ValidString(value[:maxBytes]) {
		maxBytes--
	}
	if maxBytes <= 0 {
		return ""
	}
	return value[:maxBytes]
}

func cloneServerRequest(request appserver.ServerRequest) appserver.ServerRequest {
	return appserver.ServerRequest{
		ID:     append(json.RawMessage(nil), request.ID...),
		Method: request.Method,
		Params: append(json.RawMessage(nil), request.Params...),
		TaskID: request.TaskID,
	}
}

func (s *Service) autoResolveChatPending(connection AppServerClient, request appserver.ServerRequest, build pending.BuildResult) {
	pendingType, ok := pending.MethodPendingType(request.Method)
	if !ok {
		return
	}
	payload, isError, code, message := pending.AutoResolutionPayload(pendingType)
	var err error
	if isError {
		err = respondServerRequestError(connection, request, code, message, s.pendingResponseTimeout)
	} else {
		err = connection.RespondServerRequest(context.Background(), request, payload, s.pendingResponseTimeout)
	}
	if err != nil {
		return
	}
	chatID := appserver.ParseThreadID(request.Params)
	runID := appserver.ParseTurnID(request.Params)
	s.store.RecordDiagnostic(chatstate.DiagnosticRecord{
		SessionGroupID: connectionSessionGroupID(connection),
		ChatID:         chatID,
		RunID:          runID,
		RequestID:      build.RequestType,
		State:          "pending_auto_resolved",
		Reason:         domain.ReasonResourceExhausted,
	})
}

func respondServerRequestError(connection AppServerClient, request appserver.ServerRequest, code int, message string, timeout time.Duration) error {
	responder, ok := connection.(interface {
		RespondServerRequestError(context.Context, appserver.ServerRequest, int, string, time.Duration) error
	})
	if !ok {
		return fmt.Errorf("app-server error response is unavailable")
	}
	return responder.RespondServerRequestError(context.Background(), request, code, message, timeout)
}

func nowUnixMilli() int64 {
	return time.Now().UnixMilli()
}

func unknownPending(sessionGroupID string, chatID string, pendingRequestID string, clientResponseID string) error {
	return chatPendingError(domain.GatewayErrorCodeNotFound, domain.ReasonUnknownPendingRequest, "unknown pending request", sessionGroupID, chatID, pendingRequestID, clientResponseID, false)
}

func pendingUnavailableAfterRestart(sessionGroupID string, chatID string, pendingRequestID string, clientResponseID string) error {
	return chatPendingError(domain.GatewayErrorCodeUnavailable, domain.ReasonPendingUnavailableAfterRestart, "pending correlation is unavailable in this gateway process", sessionGroupID, chatID, pendingRequestID, clientResponseID, true)
}

func responseTypeMismatch(sessionGroupID string, chatID string, pendingRequestID string, clientResponseID string) error {
	return chatPendingError(domain.GatewayErrorCodeInvalidArgument, domain.ReasonResponseTypeMismatch, "pending response type does not match request", sessionGroupID, chatID, pendingRequestID, clientResponseID, false)
}

func pendingResponseScopeMismatch(sessionGroupID string, chatID string, pendingRequestID string, clientResponseID string) error {
	return chatPendingError(domain.GatewayErrorCodeAborted, domain.ReasonIdempotencyScopeMismatch, "pending response idempotency key was reused with a different safe scope", sessionGroupID, chatID, pendingRequestID, clientResponseID, false)
}

func pendingAlreadyResolved(sessionGroupID string, chatID string, pendingRequestID string, clientResponseID string) error {
	return chatPendingError(domain.GatewayErrorCodeFailedPrecondition, domain.ReasonPendingRequestAlreadyResolved, "pending request is already resolved", sessionGroupID, chatID, pendingRequestID, clientResponseID, false)
}

func invalidPendingResponse(sessionGroupID string, chatID string, pendingRequestID string, clientResponseID string) error {
	return chatPendingError(domain.GatewayErrorCodeInvalidArgument, domain.ReasonInvalidRequest, "pending response is invalid", sessionGroupID, chatID, pendingRequestID, clientResponseID, false)
}

func dispatcherUnavailable(sessionGroupID string, chatID string, pendingRequestID string, clientResponseID string) error {
	return chatPendingError(domain.GatewayErrorCodeUnavailable, domain.ReasonDispatcherUnavailable, "app-server call failed", sessionGroupID, chatID, pendingRequestID, clientResponseID, true)
}

func chatPendingError(code domain.GatewayErrorCode, reason domain.GatewayErrorReason, message string, sessionGroupID string, chatID string, pendingRequestID string, clientResponseID string, retryable bool) error {
	return &domain.GatewayError{
		Code: code,
		Details: domain.GatewayErrorDetails{
			Reason:           reason,
			DisplayMessage:   message,
			SessionGroupID:   sessionGroupID,
			ThreadID:         chatID,
			PendingRequestID: pendingRequestID,
			ClientResponseID: clientResponseID,
			Retryable:        retryable,
		},
	}
}

func noActiveRun(sessionGroupID string, chatID string, clientRequestID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeFailedPrecondition,
		Details: domain.GatewayErrorDetails{
			Reason:          domain.ReasonInvalidRequest,
			DisplayMessage:  "chat has no active run to interrupt",
			SessionGroupID:  sessionGroupID,
			ThreadID:        chatID,
			ClientMessageID: clientRequestID,
		},
	}
}

func runMismatch(sessionGroupID string, chatID string, runID string, clientRequestID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeFailedPrecondition,
		Details: domain.GatewayErrorDetails{
			Reason:          domain.ReasonInvalidRequest,
			DisplayMessage:  "run_id does not match the active run",
			SessionGroupID:  sessionGroupID,
			ThreadID:        chatID,
			ClientMessageID: clientRequestID,
		},
	}
}

func internalGatewayError(sessionGroupID string, clientMessageID string, chatID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeInternal,
		Details: domain.GatewayErrorDetails{
			Reason:          domain.ReasonInternalGatewayError,
			DisplayMessage:  "internal gateway error",
			SessionGroupID:  sessionGroupID,
			ClientMessageID: clientMessageID,
			ThreadID:        chatID,
		},
	}
}
