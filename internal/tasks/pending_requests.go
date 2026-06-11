package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Dirard/codex-runtime/internal/appserver"
	"github.com/Dirard/codex-runtime/internal/domain"
	"github.com/Dirard/codex-runtime/internal/pending"
	"github.com/Dirard/codex-runtime/internal/redact"
)

const (
	pendingResponseWriteTimeout       = 10 * time.Second
	pendingAutoResolveTimeout         = 5 * time.Second
	unsupportedRequestWriteTimeout    = 5 * time.Second
	resolvedServerRequestBacklogLimit = 256
)

func (s *Service) handleServerRequest(request appserver.ServerRequest, connection *appserver.Connection) {
	pendingType, supported := pending.MethodPendingType(request.Method)
	if !supported {
		s.handleForwardedUnsupportedServerRequest(request, connection)
		return
	}

	_, taskID, subscribers, events, limitReason, ok := s.tryCreatePendingRequest(request, connection)
	if !ok {
		s.autoResolvePendingRequest(request, connection, pendingType, taskID, limitReason)
		return
	}
	for _, event := range events {
		s.publishEvent(taskID, subscribers, event, false)
	}
}

func (s *Service) tryCreatePendingRequest(
	request appserver.ServerRequest,
	connection *appserver.Connection,
) (pending.BuildResult, string, []*taskSubscriber, []domain.TaskEvent, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task := s.taskForServerRequestLocked(request, connection)
	if task == nil || isTerminalState(task.state) || task.state == domain.TaskStateInterrupting {
		return pending.BuildResult{}, "", nil, nil, pending.LimitReasonStatusBudgetExceeded, false
	}
	if task.pending == nil {
		task.pending = pending.NewManager()
	}

	input := pending.BuildInput{
		TaskID:            task.id,
		ThreadID:          task.threadID,
		TurnID:            task.turnID,
		CreatedAtUnixMS:   s.now().UnixMilli(),
		RedactString:      s.pendingRedactFuncLocked(task),
		SanitizePathLabel: s.pendingPathFuncLocked(task),
		CachedFileDiff:    cachedFileDiff(task, pendingItemID(request.Params)),
	}
	build, err := pending.Build(request.Method, request.Params, request.ID, input)
	if err != nil {
		if errors.Is(err, pending.ErrOverLimit) {
			return build, task.id, nil, nil, build.LimitReason, false
		}
		return build, task.id, nil, nil, pending.LimitReasonControlsTooLarge, false
	}
	limitReason := s.pendingAdmissionLimitReasonLocked(task, build.Record)
	if limitReason != "" {
		return build, task.id, nil, nil, limitReason, false
	}

	task.pending.Add(build.Record)
	task.state = domain.TaskStateWaitingForPendingRequest
	event, subscribers := s.appendEventLocked(task, domain.PendingRequestCreatedEvent{
		PendingRequestID: build.Record.Pending.PendingRequestID,
		PendingType:      build.Record.Pending.PendingType,
		Display:          build.Record.Pending.Display,
	})
	events := []domain.TaskEvent{event}
	if s.consumeResolvedServerRequestLocked(connection, build.Record) {
		task.pending.MarkResolved(build.Record)
		if task.pending.ActiveCount() == 0 {
			task.state = domain.TaskStateRunning
		}
		resolved, resolvedSubscribers := s.appendEventLocked(task, domain.PendingRequestResolvedEvent{
			PendingRequestID: build.Record.Pending.PendingRequestID,
			PendingType:      build.Record.Pending.PendingType,
			Resolution:       domain.PendingResolutionCleared,
		})
		events = append(events, resolved)
		subscribers = resolvedSubscribers
	}
	return build, task.id, subscribers, events, "", true
}

func (s *Service) autoResolvePendingRequest(
	request appserver.ServerRequest,
	connection *appserver.Connection,
	pendingType domain.PendingType,
	taskID string,
	limitReason string,
) {
	requestType := pending.RequestTypeForPendingType(pendingType)
	autoResolution := pending.AutoResolutionForPendingType(pendingType)
	if limitReason == "" {
		limitReason = pending.LimitReasonStatusBudgetExceeded
	}
	payload, isError, code, message := pending.AutoResolutionPayload(pendingType)
	var err error
	if isError {
		err = connection.RespondServerRequestError(context.Background(), request, code, message, pendingAutoResolveTimeout)
	} else {
		err = connection.RespondServerRequest(context.Background(), request, payload, pendingAutoResolveTimeout)
	}
	if err != nil {
		return
	}
	if taskID == "" {
		s.recordConnectionDiagnostic(connectionDiagnostic{
			SessionGroupID: connection.SessionGroupID(),
			Code:           pending.WarningCodeOverLimit,
			Method:         request.Method,
			RequestType:    requestType,
			AutoResolution: autoResolution,
			LimitReason:    limitReason,
		})
		return
	}
	s.publishPendingWarning(taskID, domain.GatewayWarningEvent{
		Code:           pending.WarningCodeOverLimit,
		Message:        "pending request exceeded gateway safety limits and was auto-resolved",
		RequestType:    requestType,
		AutoResolution: autoResolution,
		LimitReason:    limitReason,
	})
}

func (s *Service) handleForwardedUnsupportedServerRequest(request appserver.ServerRequest, connection *appserver.Connection) {
	taskID := ""
	s.mu.Lock()
	if task := s.taskForServerRequestLocked(request, connection); task != nil && !isTerminalState(task.state) {
		taskID = task.id
	}
	s.mu.Unlock()
	_ = connection.RespondServerRequestError(
		context.Background(),
		request,
		pending.UnsupportedServerRequestCode,
		pending.UnsupportedServerRequestMessage,
		unsupportedRequestWriteTimeout,
	)
	if taskID == "" {
		s.recordConnectionDiagnostic(connectionDiagnostic{
			SessionGroupID: connection.SessionGroupID(),
			Code:           pending.WarningCodeUnsupportedServerRequest,
			Method:         request.Method,
		})
		return
	}
	s.publishPendingWarning(taskID, domain.GatewayWarningEvent{
		Code:    pending.WarningCodeUnsupportedServerRequest,
		Message: "app-server sent unsupported request: " + request.Method,
	})
}

func (s *Service) publishPendingWarning(taskID string, warning domain.GatewayWarningEvent) {
	s.mu.Lock()
	task := s.tasks[taskID]
	if task == nil || isTerminalState(task.state) {
		s.mu.Unlock()
		return
	}
	warning = s.redactPayloadLocked(task, warning).(domain.GatewayWarningEvent)
	event, subscribers := s.appendEventLocked(task, warning)
	s.mu.Unlock()
	s.publishEvent(taskID, subscribers, event, false)
}

func (s *Service) taskForServerRequestLocked(request appserver.ServerRequest, connection *appserver.Connection) *task {
	root := decodeNotificationObject(request.Params)
	threadID := appserver.ParseThreadID(request.Params)
	turnID := appserver.ParseTurnID(request.Params)
	itemID := pendingItemID(request.Params)
	notification := appserver.Notification{
		Params: request.Params,
		TaskID: request.TaskID,
	}
	if request.TaskID != "" {
		task := s.taskForNotificationTaskIDLocked(notification, connection)
		if notificationTaskMatchesTurn(task, notification, threadID, turnID) {
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
	if itemID != "" {
		return s.taskForRegisteredItemLocked(connection, itemID)
	}
	if request.TaskID == "" && firstString(root, "taskId") != "" {
		request.TaskID = firstString(root, "taskId")
		return s.taskForServerRequestLocked(request, connection)
	}
	return nil
}

func (s *Service) pendingAdmissionLimitReasonLocked(task *task, record *pending.Record) string {
	session := s.sessions[task.sessionGroupID]
	if session == nil {
		return pending.LimitReasonStatusBudgetExceeded
	}
	maxActive := session.group.PendingLimits.MaxActiveRequests
	if maxActive <= 0 {
		maxActive = domain.MaxActivePendingRequests
	}
	maxDisplayBytes := session.group.PendingLimits.MaxDisplayPayloadBytes
	if maxDisplayBytes <= 0 {
		maxDisplayBytes = domain.MaxOutboundPendingDisplayPayloadBytes
	}
	statusBudgetBytes := session.group.PendingLimits.StatusNonPendingBudgetBytes
	if statusBudgetBytes <= 0 {
		statusBudgetBytes = 64 * domain.KiB
	}
	outboundBytes := session.group.GRPCLimits.OutboundMessageBytes
	if outboundBytes <= 0 {
		outboundBytes = 4 * domain.MiB
	}
	if task.pending.ActiveCount() >= maxActive {
		return pending.LimitReasonStatusBudgetExceeded
	}
	displayBytes := pending.DisplayPayloadBytes(record.Pending.Display)
	if int64(displayBytes) > maxDisplayBytes {
		return pending.LimitReasonDisplayPayloadTooLarge
	}
	totalDisplayBytes := int64(displayBytes)
	for _, active := range task.pending.Active() {
		totalDisplayBytes += int64(pending.DisplayPayloadBytes(active.Display))
	}
	if totalDisplayBytes+statusBudgetBytes > outboundBytes {
		return pending.LimitReasonStatusBudgetExceeded
	}
	return ""
}

func (s *Service) pendingRedactFuncLocked(task *task) func(string, int, string) string {
	redactor := s.scopedRedactorLocked(task)
	return func(value string, maxBytes int, fallback string) string {
		return publicTextWithRedactor(redactor, value, maxBytes, fallback)
	}
}

func (s *Service) pendingPathFuncLocked(task *task) func(string) (string, bool) {
	return func(path string) (string, bool) {
		if task == nil || task.pathSanitizer == nil {
			return redact.PathMarker, false
		}
		label := task.pathSanitizer.SanitizeLabel(path)
		return label, label != "" && label != redact.PathMarker
	}
}

func pendingItemID(params json.RawMessage) string {
	root := decodeNotificationObject(params)
	return firstString(root, "itemId", "itemID", "item.id", "id")
}

func cachedFileDiff(task *task, itemID string) *domain.FileDiffUpdatedEvent {
	if task == nil || itemID == "" {
		return nil
	}
	diff, ok := task.fileDiffs[itemID]
	if !ok {
		return nil
	}
	copied := diff
	return &copied
}

func (s *Service) ResolvePendingRequestSession(ctx context.Context, taskID string, pendingRequestID string) (domain.SessionGroupMetadata, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	task := s.tasks[taskID]
	if task == nil {
		return domain.SessionGroupMetadata{}, unknownTask("", taskID, "")
	}
	if task.pending == nil {
		return domain.SessionGroupMetadata{}, unknownPendingRequest(task.sessionGroupID, taskID, pendingRequestID, "")
	}
	record := task.pending.Get(pendingRequestID)
	if record == nil {
		return domain.SessionGroupMetadata{}, unknownPendingRequest(task.sessionGroupID, taskID, pendingRequestID, "")
	}
	session := s.sessions[task.sessionGroupID]
	if session == nil {
		return domain.SessionGroupMetadata{}, unknownSession(task.sessionGroupID)
	}
	return domain.SessionGroupMetadata{
		SessionGroupID:           session.group.SessionGroupID,
		WorkspaceID:              session.group.WorkspaceID,
		GRPCInboundMessageBytes:  int(session.group.GRPCLimits.InboundMessageBytes),
		GRPCOutboundMessageBytes: int(session.group.GRPCLimits.OutboundMessageBytes),
	}, nil
}

func (s *Service) RespondPendingRequest(ctx context.Context, command domain.RespondPendingRequestCommand) (domain.RespondPendingRequestResponse, error) {
	claim, err := s.claimPendingResponse(command)
	if err != nil {
		return domain.RespondPendingRequestResponse{}, err
	}
	if claim.wait != nil {
		return waitForPendingResponse(ctx, claim.wait)
	}

	err = claim.connection.RespondServerRequest(context.Background(), claim.request, claim.payload, pendingResponseWriteTimeout)
	return s.completePendingResponse(command.TaskID, command.PendingRequestID, command.ClientResponseID, claim.entry, claim.resolution, err)
}

type pendingResponseClaim struct {
	connection *appserver.Connection
	request    appserver.ServerRequest
	payload    any
	resolution domain.PendingResolution
	entry      *pending.ResponseEntry
	wait       *pending.ResponseEntry
}

func (s *Service) claimPendingResponse(command domain.RespondPendingRequestCommand) (pendingResponseClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[command.TaskID]
	if task == nil {
		return pendingResponseClaim{}, unknownTask("", command.TaskID, "")
	}
	if task.pending == nil {
		return pendingResponseClaim{}, unknownPendingRequest(task.sessionGroupID, command.TaskID, command.PendingRequestID, command.ClientResponseID)
	}
	record := task.pending.Get(command.PendingRequestID)
	if record == nil {
		return pendingResponseClaim{}, unknownPendingRequest(task.sessionGroupID, command.TaskID, command.PendingRequestID, command.ClientResponseID)
	}
	if !pending.ResponseMatchesType(record.Pending.PendingType, command.Response) {
		return pendingResponseClaim{}, responseTypeMismatch(task.sessionGroupID, command.TaskID, command.PendingRequestID, command.ClientResponseID)
	}
	fingerprint, err := domain.PendingResponseFingerprintV1SHA256Hex(command.TaskID, command.PendingRequestID, record.Pending.PendingType, command.Response)
	if err != nil {
		return pendingResponseClaim{}, invalidPendingResponse(task.sessionGroupID, command.TaskID, command.PendingRequestID, command.ClientResponseID)
	}
	if existing := record.Responses[command.ClientResponseID]; existing != nil {
		if existing.Fingerprint != fingerprint {
			return pendingResponseClaim{}, pendingResponseFingerprintMismatch(task.sessionGroupID, command.TaskID, command.PendingRequestID, command.ClientResponseID)
		}
		if existing.State == pending.ResponseStateResponding {
			return pendingResponseClaim{wait: existing}, nil
		}
		if existing.Err != nil {
			return pendingResponseClaim{}, existing.Err
		}
		return pendingResponseClaim{wait: existing}, nil
	}
	if !record.Active || record.InFlightClientResponseID != "" {
		return pendingResponseClaim{}, pendingRequestAlreadyResolved(task.sessionGroupID, command.TaskID, command.PendingRequestID, command.ClientResponseID)
	}
	validated, err := pending.ValidateResponse(record, command.Response, task.sensitive)
	if err != nil {
		return pendingResponseClaim{}, invalidPendingResponse(task.sessionGroupID, command.TaskID, command.PendingRequestID, command.ClientResponseID)
	}
	entry := &pending.ResponseEntry{
		ClientResponseID: command.ClientResponseID,
		Fingerprint:      fingerprint,
		State:            pending.ResponseStateResponding,
		Done:             make(chan struct{}),
	}
	record.Responses[command.ClientResponseID] = entry
	record.InFlightClientResponseID = command.ClientResponseID
	return pendingResponseClaim{
		connection: task.connection,
		request: appserver.ServerRequest{
			ID:     append(json.RawMessage(nil), record.JSONRPCID...),
			Method: record.Method,
		},
		payload:    validated.Payload,
		resolution: validated.Resolution,
		entry:      entry,
	}, nil
}

func (s *Service) completePendingResponse(
	taskID string,
	pendingRequestID string,
	clientResponseID string,
	entry *pending.ResponseEntry,
	resolution domain.PendingResolution,
	writeErr error,
) (domain.RespondPendingRequestResponse, error) {
	s.mu.Lock()
	task := s.tasks[taskID]
	if task == nil {
		s.mu.Unlock()
		return domain.RespondPendingRequestResponse{}, unknownTask("", taskID, "")
	}
	if task.pending == nil {
		s.mu.Unlock()
		return domain.RespondPendingRequestResponse{}, unknownPendingRequest(task.sessionGroupID, taskID, pendingRequestID, clientResponseID)
	}
	record := task.pending.Get(pendingRequestID)
	if record == nil {
		s.mu.Unlock()
		return domain.RespondPendingRequestResponse{}, unknownPendingRequest(task.sessionGroupID, taskID, pendingRequestID, clientResponseID)
	}
	if entry.State == pending.ResponseStateAccepted {
		response := entry.Response
		if !record.Active && response.ResolvedEventID == 0 {
			response.ResolvedEventID = pendingResolvedEventIDLocked(task, pendingRequestID)
			entry.Response = response
		}
		s.mu.Unlock()
		return response, nil
	}
	if entry.State == pending.ResponseStateFailed {
		responseErr := entry.Err
		s.mu.Unlock()
		return domain.RespondPendingRequestResponse{}, responseErr
	}
	if !record.Active {
		record.InFlightClientResponseID = ""
		response := domain.RespondPendingRequestResponse{
			TaskID:           task.id,
			SessionGroupID:   task.sessionGroupID,
			PendingRequestID: pendingRequestID,
			ClientResponseID: clientResponseID,
			Accepted:         true,
			AlreadyApplied:   true,
			ResolvedEventID:  pendingResolvedEventIDLocked(task, pendingRequestID),
		}
		entry.State = pending.ResponseStateAccepted
		entry.Response = response
		close(entry.Done)
		s.mu.Unlock()
		return response, nil
	}
	record.InFlightClientResponseID = ""
	if writeErr != nil {
		responseErr := dispatcherUnavailable(task.sessionGroupID, taskID, pendingRequestID, clientResponseID)
		task.pending.MarkResolved(record)
		if task.state == domain.TaskStateWaitingForPendingRequest && task.pending.ActiveCount() == 0 {
			task.state = domain.TaskStateRunning
		}
		event, subscribers := s.appendEventLocked(task, domain.PendingRequestResolvedEvent{
			PendingRequestID: pendingRequestID,
			PendingType:      record.Pending.PendingType,
			Resolution:       domain.PendingResolutionFailed,
		})
		entry.State = pending.ResponseStateFailed
		entry.Err = responseErr
		close(entry.Done)
		s.mu.Unlock()
		s.publishEvent(taskID, subscribers, event, false)
		return domain.RespondPendingRequestResponse{}, responseErr
	}
	task.pending.MarkResolved(record)
	if task.state == domain.TaskStateWaitingForPendingRequest && task.pending.ActiveCount() == 0 {
		task.state = domain.TaskStateRunning
	}
	event, subscribers := s.appendEventLocked(task, domain.PendingRequestResolvedEvent{
		PendingRequestID: pendingRequestID,
		PendingType:      record.Pending.PendingType,
		Resolution:       resolution,
	})
	response := domain.RespondPendingRequestResponse{
		TaskID:           task.id,
		SessionGroupID:   task.sessionGroupID,
		PendingRequestID: pendingRequestID,
		ClientResponseID: clientResponseID,
		Accepted:         true,
		ResolvedEventID:  event.EventID,
	}
	entry.State = pending.ResponseStateAccepted
	entry.Response = response
	close(entry.Done)
	s.mu.Unlock()
	s.publishEvent(taskID, subscribers, event, false)
	return response, nil
}

func pendingResolvedEventIDLocked(task *task, pendingRequestID string) uint64 {
	for i := len(task.events); i > 0; i-- {
		event := task.events[i-1]
		resolved, ok := event.Payload.(domain.PendingRequestResolvedEvent)
		if ok && resolved.PendingRequestID == pendingRequestID {
			return event.EventID
		}
	}
	return 0
}

func waitForPendingResponse(ctx context.Context, entry *pending.ResponseEntry) (domain.RespondPendingRequestResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-entry.Done:
		if entry.Err != nil {
			return domain.RespondPendingRequestResponse{}, entry.Err
		}
		response := entry.Response
		response.AlreadyApplied = true
		return response, nil
	case <-ctx.Done():
		return domain.RespondPendingRequestResponse{}, callerErrorFromContext(ctx, "", "")
	}
}

func (s *Service) handleServerRequestResolved(notification appserver.Notification, connection *appserver.Connection) {
	serverRequestID := serverRequestIDFromResolved(notification.Params)
	if len(serverRequestID) == 0 {
		return
	}
	if notification.ServerRequestResolvedChecked && !notification.ServerRequestResolvedMatched {
		return
	}
	s.mu.Lock()
	var event domain.TaskEvent
	var subscribers []*taskSubscriber
	var taskID string
	matched := false
	for _, task := range s.tasks {
		if task.connection != connection || task.pending == nil {
			continue
		}
		record := task.pending.ActiveByResolvedServerRequestID(serverRequestID)
		if record == nil {
			continue
		}
		matched = true
		task.pending.MarkResolved(record)
		if task.state == domain.TaskStateWaitingForPendingRequest && task.pending.ActiveCount() == 0 {
			task.state = domain.TaskStateRunning
		}
		event, subscribers = s.appendEventLocked(task, domain.PendingRequestResolvedEvent{
			PendingRequestID: record.Pending.PendingRequestID,
			PendingType:      record.Pending.PendingType,
			Resolution:       domain.PendingResolutionCleared,
		})
		taskID = task.id
		if clientResponseID := record.InFlightClientResponseID; clientResponseID != "" {
			if entry := record.Responses[clientResponseID]; entry != nil && entry.State == pending.ResponseStateResponding {
				entry.State = pending.ResponseStateAccepted
				entry.Response = domain.RespondPendingRequestResponse{
					TaskID:           task.id,
					SessionGroupID:   task.sessionGroupID,
					PendingRequestID: record.Pending.PendingRequestID,
					ClientResponseID: clientResponseID,
					Accepted:         true,
					AlreadyApplied:   true,
					ResolvedEventID:  event.EventID,
				}
				close(entry.Done)
			}
			record.InFlightClientResponseID = ""
		}
		break
	}
	if !matched && (!notification.ServerRequestResolvedChecked || notification.ServerRequestResolvedMatched) {
		s.markResolvedServerRequestLocked(connection, serverRequestID)
	}
	s.mu.Unlock()
	s.publishEvent(taskID, subscribers, event, false)
}

func (s *Service) markResolvedServerRequestLocked(connection *appserver.Connection, serverRequestID json.RawMessage) {
	if connection == nil || pending.RequestIDKey(serverRequestID) == "" {
		return
	}
	ids := s.resolvedServerRequests[connection]
	if ids == nil {
		ids = map[string]struct{}{}
		s.resolvedServerRequests[connection] = ids
	}
	if len(ids) >= resolvedServerRequestBacklogLimit {
		return
	}
	ids[resolvedServerRequestBacklogKey(serverRequestID)] = struct{}{}
}

func (s *Service) consumeResolvedServerRequestLocked(connection *appserver.Connection, record *pending.Record) bool {
	if connection == nil || record == nil {
		return false
	}
	ids := s.resolvedServerRequests[connection]
	if len(ids) == 0 {
		return false
	}
	if key, ok := resolvedServerRequestExactBacklogMatch(ids, record.JSONRPCID); ok {
		delete(ids, key)
	} else if key, ok := resolvedServerRequestLogicalBacklogMatch(ids, record); ok {
		delete(ids, key)
	} else {
		return false
	}
	if len(ids) == 0 {
		delete(s.resolvedServerRequests, connection)
	}
	return true
}

func logicalServerRequestID(record *pending.Record) string {
	if record == nil {
		return ""
	}
	if record.AppServerRequestID != "" {
		return record.AppServerRequestID
	}
	return pending.NormalizeServerRequestID("", record.JSONRPCID)
}

func serverRequestIDFromResolved(params json.RawMessage) json.RawMessage {
	if len(params) == 0 {
		return nil
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(params, &payload) != nil {
		return nil
	}
	for _, key := range []string{"requestId", "requestID"} {
		raw := payload[key]
		if pending.RequestIDKey(raw) != "" {
			return append(json.RawMessage(nil), raw...)
		}
	}
	return nil
}

func resolvedServerRequestBacklogKey(serverRequestID json.RawMessage) string {
	return pending.RequestIDKey(serverRequestID) + "\x00" + pending.NormalizeServerRequestID("", serverRequestID)
}

func splitResolvedServerRequestBacklogKey(key string) (string, string) {
	rawID, logicalID, ok := strings.Cut(key, "\x00")
	if !ok {
		return key, key
	}
	return rawID, logicalID
}

func resolvedServerRequestExactBacklogMatch(ids map[string]struct{}, jsonrpcID json.RawMessage) (string, bool) {
	rawID := pending.RequestIDKey(jsonrpcID)
	if rawID == "" {
		return "", false
	}
	for key := range ids {
		backlogRawID, _ := splitResolvedServerRequestBacklogKey(key)
		if backlogRawID == rawID {
			return key, true
		}
	}
	return "", false
}

func resolvedServerRequestLogicalBacklogMatch(ids map[string]struct{}, record *pending.Record) (string, bool) {
	serverRequestID := logicalServerRequestID(record)
	if serverRequestID == "" {
		return "", false
	}
	jsonrpcID := pending.NormalizeServerRequestID("", record.JSONRPCID)
	for key := range ids {
		_, backlogServerRequestID := splitResolvedServerRequestBacklogKey(key)
		if backlogServerRequestID == serverRequestID {
			if backlogServerRequestID == jsonrpcID {
				continue
			}
			return key, true
		}
	}
	return "", false
}

func (s *Service) clearPendingLocked(task *task, resolution domain.PendingResolution) []domain.TaskEventPayload {
	if task == nil || task.pending == nil {
		return nil
	}
	active := task.pending.Active()
	payloads := make([]domain.TaskEventPayload, 0, len(active))
	for _, activePending := range active {
		record := task.pending.Get(activePending.PendingRequestID)
		task.pending.MarkResolved(record)
		payloads = append(payloads, domain.PendingRequestResolvedEvent{
			PendingRequestID: activePending.PendingRequestID,
			PendingType:      activePending.PendingType,
			Resolution:       resolution,
		})
	}
	if task.state == domain.TaskStateWaitingForPendingRequest {
		task.state = domain.TaskStateRunning
	}
	return payloads
}
