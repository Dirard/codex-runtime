package grpcapi

import (
	"context"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

type ControlServices struct {
	SessionGroups SessionGroupResolver
	Tasks         TaskService
	Pending       PendingService
}

type TaskService interface {
	StartTask(context.Context, domain.StartTaskCommand) (domain.StartTaskResponse, error)
	StreamTask(context.Context, domain.StreamTaskCommand) (TaskStream, error)
	InterruptTask(context.Context, domain.InterruptTaskCommand) (domain.InterruptTaskResponse, error)
	GetTaskStatus(context.Context, domain.GetTaskStatusCommand) (domain.GetTaskStatusResponse, error)
}

type PendingService interface {
	ResolvePendingRequestSession(context.Context, string, string) (domain.SessionGroupMetadata, error)
	RespondPendingRequest(context.Context, domain.RespondPendingRequestCommand) (domain.RespondPendingRequestResponse, error)
}

type ChatRuntimeService interface {
	StartChatRun(context.Context, domain.StartChatRunCommand) (domain.StartChatRunResponse, error)
	GetChat(context.Context, domain.GetChatCommand) (domain.GetChatResponse, error)
	RunChatTurn(context.Context, domain.RunChatTurnCommand) (domain.RunChatTurnResponse, error)
	GetChatStatus(context.Context, domain.GetChatStatusCommand) (domain.GetChatStatusResponse, error)
	GetChatHistory(context.Context, domain.GetChatHistoryCommand) (domain.GetChatHistoryResponse, error)
	StreamChatEvents(context.Context, domain.StreamChatEventsCommand) (ChatEventStream, error)
	RespondChatPending(context.Context, domain.RespondChatPendingCommand) (domain.RespondChatPendingResponse, error)
	InterruptChatRun(context.Context, domain.InterruptChatRunCommand) (domain.InterruptChatRunResponse, error)
}

type ChatEventStream interface {
	Next(context.Context) (StreamChatEventsMessage, error)
	Close() error
}

type StreamChatEventsMessage struct {
	SessionGroupID string
	Event          *domain.ChatEvent
	ReplayNotice   *domain.ChatReplayNotice
	Narrowed       *domain.ChatNarrowedOutcome
}

type TaskStream interface {
	Next(context.Context) (StreamTaskMessage, error)
	Close() error
}

type StreamTaskMessage struct {
	SessionGroupID string
	Event          *domain.TaskEvent
	ReplayNotice   *domain.ReplayNotice
}
