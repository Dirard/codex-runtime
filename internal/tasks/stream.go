package tasks

import (
	"context"
	"encoding/json"
	"io"
	"sort"
	"sync"

	"github.com/Dirard/codex-runtime/internal/domain"
	"github.com/Dirard/codex-runtime/internal/grpcapi"
	"github.com/Dirard/codex-runtime/internal/redact"
)

const streamSubscriberQueue = 256

type taskStream struct {
	replay []grpcapi.StreamTaskMessage
	live   <-chan domain.TaskEvent

	closeOnce sync.Once
	closeFunc func()
}

type taskSubscriber struct {
	id     uint64
	events chan domain.TaskEvent

	mu     sync.Mutex
	closed bool
}

type taskStreamKind string

const (
	assistantDeltaStream taskStreamKind = "assistant_delta"
	commandOutputStream  taskStreamKind = "command_output"
)

type taskStreamKey struct {
	kind   taskStreamKind
	itemID string
}

func (s *taskStream) Next(ctx context.Context) (grpcapi.StreamTaskMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(s.replay) > 0 {
		message := s.replay[0]
		s.replay = s.replay[1:]
		return message, nil
	}
	if s.live == nil {
		return grpcapi.StreamTaskMessage{}, io.EOF
	}
	select {
	case event, ok := <-s.live:
		if !ok {
			return grpcapi.StreamTaskMessage{}, io.EOF
		}
		return grpcapi.StreamTaskMessage{
			SessionGroupID: event.SessionGroupID,
			Event:          &event,
		}, nil
	case <-ctx.Done():
		return grpcapi.StreamTaskMessage{}, ctx.Err()
	}
}

func (s *taskStream) Close() error {
	s.closeOnce.Do(func() {
		if s.closeFunc != nil {
			s.closeFunc()
		}
	})
	return nil
}

func (s *Service) subscribeLocked(task *task, command domain.StreamTaskCommand) ([]grpcapi.StreamTaskMessage, <-chan domain.TaskEvent, uint64, error) {
	messages, err := replayMessagesLocked(task, command)
	if err != nil {
		return nil, nil, 0, err
	}
	if isTerminalState(task.state) {
		return messages, nil, 0, nil
	}
	task.nextSubscriberID++
	subscriber := &taskSubscriber{
		id:     task.nextSubscriberID,
		events: make(chan domain.TaskEvent, streamSubscriberQueue),
	}
	task.subscribers[subscriber.id] = subscriber
	return messages, subscriber.events, subscriber.id, nil
}

func replayMessagesLocked(task *task, command domain.StreamTaskCommand) ([]grpcapi.StreamTaskMessage, error) {
	switch command.CursorKind {
	case domain.StreamCursorFromStart:
		messages := make([]grpcapi.StreamTaskMessage, 0, len(task.events)+1)
		if task.startEvictedBeforeEventID != 0 {
			messages = append(messages, replayNoticeMessageLocked(task, domain.ReplayNoticeStartEvicted))
		}
		return appendEventMessages(messages, task.events), nil
	case domain.StreamCursorAfterEventID:
		if command.AfterEventID > task.nextEventID {
			return nil, invalidCursor(task.sessionGroupID, task.id)
		}
		messages := make([]grpcapi.StreamTaskMessage, 0, len(task.events)+1)
		if len(task.events) > 0 && command.AfterEventID < task.events[0].EventID {
			messages = append(messages, replayNoticeMessageLocked(task, domain.ReplayNoticeCursorEvicted))
			return appendEventMessages(messages, task.events), nil
		}
		for _, event := range task.events {
			if event.EventID > command.AfterEventID {
				event := event
				messages = append(messages, grpcapi.StreamTaskMessage{
					SessionGroupID: event.SessionGroupID,
					Event:          &event,
				})
			}
		}
		return messages, nil
	default:
		return nil, invalidCursor(task.sessionGroupID, task.id)
	}
}

func appendEventMessages(messages []grpcapi.StreamTaskMessage, events []domain.TaskEvent) []grpcapi.StreamTaskMessage {
	for _, event := range events {
		event := event
		messages = append(messages, grpcapi.StreamTaskMessage{
			SessionGroupID: event.SessionGroupID,
			Event:          &event,
		})
	}
	return messages
}

func replayNoticeMessageLocked(task *task, code domain.ReplayNoticeCode) grpcapi.StreamTaskMessage {
	return grpcapi.StreamTaskMessage{
		SessionGroupID: task.sessionGroupID,
		ReplayNotice: &domain.ReplayNotice{
			Code:                      code,
			Message:                   replayNoticeMessage(code),
			OldestBufferedEventID:     oldestBufferedEventID(task.events),
			NewestBufferedEventID:     newestBufferedEventID(task.events),
			FromStartAvailable:        task.startEvictedBeforeEventID == 0,
			StartEvictedBeforeEventID: task.startEvictedBeforeEventID,
		},
	}
}

func replayNoticeMessage(code domain.ReplayNoticeCode) string {
	switch code {
	case domain.ReplayNoticeStartEvicted:
		return "the beginning of the task event log is no longer buffered"
	case domain.ReplayNoticeCursorEvicted:
		return "the requested task event cursor is no longer buffered"
	default:
		return "task event replay starts from the oldest buffered event"
	}
}

func oldestBufferedEventID(events []domain.TaskEvent) uint64 {
	if len(events) == 0 {
		return 0
	}
	return events[0].EventID
}

func newestBufferedEventID(events []domain.TaskEvent) uint64 {
	if len(events) == 0 {
		return 0
	}
	return events[len(events)-1].EventID
}

func (s *Service) unsubscribe(taskID string, subscriberID uint64) {
	if subscriberID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if task := s.tasks[taskID]; task != nil {
		delete(task.subscribers, subscriberID)
	}
}

func (s *Service) appendEventLocked(task *task, payload domain.TaskEventPayload) (domain.TaskEvent, []*taskSubscriber) {
	task.nextEventID++
	event := domain.TaskEvent{
		EventID:         task.nextEventID,
		TaskID:          task.id,
		SessionGroupID:  task.sessionGroupID,
		WorkspaceID:     task.workspaceID,
		ThreadID:        task.threadID,
		TurnID:          task.turnID,
		CreatedAtUnixMS: s.now().UnixMilli(),
		Payload:         payload,
	}
	task.events = append(task.events, event)
	task.retainedEventBytes += retainedEventSize(event)
	s.enforceReplayLimitsLocked(task)
	subscribers := make([]*taskSubscriber, 0, len(task.subscribers))
	for _, subscriber := range task.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	return event, subscribers
}

func (s *Service) appendPublicationsLocked(task *task, payloads []domain.TaskEventPayload, closeAfter bool) []publication {
	publications := make([]publication, 0, len(payloads))
	for _, payload := range payloads {
		if payload == nil {
			continue
		}
		event, subscribers := s.appendEventLocked(task, payload)
		publications = append(publications, publication{
			event:       event,
			taskID:      task.id,
			subscribers: subscribers,
		})
	}
	if closeAfter && len(publications) > 0 {
		publications[len(publications)-1].closeAfter = true
	}
	return publications
}

func (s *Service) publishPublications(publications []publication) {
	for _, publication := range publications {
		s.publishEvent(publication.taskID, publication.subscribers, publication.event, publication.closeAfter)
	}
}

func (s *Service) scopedRedactorLocked(task *task) *redact.Redactor {
	if task == nil {
		return redact.New()
	}
	if task.sensitive == nil {
		task.sensitive = redact.NewRegistry()
	}
	options := []redact.Option{redact.WithTaskRegistry(task.sensitive)}
	if task.connection != nil {
		options = append(options, redact.WithConnectionRegistry(task.connection.SensitiveRegistry()))
	}
	if task.pathSanitizer != nil {
		options = append(options, redact.WithPathSanitizer(task.pathSanitizer))
	}
	return redact.New(options...)
}

func (s *Service) writeStreamDeltaLocked(task *task, key taskStreamKey, value string, maxBytes int) (string, bool) {
	if value == "" {
		return "", false
	}
	stream := s.streamForTaskLocked(task, key)
	return truncateUTF8Bytes(stream.Write(value), maxBytes)
}

func (s *Service) streamForTaskLocked(task *task, key taskStreamKey) *redact.Stream {
	if task.streams == nil {
		task.streams = map[taskStreamKey]*redact.Stream{}
	}
	stream := task.streams[key]
	if stream == nil {
		stream = redact.NewStream(s.scopedRedactorLocked(task))
		task.streams[key] = stream
	}
	return stream
}

func (s *Service) flushItemStreamsLocked(task *task, itemID string) []domain.TaskEventPayload {
	if task == nil || len(task.streams) == 0 {
		return nil
	}
	var payloads []domain.TaskEventPayload
	for _, key := range sortedStreamKeys(task.streams) {
		if key.itemID != itemID {
			continue
		}
		stream := task.streams[key]
		if payload := flushStreamPayload(key, stream); payload != nil {
			payloads = append(payloads, payload)
		}
		delete(task.streams, key)
	}
	return payloads
}

func (s *Service) flushTaskStreamsLocked(task *task) []domain.TaskEventPayload {
	if task == nil || len(task.streams) == 0 {
		return nil
	}
	payloads := make([]domain.TaskEventPayload, 0, len(task.streams))
	for _, key := range sortedStreamKeys(task.streams) {
		stream := task.streams[key]
		if payload := flushStreamPayload(key, stream); payload != nil {
			payloads = append(payloads, payload)
		}
		delete(task.streams, key)
	}
	return payloads
}

func sortedStreamKeys(streams map[taskStreamKey]*redact.Stream) []taskStreamKey {
	keys := make([]taskStreamKey, 0, len(streams))
	for key := range streams {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i int, j int) bool {
		if keys[i].itemID == keys[j].itemID {
			return keys[i].kind < keys[j].kind
		}
		return keys[i].itemID < keys[j].itemID
	})
	return keys
}

func flushStreamPayload(key taskStreamKey, stream *redact.Stream) domain.TaskEventPayload {
	delta := stream.Flush()
	if delta == "" {
		return nil
	}
	switch key.kind {
	case assistantDeltaStream:
		text, truncated := truncateUTF8Bytes(delta, domain.MaxOutboundAssistantTextBytes)
		if text == "" {
			return nil
		}
		return domain.AssistantDeltaEvent{TextDelta: text, Truncated: truncated}
	case commandOutputStream:
		text, truncated := truncateUTF8Bytes(delta, domain.MaxOutboundCommandOutputDeltaBytes)
		if text == "" {
			return nil
		}
		return domain.CommandOutputDeltaEvent{
			ItemID:    key.itemID,
			Stream:    domain.CommandOutputStreamCombined,
			Delta:     text,
			Truncated: truncated,
		}
	default:
		return nil
	}
}

func (s *Service) enforceReplayLimitsLocked(task *task) {
	maxEvents := s.replayRetentionEventCapLocked(task.sessionGroupID)
	maxBytes := s.replayRetentionByteCapLocked(task.sessionGroupID)
	for len(task.events) > maxEvents || len(task.events) > 1 && task.retainedEventBytes > maxBytes {
		evicted := task.events[0]
		task.events = task.events[1:]
		task.retainedEventBytes -= retainedEventSize(evicted)
		if task.retainedEventBytes < 0 {
			task.retainedEventBytes = 0
		}
		task.startEvictedBeforeEventID = evicted.EventID
	}
}

func retainedEventSize(event domain.TaskEvent) int64 {
	raw, err := json.Marshal(event)
	if err != nil {
		return 0
	}
	return int64(len(raw))
}

func (s *Service) publishEvent(taskID string, subscribers []*taskSubscriber, event domain.TaskEvent, closeAfter bool) {
	if event.EventID == 0 {
		return
	}
	for _, subscriber := range subscribers {
		if !subscriber.send(event) {
			s.removeSubscriber(taskID, subscriber.id, subscriber)
			continue
		}
		if closeAfter {
			subscriber.close()
		}
	}
}

func (s *Service) removeSubscriber(taskID string, subscriberID uint64, subscriber *taskSubscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[taskID]
	if task == nil || task.subscribers[subscriberID] != subscriber {
		return
	}
	delete(task.subscribers, subscriberID)
}

func (s *taskSubscriber) send(event domain.TaskEvent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.events <- event:
		return true
	default:
		close(s.events)
		s.closed = true
		return false
	}
}

func (s *taskSubscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	close(s.events)
	s.closed = true
}
