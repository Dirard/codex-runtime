package appserver

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Dirard/codex-runtime/internal/domain"
)

const (
	restartFailureThreshold = 3
	restartCooldown         = 30 * time.Second
)

type Connector func(context.Context) (*Connection, error)

type Supervisor struct {
	sessionGroupID string
	connect        Connector
	now            func() time.Time

	mu            sync.Mutex
	active        *Connection
	connecting    *connectAttempt
	failures      int
	cooldownUntil time.Time
	closed        bool
}

type connectAttempt struct {
	done       chan struct{}
	connection *Connection
	err        error
	cancel     context.CancelFunc
}

func NewSupervisor(sessionGroupID string, connect Connector) *Supervisor {
	return &Supervisor{
		sessionGroupID: sessionGroupID,
		connect:        connect,
		now:            time.Now,
	}
}

func (s *Supervisor) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

func (s *Supervisor) Connection(ctx context.Context) (*Connection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return nil, context.Canceled
		}
		if s.active != nil {
			active := s.active
			select {
			case <-active.Done():
				s.active = nil
			default:
				s.mu.Unlock()
				return active, nil
			}
		}

		now := s.now()
		if !s.cooldownUntil.IsZero() && now.Before(s.cooldownUntil) {
			s.mu.Unlock()
			return nil, &domain.GatewayError{
				Code: domain.GatewayErrorCodeUnavailable,
				Details: domain.GatewayErrorDetails{
					Reason:         domain.ReasonAppServerRestartBackoff,
					DisplayMessage: "app-server restart cooldown is active",
					SessionGroupID: s.sessionGroupID,
					Retryable:      true,
				},
			}
		}

		if attempt := s.connecting; attempt != nil {
			done := attempt.done
			s.mu.Unlock()
			select {
			case <-done:
				if attempt.err != nil {
					return nil, attempt.err
				}
				return attempt.connection, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		connectCtx, cancel := context.WithCancel(ctx)
		attempt := &connectAttempt{done: make(chan struct{}), cancel: cancel}
		s.connecting = attempt
		s.mu.Unlock()

		connection, err := s.connect(connectCtx)
		cancel()

		s.mu.Lock()
		var connectionToClose *Connection
		if s.closed {
			if connection != nil {
				connectionToClose = connection
				connection = nil
			}
			if err == nil {
				err = context.Canceled
			}
		} else if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			s.failures++
			if s.failures >= restartFailureThreshold {
				s.cooldownUntil = s.now().Add(restartCooldown)
			}
		} else if err == nil {
			s.failures = 0
			s.cooldownUntil = time.Time{}
			s.active = connection
		}

		attempt.connection = connection
		attempt.err = err
		if connectionToClose != nil {
			s.mu.Unlock()
			_ = connectionToClose.Close()
			s.mu.Lock()
		}
		if s.connecting == attempt {
			s.connecting = nil
		}
		close(attempt.done)
		s.mu.Unlock()
		return connection, err
	}
}

func (s *Supervisor) MarkClosed(connection *Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == connection {
		s.active = nil
	}
}

func (s *Supervisor) Close() error {
	s.mu.Lock()
	s.closed = true
	active := s.active
	s.active = nil
	var cancel context.CancelFunc
	var connectingDone <-chan struct{}
	if s.connecting != nil {
		cancel = s.connecting.cancel
		connectingDone = s.connecting.done
	}
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	var closeErr error
	if active != nil {
		closeErr = active.Close()
	}
	if connectingDone != nil {
		<-connectingDone
	}
	return closeErr
}
