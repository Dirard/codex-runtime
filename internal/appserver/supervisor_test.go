package appserver

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dirard/codex-runtime/internal/domain"
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
