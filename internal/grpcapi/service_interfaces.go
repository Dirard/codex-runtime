package grpcapi

import (
	"context"

	"github.com/Dirard/codex-runtime/internal/domain"
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

type TaskStream interface {
	Next(context.Context) (StreamTaskMessage, error)
	Close() error
}

type StreamTaskMessage struct {
	SessionGroupID string
	Event          *domain.TaskEvent
	ReplayNotice   *domain.ReplayNotice
}
