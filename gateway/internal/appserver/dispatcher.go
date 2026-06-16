package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	DefaultMaxJSONLLineBytes        = 16 * 1024 * 1024
	defaultNotificationWriteTimeout = 5 * time.Second
	defaultServerResponseTimeout    = 5 * time.Second
)

var (
	ErrDispatcherClosed  = errors.New("app-server dispatcher closed")
	ErrDuplicateResponse = errors.New("app-server request already answered")
	ErrProtocolMismatch  = errors.New("app-server protocol id mismatch")
)

type CallMetadata struct {
	TaskID           string
	SessionGroupID   string
	ExpectedThreadID string
	ExpectedTurnID   string
	CloseOnUncertain bool
}

type ServerRequest struct {
	ID     json.RawMessage
	Method string
	Params json.RawMessage
	TaskID string
}

type Notification struct {
	Method         string
	Params         json.RawMessage
	TaskID         string
	SessionGroupID string
	// Set only for serverRequest/resolved notifications after checking open server requests.
	ServerRequestResolvedChecked bool
	ServerRequestResolvedMatched bool
}

type Dispatcher struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser

	maxLineBytes int

	mu             sync.Mutex
	writeMu        sync.Mutex
	nextID         uint64
	pending        map[string]*pendingCall
	serverRequests map[string]*serverRequestState
	closed         bool
	closeErr       error

	requests      chan ServerRequest
	notifications chan Notification
	done          chan struct{}
	closeOnce     sync.Once
	outputsOnce   sync.Once
}

type pendingCall struct {
	id               uint64
	method           string
	taskID           string
	sessionGroupID   string
	expectedThreadID string
	expectedTurnID   string
	closeOnUncertain bool
	result           chan callResult
	provisionalID    string
}

type callResult struct {
	result json.RawMessage
	err    error
}

type serverRequestState struct {
	request   ServerRequest
	responded bool
}

func NewDispatcher(stdin io.WriteCloser, stdout io.ReadCloser, maxLineBytes int) *Dispatcher {
	if maxLineBytes <= 0 {
		maxLineBytes = DefaultMaxJSONLLineBytes
	}
	dispatcher := &Dispatcher{
		stdin:          stdin,
		stdout:         stdout,
		maxLineBytes:   maxLineBytes,
		nextID:         1,
		pending:        map[string]*pendingCall{},
		serverRequests: map[string]*serverRequestState{},
		requests:       make(chan ServerRequest, 32),
		notifications:  make(chan Notification, 64),
		done:           make(chan struct{}),
	}
	go dispatcher.readLoop()
	return dispatcher
}

func (d *Dispatcher) Call(ctx context.Context, method string, params any, timeout time.Duration, metadata CallMetadata) (json.RawMessage, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	call, message, err := d.newPendingCall(method, params, metadata)
	if err != nil {
		return nil, err
	}
	if err := d.writeMessageContext(ctx, message); err != nil {
		d.removePending(call.id)
		return nil, err
	}

	select {
	case result := <-call.result:
		return result.result, result.err
	case <-ctx.Done():
		if call.closeOnUncertain {
			d.closeWithError(ctx.Err())
			return nil, ctx.Err()
		}
		if d.removePending(call.id) {
			return nil, ctx.Err()
		}
		select {
		case result := <-call.result:
			return result.result, result.err
		default:
			return nil, ctx.Err()
		}
	case <-d.done:
		return nil, d.closeError()
	}
}

func (d *Dispatcher) Notify(method string, params any) error {
	message, err := newNotification(method, params)
	if err != nil {
		return err
	}
	ctx, cancel := withDefaultWriteTimeout(context.Background(), defaultNotificationWriteTimeout)
	defer cancel()
	return d.writeMessageContext(ctx, message)
}

func (d *Dispatcher) Respond(ctx context.Context, id json.RawMessage, result any) error {
	message, err := newSuccessResponse(id, result)
	if err != nil {
		return err
	}
	return d.respond(ctx, id, message)
}

func (d *Dispatcher) RespondError(ctx context.Context, id json.RawMessage, code int, message string) error {
	return d.respond(ctx, id, newErrorResponse(id, code, message))
}

func (d *Dispatcher) Requests() <-chan ServerRequest {
	return d.requests
}

func (d *Dispatcher) Notifications() <-chan Notification {
	return d.notifications
}

func (d *Dispatcher) Done() <-chan struct{} {
	return d.done
}

func (d *Dispatcher) Close() error {
	d.closeWithError(ErrDispatcherClosed)
	return nil
}

func (d *Dispatcher) newPendingCall(method string, params any, metadata CallMetadata) (*pendingCall, rpcMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil, rpcMessage{}, d.closeErr
	}
	id := d.nextID
	d.nextID++
	message, err := newRequest(id, method, params)
	if err != nil {
		return nil, rpcMessage{}, err
	}
	metadata = expectedCallMetadata(method, message.Params, metadata)
	call := &pendingCall{
		id:               id,
		method:           method,
		taskID:           metadata.TaskID,
		sessionGroupID:   metadata.SessionGroupID,
		expectedThreadID: metadata.ExpectedThreadID,
		expectedTurnID:   metadata.ExpectedTurnID,
		closeOnUncertain: metadata.CloseOnUncertain,
		result:           make(chan callResult, 1),
	}
	d.pending[idKey(message.ID)] = call
	return call, message, nil
}

func (d *Dispatcher) respond(ctx context.Context, id json.RawMessage, message rpcMessage) error {
	key := idKey(id)
	d.mu.Lock()
	state, ok := d.serverRequests[key]
	if !ok {
		d.mu.Unlock()
		return ErrDispatcherClosed
	}
	if state.responded {
		d.mu.Unlock()
		return ErrDuplicateResponse
	}
	state.responded = true
	d.mu.Unlock()

	writeCtx, cancel := withDefaultWriteTimeout(ctx, defaultServerResponseTimeout)
	defer cancel()
	if err := d.writeMessageContext(writeCtx, message); err != nil {
		return err
	}
	d.completeServerRequest(key)
	return nil
}

func (d *Dispatcher) writeMessage(message rpcMessage) error {
	return d.writeMessageContext(context.Background(), message)
}

func withDefaultWriteTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok || timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func (d *Dispatcher) writeMessageContext(ctx context.Context, message rpcMessage) error {
	encoded, err := encodeRPCJSONL(message)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		d.closeWithError(ctx.Err())
		return ctx.Err()
	default:
	}

	errCh := make(chan error, 1)
	go func() {
		d.writeMu.Lock()
		defer d.writeMu.Unlock()

		d.mu.Lock()
		closed := d.closed
		closeErr := d.closeErr
		d.mu.Unlock()
		if closed {
			errCh <- closeErr
			return
		}
		_, err := d.stdin.Write(encoded)
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err != nil {
			d.closeWithError(err)
		}
		return err
	case <-ctx.Done():
		d.closeWithError(ctx.Err())
		return ctx.Err()
	case <-d.done:
		return d.closeError()
	}
}

func (d *Dispatcher) readLoop() {
	defer d.closeOutputs()

	reader := bufio.NewReader(d.stdout)
	for {
		line, err := readJSONLLine(reader, d.maxLineBytes)
		if len(line) > 0 {
			message, parseErr := parseRPCLine(line)
			if parseErr != nil {
				d.closeWithError(parseErr)
				return
			}
			if err := d.handleMessage(message); err != nil {
				d.closeWithError(err)
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				d.closeWithError(ErrDispatcherClosed)
			} else {
				d.closeWithError(err)
			}
			return
		}
	}
}

func readJSONLLine(reader *bufio.Reader, maxLineBytes int) ([]byte, error) {
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		line = append(line, fragment...)
		if len(line) > maxLineBytes {
			return nil, fmt.Errorf("app-server JSONL line exceeds limit")
		}
		if err == nil {
			return line, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if len(line) > 0 && errors.Is(err, io.EOF) {
			return line, nil
		}
		return line, err
	}
}

func (d *Dispatcher) handleMessage(message rpcMessage) error {
	switch {
	case message.Method != "" && len(message.ID) != 0:
		return d.handleServerRequest(message)
	case message.Method != "":
		return d.handleNotification(message)
	case len(message.ID) != 0:
		return d.handleResponse(message)
	default:
		return fmt.Errorf("invalid JSON-RPC message shape")
	}
}

func (d *Dispatcher) handleServerRequest(message rpcMessage) error {
	request := ServerRequest{
		ID:     cloneRaw(message.ID),
		Method: message.Method,
		Params: cloneRaw(message.Params),
		TaskID: extractTaskID(message.Params),
	}
	key := idKey(request.ID)

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return d.closeErr
	}
	if _, exists := d.serverRequests[key]; exists {
		d.mu.Unlock()
		return fmt.Errorf("duplicate app-server request id")
	}
	d.serverRequests[key] = &serverRequestState{request: request}
	d.mu.Unlock()

	select {
	case d.requests <- request:
		return nil
	case <-d.done:
		return d.closeError()
	}
}

func (d *Dispatcher) handleNotification(message rpcMessage) error {
	metadata := d.recordProvisionalID(message)
	resolvedChecked := message.Method == "serverRequest/resolved"
	resolvedMatched := false
	if resolvedChecked {
		resolvedMatched = d.removeResolvedServerRequest(message.Params)
	}
	notification := Notification{
		Method:                       message.Method,
		Params:                       cloneRaw(message.Params),
		TaskID:                       metadata.TaskID,
		SessionGroupID:               metadata.SessionGroupID,
		ServerRequestResolvedChecked: resolvedChecked,
		ServerRequestResolvedMatched: resolvedMatched,
	}
	select {
	case d.notifications <- notification:
		return nil
	case <-d.done:
		return d.closeError()
	}
}

func (d *Dispatcher) handleResponse(message rpcMessage) error {
	key := idKey(message.ID)
	d.mu.Lock()
	call, ok := d.pending[key]
	if ok {
		delete(d.pending, key)
	}
	d.mu.Unlock()
	if !ok {
		return nil
	}

	var err error
	if message.Error != nil {
		err = fmt.Errorf("app-server %s failed with code %d", call.method, message.Error.Code)
	} else {
		err = d.confirmProvisionalID(call, message.Result)
	}
	if err != nil {
		call.result <- callResult{err: err}
		if errors.Is(err, ErrProtocolMismatch) {
			d.closeWithError(err)
		}
		return nil
	}
	call.result <- callResult{result: cloneRaw(message.Result)}
	return nil
}

func (d *Dispatcher) recordProvisionalID(message rpcMessage) CallMetadata {
	var lifecycle provisionalLifecycle
	var methods []string
	switch message.Method {
	case "thread/started":
		lifecycle.threadID = extractThreadID(message.Params)
		lifecycle.provisionalID = lifecycle.threadID
		methods = []string{"thread/start", "thread/resume"}
	case "turn/started":
		lifecycle.threadID = extractThreadID(message.Params)
		lifecycle.turnID = extractTurnID(message.Params)
		lifecycle.provisionalID = lifecycle.turnID
		methods = []string{"turn/start"}
	default:
		return CallMetadata{}
	}
	if lifecycle.provisionalID == "" {
		return CallMetadata{}
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	var fallback *pendingCall
	for _, method := range methods {
		for _, call := range d.pending {
			if call.matchesLifecycle(method, lifecycle) {
				if call.hasExpectedLifecycleID() {
					return call.bindProvisionalID(lifecycle.provisionalID)
				}
				if fallback == nil {
					fallback = call
				}
			}
		}
	}
	if fallback != nil {
		return fallback.bindProvisionalID(lifecycle.provisionalID)
	}
	return CallMetadata{}
}

func (d *Dispatcher) confirmProvisionalID(call *pendingCall, result json.RawMessage) error {
	if call.provisionalID == "" {
		return nil
	}
	var confirmed string
	switch call.method {
	case "thread/start", "thread/resume":
		confirmed = extractThreadID(result)
	case "turn/start":
		confirmed = extractTurnID(result)
	default:
		return nil
	}
	if confirmed == "" || confirmed != call.provisionalID {
		return ErrProtocolMismatch
	}
	return nil
}

type provisionalLifecycle struct {
	provisionalID string
	threadID      string
	turnID        string
}

func expectedCallMetadata(method string, params json.RawMessage, metadata CallMetadata) CallMetadata {
	switch method {
	case "thread/resume", "turn/start":
		if metadata.ExpectedThreadID == "" {
			metadata.ExpectedThreadID = extractThreadID(params)
		}
	}
	return metadata
}

func (call *pendingCall) matchesLifecycle(method string, lifecycle provisionalLifecycle) bool {
	if call.method != method || call.provisionalID != "" {
		return false
	}
	if call.expectedThreadID != "" && call.expectedThreadID != lifecycle.threadID {
		return false
	}
	if call.expectedTurnID != "" && call.expectedTurnID != lifecycle.turnID {
		return false
	}
	return true
}

func (call *pendingCall) hasExpectedLifecycleID() bool {
	return call.expectedThreadID != "" || call.expectedTurnID != ""
}

func (call *pendingCall) bindProvisionalID(provisionalID string) CallMetadata {
	call.provisionalID = provisionalID
	return CallMetadata{
		TaskID:         call.taskID,
		SessionGroupID: call.sessionGroupID,
	}
}

func (d *Dispatcher) removePending(id uint64) bool {
	key := string(mustID(id))
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.pending[key]; ok {
		delete(d.pending, key)
		return true
	}
	return false
}

func (d *Dispatcher) completeServerRequest(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.serverRequests[key]; !ok {
		return false
	}
	delete(d.serverRequests, key)
	return true
}

func (d *Dispatcher) removeResolvedServerRequest(params json.RawMessage) bool {
	requestID := serverRequestIDFromResolved(params)
	if len(requestID) == 0 {
		return false
	}
	key := idKey(requestID)
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.serverRequests[key]; ok {
		delete(d.serverRequests, key)
		return true
	}
	return false
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
		if raw := payload[key]; len(raw) != 0 {
			return raw
		}
	}
	return nil
}

func (d *Dispatcher) closeWithError(err error) {
	if err == nil {
		err = ErrDispatcherClosed
	}
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		d.closeErr = err
		pending := d.pending
		d.pending = map[string]*pendingCall{}
		d.serverRequests = map[string]*serverRequestState{}
		d.mu.Unlock()

		for _, call := range pending {
			call.result <- callResult{err: err}
		}
		_ = d.stdin.Close()
		_ = d.stdout.Close()
		close(d.done)
	})
}

func (d *Dispatcher) closeOutputs() {
	d.outputsOnce.Do(func() {
		close(d.requests)
		close(d.notifications)
	})
}

func (d *Dispatcher) closeError() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closeErr != nil {
		return d.closeErr
	}
	return ErrDispatcherClosed
}
