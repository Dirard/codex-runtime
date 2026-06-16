package grpcapi

import (
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
)

func StartTaskResponseToProto(response domain.StartTaskResponse) (*pb.StartTaskResponse, bool) {
	if startTaskResponseMappingFailure(response) != nil {
		return nil, false
	}
	state, ok := TaskStateToProto(response.State)
	if !ok {
		return nil, false
	}
	return &pb.StartTaskResponse{
		TaskId:         response.TaskID,
		ThreadId:       response.ThreadID,
		TurnId:         response.TurnID,
		SessionGroupId: response.SessionGroupID,
		State:          state,
		LastEventId:    response.LastEventID,
	}, true
}

func ReplayNoticeToProto(notice domain.ReplayNotice) (*pb.ReplayNotice, bool) {
	if replayNoticeMappingFailure(notice) != nil {
		return nil, false
	}
	code, ok := ReplayNoticeCodeToProto(notice.Code)
	if !ok {
		return nil, false
	}
	return &pb.ReplayNotice{
		Code:                      code,
		Message:                   notice.Message,
		OldestBufferedEventId:     notice.OldestBufferedEventID,
		NewestBufferedEventId:     notice.NewestBufferedEventID,
		FromStartAvailable:        notice.FromStartAvailable,
		StartEvictedBeforeEventId: notice.StartEvictedBeforeEventID,
	}, true
}

func StreamTaskResponseEventToProto(event domain.TaskEvent) (*pb.StreamTaskResponse, bool) {
	protoEvent, ok := TaskEventToProto(event)
	if !ok {
		return nil, false
	}
	return &pb.StreamTaskResponse{
		Payload: &pb.StreamTaskResponse_Event{Event: protoEvent},
	}, true
}

func StreamTaskResponseReplayNoticeToProto(notice domain.ReplayNotice) (*pb.StreamTaskResponse, bool) {
	protoNotice, ok := ReplayNoticeToProto(notice)
	if !ok {
		return nil, false
	}
	return &pb.StreamTaskResponse{
		Payload: &pb.StreamTaskResponse_ReplayNotice{ReplayNotice: protoNotice},
	}, true
}

func TaskEventToProto(event domain.TaskEvent) (*pb.TaskEvent, bool) {
	if taskEventMappingFailure(event) != nil {
		return nil, false
	}
	protoEvent := &pb.TaskEvent{
		EventId:         event.EventID,
		TaskId:          event.TaskID,
		SessionGroupId:  event.SessionGroupID,
		WorkspaceId:     event.WorkspaceID,
		ThreadId:        event.ThreadID,
		TurnId:          event.TurnID,
		CreatedAtUnixMs: event.CreatedAtUnixMS,
	}
	if !populateTaskEventPayload(protoEvent, event.Payload) {
		return nil, false
	}
	return protoEvent, true
}

func populateTaskEventPayload(protoEvent *pb.TaskEvent, eventPayload domain.TaskEventPayload) bool {
	switch payload := eventPayload.(type) {
	case domain.TaskLifecycleEvent:
		lifecycle, ok := taskLifecycleEventToProto(payload)
		if !ok {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_Lifecycle{Lifecycle: lifecycle}
	case *domain.TaskLifecycleEvent:
		if payload == nil {
			return false
		}
		lifecycle, ok := taskLifecycleEventToProto(*payload)
		if !ok {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_Lifecycle{Lifecycle: lifecycle}
	case domain.AssistantDeltaEvent:
		protoEvent.Payload = &pb.TaskEvent_AssistantDelta{AssistantDelta: &pb.AssistantDeltaEvent{
			TextDelta: payload.TextDelta,
			Truncated: payload.Truncated,
		}}
	case *domain.AssistantDeltaEvent:
		if payload == nil {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_AssistantDelta{AssistantDelta: &pb.AssistantDeltaEvent{
			TextDelta: payload.TextDelta,
			Truncated: payload.Truncated,
		}}
	case domain.AssistantMessageCompletedEvent:
		protoEvent.Payload = &pb.TaskEvent_AssistantMessageCompleted{AssistantMessageCompleted: &pb.AssistantMessageCompletedEvent{
			Message:   payload.Message,
			Truncated: payload.Truncated,
		}}
	case *domain.AssistantMessageCompletedEvent:
		if payload == nil {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_AssistantMessageCompleted{AssistantMessageCompleted: &pb.AssistantMessageCompletedEvent{
			Message:   payload.Message,
			Truncated: payload.Truncated,
		}}
	case domain.PlanUpdatedEvent:
		planUpdated, ok := planUpdatedEventToProto(payload)
		if !ok {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_PlanUpdated{PlanUpdated: planUpdated}
	case *domain.PlanUpdatedEvent:
		if payload == nil {
			return false
		}
		planUpdated, ok := planUpdatedEventToProto(*payload)
		if !ok {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_PlanUpdated{PlanUpdated: planUpdated}
	case domain.ToolProgressEvent:
		toolProgress, ok := toolProgressEventToProto(payload)
		if !ok {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_ToolProgress{ToolProgress: toolProgress}
	case *domain.ToolProgressEvent:
		if payload == nil {
			return false
		}
		toolProgress, ok := toolProgressEventToProto(*payload)
		if !ok {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_ToolProgress{ToolProgress: toolProgress}
	case domain.CommandStartedEvent:
		protoEvent.Payload = &pb.TaskEvent_CommandStarted{CommandStarted: &pb.CommandStartedEvent{
			ItemId:         payload.ItemID,
			CommandDisplay: payload.CommandDisplay,
			WorkspaceLabel: payload.WorkspaceLabel,
		}}
	case *domain.CommandStartedEvent:
		if payload == nil {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_CommandStarted{CommandStarted: &pb.CommandStartedEvent{
			ItemId:         payload.ItemID,
			CommandDisplay: payload.CommandDisplay,
			WorkspaceLabel: payload.WorkspaceLabel,
		}}
	case domain.CommandOutputDeltaEvent:
		commandOutput, ok := commandOutputDeltaEventToProto(payload)
		if !ok {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_CommandOutputDelta{CommandOutputDelta: commandOutput}
	case *domain.CommandOutputDeltaEvent:
		if payload == nil {
			return false
		}
		commandOutput, ok := commandOutputDeltaEventToProto(*payload)
		if !ok {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_CommandOutputDelta{CommandOutputDelta: commandOutput}
	case domain.FileDiffUpdatedEvent:
		protoEvent.Payload = &pb.TaskEvent_FileDiffUpdated{FileDiffUpdated: &pb.FileDiffUpdatedEvent{
			ItemId:      payload.ItemID,
			FileLabel:   payload.FileLabel,
			ChangeKind:  payload.ChangeKind,
			DiffSummary: payload.DiffSummary,
			DiffUnified: payload.DiffUnified,
			Truncated:   payload.Truncated,
		}}
	case *domain.FileDiffUpdatedEvent:
		if payload == nil {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_FileDiffUpdated{FileDiffUpdated: &pb.FileDiffUpdatedEvent{
			ItemId:      payload.ItemID,
			FileLabel:   payload.FileLabel,
			ChangeKind:  payload.ChangeKind,
			DiffSummary: payload.DiffSummary,
			DiffUnified: payload.DiffUnified,
			Truncated:   payload.Truncated,
		}}
	case domain.TurnDiffUpdatedEvent:
		protoEvent.Payload = &pb.TaskEvent_TurnDiffUpdated{TurnDiffUpdated: &pb.TurnDiffUpdatedEvent{
			DiffSummary: payload.DiffSummary,
			DiffUnified: payload.DiffUnified,
			Truncated:   payload.Truncated,
		}}
	case *domain.TurnDiffUpdatedEvent:
		if payload == nil {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_TurnDiffUpdated{TurnDiffUpdated: &pb.TurnDiffUpdatedEvent{
			DiffSummary: payload.DiffSummary,
			DiffUnified: payload.DiffUnified,
			Truncated:   payload.Truncated,
		}}
	case domain.PendingRequestCreatedEvent:
		pendingCreated, ok := pendingRequestCreatedEventToProto(payload)
		if !ok {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_PendingRequestCreated{PendingRequestCreated: pendingCreated}
	case *domain.PendingRequestCreatedEvent:
		if payload == nil {
			return false
		}
		pendingCreated, ok := pendingRequestCreatedEventToProto(*payload)
		if !ok {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_PendingRequestCreated{PendingRequestCreated: pendingCreated}
	case domain.PendingRequestResolvedEvent:
		pendingResolved, ok := pendingRequestResolvedEventToProto(payload)
		if !ok {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_PendingRequestResolved{PendingRequestResolved: pendingResolved}
	case *domain.PendingRequestResolvedEvent:
		if payload == nil {
			return false
		}
		pendingResolved, ok := pendingRequestResolvedEventToProto(*payload)
		if !ok {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_PendingRequestResolved{PendingRequestResolved: pendingResolved}
	case domain.TaskTerminalEvent:
		terminal, ok := TaskTerminalEventToProto(payload)
		if !ok {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_Terminal{Terminal: terminal}
	case *domain.TaskTerminalEvent:
		if payload == nil {
			return false
		}
		terminal, ok := TaskTerminalEventToProto(*payload)
		if !ok {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_Terminal{Terminal: terminal}
	case domain.GatewayWarningEvent:
		protoEvent.Payload = &pb.TaskEvent_GatewayWarning{GatewayWarning: &pb.GatewayWarningEvent{
			Code:           payload.Code,
			Message:        payload.Message,
			RequestType:    payload.RequestType,
			AutoResolution: payload.AutoResolution,
			LimitReason:    payload.LimitReason,
		}}
	case *domain.GatewayWarningEvent:
		if payload == nil {
			return false
		}
		protoEvent.Payload = &pb.TaskEvent_GatewayWarning{GatewayWarning: &pb.GatewayWarningEvent{
			Code:           payload.Code,
			Message:        payload.Message,
			RequestType:    payload.RequestType,
			AutoResolution: payload.AutoResolution,
			LimitReason:    payload.LimitReason,
		}}
	default:
		return false
	}
	return true
}

func taskLifecycleEventToProto(event domain.TaskLifecycleEvent) (*pb.TaskLifecycleEvent, bool) {
	lifecycleEvent, ok := TaskLifecycleEventTypeToProto(event.LifecycleEvent)
	if !ok {
		return nil, false
	}
	state, ok := TaskStateToProto(event.State)
	if !ok {
		return nil, false
	}
	return &pb.TaskLifecycleEvent{
		LifecycleEvent: lifecycleEvent,
		State:          state,
		ReasonCode:     event.ReasonCode,
		DisplayMessage: event.DisplayMessage,
	}, true
}

func toolProgressEventToProto(event domain.ToolProgressEvent) (*pb.ToolProgressEvent, bool) {
	state, ok := ToolStateToProto(event.State)
	if !ok {
		return nil, false
	}
	return &pb.ToolProgressEvent{
		ItemId:   event.ItemID,
		ToolName: event.ToolName,
		State:    state,
		Summary:  event.Summary,
	}, true
}

func commandOutputDeltaEventToProto(event domain.CommandOutputDeltaEvent) (*pb.CommandOutputDeltaEvent, bool) {
	stream, ok := CommandOutputStreamToProto(event.Stream)
	if !ok {
		return nil, false
	}
	return &pb.CommandOutputDeltaEvent{
		ItemId:    event.ItemID,
		Stream:    stream,
		Delta:     event.Delta,
		Truncated: event.Truncated,
	}, true
}

func pendingRequestCreatedEventToProto(event domain.PendingRequestCreatedEvent) (*pb.PendingRequestCreatedEvent, bool) {
	pendingType, ok := PendingTypeToProto(event.PendingType)
	if !ok {
		return nil, false
	}
	display, ok := PendingRequestDisplayToProto(event.Display)
	if !ok {
		return nil, false
	}
	return &pb.PendingRequestCreatedEvent{
		PendingRequestId: event.PendingRequestID,
		PendingType:      pendingType,
		Display:          display,
	}, true
}

func pendingRequestResolvedEventToProto(event domain.PendingRequestResolvedEvent) (*pb.PendingRequestResolvedEvent, bool) {
	pendingType, ok := PendingTypeToProto(event.PendingType)
	if !ok {
		return nil, false
	}
	resolution, ok := PendingResolutionToProto(event.Resolution)
	if !ok {
		return nil, false
	}
	return &pb.PendingRequestResolvedEvent{
		PendingRequestId: event.PendingRequestID,
		PendingType:      pendingType,
		Resolution:       resolution,
		DisplayMessage:   event.DisplayMessage,
	}, true
}

func planUpdatedEventToProto(event domain.PlanUpdatedEvent) (*pb.PlanUpdatedEvent, bool) {
	if len(event.Steps) > domain.MaxPlanSteps {
		return nil, false
	}
	steps := make([]*pb.PlanStep, 0, len(event.Steps))
	for _, step := range event.Steps {
		steps = append(steps, &pb.PlanStep{
			Step:   step.Step,
			Status: step.Status,
		})
	}
	return &pb.PlanUpdatedEvent{
		Explanation: event.Explanation,
		Steps:       steps,
	}, true
}

func TaskTerminalEventToProto(event domain.TaskTerminalEvent) (*pb.TaskTerminalEvent, bool) {
	terminalState, ok := TerminalStateToProto(event.TerminalState)
	if !ok {
		return nil, false
	}
	return &pb.TaskTerminalEvent{
		TerminalState: terminalState,
		ResultSummary: event.ResultSummary,
		ErrorMessage:  event.ErrorMessage,
	}, true
}

func PendingRequestToProto(request domain.PendingRequest) (*pb.PendingRequest, bool) {
	if pendingRequestMappingFailure(request) != nil {
		return nil, false
	}
	pendingType, ok := PendingTypeToProto(request.PendingType)
	if !ok {
		return nil, false
	}
	display, ok := PendingRequestDisplayToProto(request.Display)
	if !ok {
		return nil, false
	}
	return &pb.PendingRequest{
		PendingRequestId: request.PendingRequestID,
		TaskId:           request.TaskID,
		PendingType:      pendingType,
		CreatedAtUnixMs:  request.CreatedAtUnixMS,
		ThreadId:         request.ThreadID,
		TurnId:           request.TurnID,
		ItemId:           request.ItemID,
		Display:          display,
	}, true
}

func PendingRequestDisplayToProto(display domain.PendingRequestDisplay) (*pb.PendingRequestDisplay, bool) {
	if pendingRequestDisplayMappingFailure(display) != nil {
		return nil, false
	}
	protoDisplay := &pb.PendingRequestDisplay{}
	switch payload := display.(type) {
	case domain.CommandApprovalDisplay:
		commandApproval, ok := commandApprovalDisplayToProto(payload)
		if !ok {
			return nil, false
		}
		protoDisplay.Payload = &pb.PendingRequestDisplay_CommandApproval{CommandApproval: commandApproval}
	case *domain.CommandApprovalDisplay:
		if payload == nil {
			return nil, false
		}
		commandApproval, ok := commandApprovalDisplayToProto(*payload)
		if !ok {
			return nil, false
		}
		protoDisplay.Payload = &pb.PendingRequestDisplay_CommandApproval{CommandApproval: commandApproval}
	case domain.FileChangeApprovalDisplay:
		fileChangeApproval, ok := fileChangeApprovalDisplayToProto(payload)
		if !ok {
			return nil, false
		}
		protoDisplay.Payload = &pb.PendingRequestDisplay_FileChangeApproval{FileChangeApproval: fileChangeApproval}
	case *domain.FileChangeApprovalDisplay:
		if payload == nil {
			return nil, false
		}
		fileChangeApproval, ok := fileChangeApprovalDisplayToProto(*payload)
		if !ok {
			return nil, false
		}
		protoDisplay.Payload = &pb.PendingRequestDisplay_FileChangeApproval{FileChangeApproval: fileChangeApproval}
	case domain.PermissionsApprovalDisplay:
		permissionsApproval, ok := permissionsApprovalDisplayToProto(payload)
		if !ok {
			return nil, false
		}
		protoDisplay.Payload = &pb.PendingRequestDisplay_PermissionsApproval{PermissionsApproval: permissionsApproval}
	case *domain.PermissionsApprovalDisplay:
		if payload == nil {
			return nil, false
		}
		permissionsApproval, ok := permissionsApprovalDisplayToProto(*payload)
		if !ok {
			return nil, false
		}
		protoDisplay.Payload = &pb.PendingRequestDisplay_PermissionsApproval{PermissionsApproval: permissionsApproval}
	case domain.McpElicitationDisplay:
		mcpElicitation, ok := mcpElicitationDisplayToProto(payload)
		if !ok {
			return nil, false
		}
		protoDisplay.Payload = &pb.PendingRequestDisplay_McpElicitation{McpElicitation: mcpElicitation}
	case *domain.McpElicitationDisplay:
		if payload == nil {
			return nil, false
		}
		mcpElicitation, ok := mcpElicitationDisplayToProto(*payload)
		if !ok {
			return nil, false
		}
		protoDisplay.Payload = &pb.PendingRequestDisplay_McpElicitation{McpElicitation: mcpElicitation}
	case domain.ToolUserInputDisplay:
		toolUserInput, ok := toolUserInputDisplayToProto(payload)
		if !ok {
			return nil, false
		}
		protoDisplay.Payload = &pb.PendingRequestDisplay_ToolUserInput{ToolUserInput: toolUserInput}
	case *domain.ToolUserInputDisplay:
		if payload == nil {
			return nil, false
		}
		toolUserInput, ok := toolUserInputDisplayToProto(*payload)
		if !ok {
			return nil, false
		}
		protoDisplay.Payload = &pb.PendingRequestDisplay_ToolUserInput{ToolUserInput: toolUserInput}
	default:
		return nil, false
	}
	return protoDisplay, true
}

func commandApprovalDisplayToProto(display domain.CommandApprovalDisplay) (*pb.CommandApprovalDisplay, bool) {
	decisionOptions, ok := approvalDecisionOptionsToProto(display.DecisionOptions)
	if !ok {
		return nil, false
	}
	approvalSecurity, ok := approvalSecurityMetadataToProto(display.ApprovalSecurity)
	if !ok {
		return nil, false
	}
	return &pb.CommandApprovalDisplay{
		CommandDisplay:   display.CommandDisplay,
		WorkspaceLabel:   display.WorkspaceLabel,
		Reason:           display.Reason,
		ApprovalSecurity: approvalSecurity,
		DecisionOptions:  decisionOptions,
	}, true
}

func fileChangeApprovalDisplayToProto(display domain.FileChangeApprovalDisplay) (*pb.FileChangeApprovalDisplay, bool) {
	decisionOptions, ok := approvalDecisionOptionsToProto(display.DecisionOptions)
	if !ok {
		return nil, false
	}
	return &pb.FileChangeApprovalDisplay{
		FileLabel:       display.FileLabel,
		ChangeKind:      display.ChangeKind,
		DiffSummary:     display.DiffSummary,
		DiffUnified:     display.DiffUnified,
		DiffUnavailable: display.DiffUnavailable,
		GrantRoot:       fileGrantRootDisplayToProto(display.GrantRoot),
		DecisionOptions: decisionOptions,
	}, true
}

func permissionsApprovalDisplayToProto(display domain.PermissionsApprovalDisplay) (*pb.PermissionsApprovalDisplay, bool) {
	recommendedScope := pb.PermissionScope_PERMISSION_SCOPE_UNSPECIFIED
	if display.RecommendedScope != "" {
		var ok bool
		recommendedScope, ok = PermissionScopeToProto(display.RecommendedScope)
		if !ok {
			return nil, false
		}
	}
	if len(display.RequestedPermissions) > domain.MaxPermissionAtoms {
		return nil, false
	}
	permissions := make([]*pb.PermissionAtom, 0, len(display.RequestedPermissions))
	for _, permission := range display.RequestedPermissions {
		permissions = append(permissions, &pb.PermissionAtom{
			PermissionId:      permission.PermissionID,
			Kind:              permission.Kind,
			DisplayLabel:      permission.DisplayLabel,
			ScopeLabel:        permission.ScopeLabel,
			Grantable:         permission.Grantable,
			UngrantableReason: permission.UngrantableReason,
		})
	}
	return &pb.PermissionsApprovalDisplay{
		RequestedPermissions: permissions,
		RecommendedScope:     recommendedScope,
		Reason:               display.Reason,
	}, true
}

func mcpElicitationDisplayToProto(display domain.McpElicitationDisplay) (*pb.McpElicitationDisplay, bool) {
	mode, ok := ElicitationModeToProto(display.Mode)
	if !ok {
		return nil, false
	}
	return &pb.McpElicitationDisplay{
		Mode:           mode,
		Message:        display.Message,
		FormSchemaJson: display.FormSchemaJSON,
		Url:            display.URL,
		SubmitLabel:    display.SubmitLabel,
	}, true
}

func toolUserInputDisplayToProto(display domain.ToolUserInputDisplay) (*pb.ToolUserInputDisplay, bool) {
	if len(display.Questions) > domain.MaxToolUserInputQuestions {
		return nil, false
	}
	questions := make([]*pb.ToolUserInputQuestion, 0, len(display.Questions))
	for _, question := range display.Questions {
		if len(question.Options) > domain.MaxToolUserInputOptionsPerQuestion {
			return nil, false
		}
		questions = append(questions, &pb.ToolUserInputQuestion{
			Id:       question.ID,
			Header:   question.Header,
			Question: question.Question,
			IsOther:  question.IsOther,
			IsSecret: question.IsSecret,
			Options:  append([]string(nil), question.Options...),
		})
	}
	return &pb.ToolUserInputDisplay{Questions: questions}, true
}

func approvalSecurityMetadataToProto(metadata *domain.ApprovalSecurityMetadata) (*pb.ApprovalSecurityMetadata, bool) {
	if metadata == nil {
		return nil, true
	}
	entries := len(metadata.AdditionalFilesystemEntries) + len(metadata.NetworkPolicyAmendmentSummaries)
	if entries > domain.MaxApprovalSecurityMetadataEntries {
		return nil, false
	}
	return &pb.ApprovalSecurityMetadata{
		HasPrivilegeExpansion:           metadata.HasPrivilegeExpansion,
		NetworkContext:                  networkContextDisplayToProto(metadata.NetworkContext),
		AdditionalNetwork:               additionalNetworkDisplayToProto(metadata.AdditionalNetwork),
		AdditionalFilesystemEntries:     additionalFilesystemEntriesToProto(metadata.AdditionalFilesystemEntries),
		ExecpolicyAmendmentSummary:      execPolicyAmendmentSummaryToProto(metadata.ExecPolicyAmendmentSummary),
		NetworkPolicyAmendmentSummaries: networkPolicyAmendmentSummariesToProto(metadata.NetworkPolicyAmendmentSummaries),
		BlockingReason:                  metadata.BlockingReason,
	}, true
}

func networkContextDisplayToProto(display *domain.NetworkContextDisplay) *pb.NetworkContextDisplay {
	if display == nil {
		return nil
	}
	return &pb.NetworkContextDisplay{
		HostLabel: display.HostLabel,
		Protocol:  display.Protocol,
	}
}

func additionalNetworkDisplayToProto(display *domain.AdditionalNetworkDisplay) *pb.AdditionalNetworkDisplay {
	if display == nil {
		return nil
	}
	return &pb.AdditionalNetworkDisplay{Enabled: display.Enabled}
}

func additionalFilesystemEntriesToProto(entries []domain.AdditionalFilesystemEntry) []*pb.AdditionalFilesystemEntry {
	protoEntries := make([]*pb.AdditionalFilesystemEntry, 0, len(entries))
	for _, entry := range entries {
		protoEntries = append(protoEntries, &pb.AdditionalFilesystemEntry{
			EntryId:            entry.EntryID,
			Access:             entry.Access,
			PathLabel:          entry.PathLabel,
			Approvable:         entry.Approvable,
			UnapprovableReason: entry.UnapprovableReason,
		})
	}
	return protoEntries
}

func execPolicyAmendmentSummaryToProto(summary *domain.ExecPolicyAmendmentSummary) *pb.ExecPolicyAmendmentSummary {
	if summary == nil {
		return nil
	}
	return &pb.ExecPolicyAmendmentSummary{
		CommandDisplay: summary.CommandDisplay,
		Truncated:      summary.Truncated,
	}
}

func networkPolicyAmendmentSummariesToProto(summaries []domain.NetworkPolicyAmendmentSummary) []*pb.NetworkPolicyAmendmentSummary {
	protoSummaries := make([]*pb.NetworkPolicyAmendmentSummary, 0, len(summaries))
	for _, summary := range summaries {
		protoSummaries = append(protoSummaries, &pb.NetworkPolicyAmendmentSummary{
			HostLabel:  summary.HostLabel,
			Action:     summary.Action,
			Approvable: summary.Approvable,
		})
	}
	return protoSummaries
}

func fileGrantRootDisplayToProto(display *domain.FileGrantRootDisplay) *pb.FileGrantRootDisplay {
	if display == nil {
		return nil
	}
	return &pb.FileGrantRootDisplay{
		Present:            display.Present,
		RootLabel:          display.RootLabel,
		UnderConfiguredCwd: display.UnderConfiguredCWD,
		Approvable:         display.Approvable,
		UnapprovableReason: display.UnapprovableReason,
	}
}

func approvalDecisionOptionsToProto(options []domain.ApprovalDecisionOption) ([]*pb.ApprovalDecisionOption, bool) {
	if len(options) > domain.MaxApprovalDecisionOptions {
		return nil, false
	}
	protoOptions := make([]*pb.ApprovalDecisionOption, 0, len(options))
	for _, option := range options {
		wireDecision := pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_UNSPECIFIED
		if option.WireDecision == "" {
			if option.Selectable {
				return nil, false
			}
		} else {
			var ok bool
			wireDecision, ok = ApprovalWireDecisionToProto(option.WireDecision)
			if !ok {
				return nil, false
			}
		}
		protoOptions = append(protoOptions, &pb.ApprovalDecisionOption{
			DecisionId:        option.DecisionID,
			WireDecision:      wireDecision,
			DisplayLabel:      option.DisplayLabel,
			Summary:           option.Summary,
			Selectable:        option.Selectable,
			UnsupportedReason: option.UnsupportedReason,
		})
	}
	return protoOptions, true
}

func GetTaskStatusResponseToProto(response domain.GetTaskStatusResponse) (*pb.GetTaskStatusResponse, bool) {
	if getTaskStatusResponseMappingFailure(response) != nil {
		return nil, false
	}
	state, ok := TaskStateToProto(response.State)
	if !ok {
		return nil, false
	}
	activePending := make([]*pb.PendingRequest, 0, len(response.ActivePendingRequests))
	for _, pending := range response.ActivePendingRequests {
		protoPending, ok := PendingRequestToProto(pending)
		if !ok {
			return nil, false
		}
		activePending = append(activePending, protoPending)
	}
	var terminal *pb.TaskTerminalEvent
	if response.Terminal != nil {
		protoTerminal, ok := TaskTerminalEventToProto(*response.Terminal)
		if !ok {
			return nil, false
		}
		terminal = protoTerminal
	}
	return &pb.GetTaskStatusResponse{
		TaskId:                    response.TaskID,
		State:                     state,
		SessionGroupId:            response.SessionGroupID,
		WorkspaceId:               response.WorkspaceID,
		ThreadId:                  response.ThreadID,
		TurnId:                    response.TurnID,
		LastEventId:               response.LastEventID,
		OldestBufferedEventId:     response.OldestBufferedEventID,
		NewestBufferedEventId:     response.NewestBufferedEventID,
		FromStartAvailable:        response.FromStartAvailable,
		StartEvictedBeforeEventId: response.StartEvictedBeforeEventID,
		ActivePendingRequests:     activePending,
		Terminal:                  terminal,
	}, true
}

func RespondPendingRequestResponseToProto(response domain.RespondPendingRequestResponse) *pb.RespondPendingRequestResponse {
	if respondPendingRequestResponseMappingFailure(response) != nil {
		return nil
	}
	return &pb.RespondPendingRequestResponse{
		TaskId:           response.TaskID,
		PendingRequestId: response.PendingRequestID,
		ClientResponseId: response.ClientResponseID,
		Accepted:         response.Accepted,
		AlreadyApplied:   response.AlreadyApplied,
		ResolvedEventId:  response.ResolvedEventID,
	}
}

func InterruptTaskResponseToProto(response domain.InterruptTaskResponse) (*pb.InterruptTaskResponse, bool) {
	if interruptTaskResponseMappingFailure(response) != nil {
		return nil, false
	}
	state, ok := TaskStateToProto(response.State)
	if !ok {
		return nil, false
	}
	return &pb.InterruptTaskResponse{
		TaskId:                response.TaskID,
		SessionGroupId:        response.SessionGroupID,
		ThreadId:              response.ThreadID,
		TurnId:                response.TurnID,
		State:                 state,
		InterruptSent:         response.InterruptSent,
		PreTurnCancelRecorded: response.PreTurnCancelRecorded,
		AlreadyInterrupting:   response.AlreadyInterrupting,
		AlreadyTerminal:       response.AlreadyTerminal,
		LastEventId:           response.LastEventID,
	}, true
}
