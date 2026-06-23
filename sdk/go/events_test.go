package codex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc/codes"
)

func TestNextEventDecodesCurrentP0Events(t *testing.T) {
	events := &EventStream{stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{
		chatEventMessage(&pb.ChatEvent{
			EventId:     1,
			EventCursor: "cursor-1",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_StatusUpdated{
				StatusUpdated: &pb.ChatStatusUpdatedEvent{
					Status: &pb.ChatStatus{ChatId: "thread-1", CurrentRunId: "run-1"},
				},
			},
		}),
		chatEventMessage(&pb.ChatEvent{
			EventId:     2,
			EventCursor: "cursor-2",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_AssistantDelta{
				AssistantDelta: &pb.AssistantDeltaEvent{TextDelta: "hel"},
			},
		}),
		chatEventMessage(&pb.ChatEvent{
			EventId:     3,
			EventCursor: "cursor-3",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_AssistantMessageCompleted{
				AssistantMessageCompleted: &pb.AssistantMessageCompletedEvent{Message: "hello"},
			},
		}),
		chatEventMessage(&pb.ChatEvent{
			EventId:     4,
			EventCursor: "cursor-4",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_PendingRequestCreated{
				PendingRequestCreated: &pb.ChatPendingCreatedEvent{
					PendingRequest: &pb.ChatPendingRequest{
						PendingRequestId: "pending-approval",
						PendingType:      pb.PendingType_PENDING_TYPE_COMMAND_APPROVAL,
						Display: &pb.PendingRequestDisplay{
							Payload: &pb.PendingRequestDisplay_CommandApproval{
								CommandApproval: &pb.CommandApprovalDisplay{
									CommandDisplay: "go test ./...",
									DecisionOptions: []*pb.ApprovalDecisionOption{
										{DecisionId: "approve", WireDecision: pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT, DisplayLabel: "Approve", Selectable: true},
										{DecisionId: "deny", WireDecision: pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_DECLINE, DisplayLabel: "Deny", Selectable: true},
									},
								},
							},
						},
					},
				},
			},
		}),
		chatEventMessage(&pb.ChatEvent{
			EventId:     5,
			EventCursor: "cursor-5",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_PendingRequestResolved{
				PendingRequestResolved: &pb.ChatPendingResolvedEvent{
					PendingRequestId: "pending-approval",
					PendingType:      pb.PendingType_PENDING_TYPE_COMMAND_APPROVAL,
					Resolution:       pb.PendingResolution_PENDING_RESOLUTION_ACCEPTED,
				},
			},
		}),
		chatEventMessage(&pb.ChatEvent{
			EventId:     6,
			EventCursor: "cursor-6",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_Terminal{
				Terminal: &pb.ChatTerminalEvent{
					Terminal: &pb.ChatTerminal{
						TerminalLifecycle: pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_COMPLETED,
						ResultSummary:     "done",
					},
				},
			},
		}),
	}}}

	status := mustNextEvent[*StatusChanged](t, events)
	if status.EventKind() != EventKindStatusChanged || status.Status().RunID != "run-1" {
		t.Fatalf("status event = %#v", status)
	}
	delta := mustNextEvent[*AssistantTextDelta](t, events)
	if delta.TextDelta() != "hel" || !delta.Meta().CanResumeAfter {
		t.Fatalf("delta = %#v meta=%#v", delta, delta.Meta())
	}
	completed := mustNextEvent[*AssistantMessageCompleted](t, events)
	if completed.Message() != "hello" {
		t.Fatalf("completed message = %q", completed.Message())
	}
	approval := mustNextEvent[*ApprovalRequested](t, events)
	if approval.PendingKind() != PendingKindApproval || approval.PendingID() != "pending-approval" {
		t.Fatalf("approval identity = %s/%s", approval.PendingKind(), approval.PendingID())
	}
	if approval.Display().Title == "" || len(approval.Decisions()) != 2 || approval.Subject() != ApprovalSubjectCommand {
		t.Fatalf("approval display/decisions/subject = %#v %v %s", approval.Display(), approval.Decisions(), approval.Subject())
	}
	if approval.Meta().CanResumeAfter {
		t.Fatalf("pending approval must not be a safe resume point: %#v", approval.Meta())
	}
	resolved := mustNextEvent[*ActionResolved](t, events)
	if resolved.PendingID() != "pending-approval" || resolved.Resolution() != PendingResolutionAccepted {
		t.Fatalf("resolved = %#v", resolved)
	}
	terminal := mustNextEvent[*RunCompleted](t, events)
	if terminal.Result().State != TerminalStateCompleted || terminal.Result().Err != nil {
		t.Fatalf("terminal result = %#v", terminal.Result())
	}
}

func TestNextEventDecodesCommandAndWarningEventsWithCommandContext(t *testing.T) {
	events := &EventStream{stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{
		chatEventMessage(&pb.ChatEvent{
			EventId:     10,
			EventCursor: "cursor-10",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_CommandStarted{
				CommandStarted: &pb.CommandStartedEvent{
					ItemId:         "cmd-1",
					CommandDisplay: "go test ./...",
					WorkspaceLabel: "codex-runtime",
				},
			},
		}),
		chatEventMessage(&pb.ChatEvent{
			EventId:     11,
			EventCursor: "cursor-11",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_CommandOutputDelta{
				CommandOutputDelta: &pb.CommandOutputDeltaEvent{
					ItemId:    "cmd-1",
					Stream:    pb.CommandOutputStream_COMMAND_OUTPUT_STREAM_COMBINED,
					Delta:     "ok\n",
					Truncated: true,
				},
			},
		}),
		chatEventMessage(&pb.ChatEvent{
			EventId:     12,
			EventCursor: "cursor-12",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_GatewayWarning{
				GatewayWarning: &pb.GatewayWarningEvent{
					Code:           "model_rerouted",
					Message:        "model request was rerouted",
					RequestType:    "model/rerouted",
					AutoResolution: "continue",
					LimitReason:    "policy",
				},
			},
		}),
	}}}

	started := mustNextEvent[*CommandStarted](t, events)
	if started.EventKind() != EventKindCommandStarted || started.Command().ID != "cmd-1" || started.Command().Display != "go test ./..." {
		t.Fatalf("command started = %#v command=%#v", started, started.Command())
	}
	if preview, ok := started.Preview(); preview != "" || ok {
		t.Fatalf("preview = %q known=%t", preview, ok)
	}
	if workspaceLabel, ok := started.WorkspaceLabel(); workspaceLabel != "codex-runtime" || !ok {
		t.Fatalf("workspace label = %q known=%t", workspaceLabel, ok)
	}
	output := mustNextEvent[*CommandOutput](t, events)
	truncated, truncatedKnown := output.Truncated()
	if output.EventKind() != EventKindCommandOutput || output.Delta() != "ok\n" || output.Stream() != CommandOutputStreamCombined || !truncated || !truncatedKnown {
		t.Fatalf("command output = %#v", output)
	}
	if output.Command().ID != "cmd-1" || output.Command().Display != "go test ./..." || !output.Command().Known || output.OrphanReplay() {
		t.Fatalf("command output command context = %#v orphan=%t", output.Command(), output.OrphanReplay())
	}
	warning := mustNextEvent[*Warning](t, events)
	code, codeKnown := warning.Code()
	requestType, requestTypeKnown := warning.RequestType()
	if warning.EventKind() != EventKindWarning || code != "model_rerouted" || !codeKnown || warning.Message() == "" || requestType != "model/rerouted" || !requestTypeKnown {
		t.Fatalf("warning = %#v code=%q/%t request=%q/%t", warning, code, codeKnown, requestType, requestTypeKnown)
	}
	autoResolution, autoResolutionKnown := warning.AutoResolution()
	limitReason, limitReasonKnown := warning.LimitReason()
	if autoResolution != "continue" || !autoResolutionKnown || limitReason != "policy" || !limitReasonKnown {
		t.Fatalf("warning resolution/limit = %q/%t %q/%t", autoResolution, autoResolutionKnown, limitReason, limitReasonKnown)
	}
}

func TestNextEventMarksCommandOutputWithoutStartAsOrphanReplay(t *testing.T) {
	events := &EventStream{stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{
		chatEventMessage(&pb.ChatEvent{
			EventId:     21,
			EventCursor: "cursor-21",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_CommandOutputDelta{
				CommandOutputDelta: &pb.CommandOutputDeltaEvent{
					ItemId: "cmd-orphan",
					Delta:  "late output",
				},
			},
		}),
	}}}

	output := mustNextEvent[*CommandOutput](t, events)
	if output.Command().ID != "" || output.Command().Display != "" || output.Command().Known || !output.OrphanReplay() {
		t.Fatalf("orphan command output context = %#v orphan=%t", output.Command(), output.OrphanReplay())
	}
	if truncated, ok := output.Truncated(); truncated || ok {
		t.Fatalf("orphan command output truncated = %t known=%t", truncated, ok)
	}
}

func TestNextEventRejectsPendingTypeDisplayMismatch(t *testing.T) {
	events := &EventStream{stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{
		chatEventMessage(&pb.ChatEvent{
			EventId:     27,
			EventCursor: "cursor-27",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_PendingRequestCreated{
				PendingRequestCreated: &pb.ChatPendingCreatedEvent{PendingRequest: &pb.ChatPendingRequest{
					PendingRequestId: "pending-mismatch",
					PendingType:      pb.PendingType_PENDING_TYPE_PERMISSIONS_APPROVAL,
					Display: &pb.PendingRequestDisplay{Payload: &pb.PendingRequestDisplay_CommandApproval{
						CommandApproval: &pb.CommandApprovalDisplay{CommandDisplay: "go test ./..."},
					}},
				}},
			},
		}),
	}}}

	_, err := events.NextEvent(context.Background())
	var decodeErr *EventDecodeError
	if !errors.Is(err, ErrEventDecode) || !errors.As(err, &decodeErr) {
		t.Fatalf("pending mismatch error = %T %v", err, err)
	}
	if decodeErr.Position().Cursor != "cursor-27" || decodeErr.SafeMeta().ID != 27 {
		t.Fatalf("pending mismatch decode context = %#v meta=%#v", decodeErr.Position(), decodeErr.SafeMeta())
	}
}

func TestNextEventRejectsPendingMissingIDAndIdentityMismatch(t *testing.T) {
	tests := []struct {
		name    string
		pending *pb.ChatPendingRequest
	}{
		{
			name: "missing pending id",
			pending: &pb.ChatPendingRequest{
				PendingType: pb.PendingType_PENDING_TYPE_COMMAND_APPROVAL,
				Display: &pb.PendingRequestDisplay{Payload: &pb.PendingRequestDisplay_CommandApproval{
					CommandApproval: &pb.CommandApprovalDisplay{CommandDisplay: "go test ./..."},
				}},
			},
		},
		{
			name: "chat id mismatch",
			pending: &pb.ChatPendingRequest{
				PendingRequestId: "pending-chat",
				ChatId:           "thread-other",
				PendingType:      pb.PendingType_PENDING_TYPE_COMMAND_APPROVAL,
				Display: &pb.PendingRequestDisplay{Payload: &pb.PendingRequestDisplay_CommandApproval{
					CommandApproval: &pb.CommandApprovalDisplay{CommandDisplay: "go test ./..."},
				}},
			},
		},
		{
			name: "run id mismatch",
			pending: &pb.ChatPendingRequest{
				PendingRequestId: "pending-run",
				ChatId:           "thread-1",
				RunId:            "run-other",
				PendingType:      pb.PendingType_PENDING_TYPE_COMMAND_APPROVAL,
				Display: &pb.PendingRequestDisplay{Payload: &pb.PendingRequestDisplay_CommandApproval{
					CommandApproval: &pb.CommandApprovalDisplay{CommandDisplay: "go test ./..."},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := &EventStream{stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{
				chatEventMessage(&pb.ChatEvent{
					EventId:     28,
					EventCursor: "cursor-28",
					ChatId:      "thread-1",
					RunId:       "run-1",
					Payload: &pb.ChatEvent_PendingRequestCreated{
						PendingRequestCreated: &pb.ChatPendingCreatedEvent{PendingRequest: tt.pending},
					},
				}),
			}}}

			_, err := events.NextEvent(context.Background())
			var decodeErr *EventDecodeError
			if !errors.Is(err, ErrEventDecode) || !errors.As(err, &decodeErr) {
				t.Fatalf("pending identity error = %T %v", err, err)
			}
			if decodeErr.Position().Cursor != "cursor-28" || decodeErr.SafeMeta().ChatID != "thread-1" || decodeErr.SafeMeta().RunID != "run-1" {
				t.Fatalf("pending identity decode context = %#v meta=%#v", decodeErr.Position(), decodeErr.SafeMeta())
			}
		})
	}
}

func TestNextEventDecodesMessageOnlyWarningWithoutSyntheticOptionalFields(t *testing.T) {
	events := &EventStream{stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{
		chatEventMessage(&pb.ChatEvent{
			EventId:     31,
			EventCursor: "cursor-31",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_GatewayWarning{
				GatewayWarning: &pb.GatewayWarningEvent{
					Message: "configuration warning",
				},
			},
		}),
	}}}

	warning := mustNextEvent[*Warning](t, events)
	if warning.Message() != "configuration warning" {
		t.Fatalf("warning message = %q", warning.Message())
	}
	if code, ok := warning.Code(); code != "" || ok {
		t.Fatalf("warning code = %q known=%t", code, ok)
	}
	if requestType, ok := warning.RequestType(); requestType != "" || ok {
		t.Fatalf("warning request type = %q known=%t", requestType, ok)
	}
	if autoResolution, ok := warning.AutoResolution(); autoResolution != "" || ok {
		t.Fatalf("warning auto resolution = %q known=%t", autoResolution, ok)
	}
	if limitReason, ok := warning.LimitReason(); limitReason != "" || ok {
		t.Fatalf("warning limit reason = %q known=%t", limitReason, ok)
	}
}

func TestNextEventDecodesNoticesUnknownAndRawSafely(t *testing.T) {
	events := &EventStream{stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{
		{
			Payload: &pb.StreamChatEventsResponse_ReplayNotice{
				ReplayNotice: &pb.ChatReplayNotice{
					Code:                  pb.ChatReplayNoticeCode_CHAT_REPLAY_NOTICE_CODE_CURSOR_EVICTED,
					Message:               "cursor evicted",
					OldestBufferedEventId: 10,
					NewestBufferedEventId: 20,
				},
			},
		},
		chatEventMessage(&pb.ChatEvent{
			EventId:     11,
			EventCursor: "cursor-11",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_ToolProgress{
				ToolProgress: &pb.ToolProgressEvent{ItemId: "tool-1", ToolName: "search", State: pb.ToolState_TOOL_STATE_RUNNING},
			},
		}),
	}}}

	notice := mustNextEvent[*ReplayNotice](t, events)
	if notice.Meta().CanResumeAfter || notice.BufferedRange().NewestEventID != 20 {
		t.Fatalf("replay notice meta/range = %#v %#v", notice.Meta(), notice.BufferedRange())
	}
	unknown := mustNextEvent[*UnknownEvent](t, events)
	if unknown.EventKind() != EventKindUnknown || unknown.Meta().Cursor != "cursor-11" {
		t.Fatalf("unknown = %#v", unknown)
	}
	if !unknown.Raw().HasPayload() || unknown.Raw().Proto().GetEvent().GetToolProgress().GetToolName() != "search" {
		t.Fatalf("unknown raw proto missing payload")
	}
	clone := unknown.Raw().Proto()
	clone.GetEvent().EventCursor = "mutated"
	if unknown.Raw().Proto().GetEvent().GetEventCursor() != "cursor-11" {
		t.Fatalf("RawEvent.Proto did not return a clone")
	}
	if rendered := fmt.Sprint(unknown.Raw()); contains(rendered, "search") {
		t.Fatalf("RawEvent string leaked payload details: %s", rendered)
	}
}

func TestNextEventDecodesAllPendingCreatedKindsReadOnly(t *testing.T) {
	events := &EventStream{stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{
		chatEventMessage(&pb.ChatEvent{
			EventId:     1,
			EventCursor: "cursor-1",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_PendingRequestCreated{
				PendingRequestCreated: &pb.ChatPendingCreatedEvent{PendingRequest: &pb.ChatPendingRequest{
					PendingRequestId: "pending-perms",
					PendingType:      pb.PendingType_PENDING_TYPE_PERMISSIONS_APPROVAL,
					Display: &pb.PendingRequestDisplay{Payload: &pb.PendingRequestDisplay_PermissionsApproval{
						PermissionsApproval: &pb.PermissionsApprovalDisplay{
							RequestedPermissions: []*pb.PermissionAtom{{PermissionId: "fs.write", DisplayLabel: "Write files", Grantable: true}},
							RecommendedScope:     pb.PermissionScope_PERMISSION_SCOPE_TURN,
							Reason:               "needs a file edit",
						},
					}},
				}},
			},
		}),
		chatEventMessage(&pb.ChatEvent{
			EventId:     2,
			EventCursor: "cursor-2",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_PendingRequestCreated{
				PendingRequestCreated: &pb.ChatPendingCreatedEvent{PendingRequest: &pb.ChatPendingRequest{
					PendingRequestId: "pending-form",
					PendingType:      pb.PendingType_PENDING_TYPE_MCP_ELICITATION,
					Display: &pb.PendingRequestDisplay{Payload: &pb.PendingRequestDisplay_McpElicitation{
						McpElicitation: &pb.McpElicitationDisplay{
							Mode:           pb.ElicitationMode_ELICITATION_MODE_FORM,
							Message:        "Pick a value",
							FormSchemaJson: `{"type":"object","required":["name"],"properties":{"name":{"type":"string","description":"Name"}}}`,
							SubmitLabel:    "Submit",
						},
					}},
				}},
			},
		}),
		chatEventMessage(&pb.ChatEvent{
			EventId:     3,
			EventCursor: "cursor-3",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_PendingRequestCreated{
				PendingRequestCreated: &pb.ChatPendingCreatedEvent{PendingRequest: &pb.ChatPendingRequest{
					PendingRequestId: "pending-user",
					PendingType:      pb.PendingType_PENDING_TYPE_TOOL_USER_INPUT,
					Display: &pb.PendingRequestDisplay{Payload: &pb.PendingRequestDisplay_ToolUserInput{
						ToolUserInput: &pb.ToolUserInputDisplay{Questions: []*pb.ToolUserInputQuestion{
							{Id: "q1", Header: "Secret", Question: "Token?", IsSecret: true, IsOther: true, Options: []string{"skip"}},
						}},
					}},
				}},
			},
		}),
	}}}

	perms := mustNextEvent[*PermissionsRequested](t, events)
	if perms.PendingKind() != PendingKindPermissions || perms.RecommendedScope() != PermissionScopeTurn || len(perms.RequestedPermissions()) != 1 {
		t.Fatalf("permissions event = %#v", perms)
	}
	form := mustNextEvent[*StructuredInputRequested](t, events)
	fields, err := form.Fields()
	if err != nil {
		t.Fatalf("Fields returned error: %v", err)
	}
	if form.Mode() != StructuredInputModeForm || form.Message() != "Pick a value" || len(fields) != 1 || !fields[0].Required {
		t.Fatalf("structured input = %#v fields=%#v", form, fields)
	}
	userInput := mustNextEvent[*UserInputRequested](t, events)
	questions := userInput.Questions()
	if userInput.PendingKind() != PendingKindUserInput || len(questions) != 1 || !questions[0].Secret || !questions[0].Other {
		t.Fatalf("user input = %#v questions=%#v", userInput, questions)
	}
}

func TestNextEventDefaultAcceptedEmptyCursorDoesNotLockRawRecv(t *testing.T) {
	fake := &fakeRuntimeClient{
		startResponse: &pb.StartChatRunResponse{
			ChatId:            "thread-new",
			RunId:             "run-new",
			LastEventId:       12,
			FirstTurnAccepted: true,
		},
		stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{
			chatEventMessage(&pb.ChatEvent{EventId: 13, EventCursor: "cursor-13", ChatId: "thread-new", RunId: "run-new"}),
		}},
	}
	client := newTestClient(t, fake)

	_, events, err := client.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	_, err = events.NextEvent(context.Background())
	var cursorErr *EventCursorUnavailableError
	if !errors.Is(err, ErrEventCursorUnavailable) || !errors.As(err, &cursorErr) {
		t.Fatalf("NextEvent error = %T %v, want ErrEventCursorUnavailable", err, err)
	}
	raw, err := events.Recv()
	if err != nil {
		t.Fatalf("Recv after no-lock cursor error returned error: %v", err)
	}
	if raw.GetEvent().GetEventCursor() != "cursor-13" {
		t.Fatalf("raw Recv cursor = %q", raw.GetEvent().GetEventCursor())
	}
}

func TestChatRunWithEventsAndEventsForRunUseEventCursor(t *testing.T) {
	fake := &fakeRuntimeClient{
		runResponse: &pb.RunChatTurnResponse{
			ChatId:       "thread-existing",
			RunId:        "run-next",
			EventCursor:  "cursor-run",
			LastEventId:  99,
			TurnAccepted: true,
		},
		stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{
			chatEventMessage(&pb.ChatEvent{
				EventId:     100,
				EventCursor: "cursor-100",
				ChatId:      "thread-existing",
				RunId:       "run-next",
				Payload: &pb.ChatEvent_Terminal{
					Terminal: &pb.ChatTerminalEvent{Terminal: &pb.ChatTerminal{TerminalLifecycle: pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_COMPLETED}},
				},
			}),
		}},
	}
	client := newTestClient(t, fake)
	chat := &Chat{ID: "thread-existing", client: client}

	result, events, err := chat.RunWithEvents(context.Background(), "continue")
	if err != nil {
		t.Fatalf("RunWithEvents returned error: %v", err)
	}
	if result.RunID != "run-next" || fake.streamRequest.GetAfterEventCursor() != "cursor-run" {
		t.Fatalf("result/stream cursor = %#v / %#v", result, fake.streamRequest)
	}
	if _, ok := mustNextEvent[TerminalEvent](t, events).(*RunCompleted); !ok {
		t.Fatalf("terminal event type mismatch")
	}
	if _, err := events.NextEvent(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("after terminal NextEvent = %v, want EOF", err)
	}

	_, err = chat.EventsForRun(context.Background(), &RunResult{ChatID: "thread-existing", RunID: "run-next"})
	if !errors.Is(err, ErrEventCursorUnavailable) {
		t.Fatalf("EventsForRun missing cursor error = %v", err)
	}
	_, _, err = chat.RunWithEvents(context.Background(), "continue", WithInitialStreamOptions(AfterEventID(5)))
	if !errors.Is(err, ErrStreamCursorConflict) {
		t.Fatalf("RunWithEvents cursor conflict = %v", err)
	}
}

func TestFriendlyStatusAndHistoryWrappersMapRawResponses(t *testing.T) {
	fake := &fakeRuntimeClient{
		statusResponse: &pb.GetChatStatusResponse{
			Status: &pb.ChatStatus{
				ChatId:              "thread-existing",
				CurrentRunId:        "run-1",
				ThreadLifecycle:     pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_ACTIVE_RUNNING,
				CurrentRunLifecycle: pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_IN_PROGRESS,
			},
		},
		historyResponse: &pb.GetChatHistoryResponse{
			ChatId: "thread-existing",
			Turns: []*pb.ChatTurnSummary{
				{RunId: "run-1", Lifecycle: pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_COMPLETED, Summary: "done"},
			},
			ReturnedDepth: pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_TURN_SUMMARY,
			NextCursor:    "cursor-next",
		},
	}
	client := newTestClient(t, fake)
	chat := &Chat{ID: "thread-existing", client: client}

	if _, ok := chat.CachedStatusSnapshot(); ok {
		t.Fatal("CachedStatusSnapshot returned ok before status cache exists")
	}
	status, err := chat.GetStatusSnapshot(context.Background())
	if err != nil {
		t.Fatalf("GetStatusSnapshot returned error: %v", err)
	}
	if status.ChatID != "thread-existing" || status.RunID != "run-1" {
		t.Fatalf("status snapshot = %#v", status)
	}
	if status.ThreadLifecycle != ThreadLifecycleActiveRunning || status.RunLifecycle != RunLifecycleInProgress {
		t.Fatalf("status lifecycles = %#v", status)
	}
	cached, ok := chat.CachedStatusSnapshot()
	if !ok || cached.RunID != "run-1" || cached.ThreadLifecycle != ThreadLifecycleActiveRunning {
		t.Fatalf("cached status = %#v ok=%v", cached, ok)
	}

	page, err := chat.GetHistoryPage(
		context.Background(),
		WithHistoryPageDepth(HistoryDepthTurnSummary),
		WithHistoryPageCursor("cursor-1"),
		WithHistoryPageLimit(1),
		WithHistoryPageSort(HistorySortAscending),
	)
	if err != nil {
		t.Fatalf("GetHistoryPage returned error: %v", err)
	}
	if page.ChatID != "thread-existing" || page.NextCursor != "cursor-next" || len(page.Turns) != 1 || page.Turns[0].RunID != "run-1" {
		t.Fatalf("history page = %#v", page)
	}
	if page.Turns[0].Lifecycle != RunLifecycleCompleted {
		t.Fatalf("history turn lifecycle = %#v", page.Turns[0])
	}
	if fake.historyRequest.GetRequestedDepth() != pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_TURN_SUMMARY ||
		fake.historyRequest.GetCursor() != "cursor-1" ||
		fake.historyRequest.GetLimit() != 1 ||
		fake.historyRequest.GetSortDirection() != pb.ChatHistorySortDirection_CHAT_HISTORY_SORT_DIRECTION_ASCENDING {
		t.Fatalf("history request = %#v", fake.historyRequest)
	}
}

func TestTerminalFailureAndInterruptedExposeErrors(t *testing.T) {
	events := &EventStream{stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{
		chatEventMessage(&pb.ChatEvent{
			EventId:     1,
			EventCursor: "cursor-1",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_Terminal{
				Terminal: &pb.ChatTerminalEvent{
					Terminal: &pb.ChatTerminal{TerminalLifecycle: pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_FAILED, ErrorMessage: "failed"},
				},
			},
		}),
		chatEventMessage(&pb.ChatEvent{
			EventId:     2,
			EventCursor: "cursor-2",
			ChatId:      "thread-1",
			RunId:       "run-2",
			Payload: &pb.ChatEvent_Terminal{
				Terminal: &pb.ChatTerminalEvent{
					Terminal: &pb.ChatTerminal{TerminalLifecycle: pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_INTERRUPTED, ErrorMessage: "interrupted"},
				},
			},
		}),
	}}}

	failed := mustNextEvent[*RunFailed](t, events)
	var runFailed *RunFailedError
	if failed.Result().Err == nil || !errors.Is(failed.Result().Err, ErrRunFailed) || !errors.As(failed.Result().Err, &runFailed) {
		t.Fatalf("failed result err = %T %v", failed.Result().Err, failed.Result().Err)
	}
	interrupted := mustNextEvent[*RunInterrupted](t, events)
	var runInterrupted *RunInterruptedError
	if interrupted.Result().Err == nil || !errors.Is(interrupted.Result().Err, ErrRunInterrupted) || !errors.As(interrupted.Result().Err, &runInterrupted) {
		t.Fatalf("interrupted result err = %T %v", interrupted.Result().Err, interrupted.Result().Err)
	}
}

func TestFatalDecodeErrorMarksStreamUnusable(t *testing.T) {
	events := &EventStream{stream: &fakeEventStream{messages: []*pb.StreamChatEventsResponse{
		chatEventMessage(&pb.ChatEvent{
			EventId:     1,
			EventCursor: "",
			ChatId:      "thread-1",
			RunId:       "run-1",
			Payload: &pb.ChatEvent_AssistantDelta{
				AssistantDelta: &pb.AssistantDeltaEvent{TextDelta: "bad"},
			},
		}),
	}}}

	_, err := events.NextEvent(context.Background())
	var decodeErr *EventDecodeError
	if !errors.Is(err, ErrEventDecode) || !errors.As(err, &decodeErr) {
		t.Fatalf("decode error = %T %v", err, err)
	}
	if decodeErr.SafeMeta().ID != 1 {
		t.Fatalf("decode safe meta = %#v", decodeErr.SafeMeta())
	}
	_, err = events.NextEvent(context.Background())
	if !errors.Is(err, ErrEventDecode) {
		t.Fatalf("post-error NextEvent = %v, want ErrEventDecode", err)
	}
}

func mustNextEvent[T StreamEvent](t *testing.T, events *EventStream) T {
	t.Helper()
	event, err := events.NextEvent(context.Background())
	if err != nil {
		t.Fatalf("NextEvent returned error: %v", err)
	}
	typed, ok := event.(T)
	if !ok {
		t.Fatalf("NextEvent type = %T, want %T", event, *new(T))
	}
	return typed
}

func chatEventMessage(event *pb.ChatEvent) *pb.StreamChatEventsResponse {
	return &pb.StreamChatEventsResponse{
		Payload: &pb.StreamChatEventsResponse_Event{Event: event},
	}
}

func TestNextEventContextCancellationBeforeReceive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	events := &EventStream{stream: &fakeEventStream{}}
	_, err := events.NextEvent(ctx)
	assertSDKError(t, err, codes.Canceled, "caller_canceled")
}
