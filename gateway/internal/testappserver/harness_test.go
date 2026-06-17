package testappserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestScriptOrderingAndLifecyclePrimitives(t *testing.T) {
	script := append([]Step{}, Initialize(`C:\codex-home`)...)
	script = append(script, ThreadStart("thread-1")...)
	harness := New(t, script...)
	reader := bufio.NewReader(harness.Stdout())

	writeMessage(t, harness.Stdin(), Request(1, MethodInitialize, map[string]any{
		"clientInfo": map[string]any{
			"name": "gateway-test",
		},
		"capabilities": map[string]any{
			"experimentalApi":    true,
			"requestAttestation": false,
		},
	}))
	assertMessage(t, readMessage(t, reader), Response(1, map[string]any{"codexHome": `C:\codex-home`}))

	writeMessage(t, harness.Stdin(), Notification(MethodInitialized, nil))
	writeMessage(t, harness.Stdin(), Request(2, MethodThreadStart, map[string]any{
		"cwd":               `C:\work`,
		"approvalPolicy":    "on-request",
		"approvalsReviewer": "user",
		"sandbox":           "workspace-write",
	}))
	assertMessage(t, readMessage(t, reader), Notification(MethodThreadStarted, ThreadStartedParams("thread-1")))
	assertMessage(t, readMessage(t, reader), Response(2, ThreadResult("thread-1")))

	harness.RequireDone(t)
	harness.RequireOutboundRequest(t, 0, MethodInitialize)
	harness.RequireOutboundNotification(t, 1, MethodInitialized)
	harness.RequireOutboundRequest(t, 2, MethodThreadStart)
}

func TestOutboundPayloadCaptureAndAssertions(t *testing.T) {
	input := turnStartParams("thread-1")
	harness := New(t,
		ExpectRequest(MethodTurnStart, CaptureID("turn"), WithParams(input)),
		SendResponseFor("turn", TurnResult("turn-1", "running")),
	)
	reader := bufio.NewReader(harness.Stdout())

	writeMessage(t, harness.Stdin(), Request(7, MethodTurnStart, input))
	assertMessage(t, readMessage(t, reader), Response(7, TurnResult("turn-1", "running")))

	harness.RequireDone(t)
	outbound := harness.RequireOutboundRequest(t, 0, MethodTurnStart)
	assertRawJSON(t, outbound.Params, input)
}

func TestLifecyclePrimitiveSequences(t *testing.T) {
	tests := []struct {
		name    string
		steps   []Step
		request Message
		want    []Message
	}{
		{
			name:    "thread resume",
			steps:   ThreadResume("thread-1"),
			request: Request(10, MethodThreadResume, map[string]any{"threadId": "thread-1"}),
			want: []Message{
				Notification(MethodThreadStarted, ThreadStartedParams("thread-1")),
				Response(10, ThreadResumeResult("thread-1")),
			},
		},
		{
			name:    "turn start",
			steps:   TurnStart("thread-1", "turn-1"),
			request: Request(11, MethodTurnStart, turnStartParams("thread-1")),
			want: []Message{
				Notification(MethodTurnStarted, TurnStartedParams("thread-1", "turn-1")),
				Response(11, TurnResult("turn-1", "running")),
			},
		},
		{
			name:    "turn interrupt",
			steps:   TurnInterrupt("thread-1", "turn-1"),
			request: Request(12, MethodTurnInterrupt, map[string]any{"threadId": "thread-1", "turnId": "turn-1"}),
			want: []Message{
				Response(12, TurnResult("turn-1", "interrupted")),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := New(t, tt.steps...)
			reader := bufio.NewReader(harness.Stdout())

			writeMessage(t, harness.Stdin(), tt.request)
			for _, want := range tt.want {
				assertMessage(t, readMessage(t, reader), want)
			}
			harness.RequireDone(t)
			harness.RequireOutboundRequest(t, 0, tt.request.Method)
		})
	}
}

func TestTurnStartRejectsInvalidInputShape(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{
			name:   "wrong thread id",
			params: turnStartParams("thread-other"),
			want:   `turn/start params.threadId = "thread-other", want "thread-1"`,
		},
		{
			name: "bare string input",
			params: map[string]any{
				"threadId": "thread-1",
				"input":    "hello",
			},
			want: "turn/start params.input = string, want array",
		},
		{
			name: "wrong text elements casing",
			params: map[string]any{
				"threadId": "thread-1",
				"input": []map[string]any{
					{
						"type":         "text",
						"text":         "hello",
						"textElements": []any{},
					},
				},
			},
			want: "turn/start params.input[0].text_elements missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newHarness(TurnStart("thread-1", "turn-1")...)
			defer harness.Close()

			writeMessage(t, harness.Stdin(), Request(1, MethodTurnStart, tt.params))
			err := waitForScriptError(t, harness)
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("script error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestServerRequestPrimitivesCaptureClientResponses(t *testing.T) {
	requests := []struct {
		name   string
		id     int
		method string
		send   Step
		params map[string]any
		result map[string]any
	}{
		{
			name:   "command approval",
			id:     101,
			method: MethodCommandApprovalRequest,
			send:   SendCommandApprovalRequest(101, "thread-1", "turn-1", "item-command"),
			params: map[string]any{
				"threadId": "thread-1",
				"turnId":   "turn-1",
				"item": map[string]any{
					"id":   "item-command",
					"type": "commandExecution",
				},
				"command": []string{"echo", "hello"},
			},
			result: map[string]any{"decision": "acceptForSession"},
		},
		{
			name:   "file approval",
			id:     102,
			method: MethodFileApprovalRequest,
			send:   SendFileApprovalRequest(102, "thread-1", "turn-1", "item-file"),
			params: map[string]any{
				"threadId": "thread-1",
				"turnId":   "turn-1",
				"item": map[string]any{
					"id":   "item-file",
					"type": "fileChange",
				},
				"changes": []map[string]any{
					{
						"path":   "README.md",
						"action": "modify",
					},
				},
			},
			result: map[string]any{"decision": "accept"},
		},
		{
			name:   "permissions approval",
			id:     103,
			method: MethodPermissionsRequest,
			send:   SendPermissionsRequest(103, "thread-1", "turn-1", "permissions-request"),
			params: map[string]any{
				"threadId":  "thread-1",
				"turnId":    "turn-1",
				"requestId": "permissions-request",
				"permissions": map[string]any{
					"network": map[string]any{
						"enabled": true,
					},
				},
			},
			result: map[string]any{"permissions": map[string]any{}},
		},
		{
			name:   "MCP elicitation",
			id:     104,
			method: MethodMcpElicitationRequest,
			send:   SendMcpElicitationRequest(104, "thread-1", "turn-1", "mcp-request"),
			params: map[string]any{
				"threadId":   "thread-1",
				"turnId":     "turn-1",
				"requestId":  "mcp-request",
				"serverName": "test-mcp",
				"message":    "choose a value",
				"schema": map[string]any{
					"type": "object",
				},
			},
			result: map[string]any{"action": "decline", "content": nil, "_meta": nil},
		},
		{
			name:   "tool user input",
			id:     105,
			method: MethodToolUserInputRequest,
			send:   SendToolUserInputRequest(105, "thread-1", "turn-1", "tool-input"),
			params: map[string]any{
				"threadId":  "thread-1",
				"turnId":    "turn-1",
				"requestId": "tool-input",
				"questions": []map[string]any{
					{
						"id":       "q1",
						"label":    "Choice",
						"required": true,
						"options": []map[string]any{
							{"label": "One", "value": "one"},
							{"label": "Two", "value": "two"},
						},
					},
				},
			},
			result: map[string]any{
				"answers": map[string]any{
					"q1": map[string]any{"answers": []string{"one"}},
				},
			},
		},
	}
	steps := make([]Step, 0, len(requests)*2)
	for _, request := range requests {
		steps = append(steps, request.send, ExpectResponseID(request.id, WithResult(request.result)))
	}
	harness := New(t, steps...)
	reader := bufio.NewReader(harness.Stdout())

	for _, expected := range requests {
		t.Logf("checking %s request", expected.name)
		request := readMessage(t, reader)
		if err := requireRequestShape(request, expected.method); err != nil {
			t.Fatalf("%s request shape: %v", expected.name, err)
		}
		assertRawJSON(t, request.Params, expected.params)
		writeMessage(t, harness.Stdin(), Response(expected.id, expected.result))
	}

	harness.RequireDone(t)
	for index, expected := range requests {
		response := harness.RequireOutboundResponse(t, index, expected.id)
		assertRawJSON(t, response.Result, expected.result)
	}
}

func TestScriptFailureClosesStdoutWithScriptError(t *testing.T) {
	harness := newHarness(ExpectRequest(MethodInitialize))
	defer harness.Close()

	writeMessage(t, harness.Stdin(), Notification(MethodInitialized, nil))

	readDone := make(chan error, 1)
	go func() {
		_, err := harness.Stdout().Read(make([]byte, 1))
		readDone <- err
	}()

	var stdoutErr error
	select {
	case stdoutErr = <-readDone:
		if stdoutErr == nil {
			t.Fatal("Stdout().Read() succeeded, want script error")
		}
	case <-time.After(requireTimeout):
		_ = harness.Close()
		t.Fatalf("Stdout().Read() did not unblock within %s", requireTimeout)
	}

	scriptErr := waitForScriptError(t, harness)
	if stdoutErr.Error() != scriptErr.Error() {
		t.Fatalf("Stdout().Read() error = %q, want script error %q", stdoutErr, scriptErr)
	}
}

func TestFragmentedOutput(t *testing.T) {
	harness := New(t, SendResponseID(1, map[string]any{"ok": true}, Fragmented(1, 2, 3, 5)))
	reader := bufio.NewReader(harness.Stdout())

	assertMessage(t, readMessage(t, reader), Response(1, map[string]any{"ok": true}))
	harness.RequireDone(t)
}

func TestCRLFOutput(t *testing.T) {
	harness := New(t, SendNotification("warning", map[string]any{"message": "crlf"}, CRLF()))
	reader := bufio.NewReader(harness.Stdout())

	line, err := readLine(t, reader)
	if err != nil {
		t.Fatalf("ReadBytes() error = %v", err)
	}
	if !strings.HasSuffix(string(line), "\r\n") {
		t.Fatalf("line ending = %q, want CRLF", line)
	}
	message, err := parseMessageLine(line)
	if err != nil {
		t.Fatalf("parseMessageLine() error = %v", err)
	}
	assertMessage(t, message, Notification("warning", map[string]any{"message": "crlf"}))
	harness.RequireDone(t)
}

func TestMalformedJSONLAndPeerClosePrimitives(t *testing.T) {
	harness := New(t, SendMalformedJSONL(`{"jsonrpc":"2.0","id":`, CRLF()), ClosePeer())
	reader := bufio.NewReader(harness.Stdout())

	line, err := readLine(t, reader)
	if err != nil {
		t.Fatalf("ReadBytes() malformed line error = %v", err)
	}
	if _, err := parseMessageLine(line); err == nil {
		t.Fatal("parseMessageLine() accepted malformed JSONL")
	}
	if _, err := readLine(t, reader); !errors.Is(err, io.EOF) {
		t.Fatalf("ReadBytes() after peer close error = %v, want EOF", err)
	}
	harness.RequireDone(t)
}

func TestClosePeerClosesStdinAndStopsOutboundCapture(t *testing.T) {
	harness := New(t, ClosePeer())
	harness.RequireDone(t)

	if err := writeMessageError(harness.Stdin(), Request(1, MethodInitialize, nil)); err == nil {
		t.Fatal("Write() after peer close succeeded, want error")
	}
	if messages := harness.OutboundMessages(); len(messages) != 0 {
		t.Fatalf("captured outbound messages after peer close = %d, want 0", len(messages))
	}
}

func TestCrashPeerPrimitive(t *testing.T) {
	crashErr := errors.New("fake app-server crash")
	harness := New(t, CrashPeer(crashErr))
	reader := bufio.NewReader(harness.Stdout())

	_, err := readLine(t, reader)
	if !errors.Is(err, crashErr) {
		t.Fatalf("ReadBytes() error = %v, want %v", err, crashErr)
	}
	harness.RequireDone(t)
}

func TestCrashPeerClosesStdinWithCrashErrorAndStopsOutboundCapture(t *testing.T) {
	crashErr := errors.New("fake app-server crash")
	harness := New(t, CrashPeer(crashErr))
	harness.RequireDone(t)

	err := writeMessageError(harness.Stdin(), Request(1, MethodInitialize, nil))
	if !errors.Is(err, crashErr) {
		t.Fatalf("Write() after peer crash error = %v, want %v", err, crashErr)
	}
	if messages := harness.OutboundMessages(); len(messages) != 0 {
		t.Fatalf("captured outbound messages after peer crash = %d, want 0", len(messages))
	}
}

func TestExpectErrorResponseRejectsResultAndErrorData(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		want    string
	}{
		{
			name: "result and error",
			message: func() Message {
				message := ErrorResponse(101, -32002, "unsupported_server_request")
				message.Result = mustMarshalValue(map[string]any{"unexpected": true})
				return message
			}(),
			want: "error response carries result",
		},
		{
			name: "error data",
			message: func() Message {
				message := ErrorResponse(101, -32002, "unsupported_server_request")
				message.Error.Data = mustMarshalValue(map[string]any{"details": "leak"})
				return message
			}(),
			want: "error response carries data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newHarness(ExpectErrorResponseID(101, -32002, "unsupported_server_request"))
			defer harness.Close()

			writeMessage(t, harness.Stdin(), tt.message)
			err := waitForScriptError(t, harness)
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("script error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestCaptureOnlyOutboundMessagesDoNotBlockScriptQueue(t *testing.T) {
	harness := New(t)
	const messageCount = 64

	writeDone := make(chan error, 1)
	go func() {
		for i := 0; i < messageCount; i++ {
			if err := writeMessageError(harness.Stdin(), Request(i+1, MethodTurnStart, map[string]any{"seq": i})); err != nil {
				writeDone <- err
				return
			}
		}
		writeDone <- nil
	}()

	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("write outbound messages: %v", err)
		}
	case <-time.After(requireTimeout):
		_ = harness.Close()
		t.Fatalf("writing %d capture-only outbound messages timed out", messageCount)
	}

	messages := harness.RequireOutboundCount(t, messageCount)
	for index, message := range messages {
		if err := requireRequestShape(message, MethodTurnStart); err != nil {
			t.Fatalf("outbound[%d] shape: %v", index, err)
		}
	}
	harness.RequireDone(t)
}

func TestInitializeRejectsMissingOrWrongCapabilities(t *testing.T) {
	tests := []struct {
		name         string
		capabilities map[string]any
		want         string
	}{
		{
			name:         "missing experimentalApi",
			capabilities: map[string]any{"requestAttestation": false},
			want:         "initialize capabilities.experimentalApi missing",
		},
		{
			name:         "false experimentalApi",
			capabilities: map[string]any{"experimentalApi": false, "requestAttestation": false},
			want:         "initialize capabilities.experimentalApi = false, want true",
		},
		{
			name:         "missing requestAttestation",
			capabilities: map[string]any{"experimentalApi": true},
			want:         "initialize capabilities.requestAttestation missing",
		},
		{
			name:         "true requestAttestation",
			capabilities: map[string]any{"experimentalApi": true, "requestAttestation": true},
			want:         "initialize capabilities.requestAttestation = true, want false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newHarness(Initialize(`C:\codex-home`)...)
			defer harness.Close()

			writeMessage(t, harness.Stdin(), Request(1, MethodInitialize, map[string]any{
				"clientInfo":   map[string]any{"name": "gateway-test"},
				"capabilities": tt.capabilities,
			}))
			err := waitForScriptError(t, harness)
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("script error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestInitializeRejectsInitializedParams(t *testing.T) {
	harness := newHarness(Initialize(`C:\codex-home`)...)
	defer harness.Close()
	reader := bufio.NewReader(harness.Stdout())

	writeMessage(t, harness.Stdin(), Request(1, MethodInitialize, map[string]any{
		"clientInfo": map[string]any{
			"name": "gateway-test",
		},
		"capabilities": map[string]any{
			"experimentalApi":    true,
			"requestAttestation": false,
		},
	}))
	assertMessage(t, readMessage(t, reader), Response(1, map[string]any{"codexHome": `C:\codex-home`}))

	writeMessage(t, harness.Stdin(), Notification(MethodInitialized, map[string]any{"unexpected": true}))
	err := waitForScriptError(t, harness)
	if !strings.Contains(err.Error(), "params =") {
		t.Fatalf("script error = %q, want initialized params rejection", err)
	}
}

func turnStartParams(threadID string) map[string]any {
	return map[string]any{
		"threadId": threadID,
		"input": []map[string]any{
			{
				"type":          "text",
				"text":          "hello",
				"text_elements": []any{},
			},
		},
	}
}

func writeMessage(t *testing.T, writer io.Writer, message Message) {
	t.Helper()

	if err := writeMessageError(writer, message); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
}

func writeMessageError(writer io.Writer, message Message) error {
	line, err := message.JSONL()
	if err != nil {
		return err
	}
	if _, err := writer.Write(line); err != nil {
		return err
	}
	return nil
}

func waitForScriptError(t *testing.T, harness *Harness) error {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- harness.Wait()
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("fake app-server script succeeded, want failure")
		}
		return err
	case <-time.After(requireTimeout):
		_ = harness.Close()
		t.Fatalf("fake app-server script did not fail within %s", requireTimeout)
		return nil
	}
}

func readMessage(t *testing.T, reader *bufio.Reader) Message {
	t.Helper()

	line, err := readLine(t, reader)
	if err != nil {
		t.Fatalf("ReadBytes() error = %v", err)
	}
	message, err := parseMessageLine(line)
	if err != nil {
		t.Fatalf("parseMessageLine() error = %v", err)
	}
	return message
}

func readLine(t *testing.T, reader *bufio.Reader) ([]byte, error) {
	t.Helper()

	type result struct {
		line []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := reader.ReadBytes('\n')
		done <- result{line: line, err: err}
	}()

	select {
	case result := <-done:
		return result.line, result.err
	case <-time.After(requireTimeout):
		t.Fatalf("ReadBytes() did not finish within %s", requireTimeout)
		return nil, nil
	}
}

func assertMessage(t *testing.T, got Message, want Message) {
	t.Helper()

	if got.JSONRPC != want.JSONRPC || got.Method != want.Method || string(got.ID) != string(want.ID) {
		t.Fatalf("message identity = (%q, %q, %s), want (%q, %q, %s)", got.JSONRPC, got.Method, got.ID, want.JSONRPC, want.Method, want.ID)
	}
	assertRawJSON(t, got.Params, want.Params)
	assertRawJSON(t, got.Result, want.Result)
	if !reflect.DeepEqual(got.Error, want.Error) {
		t.Fatalf("message error = %#v, want %#v", got.Error, want.Error)
	}
}

func assertRawJSON(t *testing.T, got json.RawMessage, want any) {
	t.Helper()

	if len(got) == 0 && want == nil {
		return
	}
	if err := requireJSONEqual(got, want, "json"); err != nil {
		t.Fatal(err)
	}
}
