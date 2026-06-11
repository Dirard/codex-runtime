package grpcapi

import "github.com/Dirard/codex-runtime/internal/domain"

func pendingRequestMappingFailure(request domain.PendingRequest) *MappingFailure {
	if request.PendingRequestID == "" || request.TaskID == "" {
		return invalidMappingFailure("pending request is missing a required identity field")
	}
	if failure := outboundPublicIDMappingFailure(
		outboundPublicIDCheck{field: "pending request pending_request_id", value: request.PendingRequestID},
		outboundPublicIDCheck{field: "pending request task_id", value: request.TaskID},
		outboundPublicIDCheck{field: "pending request thread_id", value: request.ThreadID},
		outboundPublicIDCheck{field: "pending request turn_id", value: request.TurnID},
		outboundPublicIDCheck{field: "pending request item_id", value: request.ItemID},
	); failure != nil {
		return failure
	}
	if _, ok := PendingTypeToProto(request.PendingType); !ok {
		return invalidMappingFailure("pending request has invalid pending_type")
	}
	return pendingRequestDisplayMappingFailure(request.Display)
}

func getTaskStatusResponseMappingFailure(response domain.GetTaskStatusResponse) *MappingFailure {
	if response.TaskID == "" || response.SessionGroupID == "" {
		return invalidMappingFailure("get task status response is missing a required identity field")
	}
	if failure := outboundPublicIDMappingFailure(
		outboundPublicIDCheck{field: "get task status response task_id", value: response.TaskID},
		outboundPublicIDCheck{field: "get task status response session_group_id", value: response.SessionGroupID},
		outboundPublicIDCheck{field: "get task status response workspace_id", value: response.WorkspaceID},
		outboundPublicIDCheck{field: "get task status response thread_id", value: response.ThreadID},
		outboundPublicIDCheck{field: "get task status response turn_id", value: response.TurnID},
	); failure != nil {
		return failure
	}
	if _, ok := TaskStateToProto(response.State); !ok {
		return invalidMappingFailure("get task status response has invalid state")
	}
	if len(response.ActivePendingRequests) > domain.MaxActivePendingRequests {
		return resourceExhaustedMappingFailure("active pending request count exceeds outbound cap")
	}
	for _, pending := range response.ActivePendingRequests {
		if pending.TaskID != response.TaskID {
			return invalidMappingFailure("active pending request task_id does not match status response task_id")
		}
		if failure := pendingRequestMappingFailure(pending); failure != nil {
			return failure
		}
	}
	if response.Terminal != nil {
		if failure := taskTerminalEventMappingFailure(*response.Terminal); failure != nil {
			return failure
		}
	}
	return nil
}

func respondPendingRequestResponseMappingFailure(response domain.RespondPendingRequestResponse) *MappingFailure {
	if response.TaskID == "" || response.SessionGroupID == "" || response.PendingRequestID == "" || response.ClientResponseID == "" {
		return invalidMappingFailure("respond pending request response is missing a required identity field")
	}
	return outboundPublicIDMappingFailure(
		outboundPublicIDCheck{field: "respond pending request response task_id", value: response.TaskID},
		outboundPublicIDCheck{field: "respond pending request response session_group_id", value: response.SessionGroupID},
		outboundPublicIDCheck{field: "respond pending request response pending_request_id", value: response.PendingRequestID},
		outboundPublicIDCheck{field: "respond pending request response client_response_id", value: response.ClientResponseID},
	)
}

func interruptTaskResponseMappingFailure(response domain.InterruptTaskResponse) *MappingFailure {
	if response.TaskID == "" || response.SessionGroupID == "" {
		return invalidMappingFailure("interrupt task response is missing a required identity field")
	}
	if failure := outboundPublicIDMappingFailure(
		outboundPublicIDCheck{field: "interrupt task response task_id", value: response.TaskID},
		outboundPublicIDCheck{field: "interrupt task response session_group_id", value: response.SessionGroupID},
		outboundPublicIDCheck{field: "interrupt task response thread_id", value: response.ThreadID},
		outboundPublicIDCheck{field: "interrupt task response turn_id", value: response.TurnID},
	); failure != nil {
		return failure
	}
	if _, ok := TaskStateToProto(response.State); !ok {
		return invalidMappingFailure("interrupt task response has invalid state")
	}
	return nil
}
