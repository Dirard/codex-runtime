package grpcapi

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type testResolver map[string]domain.SessionGroupMetadata

func (r testResolver) ResolveSessionGroup(sessionGroupID string) (domain.SessionGroupMetadata, bool) {
	metadata, ok := r[sessionGroupID]
	return metadata, ok
}

func TestStartTaskProtoDoesNotExposePrivilegedFields(t *testing.T) {
	fields := pb.File_codex_control_v1_codex_control_proto.Messages().ByName("StartTaskRequest").Fields()
	forbidden := []protoreflect.Name{
		"cwd",
		"sandbox",
		"permissions",
		"approval_policy",
		"approvals_reviewer",
		"model",
		"provider",
		"service_tier",
		"reasoning",
		"config",
		"instructions",
		"developer_instructions",
		"base_instructions",
		"runtime_workspace_roots",
		"environment",
		"env",
		"raw_json_rpc",
		"raw_history",
	}
	for _, name := range forbidden {
		if fields.ByName(name) != nil {
			t.Fatalf("StartTaskRequest exposes forbidden field %q", name)
		}
	}
}

func TestValidateStartTask(t *testing.T) {
	resolver := testResolver{
		"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
	}

	req := &pb.StartTaskRequest{
		SessionGroupId:  "sg-1",
		WorkspaceId:     "ws-1",
		Prompt:          "hello",
		ClientMessageId: "client-message-1",
		ContextBlocks: []*pb.ContextBlock{
			{
				Kind:        pb.ContextBlockKind_CONTEXT_BLOCK_KIND_APPLICATION,
				SourceLabel: " ticket ",
				SourceUri:   "https://example.com/source",
				MimeType:    "text/plain",
				Content:     "context",
			},
		},
		UiCorrelationMetadata: map[string]string{"ui": "value"},
	}

	command, err := ValidateStartTask(req, resolver)
	if err != nil {
		t.Fatalf("ValidateStartTask() error = %v", err)
	}
	want := domain.StartTaskCommand{
		SessionGroupID:  "sg-1",
		WorkspaceID:     "ws-1",
		Prompt:          "hello",
		ClientMessageID: "client-message-1",
		ContextBlocks: []domain.ContextBlock{
			{
				Kind:        domain.ContextBlockKindApplication,
				SourceLabel: "ticket",
				SourceURI:   "https://example.com/source",
				MimeType:    "text/plain",
				Content:     "context",
			},
		},
		UICorrelationMetadata: map[string]string{"ui": "value"},
	}
	if !reflect.DeepEqual(command, want) {
		t.Fatalf("ValidateStartTask() = %#v, want %#v", command, want)
	}
}

func TestValidateStartTaskRejectsMissingRequiredIDsAndWorkspaceMismatch(t *testing.T) {
	resolver := testResolver{
		"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
	}

	tests := []struct {
		name   string
		req    *pb.StartTaskRequest
		reason domain.GatewayErrorReason
		code   codes.Code
	}{
		{
			name:   "missing session group",
			req:    &pb.StartTaskRequest{ClientMessageId: "client-message-1"},
			reason: domain.ReasonInvalidRequest,
			code:   codes.InvalidArgument,
		},
		{
			name:   "missing client message id",
			req:    &pb.StartTaskRequest{SessionGroupId: "sg-1"},
			reason: domain.ReasonInvalidRequest,
			code:   codes.InvalidArgument,
		},
		{
			name:   "unknown session group",
			req:    &pb.StartTaskRequest{SessionGroupId: "missing", ClientMessageId: "client-message-1"},
			reason: domain.ReasonUnknownSessionGroup,
			code:   codes.NotFound,
		},
		{
			name:   "workspace mismatch",
			req:    &pb.StartTaskRequest{SessionGroupId: "sg-1", WorkspaceId: "other", ClientMessageId: "client-message-1"},
			reason: domain.ReasonWorkspaceMismatch,
			code:   codes.InvalidArgument,
		},
		{
			name: "unsafe source uri",
			req: &pb.StartTaskRequest{
				SessionGroupId:  "sg-1",
				ClientMessageId: "client-message-1",
				ContextBlocks: []*pb.ContextBlock{
					{
						Kind:      pb.ContextBlockKind_CONTEXT_BLOCK_KIND_UNTRUSTED,
						SourceUri: "file:///tmp/context.txt",
					},
				},
			},
			reason: domain.ReasonInvalidRequest,
			code:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ValidateStartTask(tt.req, resolver); !hasRequestError(err, tt.code, tt.reason) {
				t.Fatalf("ValidateStartTask() error = %#v, want %s %s", err, tt.code, tt.reason)
			}
		})
	}
}

func TestValidatePublicIDsRejectWhitespacePadding(t *testing.T) {
	resolver := testResolver{
		"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
	}
	tests := []struct {
		name     string
		code     codes.Code
		reason   domain.GatewayErrorReason
		validate func() *RequestError
	}{
		{
			name:   "start padded session_group_id",
			code:   codes.InvalidArgument,
			reason: domain.ReasonInvalidRequest,
			validate: func() *RequestError {
				req := validStartTaskRequest()
				req.SessionGroupId = " sg-1 "
				_, err := ValidateStartTask(req, resolver)
				return err
			},
		},
		{
			name:   "start padded client_message_id",
			code:   codes.InvalidArgument,
			reason: domain.ReasonInvalidRequest,
			validate: func() *RequestError {
				req := validStartTaskRequest()
				req.ClientMessageId = " client-message-1 "
				_, err := ValidateStartTask(req, resolver)
				return err
			},
		},
		{
			name:   "start padded optional thread_id",
			code:   codes.InvalidArgument,
			reason: domain.ReasonInvalidRequest,
			validate: func() *RequestError {
				req := validStartTaskRequest()
				req.ThreadId = " thread-1 "
				_, err := ValidateStartTask(req, resolver)
				return err
			},
		},
		{
			name:   "start whitespace-only optional thread_id",
			code:   codes.InvalidArgument,
			reason: domain.ReasonInvalidRequest,
			validate: func() *RequestError {
				req := validStartTaskRequest()
				req.ThreadId = "   "
				_, err := ValidateStartTask(req, resolver)
				return err
			},
		},
		{
			name:   "get status padded task_id locator",
			code:   codes.InvalidArgument,
			reason: domain.ReasonInvalidLocator,
			validate: func() *RequestError {
				_, err := ValidateGetTaskStatus(&pb.GetTaskStatusRequest{
					Locator: &pb.GetTaskStatusRequest_TaskId{TaskId: " task-1 "},
				})
				return err
			},
		},
		{
			name:   "respond padded pending_request_id",
			code:   codes.InvalidArgument,
			reason: domain.ReasonInvalidLocator,
			validate: func() *RequestError {
				req := basePendingResponseRequest(&pb.RespondPendingRequestRequest_Approval{
					Approval: &pb.ApprovalPendingResponse{DecisionId: "decline"},
				})
				req.PendingRequestId = " pending-1 "
				_, err := ValidateRespondPendingRequest(req)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.validate(); !hasRequestError(err, tt.code, tt.reason) {
				t.Fatalf("%s error = %#v, want %s %s", tt.name, err, tt.code, tt.reason)
			}
		})
	}
}

func TestValidateChatRuntimeRejectsNonCodexThreadID(t *testing.T) {
	resolver := testResolver{
		"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
	}
	context := &pb.ChatRuntimeContext{SessionGroupId: "sg-1", WorkspaceId: "ws-1"}
	tests := []struct {
		name     string
		validate func() *RequestError
	}{
		{
			name: "get chat",
			validate: func() *RequestError {
				_, err := ValidateGetChat(&pb.GetChatRequest{Context: context, ChatId: "thread-1"}, resolver)
				return err
			},
		},
		{
			name: "run turn",
			validate: func() *RequestError {
				_, err := ValidateRunChatTurn(&pb.RunChatTurnRequest{
					Context:         context,
					ChatId:          "thread-1",
					Prompt:          "continue",
					ClientMessageId: "client-message-1",
					IdempotencyKey:  "idem-1",
				}, resolver)
				return err
			},
		},
		{
			name: "get status",
			validate: func() *RequestError {
				_, err := ValidateGetChatStatus(&pb.GetChatStatusRequest{Context: context, ChatId: "thread-1"}, resolver)
				return err
			},
		},
		{
			name: "get history",
			validate: func() *RequestError {
				_, err := ValidateGetChatHistory(&pb.GetChatHistoryRequest{Context: context, ChatId: "thread-1"}, resolver)
				return err
			},
		},
		{
			name: "stream events",
			validate: func() *RequestError {
				_, err := ValidateStreamChatEvents(&pb.StreamChatEventsRequest{
					Context: context,
					ChatId:  "thread-1",
					Cursor:  &pb.StreamChatEventsRequest_FromStart{FromStart: &pb.ChatFromStartCursor{}},
				}, resolver)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.validate(); !hasRequestError(err, codes.InvalidArgument, domain.ReasonInvalidLocator) {
				t.Fatalf("%s error = %#v, want invalid locator", tt.name, err)
			}
		})
	}
}

func TestValidateChatRuntimeAcceptsCodexThreadID(t *testing.T) {
	resolver := testResolver{
		"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
	}
	command, err := ValidateRunChatTurn(&pb.RunChatTurnRequest{
		Context: &pb.ChatRuntimeContext{
			SessionGroupId: "sg-1",
			WorkspaceId:    "ws-1",
		},
		ChatId:          testCodexThreadID,
		Prompt:          "continue",
		ClientMessageId: "client-message-1",
		IdempotencyKey:  "idem-1",
	}, resolver)
	if err != nil {
		t.Fatalf("ValidateRunChatTurn() error = %v", err)
	}
	if command.ChatID != testCodexThreadID {
		t.Fatalf("ValidateRunChatTurn() ChatID = %q, want %q", command.ChatID, testCodexThreadID)
	}
}

func TestValidateNestedPublicIDsRejectWhitespacePadding(t *testing.T) {
	tests := []struct {
		name     string
		reason   domain.GatewayErrorReason
		validate func() *RequestError
	}{
		{
			name:   "get client-message locator padded session_group_id",
			reason: domain.ReasonInvalidLocator,
			validate: func() *RequestError {
				_, err := ValidateGetTaskStatus(&pb.GetTaskStatusRequest{
					Locator: &pb.GetTaskStatusRequest_ClientMessageLocator{
						ClientMessageLocator: &pb.ClientMessageTaskLocator{
							SessionGroupId:  " sg-1 ",
							ClientMessageId: "client-message-1",
						},
					},
				})
				return err
			},
		},
		{
			name:   "get client-message locator padded client_message_id",
			reason: domain.ReasonInvalidLocator,
			validate: func() *RequestError {
				_, err := ValidateGetTaskStatus(&pb.GetTaskStatusRequest{
					Locator: &pb.GetTaskStatusRequest_ClientMessageLocator{
						ClientMessageLocator: &pb.ClientMessageTaskLocator{
							SessionGroupId:  "sg-1",
							ClientMessageId: " client-message-1 ",
						},
					},
				})
				return err
			},
		},
		{
			name:   "get thread locator padded session_group_id",
			reason: domain.ReasonInvalidLocator,
			validate: func() *RequestError {
				_, err := ValidateGetTaskStatus(&pb.GetTaskStatusRequest{
					Locator: &pb.GetTaskStatusRequest_ThreadLocator{
						ThreadLocator: &pb.ThreadTaskLocator{
							SessionGroupId: " sg-1 ",
							ThreadId:       "thread-1",
						},
					},
				})
				return err
			},
		},
		{
			name:   "get thread locator padded thread_id",
			reason: domain.ReasonInvalidLocator,
			validate: func() *RequestError {
				_, err := ValidateGetTaskStatus(&pb.GetTaskStatusRequest{
					Locator: &pb.GetTaskStatusRequest_ThreadLocator{
						ThreadLocator: &pb.ThreadTaskLocator{
							SessionGroupId: "sg-1",
							ThreadId:       " thread-1 ",
						},
					},
				})
				return err
			},
		},
		{
			name:   "interrupt client-message locator padded client_message_id",
			reason: domain.ReasonInvalidLocator,
			validate: func() *RequestError {
				_, err := ValidateInterruptTask(&pb.InterruptTaskRequest{
					Locator: &pb.InterruptTaskRequest_ClientMessageLocator{
						ClientMessageLocator: &pb.ClientMessageTaskLocator{
							SessionGroupId:  "sg-1",
							ClientMessageId: " client-message-1 ",
						},
					},
				})
				return err
			},
		},
		{
			name:   "permissions padded permission_id",
			reason: domain.ReasonInvalidRequest,
			validate: func() *RequestError {
				_, err := ValidateRespondPendingRequest(basePendingResponseRequest(&pb.RespondPendingRequestRequest_Permissions{
					Permissions: &pb.PermissionsPendingResponse{PermissionIds: []string{" perm-1 "}},
				}))
				return err
			},
		},
		{
			name:   "tool user input padded question_id",
			reason: domain.ReasonInvalidRequest,
			validate: func() *RequestError {
				_, err := ValidateRespondPendingRequest(basePendingResponseRequest(&pb.RespondPendingRequestRequest_ToolUserInput{
					ToolUserInput: &pb.ToolUserInputPendingResponse{
						Answers: []*pb.ToolUserInputAnswer{{QuestionId: " q1 ", Answers: []string{"one"}}},
					},
				}))
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.validate(); !hasRequestError(err, codes.InvalidArgument, tt.reason) {
				t.Fatalf("%s error = %#v, want invalid_argument %s", tt.name, err, tt.reason)
			}
		})
	}
}

func TestValidateStartTaskRejectsInvalidResolvedWorkspaceID(t *testing.T) {
	tests := []struct {
		name        string
		workspaceID string
		code        codes.Code
		reason      domain.GatewayErrorReason
	}{
		{
			name:        "empty",
			workspaceID: "",
			code:        codes.InvalidArgument,
			reason:      domain.ReasonInvalidRequest,
		},
		{
			name:        "whitespace-padded",
			workspaceID: " ws-1 ",
			code:        codes.InvalidArgument,
			reason:      domain.ReasonInvalidRequest,
		},
		{
			name:        "over-cap",
			workspaceID: strings.Repeat("x", domain.MaxPublicIDBytes+1),
			code:        codes.ResourceExhausted,
			reason:      domain.ReasonResourceExhausted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := testResolver{
				"sg-1": testSessionGroupMetadata("sg-1", tt.workspaceID),
			}
			req := validStartTaskRequest()
			req.WorkspaceId = ""

			if _, err := ValidateStartTask(req, resolver); !hasRequestError(err, tt.code, tt.reason) {
				t.Fatalf("ValidateStartTask() error = %#v, want %s %s", err, tt.code, tt.reason)
			}
		})
	}
}

func TestValidateStartTaskRequiresSourceLabel(t *testing.T) {
	resolver := testResolver{
		"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
	}
	tests := []struct {
		name        string
		sourceLabel string
	}{
		{name: "empty"},
		{name: "whitespace", sourceLabel: " \t "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validStartTaskRequest()
			req.ContextBlocks[0].SourceLabel = tt.sourceLabel
			if _, err := ValidateStartTask(req, resolver); !hasRequestError(err, codes.InvalidArgument, domain.ReasonInvalidRequest) {
				t.Fatalf("ValidateStartTask() error = %#v, want invalid_request", err)
			}
		})
	}
}

func TestValidateStartTaskRejectsWhitespacePaddedOverCapRawSourceLabel(t *testing.T) {
	resolver := testResolver{
		"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
	}
	rawSourceLabel := strings.Repeat(" ", domain.MaxSourceLabelBytes) + "x"
	req := validStartTaskRequest()
	req.ContextBlocks[0].SourceLabel = rawSourceLabel

	_, err := ValidateStartTask(req, resolver)
	assertResourceExhaustedRequestErrorDoesNotEcho(t, "source_label", err, rawSourceLabel)
}

func TestValidateStartTaskSourceURIRejectsUnsafeEdges(t *testing.T) {
	resolver := testResolver{
		"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
	}
	tests := []struct {
		name      string
		sourceURI string
	}{
		{name: "query", sourceURI: "https://example.com/source?token=secret"},
		{name: "fragment", sourceURI: "https://example.com/source#section"},
		{name: "userinfo", sourceURI: "https://user@example.com/source"},
		{name: "windows path", sourceURI: `C:\tmp\source.txt`},
		{name: "unix path", sourceURI: "/tmp/source.txt"},
		{name: "unc path", sourceURI: `\\server\share\source.txt`},
		{name: "leading whitespace", sourceURI: " https://example.com/source"},
		{name: "trailing whitespace", sourceURI: "https://example.com/source "},
		{name: "embedded whitespace", sourceURI: "https://example.com/source path"},
		{name: "malformed", sourceURI: "://missing-scheme"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validStartTaskRequest()
			req.ContextBlocks[0].SourceUri = tt.sourceURI
			if _, err := ValidateStartTask(req, resolver); !hasRequestError(err, codes.InvalidArgument, domain.ReasonInvalidRequest) {
				t.Fatalf("ValidateStartTask() error = %#v, want invalid_request", err)
			}
		})
	}
}

func TestValidateStartTaskSourceContentLineCap(t *testing.T) {
	resolver := testResolver{
		"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
	}
	atCap := strings.Repeat("x", domain.MaxContextSourceLineBytes)
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{name: "exact cap single line", content: atCap},
		{name: "exact cap lf split", content: atCap + "\n" + atCap},
		{name: "exact cap crlf split", content: atCap + "\r\n" + atCap},
		{name: "cap plus one", content: atCap + "x", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validStartTaskRequest()
			req.ContextBlocks[0].Content = tt.content
			_, err := ValidateStartTask(req, resolver)
			if tt.wantErr {
				if !hasRequestError(err, codes.ResourceExhausted, domain.ReasonResourceExhausted) {
					t.Fatalf("ValidateStartTask() error = %#v, want resource_exhausted", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateStartTask() error = %v", err)
			}
		})
	}
}

func TestValidateStartTaskRejectsOversizedPrompt(t *testing.T) {
	resolver := testResolver{
		"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
	}
	req := &pb.StartTaskRequest{
		SessionGroupId:  "sg-1",
		ClientMessageId: "client-message-1",
		Prompt:          strings.Repeat("x", domain.MaxPromptBytes+1),
	}

	if _, err := ValidateStartTask(req, resolver); !hasRequestError(err, codes.ResourceExhausted, domain.ReasonResourceExhausted) {
		t.Fatalf("ValidateStartTask() error = %#v, want resource_exhausted", err)
	}
}

func TestValidateStreamTaskCursor(t *testing.T) {
	if _, err := ValidateStreamTask(&pb.StreamTaskRequest{TaskId: "task-1"}); !hasRequestError(err, codes.InvalidArgument, domain.ReasonInvalidCursor) {
		t.Fatalf("ValidateStreamTask() error = %#v, want invalid_cursor", err)
	}

	fromStartCommand, err := ValidateStreamTask(&pb.StreamTaskRequest{
		TaskId: "task-1",
		Cursor: &pb.StreamTaskRequest_FromStart{
			FromStart: &pb.FromStartCursor{},
		},
	})
	if err != nil {
		t.Fatalf("ValidateStreamTask(from_start) error = %v", err)
	}
	if fromStartCommand.CursorKind != domain.StreamCursorFromStart {
		t.Fatalf("ValidateStreamTask(from_start) = %#v, want from_start cursor", fromStartCommand)
	}

	command, err := ValidateStreamTask(&pb.StreamTaskRequest{
		TaskId: "task-1",
		Cursor: &pb.StreamTaskRequest_AfterEventId{
			AfterEventId: 12,
		},
	})
	if err != nil {
		t.Fatalf("ValidateStreamTask() error = %v", err)
	}
	if command.CursorKind != domain.StreamCursorAfterEventID || command.AfterEventID != 12 {
		t.Fatalf("ValidateStreamTask() = %#v, want after_event_id 12", command)
	}

	zeroCursorCommand, err := ValidateStreamTask(&pb.StreamTaskRequest{
		TaskId: "task-1",
		Cursor: &pb.StreamTaskRequest_AfterEventId{
			AfterEventId: 0,
		},
	})
	if err != nil {
		t.Fatalf("ValidateStreamTask(after_event_id=0) error = %v", err)
	}
	if zeroCursorCommand.CursorKind != domain.StreamCursorAfterEventID || zeroCursorCommand.AfterEventID != 0 {
		t.Fatalf("ValidateStreamTask(after_event_id=0) = %#v, want after_event_id 0", zeroCursorCommand)
	}

	fields := pb.File_codex_control_v1_codex_control_proto.Messages().ByName("StreamTaskRequest").Fields()
	if fields.ByName("from_start").ContainingOneof() == nil || fields.ByName("after_event_id").ContainingOneof() == nil {
		t.Fatal("StreamTaskRequest cursor fields must be oneof members")
	}
}

func TestValidateRespondPendingRequestRequiresClientResponseID(t *testing.T) {
	req := &pb.RespondPendingRequestRequest{
		TaskId:           "task-1",
		PendingRequestId: "pending-1",
		Response: &pb.RespondPendingRequestRequest_Approval{
			Approval: &pb.ApprovalPendingResponse{DecisionId: "decline"},
		},
	}

	if _, err := ValidateRespondPendingRequest(req); !hasRequestError(err, codes.InvalidArgument, domain.ReasonInvalidRequest) {
		t.Fatalf("ValidateRespondPendingRequest() error = %#v, want invalid_request", err)
	}
}

func TestValidateRespondPendingRequestMapsPayloads(t *testing.T) {
	tests := []struct {
		name string
		req  *pb.RespondPendingRequestRequest
		want domain.PendingResponse
	}{
		{
			name: "approval",
			req: basePendingResponseRequest(&pb.RespondPendingRequestRequest_Approval{
				Approval: &pb.ApprovalPendingResponse{DecisionId: "accept-session"},
			}),
			want: domain.PendingResponse{Approval: &domain.ApprovalPendingResponse{DecisionID: "accept-session"}},
		},
		{
			name: "permissions",
			req: basePendingResponseRequest(&pb.RespondPendingRequestRequest_Permissions{
				Permissions: &pb.PermissionsPendingResponse{
					PermissionIds:    []string{"perm-1"},
					Scope:            pb.PermissionScope_PERMISSION_SCOPE_SESSION,
					StrictAutoReview: true,
				},
			}),
			want: domain.PendingResponse{Permissions: &domain.PermissionsPendingResponse{
				PermissionIDs:    []string{"perm-1"},
				Scope:            domain.PermissionScopeSession,
				StrictAutoReview: true,
			}},
		},
		{
			name: "mcp elicitation",
			req: basePendingResponseRequest(&pb.RespondPendingRequestRequest_McpElicitation{
				McpElicitation: &pb.McpElicitationPendingResponse{
					Action:      pb.McpElicitationAction_MCP_ELICITATION_ACTION_ACCEPT,
					ContentJson: `{"ok":true}`,
				},
			}),
			want: domain.PendingResponse{McpElicitation: &domain.McpElicitationPendingResponse{
				Action:      domain.McpElicitationActionAccept,
				ContentJSON: `{"ok":true}`,
			}},
		},
		{
			name: "tool user input",
			req: basePendingResponseRequest(&pb.RespondPendingRequestRequest_ToolUserInput{
				ToolUserInput: &pb.ToolUserInputPendingResponse{
					Answers: []*pb.ToolUserInputAnswer{
						{QuestionId: "q1", Answers: []string{"one"}},
					},
				},
			}),
			want: domain.PendingResponse{ToolUserInput: &domain.ToolUserInputPendingResponse{
				Answers: []domain.ToolUserInputAnswer{{QuestionID: "q1", Answers: []string{"one"}}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, err := ValidateRespondPendingRequest(tt.req)
			if err != nil {
				t.Fatalf("ValidateRespondPendingRequest() error = %v", err)
			}
			if !reflect.DeepEqual(command.Response, tt.want) {
				t.Fatalf("pending response = %#v, want %#v", command.Response, tt.want)
			}
		})
	}
}

func TestValidateRespondPendingRequestRejectsDuplicateToolUserInputQuestionIDs(t *testing.T) {
	req := basePendingResponseRequest(&pb.RespondPendingRequestRequest_ToolUserInput{
		ToolUserInput: &pb.ToolUserInputPendingResponse{
			Answers: []*pb.ToolUserInputAnswer{
				{QuestionId: "q1", Answers: []string{"one"}},
				{QuestionId: "q1", Answers: []string{"two"}},
			},
		},
	})

	if _, err := ValidateRespondPendingRequest(req); !hasRequestError(err, codes.InvalidArgument, domain.ReasonInvalidRequest) {
		t.Fatalf("ValidateRespondPendingRequest() error = %#v, want invalid_request", err)
	}
}

func TestValidateRespondPendingRequestRejectsDuplicatePermissionIDs(t *testing.T) {
	req := basePendingResponseRequest(&pb.RespondPendingRequestRequest_Permissions{
		Permissions: &pb.PermissionsPendingResponse{
			PermissionIds: []string{"perm-1", "perm-1"},
		},
	})

	if _, err := ValidateRespondPendingRequest(req); !hasRequestError(err, codes.InvalidArgument, domain.ReasonInvalidRequest) {
		t.Fatalf("ValidateRespondPendingRequest() error = %#v, want invalid_request", err)
	}
}

func TestValidateRespondPendingRequestRejectsEmptyPermissionsWithScopeOrStrictAutoReview(t *testing.T) {
	tests := []struct {
		name     string
		response *pb.PermissionsPendingResponse
	}{
		{
			name:     "turn scope",
			response: &pb.PermissionsPendingResponse{Scope: pb.PermissionScope_PERMISSION_SCOPE_TURN},
		},
		{
			name:     "session scope",
			response: &pb.PermissionsPendingResponse{Scope: pb.PermissionScope_PERMISSION_SCOPE_SESSION},
		},
		{
			name:     "strict auto review",
			response: &pb.PermissionsPendingResponse{StrictAutoReview: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := basePendingResponseRequest(&pb.RespondPendingRequestRequest_Permissions{
				Permissions: tt.response,
			})

			if _, err := ValidateRespondPendingRequest(req); !hasRequestError(err, codes.InvalidArgument, domain.ReasonInvalidRequest) {
				t.Fatalf("ValidateRespondPendingRequest() error = %#v, want invalid_request", err)
			}
		})
	}
}

func TestValidateInboundPublicIDsRejectOverCapValues(t *testing.T) {
	overCapID := strings.Repeat("x", domain.MaxPublicIDBytes+1)
	resolver := testResolver{
		"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
	}
	tests := []struct {
		name     string
		validate func() *RequestError
	}{
		{
			name: "start session_group_id",
			validate: func() *RequestError {
				req := validStartTaskRequest()
				req.SessionGroupId = overCapID
				_, err := ValidateStartTask(req, resolver)
				return err
			},
		},
		{
			name: "start workspace_id",
			validate: func() *RequestError {
				req := validStartTaskRequest()
				req.WorkspaceId = overCapID
				_, err := ValidateStartTask(req, resolver)
				return err
			},
		},
		{
			name: "start thread_id",
			validate: func() *RequestError {
				req := validStartTaskRequest()
				req.ThreadId = overCapID
				_, err := ValidateStartTask(req, resolver)
				return err
			},
		},
		{
			name: "start client_message_id",
			validate: func() *RequestError {
				req := validStartTaskRequest()
				req.ClientMessageId = overCapID
				_, err := ValidateStartTask(req, resolver)
				return err
			},
		},
		{
			name: "stream task_id",
			validate: func() *RequestError {
				_, err := ValidateStreamTask(&pb.StreamTaskRequest{
					TaskId: overCapID,
					Cursor: &pb.StreamTaskRequest_FromStart{
						FromStart: &pb.FromStartCursor{},
					},
				})
				return err
			},
		},
		{
			name: "stream client_subscriber_id",
			validate: func() *RequestError {
				_, err := ValidateStreamTask(&pb.StreamTaskRequest{
					TaskId:             "task-1",
					ClientSubscriberId: overCapID,
					Cursor: &pb.StreamTaskRequest_FromStart{
						FromStart: &pb.FromStartCursor{},
					},
				})
				return err
			},
		},
		{
			name: "get task locator task_id",
			validate: func() *RequestError {
				_, err := ValidateGetTaskStatus(&pb.GetTaskStatusRequest{
					Locator: &pb.GetTaskStatusRequest_TaskId{TaskId: overCapID},
				})
				return err
			},
		},
		{
			name: "get client locator session_group_id",
			validate: func() *RequestError {
				_, err := ValidateGetTaskStatus(&pb.GetTaskStatusRequest{
					Locator: &pb.GetTaskStatusRequest_ClientMessageLocator{
						ClientMessageLocator: &pb.ClientMessageTaskLocator{
							SessionGroupId:  overCapID,
							ClientMessageId: "client-message-1",
						},
					},
				})
				return err
			},
		},
		{
			name: "get client locator client_message_id",
			validate: func() *RequestError {
				_, err := ValidateGetTaskStatus(&pb.GetTaskStatusRequest{
					Locator: &pb.GetTaskStatusRequest_ClientMessageLocator{
						ClientMessageLocator: &pb.ClientMessageTaskLocator{
							SessionGroupId:  "sg-1",
							ClientMessageId: overCapID,
						},
					},
				})
				return err
			},
		},
		{
			name: "get thread locator session_group_id",
			validate: func() *RequestError {
				_, err := ValidateGetTaskStatus(&pb.GetTaskStatusRequest{
					Locator: &pb.GetTaskStatusRequest_ThreadLocator{
						ThreadLocator: &pb.ThreadTaskLocator{
							SessionGroupId: overCapID,
							ThreadId:       "thread-1",
						},
					},
				})
				return err
			},
		},
		{
			name: "get thread locator thread_id",
			validate: func() *RequestError {
				_, err := ValidateGetTaskStatus(&pb.GetTaskStatusRequest{
					Locator: &pb.GetTaskStatusRequest_ThreadLocator{
						ThreadLocator: &pb.ThreadTaskLocator{
							SessionGroupId: "sg-1",
							ThreadId:       overCapID,
						},
					},
				})
				return err
			},
		},
		{
			name: "interrupt task locator task_id",
			validate: func() *RequestError {
				_, err := ValidateInterruptTask(&pb.InterruptTaskRequest{
					Locator: &pb.InterruptTaskRequest_TaskId{TaskId: overCapID},
				})
				return err
			},
		},
		{
			name: "interrupt client_request_id",
			validate: func() *RequestError {
				_, err := ValidateInterruptTask(&pb.InterruptTaskRequest{
					Locator:         &pb.InterruptTaskRequest_TaskId{TaskId: "task-1"},
					ClientRequestId: overCapID,
				})
				return err
			},
		},
		{
			name: "pending task_id",
			validate: func() *RequestError {
				req := basePendingResponseRequest(&pb.RespondPendingRequestRequest_Approval{
					Approval: &pb.ApprovalPendingResponse{DecisionId: "accept"},
				})
				req.TaskId = overCapID
				_, err := ValidateRespondPendingRequest(req)
				return err
			},
		},
		{
			name: "pending pending_request_id",
			validate: func() *RequestError {
				req := basePendingResponseRequest(&pb.RespondPendingRequestRequest_Approval{
					Approval: &pb.ApprovalPendingResponse{DecisionId: "accept"},
				})
				req.PendingRequestId = overCapID
				_, err := ValidateRespondPendingRequest(req)
				return err
			},
		},
		{
			name: "pending client_response_id",
			validate: func() *RequestError {
				req := basePendingResponseRequest(&pb.RespondPendingRequestRequest_Approval{
					Approval: &pb.ApprovalPendingResponse{DecisionId: "accept"},
				})
				req.ClientResponseId = overCapID
				_, err := ValidateRespondPendingRequest(req)
				return err
			},
		},
		{
			name: "approval decision_id",
			validate: func() *RequestError {
				_, err := ValidateRespondPendingRequest(basePendingResponseRequest(&pb.RespondPendingRequestRequest_Approval{
					Approval: &pb.ApprovalPendingResponse{DecisionId: overCapID},
				}))
				return err
			},
		},
		{
			name: "permission_id",
			validate: func() *RequestError {
				_, err := ValidateRespondPendingRequest(basePendingResponseRequest(&pb.RespondPendingRequestRequest_Permissions{
					Permissions: &pb.PermissionsPendingResponse{PermissionIds: []string{overCapID}},
				}))
				return err
			},
		},
		{
			name: "tool user input question_id",
			validate: func() *RequestError {
				_, err := ValidateRespondPendingRequest(basePendingResponseRequest(&pb.RespondPendingRequestRequest_ToolUserInput{
					ToolUserInput: &pb.ToolUserInputPendingResponse{
						Answers: []*pb.ToolUserInputAnswer{{QuestionId: overCapID, Answers: []string{"one"}}},
					},
				}))
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertResourceExhaustedRequestErrorDoesNotEcho(t, tt.name, tt.validate(), overCapID)
		})
	}
}

func TestValidateInboundPublicIDsRejectWhitespacePaddedOverCapRawValues(t *testing.T) {
	rawOverCapID := strings.Repeat(" ", domain.MaxPublicIDBytes) + "x"
	resolver := testResolver{
		"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
	}
	tests := []struct {
		name     string
		validate func() *RequestError
	}{
		{
			name: "start client_message_id",
			validate: func() *RequestError {
				req := validStartTaskRequest()
				req.ClientMessageId = rawOverCapID
				_, err := ValidateStartTask(req, resolver)
				return err
			},
		},
		{
			name: "locator session_group_id",
			validate: func() *RequestError {
				_, err := ValidateGetTaskStatus(&pb.GetTaskStatusRequest{
					Locator: &pb.GetTaskStatusRequest_ClientMessageLocator{
						ClientMessageLocator: &pb.ClientMessageTaskLocator{
							SessionGroupId:  rawOverCapID,
							ClientMessageId: "client-message-1",
						},
					},
				})
				return err
			},
		},
		{
			name: "approval decision_id",
			validate: func() *RequestError {
				_, err := ValidateRespondPendingRequest(basePendingResponseRequest(&pb.RespondPendingRequestRequest_Approval{
					Approval: &pb.ApprovalPendingResponse{DecisionId: rawOverCapID},
				}))
				return err
			},
		},
		{
			name: "permission_id",
			validate: func() *RequestError {
				_, err := ValidateRespondPendingRequest(basePendingResponseRequest(&pb.RespondPendingRequestRequest_Permissions{
					Permissions: &pb.PermissionsPendingResponse{PermissionIds: []string{rawOverCapID}},
				}))
				return err
			},
		},
		{
			name: "tool user input question_id",
			validate: func() *RequestError {
				_, err := ValidateRespondPendingRequest(basePendingResponseRequest(&pb.RespondPendingRequestRequest_ToolUserInput{
					ToolUserInput: &pb.ToolUserInputPendingResponse{
						Answers: []*pb.ToolUserInputAnswer{{QuestionId: rawOverCapID, Answers: []string{"one"}}},
					},
				}))
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertResourceExhaustedRequestErrorDoesNotEcho(t, tt.name, tt.validate(), rawOverCapID)
		})
	}
}

func TestValidateRespondPendingRequestRejectsOverCapPermissionIDs(t *testing.T) {
	permissionIDs := make([]string, domain.MaxPermissionAtoms+1)
	for index := range permissionIDs {
		permissionIDs[index] = "perm"
	}
	req := basePendingResponseRequest(&pb.RespondPendingRequestRequest_Permissions{
		Permissions: &pb.PermissionsPendingResponse{PermissionIds: permissionIDs},
	})

	if _, err := ValidateRespondPendingRequest(req); !hasRequestError(err, codes.ResourceExhausted, domain.ReasonResourceExhausted) {
		t.Fatalf("ValidateRespondPendingRequest() error = %#v, want resource_exhausted", err)
	}
}

func TestValidateMcpElicitationResponseContentBoundary(t *testing.T) {
	tests := []struct {
		name        string
		action      pb.McpElicitationAction
		contentJSON string
		wantErr     bool
	}{
		{name: "accept valid", action: pb.McpElicitationAction_MCP_ELICITATION_ACTION_ACCEPT, contentJSON: `{"ok":true}`},
		{name: "accept trailing json", action: pb.McpElicitationAction_MCP_ELICITATION_ACTION_ACCEPT, contentJSON: `{"ok":true} {"extra":true}`, wantErr: true},
		{name: "decline with content", action: pb.McpElicitationAction_MCP_ELICITATION_ACTION_DECLINE, contentJSON: `{"ok":false}`, wantErr: true},
		{name: "cancel with content", action: pb.McpElicitationAction_MCP_ELICITATION_ACTION_CANCEL, contentJSON: `{"ok":false}`, wantErr: true},
		{name: "decline empty content", action: pb.McpElicitationAction_MCP_ELICITATION_ACTION_DECLINE},
		{name: "cancel empty content", action: pb.McpElicitationAction_MCP_ELICITATION_ACTION_CANCEL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := basePendingResponseRequest(&pb.RespondPendingRequestRequest_McpElicitation{
				McpElicitation: &pb.McpElicitationPendingResponse{
					Action:      tt.action,
					ContentJson: tt.contentJSON,
				},
			})
			command, err := ValidateRespondPendingRequest(req)
			if tt.wantErr {
				if !hasRequestError(err, codes.InvalidArgument, domain.ReasonInvalidRequest) {
					t.Fatalf("ValidateRespondPendingRequest() error = %#v, want invalid_request", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateRespondPendingRequest() error = %v", err)
			}
			if command.Response.McpElicitation == nil {
				t.Fatal("McpElicitation response was not mapped")
			}
			if tt.action != pb.McpElicitationAction_MCP_ELICITATION_ACTION_ACCEPT && command.Response.McpElicitation.ContentJSON != "" {
				t.Fatalf("decline/cancel content = %q, want empty", command.Response.McpElicitation.ContentJSON)
			}
		})
	}
}

func TestValidateInterruptTaskLocator(t *testing.T) {
	if _, err := ValidateInterruptTask(&pb.InterruptTaskRequest{}); !hasRequestError(err, codes.InvalidArgument, domain.ReasonInvalidLocator) {
		t.Fatalf("ValidateInterruptTask() error = %#v, want invalid_locator", err)
	}

	command, err := ValidateInterruptTask(&pb.InterruptTaskRequest{
		Locator: &pb.InterruptTaskRequest_ClientMessageLocator{
			ClientMessageLocator: &pb.ClientMessageTaskLocator{
				SessionGroupId:  "sg-1",
				ClientMessageId: "client-message-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateInterruptTask() error = %v", err)
	}
	if command.Locator.Kind != domain.TaskLocatorByClientMessage || command.Locator.ClientMessageLocator.ClientMessageID != "client-message-1" {
		t.Fatalf("ValidateInterruptTask() = %#v, want client message locator", command)
	}
}

func TestApprovalWireDecisionMapping(t *testing.T) {
	tests := []struct {
		decision pb.ApprovalWireDecision
		wire     string
	}{
		{decision: pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT, wire: "accept"},
		{decision: pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT_FOR_SESSION, wire: "acceptForSession"},
		{decision: pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_DECLINE, wire: "decline"},
		{decision: pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_CANCEL, wire: "cancel"},
	}

	for _, tt := range tests {
		wire, err := ApprovalDecisionAppServerWire(tt.decision)
		if err != nil {
			t.Fatalf("ApprovalDecisionAppServerWire(%s) error = %v", tt.decision, err)
		}
		if wire != tt.wire {
			t.Fatalf("ApprovalDecisionAppServerWire(%s) = %q, want %q", tt.decision, wire, tt.wire)
		}
	}
}

func TestGatewayErrorDetailsMappingAndStatusDetails(t *testing.T) {
	for _, reason := range domain.CanonicalGatewayErrorReasons {
		t.Run(string(reason), func(t *testing.T) {
			details := domain.GatewayErrorDetails{
				Reason:           reason,
				DisplayMessage:   "redacted display",
				TaskID:           "task-1",
				SessionGroupID:   "sg-1",
				ClientMessageID:  "client-message-1",
				ClientResponseID: "client-response-1",
				PendingRequestID: "pending-1",
				ThreadID:         "thread-1",
				Retryable:        true,
			}

			roundTrip := GatewayErrorDetailsFromProto(GatewayErrorDetailsToProto(details))
			if !reflect.DeepEqual(roundTrip, details) {
				t.Fatalf("GatewayErrorDetails round trip = %#v, want %#v", roundTrip, details)
			}

			err := NewStatusError(codes.FailedPrecondition, details)
			st, ok := status.FromError(err)
			if !ok {
				t.Fatalf("status.FromError(%v) failed", err)
			}
			if st.Code() != codes.FailedPrecondition {
				t.Fatalf("status code = %s, want %s", st.Code(), codes.FailedPrecondition)
			}
			statusDetails := st.Details()
			if len(statusDetails) != 1 {
				t.Fatalf("status details count = %d, want 1", len(statusDetails))
			}
			protoDetails, ok := statusDetails[0].(*pb.GatewayErrorDetails)
			if !ok {
				t.Fatalf("status detail type = %T, want GatewayErrorDetails", statusDetails[0])
			}
			if protoDetails.GetReason() != string(reason) {
				t.Fatalf("status detail reason = %q, want %q", protoDetails.GetReason(), reason)
			}
		})
	}
}

func TestTaskEventPayloadMappingCoversCanonicalVariants(t *testing.T) {
	events := []domain.TaskEvent{
		eventWithPayload(domain.TaskLifecycleEvent{LifecycleEvent: domain.TaskLifecycleEventTaskStarted, State: domain.TaskStateStarting}),
		eventWithPayload(domain.AssistantDeltaEvent{TextDelta: "hi"}),
		eventWithPayload(domain.AssistantMessageCompletedEvent{Message: "done"}),
		eventWithPayload(domain.PlanUpdatedEvent{Explanation: "plan", Steps: []domain.PlanStep{{Step: "one", Status: "done"}}}),
		eventWithPayload(domain.ToolProgressEvent{ItemID: "item-1", ToolName: "shell", State: domain.ToolStateStarted}),
		eventWithPayload(domain.CommandStartedEvent{ItemID: "item-1", CommandDisplay: "go test"}),
		eventWithPayload(domain.CommandOutputDeltaEvent{ItemID: "item-1", Stream: domain.CommandOutputStreamCombined, Delta: "out"}),
		eventWithPayload(domain.FileDiffUpdatedEvent{ItemID: "item-1", FileLabel: "main.go", ChangeKind: "modified"}),
		eventWithPayload(domain.TurnDiffUpdatedEvent{DiffSummary: "summary"}),
		eventWithPayload(domain.PendingRequestCreatedEvent{PendingRequestID: "pending-1", PendingType: domain.PendingTypeCommandApproval, Display: sampleCommandApprovalDisplay()}),
		eventWithPayload(domain.PendingRequestResolvedEvent{PendingRequestID: "pending-1", PendingType: domain.PendingTypeCommandApproval, Resolution: domain.PendingResolutionDeclined}),
		eventWithPayload(domain.TaskTerminalEvent{TerminalState: domain.TerminalStateCompleted, ResultSummary: "ok"}),
		eventWithPayload(domain.GatewayWarningEvent{Code: "code", Message: "warning"}),
	}

	for _, event := range events {
		assertTaskEventPayloadArm(t, event)
	}

	turnDiffFields := pb.File_codex_control_v1_codex_control_proto.Messages().ByName("TurnDiffUpdatedEvent").Fields()
	if turnDiffFields.ByName("item_id") != nil {
		t.Fatal("TurnDiffUpdatedEvent exposes forbidden item_id")
	}
}

func TestReplayNoticeMappingFields(t *testing.T) {
	notice := domain.ReplayNotice{
		Code:                      domain.ReplayNoticeCursorEvicted,
		Message:                   "cursor was evicted",
		OldestBufferedEventID:     10,
		NewestBufferedEventID:     20,
		FromStartAvailable:        true,
		StartEvictedBeforeEventID: 9,
	}

	want := &pb.ReplayNotice{
		Code:                      pb.ReplayNoticeCode_REPLAY_NOTICE_CODE_CURSOR_EVICTED,
		Message:                   "cursor was evicted",
		OldestBufferedEventId:     10,
		NewestBufferedEventId:     20,
		FromStartAvailable:        true,
		StartEvictedBeforeEventId: 9,
	}
	got, ok := ReplayNoticeToProto(notice)
	if !ok {
		t.Fatal("ReplayNoticeToProto() failed")
	}
	if !proto.Equal(got, want) {
		t.Fatalf("ReplayNoticeToProto() = %v, want %v", got, want)
	}
	streamResponse, ok := StreamTaskResponseReplayNoticeToProto(notice)
	if !ok {
		t.Fatal("StreamTaskResponseReplayNoticeToProto() failed")
	}
	if got := streamResponse.GetReplayNotice(); !proto.Equal(got, want) {
		t.Fatalf("StreamTaskResponseReplayNoticeToProto() = %v, want %v", got, want)
	}
}

func TestStartTaskResponseToProtoRejectsMissingRequiredIDs(t *testing.T) {
	validResponse := domain.StartTaskResponse{
		TaskID:         "task-1",
		ThreadID:       "thread-1",
		TurnID:         "turn-1",
		SessionGroupID: "sg-1",
		State:          domain.TaskStateRunning,
	}

	tests := []struct {
		name   string
		mutate func(*domain.StartTaskResponse)
	}{
		{
			name: "task_id",
			mutate: func(response *domain.StartTaskResponse) {
				response.TaskID = ""
			},
		},
		{
			name: "thread_id",
			mutate: func(response *domain.StartTaskResponse) {
				response.ThreadID = ""
			},
		},
		{
			name: "turn_id",
			mutate: func(response *domain.StartTaskResponse) {
				response.TurnID = ""
			},
		},
		{
			name: "session_group_id",
			mutate: func(response *domain.StartTaskResponse) {
				response.SessionGroupID = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := validResponse
			tt.mutate(&response)
			if got, ok := StartTaskResponseToProto(response); ok || got != nil {
				t.Fatalf("StartTaskResponseToProto(empty %s) = (%v, %t), want (nil, false)", tt.name, got, ok)
			}
		})
	}
}

func TestDomainToProtoMappingRejectsUnknownRequiredEnums(t *testing.T) {
	if got, ok := StartTaskResponseToProto(domain.StartTaskResponse{State: domain.TaskState("unknown")}); ok || got != nil {
		t.Fatalf("StartTaskResponseToProto(unknown state) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := StreamTaskResponseReplayNoticeToProto(domain.ReplayNotice{Code: domain.ReplayNoticeCode("unknown")}); ok || got != nil {
		t.Fatalf("StreamTaskResponseReplayNoticeToProto(unknown code) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := TaskEventToProto(eventWithPayload(domain.TaskLifecycleEvent{
		LifecycleEvent: domain.TaskLifecycleEventType("unknown"),
		State:          domain.TaskStateRunning,
	})); ok || got != nil {
		t.Fatalf("TaskEventToProto(unknown lifecycle event) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := TaskEventToProto(eventWithPayload(domain.TaskLifecycleEvent{
		LifecycleEvent: domain.TaskLifecycleEventTaskStarted,
		State:          domain.TaskState("unknown"),
	})); ok || got != nil {
		t.Fatalf("TaskEventToProto(unknown lifecycle state) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := TaskEventToProto(eventWithPayload(domain.ToolProgressEvent{State: domain.ToolState("unknown")})); ok || got != nil {
		t.Fatalf("TaskEventToProto(unknown tool state) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := TaskEventToProto(eventWithPayload(domain.CommandOutputDeltaEvent{Stream: domain.CommandOutputStream("unknown")})); ok || got != nil {
		t.Fatalf("TaskEventToProto(unknown command stream) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := TaskEventToProto(eventWithPayload(domain.PendingRequestCreatedEvent{
		PendingType: domain.PendingType("unknown"),
		Display:     sampleCommandApprovalDisplay(),
	})); ok || got != nil {
		t.Fatalf("TaskEventToProto(unknown pending type) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := TaskEventToProto(eventWithPayload(domain.PendingRequestResolvedEvent{
		PendingType: domain.PendingTypeCommandApproval,
		Resolution:  domain.PendingResolution("unknown"),
	})); ok || got != nil {
		t.Fatalf("TaskEventToProto(unknown pending resolution) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := TaskEventToProto(eventWithPayload(domain.TaskTerminalEvent{TerminalState: domain.TerminalState("unknown")})); ok || got != nil {
		t.Fatalf("TaskEventToProto(unknown terminal state) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := PendingRequestToProto(domain.PendingRequest{
		PendingType: domain.PendingType("unknown"),
		Display:     sampleCommandApprovalDisplay(),
	}); ok || got != nil {
		t.Fatalf("PendingRequestToProto(unknown pending type) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := PendingRequestDisplayToProto(commandApprovalDisplayWithDecision(domain.ApprovalWireDecision("unknown"))); ok || got != nil {
		t.Fatalf("PendingRequestDisplayToProto(unknown command decision) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := PendingRequestDisplayToProto(commandApprovalDisplayWithOptions([]domain.ApprovalDecisionOption{
		{DecisionID: "advanced", Selectable: true, UnsupportedReason: "advanced_decision_out_of_mvp"},
	})); ok || got != nil {
		t.Fatalf("PendingRequestDisplayToProto(selectable empty decision) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := PendingRequestDisplayToProto(domain.FileChangeApprovalDisplay{
		DecisionOptions: []domain.ApprovalDecisionOption{{DecisionID: "bad", WireDecision: domain.ApprovalWireDecision("unknown")}},
	}); ok || got != nil {
		t.Fatalf("PendingRequestDisplayToProto(unknown file decision) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := PendingRequestDisplayToProto(domain.PermissionsApprovalDisplay{RecommendedScope: domain.PermissionScope("unknown")}); ok || got != nil {
		t.Fatalf("PendingRequestDisplayToProto(unknown permission scope) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := PendingRequestDisplayToProto(domain.McpElicitationDisplay{Mode: domain.ElicitationMode("unknown")}); ok || got != nil {
		t.Fatalf("PendingRequestDisplayToProto(unknown mcp mode) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := GetTaskStatusResponseToProto(domain.GetTaskStatusResponse{State: domain.TaskState("unknown")}); ok || got != nil {
		t.Fatalf("GetTaskStatusResponseToProto(unknown state) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := GetTaskStatusResponseToProto(domain.GetTaskStatusResponse{
		State:    domain.TaskStateRunning,
		Terminal: &domain.TaskTerminalEvent{TerminalState: domain.TerminalState("unknown")},
	}); ok || got != nil {
		t.Fatalf("GetTaskStatusResponseToProto(unknown terminal) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := InterruptTaskResponseToProto(domain.InterruptTaskResponse{State: domain.TaskState("unknown")}); ok || got != nil {
		t.Fatalf("InterruptTaskResponseToProto(unknown state) = (%v, %t), want (nil, false)", got, ok)
	}
}

func TestPendingDisplayMappingAllowsOptionalApprovalDecisionAndPermissionScope(t *testing.T) {
	command := mustPendingRequestDisplayToProto(t, commandApprovalDisplayWithOptions([]domain.ApprovalDecisionOption{
		{
			DecisionID:        "advanced",
			DisplayLabel:      "Review structured command",
			Selectable:        false,
			UnsupportedReason: "advanced_decision_out_of_mvp",
		},
		{
			DecisionID:   "cancel",
			WireDecision: domain.ApprovalWireDecisionCancel,
			Selectable:   true,
		},
	})).GetCommandApproval()
	option := command.GetDecisionOptions()[0]
	if option.GetWireDecision() != pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_UNSPECIFIED ||
		option.GetSelectable() ||
		option.GetUnsupportedReason() != "advanced_decision_out_of_mvp" {
		t.Fatalf("advanced decision option mapping = %v", option)
	}

	permissions := mustPendingRequestDisplayToProto(t, domain.PermissionsApprovalDisplay{
		RequestedPermissions: []domain.PermissionAtom{{PermissionID: "perm-1", Kind: "network"}},
	}).GetPermissionsApproval()
	if permissions.GetRecommendedScope() != pb.PermissionScope_PERMISSION_SCOPE_UNSPECIFIED {
		t.Fatalf("recommended scope = %s, want unspecified", permissions.GetRecommendedScope())
	}
}

func TestDomainToProtoMappingRejectsOverCapRepeatedFields(t *testing.T) {
	planSteps := make([]domain.PlanStep, domain.MaxPlanSteps+1)
	gotEvent, failure := TaskEventToProtoWithFailure(eventWithPayload(domain.PlanUpdatedEvent{Steps: planSteps}))
	assertResourceExhaustedMappingFailure(t, "TaskEventToProtoWithFailure(over-cap plan steps)", gotEvent, failure)

	approvalOptions := make([]domain.ApprovalDecisionOption, domain.MaxApprovalDecisionOptions+1)
	gotDisplay, failure := PendingRequestDisplayToProtoWithFailure(commandApprovalDisplayWithOptions(approvalOptions))
	assertResourceExhaustedMappingFailure(t, "PendingRequestDisplayToProtoWithFailure(over-cap approval options)", gotDisplay, failure)

	permissionAtoms := make([]domain.PermissionAtom, domain.MaxPermissionAtoms+1)
	gotDisplay, failure = PendingRequestDisplayToProtoWithFailure(domain.PermissionsApprovalDisplay{RequestedPermissions: permissionAtoms})
	assertResourceExhaustedMappingFailure(t, "PendingRequestDisplayToProtoWithFailure(over-cap permission atoms)", gotDisplay, failure)

	additionalFilesystemEntries := make([]domain.AdditionalFilesystemEntry, domain.MaxApprovalSecurityMetadataEntries+1)
	display := sampleCommandApprovalDisplay()
	display.ApprovalSecurity.AdditionalFilesystemEntries = additionalFilesystemEntries
	display.ApprovalSecurity.NetworkPolicyAmendmentSummaries = nil
	gotDisplay, failure = PendingRequestDisplayToProtoWithFailure(display)
	assertResourceExhaustedMappingFailure(t, "PendingRequestDisplayToProtoWithFailure(over-cap filesystem security metadata)", gotDisplay, failure)

	networkPolicySummaries := make([]domain.NetworkPolicyAmendmentSummary, domain.MaxApprovalSecurityMetadataEntries+1)
	display = sampleCommandApprovalDisplay()
	display.ApprovalSecurity.AdditionalFilesystemEntries = nil
	display.ApprovalSecurity.NetworkPolicyAmendmentSummaries = networkPolicySummaries
	gotDisplay, failure = PendingRequestDisplayToProtoWithFailure(display)
	assertResourceExhaustedMappingFailure(t, "PendingRequestDisplayToProtoWithFailure(over-cap network security metadata)", gotDisplay, failure)

	display = sampleCommandApprovalDisplay()
	display.ApprovalSecurity.AdditionalFilesystemEntries = make([]domain.AdditionalFilesystemEntry, domain.MaxApprovalSecurityMetadataEntries)
	display.ApprovalSecurity.NetworkPolicyAmendmentSummaries = []domain.NetworkPolicyAmendmentSummary{{HostLabel: "example.com"}}
	gotDisplay, failure = PendingRequestDisplayToProtoWithFailure(display)
	assertResourceExhaustedMappingFailure(t, "PendingRequestDisplayToProtoWithFailure(over-cap combined security metadata)", gotDisplay, failure)

	questions := make([]domain.ToolUserInputQuestion, domain.MaxToolUserInputQuestions+1)
	gotDisplay, failure = PendingRequestDisplayToProtoWithFailure(domain.ToolUserInputDisplay{Questions: questions})
	assertResourceExhaustedMappingFailure(t, "PendingRequestDisplayToProtoWithFailure(over-cap tool questions)", gotDisplay, failure)

	options := make([]string, domain.MaxToolUserInputOptionsPerQuestion+1)
	gotDisplay, failure = PendingRequestDisplayToProtoWithFailure(domain.ToolUserInputDisplay{
		Questions: []domain.ToolUserInputQuestion{{ID: "q1", Options: options}},
	})
	assertResourceExhaustedMappingFailure(t, "PendingRequestDisplayToProtoWithFailure(over-cap tool options)", gotDisplay, failure)
}

func TestTaskEventPayloadMappingFields(t *testing.T) {
	tests := []struct {
		name   string
		event  domain.TaskEvent
		assert func(*testing.T, *pb.TaskEvent)
	}{
		{
			name: "lifecycle",
			event: eventWithPayload(domain.TaskLifecycleEvent{
				LifecycleEvent: domain.TaskLifecycleEventThreadStarted,
				State:          domain.TaskStateRunning,
				ReasonCode:     "thread_ready",
				DisplayMessage: "thread ready",
			}),
			assert: func(t *testing.T, event *pb.TaskEvent) {
				got := event.GetLifecycle()
				if got.GetLifecycleEvent() != pb.TaskLifecycleEventType_TASK_LIFECYCLE_EVENT_TYPE_THREAD_STARTED ||
					got.GetState() != pb.TaskState_TASK_STATE_RUNNING ||
					got.GetReasonCode() != "thread_ready" ||
					got.GetDisplayMessage() != "thread ready" {
					t.Fatalf("lifecycle mapping = %v", got)
				}
			},
		},
		{
			name:  "assistant delta",
			event: eventWithPayload(domain.AssistantDeltaEvent{TextDelta: "hi", Truncated: true}),
			assert: func(t *testing.T, event *pb.TaskEvent) {
				got := event.GetAssistantDelta()
				if got.GetTextDelta() != "hi" || !got.GetTruncated() {
					t.Fatalf("assistant delta mapping = %v", got)
				}
			},
		},
		{
			name:  "assistant completed",
			event: eventWithPayload(domain.AssistantMessageCompletedEvent{Message: "done", Truncated: true}),
			assert: func(t *testing.T, event *pb.TaskEvent) {
				got := event.GetAssistantMessageCompleted()
				if got.GetMessage() != "done" || !got.GetTruncated() {
					t.Fatalf("assistant completed mapping = %v", got)
				}
			},
		},
		{
			name:  "plan updated",
			event: eventWithPayload(domain.PlanUpdatedEvent{Explanation: "plan", Steps: []domain.PlanStep{{Step: "one", Status: "done"}}}),
			assert: func(t *testing.T, event *pb.TaskEvent) {
				got := event.GetPlanUpdated()
				if got.GetExplanation() != "plan" || len(got.GetSteps()) != 1 ||
					got.GetSteps()[0].GetStep() != "one" ||
					got.GetSteps()[0].GetStatus() != "done" {
					t.Fatalf("plan mapping = %v", got)
				}
			},
		},
		{
			name:  "tool progress",
			event: eventWithPayload(domain.ToolProgressEvent{ItemID: "item-1", ToolName: "shell", State: domain.ToolStateStarted, Summary: "running"}),
			assert: func(t *testing.T, event *pb.TaskEvent) {
				got := event.GetToolProgress()
				if got.GetItemId() != "item-1" ||
					got.GetToolName() != "shell" ||
					got.GetState() != pb.ToolState_TOOL_STATE_STARTED ||
					got.GetSummary() != "running" {
					t.Fatalf("tool progress mapping = %v", got)
				}
			},
		},
		{
			name:  "command started",
			event: eventWithPayload(domain.CommandStartedEvent{ItemID: "item-1", CommandDisplay: "go test", WorkspaceLabel: "."}),
			assert: func(t *testing.T, event *pb.TaskEvent) {
				got := event.GetCommandStarted()
				if got.GetItemId() != "item-1" ||
					got.GetCommandDisplay() != "go test" ||
					got.GetWorkspaceLabel() != "." {
					t.Fatalf("command started mapping = %v", got)
				}
			},
		},
		{
			name:  "command output",
			event: eventWithPayload(domain.CommandOutputDeltaEvent{ItemID: "item-1", Stream: domain.CommandOutputStreamCombined, Delta: "out", Truncated: true}),
			assert: func(t *testing.T, event *pb.TaskEvent) {
				got := event.GetCommandOutputDelta()
				if got.GetItemId() != "item-1" ||
					got.GetStream() != pb.CommandOutputStream_COMMAND_OUTPUT_STREAM_COMBINED ||
					got.GetDelta() != "out" ||
					!got.GetTruncated() {
					t.Fatalf("command output mapping = %v", got)
				}
			},
		},
		{
			name:  "file diff",
			event: eventWithPayload(domain.FileDiffUpdatedEvent{ItemID: "item-1", FileLabel: "main.go", ChangeKind: "modified", DiffSummary: "summary", DiffUnified: "diff", Truncated: true}),
			assert: func(t *testing.T, event *pb.TaskEvent) {
				got := event.GetFileDiffUpdated()
				if got.GetItemId() != "item-1" ||
					got.GetFileLabel() != "main.go" ||
					got.GetChangeKind() != "modified" ||
					got.GetDiffSummary() != "summary" ||
					got.GetDiffUnified() != "diff" ||
					!got.GetTruncated() {
					t.Fatalf("file diff mapping = %v", got)
				}
			},
		},
		{
			name:  "turn diff",
			event: eventWithPayload(domain.TurnDiffUpdatedEvent{DiffSummary: "summary", DiffUnified: "--- a\n+++ b", Truncated: true}),
			assert: func(t *testing.T, event *pb.TaskEvent) {
				got := event.GetTurnDiffUpdated()
				if got.GetDiffSummary() != "summary" ||
					got.GetDiffUnified() != "--- a\n+++ b" ||
					!got.GetTruncated() {
					t.Fatalf("turn diff mapping = %v", got)
				}
			},
		},
		{
			name:  "pending created",
			event: eventWithPayload(domain.PendingRequestCreatedEvent{PendingRequestID: "pending-1", PendingType: domain.PendingTypeCommandApproval, Display: sampleCommandApprovalDisplay()}),
			assert: func(t *testing.T, event *pb.TaskEvent) {
				got := event.GetPendingRequestCreated()
				if got.GetPendingRequestId() != "pending-1" ||
					got.GetPendingType() != pb.PendingType_PENDING_TYPE_COMMAND_APPROVAL ||
					got.GetDisplay().GetCommandApproval().GetDecisionOptions()[0].GetWireDecision() != pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT_FOR_SESSION {
					t.Fatalf("pending created mapping = %v", got)
				}
			},
		},
		{
			name: "pending resolved",
			event: eventWithPayload(domain.PendingRequestResolvedEvent{
				PendingRequestID: "pending-1",
				PendingType:      domain.PendingTypePermissionsApproval,
				Resolution:       domain.PendingResolutionGranted,
				DisplayMessage:   "granted",
			}),
			assert: func(t *testing.T, event *pb.TaskEvent) {
				got := event.GetPendingRequestResolved()
				if got.GetPendingType() != pb.PendingType_PENDING_TYPE_PERMISSIONS_APPROVAL ||
					got.GetResolution() != pb.PendingResolution_PENDING_RESOLUTION_GRANTED ||
					got.GetDisplayMessage() != "granted" {
					t.Fatalf("pending resolved mapping = %v", got)
				}
			},
		},
		{
			name:  "terminal",
			event: eventWithPayload(domain.TaskTerminalEvent{TerminalState: domain.TerminalStateCompleted, ResultSummary: "ok", ErrorMessage: "none"}),
			assert: func(t *testing.T, event *pb.TaskEvent) {
				got := event.GetTerminal()
				if got.GetTerminalState() != pb.TerminalState_TERMINAL_STATE_COMPLETED ||
					got.GetResultSummary() != "ok" ||
					got.GetErrorMessage() != "none" {
					t.Fatalf("terminal mapping = %v", got)
				}
			},
		},
		{
			name: "warning",
			event: eventWithPayload(domain.GatewayWarningEvent{
				Code:           "code",
				Message:        "warning",
				RequestType:    "file_approval",
				AutoResolution: "decline",
				LimitReason:    "display_payload_too_large",
			}),
			assert: func(t *testing.T, event *pb.TaskEvent) {
				got := event.GetGatewayWarning()
				if got.GetCode() != "code" ||
					got.GetMessage() != "warning" ||
					got.GetRequestType() != "file_approval" ||
					got.GetAutoResolution() != "decline" ||
					got.GetLimitReason() != "display_payload_too_large" {
					t.Fatalf("warning mapping = %v", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.assert(t, mustTaskEventToProto(t, tt.event))
		})
	}
}

func TestTaskEventMappingRejectsInvalidPayloads(t *testing.T) {
	if got, ok := TaskEventToProto(eventWithPayload(nil)); ok || got != nil {
		t.Fatalf("TaskEventToProto(nil payload) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := StreamTaskResponseEventToProto(eventWithPayload(nil)); ok || got != nil {
		t.Fatalf("StreamTaskResponseEventToProto(nil payload) = (%v, %t), want (nil, false)", got, ok)
	}

	var typedNil *domain.AssistantDeltaEvent
	if got, ok := TaskEventToProto(eventWithPayload(typedNil)); ok || got != nil {
		t.Fatalf("TaskEventToProto(typed nil payload) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := TaskEventToProto(eventWithPayload(unsupportedTaskEventPayload{})); ok || got != nil {
		t.Fatalf("TaskEventToProto(unsupported payload) = (%v, %t), want (nil, false)", got, ok)
	}
}

func TestTaskEventMappingAcceptsPointerPayloads(t *testing.T) {
	got := mustTaskEventToProto(t, eventWithPayload(&domain.AssistantDeltaEvent{
		TextDelta: "pointer payload",
		Truncated: true,
	}))

	payload, ok := got.GetPayload().(*pb.TaskEvent_AssistantDelta)
	if !ok {
		t.Fatalf("payload arm = %T, want *pb.TaskEvent_AssistantDelta", got.GetPayload())
	}
	if payload.AssistantDelta.GetTextDelta() != "pointer payload" || !payload.AssistantDelta.GetTruncated() {
		t.Fatalf("assistant delta pointer mapping = %v", payload.AssistantDelta)
	}
}

func TestPendingDisplayMappingCoversCanonicalVariants(t *testing.T) {
	displays := []domain.PendingRequestDisplay{
		sampleCommandApprovalDisplay(),
		domain.FileChangeApprovalDisplay{
			FileLabel:  "main.go",
			ChangeKind: "modified",
			GrantRoot: &domain.FileGrantRootDisplay{
				Present:            true,
				RootLabel:          ".",
				UnderConfiguredCWD: true,
				Approvable:         true,
				UnapprovableReason: "",
			},
			DecisionOptions: []domain.ApprovalDecisionOption{{DecisionID: "cancel", WireDecision: domain.ApprovalWireDecisionCancel, Selectable: true}},
		},
		domain.PermissionsApprovalDisplay{
			RequestedPermissions: []domain.PermissionAtom{
				{PermissionID: "perm-1", Kind: "network", DisplayLabel: "network", Grantable: true},
			},
			RecommendedScope: domain.PermissionScopeSession,
		},
		domain.McpElicitationDisplay{Mode: domain.ElicitationModeForm, Message: "fill form", FormSchemaJSON: `{"type":"object"}`},
		domain.ToolUserInputDisplay{Questions: []domain.ToolUserInputQuestion{{ID: "q1", Question: "value?", Options: []string{"a"}}}},
	}

	for _, display := range displays {
		assertPendingDisplayPayloadArm(t, display)
	}
}

func TestPendingDisplayMappingFields(t *testing.T) {
	command := mustPendingRequestDisplayToProto(t, sampleCommandApprovalDisplay()).GetCommandApproval()
	if command.GetCommandDisplay() != "go test ./..." ||
		command.GetApprovalSecurity().GetNetworkContext().GetHostLabel() != "example.com" ||
		command.GetDecisionOptions()[0].GetWireDecision() != pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT_FOR_SESSION {
		t.Fatalf("command approval mapping = %v", command)
	}

	fileDisplay := domain.FileChangeApprovalDisplay{
		FileLabel:       "main.go",
		ChangeKind:      "modified",
		DiffUnavailable: true,
		GrantRoot: &domain.FileGrantRootDisplay{
			Present:            true,
			RootLabel:          ".",
			UnderConfiguredCWD: true,
			Approvable:         false,
			UnapprovableReason: "outside workspace",
		},
		DecisionOptions: []domain.ApprovalDecisionOption{{DecisionID: "cancel", WireDecision: domain.ApprovalWireDecisionCancel, Selectable: true}},
	}
	file := mustPendingRequestDisplayToProto(t, fileDisplay).GetFileChangeApproval()
	if file.GetFileLabel() != "main.go" ||
		!file.GetDiffUnavailable() ||
		file.GetGrantRoot().GetUnapprovableReason() != "outside workspace" ||
		file.GetDecisionOptions()[0].GetWireDecision() != pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_CANCEL {
		t.Fatalf("file approval mapping = %v", file)
	}

	permissions := mustPendingRequestDisplayToProto(t, domain.PermissionsApprovalDisplay{
		RequestedPermissions: []domain.PermissionAtom{{PermissionID: "perm-1", Kind: "network", DisplayLabel: "network", Grantable: true}},
		RecommendedScope:     domain.PermissionScopeSession,
		Reason:               "needed",
	}).GetPermissionsApproval()
	if permissions.GetRecommendedScope() != pb.PermissionScope_PERMISSION_SCOPE_SESSION ||
		permissions.GetRequestedPermissions()[0].GetPermissionId() != "perm-1" ||
		permissions.GetReason() != "needed" {
		t.Fatalf("permissions approval mapping = %v", permissions)
	}

	mcp := mustPendingRequestDisplayToProto(t, domain.McpElicitationDisplay{
		Mode:           domain.ElicitationModeForm,
		Message:        "fill form",
		FormSchemaJSON: `{"type":"object"}`,
		URL:            "https://example.com/form",
		SubmitLabel:    "Submit",
	}).GetMcpElicitation()
	if mcp.GetMode() != pb.ElicitationMode_ELICITATION_MODE_FORM ||
		mcp.GetMessage() != "fill form" ||
		mcp.GetFormSchemaJson() != `{"type":"object"}` ||
		mcp.GetUrl() != "https://example.com/form" ||
		mcp.GetSubmitLabel() != "Submit" {
		t.Fatalf("mcp elicitation mapping = %v", mcp)
	}

	toolInput := mustPendingRequestDisplayToProto(t, domain.ToolUserInputDisplay{
		Questions: []domain.ToolUserInputQuestion{
			{ID: "q1", Header: "Header", Question: "Question?", IsOther: true, IsSecret: true, Options: []string{"a", "b"}},
		},
	}).GetToolUserInput()
	if len(toolInput.GetQuestions()) != 1 ||
		toolInput.GetQuestions()[0].GetId() != "q1" ||
		toolInput.GetQuestions()[0].GetHeader() != "Header" ||
		toolInput.GetQuestions()[0].GetQuestion() != "Question?" ||
		!toolInput.GetQuestions()[0].GetIsOther() ||
		!toolInput.GetQuestions()[0].GetIsSecret() ||
		!reflect.DeepEqual(toolInput.GetQuestions()[0].GetOptions(), []string{"a", "b"}) {
		t.Fatalf("tool user input mapping = %v", toolInput)
	}
}

func TestPendingDisplayMappingRejectsInvalidPayloads(t *testing.T) {
	if got, ok := PendingRequestDisplayToProto(nil); ok || got != nil {
		t.Fatalf("PendingRequestDisplayToProto(nil display) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := PendingRequestToProto(domain.PendingRequest{Display: nil}); ok || got != nil {
		t.Fatalf("PendingRequestToProto(nil display) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := GetTaskStatusResponseToProto(domain.GetTaskStatusResponse{
		ActivePendingRequests: []domain.PendingRequest{{Display: nil}},
	}); ok || got != nil {
		t.Fatalf("GetTaskStatusResponseToProto(nil active pending display) = (%v, %t), want (nil, false)", got, ok)
	}

	var typedNil *domain.CommandApprovalDisplay
	if got, ok := PendingRequestDisplayToProto(typedNil); ok || got != nil {
		t.Fatalf("PendingRequestDisplayToProto(typed nil display) = (%v, %t), want (nil, false)", got, ok)
	}
	if got, ok := PendingRequestDisplayToProto(unsupportedPendingRequestDisplay{}); ok || got != nil {
		t.Fatalf("PendingRequestDisplayToProto(unsupported display) = (%v, %t), want (nil, false)", got, ok)
	}
}

func TestPendingDisplayMappingAcceptsPointerPayloads(t *testing.T) {
	display := sampleCommandApprovalDisplay()
	display.CommandDisplay = "pointer command"
	got := mustPendingRequestDisplayToProto(t, &display)

	payload, ok := got.GetPayload().(*pb.PendingRequestDisplay_CommandApproval)
	if !ok {
		t.Fatalf("display payload arm = %T, want *pb.PendingRequestDisplay_CommandApproval", got.GetPayload())
	}
	if payload.CommandApproval.GetCommandDisplay() != "pointer command" ||
		payload.CommandApproval.GetDecisionOptions()[0].GetWireDecision() != pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT_FOR_SESSION {
		t.Fatalf("command display pointer mapping = %v", payload.CommandApproval)
	}
}

func TestStatusResponseCanCarryActivePendingWithoutReplayData(t *testing.T) {
	response := domain.GetTaskStatusResponse{
		TaskID:         "task-1",
		State:          domain.TaskStateWaitingForPendingRequest,
		SessionGroupID: "sg-1",
		ActivePendingRequests: []domain.PendingRequest{
			{
				PendingRequestID: "pending-1",
				TaskID:           "task-1",
				PendingType:      domain.PendingTypeCommandApproval,
				Display:          sampleCommandApprovalDisplay(),
			},
		},
	}

	got, ok := GetTaskStatusResponseToProto(response)
	if !ok {
		t.Fatal("GetTaskStatusResponseToProto() failed")
	}
	if len(got.GetActivePendingRequests()) != 1 {
		t.Fatalf("active pending count = %d, want 1", len(got.GetActivePendingRequests()))
	}
	if got.GetOldestBufferedEventId() != 0 || got.GetNewestBufferedEventId() != 0 {
		t.Fatalf("status replay ids = %d/%d, want zero values", got.GetOldestBufferedEventId(), got.GetNewestBufferedEventId())
	}
	pendingDisplay := got.GetActivePendingRequests()[0].GetDisplay().GetCommandApproval()
	if pendingDisplay.GetDecisionOptions()[0].GetWireDecision() != pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT_FOR_SESSION {
		t.Fatalf("status active pending decision = %s, want accept_for_session", pendingDisplay.GetDecisionOptions()[0].GetWireDecision())
	}
}

func basePendingResponseRequest(response isPendingResponseForTest) *pb.RespondPendingRequestRequest {
	req := &pb.RespondPendingRequestRequest{
		TaskId:           "task-1",
		PendingRequestId: "pending-1",
		ClientResponseId: "client-response-1",
	}
	switch typed := response.(type) {
	case *pb.RespondPendingRequestRequest_Approval:
		req.Response = typed
	case *pb.RespondPendingRequestRequest_Permissions:
		req.Response = typed
	case *pb.RespondPendingRequestRequest_McpElicitation:
		req.Response = typed
	case *pb.RespondPendingRequestRequest_ToolUserInput:
		req.Response = typed
	}
	return req
}

type isPendingResponseForTest interface{}

type unsupportedTaskEventPayload struct {
	domain.AssistantDeltaEvent
}

type unsupportedPendingRequestDisplay struct {
	domain.CommandApprovalDisplay
}

func eventWithPayload(payload domain.TaskEventPayload) domain.TaskEvent {
	return domain.TaskEvent{
		EventID:        1,
		TaskID:         "task-1",
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		ThreadID:       "thread-1",
		TurnID:         "turn-1",
		Payload:        payload,
	}
}

func mustTaskEventToProto(t *testing.T, event domain.TaskEvent) *pb.TaskEvent {
	t.Helper()

	got, ok := TaskEventToProto(event)
	if !ok {
		t.Fatalf("TaskEventToProto(%T) failed", event.Payload)
	}
	if got.GetPayload() == nil {
		t.Fatalf("TaskEventToProto(%T) produced empty payload", event.Payload)
	}
	return got
}

func mustPendingRequestDisplayToProto(t *testing.T, display domain.PendingRequestDisplay) *pb.PendingRequestDisplay {
	t.Helper()

	got, ok := PendingRequestDisplayToProto(display)
	if !ok {
		t.Fatalf("PendingRequestDisplayToProto(%T) failed", display)
	}
	if got.GetPayload() == nil {
		t.Fatalf("PendingRequestDisplayToProto(%T) produced empty payload", display)
	}
	return got
}

func assertResourceExhaustedMappingFailure(t *testing.T, name string, protoValue any, failure *MappingFailure) {
	t.Helper()

	if protoValue != nil && !reflect.ValueOf(protoValue).IsNil() {
		t.Fatalf("%s proto = %v, want nil", name, protoValue)
	}
	if failure == nil {
		t.Fatalf("%s failure = nil, want resource_exhausted", name)
	}
	if failure.Reason != domain.ReasonResourceExhausted {
		t.Fatalf("%s failure reason = %s, want %s", name, failure.Reason, domain.ReasonResourceExhausted)
	}
}

func assertTaskEventPayloadArm(t *testing.T, event domain.TaskEvent) {
	t.Helper()

	got := mustTaskEventToProto(t, event)
	switch event.Payload.(type) {
	case domain.TaskLifecycleEvent, *domain.TaskLifecycleEvent:
		if _, ok := got.GetPayload().(*pb.TaskEvent_Lifecycle); !ok {
			t.Fatalf("payload arm = %T, want lifecycle", got.GetPayload())
		}
	case domain.AssistantDeltaEvent, *domain.AssistantDeltaEvent:
		if _, ok := got.GetPayload().(*pb.TaskEvent_AssistantDelta); !ok {
			t.Fatalf("payload arm = %T, want assistant_delta", got.GetPayload())
		}
	case domain.AssistantMessageCompletedEvent, *domain.AssistantMessageCompletedEvent:
		if _, ok := got.GetPayload().(*pb.TaskEvent_AssistantMessageCompleted); !ok {
			t.Fatalf("payload arm = %T, want assistant_message_completed", got.GetPayload())
		}
	case domain.PlanUpdatedEvent, *domain.PlanUpdatedEvent:
		if _, ok := got.GetPayload().(*pb.TaskEvent_PlanUpdated); !ok {
			t.Fatalf("payload arm = %T, want plan_updated", got.GetPayload())
		}
	case domain.ToolProgressEvent, *domain.ToolProgressEvent:
		if _, ok := got.GetPayload().(*pb.TaskEvent_ToolProgress); !ok {
			t.Fatalf("payload arm = %T, want tool_progress", got.GetPayload())
		}
	case domain.CommandStartedEvent, *domain.CommandStartedEvent:
		if _, ok := got.GetPayload().(*pb.TaskEvent_CommandStarted); !ok {
			t.Fatalf("payload arm = %T, want command_started", got.GetPayload())
		}
	case domain.CommandOutputDeltaEvent, *domain.CommandOutputDeltaEvent:
		if _, ok := got.GetPayload().(*pb.TaskEvent_CommandOutputDelta); !ok {
			t.Fatalf("payload arm = %T, want command_output_delta", got.GetPayload())
		}
	case domain.FileDiffUpdatedEvent, *domain.FileDiffUpdatedEvent:
		if _, ok := got.GetPayload().(*pb.TaskEvent_FileDiffUpdated); !ok {
			t.Fatalf("payload arm = %T, want file_diff_updated", got.GetPayload())
		}
	case domain.TurnDiffUpdatedEvent, *domain.TurnDiffUpdatedEvent:
		if _, ok := got.GetPayload().(*pb.TaskEvent_TurnDiffUpdated); !ok {
			t.Fatalf("payload arm = %T, want turn_diff_updated", got.GetPayload())
		}
	case domain.PendingRequestCreatedEvent, *domain.PendingRequestCreatedEvent:
		if _, ok := got.GetPayload().(*pb.TaskEvent_PendingRequestCreated); !ok {
			t.Fatalf("payload arm = %T, want pending_request_created", got.GetPayload())
		}
	case domain.PendingRequestResolvedEvent, *domain.PendingRequestResolvedEvent:
		if _, ok := got.GetPayload().(*pb.TaskEvent_PendingRequestResolved); !ok {
			t.Fatalf("payload arm = %T, want pending_request_resolved", got.GetPayload())
		}
	case domain.TaskTerminalEvent, *domain.TaskTerminalEvent:
		if _, ok := got.GetPayload().(*pb.TaskEvent_Terminal); !ok {
			t.Fatalf("payload arm = %T, want terminal", got.GetPayload())
		}
	case domain.GatewayWarningEvent, *domain.GatewayWarningEvent:
		if _, ok := got.GetPayload().(*pb.TaskEvent_GatewayWarning); !ok {
			t.Fatalf("payload arm = %T, want gateway_warning", got.GetPayload())
		}
	default:
		t.Fatalf("unhandled test payload type %T", event.Payload)
	}
}

func assertPendingDisplayPayloadArm(t *testing.T, display domain.PendingRequestDisplay) {
	t.Helper()

	got := mustPendingRequestDisplayToProto(t, display)
	switch display.(type) {
	case domain.CommandApprovalDisplay, *domain.CommandApprovalDisplay:
		if _, ok := got.GetPayload().(*pb.PendingRequestDisplay_CommandApproval); !ok {
			t.Fatalf("display payload arm = %T, want command_approval", got.GetPayload())
		}
	case domain.FileChangeApprovalDisplay, *domain.FileChangeApprovalDisplay:
		if _, ok := got.GetPayload().(*pb.PendingRequestDisplay_FileChangeApproval); !ok {
			t.Fatalf("display payload arm = %T, want file_change_approval", got.GetPayload())
		}
	case domain.PermissionsApprovalDisplay, *domain.PermissionsApprovalDisplay:
		if _, ok := got.GetPayload().(*pb.PendingRequestDisplay_PermissionsApproval); !ok {
			t.Fatalf("display payload arm = %T, want permissions_approval", got.GetPayload())
		}
	case domain.McpElicitationDisplay, *domain.McpElicitationDisplay:
		if _, ok := got.GetPayload().(*pb.PendingRequestDisplay_McpElicitation); !ok {
			t.Fatalf("display payload arm = %T, want mcp_elicitation", got.GetPayload())
		}
	case domain.ToolUserInputDisplay, *domain.ToolUserInputDisplay:
		if _, ok := got.GetPayload().(*pb.PendingRequestDisplay_ToolUserInput); !ok {
			t.Fatalf("display payload arm = %T, want tool_user_input", got.GetPayload())
		}
	default:
		t.Fatalf("unhandled test display type %T", display)
	}
}

func commandApprovalDisplayWithDecision(decision domain.ApprovalWireDecision) domain.CommandApprovalDisplay {
	return commandApprovalDisplayWithOptions([]domain.ApprovalDecisionOption{{DecisionID: "decision-1", WireDecision: decision}})
}

func commandApprovalDisplayWithOptions(options []domain.ApprovalDecisionOption) domain.CommandApprovalDisplay {
	display := sampleCommandApprovalDisplay()
	display.DecisionOptions = options
	return display
}

func sampleCommandApprovalDisplay() domain.CommandApprovalDisplay {
	return domain.CommandApprovalDisplay{
		CommandDisplay: "go test ./...",
		WorkspaceLabel: ".",
		ApprovalSecurity: &domain.ApprovalSecurityMetadata{
			HasPrivilegeExpansion: true,
			NetworkContext:        &domain.NetworkContextDisplay{HostLabel: "example.com", Protocol: "https"},
			AdditionalNetwork:     &domain.AdditionalNetworkDisplay{Enabled: true},
			AdditionalFilesystemEntries: []domain.AdditionalFilesystemEntry{
				{EntryID: "fs-1", Access: "read", PathLabel: "README.md", Approvable: true},
			},
			ExecPolicyAmendmentSummary: &domain.ExecPolicyAmendmentSummary{CommandDisplay: "go test", Truncated: false},
			NetworkPolicyAmendmentSummaries: []domain.NetworkPolicyAmendmentSummary{
				{HostLabel: "example.com", Action: "allow", Approvable: false},
			},
		},
		DecisionOptions: []domain.ApprovalDecisionOption{
			{
				DecisionID:   "accept-session",
				WireDecision: domain.ApprovalWireDecisionAcceptForSession,
				DisplayLabel: "Accept for session",
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
}

func hasRequestError(err *RequestError, code codes.Code, reason domain.GatewayErrorReason) bool {
	if err == nil {
		return false
	}
	return err.Code == code && err.Details.Reason == reason
}

func assertResourceExhaustedRequestErrorDoesNotEcho(t *testing.T, name string, err *RequestError, rawValue string) {
	t.Helper()

	if !hasRequestError(err, codes.ResourceExhausted, domain.ReasonResourceExhausted) {
		if err == nil {
			t.Fatalf("%s error = nil, want resource_exhausted", name)
		}
		t.Fatalf("%s error code/reason = %s/%s, want %s/%s", name, err.Code, err.Details.Reason, codes.ResourceExhausted, domain.ReasonResourceExhausted)
	}
	if strings.Contains(err.Error(), rawValue) || strings.Contains(err.Details.DisplayMessage, rawValue) {
		t.Fatalf("%s error echoed an over-cap public id", name)
	}
}

func validStartTaskRequest() *pb.StartTaskRequest {
	return &pb.StartTaskRequest{
		SessionGroupId:  "sg-1",
		WorkspaceId:     "ws-1",
		Prompt:          "hello",
		ClientMessageId: "client-message-1",
		ContextBlocks: []*pb.ContextBlock{
			{
				Kind:        pb.ContextBlockKind_CONTEXT_BLOCK_KIND_APPLICATION,
				SourceLabel: "ticket",
				SourceUri:   "https://example.com/source",
				MimeType:    "text/plain",
				Content:     "context",
			},
		},
	}
}

func TestStatusErrorFromRequestErrorNil(t *testing.T) {
	if err := StatusErrorFromRequestError(nil); err != nil {
		t.Fatalf("StatusErrorFromRequestError(nil) = %v, want nil", err)
	}
}

func TestStatusErrorFromRequestError(t *testing.T) {
	requestErr := &RequestError{
		Code: codes.InvalidArgument,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonInvalidRequest,
			DisplayMessage: "invalid",
		},
	}

	err := StatusErrorFromRequestError(requestErr)
	if err == nil {
		t.Fatal("StatusErrorFromRequestError() returned nil")
	}
	if errors.Is(err, requestErr) {
		t.Fatal("StatusErrorFromRequestError() should return a gRPC status error, not the request error")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Fatalf("status = %v, %t; want InvalidArgument", st.Code(), ok)
	}
}
