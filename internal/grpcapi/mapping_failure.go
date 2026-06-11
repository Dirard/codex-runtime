package grpcapi

import (
	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"github.com/Dirard/codex-runtime/internal/domain"
)

// MappingFailure describes why a domain value could not be mapped to proto.
type MappingFailure struct {
	Reason  domain.GatewayErrorReason
	Message string
}

func (failure *MappingFailure) Error() string {
	if failure.Message != "" {
		return failure.Message
	}
	return string(failure.Reason)
}

func StartTaskResponseToProtoWithFailure(response domain.StartTaskResponse) (*pb.StartTaskResponse, *MappingFailure) {
	if failure := startTaskResponseMappingFailure(response); failure != nil {
		return nil, failure
	}
	protoResponse, ok := StartTaskResponseToProto(response)
	if !ok {
		return nil, invalidMappingFailure("start task response cannot be mapped to proto")
	}
	return protoResponse, nil
}

func ReplayNoticeToProtoWithFailure(notice domain.ReplayNotice) (*pb.ReplayNotice, *MappingFailure) {
	if failure := replayNoticeMappingFailure(notice); failure != nil {
		return nil, failure
	}
	protoNotice, ok := ReplayNoticeToProto(notice)
	if !ok {
		return nil, invalidMappingFailure("replay notice cannot be mapped to proto")
	}
	return protoNotice, nil
}

func StreamTaskResponseEventToProtoWithFailure(event domain.TaskEvent) (*pb.StreamTaskResponse, *MappingFailure) {
	if failure := taskEventMappingFailure(event); failure != nil {
		return nil, failure
	}
	protoResponse, ok := StreamTaskResponseEventToProto(event)
	if !ok {
		return nil, invalidMappingFailure("stream task response event cannot be mapped to proto")
	}
	return protoResponse, nil
}

func StreamTaskResponseReplayNoticeToProtoWithFailure(notice domain.ReplayNotice) (*pb.StreamTaskResponse, *MappingFailure) {
	if failure := replayNoticeMappingFailure(notice); failure != nil {
		return nil, failure
	}
	protoResponse, ok := StreamTaskResponseReplayNoticeToProto(notice)
	if !ok {
		return nil, invalidMappingFailure("stream task response replay notice cannot be mapped to proto")
	}
	return protoResponse, nil
}

func TaskEventToProtoWithFailure(event domain.TaskEvent) (*pb.TaskEvent, *MappingFailure) {
	if failure := taskEventMappingFailure(event); failure != nil {
		return nil, failure
	}
	protoEvent, ok := TaskEventToProto(event)
	if !ok {
		return nil, invalidMappingFailure("task event cannot be mapped to proto")
	}
	return protoEvent, nil
}

func PendingRequestToProtoWithFailure(request domain.PendingRequest) (*pb.PendingRequest, *MappingFailure) {
	if failure := pendingRequestMappingFailure(request); failure != nil {
		return nil, failure
	}
	protoRequest, ok := PendingRequestToProto(request)
	if !ok {
		return nil, invalidMappingFailure("pending request cannot be mapped to proto")
	}
	return protoRequest, nil
}

func PendingRequestDisplayToProtoWithFailure(display domain.PendingRequestDisplay) (*pb.PendingRequestDisplay, *MappingFailure) {
	if failure := pendingRequestDisplayMappingFailure(display); failure != nil {
		return nil, failure
	}
	protoDisplay, ok := PendingRequestDisplayToProto(display)
	if !ok {
		return nil, invalidMappingFailure("pending request display cannot be mapped to proto")
	}
	return protoDisplay, nil
}

func GetTaskStatusResponseToProtoWithFailure(response domain.GetTaskStatusResponse) (*pb.GetTaskStatusResponse, *MappingFailure) {
	if failure := getTaskStatusResponseMappingFailure(response); failure != nil {
		return nil, failure
	}
	protoResponse, ok := GetTaskStatusResponseToProto(response)
	if !ok {
		return nil, invalidMappingFailure("get task status response cannot be mapped to proto")
	}
	return protoResponse, nil
}

func RespondPendingRequestResponseToProtoWithFailure(response domain.RespondPendingRequestResponse) (*pb.RespondPendingRequestResponse, *MappingFailure) {
	if failure := respondPendingRequestResponseMappingFailure(response); failure != nil {
		return nil, failure
	}
	protoResponse := RespondPendingRequestResponseToProto(response)
	if protoResponse == nil {
		return nil, invalidMappingFailure("respond pending request response cannot be mapped to proto")
	}
	return protoResponse, nil
}

func InterruptTaskResponseToProtoWithFailure(response domain.InterruptTaskResponse) (*pb.InterruptTaskResponse, *MappingFailure) {
	if failure := interruptTaskResponseMappingFailure(response); failure != nil {
		return nil, failure
	}
	protoResponse, ok := InterruptTaskResponseToProto(response)
	if !ok {
		return nil, invalidMappingFailure("interrupt task response cannot be mapped to proto")
	}
	return protoResponse, nil
}

func startTaskResponseMappingFailure(response domain.StartTaskResponse) *MappingFailure {
	if response.TaskID == "" || response.ThreadID == "" || response.TurnID == "" || response.SessionGroupID == "" {
		return invalidMappingFailure("start task response is missing a required id")
	}
	return outboundPublicIDMappingFailure(
		outboundPublicIDCheck{field: "start task response task_id", value: response.TaskID},
		outboundPublicIDCheck{field: "start task response thread_id", value: response.ThreadID},
		outboundPublicIDCheck{field: "start task response turn_id", value: response.TurnID},
		outboundPublicIDCheck{field: "start task response session_group_id", value: response.SessionGroupID},
	)
}

func taskEventMappingFailure(event domain.TaskEvent) *MappingFailure {
	if event.EventID == 0 || event.TaskID == "" || event.SessionGroupID == "" {
		return invalidMappingFailure("task event is missing a required identity field")
	}
	if failure := outboundPublicIDMappingFailure(
		outboundPublicIDCheck{field: "task event task_id", value: event.TaskID},
		outboundPublicIDCheck{field: "task event session_group_id", value: event.SessionGroupID},
		outboundPublicIDCheck{field: "task event workspace_id", value: event.WorkspaceID},
		outboundPublicIDCheck{field: "task event thread_id", value: event.ThreadID},
		outboundPublicIDCheck{field: "task event turn_id", value: event.TurnID},
	); failure != nil {
		return failure
	}
	switch payload := event.Payload.(type) {
	case domain.TaskLifecycleEvent:
		return taskLifecycleEventMappingFailure(payload)
	case *domain.TaskLifecycleEvent:
		if payload == nil {
			return nil
		}
		return taskLifecycleEventMappingFailure(*payload)
	case domain.AssistantDeltaEvent:
		return assistantDeltaEventMappingFailure(payload)
	case *domain.AssistantDeltaEvent:
		if payload == nil {
			return nil
		}
		return assistantDeltaEventMappingFailure(*payload)
	case domain.AssistantMessageCompletedEvent:
		return assistantMessageCompletedEventMappingFailure(payload)
	case *domain.AssistantMessageCompletedEvent:
		if payload == nil {
			return nil
		}
		return assistantMessageCompletedEventMappingFailure(*payload)
	case domain.PlanUpdatedEvent:
		return planUpdatedMappingFailure(payload)
	case *domain.PlanUpdatedEvent:
		if payload == nil {
			return nil
		}
		return planUpdatedMappingFailure(*payload)
	case domain.ToolProgressEvent:
		return toolProgressEventMappingFailure(payload)
	case *domain.ToolProgressEvent:
		if payload == nil {
			return nil
		}
		return toolProgressEventMappingFailure(*payload)
	case domain.CommandStartedEvent:
		return commandStartedEventMappingFailure(payload)
	case *domain.CommandStartedEvent:
		if payload == nil {
			return nil
		}
		return commandStartedEventMappingFailure(*payload)
	case domain.CommandOutputDeltaEvent:
		return commandOutputDeltaEventMappingFailure(payload)
	case *domain.CommandOutputDeltaEvent:
		if payload == nil {
			return nil
		}
		return commandOutputDeltaEventMappingFailure(*payload)
	case domain.FileDiffUpdatedEvent:
		return fileDiffUpdatedEventMappingFailure(payload)
	case *domain.FileDiffUpdatedEvent:
		if payload == nil {
			return nil
		}
		return fileDiffUpdatedEventMappingFailure(*payload)
	case domain.TurnDiffUpdatedEvent:
		return turnDiffUpdatedEventMappingFailure(payload)
	case *domain.TurnDiffUpdatedEvent:
		if payload == nil {
			return nil
		}
		return turnDiffUpdatedEventMappingFailure(*payload)
	case domain.PendingRequestCreatedEvent:
		return pendingRequestCreatedEventMappingFailure(payload)
	case *domain.PendingRequestCreatedEvent:
		if payload == nil {
			return nil
		}
		return pendingRequestCreatedEventMappingFailure(*payload)
	case domain.PendingRequestResolvedEvent:
		return pendingRequestResolvedEventMappingFailure(payload)
	case *domain.PendingRequestResolvedEvent:
		if payload == nil {
			return nil
		}
		return pendingRequestResolvedEventMappingFailure(*payload)
	case domain.TaskTerminalEvent:
		return taskTerminalEventMappingFailure(payload)
	case *domain.TaskTerminalEvent:
		if payload == nil {
			return nil
		}
		return taskTerminalEventMappingFailure(*payload)
	case domain.GatewayWarningEvent:
		return gatewayWarningEventMappingFailure(payload)
	case *domain.GatewayWarningEvent:
		if payload == nil {
			return nil
		}
		return gatewayWarningEventMappingFailure(*payload)
	case domain.UnknownRawEvent:
		return unknownRawEventMappingFailure(payload)
	case *domain.UnknownRawEvent:
		if payload == nil {
			return nil
		}
		return unknownRawEventMappingFailure(*payload)
	default:
		return nil
	}
}

func invalidMappingFailure(message string) *MappingFailure {
	return &MappingFailure{
		Reason:  domain.ReasonInternalGatewayError,
		Message: message,
	}
}

func resourceExhaustedMappingFailure(message string) *MappingFailure {
	return &MappingFailure{
		Reason:  domain.ReasonResourceExhausted,
		Message: message,
	}
}
