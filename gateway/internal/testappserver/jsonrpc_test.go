package testappserver

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestMessageJSONLOmitsJSONRPCField(t *testing.T) {
	encoded, err := Response(7, map[string]any{"ok": true}).JSONL()
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSuffix(encoded, []byte("\n")), &fields); err != nil {
		t.Fatalf("encoded message is not JSON: %v", err)
	}
	if _, ok := fields["jsonrpc"]; ok {
		t.Fatalf("encoded message included jsonrpc field: %s", encoded)
	}
	if !bytes.Equal(fields["id"], []byte("7")) {
		t.Fatalf("encoded id = %s, want 7", fields["id"])
	}
}

func TestParseMessageLineAcceptsAppServerMessageWithoutJSONRPCField(t *testing.T) {
	message, err := parseMessageLine([]byte(`{"id":7,"result":{"ok":true}}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}

	assertMessage(t, message, Response(7, map[string]any{"ok": true}))
}
