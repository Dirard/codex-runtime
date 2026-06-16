package appserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const jsonrpcVersion = "2.0"

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func newRequest(id uint64, method string, params any) (rpcMessage, error) {
	paramsRaw, err := marshalOptional(params)
	if err != nil {
		return rpcMessage{}, err
	}
	return rpcMessage{
		JSONRPC: jsonrpcVersion,
		ID:      mustID(id),
		Method:  method,
		Params:  paramsRaw,
	}, nil
}

func newNotification(method string, params any) (rpcMessage, error) {
	paramsRaw, err := marshalOptional(params)
	if err != nil {
		return rpcMessage{}, err
	}
	return rpcMessage{
		JSONRPC: jsonrpcVersion,
		Method:  method,
		Params:  paramsRaw,
	}, nil
}

func newSuccessResponse(id json.RawMessage, result any) (rpcMessage, error) {
	resultRaw, err := marshalValue(result)
	if err != nil {
		return rpcMessage{}, err
	}
	return rpcMessage{
		JSONRPC: jsonrpcVersion,
		ID:      cloneRaw(id),
		Result:  resultRaw,
	}, nil
}

func newErrorResponse(id json.RawMessage, code int, message string) rpcMessage {
	return rpcMessage{
		JSONRPC: jsonrpcVersion,
		ID:      cloneRaw(id),
		Error: &rpcError{
			Code:    code,
			Message: message,
		},
	}
}

func parseRPCLine(line []byte) (rpcMessage, error) {
	trimmed := bytes.TrimSuffix(line, []byte("\n"))
	trimmed = bytes.TrimSuffix(trimmed, []byte("\r"))
	if len(bytes.TrimSpace(trimmed)) == 0 {
		return rpcMessage{}, fmt.Errorf("empty JSONL line")
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var message rpcMessage
	if err := decoder.Decode(&message); err != nil {
		return rpcMessage{}, fmt.Errorf("parse JSON-RPC line: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return rpcMessage{}, fmt.Errorf("parse JSON-RPC line: trailing data")
	}
	if message.JSONRPC == "" {
		message.JSONRPC = jsonrpcVersion
	}
	if message.JSONRPC != jsonrpcVersion {
		return rpcMessage{}, fmt.Errorf("jsonrpc version mismatch")
	}
	return message, nil
}

func encodeRPCJSONL(message rpcMessage) ([]byte, error) {
	message.JSONRPC = ""
	encoded, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func marshalOptional(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	return marshalValue(value)
}

func marshalValue(value any) (json.RawMessage, error) {
	if raw, ok := value.(json.RawMessage); ok {
		return cloneRaw(raw), nil
	}
	if value == nil {
		return json.RawMessage("null"), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func mustID(id uint64) json.RawMessage {
	encoded, err := json.Marshal(id)
	if err != nil {
		panic(err)
	}
	return encoded
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func idKey(id json.RawMessage) string {
	return string(bytes.TrimSpace(id))
}
