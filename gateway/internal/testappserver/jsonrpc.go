package testappserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const jsonrpcVersion = "2.0"

// Message is an app-server JSONL stdio message with JSON-RPC-like semantics.
type Message struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	Raw     []byte          `json:"-"`
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Request builds a JSON-RPC request message.
func Request(id any, method string, params any) Message {
	return Message{
		JSONRPC: jsonrpcVersion,
		ID:      mustMarshalID(id),
		Method:  method,
		Params:  mustMarshalOptional(params),
	}
}

// Notification builds a JSON-RPC notification message.
func Notification(method string, params any) Message {
	return Message{
		JSONRPC: jsonrpcVersion,
		Method:  method,
		Params:  mustMarshalOptional(params),
	}
}

// Response builds a JSON-RPC success response message.
func Response(id any, result any) Message {
	return Message{
		JSONRPC: jsonrpcVersion,
		ID:      mustMarshalID(id),
		Result:  mustMarshalValue(result),
	}
}

// ErrorResponse builds a JSON-RPC error response message.
func ErrorResponse(id any, code int, message string) Message {
	return Message{
		JSONRPC: jsonrpcVersion,
		ID:      mustMarshalID(id),
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
}

// JSON encodes the message as a single JSON object.
func (m Message) JSON() ([]byte, error) {
	m.JSONRPC = ""
	encoded, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal JSON-RPC message: %w", err)
	}
	return encoded, nil
}

// JSONL encodes the message as one LF-terminated JSONL line.
func (m Message) JSONL() ([]byte, error) {
	encoded, err := m.JSON()
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// Clone returns a deep copy of the message.
func (m Message) Clone() Message {
	clone := m
	clone.ID = cloneRaw(m.ID)
	clone.Params = cloneRaw(m.Params)
	clone.Result = cloneRaw(m.Result)
	clone.Raw = append([]byte(nil), m.Raw...)
	if m.Error != nil {
		errorCopy := *m.Error
		errorCopy.Data = cloneRaw(m.Error.Data)
		clone.Error = &errorCopy
	}
	return clone
}

func parseMessageLine(line []byte) (Message, error) {
	trimmed := bytes.TrimSuffix(line, []byte("\n"))
	trimmed = bytes.TrimSuffix(trimmed, []byte("\r"))
	if len(bytes.TrimSpace(trimmed)) == 0 {
		return Message{}, fmt.Errorf("empty JSONL line")
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var message Message
	if err := decoder.Decode(&message); err != nil {
		return Message{}, fmt.Errorf("parse JSON-RPC line: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Message{}, fmt.Errorf("parse JSON-RPC line: trailing data")
	}
	if message.JSONRPC == "" {
		message.JSONRPC = jsonrpcVersion
	}
	message.Raw = append([]byte(nil), trimmed...)
	return message, nil
}

func mustMarshalOptional(value any) json.RawMessage {
	if value == nil {
		return nil
	}
	return mustMarshalValue(value)
}

func mustMarshalID(id any) json.RawMessage {
	return mustMarshalRaw(id, true)
}

func mustMarshalValue(value any) json.RawMessage {
	return mustMarshalRaw(value, false)
}

func mustMarshalRaw(value any, allowNil bool) json.RawMessage {
	if value == nil && !allowNil {
		return json.RawMessage("null")
	}
	if raw, ok := value.(json.RawMessage); ok {
		return cloneRaw(raw)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("testappserver: marshal fixture value: %v", err))
	}
	return encoded
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}
