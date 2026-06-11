package domain

import "testing"

func TestStartTaskFingerprintV1CanonicalFixture(t *testing.T) {
	command := baseStartTaskFingerprintCommand()

	canonical, err := StartTaskFingerprintV1CanonicalJSON(command)
	if err != nil {
		t.Fatalf("StartTaskFingerprintV1CanonicalJSON() error = %v", err)
	}
	want := `{"schema_version":1,"session_group_id":"sg-1","workspace_id":"ws-1","thread_id":null,"user_prompt":"hello","context_blocks":[{"kind":"application","source_label":"ticket","source_uri":"https://example.com/source","mime_type":"text/plain","content":"context"},{"kind":"untrusted","source_label":"log","source_uri":null,"mime_type":null,"content":"line"}]}`
	if got := string(canonical); got != want {
		t.Fatalf("StartTask fingerprint canonical JSON = %s, want %s", got, want)
	}

	digest, err := StartTaskFingerprintV1SHA256Hex(command)
	if err != nil {
		t.Fatalf("StartTaskFingerprintV1SHA256Hex() error = %v", err)
	}
	if wantDigest := sha256Hex([]byte(want)); digest != wantDigest {
		t.Fatalf("StartTask fingerprint digest = %s, want %s", digest, wantDigest)
	}
}

func TestStartTaskFingerprintV1RetryStabilityAndExclusions(t *testing.T) {
	first := baseStartTaskFingerprintCommand()
	second := baseStartTaskFingerprintCommand()
	second.ClientMessageID = "client-message-retry"
	second.UICorrelationMetadata = map[string]string{
		"render": "expanded",
		"trace":  "other",
	}

	firstDigest := mustStartTaskFingerprintDigest(t, first)
	secondDigest := mustStartTaskFingerprintDigest(t, second)
	if firstDigest != secondDigest {
		t.Fatalf("StartTask retry digest changed after excluded fields: %s != %s", firstDigest, secondDigest)
	}
}

func TestStartTaskFingerprintV1SemanticChanges(t *testing.T) {
	base := baseStartTaskFingerprintCommand()
	baseDigest := mustStartTaskFingerprintDigest(t, base)

	tests := []struct {
		name   string
		mutate func(*StartTaskCommand)
	}{
		{
			name: "prompt",
			mutate: func(command *StartTaskCommand) {
				command.Prompt = "different"
			},
		},
		{
			name: "context content",
			mutate: func(command *StartTaskCommand) {
				command.ContextBlocks[0].Content = "different"
			},
		},
		{
			name: "thread",
			mutate: func(command *StartTaskCommand) {
				command.ThreadID = "thread-1"
			},
		},
		{
			name: "context order",
			mutate: func(command *StartTaskCommand) {
				command.ContextBlocks[0], command.ContextBlocks[1] = command.ContextBlocks[1], command.ContextBlocks[0]
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := baseStartTaskFingerprintCommand()
			tt.mutate(&changed)
			if digest := mustStartTaskFingerprintDigest(t, changed); digest == baseDigest {
				t.Fatalf("StartTask digest did not change for %s", tt.name)
			}
		})
	}
}

func TestPendingResponseFingerprintV1CanonicalFixtures(t *testing.T) {
	tests := []struct {
		name              string
		pendingType       PendingType
		response          PendingResponse
		wantCanonicalJSON string
	}{
		{
			name:        "approval",
			pendingType: PendingTypeCommandApproval,
			response: PendingResponse{
				Approval: &ApprovalPendingResponse{DecisionID: "accept-session"},
			},
			wantCanonicalJSON: `{"schema_version":1,"task_id":"task-1","pending_request_id":"pending-1","pending_type":"command_approval","response":{"decision_id":"accept-session"}}`,
		},
		{
			name:        "permissions",
			pendingType: PendingTypePermissionsApproval,
			response: PendingResponse{
				Permissions: &PermissionsPendingResponse{
					PermissionIDs:    []string{"perm-b", "perm-a"},
					Scope:            PermissionScopeSession,
					StrictAutoReview: true,
				},
			},
			wantCanonicalJSON: `{"schema_version":1,"task_id":"task-1","pending_request_id":"pending-1","pending_type":"permissions_approval","response":{"permission_ids":["perm-a","perm-b"],"scope":"session","strict_auto_review":true}}`,
		},
		{
			name:        "mcp accept",
			pendingType: PendingTypeMcpElicitation,
			response: PendingResponse{
				McpElicitation: &McpElicitationPendingResponse{
					Action:      McpElicitationActionAccept,
					ContentJSON: `{"b":2,"a":1}`,
				},
			},
			wantCanonicalJSON: `{"schema_version":1,"task_id":"task-1","pending_request_id":"pending-1","pending_type":"mcp_elicitation","response":{"action":"accept","content":{"a":1,"b":2}}}`,
		},
		{
			name:        "tool user input",
			pendingType: PendingTypeToolUserInput,
			response: PendingResponse{
				ToolUserInput: &ToolUserInputPendingResponse{
					Answers: []ToolUserInputAnswer{
						{QuestionID: "q2", Answers: []string{"two"}},
						{QuestionID: "q1", Answers: []string{"one", "other"}},
					},
				},
			},
			wantCanonicalJSON: `{"schema_version":1,"task_id":"task-1","pending_request_id":"pending-1","pending_type":"tool_user_input","response":{"answers":{"q1":{"answers":["one","other"]},"q2":{"answers":["two"]}}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canonical, err := PendingResponseFingerprintV1CanonicalJSON("task-1", "pending-1", tt.pendingType, tt.response)
			if err != nil {
				t.Fatalf("PendingResponseFingerprintV1CanonicalJSON() error = %v", err)
			}
			if got := string(canonical); got != tt.wantCanonicalJSON {
				t.Fatalf("PendingResponse canonical JSON = %s, want %s", got, tt.wantCanonicalJSON)
			}

			digest, err := PendingResponseFingerprintV1SHA256Hex("task-1", "pending-1", tt.pendingType, tt.response)
			if err != nil {
				t.Fatalf("PendingResponseFingerprintV1SHA256Hex() error = %v", err)
			}
			if wantDigest := sha256Hex([]byte(tt.wantCanonicalJSON)); digest != wantDigest {
				t.Fatalf("PendingResponse digest = %s, want %s", digest, wantDigest)
			}
		})
	}
}

func TestPendingResponseFingerprintV1Mismatch(t *testing.T) {
	base := PendingResponse{Approval: &ApprovalPendingResponse{DecisionID: "decline"}}
	changed := PendingResponse{Approval: &ApprovalPendingResponse{DecisionID: "cancel"}}

	baseDigest := mustPendingResponseFingerprintDigest(t, PendingTypeCommandApproval, base)
	changedDigest := mustPendingResponseFingerprintDigest(t, PendingTypeCommandApproval, changed)
	if baseDigest == changedDigest {
		t.Fatal("PendingResponse digest did not change for a different approval decision")
	}

	pendingChangedDigest, err := PendingResponseFingerprintV1SHA256Hex("task-1", "pending-2", PendingTypeCommandApproval, base)
	if err != nil {
		t.Fatalf("PendingResponseFingerprintV1SHA256Hex() error = %v", err)
	}
	if baseDigest == pendingChangedDigest {
		t.Fatal("PendingResponse digest did not change for a different pending_request_id")
	}
}

func TestPendingResponseFingerprintV1PreservesMcpNumberText(t *testing.T) {
	first := PendingResponse{McpElicitation: &McpElicitationPendingResponse{
		Action:      McpElicitationActionAccept,
		ContentJSON: `{"value":9007199254740992}`,
	}}
	second := PendingResponse{McpElicitation: &McpElicitationPendingResponse{
		Action:      McpElicitationActionAccept,
		ContentJSON: `{"value":9007199254740993}`,
	}}

	firstCanonical, err := PendingResponseFingerprintV1CanonicalJSON("task-1", "pending-1", PendingTypeMcpElicitation, first)
	if err != nil {
		t.Fatalf("PendingResponseFingerprintV1CanonicalJSON(first) error = %v", err)
	}
	secondCanonical, err := PendingResponseFingerprintV1CanonicalJSON("task-1", "pending-1", PendingTypeMcpElicitation, second)
	if err != nil {
		t.Fatalf("PendingResponseFingerprintV1CanonicalJSON(second) error = %v", err)
	}
	if string(firstCanonical) == string(secondCanonical) {
		t.Fatalf("MCP numeric canonical JSON collapsed: %s", firstCanonical)
	}

	firstDigest, err := PendingResponseFingerprintV1SHA256Hex("task-1", "pending-1", PendingTypeMcpElicitation, first)
	if err != nil {
		t.Fatalf("PendingResponseFingerprintV1SHA256Hex(first) error = %v", err)
	}
	secondDigest, err := PendingResponseFingerprintV1SHA256Hex("task-1", "pending-1", PendingTypeMcpElicitation, second)
	if err != nil {
		t.Fatalf("PendingResponseFingerprintV1SHA256Hex(second) error = %v", err)
	}
	if firstDigest == secondDigest {
		t.Fatal("MCP numeric digests collapsed for distinct JSON numbers")
	}
}

func TestPendingResponseFingerprintV1RejectsMismatchedType(t *testing.T) {
	response := PendingResponse{Approval: &ApprovalPendingResponse{DecisionID: "decline"}}
	if _, err := PendingResponseFingerprintV1CanonicalJSON("task-1", "pending-1", PendingTypePermissionsApproval, response); err == nil {
		t.Fatal("PendingResponseFingerprintV1CanonicalJSON() accepted mismatched pending type")
	}
}

func mustStartTaskFingerprintDigest(t *testing.T, command StartTaskCommand) string {
	t.Helper()
	digest, err := StartTaskFingerprintV1SHA256Hex(command)
	if err != nil {
		t.Fatalf("StartTaskFingerprintV1SHA256Hex() error = %v", err)
	}
	return digest
}

func mustPendingResponseFingerprintDigest(t *testing.T, pendingType PendingType, response PendingResponse) string {
	t.Helper()
	digest, err := PendingResponseFingerprintV1SHA256Hex("task-1", "pending-1", pendingType, response)
	if err != nil {
		t.Fatalf("PendingResponseFingerprintV1SHA256Hex() error = %v", err)
	}
	return digest
}

func baseStartTaskFingerprintCommand() StartTaskCommand {
	return StartTaskCommand{
		SessionGroupID:  "sg-1",
		WorkspaceID:     "ws-1",
		Prompt:          "hello",
		ClientMessageID: "client-message-1",
		ContextBlocks: []ContextBlock{
			{
				Kind:        ContextBlockKindApplication,
				SourceLabel: "ticket",
				SourceURI:   "https://example.com/source",
				MimeType:    "text/plain",
				Content:     "context",
			},
			{
				Kind:        ContextBlockKindUntrusted,
				SourceLabel: "log",
				Content:     "line",
			},
		},
		UICorrelationMetadata: map[string]string{"view": "compact"},
	}
}
