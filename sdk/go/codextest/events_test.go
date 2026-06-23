package codextest

import (
	"testing"

	codex "github.com/Dirard/codex-runtime/sdk/go"
)

func TestHelpersConstructFriendlyEventsWithoutProtobuf(t *testing.T) {
	text := AssistantTextDelta("hello")
	if text.TextDelta() != "hello" || text.EventKind() != codex.EventKindAssistantTextDelta {
		t.Fatalf("text helper = %#v", text)
	}

	action := ApprovalRequested()
	if action.PendingKind() != codex.PendingKindApproval || action.Display().Title == "" || len(action.Decisions()) != 2 {
		t.Fatalf("approval helper = %#v", action)
	}

	terminal := RunCompleted("done")
	if terminal.Result().State != codex.TerminalStateCompleted || terminal.Result().Summary != "done" {
		t.Fatalf("terminal helper = %#v", terminal.Result())
	}
}
