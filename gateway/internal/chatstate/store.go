package chatstate

import (
	"fmt"
	"sync"
	"time"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

type Store struct {
	mu     sync.Mutex
	epoch  string
	now    func() time.Time
	limits Limits

	nextEventID           uint64
	nextSubscriberID      uint64
	activeRuns            map[string]*RunRecord
	activeRunReservations map[string]Scope
	replay                map[string]*replayBuffer
	subscribers           map[string]map[uint64]chan EventRecord
	pending               map[string]*PendingRecord
	idempotency           map[string]*IdempotencyEntry
	diagnostics           []DiagnosticRecord
}

type replayBuffer struct {
	events                    []EventRecord
	bytes                     int64
	startEvictedBeforeEventID uint64
}

func NewStore(options StoreOptions) *Store {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	epoch := options.Epoch
	if epoch == "" {
		epoch = fmt.Sprintf("epoch-%d", now().UnixNano())
	}
	return &Store{
		epoch:                 epoch,
		now:                   now,
		limits:                normalizeLimits(options.Limits),
		activeRuns:            map[string]*RunRecord{},
		activeRunReservations: map[string]Scope{},
		replay:                map[string]*replayBuffer{},
		subscribers:           map[string]map[uint64]chan EventRecord{},
		pending:               map[string]*PendingRecord{},
		idempotency:           map[string]*IdempotencyEntry{},
	}
}

func (s *Store) Epoch() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.epoch
}

func (s *Store) StartRun(scope RunScope, idempotencyKey string) (RunRecord, error) {
	if err := validateRunScope(scope); err != nil {
		return RunRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	key := activeRunKey(scope.Scope)
	if existing := s.activeRuns[key]; existing != nil && !isTerminalRunState(existing.State) {
		return RunRecord{}, alreadyRunning(scope)
	}
	if s.activeRunCapacityUsedLocked() >= s.limits.ActiveRunsCap {
		return RunRecord{}, resourceExhausted("active run capacity exceeded", scope.Scope)
	}
	now := s.now().UnixMilli()
	record := &RunRecord{
		Scope:           scope.Scope,
		RunID:           scope.RunID,
		State:           RunStateStarting,
		IdempotencyKey:  idempotencyKey,
		StartedAtUnixMS: now,
		UpdatedAtUnixMS: now,
	}
	s.activeRuns[key] = record
	return *record, nil
}

func (s *Store) ReserveActiveRunCapacity(scope IdempotencyScope) error {
	if err := validateIdempotencyScope(scope); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	key := activeRunReservationKey(scope)
	if _, ok := s.activeRunReservations[key]; ok {
		return nil
	}
	capacityScope := Scope{SessionGroupID: scope.SessionGroupID, WorkspaceID: scope.WorkspaceID, ChatID: scope.ChatID}
	if s.activeRunCapacityUsedLocked() >= s.limits.ActiveRunsCap {
		return resourceExhausted("active run capacity exceeded", capacityScope)
	}
	s.activeRunReservations[key] = capacityScope
	return nil
}

func (s *Store) ReleaseActiveRunCapacity(scope IdempotencyScope) error {
	if err := validateIdempotencyScope(scope); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.activeRunReservations, activeRunReservationKey(scope))
	return nil
}

func (s *Store) StartRunWithReservation(reservation IdempotencyScope, scope RunScope, idempotencyKey string) (RunRecord, error) {
	if err := validateIdempotencyScope(reservation); err != nil {
		return RunRecord{}, err
	}
	if err := validateRunScope(scope); err != nil {
		return RunRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	reservationKey := activeRunReservationKey(reservation)
	if _, ok := s.activeRunReservations[reservationKey]; !ok {
		return RunRecord{}, invalidRequest("active run capacity reservation is required", scope.Scope)
	}
	key := activeRunKey(scope.Scope)
	if existing := s.activeRuns[key]; existing != nil && !isTerminalRunState(existing.State) {
		return RunRecord{}, alreadyRunning(scope)
	}
	now := s.now().UnixMilli()
	record := &RunRecord{
		Scope:           scope.Scope,
		RunID:           scope.RunID,
		State:           RunStateStarting,
		IdempotencyKey:  idempotencyKey,
		StartedAtUnixMS: now,
		UpdatedAtUnixMS: now,
	}
	s.activeRuns[key] = record
	delete(s.activeRunReservations, reservationKey)
	return *record, nil
}

func (s *Store) UpdateRunState(scope RunScope, state RunState) (RunRecord, error) {
	if err := validateRunScope(scope); err != nil {
		return RunRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.activeRuns[activeRunKey(scope.Scope)]
	if record == nil || record.RunID != scope.RunID {
		return RunRecord{}, unknownActiveRun(scope)
	}
	record.State = state
	record.UpdatedAtUnixMS = s.now().UnixMilli()
	return *record, nil
}

func (s *Store) RestoreRunStateAfterPending(scope RunScope) (RunRecord, error) {
	if err := validateRunScope(scope); err != nil {
		return RunRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.activeRuns[activeRunKey(scope.Scope)]
	if record == nil || record.RunID != scope.RunID {
		return RunRecord{}, unknownActiveRun(scope)
	}
	if record.State != RunStateInterrupting {
		record.State = RunStateRunning
		record.UpdatedAtUnixMS = s.now().UnixMilli()
	}
	return *record, nil
}

func (s *Store) BindRunID(scope RunScope, runID string) (RunRecord, error) {
	if err := validateRunScope(scope); err != nil {
		return RunRecord{}, err
	}
	if runID == "" {
		return RunRecord{}, invalidRequest("run id is required", scope.Scope)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.activeRuns[activeRunKey(scope.Scope)]
	if record == nil || record.RunID != scope.RunID {
		return RunRecord{}, unknownActiveRun(scope)
	}
	record.RunID = runID
	record.UpdatedAtUnixMS = s.now().UnixMilli()
	return *record, nil
}

func (s *Store) CompleteRun(scope RunScope, state RunState) (RunRecord, error) {
	if !isTerminalRunState(state) {
		return RunRecord{}, invalidRequest("run completion requires a terminal state", scope.Scope)
	}
	if err := validateRunScope(scope); err != nil {
		return RunRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := activeRunKey(scope.Scope)
	record := s.activeRuns[key]
	if record == nil || record.RunID != scope.RunID {
		return RunRecord{}, unknownActiveRun(scope)
	}
	record.State = state
	record.UpdatedAtUnixMS = s.now().UnixMilli()
	snapshot := *record
	delete(s.activeRuns, key)
	s.closeSubscribersLocked(replayKey(scope))
	return snapshot, nil
}

func (s *Store) ActiveRun(scope Scope) (RunRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.activeRuns[activeRunKey(scope)]
	if record == nil || isTerminalRunState(record.State) {
		return RunRecord{}, false
	}
	return *record, true
}

func (s *Store) AppendEvent(input EventInput) (EventRecord, error) {
	if err := validateRunScope(input.RunScope); err != nil {
		return EventRecord{}, err
	}
	if input.SizeBytes < 0 {
		return EventRecord{}, invalidRequest("event size must not be negative", input.Scope)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	s.nextEventID++
	record := EventRecord{
		EventID:                   s.nextEventID,
		Epoch:                     s.epoch,
		RunScope:                  input.RunScope,
		Kind:                      safeLabel(input.Kind),
		State:                     safeLabel(input.State),
		Reason:                    input.Reason,
		AssistantDelta:            cloneAssistantDeltaEvent(input.AssistantDelta),
		AssistantMessageCompleted: cloneAssistantMessageCompletedEvent(input.AssistantMessageCompleted),
		PendingCreated:            cloneChatPendingRequest(input.PendingCreated),
		PendingResolved:           cloneChatPendingResolved(input.PendingResolved),
		Terminal:                  cloneChatTerminal(input.Terminal),
		CreatedAtUnixMS:           s.now().UnixMilli(),
		SizeBytes:                 input.SizeBytes,
	}
	key := replayKey(input.RunScope)
	buffer := s.replay[key]
	if buffer == nil {
		buffer = &replayBuffer{}
		s.replay[key] = buffer
	}
	buffer.events = append(buffer.events, record)
	buffer.bytes += record.SizeBytes
	s.enforceReplayLimitsLocked(buffer)
	s.publishEventLocked(replayKey(input.RunScope), record)
	return record, nil
}

func (s *Store) Subscribe(scope RunScope) (<-chan EventRecord, func(), error) {
	if err := validateRunScope(scope); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextSubscriberID++
	id := s.nextSubscriberID
	key := replayKey(scope)
	active := s.activeRuns[activeRunKey(scope.Scope)]
	if active == nil || active.RunID != scope.RunID || isTerminalRunState(active.State) {
		return nil, func() {}, nil
	}
	subscribers := s.subscribers[key]
	if subscribers == nil {
		subscribers = map[uint64]chan EventRecord{}
		s.subscribers[key] = subscribers
	}
	queue := s.limits.SubscriberQueue
	if queue <= 0 {
		queue = DefaultSubscriberQueue
	}
	ch := make(chan EventRecord, queue)
	subscribers[id] = ch
	unsubscribe := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		subscribers := s.subscribers[key]
		if subscribers == nil {
			return
		}
		if existing := subscribers[id]; existing != nil {
			delete(subscribers, id)
			close(existing)
		}
		if len(subscribers) == 0 {
			delete(s.subscribers, key)
		}
	}
	return ch, unsubscribe, nil
}

func (s *Store) Replay(cursor Cursor) (ReplayResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	if cursor.Epoch != s.epoch {
		return ReplayResult{}, replayUnavailableAfterRestart(cursor)
	}
	key := replayKey(RunScope{
		Scope: Scope{
			SessionGroupID: cursor.SessionGroupID,
			WorkspaceID:    cursor.WorkspaceID,
			ChatID:         cursor.ChatID,
		},
		RunID: cursor.RunID,
	})
	buffer := s.replay[key]
	if buffer == nil || len(buffer.events) == 0 {
		return ReplayResult{}, replayUnavailableAfterRestart(cursor)
	}
	s.enforceReplayLimitsLocked(buffer)
	if len(buffer.events) == 0 {
		delete(s.replay, key)
		return ReplayResult{}, replayUnavailableAfterRestart(cursor)
	}
	result := ReplayResult{
		OldestBufferedEventID:     buffer.events[0].EventID,
		NewestBufferedEventID:     buffer.events[len(buffer.events)-1].EventID,
		FromStartAvailable:        buffer.startEvictedBeforeEventID == 0,
		StartEvictedBeforeEventID: buffer.startEvictedBeforeEventID,
	}
	if cursor.AfterEventID < result.OldestBufferedEventID-1 && !(cursor.AfterEventID == 0 && !result.FromStartAvailable) {
		return ReplayResult{}, replayCursorEvicted(cursor)
	}
	if cursor.AfterEventID > result.NewestBufferedEventID {
		return ReplayResult{}, replayOutOfRange(cursor)
	}
	for _, event := range buffer.events {
		if event.EventID > cursor.AfterEventID {
			result.Events = append(result.Events, event)
		}
	}
	return result, nil
}

func (s *Store) RegisterPending(input PendingInput) (PendingRecord, error) {
	if err := validateRunScope(input.RunScope); err != nil {
		return PendingRecord{}, err
	}
	if input.PendingRequestID == "" {
		return PendingRecord{}, invalidRequest("pending request id is required", input.Scope)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	if s.activePendingCountLocked() >= s.limits.PendingCap {
		return PendingRecord{}, resourceExhausted("pending capacity exceeded", input.Scope)
	}
	ttl := input.TTL
	if ttl <= 0 {
		ttl = s.limits.PendingTTL
	}
	now := s.now()
	record := &PendingRecord{
		RunScope:         input.RunScope,
		PendingRequestID: input.PendingRequestID,
		Status:           PendingStatusActive,
		Reason:           input.Reason,
		CreatedAtUnixMS:  now.UnixMilli(),
		ExpiresAtUnixMS:  now.Add(ttl).UnixMilli(),
	}
	s.pending[pendingKey(input.RunScope, input.PendingRequestID)] = record
	return *record, nil
}

func (s *Store) RegisterPendingForActiveRun(input PendingInput) (PendingRecord, RunRecord, bool, error) {
	if err := validateRunScope(input.RunScope); err != nil {
		return PendingRecord{}, RunRecord{}, false, err
	}
	if input.PendingRequestID == "" {
		return PendingRecord{}, RunRecord{}, false, invalidRequest("pending request id is required", input.Scope)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	active := s.activeRuns[activeRunKey(input.Scope)]
	if active == nil || active.RunID != input.RunID || active.State == RunStateInterrupting || isTerminalRunState(active.State) {
		return PendingRecord{}, RunRecord{}, false, nil
	}
	if s.activePendingCountLocked() >= s.limits.PendingCap {
		return PendingRecord{}, RunRecord{}, true, resourceExhausted("pending capacity exceeded", input.Scope)
	}
	ttl := input.TTL
	if ttl <= 0 {
		ttl = s.limits.PendingTTL
	}
	now := s.now()
	record := &PendingRecord{
		RunScope:         input.RunScope,
		PendingRequestID: input.PendingRequestID,
		Status:           PendingStatusActive,
		Reason:           input.Reason,
		CreatedAtUnixMS:  now.UnixMilli(),
		ExpiresAtUnixMS:  now.Add(ttl).UnixMilli(),
	}
	s.pending[pendingKey(input.RunScope, input.PendingRequestID)] = record
	active.State = RunStatePending
	active.UpdatedAtUnixMS = now.UnixMilli()
	return *record, *active, true, nil
}

func (s *Store) ClaimPendingResolution(scope RunScope, pendingRequestID string) (PendingRecord, error) {
	if err := validateRunScope(scope); err != nil {
		return PendingRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	record := s.pending[pendingKey(scope, pendingRequestID)]
	if record == nil {
		return PendingRecord{}, pendingUnavailable(scope, pendingRequestID)
	}
	now := s.now()
	if now.UnixMilli() > record.ExpiresAtUnixMS {
		record.Status = PendingStatusExpired
		return *record, pendingUnavailable(scope, pendingRequestID)
	}
	if record.Status != PendingStatusActive {
		return *record, pendingAlreadyResolved(scope, pendingRequestID)
	}
	record.Status = PendingStatusClaimed
	return *record, nil
}

func (s *Store) ReleasePendingResolutionClaim(scope RunScope, pendingRequestID string) error {
	if err := validateRunScope(scope); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	record := s.pending[pendingKey(scope, pendingRequestID)]
	if record == nil {
		return pendingUnavailable(scope, pendingRequestID)
	}
	if record.Status != PendingStatusClaimed {
		return pendingAlreadyResolved(scope, pendingRequestID)
	}
	if s.now().UnixMilli() > record.ExpiresAtUnixMS {
		record.Status = PendingStatusExpired
		return pendingUnavailable(scope, pendingRequestID)
	}
	record.Status = PendingStatusActive
	return nil
}

func (s *Store) Pending(scope RunScope, pendingRequestID string) (PendingRecord, bool) {
	if err := validateRunScope(scope); err != nil {
		return PendingRecord{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	record := s.pending[pendingKey(scope, pendingRequestID)]
	if record == nil {
		return PendingRecord{}, false
	}
	if record.Status != PendingStatusActive && record.Status != PendingStatusClaimed {
		return PendingRecord{}, false
	}
	return *record, true
}

func (s *Store) ResolvePending(scope RunScope, pendingRequestID string) (PendingRecord, error) {
	if err := validateRunScope(scope); err != nil {
		return PendingRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	record := s.pending[pendingKey(scope, pendingRequestID)]
	if record == nil {
		return PendingRecord{}, pendingUnavailable(scope, pendingRequestID)
	}
	now := s.now()
	if now.UnixMilli() > record.ExpiresAtUnixMS {
		record.Status = PendingStatusExpired
		return *record, pendingUnavailable(scope, pendingRequestID)
	}
	if record.Status != PendingStatusActive && record.Status != PendingStatusClaimed {
		return *record, pendingAlreadyResolved(scope, pendingRequestID)
	}
	record.Status = PendingStatusResolved
	record.ResolvedAtUnixMS = now.UnixMilli()
	return *record, nil
}

func (s *Store) ReserveIdempotency(scope IdempotencyScope) (IdempotencyEntry, bool, error) {
	if err := validateIdempotencyScope(scope); err != nil {
		return IdempotencyEntry{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	storeKey := idempotencyKey(scope)
	if existing := s.idempotency[storeKey]; existing != nil {
		if existing.Scope != scope {
			return IdempotencyEntry{}, true, idempotencyScopeMismatch(scope)
		}
		return *existing, true, nil
	}
	if len(s.idempotency) >= s.limits.IdempotencyCap {
		return IdempotencyEntry{}, false, resourceExhausted("idempotency capacity exceeded", Scope{SessionGroupID: scope.SessionGroupID, WorkspaceID: scope.WorkspaceID, ChatID: scope.ChatID})
	}
	now := s.now().UnixMilli()
	entry := &IdempotencyEntry{
		Scope:           scope,
		State:           IdempotencyStateInFlight,
		CreatedAtUnixMS: now,
		UpdatedAtUnixMS: now,
	}
	s.idempotency[storeKey] = entry
	return *entry, false, nil
}

func (s *Store) CompleteIdempotency(scope IdempotencyScope, result IdempotencyResultRef) (IdempotencyEntry, error) {
	if err := validateIdempotencyScope(scope); err != nil {
		return IdempotencyEntry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	entry := s.idempotency[idempotencyKey(scope)]
	if entry == nil || entry.Scope != scope {
		return IdempotencyEntry{}, idempotencyUnavailable(scope)
	}
	entry.State = IdempotencyStateSucceeded
	entry.Result = result
	entry.UpdatedAtUnixMS = s.now().UnixMilli()
	return *entry, nil
}

func (s *Store) ReleaseIdempotency(scope IdempotencyScope) error {
	if err := validateIdempotencyScope(scope); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	storeKey := idempotencyKey(scope)
	entry := s.idempotency[storeKey]
	if entry == nil || entry.Scope != scope {
		return idempotencyUnavailable(scope)
	}
	if entry.State != IdempotencyStateInFlight {
		return idempotencyUnavailable(scope)
	}
	delete(s.idempotency, storeKey)
	return nil
}

func (s *Store) Idempotency(scope IdempotencyScope) (IdempotencyEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.idempotency[idempotencyKey(scope)]
	if entry == nil || entry.Scope != scope {
		return IdempotencyEntry{}, false
	}
	return *entry, true
}

func (s *Store) RecordDiagnostic(record DiagnosticRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.CreatedAtUnixMS == 0 {
		record.CreatedAtUnixMS = s.now().UnixMilli()
	}
	record.SessionGroupID = safeLabel(record.SessionGroupID)
	record.WorkspaceID = safeLabel(record.WorkspaceID)
	record.ChatID = safeLabel(record.ChatID)
	record.RunID = safeLabel(record.RunID)
	record.RequestID = safeLabel(record.RequestID)
	record.State = safeLabel(record.State)
	if len(s.diagnostics) >= s.limits.DiagnosticsCap {
		copy(s.diagnostics, s.diagnostics[1:])
		s.diagnostics[len(s.diagnostics)-1] = record
		return
	}
	s.diagnostics = append(s.diagnostics, record)
}

func (s *Store) Diagnostics() []DiagnosticRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]DiagnosticRecord(nil), s.diagnostics...)
}

func normalizeLimits(limits Limits) Limits {
	if limits.ReplayMaxEvents <= 0 {
		limits.ReplayMaxEvents = DefaultReplayMaxEvents
	}
	if limits.ReplayMaxBytes <= 0 {
		limits.ReplayMaxBytes = DefaultReplayMaxBytes
	}
	if limits.ReplayTTL <= 0 {
		limits.ReplayTTL = DefaultReplayTTL
	}
	if limits.PendingTTL <= 0 {
		limits.PendingTTL = DefaultPendingTTL
	}
	if limits.DiagnosticsCap <= 0 {
		limits.DiagnosticsCap = DefaultDiagnosticsCap
	}
	if limits.ActiveRunsCap <= 0 {
		limits.ActiveRunsCap = DefaultActiveRunsCap
	}
	if limits.PendingCap <= 0 {
		limits.PendingCap = DefaultPendingCap
	}
	if limits.IdempotencyCap <= 0 {
		limits.IdempotencyCap = DefaultIdempotencyCap
	}
	if limits.SubscriberQueue <= 0 {
		limits.SubscriberQueue = DefaultSubscriberQueue
	}
	return limits
}

func (s *Store) cleanupLocked() {
	now := s.now()
	for key, record := range s.pending {
		if record == nil {
			delete(s.pending, key)
			continue
		}
		if (record.Status == PendingStatusActive || record.Status == PendingStatusClaimed) && now.UnixMilli() > record.ExpiresAtUnixMS {
			record.Status = PendingStatusExpired
		}
		retainUntil := time.UnixMilli(record.ExpiresAtUnixMS)
		if record.ResolvedAtUnixMS > 0 {
			retainUntil = time.UnixMilli(record.ResolvedAtUnixMS).Add(s.limits.PendingTTL)
		}
		if record.Status != PendingStatusActive && record.Status != PendingStatusClaimed && now.After(retainUntil) {
			delete(s.pending, key)
		}
	}
	for key, entry := range s.idempotency {
		if entry == nil {
			delete(s.idempotency, key)
			continue
		}
		if entry.State != IdempotencyStateInFlight && now.After(time.UnixMilli(entry.UpdatedAtUnixMS).Add(s.limits.ReplayTTL)) {
			delete(s.idempotency, key)
		}
	}
}

func (s *Store) enforceReplayLimitsLocked(buffer *replayBuffer) {
	now := s.now()
	for len(buffer.events) > 0 {
		oldest := buffer.events[0]
		tooManyEvents := len(buffer.events) > s.limits.ReplayMaxEvents
		tooManyBytes := buffer.bytes > s.limits.ReplayMaxBytes
		expired := now.Sub(time.UnixMilli(oldest.CreatedAtUnixMS)) > s.limits.ReplayTTL
		if !tooManyEvents && !tooManyBytes && !expired {
			return
		}
		buffer.startEvictedBeforeEventID = oldest.EventID + 1
		buffer.bytes -= oldest.SizeBytes
		buffer.events = buffer.events[1:]
	}
}

func (s *Store) activePendingCountLocked() int {
	count := 0
	for _, record := range s.pending {
		if record != nil && (record.Status == PendingStatusActive || record.Status == PendingStatusClaimed) {
			count++
		}
	}
	return count
}

func (s *Store) activeRunCapacityUsedLocked() int {
	return len(s.activeRuns) + len(s.activeRunReservations)
}

func validateRunScope(scope RunScope) error {
	if scope.SessionGroupID == "" || scope.WorkspaceID == "" || scope.ChatID == "" || scope.RunID == "" {
		return invalidRequest("session group, workspace, chat, and run ids are required", scope.Scope)
	}
	return nil
}

func validateIdempotencyScope(scope IdempotencyScope) error {
	if scope.Operation == "" || scope.SessionGroupID == "" || scope.WorkspaceID == "" || scope.Key == "" {
		return invalidRequest("operation, session group, workspace, and idempotency key are required", Scope{SessionGroupID: scope.SessionGroupID, WorkspaceID: scope.WorkspaceID, ChatID: scope.ChatID})
	}
	return nil
}

func isTerminalRunState(state RunState) bool {
	switch state {
	case RunStateCompleted, RunStateFailed, RunStateInterrupted:
		return true
	default:
		return false
	}
}

func (s *Store) publishEventLocked(key string, record EventRecord) {
	for id, subscriber := range s.subscribers[key] {
		select {
		case subscriber <- record:
		default:
			delete(s.subscribers[key], id)
			close(subscriber)
		}
	}
	if len(s.subscribers[key]) == 0 {
		delete(s.subscribers, key)
	}
}

func (s *Store) closeSubscribersLocked(key string) {
	for id, subscriber := range s.subscribers[key] {
		delete(s.subscribers[key], id)
		close(subscriber)
	}
	delete(s.subscribers, key)
}

func activeRunKey(scope Scope) string {
	return scope.SessionGroupID + "\x00" + scope.WorkspaceID + "\x00" + scope.ChatID
}

func replayKey(scope RunScope) string {
	return activeRunKey(scope.Scope) + "\x00" + scope.RunID
}

func idempotencyKey(scope IdempotencyScope) string {
	return scope.SessionGroupID + "\x00" + scope.WorkspaceID + "\x00" + scope.ChatID + "\x00" + scope.Key
}

func activeRunReservationKey(scope IdempotencyScope) string {
	return string(scope.Operation) + "\x00" +
		scope.SessionGroupID + "\x00" +
		scope.WorkspaceID + "\x00" +
		scope.ChatID + "\x00" +
		scope.PendingRequestID + "\x00" +
		scope.ClientMessageID + "\x00" +
		scope.Key
}

func pendingKey(scope RunScope, pendingRequestID string) string {
	return replayKey(scope) + "\x00" + pendingRequestID
}

func safeLabel(value string) string {
	if len(value) <= domain.MaxPublicIDBytes {
		return value
	}
	return value[:domain.MaxPublicIDBytes]
}

func cloneChatPendingRequest(request *domain.ChatPendingRequest) *domain.ChatPendingRequest {
	if request == nil {
		return nil
	}
	cloned := *request
	return &cloned
}

func cloneAssistantDeltaEvent(event *domain.AssistantDeltaEvent) *domain.AssistantDeltaEvent {
	if event == nil {
		return nil
	}
	cloned := *event
	return &cloned
}

func cloneAssistantMessageCompletedEvent(event *domain.AssistantMessageCompletedEvent) *domain.AssistantMessageCompletedEvent {
	if event == nil {
		return nil
	}
	cloned := *event
	return &cloned
}

func cloneChatPendingResolved(resolved *domain.ChatPendingResolved) *domain.ChatPendingResolved {
	if resolved == nil {
		return nil
	}
	cloned := *resolved
	return &cloned
}

func cloneChatTerminal(terminal *domain.ChatTerminal) *domain.ChatTerminal {
	if terminal == nil {
		return nil
	}
	cloned := *terminal
	return &cloned
}

func invalidRequest(message string, scope Scope) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeInvalidArgument,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonInvalidRequest,
			DisplayMessage: message,
			SessionGroupID: scope.SessionGroupID,
			ThreadID:       scope.ChatID,
		},
	}
}

func resourceExhausted(message string, scope Scope) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeResourceExhausted,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonResourceExhausted,
			DisplayMessage: message,
			SessionGroupID: scope.SessionGroupID,
			ThreadID:       scope.ChatID,
		},
	}
}

func alreadyRunning(scope RunScope) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeFailedPrecondition,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonAlreadyRunning,
			DisplayMessage: "chat already has active run",
			SessionGroupID: scope.SessionGroupID,
			ThreadID:       scope.ChatID,
		},
	}
}

func unknownActiveRun(scope RunScope) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeFailedPrecondition,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonInvalidRequest,
			DisplayMessage: "active run correlation is unavailable",
			SessionGroupID: scope.SessionGroupID,
			ThreadID:       scope.ChatID,
			Retryable:      true,
		},
	}
}

func replayUnavailableAfterRestart(cursor Cursor) error {
	return &ReplayError{
		Kind:    ReplayFailureUnavailableAfterRestart,
		Cursor:  cursor,
		Message: "replay is unavailable in this gateway process",
	}
}

func replayCursorEvicted(cursor Cursor) error {
	return &ReplayError{
		Kind:    ReplayFailureCursorEvicted,
		Cursor:  cursor,
		Message: "the requested chat event cursor is no longer buffered",
	}
}

func replayOutOfRange(cursor Cursor) error {
	return &ReplayError{
		Kind:    ReplayFailureOutOfRange,
		Cursor:  cursor,
		Message: "the requested chat event cursor is outside the buffered range",
	}
}

func pendingUnavailable(scope RunScope, pendingRequestID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeUnavailable,
		Details: domain.GatewayErrorDetails{
			Reason:           domain.ReasonPendingUnavailableAfterRestart,
			DisplayMessage:   "pending correlation is unavailable in this gateway process",
			SessionGroupID:   scope.SessionGroupID,
			ThreadID:         scope.ChatID,
			PendingRequestID: pendingRequestID,
			Retryable:        true,
		},
	}
}

func pendingAlreadyResolved(scope RunScope, pendingRequestID string) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeFailedPrecondition,
		Details: domain.GatewayErrorDetails{
			Reason:           domain.ReasonPendingRequestAlreadyResolved,
			DisplayMessage:   "pending request is not active",
			SessionGroupID:   scope.SessionGroupID,
			ThreadID:         scope.ChatID,
			PendingRequestID: pendingRequestID,
		},
	}
}

func idempotencyScopeMismatch(scope IdempotencyScope) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeAborted,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonIdempotencyScopeMismatch,
			DisplayMessage: "idempotency key was reused with a different safe scope",
			SessionGroupID: scope.SessionGroupID,
			ThreadID:       scope.ChatID,
		},
	}
}

func idempotencyUnavailable(scope IdempotencyScope) error {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeUnavailable,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonIdempotencyUnavailableAfterRestart,
			DisplayMessage: "idempotency record is unavailable in this gateway process",
			SessionGroupID: scope.SessionGroupID,
			ThreadID:       scope.ChatID,
			Retryable:      true,
		},
	}
}
