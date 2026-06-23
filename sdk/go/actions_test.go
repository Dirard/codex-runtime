package codex

import (
	"context"
	"errors"
	"testing"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
)

func TestFriendlyActionBuildersAndChatRespondMapWirePayloads(t *testing.T) {
	tests := []struct {
		name  string
		event StreamEvent
		build func(StreamEvent) (ActionResponse, error)
		check func(*testing.T, *pb.RespondChatPendingRequest)
	}{
		{
			name:  "command approval approve",
			event: pendingEventForTest(pb.PendingType_PENDING_TYPE_COMMAND_APPROVAL, "pending-command", commandApprovalDisplayForTest()),
			build: func(event StreamEvent) (ActionResponse, error) {
				return event.(*ApprovalRequested).Approve()
			},
			check: func(t *testing.T, req *pb.RespondChatPendingRequest) {
				if req.GetPendingRequestId() != "pending-command" || req.GetApproval().GetDecisionId() != "approve" {
					t.Fatalf("approval response = %#v", req)
				}
			},
		},
		{
			name:  "file approval deny",
			event: pendingEventForTest(pb.PendingType_PENDING_TYPE_FILE_CHANGE_APPROVAL, "pending-file", fileApprovalDisplayForTest()),
			build: func(event StreamEvent) (ActionResponse, error) {
				return event.(*ApprovalRequested).Deny()
			},
			check: func(t *testing.T, req *pb.RespondChatPendingRequest) {
				if req.GetApproval().GetDecisionId() != "deny" {
					t.Fatalf("file approval response = %#v", req)
				}
			},
		},
		{
			name:  "permissions grant turn strict",
			event: pendingEventForTest(pb.PendingType_PENDING_TYPE_PERMISSIONS_APPROVAL, "pending-perms", permissionsDisplayForTest()),
			build: func(event StreamEvent) (ActionResponse, error) {
				return event.(*PermissionsRequested).GrantTurn("fs.write")
			},
			check: func(t *testing.T, req *pb.RespondChatPendingRequest) {
				got := req.GetPermissions()
				if got.GetScope() != pb.PermissionScope_PERMISSION_SCOPE_TURN || !got.GetStrictAutoReview() || got.GetPermissionIds()[0] != "fs.write" {
					t.Fatalf("permissions grant = %#v", got)
				}
			},
		},
		{
			name:  "permissions deny",
			event: pendingEventForTest(pb.PendingType_PENDING_TYPE_PERMISSIONS_APPROVAL, "pending-perms", permissionsDisplayForTest()),
			build: func(event StreamEvent) (ActionResponse, error) {
				return event.(*PermissionsRequested).Deny(), nil
			},
			check: func(t *testing.T, req *pb.RespondChatPendingRequest) {
				got := req.GetPermissions()
				if len(got.GetPermissionIds()) != 0 || got.GetScope() != pb.PermissionScope_PERMISSION_SCOPE_TURN || got.GetStrictAutoReview() {
					t.Fatalf("permissions deny = %#v", got)
				}
			},
		},
		{
			name:  "structured input submit values",
			event: pendingEventForTest(pb.PendingType_PENDING_TYPE_MCP_ELICITATION, "pending-form", structuredInputDisplayForTest()),
			build: func(event StreamEvent) (ActionResponse, error) {
				return event.(*StructuredInputRequested).Submit(map[string]any{"name": "Codex"})
			},
			check: func(t *testing.T, req *pb.RespondChatPendingRequest) {
				got := req.GetMcpElicitation()
				if got.GetAction() != pb.McpElicitationAction_MCP_ELICITATION_ACTION_ACCEPT || got.GetContentJson() != `{"name":"Codex"}` {
					t.Fatalf("structured submit = %#v", got)
				}
			},
		},
		{
			name:  "user input answer",
			event: pendingEventForTest(pb.PendingType_PENDING_TYPE_TOOL_USER_INPUT, "pending-user", userInputDisplayForTest()),
			build: func(event StreamEvent) (ActionResponse, error) {
				return event.(*UserInputRequested).Answer(UserInputAnswer{QuestionID: "q1", Values: []string{"yes"}})
			},
			check: func(t *testing.T, req *pb.RespondChatPendingRequest) {
				got := req.GetToolUserInput()
				if len(got.GetAnswers()) != 1 || got.GetAnswers()[0].GetQuestionId() != "q1" || got.GetAnswers()[0].GetAnswers()[0] != "yes" {
					t.Fatalf("user input = %#v", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := tt.build(tt.event)
			if err != nil {
				t.Fatalf("builder returned error: %v", err)
			}
			fake := &fakeRuntimeClient{pendingResponse: &pb.RespondChatPendingResponse{Accepted: true}}
			client := newTestClient(t, fake)
			chat := &Chat{ID: "thread-1", client: client}

			if err := chat.Respond(context.Background(), response, WithClientResponseID("response-1"), WithIdempotencyKey("idem-1")); err != nil {
				t.Fatalf("Chat.Respond returned error: %v", err)
			}
			if fake.pendingRequest.GetClientResponseId() != "response-1" || fake.pendingRequest.GetIdempotencyKey() != "idem-1" {
				t.Fatalf("response ids = %#v", fake.pendingRequest)
			}
			tt.check(t, fake.pendingRequest)
		})
	}
}

func TestFriendlyActionBuilderValidationAndRespondRejectsInvalid(t *testing.T) {
	approval := pendingEventForTest(pb.PendingType_PENDING_TYPE_COMMAND_APPROVAL, "pending-command", commandApprovalDisplayForTest()).(*ApprovalRequested)
	if _, err := approval.Choose(ApprovalDecision{ID: "missing", Decision: ApprovalDecisionAccept, Selectable: true}); !errors.Is(err, ErrActionValidation) {
		t.Fatalf("Choose missing decision err = %v", err)
	}

	form := pendingEventForTest(pb.PendingType_PENDING_TYPE_MCP_ELICITATION, "pending-form", structuredInputDisplayForTest()).(*StructuredInputRequested)
	if _, err := form.Submit(map[string]any{}); !errors.Is(err, ErrActionValidation) {
		t.Fatalf("Submit missing required err = %v", err)
	}
	if _, err := form.Submit(map[string]any{"name": 7}); !errors.Is(err, ErrActionValidation) {
		t.Fatalf("Submit wrong type err = %v", err)
	}
	if _, err := form.Submit(map[string]any{"name": "Codex", "extra": true}); !errors.Is(err, ErrActionValidation) {
		t.Fatalf("Submit unknown field err = %v", err)
	}

	fake := &fakeRuntimeClient{}
	client := newTestClient(t, fake)
	chat := &Chat{ID: "thread-1", client: client}
	if err := chat.Respond(context.Background(), nil); !errors.Is(err, ErrInvalidActionResponse) {
		t.Fatalf("Respond(nil) err = %v", err)
	}
	if err := chat.Respond(context.Background(), zeroActionResponse{}); !errors.Is(err, ErrInvalidActionResponse) {
		t.Fatalf("Respond(zero foreign) err = %v", err)
	}
	response, err := approval.Approve()
	if err != nil {
		t.Fatalf("Approve returned error: %v", err)
	}
	if err := chat.Respond(context.Background(), response, WithInitialStreamOptions(AfterEventID(1))); !errors.Is(err, ErrActionValidation) {
		t.Fatalf("Respond invalid option err = %v", err)
	}
}

func TestApprovalDetailsExposeSecurityAndGrantMetadata(t *testing.T) {
	command := pendingEventForTest(pb.PendingType_PENDING_TYPE_COMMAND_APPROVAL, "pending-command", commandApprovalDisplayForTest()).(*ApprovalRequested)
	security := command.Command().Security
	if !security.HasPrivilegeExpansion || security.NetworkHostLabel != "api.example.com" || security.NetworkProtocol != "https" || !security.AdditionalNetwork {
		t.Fatalf("command approval security summary = %#v", security)
	}
	if len(security.FilesystemEntries) != 1 || security.FilesystemEntries[0].ID != "fs-1" || security.FilesystemEntries[0].Access != "write" || !security.FilesystemEntries[0].Approvable {
		t.Fatalf("filesystem security entries = %#v", security.FilesystemEntries)
	}
	if security.ExecPolicyCommand != "go test ./..." || security.ExecPolicyTruncated {
		t.Fatalf("exec policy summary = %#v", security)
	}
	if len(security.NetworkPolicy) != 1 || security.NetworkPolicy[0].HostLabel != "api.example.com" || security.NetworkPolicy[0].Action != "allow" || !security.NetworkPolicy[0].Approvable {
		t.Fatalf("network policy summary = %#v", security.NetworkPolicy)
	}
	if security.BlockingReason != "review required" {
		t.Fatalf("blocking reason = %q", security.BlockingReason)
	}

	file := pendingEventForTest(pb.PendingType_PENDING_TYPE_FILE_CHANGE_APPROVAL, "pending-file", fileApprovalDisplayForTest()).(*ApprovalRequested)
	grant := file.FileChange().GrantRoot
	if !grant.Present || grant.RootLabel != "workspace" || !grant.UnderConfiguredCWD || !grant.Approvable {
		t.Fatalf("file grant root = %#v", grant)
	}
}

func TestWorkflowChatRespondUsesWorkflowScopedRPC(t *testing.T) {
	action := pendingEventForTest(pb.PendingType_PENDING_TYPE_COMMAND_APPROVAL, "pending-command", commandApprovalDisplayForTest()).(*ApprovalRequested)
	response, err := action.Approve()
	if err != nil {
		t.Fatalf("Approve returned error: %v", err)
	}
	fakeWorkflow := newHappyWorkflowFake()
	client := newWorkflowTestClient(t, fakeWorkflow)
	workflow := newTestWorkflow(client)
	chat := &Chat{ID: "workflow-chat-1", client: client, workflow: workflow}

	if err := chat.Respond(context.Background(), response, WithIdempotencyKey("idem-workflow")); err != nil {
		t.Fatalf("workflow Chat.Respond returned error: %v", err)
	}
	if fakeWorkflow.pendingRequest == nil || fakeWorkflow.pendingRequest.GetWorkflow().GetWorkflowId() != "writer" {
		t.Fatalf("workflow pending request = %#v", fakeWorkflow.pendingRequest)
	}
	if fakeWorkflow.pendingRequest.GetApproval().GetDecisionId() != "approve" {
		t.Fatalf("workflow approval decision = %q", fakeWorkflow.pendingRequest.GetApproval().GetDecisionId())
	}
}

func TestRunWithHandlerHandlesTextActionTerminalAndRecovery(t *testing.T) {
	fake := &fakeRuntimeClient{
		startResponse: &pb.StartChatRunResponse{
			ChatId:            "thread-1",
			RunId:             "run-1",
			EventCursor:       "cursor-start",
			FirstTurnAccepted: true,
		},
		stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{
			chatEventMessage(&pb.ChatEvent{
				EventId:     1,
				EventCursor: "cursor-1",
				ChatId:      "thread-1",
				RunId:       "run-1",
				Payload: &pb.ChatEvent_AssistantDelta{
					AssistantDelta: &pb.AssistantDeltaEvent{TextDelta: "hello"},
				},
			}),
			chatEventMessage(&pb.ChatEvent{
				EventId:     2,
				EventCursor: "cursor-2",
				ChatId:      "thread-1",
				RunId:       "run-1",
				Payload: &pb.ChatEvent_PendingRequestCreated{
					PendingRequestCreated: &pb.ChatPendingCreatedEvent{PendingRequest: &pb.ChatPendingRequest{
						PendingRequestId: "pending-command",
						PendingType:      pb.PendingType_PENDING_TYPE_COMMAND_APPROVAL,
						Display:          commandApprovalDisplayForTest(),
					}},
				},
			}),
			chatEventMessage(&pb.ChatEvent{
				EventId:     3,
				EventCursor: "cursor-3",
				ChatId:      "thread-1",
				RunId:       "run-1",
				Payload: &pb.ChatEvent_Terminal{
					Terminal: &pb.ChatTerminalEvent{Terminal: &pb.ChatTerminal{TerminalLifecycle: pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_COMPLETED, ResultSummary: "done"}},
				},
			}),
		}},
		pendingResponse: &pb.RespondChatPendingResponse{Accepted: true},
	}
	client := newTestClient(t, fake)
	var textSeen bool
	result, err := client.RunWithHandler(context.Background(), "hello", RunHandler{
		Text: func(ctx context.Context, chat *Chat, event *AssistantTextDelta) error {
			textSeen = event.TextDelta() == "hello"
			return nil
		},
		Action: func(ctx context.Context, chat *Chat, action PendingAction) (ActionResponse, error) {
			return action.(*ApprovalRequested).Approve()
		},
	})
	if err != nil {
		t.Fatalf("RunWithHandler returned error: %v", err)
	}
	if !textSeen || result.Terminal == nil || result.Terminal.Result().State != TerminalStateCompleted {
		t.Fatalf("handler result = %#v textSeen=%v", result, textSeen)
	}
	if fake.pendingRequest.GetApproval().GetDecisionId() != "approve" {
		t.Fatalf("handler did not respond through Chat.Respond: %#v", fake.pendingRequest)
	}
	if result.LastSafeResumeMeta.Cursor != "cursor-3" {
		t.Fatalf("last safe resume = %#v", result.LastSafeResumeMeta)
	}
}

func TestRunWithHandlerUnhandledPendingReturnsRecoveryState(t *testing.T) {
	fake := &fakeRuntimeClient{
		startResponse: &pb.StartChatRunResponse{
			ChatId:            "thread-1",
			RunId:             "run-1",
			EventCursor:       "cursor-start",
			FirstTurnAccepted: true,
		},
		stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{
			chatEventMessage(&pb.ChatEvent{
				EventId:     1,
				EventCursor: "cursor-1",
				ChatId:      "thread-1",
				RunId:       "run-1",
				Payload: &pb.ChatEvent_AssistantDelta{
					AssistantDelta: &pb.AssistantDeltaEvent{TextDelta: "before"},
				},
			}),
			chatEventMessage(&pb.ChatEvent{
				EventId:     2,
				EventCursor: "cursor-2",
				ChatId:      "thread-1",
				RunId:       "run-1",
				Payload: &pb.ChatEvent_PendingRequestCreated{
					PendingRequestCreated: &pb.ChatPendingCreatedEvent{PendingRequest: &pb.ChatPendingRequest{
						PendingRequestId: "pending-command",
						PendingType:      pb.PendingType_PENDING_TYPE_COMMAND_APPROVAL,
						Display:          commandApprovalDisplayForTest(),
					}},
				},
			}),
		}},
	}
	client := newTestClient(t, fake)
	result, err := client.RunWithHandler(context.Background(), "hello", RunHandler{})
	if !errors.Is(err, ErrUnhandledAction) {
		t.Fatalf("RunWithHandler err = %v", err)
	}
	if result == nil || result.Terminal != nil || result.UnhandledAction == nil {
		t.Fatalf("recovery result = %#v", result)
	}
	if result.LastSafeResumeMeta.Cursor != "cursor-1" {
		t.Fatalf("last safe resume should stay before unresolved pending: %#v", result.LastSafeResumeMeta)
	}
}

type zeroActionResponse struct{}

func (zeroActionResponse) actionResponse() {}

func pendingEventForTest(kind pb.PendingType, pendingID string, display *pb.PendingRequestDisplay) StreamEvent {
	event, err := decodeChatEvent(chatEventMessage(&pb.ChatEvent{
		EventId:     1,
		EventCursor: "cursor-1",
		ChatId:      "thread-1",
		RunId:       "run-1",
		Payload: &pb.ChatEvent_PendingRequestCreated{
			PendingRequestCreated: &pb.ChatPendingCreatedEvent{PendingRequest: &pb.ChatPendingRequest{
				PendingRequestId: pendingID,
				PendingType:      kind,
				Display:          display,
			}},
		},
	}), &pb.ChatEvent{
		EventId:     1,
		EventCursor: "cursor-1",
		ChatId:      "thread-1",
		RunId:       "run-1",
		Payload: &pb.ChatEvent_PendingRequestCreated{
			PendingRequestCreated: &pb.ChatPendingCreatedEvent{PendingRequest: &pb.ChatPendingRequest{
				PendingRequestId: pendingID,
				PendingType:      kind,
				Display:          display,
			}},
		},
	})
	if err != nil {
		panic(err)
	}
	return event
}

func commandApprovalDisplayForTest() *pb.PendingRequestDisplay {
	return &pb.PendingRequestDisplay{Payload: &pb.PendingRequestDisplay_CommandApproval{
		CommandApproval: &pb.CommandApprovalDisplay{
			CommandDisplay: "go test ./...",
			ApprovalSecurity: &pb.ApprovalSecurityMetadata{
				HasPrivilegeExpansion: true,
				NetworkContext:        &pb.NetworkContextDisplay{HostLabel: "api.example.com", Protocol: "https"},
				AdditionalNetwork:     &pb.AdditionalNetworkDisplay{Enabled: true},
				AdditionalFilesystemEntries: []*pb.AdditionalFilesystemEntry{
					{EntryId: "fs-1", Access: "write", PathLabel: "workspace", Approvable: true},
				},
				ExecpolicyAmendmentSummary: &pb.ExecPolicyAmendmentSummary{CommandDisplay: "go test ./...", Truncated: false},
				NetworkPolicyAmendmentSummaries: []*pb.NetworkPolicyAmendmentSummary{
					{HostLabel: "api.example.com", Action: "allow", Approvable: true},
				},
				BlockingReason: "review required",
			},
			DecisionOptions: []*pb.ApprovalDecisionOption{
				{DecisionId: "approve", WireDecision: pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT, DisplayLabel: "Approve", Selectable: true},
				{DecisionId: "deny", WireDecision: pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_DECLINE, DisplayLabel: "Deny", Selectable: true},
				{DecisionId: "cancel", WireDecision: pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_CANCEL, DisplayLabel: "Cancel", Selectable: true},
			},
		},
	}}
}

func fileApprovalDisplayForTest() *pb.PendingRequestDisplay {
	return &pb.PendingRequestDisplay{Payload: &pb.PendingRequestDisplay_FileChangeApproval{
		FileChangeApproval: &pb.FileChangeApprovalDisplay{
			FileLabel:  "sdk/go/events.go",
			ChangeKind: "modify",
			GrantRoot:  &pb.FileGrantRootDisplay{Present: true, RootLabel: "workspace", UnderConfiguredCwd: true, Approvable: true},
			DecisionOptions: []*pb.ApprovalDecisionOption{
				{DecisionId: "approve", WireDecision: pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT, DisplayLabel: "Approve", Selectable: true},
				{DecisionId: "deny", WireDecision: pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_DECLINE, DisplayLabel: "Deny", Selectable: true},
			},
		},
	}}
}

func permissionsDisplayForTest() *pb.PendingRequestDisplay {
	return &pb.PendingRequestDisplay{Payload: &pb.PendingRequestDisplay_PermissionsApproval{
		PermissionsApproval: &pb.PermissionsApprovalDisplay{
			RequestedPermissions: []*pb.PermissionAtom{
				{PermissionId: "fs.write", DisplayLabel: "Write files", Grantable: true},
			},
			RecommendedScope: pb.PermissionScope_PERMISSION_SCOPE_TURN,
			Reason:           "need file write",
		},
	}}
}

func structuredInputDisplayForTest() *pb.PendingRequestDisplay {
	return &pb.PendingRequestDisplay{Payload: &pb.PendingRequestDisplay_McpElicitation{
		McpElicitation: &pb.McpElicitationDisplay{
			Mode:           pb.ElicitationMode_ELICITATION_MODE_FORM,
			Message:        "Pick a name",
			FormSchemaJson: `{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`,
		},
	}}
}

func userInputDisplayForTest() *pb.PendingRequestDisplay {
	return &pb.PendingRequestDisplay{Payload: &pb.PendingRequestDisplay_ToolUserInput{
		ToolUserInput: &pb.ToolUserInputDisplay{Questions: []*pb.ToolUserInputQuestion{
			{Id: "q1", Question: "Continue?", Options: []string{"yes", "no"}},
		}},
	}}
}
