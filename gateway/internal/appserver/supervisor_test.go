package appserver

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

func TestSupervisorAppliesRestartBackoffAfterThreeFailures(t *testing.T) {
	now := time.Unix(1000, 0)
	supervisor := NewSupervisor("sg-1", func(context.Context) (*Connection, error) {
		return nil, errors.New("spawn failed")
	})
	supervisor.SetClock(func() time.Time { return now })

	for i := 0; i < 3; i++ {
		if _, err := supervisor.Connection(context.Background()); err == nil {
			t.Fatal("Connection() succeeded, want failure")
		}
	}
	_, err := supervisor.Connection(context.Background())
	var gatewayErr *domain.GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Details.Reason != domain.ReasonAppServerRestartBackoff {
		t.Fatalf("Connection() error = %#v, want app_server_restart_backoff", err)
	}
}

func TestSupervisorReportsBackoffStatus(t *testing.T) {
	now := time.Unix(2000, 0)
	supervisor := NewSupervisor("sg-1", func(context.Context) (*Connection, error) {
		return nil, errors.New("spawn failed")
	})
	supervisor.SetClock(func() time.Time { return now })

	for i := 0; i < restartFailureThreshold; i++ {
		if _, err := supervisor.Connection(context.Background()); err == nil {
			t.Fatal("Connection() succeeded, want failure")
		}
	}

	status := supervisor.Status()
	if status.SessionGroupID != "sg-1" {
		t.Fatalf("Status().SessionGroupID = %q, want sg-1", status.SessionGroupID)
	}
	if status.State != SupervisorStateBackoff {
		t.Fatalf("Status().State = %q, want %q", status.State, SupervisorStateBackoff)
	}
	if status.Reason != domain.ReasonAppServerRestartBackoff {
		t.Fatalf("Status().Reason = %q, want %q", status.Reason, domain.ReasonAppServerRestartBackoff)
	}
	if !status.Retryable {
		t.Fatal("Status().Retryable = false, want true")
	}
	if status.Failures != restartFailureThreshold {
		t.Fatalf("Status().Failures = %d, want %d", status.Failures, restartFailureThreshold)
	}
	if want := now.Add(restartCooldown); !status.CooldownUntil.Equal(want) {
		t.Fatalf("Status().CooldownUntil = %v, want %v", status.CooldownUntil, want)
	}
}

func TestSupervisorAppliesStartupTimeoutWithoutCallerDeadline(t *testing.T) {
	started := make(chan struct{})
	supervisor := NewSupervisorWithOptions("sg-1", func(ctx context.Context) (*Connection, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}, SupervisorOptions{StartupTimeout: 10 * time.Millisecond})

	result := make(chan error, 1)
	go func() {
		_, err := supervisor.Connection(context.Background())
		result <- err
	}()

	<-started
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Connection() error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Connection() did not return after supervisor startup timeout")
	}
}

func TestSupervisorCallerDeadlineWinsOverStartupTimeout(t *testing.T) {
	startedAt := time.Now()
	callerDeadline := startedAt.Add(20 * time.Millisecond)
	recordedDeadline := make(chan time.Time, 1)
	supervisor := NewSupervisorWithOptions("sg-1", func(ctx context.Context) (*Connection, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return nil, errors.New("connect context has no deadline")
		}
		recordedDeadline <- deadline
		<-ctx.Done()
		return nil, ctx.Err()
	}, SupervisorOptions{StartupTimeout: time.Hour})

	ctx, cancel := context.WithDeadline(context.Background(), callerDeadline)
	defer cancel()
	_, err := supervisor.Connection(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Connection() error = %v, want context.DeadlineExceeded", err)
	}

	deadline := <-recordedDeadline
	if deadline.Before(callerDeadline.Add(-50*time.Millisecond)) || deadline.After(callerDeadline.Add(50*time.Millisecond)) {
		t.Fatalf("connect context deadline = %v, want near caller deadline %v", deadline, callerDeadline)
	}
}

func TestSupervisorStartupTimeoutWinsOverLongCallerDeadline(t *testing.T) {
	startedAt := time.Now()
	callerDeadline := startedAt.Add(time.Hour)
	recordedDeadline := make(chan time.Time, 1)
	supervisor := NewSupervisorWithOptions("sg-1", func(ctx context.Context) (*Connection, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return nil, errors.New("connect context has no deadline")
		}
		recordedDeadline <- deadline
		<-ctx.Done()
		return nil, ctx.Err()
	}, SupervisorOptions{StartupTimeout: 10 * time.Millisecond})

	ctx, cancel := context.WithDeadline(context.Background(), callerDeadline)
	defer cancel()
	_, err := supervisor.Connection(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Connection() error = %v, want context.DeadlineExceeded", err)
	}

	deadline := <-recordedDeadline
	if deadline.After(startedAt.Add(500 * time.Millisecond)) {
		t.Fatalf("connect context deadline = %v, want supervisor startup timeout before long caller deadline %v", deadline, callerDeadline)
	}
}

func TestSupervisorDoesNotBackoffCallerCancellation(t *testing.T) {
	tests := []struct {
		name       string
		contextErr error
		newContext func() (context.Context, context.CancelFunc)
	}{
		{
			name:       "canceled",
			contextErr: context.Canceled,
			newContext: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
		},
		{
			name:       "deadline exceeded",
			contextErr: context.DeadlineExceeded,
			newContext: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Unix(0, 0))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spawnErr := errors.New("spawn failed")
			var calls atomic.Int32
			supervisor := NewSupervisor("sg-1", func(ctx context.Context) (*Connection, error) {
				calls.Add(1)
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				return nil, spawnErr
			})

			for i := 0; i < restartFailureThreshold; i++ {
				ctx, cancel := tt.newContext()
				_, err := supervisor.Connection(ctx)
				cancel()
				if !errors.Is(err, tt.contextErr) {
					t.Fatalf("Connection() error = %v, want %v", err, tt.contextErr)
				}
			}

			_, err := supervisor.Connection(context.Background())
			if !errors.Is(err, spawnErr) {
				t.Fatalf("Connection() error = %v, want spawn failure without cooldown", err)
			}
			if calls.Load() != restartFailureThreshold+1 {
				t.Fatalf("connect calls = %d, want %d", calls.Load(), restartFailureThreshold+1)
			}
		})
	}
}

func TestSupervisorReportsConnectingAndClosedStatus(t *testing.T) {
	started := make(chan struct{})
	supervisor := NewSupervisor("sg-1", func(ctx context.Context) (*Connection, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	result := make(chan error, 1)
	go func() {
		_, err := supervisor.Connection(context.Background())
		result <- err
	}()
	<-started

	if status := supervisor.Status(); status.State != SupervisorStateConnecting {
		t.Fatalf("Status().State = %q, want %q", status.State, SupervisorStateConnecting)
	}
	if err := supervisor.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if status := supervisor.Status(); status.State != SupervisorStateClosed {
		t.Fatalf("Status().State = %q, want %q", status.State, SupervisorStateClosed)
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Connection() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Connection() did not unblock after Close()")
	}
}

func TestSupervisorSharesConcurrentColdStart(t *testing.T) {
	connection := &Connection{dispatcher: &Dispatcher{done: make(chan struct{})}}
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32

	supervisor := NewSupervisor("sg-1", func(context.Context) (*Connection, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return connection, nil
	})

	const workers = 8
	ready := make(chan struct{}, workers)
	results := make(chan *Connection, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			ready <- struct{}{}
			conn, err := supervisor.Connection(context.Background())
			results <- conn
			errs <- err
		}()
	}
	for i := 0; i < workers; i++ {
		<-ready
	}
	<-started
	close(release)

	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("Connection() error = %v", err)
		}
		if got := <-results; got != connection {
			t.Fatalf("Connection() = %#v, want shared connection %#v", got, connection)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("connect calls = %d, want 1", calls.Load())
	}
}

func TestSupervisorCloseCancelsInFlightConnection(t *testing.T) {
	started := make(chan struct{})
	supervisor := NewSupervisor("sg-1", func(ctx context.Context) (*Connection, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	result := make(chan error, 1)
	go func() {
		_, err := supervisor.Connection(context.Background())
		result <- err
	}()
	<-started

	if err := supervisor.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Connection() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Connection() did not unblock after Close()")
	}
}
