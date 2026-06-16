package chatruntime

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/Dirard/codex-runtime/gateway/internal/chatstate"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	"github.com/Dirard/codex-runtime/gateway/internal/grpcapi"
)

type chatEventStream struct {
	replay []grpcapi.StreamChatEventsMessage
	live   <-chan chatstate.EventRecord

	scope      chatstate.RunScope
	lastSentID uint64
	closeOnce  sync.Once
	closeFunc  func()
	statusFunc func(chatstate.EventRecord) domain.ChatStatus
}

func (s *Service) StreamChatEvents(ctx context.Context, command domain.StreamChatEventsCommand) (grpcapi.ChatEventStream, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lookup, _, err := s.prepareChatLookup(command.SessionGroupID, command.WorkspaceID, command.ChatID)
	if err != nil {
		return nil, err
	}
	command.SessionGroupID = lookup.SessionGroupID
	command.WorkspaceID = lookup.WorkspaceID
	if err := callerErrorFromContext(ctx, command.SessionGroupID, ""); err != nil {
		return nil, err
	}

	runID := ""
	afterEventID := command.AfterEventID
	epoch := s.store.Epoch()
	if command.CursorKind == domain.StreamCursorAfterEventID && command.AfterEventCursor != "" {
		parsed, ok := parseEventCursor(command.AfterEventCursor)
		if !ok {
			return nil, invalidRequest(command.SessionGroupID, "", "event cursor is invalid")
		}
		if parsed.ChatID != command.ChatID {
			return nil, cursorOutOfRange(command.SessionGroupID, command.ChatID, "event cursor is outside this chat")
		}
		epoch = parsed.Epoch
		runID = parsed.RunID
		afterEventID = parsed.EventID
	}
	active, activeOK := s.store.ActiveRun(chatstate.Scope{SessionGroupID: command.SessionGroupID, WorkspaceID: command.WorkspaceID, ChatID: command.ChatID})
	if runID == "" && activeOK {
		runID = active.RunID
	}
	if runID == "" {
		return &chatEventStream{
			replay: []grpcapi.StreamChatEventsMessage{narrowedStreamMessage(command, "no active chat run is known in this gateway process")},
		}, nil
	}

	scope := chatstate.RunScope{
		Scope: chatstate.Scope{
			SessionGroupID: command.SessionGroupID,
			WorkspaceID:    command.WorkspaceID,
			ChatID:         command.ChatID,
		},
		RunID: runID,
	}
	var live <-chan chatstate.EventRecord
	var unsubscribe func()
	if activeOK && active.RunID == runID {
		live, unsubscribe, err = s.store.Subscribe(scope)
		if err != nil {
			return nil, err
		}
	}

	replayResult, replayErr := s.store.Replay(chatstate.Cursor{
		Epoch:          epoch,
		SessionGroupID: command.SessionGroupID,
		WorkspaceID:    command.WorkspaceID,
		ChatID:         command.ChatID,
		RunID:          runID,
		AfterEventID:   afterEventID,
	})
	messages := make([]grpcapi.StreamChatEventsMessage, 0, len(replayResult.Events)+1)
	if replayErr != nil {
		var replayFailure *chatstate.ReplayError
		if errors.As(replayErr, &replayFailure) {
			switch replayFailure.Kind {
			case chatstate.ReplayFailureCursorEvicted:
				messages = append(messages, replayNoticeMessage(command, domain.ChatReplayNoticeCursorEvicted, replayResult, s.store.Epoch()))
			case chatstate.ReplayFailureOutOfRange:
				if unsubscribe != nil {
					unsubscribe()
				}
				return nil, cursorOutOfRange(command.SessionGroupID, command.ChatID, "event cursor is outside the buffered range")
			default:
				messages = append(messages, replayUnavailableMessage(command, runID, s.store.Epoch()))
			}
		} else {
			messages = append(messages, replayUnavailableMessage(command, runID, s.store.Epoch()))
		}
	} else {
		if !replayResult.FromStartAvailable && afterEventID == 0 {
			messages = append(messages, replayNoticeMessage(command, domain.ChatReplayNoticeStartEvicted, replayResult, s.store.Epoch()))
		}
		for _, event := range replayResult.Events {
			messages = append(messages, eventRecordMessage(command, event, eventStatus(command, event)))
		}
	}
	return &chatEventStream{
		replay:    messages,
		live:      live,
		scope:     scope,
		closeFunc: unsubscribe,
		statusFunc: func(event chatstate.EventRecord) domain.ChatStatus {
			return eventStatus(command, event)
		},
	}, nil
}

func (s *chatEventStream) Next(ctx context.Context) (grpcapi.StreamChatEventsMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for len(s.replay) > 0 {
		message := s.replay[0]
		s.replay = s.replay[1:]
		if message.Event != nil {
			s.lastSentID = max(s.lastSentID, message.Event.EventID)
		}
		return message, nil
	}
	if s.live == nil {
		return grpcapi.StreamChatEventsMessage{}, io.EOF
	}
	for {
		select {
		case event, ok := <-s.live:
			if !ok {
				return grpcapi.StreamChatEventsMessage{}, io.EOF
			}
			if event.EventID <= s.lastSentID {
				continue
			}
			s.lastSentID = event.EventID
			status := s.statusFunc(event)
			return eventRecordMessage(domain.StreamChatEventsCommand{
				SessionGroupID: s.scope.SessionGroupID,
				WorkspaceID:    s.scope.WorkspaceID,
				ChatID:         s.scope.ChatID,
			}, event, status), nil
		case <-ctx.Done():
			return grpcapi.StreamChatEventsMessage{}, ctx.Err()
		}
	}
}

func (s *chatEventStream) Close() error {
	s.closeOnce.Do(func() {
		if s.closeFunc != nil {
			s.closeFunc()
		}
	})
	return nil
}

func eventRecordMessage(command domain.StreamChatEventsCommand, record chatstate.EventRecord, status domain.ChatStatus) grpcapi.StreamChatEventsMessage {
	event := &domain.ChatEvent{
		EventID:         record.EventID,
		EventCursor:     eventCursor(record.Epoch, record.ChatID, record.RunID, record.EventID),
		ChatID:          record.ChatID,
		SessionGroupID:  record.SessionGroupID,
		WorkspaceID:     record.WorkspaceID,
		RunID:           record.RunID,
		CreatedAtUnixMS: record.CreatedAtUnixMS,
	}
	switch {
	case record.AssistantDelta != nil:
		event.AssistantDelta = record.AssistantDelta
	case record.AssistantMessageCompleted != nil:
		event.AssistantMessageCompleted = record.AssistantMessageCompleted
	case record.PendingCreated != nil:
		event.PendingCreated = record.PendingCreated
	case record.PendingResolved != nil:
		event.PendingResolved = record.PendingResolved
	case record.Terminal != nil:
		event.Terminal = record.Terminal
	default:
		event.StatusUpdated = &status
	}
	return grpcapi.StreamChatEventsMessage{
		SessionGroupID: command.SessionGroupID,
		Event:          event,
	}
}

func eventStatus(command domain.StreamChatEventsCommand, record chatstate.EventRecord) domain.ChatStatus {
	status := baseChatStatus(command.SessionGroupID, command.WorkspaceID, command.ChatID, record.Epoch)
	runState := chatstate.RunState(record.State)
	status.CurrentRunLifecycle = turnLifecycleFromRunState(runState)
	switch status.CurrentRunLifecycle {
	case domain.ChatTurnLifecycleCompleted, domain.ChatTurnLifecycleFailed, domain.ChatTurnLifecycleInterrupted:
		status.ThreadLifecycle = domain.ChatThreadLifecycleIdle
	default:
		status.ThreadLifecycle = domain.ChatThreadLifecycleActiveRunning
	}
	status.CurrentRunID = record.RunID
	status.LastRunID = record.RunID
	status.LastEventID = record.EventID
	status.GatewayLocal.Live = true
	status.GatewayLocal.ReplayAvailable = true
	status.GatewayLocal.ReplayUnavailable = false
	return status
}

func replayUnavailableMessage(command domain.StreamChatEventsCommand, runID string, epoch string) grpcapi.StreamChatEventsMessage {
	return grpcapi.StreamChatEventsMessage{
		SessionGroupID: command.SessionGroupID,
		ReplayNotice: &domain.ChatReplayNotice{
			Code:         domain.ChatReplayNoticeUnavailableAfterRestart,
			Message:      "replay is unavailable in this gateway process",
			ProcessEpoch: epoch,
		},
	}
}

func replayNoticeMessage(command domain.StreamChatEventsCommand, code domain.ChatReplayNoticeCode, replay chatstate.ReplayResult, epoch string) grpcapi.StreamChatEventsMessage {
	return grpcapi.StreamChatEventsMessage{
		SessionGroupID: command.SessionGroupID,
		ReplayNotice: &domain.ChatReplayNotice{
			Code:                      code,
			Message:                   chatReplayNoticeMessage(code),
			OldestBufferedEventID:     replay.OldestBufferedEventID,
			NewestBufferedEventID:     replay.NewestBufferedEventID,
			FromStartAvailable:        replay.FromStartAvailable,
			StartEvictedBeforeEventID: replay.StartEvictedBeforeEventID,
			ProcessEpoch:              epoch,
		},
	}
}

func narrowedStreamMessage(command domain.StreamChatEventsCommand, message string) grpcapi.StreamChatEventsMessage {
	return grpcapi.StreamChatEventsMessage{
		SessionGroupID: command.SessionGroupID,
		Narrowed: &domain.ChatNarrowedOutcome{
			Reason:         domain.ReasonReplayUnavailable,
			DisplayMessage: message,
			Retryable:      true,
		},
	}
}

func chatReplayNoticeMessage(code domain.ChatReplayNoticeCode) string {
	switch code {
	case domain.ChatReplayNoticeStartEvicted:
		return "the beginning of the chat event log is no longer buffered"
	case domain.ChatReplayNoticeCursorEvicted:
		return "the requested chat event cursor is no longer buffered"
	case domain.ChatReplayNoticeNarrowedToLive:
		return "chat event replay is narrowed to live events"
	default:
		return "chat event replay is unavailable in this gateway process"
	}
}
