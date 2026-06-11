package pending

import (
	"encoding/json"
	"testing"

	"github.com/Dirard/codex-runtime/internal/domain"
)

func TestNormalizeServerRequestIDPreservesLiteralStringIDs(t *testing.T) {
	tests := []struct {
		name      string
		rawID     string
		jsonrpcID json.RawMessage
		want      string
	}{
		{
			name:      "raw string keeps padding",
			rawID:     " 101 ",
			jsonrpcID: json.RawMessage(`101`),
			want:      " 101 ",
		},
		{
			name:      "jsonrpc string keeps padding",
			jsonrpcID: json.RawMessage(`" 101 "`),
			want:      " 101 ",
		},
		{
			name:      "raw whitespace falls back to numeric jsonrpc id",
			rawID:     "   ",
			jsonrpcID: json.RawMessage(`101`),
			want:      "101",
		},
		{
			name:      "exact numeric string remains numeric text",
			jsonrpcID: json.RawMessage(`"101"`),
			want:      "101",
		},
		{
			name:      "numeric id uses json number text",
			jsonrpcID: json.RawMessage(`101`),
			want:      "101",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeServerRequestID(tt.rawID, tt.jsonrpcID); got != tt.want {
				t.Fatalf("NormalizeServerRequestID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestManagerResolvedServerRequestIDMatchesExactRawBeforeLogicalFallback(t *testing.T) {
	manager := NewManager()
	numeric := managerTestRecord(json.RawMessage(`101`))
	stringID := managerTestRecord(json.RawMessage(`"101"`))
	manager.Add(numeric)
	manager.Add(stringID)

	if got := manager.ActiveByResolvedServerRequestID(json.RawMessage(`101`)); got != numeric {
		t.Fatalf("ActiveByResolvedServerRequestID(101) = %#v, want numeric record", got)
	}
	manager.MarkResolved(numeric)
	if numeric.Active {
		t.Fatal("numeric record remained active after MarkResolved")
	}
	if !stringID.Active {
		t.Fatal("string record was cleared by numeric resolved id")
	}

	if got := manager.ActiveByResolvedServerRequestID(json.RawMessage(`101`)); got != nil {
		t.Fatalf("stale ActiveByResolvedServerRequestID(101) = %#v, want nil", got)
	}
	if !stringID.Active {
		t.Fatal("string record was cleared by stale numeric resolved id")
	}

	if got := manager.ActiveByResolvedServerRequestID(json.RawMessage(`"101"`)); got != stringID {
		t.Fatalf("ActiveByResolvedServerRequestID(\"101\") = %#v, want string record", got)
	}
	manager.MarkResolved(stringID)
	if stringID.Active {
		t.Fatal("string record remained active after MarkResolved")
	}
}

func TestManagerResolvedServerRequestIDFallsBackToLogicalIDWhenExactRawIsMissing(t *testing.T) {
	manager := NewManager()
	numeric := managerTestRecord(json.RawMessage(`101`))
	manager.Add(numeric)

	if got := manager.ActiveByResolvedServerRequestID(json.RawMessage(`"101"`)); got != numeric {
		t.Fatalf("ActiveByResolvedServerRequestID(\"101\") = %#v, want numeric record via logical fallback", got)
	}
}

func TestManagerResolvedServerRequestIDPreservesPaddedStringExactMatch(t *testing.T) {
	manager := NewManager()
	numeric := managerTestRecord(json.RawMessage(`101`))
	padded := managerTestRecord(json.RawMessage(`" 101 "`))
	manager.Add(numeric)
	manager.Add(padded)

	if got := manager.ActiveByResolvedServerRequestID(json.RawMessage(`" 101 "`)); got != padded {
		t.Fatalf("ActiveByResolvedServerRequestID(\" 101 \") = %#v, want padded string record", got)
	}
	manager.MarkResolved(padded)
	if padded.Active {
		t.Fatal("padded string record remained active after MarkResolved")
	}
	if !numeric.Active {
		t.Fatal("numeric record was cleared by padded string resolved id")
	}
}

func managerTestRecord(jsonrpcID json.RawMessage) *Record {
	return &Record{
		Method:             MethodCommandApproval,
		AppServerRequestID: NormalizeServerRequestID("", jsonrpcID),
		JSONRPCID:          jsonrpcID,
		Pending: domain.PendingRequest{
			PendingType: domain.PendingTypeCommandApproval,
			Display:     domain.CommandApprovalDisplay{CommandDisplay: "echo hello"},
		},
	}
}
