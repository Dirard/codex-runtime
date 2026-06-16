package chatstate

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

func TestStoreRestartClearsVolatileStateAndRejectsOldCorrelations(t *testing.T) {
	now, _ := testClock(time.Unix(1000, 0))
	store := NewStore(StoreOptions{Epoch: "epoch-1", Now: now})
	scope := testRunScope()
	idempotencyScope := IdempotencyScope{
		Operation:      OperationRunChatTurn,
		SessionGroupID: scope.SessionGroupID,
		WorkspaceID:    scope.WorkspaceID,
		ChatID:         scope.ChatID,
		Key:            "idem-1",
	}

	if _, _, err := store.ReserveIdempotency(idempotencyScope); err != nil {
		t.Fatalf("ReserveIdempotency() error = %v", err)
	}
	if _, err := store.StartRun(scope, "idem-1"); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := store.AppendEvent(EventInput{RunScope: scope, Kind: "status", State: "running", SizeBytes: 64}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if _, err := store.RegisterPending(PendingInput{RunScope: scope, PendingRequestID: "pending-1"}); err != nil {
		t.Fatalf("RegisterPending() error = %v", err)
	}
	store.RecordDiagnostic(DiagnosticRecord{SessionGroupID: scope.SessionGroupID, ChatID: scope.ChatID, RequestID: "request-1"})

	restarted := NewStore(StoreOptions{Epoch: "epoch-2", Now: now})
	if _, ok := restarted.ActiveRun(scope.Scope); ok {
		t.Fatal("ActiveRun() after restart = true, want false")
	}
	if _, ok := restarted.Idempotency(idempotencyScope); ok {
		t.Fatal("Idempotency() after restart = true, want false")
	}
	if diagnostics := restarted.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("Diagnostics() after restart len = %d, want 0", len(diagnostics))
	}
	_, err := restarted.Replay(Cursor{
		Epoch:          "epoch-1",
		SessionGroupID: scope.SessionGroupID,
		WorkspaceID:    scope.WorkspaceID,
		ChatID:         scope.ChatID,
		RunID:          scope.RunID,
	})
	assertReplayError(t, err, ReplayFailureUnavailableAfterRestart)
	_, err = restarted.ResolvePending(scope, "pending-1")
	assertGatewayError(t, err, domain.GatewayErrorCodeUnavailable, domain.ReasonPendingUnavailableAfterRestart)
	_, err = restarted.CompleteIdempotency(idempotencyScope, IdempotencyResultRef{ChatID: scope.ChatID, RunID: scope.RunID, Status: "completed"})
	assertGatewayError(t, err, domain.GatewayErrorCodeUnavailable, domain.ReasonIdempotencyUnavailableAfterRestart)
}

func TestStoreRejectsConcurrentActiveRunUntilTerminal(t *testing.T) {
	store := NewStore(StoreOptions{Epoch: "epoch-1"})
	scope := testRunScope()
	if _, err := store.StartRun(scope, "idem-1"); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	next := scope
	next.RunID = "run-2"
	_, err := store.StartRun(next, "idem-2")
	assertGatewayError(t, err, domain.GatewayErrorCodeFailedPrecondition, domain.ReasonAlreadyRunning)

	if _, err := store.CompleteRun(scope, RunStateCompleted); err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}
	if _, err := store.StartRun(next, "idem-2"); err != nil {
		t.Fatalf("StartRun() after terminal error = %v", err)
	}
}

func TestStoreActiveRunReservationConsumesCapacityUntilStartedOrReleased(t *testing.T) {
	store := NewStore(StoreOptions{
		Epoch: "epoch-1",
		Limits: Limits{
			ActiveRunsCap: 1,
		},
	})
	reservation := IdempotencyScope{
		Operation:       OperationStartChatRun,
		SessionGroupID:  "sg-1",
		WorkspaceID:     "ws-1",
		ClientMessageID: "client-message-1",
		Key:             "idem-1",
	}
	if err := store.ReserveActiveRunCapacity(reservation); err != nil {
		t.Fatalf("ReserveActiveRunCapacity() error = %v", err)
	}
	scope := testRunScope()
	_, err := store.StartRun(scope, "idem-2")
	assertGatewayError(t, err, domain.GatewayErrorCodeResourceExhausted, domain.ReasonResourceExhausted)
	if err := store.ReleaseActiveRunCapacity(reservation); err != nil {
		t.Fatalf("ReleaseActiveRunCapacity() error = %v", err)
	}
	if _, err := store.StartRun(scope, "idem-2"); err != nil {
		t.Fatalf("StartRun() after reservation release error = %v", err)
	}
	if _, err := store.CompleteRun(scope, RunStateCompleted); err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}

	if err := store.ReserveActiveRunCapacity(reservation); err != nil {
		t.Fatalf("ReserveActiveRunCapacity() second error = %v", err)
	}
	if _, err := store.StartRunWithReservation(reservation, scope, "idem-1"); err != nil {
		t.Fatalf("StartRunWithReservation() error = %v", err)
	}
	active, ok := store.ActiveRun(scope.Scope)
	if !ok || active.RunID != scope.RunID {
		t.Fatalf("ActiveRun() after StartRunWithReservation = (%#v, %t), want started run", active, ok)
	}
	otherReservation := reservation
	otherReservation.Key = "idem-3"
	err = store.ReserveActiveRunCapacity(otherReservation)
	assertGatewayError(t, err, domain.GatewayErrorCodeResourceExhausted, domain.ReasonResourceExhausted)
}

func TestReplayEnforcesEpochScopeAndBounds(t *testing.T) {
	now, advance := testClock(time.Unix(1000, 0))
	store := NewStore(StoreOptions{
		Epoch: "epoch-1",
		Now:   now,
		Limits: Limits{
			ReplayMaxEvents: 2,
			ReplayMaxBytes:  1024,
			ReplayTTL:       time.Hour,
		},
	})
	scope := testRunScope()
	for i := 0; i < 4; i++ {
		if _, err := store.AppendEvent(EventInput{RunScope: scope, Kind: "status", State: "running", SizeBytes: 16}); err != nil {
			t.Fatalf("AppendEvent(%d) error = %v", i, err)
		}
		advance(time.Second)
	}

	result, err := store.Replay(Cursor{
		Epoch:          "epoch-1",
		SessionGroupID: scope.SessionGroupID,
		WorkspaceID:    scope.WorkspaceID,
		ChatID:         scope.ChatID,
		RunID:          scope.RunID,
		AfterEventID:   0,
	})
	if err != nil {
		t.Fatalf("Replay(from start after eviction) error = %v", err)
	}
	if len(result.Events) != 2 || result.FromStartAvailable {
		t.Fatalf("Replay(from start after eviction) = %#v, want newest two events with start unavailable", result)
	}

	_, err = store.Replay(Cursor{
		Epoch:          "epoch-1",
		SessionGroupID: scope.SessionGroupID,
		WorkspaceID:    scope.WorkspaceID,
		ChatID:         scope.ChatID,
		RunID:          scope.RunID,
		AfterEventID:   1,
	})
	assertReplayError(t, err, ReplayFailureCursorEvicted)

	_, err = store.Replay(Cursor{
		Epoch:          "epoch-1",
		SessionGroupID: scope.SessionGroupID,
		WorkspaceID:    scope.WorkspaceID,
		ChatID:         scope.ChatID,
		RunID:          scope.RunID,
		AfterEventID:   999,
	})
	assertReplayError(t, err, ReplayFailureOutOfRange)

	result, err = store.Replay(Cursor{
		Epoch:          "epoch-1",
		SessionGroupID: scope.SessionGroupID,
		WorkspaceID:    scope.WorkspaceID,
		ChatID:         scope.ChatID,
		RunID:          scope.RunID,
		AfterEventID:   2,
	})
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("Replay() events len = %d, want 2", len(result.Events))
	}
	if result.FromStartAvailable {
		t.Fatal("Replay().FromStartAvailable = true, want false after eviction")
	}
	if result.OldestBufferedEventID != 3 || result.NewestBufferedEventID != 4 {
		t.Fatalf("Replay() bounds = %d..%d, want 3..4", result.OldestBufferedEventID, result.NewestBufferedEventID)
	}
}

func TestReplayExpiresOnReadWithoutAppend(t *testing.T) {
	now, advance := testClock(time.Unix(1000, 0))
	store := NewStore(StoreOptions{
		Epoch: "epoch-1",
		Now:   now,
		Limits: Limits{
			ReplayMaxEvents: 10,
			ReplayMaxBytes:  1024,
			ReplayTTL:       time.Second,
		},
	})
	scope := testRunScope()
	if _, err := store.AppendEvent(EventInput{RunScope: scope, Kind: "status", State: "running", SizeBytes: 16}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	advance(2 * time.Second)
	_, err := store.Replay(Cursor{
		Epoch:          "epoch-1",
		SessionGroupID: scope.SessionGroupID,
		WorkspaceID:    scope.WorkspaceID,
		ChatID:         scope.ChatID,
		RunID:          scope.RunID,
		AfterEventID:   0,
	})
	assertReplayError(t, err, ReplayFailureUnavailableAfterRestart)
}

func TestPendingExpiresAndRejectsDuplicateResolution(t *testing.T) {
	now, advance := testClock(time.Unix(1000, 0))
	store := NewStore(StoreOptions{Epoch: "epoch-1", Now: now})
	scope := testRunScope()

	if _, err := store.RegisterPending(PendingInput{RunScope: scope, PendingRequestID: "pending-1", TTL: time.Minute}); err != nil {
		t.Fatalf("RegisterPending() error = %v", err)
	}
	if _, err := store.ResolvePending(scope, "pending-1"); err != nil {
		t.Fatalf("ResolvePending() error = %v", err)
	}
	_, err := store.ResolvePending(scope, "pending-1")
	assertGatewayError(t, err, domain.GatewayErrorCodeFailedPrecondition, domain.ReasonPendingRequestAlreadyResolved)

	if _, err := store.RegisterPending(PendingInput{RunScope: scope, PendingRequestID: "pending-2", TTL: time.Second}); err != nil {
		t.Fatalf("RegisterPending() error = %v", err)
	}
	advance(2 * time.Second)
	_, err = store.ResolvePending(scope, "pending-2")
	assertGatewayError(t, err, domain.GatewayErrorCodeUnavailable, domain.ReasonPendingUnavailableAfterRestart)
}

func TestRegisterPendingForActiveRunPreservesInterruptingState(t *testing.T) {
	store := NewStore(StoreOptions{Epoch: "epoch-1"})
	scope := testRunScope()
	if _, err := store.StartRun(scope, "idem-1"); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := store.UpdateRunState(scope, RunStateRunning); err != nil {
		t.Fatalf("UpdateRunState(running) error = %v", err)
	}
	if _, _, claimed, err := store.RegisterPendingForActiveRun(PendingInput{RunScope: scope, PendingRequestID: "pending-1"}); err != nil || !claimed {
		t.Fatalf("RegisterPendingForActiveRun(running) = claimed %t error %v, want claimed nil error", claimed, err)
	}
	active, ok := store.ActiveRun(scope.Scope)
	if !ok || active.State != RunStatePending {
		t.Fatalf("ActiveRun() after pending register = (%#v, %t), want pending", active, ok)
	}
	if _, err := store.UpdateRunState(scope, RunStateInterrupting); err != nil {
		t.Fatalf("UpdateRunState(interrupting) error = %v", err)
	}
	if _, _, claimed, err := store.RegisterPendingForActiveRun(PendingInput{RunScope: scope, PendingRequestID: "pending-2"}); err != nil || claimed {
		t.Fatalf("RegisterPendingForActiveRun(interrupting) = claimed %t error %v, want not claimed nil error", claimed, err)
	}
	active, ok = store.ActiveRun(scope.Scope)
	if !ok || active.State != RunStateInterrupting {
		t.Fatalf("ActiveRun() after rejected pending register = (%#v, %t), want interrupting", active, ok)
	}
	if _, ok := store.Pending(scope, "pending-2"); ok {
		t.Fatal("Pending(pending-2) = true, want no pending registered after interrupting state wins")
	}
}

func TestStoreCapsAndCleanupBoundVolatileState(t *testing.T) {
	now, advance := testClock(time.Unix(1000, 0))
	store := NewStore(StoreOptions{
		Epoch: "epoch-1",
		Now:   now,
		Limits: Limits{
			ActiveRunsCap:  1,
			PendingCap:     1,
			IdempotencyCap: 1,
			PendingTTL:     time.Second,
			ReplayTTL:      time.Second,
		},
	})
	scope := testRunScope()
	if _, err := store.StartRun(scope, "idem-1"); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	other := scope
	other.ChatID = "chat-2"
	other.RunID = "run-2"
	_, err := store.StartRun(other, "idem-2")
	assertGatewayError(t, err, domain.GatewayErrorCodeResourceExhausted, domain.ReasonResourceExhausted)
	if _, err := store.CompleteRun(scope, RunStateCompleted); err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}
	if _, ok := store.ActiveRun(scope.Scope); ok {
		t.Fatal("ActiveRun() after terminal completion = true, want false")
	}
	if _, err := store.StartRun(other, "idem-2"); err != nil {
		t.Fatalf("StartRun() after terminal cleanup error = %v", err)
	}

	if _, err := store.RegisterPending(PendingInput{RunScope: other, PendingRequestID: "pending-1"}); err != nil {
		t.Fatalf("RegisterPending(pending-1) error = %v", err)
	}
	_, err = store.RegisterPending(PendingInput{RunScope: other, PendingRequestID: "pending-2"})
	assertGatewayError(t, err, domain.GatewayErrorCodeResourceExhausted, domain.ReasonResourceExhausted)
	if _, err := store.ResolvePending(other, "pending-1"); err != nil {
		t.Fatalf("ResolvePending(pending-1) error = %v", err)
	}
	advance(2 * time.Second)
	if _, err := store.RegisterPending(PendingInput{RunScope: other, PendingRequestID: "pending-2"}); err != nil {
		t.Fatalf("RegisterPending(pending-2) after cleanup error = %v", err)
	}

	idempotencyScope := IdempotencyScope{
		Operation:      OperationRunChatTurn,
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ChatID:         "chat-2",
		Key:            "idem-2",
	}
	if _, _, err := store.ReserveIdempotency(idempotencyScope); err != nil {
		t.Fatalf("ReserveIdempotency(idem-2) error = %v", err)
	}
	anotherKey := idempotencyScope
	anotherKey.Key = "idem-3"
	_, _, err = store.ReserveIdempotency(anotherKey)
	assertGatewayError(t, err, domain.GatewayErrorCodeResourceExhausted, domain.ReasonResourceExhausted)
	if _, err := store.CompleteIdempotency(idempotencyScope, IdempotencyResultRef{ChatID: "chat-2", RunID: "run-2", Status: "completed"}); err != nil {
		t.Fatalf("CompleteIdempotency(idem-2) error = %v", err)
	}
	advance(2 * time.Second)
	if _, _, err := store.ReserveIdempotency(anotherKey); err != nil {
		t.Fatalf("ReserveIdempotency(idem-3) after cleanup error = %v", err)
	}
}

func TestIdempotencyScopeStoresOnlySafeResultReferenceAndScopesRawKeys(t *testing.T) {
	store := NewStore(StoreOptions{Epoch: "epoch-1"})
	scope := IdempotencyScope{
		Operation:       OperationRunChatTurn,
		SessionGroupID:  "sg-1",
		WorkspaceID:     "ws-1",
		ChatID:          "chat-1",
		ClientMessageID: "client-message-1",
		Key:             "idem-1",
	}
	if entry, reused, err := store.ReserveIdempotency(scope); err != nil || reused || entry.State != IdempotencyStateInFlight {
		t.Fatalf("ReserveIdempotency() = (%#v, %t, %v), want in-flight fresh entry", entry, reused, err)
	}
	if _, err := store.CompleteIdempotency(scope, IdempotencyResultRef{ChatID: "chat-1", RunID: "run-1", Status: "completed"}); err != nil {
		t.Fatalf("CompleteIdempotency() error = %v", err)
	}
	entry, reused, err := store.ReserveIdempotency(scope)
	if err != nil || !reused || entry.Result.RunID != "run-1" {
		t.Fatalf("ReserveIdempotency() reuse = (%#v, %t, %v), want prior safe result ref", entry, reused, err)
	}

	conflict := scope
	conflict.ClientMessageID = "client-message-2"
	_, _, err = store.ReserveIdempotency(conflict)
	assertGatewayError(t, err, domain.GatewayErrorCodeAborted, domain.ReasonIdempotencyScopeMismatch)

	operationConflict := scope
	operationConflict.Operation = OperationInterruptChatRun
	_, _, err = store.ReserveIdempotency(operationConflict)
	assertGatewayError(t, err, domain.GatewayErrorCodeAborted, domain.ReasonIdempotencyScopeMismatch)

	independent := scope
	independent.ChatID = "chat-2"
	entry, reused, err = store.ReserveIdempotency(independent)
	if err != nil || reused || entry.Scope.ChatID != "chat-2" {
		t.Fatalf("ReserveIdempotency() same raw key in another chat = (%#v, %t, %v), want independent scoped entry", entry, reused, err)
	}
	assertNoForbiddenFields(t, IdempotencyEntry{}, "prompt", "response", "payload", "hash", "digest")
}

func TestSubscribeAfterRunCompletionDoesNotLeaveLiveStreamOpen(t *testing.T) {
	store := NewStore(StoreOptions{Epoch: "epoch-1"})
	scope := testRunScope()
	if _, err := store.StartRun(scope, "idem-1"); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := store.CompleteRun(scope, RunStateCompleted); err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}
	events, unsubscribe, err := store.Subscribe(scope)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if unsubscribe == nil {
		t.Fatal("Subscribe() unsubscribe = nil, want no-op unsubscribe")
	}
	defer unsubscribe()
	if events != nil {
		t.Fatalf("Subscribe() events = %#v, want nil for completed run", events)
	}
}

func TestReleaseIdempotencyAllowsRetryBeforeSideEffect(t *testing.T) {
	store := NewStore(StoreOptions{Epoch: "epoch-1"})
	scope := IdempotencyScope{
		Operation:      OperationStartChatRun,
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		Key:            "idem-1",
	}
	if _, duplicate, err := store.ReserveIdempotency(scope); err != nil || duplicate {
		t.Fatalf("ReserveIdempotency() = duplicate %t, error %v, want new reservation", duplicate, err)
	}
	if err := store.ReleaseIdempotency(scope); err != nil {
		t.Fatalf("ReleaseIdempotency() error = %v", err)
	}
	if _, duplicate, err := store.ReserveIdempotency(scope); err != nil || duplicate {
		t.Fatalf("ReserveIdempotency() after release = duplicate %t, error %v, want new reservation", duplicate, err)
	}
	if _, err := store.CompleteIdempotency(scope, IdempotencyResultRef{ChatID: "chat-1", RunID: "run-1", Status: "running"}); err != nil {
		t.Fatalf("CompleteIdempotency() error = %v", err)
	}
	err := store.ReleaseIdempotency(scope)
	assertGatewayError(t, err, domain.GatewayErrorCodeUnavailable, domain.ReasonIdempotencyUnavailableAfterRestart)
}

func TestDiagnosticsAreCappedAndSafeLabelBounded(t *testing.T) {
	store := NewStore(StoreOptions{
		Epoch: "epoch-1",
		Limits: Limits{
			DiagnosticsCap: 2,
		},
	})
	longChatID := strings.Repeat("c", domain.MaxPublicIDBytes+10)
	store.RecordDiagnostic(DiagnosticRecord{RequestID: "request-1", ChatID: longChatID})
	store.RecordDiagnostic(DiagnosticRecord{RequestID: "request-2", ChatID: "chat-2"})
	store.RecordDiagnostic(DiagnosticRecord{RequestID: "request-3", ChatID: "chat-3"})

	diagnostics := store.Diagnostics()
	if len(diagnostics) != 2 {
		t.Fatalf("Diagnostics() len = %d, want 2", len(diagnostics))
	}
	if diagnostics[0].RequestID != "request-2" || diagnostics[1].RequestID != "request-3" {
		t.Fatalf("Diagnostics() = %#v, want newest two records", diagnostics)
	}
	if len(diagnostics[0].ChatID) > domain.MaxPublicIDBytes {
		t.Fatalf("Diagnostic ChatID len = %d, want <= %d", len(diagnostics[0].ChatID), domain.MaxPublicIDBytes)
	}
	assertNoForbiddenFields(t, DiagnosticRecord{}, "prompt", "response", "payload", "jsonl", "token")
	assertNoForbiddenFields(t, EventRecord{}, "prompt", "response", "payload", "jsonl", "token")
}

func testRunScope() RunScope {
	return RunScope{
		Scope: Scope{
			SessionGroupID: "sg-1",
			WorkspaceID:    "ws-1",
			ChatID:         "chat-1",
		},
		RunID: "run-1",
	}
}

func testClock(start time.Time) (func() time.Time, func(time.Duration)) {
	current := start
	return func() time.Time {
			return current
		}, func(delta time.Duration) {
			current = current.Add(delta)
		}
}

func assertGatewayError(t *testing.T, err error, code domain.GatewayErrorCode, reason domain.GatewayErrorReason) {
	t.Helper()
	var gatewayErr *domain.GatewayError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("error = %#v, want GatewayError", err)
	}
	if gatewayErr.Code != code || gatewayErr.Details.Reason != reason {
		t.Fatalf("GatewayError = (%q, %q), want (%q, %q)", gatewayErr.Code, gatewayErr.Details.Reason, code, reason)
	}
}

func assertReplayError(t *testing.T, err error, kind ReplayFailureKind) {
	t.Helper()
	var replayErr *ReplayError
	if !errors.As(err, &replayErr) {
		t.Fatalf("error = %#v, want ReplayError", err)
	}
	if replayErr.Kind != kind {
		t.Fatalf("ReplayError kind = %q, want %q", replayErr.Kind, kind)
	}
}

func assertNoForbiddenFields(t *testing.T, value any, forbidden ...string) {
	t.Helper()
	typ := reflect.TypeOf(value)
	for i := 0; i < typ.NumField(); i++ {
		fieldName := strings.ToLower(typ.Field(i).Name)
		for _, word := range forbidden {
			if strings.Contains(fieldName, word) {
				t.Fatalf("%s has forbidden field %q", typ.Name(), typ.Field(i).Name)
			}
		}
	}
}
