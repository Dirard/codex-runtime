package grpcapi

import (
	"strings"
	"testing"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

func TestTaskEventToProtoWithFailureRejectsOverCapPublicStrings(t *testing.T) {
	tests := []struct {
		name    string
		payload domain.TaskEventPayload
	}{
		{
			name:    "assistant delta",
			payload: domain.AssistantDeltaEvent{TextDelta: overCap(domain.MaxOutboundAssistantTextBytes)},
		},
		{
			name:    "assistant completed message",
			payload: domain.AssistantMessageCompletedEvent{Message: overCap(domain.MaxOutboundAssistantTextBytes)},
		},
		{
			name:    "command display",
			payload: domain.CommandStartedEvent{ItemID: "item-1", CommandDisplay: overCap(domain.MaxOutboundCommandDisplayBytes)},
		},
		{
			name: "command output",
			payload: domain.CommandOutputDeltaEvent{
				ItemID: "item-1",
				Stream: domain.CommandOutputStreamCombined,
				Delta:  overCap(domain.MaxOutboundCommandOutputDeltaBytes),
			},
		},
		{
			name:    "file diff",
			payload: domain.FileDiffUpdatedEvent{ItemID: "item-1", DiffUnified: overCap(domain.MaxOutboundDiffDisplayBytes)},
		},
		{
			name:    "turn diff",
			payload: domain.TurnDiffUpdatedEvent{DiffSummary: overCap(domain.MaxOutboundDiffDisplayBytes)},
		},
		{
			name: "terminal error",
			payload: domain.TaskTerminalEvent{
				TerminalState: domain.TerminalStateFailed,
				ErrorMessage:  overCap(domain.MaxOutboundErrorDisplayMessageBytes),
			},
		},
		{
			name:    "gateway warning",
			payload: domain.GatewayWarningEvent{Message: overCap(domain.MaxOutboundErrorDisplayMessageBytes)},
		},
		{
			name: "pending resolved display message",
			payload: domain.PendingRequestResolvedEvent{
				PendingRequestID: "pending-1",
				PendingType:      domain.PendingTypeCommandApproval,
				Resolution:       domain.PendingResolutionDeclined,
				DisplayMessage:   overCap(domain.MaxOutboundErrorDisplayMessageBytes),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, failure := TaskEventToProtoWithFailure(eventWithPayload(tt.payload))
			assertResourceExhaustedMappingFailure(t, "TaskEventToProtoWithFailure("+tt.name+")", got, failure)
		})
	}
}

func TestTaskEventToProtoWithFailureRejectsOverCapShortPublicCodeStrings(t *testing.T) {
	tests := []struct {
		name    string
		payload domain.TaskEventPayload
	}{
		{
			name: "lifecycle reason code",
			payload: domain.TaskLifecycleEvent{
				LifecycleEvent: domain.TaskLifecycleEventTaskStarted,
				State:          domain.TaskStateRunning,
				ReasonCode:     overCap(domain.MaxSourceLabelBytes),
			},
		},
		{
			name:    "gateway warning code",
			payload: domain.GatewayWarningEvent{Code: overCap(domain.MaxSourceLabelBytes), Message: "warning"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, failure := TaskEventToProtoWithFailure(eventWithPayload(tt.payload))
			assertResourceExhaustedMappingFailure(t, "TaskEventToProtoWithFailure("+tt.name+")", got, failure)
			if got, ok := TaskEventToProto(eventWithPayload(tt.payload)); ok || got != nil {
				t.Fatalf("TaskEventToProto(%s) = (%v, %t), want (nil, false)", tt.name, got, ok)
			}
		})
	}
}

func TestStartTaskResponseToProtoWithFailureRejectsOverCapPublicID(t *testing.T) {
	response := domain.StartTaskResponse{
		TaskID:         overCap(domain.MaxPublicIDBytes),
		ThreadID:       "thread-1",
		TurnID:         "turn-1",
		SessionGroupID: "sg-1",
		State:          domain.TaskStateRunning,
	}

	got, failure := StartTaskResponseToProtoWithFailure(response)
	assertResourceExhaustedMappingFailure(t, "StartTaskResponseToProtoWithFailure(over-cap task_id)", got, failure)
	if got, ok := StartTaskResponseToProto(response); ok || got != nil {
		t.Fatalf("StartTaskResponseToProto(over-cap task_id) = (%v, %t), want (nil, false)", got, ok)
	}
}

func TestTaskEventToProtoWithFailureRejectsOverCapPublicID(t *testing.T) {
	event := eventWithPayload(domain.CommandStartedEvent{
		ItemID:         overCap(domain.MaxPublicIDBytes),
		CommandDisplay: "go test ./...",
	})

	got, failure := TaskEventToProtoWithFailure(event)
	assertResourceExhaustedMappingFailure(t, "TaskEventToProtoWithFailure(over-cap item_id)", got, failure)
	if got, ok := TaskEventToProto(event); ok || got != nil {
		t.Fatalf("TaskEventToProto(over-cap item_id) = (%v, %t), want (nil, false)", got, ok)
	}
}

func TestStartTaskResponseToProtoWithFailureRejectsPaddedPublicID(t *testing.T) {
	response := domain.StartTaskResponse{
		TaskID:         " task-1 ",
		ThreadID:       "thread-1",
		TurnID:         "turn-1",
		SessionGroupID: "sg-1",
		State:          domain.TaskStateRunning,
	}

	got, failure := StartTaskResponseToProtoWithFailure(response)
	assertInternalGatewayMappingFailure(t, "StartTaskResponseToProtoWithFailure(padded task_id)", got, failure)
	if got, ok := StartTaskResponseToProto(response); ok || got != nil {
		t.Fatalf("StartTaskResponseToProto(padded task_id) = (%v, %t), want (nil, false)", got, ok)
	}
}

func TestTaskEventToProtoWithFailureRejectsPaddedPayloadPublicID(t *testing.T) {
	event := eventWithPayload(domain.PendingRequestCreatedEvent{
		PendingRequestID: " pending-1 ",
		PendingType:      domain.PendingTypeCommandApproval,
		Display:          sampleCommandApprovalDisplay(),
	})

	got, failure := TaskEventToProtoWithFailure(event)
	assertInternalGatewayMappingFailure(t, "TaskEventToProtoWithFailure(padded pending_request_id)", got, failure)
	if got, ok := TaskEventToProto(event); ok || got != nil {
		t.Fatalf("TaskEventToProto(padded pending_request_id) = (%v, %t), want (nil, false)", got, ok)
	}
}

func TestPendingRequestToProtoWithFailureRejectsPaddedPublicID(t *testing.T) {
	request := validPendingRequestForRequiredFieldTests()
	request.PendingRequestID = " pending-1 "

	got, failure := PendingRequestToProtoWithFailure(request)
	assertInternalGatewayMappingFailure(t, "PendingRequestToProtoWithFailure(padded pending_request_id)", got, failure)
	if got, ok := PendingRequestToProto(request); ok || got != nil {
		t.Fatalf("PendingRequestToProto(padded pending_request_id) = (%v, %t), want (nil, false)", got, ok)
	}
}

func TestGetTaskStatusResponseToProtoWithFailureRejectsPaddedActivePendingPublicID(t *testing.T) {
	pending := validPendingRequestForRequiredFieldTests()
	pending.PendingRequestID = " pending-1 "
	response := domain.GetTaskStatusResponse{
		TaskID:                "task-1",
		SessionGroupID:        "sg-1",
		State:                 domain.TaskStateRunning,
		ActivePendingRequests: []domain.PendingRequest{pending},
	}

	got, failure := GetTaskStatusResponseToProtoWithFailure(response)
	assertInternalGatewayMappingFailure(t, "GetTaskStatusResponseToProtoWithFailure(padded active pending)", got, failure)
	if got, ok := GetTaskStatusResponseToProto(response); ok || got != nil {
		t.Fatalf("GetTaskStatusResponseToProto(padded active pending) = (%v, %t), want (nil, false)", got, ok)
	}
}

func TestPendingRequestDisplayToProtoWithFailureRejectsOverCapPublicStrings(t *testing.T) {
	tests := []struct {
		name    string
		display domain.PendingRequestDisplay
	}{
		{
			name: "command display",
			display: func() domain.CommandApprovalDisplay {
				display := sampleCommandApprovalDisplay()
				display.CommandDisplay = overCap(domain.MaxOutboundCommandDisplayBytes)
				return display
			}(),
		},
		{
			name: "file diff",
			display: domain.FileChangeApprovalDisplay{
				DiffUnified:     overCap(domain.MaxOutboundDiffDisplayBytes),
				DecisionOptions: []domain.ApprovalDecisionOption{{DecisionID: "cancel", WireDecision: domain.ApprovalWireDecisionCancel, Selectable: true}},
			},
		},
		{
			name: "permission label",
			display: domain.PermissionsApprovalDisplay{
				RequestedPermissions: []domain.PermissionAtom{{PermissionID: "perm-1", DisplayLabel: overCap(domain.MaxOutboundPendingDisplayStringBytes)}},
			},
		},
		{
			name: "mcp message",
			display: domain.McpElicitationDisplay{
				Mode:    domain.ElicitationModeForm,
				Message: overCap(domain.MaxOutboundPendingDisplayStringBytes),
			},
		},
		{
			name: "mcp schema",
			display: domain.McpElicitationDisplay{
				Mode:           domain.ElicitationModeForm,
				FormSchemaJSON: overCap(domain.MaxOutboundMcpFormSchemaBytes),
			},
		},
		{
			name: "mcp url",
			display: domain.McpElicitationDisplay{
				Mode: domain.ElicitationModeURL,
				URL:  overCap(domain.MaxSourceURIBytes),
			},
		},
		{
			name: "tool option",
			display: domain.ToolUserInputDisplay{
				Questions: []domain.ToolUserInputQuestion{{ID: "q1", Options: []string{overCap(domain.MaxOutboundPendingDisplayStringBytes)}}},
			},
		},
		{
			name: "pending display aggregate",
			display: domain.ToolUserInputDisplay{
				Questions: []domain.ToolUserInputQuestion{{
					ID: "q1",
					Options: []string{
						strings.Repeat("a", domain.MaxOutboundPendingDisplayPayloadBytes/2),
						strings.Repeat("b", domain.MaxOutboundPendingDisplayPayloadBytes/2+1),
					},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, failure := PendingRequestDisplayToProtoWithFailure(tt.display)
			assertResourceExhaustedMappingFailure(t, "PendingRequestDisplayToProtoWithFailure("+tt.name+")", got, failure)
		})
	}
}

func TestPendingRequestDisplayToProtoWithFailureRejectsOverCapPublicIDs(t *testing.T) {
	overCapID := overCap(domain.MaxPublicIDBytes)
	tests := []struct {
		name    string
		display domain.PendingRequestDisplay
	}{
		{
			name: "approval decision option decision_id",
			display: func() domain.CommandApprovalDisplay {
				display := sampleCommandApprovalDisplay()
				display.DecisionOptions[0].DecisionID = overCapID
				return display
			}(),
		},
		{
			name: "permission atom permission_id",
			display: domain.PermissionsApprovalDisplay{
				RequestedPermissions: []domain.PermissionAtom{{PermissionID: overCapID, Kind: "filesystem"}},
			},
		},
		{
			name: "tool user input question id",
			display: domain.ToolUserInputDisplay{
				Questions: []domain.ToolUserInputQuestion{{ID: overCapID, Question: "Continue?"}},
			},
		},
		{
			name: "approval security filesystem entry_id",
			display: func() domain.CommandApprovalDisplay {
				display := sampleCommandApprovalDisplay()
				display.ApprovalSecurity.AdditionalFilesystemEntries[0].EntryID = overCapID
				return display
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, failure := PendingRequestDisplayToProtoWithFailure(tt.display)
			assertResourceExhaustedMappingFailure(t, "PendingRequestDisplayToProtoWithFailure("+tt.name+")", got, failure)
			if got, ok := PendingRequestDisplayToProto(tt.display); ok || got != nil {
				t.Fatalf("PendingRequestDisplayToProto(%s) = (%v, %t), want (nil, false)", tt.name, got, ok)
			}
		})
	}
}

func TestGetTaskStatusResponseToProtoWithFailureRejectsTerminalPublicStringCap(t *testing.T) {
	response := domain.GetTaskStatusResponse{
		TaskID:         "task-1",
		SessionGroupID: "sg-1",
		State:          domain.TaskStateCompleted,
		Terminal: &domain.TaskTerminalEvent{
			TerminalState: domain.TerminalStateCompleted,
			ErrorMessage:  overCap(domain.MaxOutboundErrorDisplayMessageBytes),
		},
	}

	got, failure := GetTaskStatusResponseToProtoWithFailure(response)
	assertResourceExhaustedMappingFailure(t, "GetTaskStatusResponseToProtoWithFailure(terminal error)", got, failure)
}

func TestGetTaskStatusResponseToProtoWithFailureRejectsOverCapActivePendingRequests(t *testing.T) {
	activePending := make([]domain.PendingRequest, domain.MaxActivePendingRequests+1)
	for i := range activePending {
		pending := validPendingRequestForRequiredFieldTests()
		pending.PendingRequestID = "pending-" + strings.Repeat("x", i+1)
		activePending[i] = pending
	}
	response := domain.GetTaskStatusResponse{
		TaskID:                "task-1",
		SessionGroupID:        "sg-1",
		State:                 domain.TaskStateRunning,
		ActivePendingRequests: activePending,
	}

	got, failure := GetTaskStatusResponseToProtoWithFailure(response)
	assertResourceExhaustedMappingFailure(t, "GetTaskStatusResponseToProtoWithFailure(active pending count)", got, failure)
	if got, ok := GetTaskStatusResponseToProto(response); ok || got != nil {
		t.Fatalf("GetTaskStatusResponseToProto(active pending count) = (%v, %t), want (nil, false)", got, ok)
	}
}

func overCap(capBytes int) string {
	return strings.Repeat("x", capBytes+1)
}
