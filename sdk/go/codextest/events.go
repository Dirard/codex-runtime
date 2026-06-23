package codextest

import codex "github.com/Dirard/codex-runtime/sdk/go"

// Meta returns a minimal resumable event metadata value for handler tests.
func Meta(id uint64, cursor string) codex.EventMeta {
	return codex.EventMeta{
		ID:             id,
		Cursor:         cursor,
		ChatID:         "test-chat",
		RunID:          "test-run",
		Source:         codex.EventSourceChatStream,
		CanResumeAfter: cursor != "",
	}
}

// AssistantTextDelta returns a friendly text event without requiring protobuf setup.
func AssistantTextDelta(text string) *codex.AssistantTextDelta {
	return codex.NewAssistantTextDeltaEvent(Meta(1, "test-cursor-1"), text)
}

// ApprovalRequested returns a friendly approval event without requiring protobuf setup.
func ApprovalRequested() *codex.ApprovalRequested {
	return codex.NewApprovalRequestedEvent(
		Meta(2, "test-cursor-2"),
		"pending-1",
		codex.ActionDisplay{Title: "Approve command", Summary: "go test ./..."},
		[]codex.ApprovalDecision{
			{ID: "approve", Decision: codex.ApprovalDecisionAccept, Label: "Approve", Selectable: true},
			{ID: "deny", Decision: codex.ApprovalDecisionDecline, Label: "Deny", Selectable: true},
		},
	)
}

// RunCompleted returns a friendly completed terminal event without requiring protobuf setup.
func RunCompleted(summary string) *codex.RunCompleted {
	return codex.NewRunCompletedEvent(Meta(3, "test-cursor-3"), summary)
}
