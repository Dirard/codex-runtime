package grpcapi

import (
	"strings"
	"testing"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestChatRuntimeServiceContractShape(t *testing.T) {
	file := pb.File_codex_control_v1_codex_control_proto
	services := file.Services()
	control := services.ByName("CodexControl")
	if control == nil {
		t.Fatal("CodexControl service missing")
	}
	chat := services.ByName("ChatRuntimeService")
	if chat == nil {
		t.Fatal("ChatRuntimeService service missing")
	}
	workflow := services.ByName("WorkflowRuntimeService")
	if workflow == nil {
		t.Fatal("WorkflowRuntimeService service missing")
	}
	if control.FullName() == chat.FullName() {
		t.Fatalf("chat runtime service must be separate from task service: %s", chat.FullName())
	}
	if workflow.FullName() == control.FullName() || workflow.FullName() == chat.FullName() {
		t.Fatalf("workflow runtime service must be additive and separate: %s", workflow.FullName())
	}

	assertServiceMethods(t, chat, []protoreflect.Name{
		"StartChatRun",
		"GetChat",
		"RunChatTurn",
		"GetChatStatus",
		"GetChatHistory",
		"StreamChatEvents",
		"RespondChatPending",
		"InterruptChatRun",
	})
	assertServiceMethods(t, control, []protoreflect.Name{
		"StartTask",
		"StreamTask",
		"RespondPendingRequest",
		"InterruptTask",
		"GetTaskStatus",
	})
	assertServiceMethods(t, workflow, []protoreflect.Name{
		"InitWorkflow",
		"GetWorkflow",
		"GetWorkflowStatus",
		"RestartWorkflow",
		"DeleteWorkflow",
		"StartWorkflowChatRun",
		"RunWorkflowChatTurn",
		"GetWorkflowChat",
		"GetWorkflowChatHistory",
		"StreamWorkflowChatEvents",
		"RespondWorkflowChatPending",
		"InterruptWorkflowChatRun",
	})
}

func TestWorkflowRuntimeContractCarriesLifecyclePackageAndSafeErrors(t *testing.T) {
	file := pb.File_codex_control_v1_codex_control_proto
	messages := file.Messages()
	assertMessageFields(t, messages.ByName("WorkflowPackage"), []protoreflect.Name{
		"schema_version",
		"workflow",
		"package_fingerprint",
		"files",
		"total_size_bytes",
	})
	assertMessageFields(t, messages.ByName("WorkflowPackageFile"), []protoreflect.Name{
		"relative_path",
		"contents",
		"size_bytes",
		"sha256",
		"executable",
	})
	assertMessageFields(t, messages.ByName("WorkflowStatus"), []protoreflect.Name{
		"workflow",
		"lifecycle",
		"active_package_fingerprint",
		"pending_package_fingerprint",
		"restart_required",
		"mcp_reload_state",
		"last_error",
	})
	assertMessageFields(t, messages.ByName("WorkflowErrorDetails"), []protoreflect.Name{
		"outcome",
		"code",
		"reason",
		"display_message",
		"retryable",
		"next_action",
		"safe_metadata",
	})
}

func TestChatRuntimeEventContractHasNoRawPayloadEscapeHatch(t *testing.T) {
	file := pb.File_codex_control_v1_codex_control_proto
	chatEvent := file.Messages().ByName("ChatEvent")
	if chatEvent == nil {
		t.Fatal("ChatEvent message missing")
	}
	payload := chatEvent.Oneofs().ByName("payload")
	if payload == nil {
		t.Fatal("ChatEvent payload oneof missing")
	}

	fields := chatEvent.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		if field.ContainingOneof() != payload {
			continue
		}
		assertNoRawPayloadField(t, field)
	}
}

func TestPublicProtoContractHasNoRawPayloadEscapeHatch(t *testing.T) {
	file := pb.File_codex_control_v1_codex_control_proto
	messages := file.Messages()
	for i := 0; i < messages.Len(); i++ {
		assertMessageTreeHasNoRawPayloadNames(t, messages.Get(i))
	}
}

func TestChatRuntimeIdentityAndErrorDetailsAreChatFirst(t *testing.T) {
	file := pb.File_codex_control_v1_codex_control_proto
	assertMessageFields(t, file.Messages().ByName("StartChatRunResponse"), []protoreflect.Name{
		"chat_id",
		"run_id",
		"status",
		"first_turn_accepted",
	})
	assertMessageFields(t, file.Messages().ByName("GetChatHistoryResponse"), []protoreflect.Name{
		"chat_id",
		"turns",
		"returned_depth",
		"capability",
	})
	assertMessageFields(t, file.Messages().ByName("ChatRuntimeErrorDetails"), []protoreflect.Name{
		"outcome",
		"reason",
		"chat_id",
		"run_id",
		"pending_request_id",
	})
}

func assertServiceMethods(t *testing.T, service protoreflect.ServiceDescriptor, want []protoreflect.Name) {
	t.Helper()
	methods := service.Methods()
	if methods.Len() != len(want) {
		t.Fatalf("%s method count = %d, want %d", service.FullName(), methods.Len(), len(want))
	}
	for _, name := range want {
		if methods.ByName(name) == nil {
			t.Fatalf("%s method %s missing", service.FullName(), name)
		}
	}
}

func assertMessageFields(t *testing.T, message protoreflect.MessageDescriptor, want []protoreflect.Name) {
	t.Helper()
	if message == nil {
		t.Fatal("message descriptor missing")
	}
	fields := message.Fields()
	for _, name := range want {
		if fields.ByName(name) == nil {
			t.Fatalf("%s field %s missing", message.FullName(), name)
		}
	}
}

func assertNoRawPayloadField(t *testing.T, field protoreflect.FieldDescriptor) {
	t.Helper()
	names := []string{
		string(field.Name()),
		string(field.JSONName()),
		string(field.Message().Name()),
	}
	for _, name := range names {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "raw") || strings.Contains(lower, "payloadjson") || strings.Contains(lower, "payload_json") {
			t.Fatalf("ChatEvent payload field %s exposes raw payload shape through %q", field.FullName(), name)
		}
	}
}

func assertMessageTreeHasNoRawPayloadNames(t *testing.T, message protoreflect.MessageDescriptor) {
	t.Helper()
	assertNoRawPayloadName(t, string(message.FullName()))
	fields := message.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		assertNoRawPayloadName(t, string(field.Name()))
		assertNoRawPayloadName(t, string(field.JSONName()))
	}
	nested := message.Messages()
	for i := 0; i < nested.Len(); i++ {
		assertMessageTreeHasNoRawPayloadNames(t, nested.Get(i))
	}
}

func assertNoRawPayloadName(t *testing.T, name string) {
	t.Helper()
	lower := strings.ToLower(name)
	if strings.Contains(lower, "raw") || strings.Contains(lower, "payloadjson") || strings.Contains(lower, "payload_json") {
		t.Fatalf("public proto descriptor exposes raw payload shape through %q", name)
	}
}
