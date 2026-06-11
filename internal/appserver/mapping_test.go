package appserver

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Dirard/codex-runtime/internal/config"
	"github.com/Dirard/codex-runtime/internal/domain"
	"github.com/Dirard/codex-runtime/internal/redact"
)

func TestThreadStartParamsMapTrustedRuntimePolicy(t *testing.T) {
	group := config.SessionGroup{
		CanonicalCWD: `C:\work\project`,
		RuntimePolicy: config.RuntimePolicy{
			ApprovalPolicy:    config.ApprovalPolicyOnRequest,
			ApprovalsReviewer: config.ApprovalsReviewerUser,
			SandboxMode:       config.SandboxWorkspaceWrite,
		},
	}

	params, err := NewThreadStartParams(group)
	if err != nil {
		t.Fatalf("NewThreadStartParams() error = %v", err)
	}

	got := mapFromJSON(t, params)
	want := map[string]interface{}{
		"cwd":               `C:\work\project`,
		"approvalPolicy":    config.ApprovalPolicyOnRequest,
		"approvalsReviewer": config.ApprovalsReviewerUser,
		"sandbox":           config.SandboxWorkspaceWrite,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ThreadStartParams JSON = %#v, want %#v", got, want)
	}
}

func TestThreadStartParamsMapPermissionsProfileInsteadOfSandbox(t *testing.T) {
	group := config.SessionGroup{
		CanonicalCWD: `/work/project`,
		RuntimePolicy: config.RuntimePolicy{
			ApprovalPolicy:       config.ApprovalPolicyUntrusted,
			ApprovalsReviewer:    config.ApprovalsReviewerUser,
			PermissionsProfileID: "trusted-profile",
		},
	}

	params, err := NewThreadStartParams(group)
	if err != nil {
		t.Fatalf("NewThreadStartParams() error = %v", err)
	}

	got := mapFromJSON(t, params)
	want := map[string]interface{}{
		"cwd":               `/work/project`,
		"approvalPolicy":    config.ApprovalPolicyUntrusted,
		"approvalsReviewer": config.ApprovalsReviewerUser,
		"permissions":       "trusted-profile",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ThreadStartParams JSON = %#v, want %#v", got, want)
	}
}

func TestThreadStartParamsRejectMissingCanonicalCWD(t *testing.T) {
	group := config.SessionGroup{
		CWD: `/work/project`,
		RuntimePolicy: config.RuntimePolicy{
			ApprovalPolicy:    config.ApprovalPolicyOnRequest,
			ApprovalsReviewer: config.ApprovalsReviewerUser,
			SandboxMode:       config.SandboxWorkspaceWrite,
		},
	}

	if _, err := NewThreadStartParams(group); err == nil {
		t.Fatal("NewThreadStartParams() succeeded without canonical cwd")
	}
}

func TestResumeAndTurnStartMappingsDoNotCarryRuntimeOverrides(t *testing.T) {
	resume := mapFromJSON(t, NewThreadResumeParams("thread-1"))
	wantResume := map[string]interface{}{
		"threadId":     "thread-1",
		"excludeTurns": true,
	}
	if !reflect.DeepEqual(resume, wantResume) {
		t.Fatalf("ThreadResumeParams JSON = %#v, want %#v", resume, wantResume)
	}

	turn := NewTurnStartParams("thread-1", "client-message-1", []UserInputText{
		NewUserInputText("hello"),
	})
	encoded, err := json.Marshal(turn)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"approvalPolicy", "approvalsReviewer", "sandbox", "permissions", "cwd", "runtimeWorkspaceRoots", "environments", "config", "developerInstructions"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("TurnStartParams JSON contains forbidden field %q: %s", forbidden, encoded)
		}
	}

	got := mapFromJSON(t, turn)
	input := got["input"].([]interface{})
	firstInput := input[0].(map[string]interface{})
	if firstInput["type"] != "text" || firstInput["text"] != "hello" {
		t.Fatalf("TurnStartParams input = %#v, want UserInput.Text", firstInput)
	}
	if _, ok := firstInput["text_elements"]; !ok {
		t.Fatalf("TurnStartParams input missing text_elements: %#v", firstInput)
	}
}

func TestApprovalServerRequestResponsePayloads(t *testing.T) {
	command, ok := NewCommandApprovalResponsePayload(domain.ApprovalWireDecisionAcceptForSession)
	if !ok {
		t.Fatal("NewCommandApprovalResponsePayload(acceptForSession) failed")
	}
	if got, want := mustJSON(t, command), `{"decision":"acceptForSession"}`; got != want {
		t.Fatalf("command approval response JSON = %s, want %s", got, want)
	}

	file, ok := NewFileChangeApprovalResponsePayload(domain.ApprovalWireDecisionCancel)
	if !ok {
		t.Fatal("NewFileChangeApprovalResponsePayload(cancel) failed")
	}
	if got, want := mustJSON(t, file), `{"decision":"cancel"}`; got != want {
		t.Fatalf("file approval response JSON = %s, want %s", got, want)
	}

	if _, ok := NewCommandApprovalResponsePayload(domain.ApprovalWireDecision("accept_for_session")); ok {
		t.Fatal("NewCommandApprovalResponsePayload() accepted non-wire decision")
	}
}

func TestPermissionsServerRequestResponsePayloads(t *testing.T) {
	denial, ok := NewPermissionsRequestApprovalResponse(nil, "", false)
	if !ok {
		t.Fatal("NewPermissionsRequestApprovalResponse(deny) failed")
	}
	if got, want := mustJSON(t, denial), `{"permissions":{}}`; got != want {
		t.Fatalf("permission denial JSON = %s, want %s", got, want)
	}
	decodedDenial := mapFromJSON(t, denial)
	if permissions, ok := decodedDenial["permissions"].(map[string]interface{}); !ok || len(permissions) != 0 {
		t.Fatalf("permission denial permissions = %#v, want empty object", decodedDenial["permissions"])
	}

	turnDenial, ok := NewPermissionsRequestApprovalResponse(map[string]any{}, domain.PermissionScopeTurn, false)
	if !ok {
		t.Fatal("NewPermissionsRequestApprovalResponse(turn denial) failed")
	}
	if got, want := mustJSON(t, turnDenial), `{"permissions":{}}`; got != want {
		t.Fatalf("permission turn denial JSON = %s, want %s", got, want)
	}

	grant := map[string]any{
		"network": map[string]any{
			"enabled": true,
		},
		"fileSystem": map[string]any{
			"read":  true,
			"write": false,
			"entries": []map[string]any{
				{
					"access": "read",
					"path": map[string]any{
						"type": "literal",
						"path": "README.md",
					},
				},
			},
		},
	}
	response, ok := NewPermissionsRequestApprovalResponse(grant, domain.PermissionScopeSession, true)
	if !ok {
		t.Fatal("NewPermissionsRequestApprovalResponse(grant) failed")
	}
	want := `{"scope":"session","permissions":{"fileSystem":{"entries":[{"access":"read","path":{"path":"README.md","type":"literal"}}],"read":true,"write":false},"network":{"enabled":true}},"strictAutoReview":true}`
	if got := mustJSON(t, response); got != want {
		t.Fatalf("permission grant JSON = %s, want %s", got, want)
	}

	if _, ok := NewPermissionsRequestApprovalResponse(nil, domain.PermissionScopeSession, false); ok {
		t.Fatal("NewPermissionsRequestApprovalResponse() accepted empty permissions with session scope")
	}
	if _, ok := NewPermissionsRequestApprovalResponse(map[string]any{}, "", true); ok {
		t.Fatal("NewPermissionsRequestApprovalResponse() accepted empty permissions with strict auto review")
	}
	if _, ok := NewPermissionsRequestApprovalResponse(nil, domain.PermissionScope("workspace"), false); ok {
		t.Fatal("NewPermissionsRequestApprovalResponse() accepted unknown scope")
	}
}

func TestMcpElicitationServerRequestResponsePayloads(t *testing.T) {
	accepted, ok := NewMcpElicitationResponsePayload(domain.McpElicitationPendingResponse{
		Action:      domain.McpElicitationActionAccept,
		ContentJSON: `{"mode":"blue","count":2}`,
	})
	if !ok {
		t.Fatal("NewMcpElicitationResponsePayload(accept) failed")
	}
	if got, want := mustJSON(t, accepted), `{"action":"accept","content":{"mode":"blue","count":2},"_meta":null}`; got != want {
		t.Fatalf("mcp accept JSON = %s, want %s", got, want)
	}
	decodedAccepted := mapFromJSON(t, accepted)
	if _, ok := decodedAccepted["content"].(map[string]interface{}); !ok {
		t.Fatalf("mcp accept content = %#v, want JSON object", decodedAccepted["content"])
	}
	if value, ok := decodedAccepted["_meta"]; !ok || value != nil {
		t.Fatalf("mcp accept _meta = %#v, present %t; want explicit null", value, ok)
	}

	for _, action := range []domain.McpElicitationAction{domain.McpElicitationActionDecline, domain.McpElicitationActionCancel} {
		payload, ok := NewMcpElicitationResponsePayload(domain.McpElicitationPendingResponse{Action: action})
		if !ok {
			t.Fatalf("NewMcpElicitationResponsePayload(%s) failed", action)
		}
		want := `{"action":"` + string(action) + `","content":null,"_meta":null}`
		if got := mustJSON(t, payload); got != want {
			t.Fatalf("mcp %s JSON = %s, want %s", action, got, want)
		}
		decoded := mapFromJSON(t, payload)
		if value, ok := decoded["content"]; !ok || value != nil {
			t.Fatalf("mcp %s content = %#v, present %t; want explicit null", action, value, ok)
		}
		if value, ok := decoded["_meta"]; !ok || value != nil {
			t.Fatalf("mcp %s _meta = %#v, present %t; want explicit null", action, value, ok)
		}
	}

	if _, ok := NewMcpElicitationResponsePayload(domain.McpElicitationPendingResponse{
		Action:      domain.McpElicitationActionAccept,
		ContentJSON: `{"broken"`,
	}); ok {
		t.Fatal("NewMcpElicitationResponsePayload() accepted invalid JSON content")
	}
}

func TestMcpElicitationResponsePayloadWithRedactionRegistersSensitiveValues(t *testing.T) {
	registry := redact.NewRegistry()
	payload, ok := NewMcpElicitationResponsePayloadWithRedaction(domain.McpElicitationPendingResponse{
		Action:      domain.McpElicitationActionAccept,
		ContentJSON: `{"api_key":"mcp-secret-value","nested":{"accessToken":"nested-secret-value"},"public":"visible"}`,
	}, registry)
	if !ok {
		t.Fatal("NewMcpElicitationResponsePayloadWithRedaction() rejected valid content")
	}
	want := `{"action":"accept","content":{"api_key":"mcp-secret-value","nested":{"accessToken":"nested-secret-value"},"public":"visible"},"_meta":null}`
	if got := mustJSON(t, payload); got != want {
		t.Fatalf("mcp accept JSON = %s, want %s", got, want)
	}

	redacted := registry.Redact("mcp-secret-value nested-secret-value visible")
	for _, secret := range []string{"mcp-secret-value", "nested-secret-value"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("registered MCP elicitation content still visible after redaction: %q", redacted)
		}
	}
	if !strings.Contains(redacted, "visible") {
		t.Fatalf("non-sensitive content was redacted unexpectedly: %q", redacted)
	}
}

func TestToolUserInputServerRequestResponsePayload(t *testing.T) {
	payload, ok := NewToolUserInputResponsePayload([]domain.ToolUserInputAnswer{
		{QuestionID: "q2", Answers: []string{"two", "three"}},
		{QuestionID: "q1", Answers: []string{"one"}},
	})
	if !ok {
		t.Fatal("NewToolUserInputResponsePayload() rejected valid answers")
	}

	want := `{"answers":{"q1":{"answers":["one"]},"q2":{"answers":["two","three"]}}}`
	if got := mustJSON(t, payload); got != want {
		t.Fatalf("tool user input response JSON = %s, want %s", got, want)
	}
}

func TestToolUserInputResponsePayloadWithRedactionRegistersAnswers(t *testing.T) {
	registry := redact.NewRegistry()
	payload, ok := NewToolUserInputResponsePayloadWithRedaction([]domain.ToolUserInputAnswer{
		{QuestionID: "q1", Answers: []string{"first secret answer", "second secret answer"}},
	}, registry)
	if !ok {
		t.Fatal("NewToolUserInputResponsePayloadWithRedaction() rejected valid answers")
	}
	want := `{"answers":{"q1":{"answers":["first secret answer","second secret answer"]}}}`
	if got := mustJSON(t, payload); got != want {
		t.Fatalf("tool user input response JSON = %s, want %s", got, want)
	}

	redacted := registry.Redact("first secret answer and second secret answer")
	for _, secret := range []string{"first secret answer", "second secret answer"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("registered tool-user-input answer still visible after redaction: %q", redacted)
		}
	}
}

func TestToolUserInputServerRequestResponsePayloadRejectsInvalidQuestionIDs(t *testing.T) {
	tests := []struct {
		name    string
		answers []domain.ToolUserInputAnswer
	}{
		{
			name:    "empty question id",
			answers: []domain.ToolUserInputAnswer{{QuestionID: "", Answers: []string{"one"}}},
		},
		{
			name: "duplicate question id",
			answers: []domain.ToolUserInputAnswer{
				{QuestionID: "q1", Answers: []string{"one"}},
				{QuestionID: "q1", Answers: []string{"two"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if payload, ok := NewToolUserInputResponsePayload(tt.answers); ok {
				t.Fatalf("NewToolUserInputResponsePayload() = (%#v, true), want failure", payload)
			}
		})
	}
}

func mapFromJSON(t *testing.T, value interface{}) map[string]interface{} {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func mustJSON(t *testing.T, value interface{}) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
