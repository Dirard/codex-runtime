package testappserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"
)

type jsonID json.RawMessage

type scriptContext struct {
	harness *Harness
	ids     map[string]jsonID
}

// Step is one deterministic fake app-server script action.
type Step struct {
	name string
	run  func(*scriptContext) error
}

type expectConfig struct {
	captureID string
	params    any
	hasParams bool
	noParams  bool
	result    any
	hasResult bool
	check     func(Message) error
}

// ExpectOption configures an expected outbound client message.
type ExpectOption func(*expectConfig)

// CaptureID stores the expected message id under alias for later responses.
func CaptureID(alias string) ExpectOption {
	return func(config *expectConfig) {
		config.captureID = alias
	}
}

// WithParams checks the expected message params as JSON.
func WithParams(params any) ExpectOption {
	return func(config *expectConfig) {
		config.params = params
		config.hasParams = true
	}
}

// WithoutParams rejects an expected message that carries params.
func WithoutParams() ExpectOption {
	return func(config *expectConfig) {
		config.noParams = true
	}
}

// WithResult checks the expected response result as JSON.
func WithResult(result any) ExpectOption {
	return func(config *expectConfig) {
		config.result = result
		config.hasResult = true
	}
}

// CheckMessage adds a custom expected-message assertion.
func CheckMessage(check func(Message) error) ExpectOption {
	return func(config *expectConfig) {
		config.check = check
	}
}

type outputConfig struct {
	crlf         bool
	fragmentSize []int
}

// OutputOption configures fake app-server stdout writes.
type OutputOption func(*outputConfig)

// CRLF writes the script output line with CRLF instead of LF.
func CRLF() OutputOption {
	return func(config *outputConfig) {
		config.crlf = true
	}
}

// Fragmented writes the script output in deterministic fragments.
func Fragmented(sizes ...int) OutputOption {
	return func(config *outputConfig) {
		config.fragmentSize = append([]int(nil), sizes...)
	}
}

// ExpectRequest waits for a client JSON-RPC request with method.
func ExpectRequest(method string, options ...ExpectOption) Step {
	return expectStep("expect request "+method, func(message Message) error {
		return requireRequestShape(message, method)
	}, options...)
}

// ExpectNotification waits for a client JSON-RPC notification with method.
func ExpectNotification(method string, options ...ExpectOption) Step {
	return expectStep("expect notification "+method, func(message Message) error {
		return requireNotificationShape(message, method)
	}, options...)
}

// ExpectResponseID waits for a client JSON-RPC success response with id.
func ExpectResponseID(id any, options ...ExpectOption) Step {
	return expectStep("expect response", func(message Message) error {
		return requireResponseShape(message, mustMarshalID(id))
	}, options...)
}

// ExpectResponseFor waits for a client JSON-RPC success response to a captured id.
func ExpectResponseFor(alias string, options ...ExpectOption) Step {
	return Step{
		name: "expect response for " + alias,
		run: func(context *scriptContext) error {
			id, err := context.id(alias)
			if err != nil {
				return err
			}
			step := ExpectResponseID(json.RawMessage(id), options...)
			return step.run(context)
		},
	}
}

// ExpectErrorResponseID waits for a client JSON-RPC error response with id.
func ExpectErrorResponseID(id any, code int, message string, options ...ExpectOption) Step {
	return expectStep("expect error response", func(actual Message) error {
		return requireErrorResponseShape(actual, mustMarshalID(id), code, message)
	}, options...)
}

// ExpectErrorResponseFor waits for a client JSON-RPC error response to a captured id.
func ExpectErrorResponseFor(alias string, code int, message string, options ...ExpectOption) Step {
	return Step{
		name: "expect error response for " + alias,
		run: func(context *scriptContext) error {
			id, err := context.id(alias)
			if err != nil {
				return err
			}
			step := ExpectErrorResponseID(json.RawMessage(id), code, message, options...)
			return step.run(context)
		},
	}
}

// SendRequest writes a server-initiated JSON-RPC request to stdout.
func SendRequest(id any, method string, params any, options ...OutputOption) Step {
	return sendMessageStep("send request "+method, Request(id, method, params), options...)
}

// SendNotification writes a server JSON-RPC notification to stdout.
func SendNotification(method string, params any, options ...OutputOption) Step {
	return sendMessageStep("send notification "+method, Notification(method, params), options...)
}

// SendResponseID writes a server JSON-RPC success response with id to stdout.
func SendResponseID(id any, result any, options ...OutputOption) Step {
	return sendMessageStep("send response", Response(id, result), options...)
}

// SendResponseFor writes a server JSON-RPC success response to a captured request id.
func SendResponseFor(alias string, result any, options ...OutputOption) Step {
	return Step{
		name: "send response for " + alias,
		run: func(context *scriptContext) error {
			id, err := context.id(alias)
			if err != nil {
				return err
			}
			return context.writeMessage(Response(json.RawMessage(id), result), outputOptions(options))
		},
	}
}

// SendResponseForIgnoringWriteError writes a captured success response and
// ignores write errors from peers that have already closed the connection.
func SendResponseForIgnoringWriteError(alias string, result any, options ...OutputOption) Step {
	return Step{
		name: "send response for " + alias + " ignoring write error",
		run: func(context *scriptContext) error {
			id, err := context.id(alias)
			if err != nil {
				return err
			}
			_ = context.writeMessage(Response(json.RawMessage(id), result), outputOptions(options))
			return nil
		},
	}
}

// SendErrorResponseID writes a server JSON-RPC error response with id to stdout.
func SendErrorResponseID(id any, code int, message string, options ...OutputOption) Step {
	return sendMessageStep("send error response", ErrorResponse(id, code, message), options...)
}

// SendErrorResponseFor writes a server JSON-RPC error response to a captured request id.
func SendErrorResponseFor(alias string, code int, message string, options ...OutputOption) Step {
	return Step{
		name: "send error response for " + alias,
		run: func(context *scriptContext) error {
			id, err := context.id(alias)
			if err != nil {
				return err
			}
			return context.writeMessage(ErrorResponse(json.RawMessage(id), code, message), outputOptions(options))
		},
	}
}

// SendMalformedJSONL writes one malformed JSONL line to stdout.
func SendMalformedJSONL(raw string, options ...OutputOption) Step {
	return Step{
		name: "send malformed JSONL",
		run: func(context *scriptContext) error {
			return context.writeLine([]byte(raw), outputOptions(options))
		},
	}
}

// ClosePeer closes fake app-server stdout and stdin normally.
func ClosePeer() Step {
	return Step{
		name: "close peer",
		run: func(context *scriptContext) error {
			return errors.Join(
				context.harness.serverStdout.Close(),
				context.harness.serverStdin.Close(),
			)
		},
	}
}

// Delay pauses the fake app-server script before continuing to the next step.
func Delay(duration time.Duration) Step {
	return Step{
		name: "delay",
		run: func(context *scriptContext) error {
			timer := time.NewTimer(duration)
			defer timer.Stop()

			select {
			case <-timer.C:
				return nil
			case <-context.harness.done:
				return nil
			}
		},
	}
}

// WaitForSignal blocks the fake app-server script until signal is closed.
func WaitForSignal(name string, signal <-chan struct{}) Step {
	return Step{
		name: "wait for signal " + name,
		run: func(context *scriptContext) error {
			if signal == nil {
				return fmt.Errorf("wait for signal %s: nil signal", name)
			}
			select {
			case <-signal:
				return nil
			case <-context.harness.done:
				return nil
			}
		},
	}
}

// CrashPeer closes fake app-server stdout and stdin with err.
func CrashPeer(err error) Step {
	return Step{
		name: "crash peer",
		run: func(context *scriptContext) error {
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return errors.Join(
				context.harness.serverStdout.CloseWithError(err),
				context.harness.serverStdin.CloseWithError(err),
			)
		},
	}
}

func expectStep(name string, requireShape func(Message) error, options ...ExpectOption) Step {
	return Step{
		name: name,
		run: func(context *scriptContext) error {
			config := expectOptions(options)
			message, err := context.nextOutbound()
			if err != nil {
				return err
			}
			if err := requireShape(message); err != nil {
				return err
			}
			if config.hasParams {
				if err := requireJSONEqual(message.Params, config.params, "params"); err != nil {
					return err
				}
			}
			if config.noParams && len(message.Params) != 0 {
				return fmt.Errorf("params = %s, want none", message.Params)
			}
			if config.hasResult {
				if err := requireJSONEqual(message.Result, config.result, "result"); err != nil {
					return err
				}
			}
			if config.check != nil {
				if err := config.check(message.Clone()); err != nil {
					return err
				}
			}
			if config.captureID != "" {
				if len(message.ID) == 0 {
					return fmt.Errorf("cannot capture missing id as %q", config.captureID)
				}
				context.ids[config.captureID] = jsonID(cloneRaw(message.ID))
			}
			return nil
		},
	}
}

func sendMessageStep(name string, message Message, options ...OutputOption) Step {
	return Step{
		name: name,
		run: func(context *scriptContext) error {
			return context.writeMessage(message, outputOptions(options))
		},
	}
}

func (context *scriptContext) nextOutbound() (Message, error) {
	record, ok := <-context.harness.inbound
	if !ok {
		return Message{}, fmt.Errorf("client stdin closed before expected message")
	}
	if record.err != nil {
		return Message{}, record.err
	}
	return record.message, nil
}

func (context *scriptContext) id(alias string) (jsonID, error) {
	id, ok := context.ids[alias]
	if !ok {
		return nil, fmt.Errorf("captured id %q not found", alias)
	}
	return id, nil
}

func (context *scriptContext) writeMessage(message Message, config outputConfig) error {
	encoded, err := message.JSON()
	if err != nil {
		return err
	}
	return context.writeLine(encoded, config)
}

func (context *scriptContext) writeLine(line []byte, config outputConfig) error {
	ending := []byte("\n")
	if config.crlf {
		ending = []byte("\r\n")
	}
	payload := append(append([]byte(nil), line...), ending...)
	return writeFragments(context.harness.serverStdout, payload, config.fragmentSize)
}

func writeFragments(writer io.Writer, payload []byte, sizes []int) error {
	if len(sizes) == 0 {
		_, err := writer.Write(payload)
		return err
	}

	offset := 0
	for _, size := range sizes {
		if size <= 0 {
			return fmt.Errorf("fragment size must be positive, got %d", size)
		}
		if offset >= len(payload) {
			return nil
		}
		end := offset + size
		if end > len(payload) {
			end = len(payload)
		}
		if _, err := writer.Write(payload[offset:end]); err != nil {
			return err
		}
		offset = end
	}
	if offset < len(payload) {
		_, err := writer.Write(payload[offset:])
		return err
	}
	return nil
}

func expectOptions(options []ExpectOption) expectConfig {
	var config expectConfig
	for _, option := range options {
		option(&config)
	}
	return config
}

func outputOptions(options []OutputOption) outputConfig {
	var config outputConfig
	for _, option := range options {
		option(&config)
	}
	return config
}

func requireRequestShape(message Message, method string) error {
	if message.JSONRPC != jsonrpcVersion {
		return fmt.Errorf("jsonrpc = %q, want %q", message.JSONRPC, jsonrpcVersion)
	}
	if len(message.ID) == 0 {
		return fmt.Errorf("missing request id")
	}
	if message.Method != method {
		return fmt.Errorf("method = %q, want %q", message.Method, method)
	}
	if len(message.Result) != 0 || message.Error != nil {
		return fmt.Errorf("request carries response fields")
	}
	return nil
}

func requireNotificationShape(message Message, method string) error {
	if message.JSONRPC != jsonrpcVersion {
		return fmt.Errorf("jsonrpc = %q, want %q", message.JSONRPC, jsonrpcVersion)
	}
	if len(message.ID) != 0 {
		return fmt.Errorf("notification carries id %s", message.ID)
	}
	if message.Method != method {
		return fmt.Errorf("method = %q, want %q", message.Method, method)
	}
	if len(message.Result) != 0 || message.Error != nil {
		return fmt.Errorf("notification carries response fields")
	}
	return nil
}

func requireResponseShape(message Message, id json.RawMessage) error {
	if err := requireResponseID(message, id); err != nil {
		return err
	}
	if len(message.Result) == 0 {
		return fmt.Errorf("missing result")
	}
	if message.Error != nil {
		return fmt.Errorf("success response carries error")
	}
	return nil
}

func requireErrorResponseShape(message Message, id json.RawMessage, code int, errorMessage string) error {
	if err := requireResponseID(message, id); err != nil {
		return err
	}
	if len(message.Result) != 0 {
		return fmt.Errorf("error response carries result")
	}
	if message.Error == nil {
		return fmt.Errorf("missing error object")
	}
	if message.Error.Code != code || message.Error.Message != errorMessage {
		return fmt.Errorf("error = (%d, %q), want (%d, %q)", message.Error.Code, message.Error.Message, code, errorMessage)
	}
	if len(message.Error.Data) != 0 {
		return fmt.Errorf("error response carries data")
	}
	return nil
}

func requireResponseID(message Message, id json.RawMessage) error {
	if message.JSONRPC != jsonrpcVersion {
		return fmt.Errorf("jsonrpc = %q, want %q", message.JSONRPC, jsonrpcVersion)
	}
	if !bytes.Equal(message.ID, id) {
		return fmt.Errorf("id = %s, want %s", message.ID, id)
	}
	if message.Method != "" || len(message.Params) != 0 {
		return fmt.Errorf("response carries request fields")
	}
	return nil
}

func requireJSONEqual(got json.RawMessage, want any, label string) error {
	wantRaw := mustMarshalValue(want)
	if len(got) == 0 && len(wantRaw) == 0 {
		return nil
	}
	gotValue, err := decodeJSONValue(got)
	if err != nil {
		return fmt.Errorf("%s: decode actual: %w", label, err)
	}
	wantValue, err := decodeJSONValue(wantRaw)
	if err != nil {
		return fmt.Errorf("%s: decode expected: %w", label, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		return fmt.Errorf("%s = %s, want %s", label, got, wantRaw)
	}
	return nil
}

func decodeJSONValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("missing JSON value")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON data")
	}
	return value, nil
}
