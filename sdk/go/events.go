package codex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrStreamEndedBeforeTerminal reports an owned stream that reached EOF
	// before a terminal run event.
	ErrStreamEndedBeforeTerminal = errors.New("codex: stream ended before terminal event")
	// ErrEventDecode wraps fatal failures while decoding a protobuf stream
	// envelope into a friendly StreamEvent.
	ErrEventDecode = errors.New("codex: failed to decode stream event")
	// ErrStreamReadMode reports mixing raw Recv and friendly NextEvent reads on
	// one EventStream.
	ErrStreamReadMode = errors.New("codex: event stream read mode already selected")
	// ErrEventCursorUnavailable reports an accepted raw-compatible run result
	// that lacks the opaque event cursor required by friendly resume helpers.
	ErrEventCursorUnavailable = errors.New("codex: event cursor unavailable for friendly stream")
	// ErrStreamCursorConflict reports a cursor-moving initial stream option on
	// helpers that own the new run stream.
	ErrStreamCursorConflict = errors.New("codex: stream cursor option conflicts with run-owned stream")
	// ErrRunFailed wraps terminal failed run errors.
	ErrRunFailed = errors.New("codex: run failed")
	// ErrRunInterrupted wraps terminal interrupted run errors.
	ErrRunInterrupted = errors.New("codex: run interrupted")
)

type noUnkeyedLiterals struct{}

// EventKind is the stable friendly category for StreamEvent values.
type EventKind string

const (
	EventKindStatusChanged             EventKind = "status_changed"
	EventKindAssistantTextDelta        EventKind = "assistant_text_delta"
	EventKindAssistantMessageCompleted EventKind = "assistant_message_completed"
	EventKindCommandStarted            EventKind = "command_started"
	EventKindCommandOutput             EventKind = "command_output"
	EventKindWarning                   EventKind = "warning"
	EventKindApprovalRequested         EventKind = "approval_requested"
	EventKindPermissionsRequested      EventKind = "permissions_requested"
	EventKindStructuredInputRequested  EventKind = "structured_input_requested"
	EventKindUserInputRequested        EventKind = "user_input_requested"
	EventKindActionResolved            EventKind = "action_resolved"
	EventKindRunCompleted              EventKind = "run_completed"
	EventKindRunFailed                 EventKind = "run_failed"
	EventKindRunInterrupted            EventKind = "run_interrupted"
	EventKindReplayNotice              EventKind = "replay_notice"
	EventKindStreamNarrowed            EventKind = "stream_narrowed"
	EventKindUnknown                   EventKind = "unknown"
)

// EventSource identifies whether an event came from a chat event payload or a
// stream-level notice.
type EventSource string

const (
	EventSourceChatStream   EventSource = "chat_stream"
	EventSourceStreamNotice EventSource = "stream_notice"
)

// EventMeta is the resumable identity shared by all friendly stream events.
// Save Cursor only when CanResumeAfter is true.
type EventMeta struct {
	_ noUnkeyedLiterals

	ID             uint64
	Cursor         string
	ChatID         string
	RunID          string
	SessionGroupID string
	WorkspaceID    string
	CreatedAt      time.Time
	Source         EventSource
	CanResumeAfter bool
}

// StreamEvent is the sealed interface implemented by every friendly event
// returned from EventStream.NextEvent.
type StreamEvent interface {
	EventKind() EventKind
	Meta() EventMeta
	Raw() RawEvent
	streamEvent()
}

// TerminalEvent is implemented by RunCompleted, RunFailed and RunInterrupted.
// Result contains the terminal state and user-facing error, if any.
type TerminalEvent interface {
	StreamEvent
	Result() TerminalResult
	terminalEvent()
}

type baseEvent struct {
	kind EventKind
	meta EventMeta
	raw  RawEvent
}

func (event baseEvent) EventKind() EventKind { return event.kind }
func (event baseEvent) Meta() EventMeta      { return event.meta }
func (event baseEvent) Raw() RawEvent        { return event.raw }
func (event baseEvent) streamEvent()         {}

// RawEvent is the advanced diagnostics view of a friendly event. Its string and
// slog forms are metadata-only; Proto returns a cloned protobuf envelope.
type RawEvent struct {
	_ noUnkeyedLiterals

	kind    EventKind
	source  EventSource
	id      uint64
	cursor  string
	payload bool
	proto   *pb.StreamChatEventsResponse
}

func (raw RawEvent) Kind() EventKind     { return raw.kind }
func (raw RawEvent) Source() EventSource { return raw.source }
func (raw RawEvent) ID() uint64          { return raw.id }
func (raw RawEvent) Cursor() string      { return raw.cursor }
func (raw RawEvent) HasPayload() bool    { return raw.payload }

// Proto returns a clone of the source protobuf envelope when available.
func (raw RawEvent) Proto() *pb.StreamChatEventsResponse {
	if raw.proto == nil {
		return nil
	}
	return proto.Clone(raw.proto).(*pb.StreamChatEventsResponse)
}

func (raw RawEvent) String() string {
	return fmt.Sprintf("RawEvent{kind:%s source:%s id:%d cursor:%q has_payload:%t}", raw.kind, raw.source, raw.id, raw.cursor, raw.payload)
}

func (raw RawEvent) GoString() string {
	return raw.String()
}

func (raw RawEvent) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("kind", string(raw.kind)),
		slog.String("source", string(raw.source)),
		slog.Uint64("id", raw.id),
		slog.String("cursor", raw.cursor),
		slog.Bool("has_payload", raw.payload),
	)
}

// StatusChanged reports a source-backed chat status snapshot.
type StatusChanged struct {
	baseEvent
	status StatusSnapshot
}

func (event *StatusChanged) Status() StatusSnapshot { return event.status }

// AssistantTextDelta carries incremental assistant text.
type AssistantTextDelta struct {
	baseEvent
	textDelta string
	truncated bool
}

func (event *AssistantTextDelta) TextDelta() string { return event.textDelta }
func (event *AssistantTextDelta) Truncated() bool   { return event.truncated }

// AssistantMessageCompleted carries the completed assistant message when the
// runtime provides one.
type AssistantMessageCompleted struct {
	baseEvent
	message   string
	truncated bool
}

func (event *AssistantMessageCompleted) Message() string { return event.message }
func (event *AssistantMessageCompleted) Truncated() bool { return event.truncated }

// ActionResolved reports that a pending action was resolved by the runtime.
type ActionResolved struct {
	baseEvent
	pendingID   string
	pendingKind PendingKind
	resolution  PendingResolution
	message     string
}

func (event *ActionResolved) PendingID() string             { return event.pendingID }
func (event *ActionResolved) PendingKind() PendingKind      { return event.pendingKind }
func (event *ActionResolved) Resolution() PendingResolution { return event.resolution }
func (event *ActionResolved) DisplayMessage() string        { return event.message }

// TerminalState is the friendly state of a terminal run event.
type TerminalState string

const (
	TerminalStateUnknown     TerminalState = ""
	TerminalStateCompleted   TerminalState = "completed"
	TerminalStateFailed      TerminalState = "failed"
	TerminalStateInterrupted TerminalState = "interrupted"
)

// TerminalResult is returned by terminal events and carries the terminal error
// for failed or interrupted runs.
type TerminalResult struct {
	_ noUnkeyedLiterals

	State   TerminalState
	Summary string
	Err     error
}

// RunCompleted marks a successful terminal run event.
type RunCompleted struct {
	baseEvent
	result TerminalResult
}

func (event *RunCompleted) Result() TerminalResult { return event.result }
func (event *RunCompleted) terminalEvent()         {}

// RunFailed marks a failed terminal run event.
type RunFailed struct {
	baseEvent
	result TerminalResult
}

func (event *RunFailed) Result() TerminalResult { return event.result }
func (event *RunFailed) terminalEvent()         {}

// RunInterrupted marks an interrupted terminal run event.
type RunInterrupted struct {
	baseEvent
	result TerminalResult
}

func (event *RunInterrupted) Result() TerminalResult { return event.result }
func (event *RunInterrupted) terminalEvent()         {}

// RunFailedError wraps source-backed failed run details and unwraps to
// ErrRunFailed.
type RunFailedError struct {
	_ noUnkeyedLiterals

	RunID   string
	Summary string
	Message string
}

func (err *RunFailedError) Error() string {
	if err == nil || strings.TrimSpace(err.Message) == "" {
		return ErrRunFailed.Error()
	}
	return err.Message
}

func (err *RunFailedError) Unwrap() error { return ErrRunFailed }

// RunInterruptedError wraps source-backed interrupted run details and unwraps to
// ErrRunInterrupted.
type RunInterruptedError struct {
	_ noUnkeyedLiterals

	RunID   string
	Summary string
	Message string
}

func (err *RunInterruptedError) Error() string {
	if err == nil || strings.TrimSpace(err.Message) == "" {
		return ErrRunInterrupted.Error()
	}
	return err.Message
}

func (err *RunInterruptedError) Unwrap() error { return ErrRunInterrupted }

// ReplayNotice is stream metadata about replay availability. It is not a resume
// point.
type ReplayNotice struct {
	baseEvent
	code          string
	message       string
	bufferedRange BufferedEventRange
}

// BufferedEventRange describes the event-id range mentioned by ReplayNotice.
type BufferedEventRange struct {
	_ noUnkeyedLiterals

	OldestEventID uint64
	NewestEventID uint64
}

func (event *ReplayNotice) Code() string                      { return event.code }
func (event *ReplayNotice) Reason() string                    { return event.code }
func (event *ReplayNotice) Message() string                   { return event.message }
func (event *ReplayNotice) BufferedRange() BufferedEventRange { return event.bufferedRange }

// StreamNarrowed reports that the gateway returned a narrower stream than
// requested.
type StreamNarrowed struct {
	baseEvent
	reason  string
	message string
}

func (event *StreamNarrowed) Reason() string  { return event.reason }
func (event *StreamNarrowed) Message() string { return event.message }

// UnknownEvent preserves bounded metadata for a source-backed event that has no
// friendly P0 type yet.
type UnknownEvent struct {
	baseEvent
	name string
}

func (event *UnknownEvent) Name() string { return event.name }

type EventPosition struct {
	_ noUnkeyedLiterals

	ID     uint64
	Cursor string
}

type EventDecodeError struct {
	_ noUnkeyedLiterals

	Message  string
	Cause    error
	meta     EventMeta
	raw      RawEvent
	position EventPosition
}

func (err *EventDecodeError) Error() string {
	if err == nil || strings.TrimSpace(err.Message) == "" {
		return ErrEventDecode.Error()
	}
	return ErrEventDecode.Error() + ": " + err.Message
}

func (err *EventDecodeError) Unwrap() error {
	if err == nil || err.Cause == nil {
		return ErrEventDecode
	}
	return errors.Join(ErrEventDecode, err.Cause)
}

func (err *EventDecodeError) SafeMeta() EventMeta {
	if err == nil {
		return EventMeta{}
	}
	return err.meta
}

func (err *EventDecodeError) SafeRaw() RawEvent {
	if err == nil {
		return RawEvent{}
	}
	return err.raw
}

func (err *EventDecodeError) Position() EventPosition {
	if err == nil {
		return EventPosition{}
	}
	return err.position
}

type StreamReadModeError struct {
	_ noUnkeyedLiterals

	Requested string
	Active    string
}

func (err *StreamReadModeError) Error() string {
	return fmt.Sprintf("%v: requested %s after %s", ErrStreamReadMode, err.Requested, err.Active)
}

func (err *StreamReadModeError) Unwrap() error { return ErrStreamReadMode }

type EventCursorUnavailableError struct {
	_ noUnkeyedLiterals

	ChatID string
	RunID  string
}

func (err *EventCursorUnavailableError) Error() string {
	return ErrEventCursorUnavailable.Error()
}

func (err *EventCursorUnavailableError) Unwrap() error { return ErrEventCursorUnavailable }

type StreamCursorConflictError struct {
	_ noUnkeyedLiterals

	Message string
}

func (err *StreamCursorConflictError) Error() string {
	if err == nil || err.Message == "" {
		return ErrStreamCursorConflict.Error()
	}
	return ErrStreamCursorConflict.Error() + ": " + err.Message
}

func (err *StreamCursorConflictError) Unwrap() error { return ErrStreamCursorConflict }

// NextEvent receives and decodes the next friendly event. The context is checked
// before each receive; cancellation of an already blocking gRPC receive is
// controlled by the context used to open the stream or by EventStream.Close.
func (s *EventStream) NextEvent(ctx context.Context) (StreamEvent, error) {
	if s == nil {
		return nil, ErrNilClient
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, normalizeError(err)
		}
	}
	if s.friendlyCursorUnavailable {
		return nil, &EventCursorUnavailableError{ChatID: s.chatID, RunID: s.runID}
	}
	if err := s.beginReadMode(eventStreamReadModeTyped); err != nil {
		return nil, err
	}
	if s.unusableErr != nil {
		return nil, s.unusableErr
	}
	message, err := s.recvRaw()
	if err != nil {
		if errors.Is(err, io.EOF) && s.expectTerminal {
			if s.terminalSeen {
				return nil, io.EOF
			}
			s.unusableErr = ErrStreamEndedBeforeTerminal
			return nil, ErrStreamEndedBeforeTerminal
		}
		if ctx != nil && ctx.Err() != nil {
			_ = s.Close()
		}
		return nil, err
	}
	event, err := s.decodeStreamEvent(message)
	if err != nil {
		s.unusableErr = err
		return nil, err
	}
	if event.Meta().CanResumeAfter {
		s.lastSafeResumeMeta = event.Meta()
	}
	if terminal, ok := event.(TerminalEvent); ok {
		_ = terminal
		s.terminalSeen = true
	}
	return event, nil
}

func (s *EventStream) decodeStreamEvent(message *pb.StreamChatEventsResponse) (StreamEvent, error) {
	switch payload := message.GetPayload().(type) {
	case *pb.StreamChatEventsResponse_Event:
		return decodeChatEventWithStream(s, message, payload.Event)
	case *pb.StreamChatEventsResponse_ReplayNotice:
		return decodeReplayNotice(message, payload.ReplayNotice), nil
	case *pb.StreamChatEventsResponse_Narrowed:
		return decodeNarrowed(message, payload.Narrowed), nil
	default:
		raw := newRawEvent(EventKindUnknown, EventSourceStreamNotice, 0, "", false, message)
		return &UnknownEvent{baseEvent: baseEvent{kind: EventKindUnknown, meta: EventMeta{Source: EventSourceStreamNotice}, raw: raw}, name: "empty"}, nil
	}
}

func decodeChatEvent(envelope *pb.StreamChatEventsResponse, event *pb.ChatEvent) (StreamEvent, error) {
	return decodeChatEventWithStream(nil, envelope, event)
}

func decodeChatEventWithStream(stream *EventStream, envelope *pb.StreamChatEventsResponse, event *pb.ChatEvent) (StreamEvent, error) {
	meta := metaFromChatEvent(event)
	raw := newRawEvent(EventKindUnknown, EventSourceChatStream, meta.ID, meta.Cursor, event.GetPayload() != nil, envelope)
	if event == nil {
		return nil, &EventDecodeError{Message: "chat event is nil", raw: raw}
	}
	if meta.ID == 0 || meta.Cursor == "" || meta.ChatID == "" || meta.RunID == "" {
		return nil, &EventDecodeError{
			Message:  "chat event missing required metadata",
			meta:     meta,
			raw:      raw,
			position: EventPosition{ID: meta.ID, Cursor: meta.Cursor},
		}
	}
	meta.Source = EventSourceChatStream
	meta.CanResumeAfter = true
	switch payload := event.GetPayload().(type) {
	case *pb.ChatEvent_StatusUpdated:
		base := newBase(EventKindStatusChanged, meta, envelope, event.GetPayload() != nil)
		return &StatusChanged{baseEvent: base, status: statusSnapshotFromProto(payload.StatusUpdated.GetStatus())}, nil
	case *pb.ChatEvent_AssistantDelta:
		base := newBase(EventKindAssistantTextDelta, meta, envelope, true)
		return &AssistantTextDelta{baseEvent: base, textDelta: payload.AssistantDelta.GetTextDelta(), truncated: payload.AssistantDelta.GetTruncated()}, nil
	case *pb.ChatEvent_AssistantMessageCompleted:
		base := newBase(EventKindAssistantMessageCompleted, meta, envelope, true)
		return &AssistantMessageCompleted{baseEvent: base, message: payload.AssistantMessageCompleted.GetMessage(), truncated: payload.AssistantMessageCompleted.GetTruncated()}, nil
	case *pb.ChatEvent_CommandStarted:
		return decodeCommandStarted(stream, meta, envelope, payload.CommandStarted), nil
	case *pb.ChatEvent_CommandOutputDelta:
		return decodeCommandOutput(stream, meta, envelope, payload.CommandOutputDelta), nil
	case *pb.ChatEvent_GatewayWarning:
		return decodeWarning(meta, envelope, payload.GatewayWarning), nil
	case *pb.ChatEvent_PendingRequestCreated:
		meta.CanResumeAfter = false
		return decodePendingCreated(meta, envelope, payload.PendingRequestCreated.GetPendingRequest())
	case *pb.ChatEvent_PendingRequestResolved:
		resolved := payload.PendingRequestResolved
		base := newBase(EventKindActionResolved, meta, envelope, true)
		return &ActionResolved{baseEvent: base, pendingID: resolved.GetPendingRequestId(), pendingKind: pendingKindFromProto(resolved.GetPendingType()), resolution: pendingResolutionFromProto(resolved.GetResolution()), message: resolved.GetDisplayMessage()}, nil
	case *pb.ChatEvent_Terminal:
		return decodeTerminal(meta, envelope, payload.Terminal.GetTerminal()), nil
	default:
		base := newBase(EventKindUnknown, meta, envelope, event.GetPayload() != nil)
		return &UnknownEvent{baseEvent: base, name: chatPayloadName(event)}, nil
	}
}

func newBase(kind EventKind, meta EventMeta, envelope *pb.StreamChatEventsResponse, hasPayload bool) baseEvent {
	meta.Source = EventSourceChatStream
	return baseEvent{
		kind: kind,
		meta: meta,
		raw:  newRawEvent(kind, meta.Source, meta.ID, meta.Cursor, hasPayload, envelope),
	}
}

func metaFromChatEvent(event *pb.ChatEvent) EventMeta {
	if event == nil {
		return EventMeta{}
	}
	var created time.Time
	if event.GetCreatedAtUnixMs() > 0 {
		created = time.UnixMilli(event.GetCreatedAtUnixMs())
	}
	return EventMeta{
		ID:             event.GetEventId(),
		Cursor:         event.GetEventCursor(),
		ChatID:         event.GetChatId(),
		RunID:          event.GetRunId(),
		SessionGroupID: event.GetSessionGroupId(),
		WorkspaceID:    event.GetWorkspaceId(),
		CreatedAt:      created,
		Source:         EventSourceChatStream,
		CanResumeAfter: event.GetEventCursor() != "",
	}
}

func newRawEvent(kind EventKind, source EventSource, id uint64, cursor string, hasPayload bool, envelope *pb.StreamChatEventsResponse) RawEvent {
	return RawEvent{kind: kind, source: source, id: id, cursor: cursor, payload: hasPayload, proto: cloneStreamResponse(envelope)}
}

func cloneStreamResponse(message *pb.StreamChatEventsResponse) *pb.StreamChatEventsResponse {
	if message == nil {
		return nil
	}
	return proto.Clone(message).(*pb.StreamChatEventsResponse)
}

func decodeReplayNotice(envelope *pb.StreamChatEventsResponse, notice *pb.ChatReplayNotice) StreamEvent {
	raw := newRawEvent(EventKindReplayNotice, EventSourceStreamNotice, 0, "", notice != nil, envelope)
	meta := EventMeta{Source: EventSourceStreamNotice}
	if notice == nil {
		return &ReplayNotice{baseEvent: baseEvent{kind: EventKindReplayNotice, meta: meta, raw: raw}}
	}
	return &ReplayNotice{
		baseEvent: baseEvent{kind: EventKindReplayNotice, meta: meta, raw: raw},
		code:      notice.GetCode().String(),
		message:   notice.GetMessage(),
		bufferedRange: BufferedEventRange{
			OldestEventID: notice.GetOldestBufferedEventId(),
			NewestEventID: notice.GetNewestBufferedEventId(),
		},
	}
}

func decodeNarrowed(envelope *pb.StreamChatEventsResponse, narrowed *pb.ChatNarrowedOutcome) StreamEvent {
	raw := newRawEvent(EventKindStreamNarrowed, EventSourceStreamNotice, 0, "", narrowed != nil, envelope)
	meta := EventMeta{Source: EventSourceStreamNotice}
	return &StreamNarrowed{
		baseEvent: baseEvent{kind: EventKindStreamNarrowed, meta: meta, raw: raw},
		reason:    narrowed.GetReason(),
		message:   narrowed.GetDisplayMessage(),
	}
}

func decodeTerminal(meta EventMeta, envelope *pb.StreamChatEventsResponse, terminal *pb.ChatTerminal) StreamEvent {
	switch terminal.GetTerminalLifecycle() {
	case pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_COMPLETED:
		base := newBase(EventKindRunCompleted, meta, envelope, true)
		return &RunCompleted{baseEvent: base, result: TerminalResult{State: TerminalStateCompleted, Summary: terminal.GetResultSummary()}}
	case pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_INTERRUPTED:
		base := newBase(EventKindRunInterrupted, meta, envelope, true)
		err := &RunInterruptedError{RunID: meta.RunID, Summary: terminal.GetResultSummary(), Message: terminal.GetErrorMessage()}
		return &RunInterrupted{baseEvent: base, result: TerminalResult{State: TerminalStateInterrupted, Summary: terminal.GetResultSummary(), Err: err}}
	case pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_FAILED:
		base := newBase(EventKindRunFailed, meta, envelope, true)
		err := &RunFailedError{RunID: meta.RunID, Summary: terminal.GetResultSummary(), Message: terminal.GetErrorMessage()}
		return &RunFailed{baseEvent: base, result: TerminalResult{State: TerminalStateFailed, Summary: terminal.GetResultSummary(), Err: err}}
	default:
		base := newBase(EventKindUnknown, meta, envelope, true)
		return &UnknownEvent{baseEvent: base, name: "terminal_unknown"}
	}
}

func chatPayloadName(event *pb.ChatEvent) string {
	switch event.GetPayload().(type) {
	case *pb.ChatEvent_Lifecycle:
		return "lifecycle"
	case *pb.ChatEvent_PlanUpdated:
		return "plan_updated"
	case *pb.ChatEvent_ToolProgress:
		return "tool_progress"
	case *pb.ChatEvent_FileDiffUpdated:
		return "file_diff_updated"
	case *pb.ChatEvent_TurnDiffUpdated:
		return "turn_diff_updated"
	case *pb.ChatEvent_CommandStarted:
		return "command_started"
	case *pb.ChatEvent_CommandOutputDelta:
		return "command_output_delta"
	case *pb.ChatEvent_GatewayWarning:
		return "gateway_warning"
	default:
		return "unknown"
	}
}

type eventStreamReadMode string

const (
	eventStreamReadModeUnset eventStreamReadMode = ""
	eventStreamReadModeRaw   eventStreamReadMode = "raw"
	eventStreamReadModeTyped eventStreamReadMode = "typed"
)

func (s *EventStream) beginReadMode(mode eventStreamReadMode) error {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	if s.readMode == eventStreamReadModeUnset {
		s.readMode = mode
		return nil
	}
	if s.readMode != mode {
		return &StreamReadModeError{Requested: string(mode), Active: string(s.readMode)}
	}
	return nil
}

func hasCursorMovingStreamOption(opts []StreamOption) bool {
	for _, opt := range opts {
		applied := streamOptions{}
		if opt != nil {
			opt(&applied)
		}
		if applied.cursor != nil {
			return true
		}
	}
	return false
}

func rejectCursorMovingInitialOptions(opts []RequestOption) error {
	applied := applyRequestOptions(opts)
	if hasCursorMovingStreamOption(applied.initialStreamOptions) {
		return &StreamCursorConflictError{Message: "initial stream cursor options are not valid for run-owned friendly streams"}
	}
	return nil
}

// RunWithEvents preserves Chat.Run compatibility while also opening the
// cursor-based friendly event stream for the accepted run.
func (chat *Chat) RunWithEvents(ctx context.Context, prompt string, opts ...RequestOption) (*RunResult, *EventStream, error) {
	if err := rejectCursorMovingInitialOptions(opts); err != nil {
		return nil, nil, err
	}
	result, err := chat.Run(ctx, prompt, opts...)
	if err != nil {
		return nil, nil, err
	}
	events, err := chat.EventsForRun(ctx, result)
	if err != nil {
		return result, nil, err
	}
	return result, events, nil
}

// EventsForRun opens a friendly event stream for an already accepted run. The
// RunResult must contain EventCursor; otherwise ErrEventCursorUnavailable is
// returned without starting another run.
func (chat *Chat) EventsForRun(ctx context.Context, result *RunResult, opts ...StreamOption) (*EventStream, error) {
	if err := chat.ready(); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("%w: run result is nil", ErrInvalidConfiguration)
	}
	if result.ChatID != chat.ID {
		return nil, fmt.Errorf("%w: run result chat id mismatch", ErrInvalidConfiguration)
	}
	if strings.TrimSpace(result.RunID) == "" {
		return nil, fmt.Errorf("%w: run id is required", ErrInvalidConfiguration)
	}
	if strings.TrimSpace(result.EventCursor) == "" {
		return nil, &EventCursorUnavailableError{ChatID: result.ChatID, RunID: result.RunID}
	}
	if hasCursorMovingStreamOption(opts) {
		return nil, &StreamCursorConflictError{Message: "continuation stream cursor is owned by RunResult.EventCursor"}
	}
	streamOpts := append([]StreamOption{AfterEventCursor(result.EventCursor)}, opts...)
	events, err := chat.GetEventsStream(ctx, streamOpts...)
	if err != nil {
		return nil, err
	}
	events.expectTerminal = true
	events.chatID = result.ChatID
	events.runID = result.RunID
	return events, nil
}

func markInitialFriendlyCursor(responseEventCursor string, events *EventStream, chatID string, runID string, explicit []StreamOption) {
	if events == nil {
		return
	}
	events.chatID = chatID
	events.runID = runID
	if responseEventCursor == "" && !hasCursorMovingStreamOption(explicit) {
		events.friendlyCursorUnavailable = true
	}
}

func statusError(code codes.Code, reason string, message string) *Error {
	return newSDKError(code, reason, message, false)
}
