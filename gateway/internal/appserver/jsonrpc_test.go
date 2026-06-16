package appserver

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEncodeRPCJSONLOmitsJSONRPCField(t *testing.T) {
	message, err := newRequest(7, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name": "gateway-test",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := encodeRPCJSONL(message)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.HasSuffix(encoded, []byte("\n")) {
		t.Fatalf("encoded message missing JSONL newline: %q", encoded)
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
	if string(fields["method"]) != `"initialize"` {
		t.Fatalf("encoded method = %s, want initialize", fields["method"])
	}
}

func TestEncodeRPCJSONLPreservesRawStringID(t *testing.T) {
	message, err := newSuccessResponse(json.RawMessage(`"request-1"`), map[string]any{"ok": true})
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := encodeRPCJSONL(message)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSuffix(encoded, []byte("\n")), &fields); err != nil {
		t.Fatalf("encoded message is not JSON: %v", err)
	}
	if !bytes.Equal(fields["id"], []byte(`"request-1"`)) {
		t.Fatalf("encoded id = %s, want string id", fields["id"])
	}
}

func TestParseRPCLineAcceptsAppServerMessageWithoutJSONRPCField(t *testing.T) {
	message, err := parseRPCLine([]byte(`{"id":7,"result":{"codexHome":"C:/codex"}}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}

	if message.JSONRPC != jsonrpcVersion {
		t.Fatalf("message.JSONRPC = %q, want %q", message.JSONRPC, jsonrpcVersion)
	}
	if !bytes.Equal(message.ID, []byte("7")) {
		t.Fatalf("message.ID = %s, want 7", message.ID)
	}
}

func TestParseRPCLineRejectsExplicitJSONRPCVersionMismatch(t *testing.T) {
	_, err := parseRPCLine([]byte(`{"jsonrpc":"1.0","id":7,"result":{}}` + "\n"))
	if err == nil || !strings.Contains(err.Error(), "jsonrpc version mismatch") {
		t.Fatalf("parseRPCLine() error = %v, want jsonrpc version mismatch", err)
	}
}
