package testappserver

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

const requireTimeout = 2 * time.Second

type inboundRecord struct {
	message Message
	err     error
}

// Harness simulates an app-server stdio JSONL peer.
type Harness struct {
	clientStdin  *io.PipeWriter
	serverStdin  *io.PipeReader
	clientStdout *io.PipeReader
	serverStdout *io.PipeWriter

	inbound chan inboundRecord
	done    chan struct{}

	closeOnce  sync.Once
	finishOnce sync.Once

	mu        sync.Mutex
	outbound  []Message
	scriptErr error
}

// New starts a fake app-server peer that executes script steps in order.
func New(t testing.TB, steps ...Step) *Harness {
	t.Helper()

	h := newHarness(steps...)
	t.Cleanup(func() {
		_ = h.Close()
		if err := h.Wait(); err != nil {
			t.Errorf("fake app-server script failed: %v", err)
		}
	})

	return h
}

func newHarness(steps ...Step) *Harness {
	serverStdin, clientStdin := io.Pipe()
	clientStdout, serverStdout := io.Pipe()
	h := &Harness{
		clientStdin:  clientStdin,
		serverStdin:  serverStdin,
		clientStdout: clientStdout,
		serverStdout: serverStdout,
		inbound:      make(chan inboundRecord, 32),
		done:         make(chan struct{}),
	}

	go h.readLoop()
	go h.runScript(steps)

	return h
}

// Stdin is the fake app-server stdin. Client code writes outbound JSONL here.
func (h *Harness) Stdin() io.WriteCloser {
	return h.clientStdin
}

// Stdout is the fake app-server stdout. Client code reads inbound JSONL here.
func (h *Harness) Stdout() io.ReadCloser {
	return h.clientStdout
}

// Close closes all in-memory pipes owned by the harness.
func (h *Harness) Close() error {
	var err error
	h.closeOnce.Do(func() {
		err = errors.Join(
			closePipe(h.clientStdin),
			closePipe(h.serverStdin),
			closePipe(h.clientStdout),
			closePipe(h.serverStdout),
		)
	})
	return err
}

// Wait waits for the script runner to finish.
func (h *Harness) Wait() error {
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.scriptErr
}

// RequireDone fails the test if the script has not completed successfully.
func (h *Harness) RequireDone(t testing.TB) {
	t.Helper()

	select {
	case <-h.done:
		if err := h.Wait(); err != nil {
			t.Fatalf("fake app-server script failed: %v", err)
		}
	case <-time.After(requireTimeout):
		t.Fatalf("fake app-server script did not finish within %s", requireTimeout)
	}
}

// OutboundMessages returns JSON-RPC payloads written by client code to stdin.
func (h *Harness) OutboundMessages() []Message {
	h.mu.Lock()
	defer h.mu.Unlock()

	messages := make([]Message, 0, len(h.outbound))
	for _, message := range h.outbound {
		messages = append(messages, message.Clone())
	}
	return messages
}

// RequireOutboundCount waits until count outbound JSON-RPC payloads are captured.
func (h *Harness) RequireOutboundCount(t testing.TB, count int) []Message {
	t.Helper()

	deadline := time.After(requireTimeout)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		messages := h.OutboundMessages()
		if len(messages) >= count {
			if len(messages) != count {
				t.Fatalf("captured outbound message count = %d, want %d", len(messages), count)
			}
			return messages
		}

		select {
		case <-deadline:
			t.Fatalf("captured outbound message count = %d, want %d", len(messages), count)
		case <-ticker.C:
		}
	}
}

// RequireOutboundRequest returns the captured request at index and checks method.
func (h *Harness) RequireOutboundRequest(t testing.TB, index int, method string) Message {
	t.Helper()

	message := h.requireOutboundAt(t, index)
	if err := requireRequestShape(message, method); err != nil {
		t.Fatalf("outbound[%d] = %#v: %v", index, message, err)
	}
	return message
}

// RequireOutboundNotification returns the captured notification at index and checks method.
func (h *Harness) RequireOutboundNotification(t testing.TB, index int, method string) Message {
	t.Helper()

	message := h.requireOutboundAt(t, index)
	if err := requireNotificationShape(message, method); err != nil {
		t.Fatalf("outbound[%d] = %#v: %v", index, message, err)
	}
	return message
}

// RequireOutboundResponse returns the captured success response at index and checks id.
func (h *Harness) RequireOutboundResponse(t testing.TB, index int, id any) Message {
	t.Helper()

	message := h.requireOutboundAt(t, index)
	if err := requireResponseShape(message, mustMarshalID(id)); err != nil {
		t.Fatalf("outbound[%d] = %#v: %v", index, message, err)
	}
	return message
}

func (h *Harness) requireOutboundAt(t testing.TB, index int) Message {
	t.Helper()

	if index < 0 {
		t.Fatalf("outbound index must be non-negative, got %d", index)
	}
	messages := h.requireOutboundAtLeast(t, index+1)
	return messages[index]
}

func (h *Harness) requireOutboundAtLeast(t testing.TB, count int) []Message {
	t.Helper()

	deadline := time.After(requireTimeout)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		messages := h.OutboundMessages()
		if len(messages) >= count {
			return messages
		}

		select {
		case <-deadline:
			t.Fatalf("captured outbound message count = %d, want at least %d", len(messages), count)
		case <-ticker.C:
		}
	}
}

func (h *Harness) readLoop() {
	reader := bufio.NewReader(h.serverStdin)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			message, parseErr := parseMessageLine(line)
			record := inboundRecord{message: message, err: parseErr}
			if parseErr == nil {
				h.mu.Lock()
				h.outbound = append(h.outbound, message.Clone())
				h.mu.Unlock()
			}
			h.queueInbound(record)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
				h.queueInbound(inboundRecord{err: err})
			}
			close(h.inbound)
			return
		}
	}
}

func (h *Harness) queueInbound(record inboundRecord) {
	select {
	case <-h.done:
		return
	default:
	}
	select {
	case h.inbound <- record:
	case <-h.done:
	}
}

func (h *Harness) runScript(steps []Step) {
	context := &scriptContext{
		harness: h,
		ids:     map[string]jsonID{},
	}

	for index, step := range steps {
		if err := step.run(context); err != nil {
			h.finish(fmt.Errorf("step %d %q: %w", index+1, step.name, err))
			return
		}
	}
	h.finish(nil)
}

func (h *Harness) finish(err error) {
	h.finishOnce.Do(func() {
		h.mu.Lock()
		h.scriptErr = err
		h.mu.Unlock()
		if err != nil {
			_ = h.serverStdout.CloseWithError(err)
		}
		close(h.done)
	})
}

func closePipe(closer io.Closer) error {
	err := closer.Close()
	if errors.Is(err, io.ErrClosedPipe) {
		return nil
	}
	return err
}
