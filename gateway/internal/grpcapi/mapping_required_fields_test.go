package grpcapi

import (
	"reflect"
	"testing"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

func TestTaskEventToProtoWithFailureRejectsMissingRequiredIdentity(t *testing.T) {
	valid := eventWithPayload(domain.AssistantDeltaEvent{TextDelta: "hi"})
	tests := []struct {
		name   string
		mutate func(*domain.TaskEvent)
	}{
		{
			name: "event_id",
			mutate: func(event *domain.TaskEvent) {
				event.EventID = 0
			},
		},
		{
			name: "task_id",
			mutate: func(event *domain.TaskEvent) {
				event.TaskID = ""
			},
		},
		{
			name: "session_group_id",
			mutate: func(event *domain.TaskEvent) {
				event.SessionGroupID = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := valid
			tt.mutate(&event)

			got, failure := TaskEventToProtoWithFailure(event)
			assertInternalGatewayMappingFailure(t, "TaskEventToProtoWithFailure("+tt.name+")", got, failure)
			if got, ok := TaskEventToProto(event); ok || got != nil {
				t.Fatalf("TaskEventToProto(%s) = (%v, %t), want (nil, false)", tt.name, got, ok)
			}
		})
	}
}

func TestTaskEventToProtoWithFailureRejectsMissingNestedRequiredIdentifiers(t *testing.T) {
	tests := []struct {
		name    string
		payload domain.TaskEventPayload
	}{
		{
			name:    "tool progress item_id",
			payload: domain.ToolProgressEvent{ToolName: "shell", State: domain.ToolStateStarted},
		},
		{
			name:    "command started item_id",
			payload: domain.CommandStartedEvent{CommandDisplay: "go test"},
		},
		{
			name: "command output item_id",
			payload: domain.CommandOutputDeltaEvent{
				Stream: domain.CommandOutputStreamCombined,
				Delta:  "ok",
			},
		},
		{
			name:    "file diff item_id",
			payload: domain.FileDiffUpdatedEvent{FileLabel: "main.go", ChangeKind: "modified"},
		},
		{
			name: "pending request created pending_request_id",
			payload: domain.PendingRequestCreatedEvent{
				PendingType: domain.PendingTypeCommandApproval,
				Display:     sampleCommandApprovalDisplay(),
			},
		},
		{
			name: "pending request created pending_type",
			payload: domain.PendingRequestCreatedEvent{
				PendingRequestID: "pending-1",
				PendingType:      domain.PendingType("unknown"),
				Display:          sampleCommandApprovalDisplay(),
			},
		},
		{
			name: "pending request created display",
			payload: domain.PendingRequestCreatedEvent{
				PendingRequestID: "pending-1",
				PendingType:      domain.PendingTypeCommandApproval,
			},
		},
		{
			name: "pending request resolved pending_request_id",
			payload: domain.PendingRequestResolvedEvent{
				PendingType: domain.PendingTypeCommandApproval,
				Resolution:  domain.PendingResolutionDeclined,
			},
		},
		{
			name: "pending request resolved pending_type",
			payload: domain.PendingRequestResolvedEvent{
				PendingRequestID: "pending-1",
				PendingType:      domain.PendingType("unknown"),
				Resolution:       domain.PendingResolutionDeclined,
			},
		},
		{
			name: "pending request resolved resolution",
			payload: domain.PendingRequestResolvedEvent{
				PendingRequestID: "pending-1",
				PendingType:      domain.PendingTypeCommandApproval,
				Resolution:       domain.PendingResolution("unknown"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := eventWithPayload(tt.payload)

			got, failure := TaskEventToProtoWithFailure(event)
			assertInternalGatewayMappingFailure(t, "TaskEventToProtoWithFailure("+tt.name+")", got, failure)
			if got, ok := TaskEventToProto(event); ok || got != nil {
				t.Fatalf("TaskEventToProto(%s) = (%v, %t), want (nil, false)", tt.name, got, ok)
			}
		})
	}
}

func TestPendingRequestToProtoWithFailureRejectsRequiredFieldFailures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*domain.PendingRequest)
	}{
		{
			name: "pending_request_id",
			mutate: func(request *domain.PendingRequest) {
				request.PendingRequestID = ""
			},
		},
		{
			name: "task_id",
			mutate: func(request *domain.PendingRequest) {
				request.TaskID = ""
			},
		},
		{
			name: "pending_type",
			mutate: func(request *domain.PendingRequest) {
				request.PendingType = domain.PendingType("unknown")
			},
		},
		{
			name: "display",
			mutate: func(request *domain.PendingRequest) {
				request.Display = nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := validPendingRequestForRequiredFieldTests()
			tt.mutate(&request)

			got, failure := PendingRequestToProtoWithFailure(request)
			assertInternalGatewayMappingFailure(t, "PendingRequestToProtoWithFailure("+tt.name+")", got, failure)
			if got, ok := PendingRequestToProto(request); ok || got != nil {
				t.Fatalf("PendingRequestToProto(%s) = (%v, %t), want (nil, false)", tt.name, got, ok)
			}
		})
	}
}

func TestPendingRequestDisplayToProtoWithFailureRejectsMissingNestedRequiredIdentifiers(t *testing.T) {
	tests := []struct {
		name    string
		display domain.PendingRequestDisplay
	}{
		{
			name: "approval decision option decision_id",
			display: func() domain.CommandApprovalDisplay {
				display := sampleCommandApprovalDisplay()
				display.DecisionOptions[0].DecisionID = ""
				return display
			}(),
		},
		{
			name: "permission atom permission_id",
			display: domain.PermissionsApprovalDisplay{
				RequestedPermissions: []domain.PermissionAtom{{Kind: "filesystem", DisplayLabel: "Workspace"}},
			},
		},
		{
			name: "tool user input question id",
			display: domain.ToolUserInputDisplay{
				Questions: []domain.ToolUserInputQuestion{{Question: "Continue?", Options: []string{"yes"}}},
			},
		},
		{
			name: "approval security filesystem entry_id",
			display: func() domain.CommandApprovalDisplay {
				display := sampleCommandApprovalDisplay()
				display.ApprovalSecurity.AdditionalFilesystemEntries[0].EntryID = ""
				return display
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, failure := PendingRequestDisplayToProtoWithFailure(tt.display)
			assertInternalGatewayMappingFailure(t, "PendingRequestDisplayToProtoWithFailure("+tt.name+")", got, failure)
			if got, ok := PendingRequestDisplayToProto(tt.display); ok || got != nil {
				t.Fatalf("PendingRequestDisplayToProto(%s) = (%v, %t), want (nil, false)", tt.name, got, ok)
			}
		})
	}
}

func TestPendingRequestDisplayToProtoWithFailureRejectsDuplicatePublicIDs(t *testing.T) {
	tests := []struct {
		name    string
		display domain.PendingRequestDisplay
	}{
		{
			name: "approval decision option decision_id",
			display: commandApprovalDisplayWithOptions([]domain.ApprovalDecisionOption{
				{DecisionID: "decision-1", WireDecision: domain.ApprovalWireDecisionAcceptForSession},
				{DecisionID: "decision-1", WireDecision: domain.ApprovalWireDecisionCancel},
			}),
		},
		{
			name: "permission atom permission_id",
			display: domain.PermissionsApprovalDisplay{
				RequestedPermissions: []domain.PermissionAtom{
					{PermissionID: "perm-1", Kind: "filesystem"},
					{PermissionID: "perm-1", Kind: "network"},
				},
			},
		},
		{
			name: "tool user input question id",
			display: domain.ToolUserInputDisplay{
				Questions: []domain.ToolUserInputQuestion{
					{ID: "q1", Question: "Continue?", Options: []string{"yes"}},
					{ID: "q1", Question: "Continue again?", Options: []string{"no"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, failure := PendingRequestDisplayToProtoWithFailure(tt.display)
			assertInternalGatewayMappingFailure(t, "PendingRequestDisplayToProtoWithFailure("+tt.name+")", got, failure)
			if got, ok := PendingRequestDisplayToProto(tt.display); ok || got != nil {
				t.Fatalf("PendingRequestDisplayToProto(%s) = (%v, %t), want (nil, false)", tt.name, got, ok)
			}
		})
	}
}

func TestPendingRequestDisplayToProtoWithFailureRejectsSelectableUnsafeApproveDecisions(t *testing.T) {
	tests := []struct {
		name    string
		display domain.PendingRequestDisplay
	}{
		{
			name: "command accept",
			display: func() domain.CommandApprovalDisplay {
				display := sampleCommandApprovalDisplay()
				display.ApprovalSecurity.BlockingReason = "approval is blocked"
				display.DecisionOptions = []domain.ApprovalDecisionOption{{
					DecisionID:   "accept",
					WireDecision: domain.ApprovalWireDecisionAccept,
					Selectable:   true,
				}}
				return display
			}(),
		},
		{
			name: "command accept for session",
			display: func() domain.CommandApprovalDisplay {
				display := sampleCommandApprovalDisplay()
				display.ApprovalSecurity.BlockingReason = "approval is blocked"
				display.DecisionOptions = []domain.ApprovalDecisionOption{{
					DecisionID:   "accept-session",
					WireDecision: domain.ApprovalWireDecisionAcceptForSession,
					Selectable:   true,
				}}
				return display
			}(),
		},
		{
			name: "file accept",
			display: domain.FileChangeApprovalDisplay{
				GrantRoot: &domain.FileGrantRootDisplay{Present: true, Approvable: false, UnapprovableReason: "outside workspace"},
				DecisionOptions: []domain.ApprovalDecisionOption{{
					DecisionID:   "accept",
					WireDecision: domain.ApprovalWireDecisionAccept,
					Selectable:   true,
				}},
			},
		},
		{
			name: "file accept for session",
			display: domain.FileChangeApprovalDisplay{
				GrantRoot: &domain.FileGrantRootDisplay{Present: true, Approvable: false, UnapprovableReason: "outside workspace"},
				DecisionOptions: []domain.ApprovalDecisionOption{{
					DecisionID:   "accept-session",
					WireDecision: domain.ApprovalWireDecisionAcceptForSession,
					Selectable:   true,
				}},
			},
		},
		{
			name: "file approvable outside configured cwd accept",
			display: domain.FileChangeApprovalDisplay{
				GrantRoot: &domain.FileGrantRootDisplay{Present: true, RootLabel: "outside", UnderConfiguredCWD: false, Approvable: true},
				DecisionOptions: []domain.ApprovalDecisionOption{{
					DecisionID:   "accept",
					WireDecision: domain.ApprovalWireDecisionAccept,
					Selectable:   true,
				}},
			},
		},
		{
			name: "file approvable outside configured cwd accept for session",
			display: domain.FileChangeApprovalDisplay{
				GrantRoot: &domain.FileGrantRootDisplay{Present: true, RootLabel: "outside", UnderConfiguredCWD: false, Approvable: true},
				DecisionOptions: []domain.ApprovalDecisionOption{{
					DecisionID:   "accept-session",
					WireDecision: domain.ApprovalWireDecisionAcceptForSession,
					Selectable:   true,
				}},
			},
		},
		{
			name: "file approvable missing root label",
			display: domain.FileChangeApprovalDisplay{
				GrantRoot: &domain.FileGrantRootDisplay{Present: true, RootLabel: "", UnderConfiguredCWD: true, Approvable: true},
				DecisionOptions: []domain.ApprovalDecisionOption{{
					DecisionID:   "accept",
					WireDecision: domain.ApprovalWireDecisionAccept,
					Selectable:   true,
				}},
			},
		},
		{
			name: "command empty decision options",
			display: func() domain.CommandApprovalDisplay {
				display := sampleCommandApprovalDisplay()
				display.DecisionOptions = nil
				return display
			}(),
		},
		{
			name: "command no selectable safe direct decision",
			display: func() domain.CommandApprovalDisplay {
				display := sampleCommandApprovalDisplay()
				display.DecisionOptions = []domain.ApprovalDecisionOption{{
					DecisionID:   "accept",
					WireDecision: domain.ApprovalWireDecisionAccept,
					Selectable:   true,
				}}
				return display
			}(),
		},
		{
			name: "file empty decision options",
			display: domain.FileChangeApprovalDisplay{
				DecisionOptions: nil,
			},
		},
		{
			name: "file no selectable safe direct decision",
			display: domain.FileChangeApprovalDisplay{
				DecisionOptions: []domain.ApprovalDecisionOption{{
					DecisionID:   "accept",
					WireDecision: domain.ApprovalWireDecisionAccept,
					Selectable:   true,
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, failure := PendingRequestDisplayToProtoWithFailure(tt.display)
			assertInternalGatewayMappingFailure(t, "PendingRequestDisplayToProtoWithFailure("+tt.name+")", got, failure)
			if got, ok := PendingRequestDisplayToProto(tt.display); ok || got != nil {
				t.Fatalf("PendingRequestDisplayToProto(%s) = (%v, %t), want (nil, false)", tt.name, got, ok)
			}
		})
	}
}

func TestPendingRequestDisplayToProtoWithFailureAllowsSelectableApproveWhenFileGrantRootAbsent(t *testing.T) {
	display := domain.FileChangeApprovalDisplay{
		FileLabel:  "main.go",
		ChangeKind: "modified",
		GrantRoot:  &domain.FileGrantRootDisplay{Present: false},
		DecisionOptions: []domain.ApprovalDecisionOption{
			{
				DecisionID:   "accept",
				WireDecision: domain.ApprovalWireDecisionAccept,
				DisplayLabel: "Accept",
				Selectable:   true,
			},
			{
				DecisionID:   "cancel",
				WireDecision: domain.ApprovalWireDecisionCancel,
				DisplayLabel: "Cancel",
				Selectable:   true,
			},
		},
	}

	got, failure := PendingRequestDisplayToProtoWithFailure(display)
	if failure != nil {
		t.Fatalf("PendingRequestDisplayToProtoWithFailure(absent grant root) failure = %v, want nil", failure)
	}
	if got == nil {
		t.Fatal("PendingRequestDisplayToProtoWithFailure(absent grant root) proto = nil, want value")
	}
	if got, ok := PendingRequestDisplayToProto(display); !ok || got == nil {
		t.Fatalf("PendingRequestDisplayToProto(absent grant root) = (%v, %t), want value and true", got, ok)
	}
}

func TestPendingRequestDisplayToProtoWithFailureRejectsApprovableNetworkPolicyAmendments(t *testing.T) {
	display := sampleCommandApprovalDisplay()
	display.ApprovalSecurity.NetworkPolicyAmendmentSummaries = []domain.NetworkPolicyAmendmentSummary{
		{HostLabel: "example.com", Action: "allow", Approvable: true},
	}

	got, failure := PendingRequestDisplayToProtoWithFailure(display)
	assertInternalGatewayMappingFailure(t, "PendingRequestDisplayToProtoWithFailure(approvable network policy)", got, failure)
	if got, ok := PendingRequestDisplayToProto(display); ok || got != nil {
		t.Fatalf("PendingRequestDisplayToProto(approvable network policy) = (%v, %t), want (nil, false)", got, ok)
	}
}

func TestGetTaskStatusResponseToProtoWithFailureRejectsRequiredFieldFailures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*domain.GetTaskStatusResponse)
	}{
		{
			name: "task_id",
			mutate: func(response *domain.GetTaskStatusResponse) {
				response.TaskID = ""
			},
		},
		{
			name: "session_group_id",
			mutate: func(response *domain.GetTaskStatusResponse) {
				response.SessionGroupID = ""
			},
		},
		{
			name: "state",
			mutate: func(response *domain.GetTaskStatusResponse) {
				response.State = domain.TaskState("unknown")
			},
		},
		{
			name: "active pending",
			mutate: func(response *domain.GetTaskStatusResponse) {
				pending := validPendingRequestForRequiredFieldTests()
				pending.PendingRequestID = ""
				response.ActivePendingRequests = []domain.PendingRequest{pending}
			},
		},
		{
			name: "active pending task_id mismatch",
			mutate: func(response *domain.GetTaskStatusResponse) {
				pending := validPendingRequestForRequiredFieldTests()
				pending.TaskID = "other-task"
				response.ActivePendingRequests = []domain.PendingRequest{pending}
			},
		},
		{
			name: "terminal",
			mutate: func(response *domain.GetTaskStatusResponse) {
				response.Terminal = &domain.TaskTerminalEvent{TerminalState: domain.TerminalState("unknown")}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := validGetTaskStatusResponseForRequiredFieldTests()
			tt.mutate(&response)

			got, failure := GetTaskStatusResponseToProtoWithFailure(response)
			assertInternalGatewayMappingFailure(t, "GetTaskStatusResponseToProtoWithFailure("+tt.name+")", got, failure)
			if got, ok := GetTaskStatusResponseToProto(response); ok || got != nil {
				t.Fatalf("GetTaskStatusResponseToProto(%s) = (%v, %t), want (nil, false)", tt.name, got, ok)
			}
		})
	}
}

func TestRespondPendingRequestResponseToProtoWithFailureRejectsMissingRequiredIDs(t *testing.T) {
	valid := domain.RespondPendingRequestResponse{
		TaskID:           "task-1",
		SessionGroupID:   "sg-1",
		PendingRequestID: "pending-1",
		ClientResponseID: "client-response-1",
		Accepted:         true,
	}
	tests := []struct {
		name   string
		mutate func(*domain.RespondPendingRequestResponse)
	}{
		{
			name: "task_id",
			mutate: func(response *domain.RespondPendingRequestResponse) {
				response.TaskID = ""
			},
		},
		{
			name: "session_group_id",
			mutate: func(response *domain.RespondPendingRequestResponse) {
				response.SessionGroupID = ""
			},
		},
		{
			name: "pending_request_id",
			mutate: func(response *domain.RespondPendingRequestResponse) {
				response.PendingRequestID = ""
			},
		},
		{
			name: "client_response_id",
			mutate: func(response *domain.RespondPendingRequestResponse) {
				response.ClientResponseID = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := valid
			tt.mutate(&response)

			got, failure := RespondPendingRequestResponseToProtoWithFailure(response)
			assertInternalGatewayMappingFailure(t, "RespondPendingRequestResponseToProtoWithFailure("+tt.name+")", got, failure)
			if got := RespondPendingRequestResponseToProto(response); got != nil {
				t.Fatalf("RespondPendingRequestResponseToProto(%s) = %v, want nil", tt.name, got)
			}
		})
	}
}

func TestInterruptTaskResponseToProtoWithFailureRejectsRequiredFieldFailures(t *testing.T) {
	valid := domain.InterruptTaskResponse{
		TaskID:         "task-1",
		SessionGroupID: "sg-1",
		State:          domain.TaskStateInterrupting,
	}
	tests := []struct {
		name   string
		mutate func(*domain.InterruptTaskResponse)
	}{
		{
			name: "task_id",
			mutate: func(response *domain.InterruptTaskResponse) {
				response.TaskID = ""
			},
		},
		{
			name: "session_group_id",
			mutate: func(response *domain.InterruptTaskResponse) {
				response.SessionGroupID = ""
			},
		},
		{
			name: "state",
			mutate: func(response *domain.InterruptTaskResponse) {
				response.State = domain.TaskState("unknown")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := valid
			tt.mutate(&response)

			got, failure := InterruptTaskResponseToProtoWithFailure(response)
			assertInternalGatewayMappingFailure(t, "InterruptTaskResponseToProtoWithFailure("+tt.name+")", got, failure)
			if got, ok := InterruptTaskResponseToProto(response); ok || got != nil {
				t.Fatalf("InterruptTaskResponseToProto(%s) = (%v, %t), want (nil, false)", tt.name, got, ok)
			}
		})
	}
}

func validPendingRequestForRequiredFieldTests() domain.PendingRequest {
	return domain.PendingRequest{
		PendingRequestID: "pending-1",
		TaskID:           "task-1",
		PendingType:      domain.PendingTypeCommandApproval,
		Display:          sampleCommandApprovalDisplay(),
	}
}

func validGetTaskStatusResponseForRequiredFieldTests() domain.GetTaskStatusResponse {
	return domain.GetTaskStatusResponse{
		TaskID:         "task-1",
		SessionGroupID: "sg-1",
		State:          domain.TaskStateRunning,
	}
}

func assertInternalGatewayMappingFailure(t *testing.T, name string, protoValue any, failure *MappingFailure) {
	t.Helper()

	if protoValue != nil && !reflect.ValueOf(protoValue).IsNil() {
		t.Fatalf("%s proto = %v, want nil", name, protoValue)
	}
	if failure == nil {
		t.Fatalf("%s failure = nil, want internal_gateway_error", name)
	}
	if failure.Reason != domain.ReasonInternalGatewayError {
		t.Fatalf("%s failure reason = %s, want %s", name, failure.Reason, domain.ReasonInternalGatewayError)
	}
}
