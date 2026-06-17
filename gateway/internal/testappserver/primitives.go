package testappserver

import "fmt"

const (
	MethodInitialize             = "initialize"
	MethodInitialized            = "initialized"
	MethodThreadStart            = "thread/start"
	MethodThreadResume           = "thread/resume"
	MethodThreadStarted          = "thread/started"
	MethodTurnStart              = "turn/start"
	MethodTurnStarted            = "turn/started"
	MethodTurnCompleted          = "turn/completed"
	MethodTurnInterrupt          = "turn/interrupt"
	MethodCommandApprovalRequest = "item/commandExecution/requestApproval"
	MethodFileApprovalRequest    = "item/fileChange/requestApproval"
	MethodPermissionsRequest     = "item/permissions/requestApproval"
	MethodMcpElicitationRequest  = "mcpServer/elicitation/request"
	MethodToolUserInputRequest   = "item/tool/requestUserInput"
)

// Initialize expects initialize, responds with codexHome, then expects initialized.
func Initialize(codexHome string) []Step {
	return []Step{
		ExpectRequest(MethodInitialize, CaptureID(MethodInitialize), CheckMessage(requireInitializeCapabilities)),
		SendResponseFor(MethodInitialize, map[string]any{"codexHome": codexHome}),
		ExpectNotification(MethodInitialized, WithoutParams()),
	}
}

func requireInitializeCapabilities(message Message) error {
	value, err := decodeJSONValue(message.Params)
	if err != nil {
		return fmt.Errorf("initialize params: %w", err)
	}
	params, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("initialize params must be object")
	}
	capabilitiesValue, ok := params["capabilities"]
	if !ok {
		return fmt.Errorf("initialize capabilities missing")
	}
	capabilities, ok := capabilitiesValue.(map[string]any)
	if !ok {
		return fmt.Errorf("initialize capabilities must be object")
	}
	if err := requireBoolCapability(capabilities, "experimentalApi", true); err != nil {
		return err
	}
	if err := requireBoolCapability(capabilities, "requestAttestation", false); err != nil {
		return err
	}
	return nil
}

func requireBoolCapability(capabilities map[string]any, name string, want bool) error {
	value, ok := capabilities[name]
	if !ok {
		return fmt.Errorf("initialize capabilities.%s missing", name)
	}
	got, ok := value.(bool)
	if !ok {
		return fmt.Errorf("initialize capabilities.%s = %T, want bool", name, value)
	}
	if got != want {
		return fmt.Errorf("initialize capabilities.%s = %t, want %t", name, got, want)
	}
	return nil
}

// ThreadStart expects thread/start, emits thread/started, then responds.
func ThreadStart(threadID string) []Step {
	return []Step{
		ExpectRequest(MethodThreadStart, CaptureID(MethodThreadStart)),
		SendNotification(MethodThreadStarted, ThreadStartedParams(threadID)),
		SendResponseFor(MethodThreadStart, ThreadResult(threadID)),
	}
}

// ThreadResume expects thread/resume, emits thread/started, then responds.
func ThreadResume(threadID string) []Step {
	return []Step{
		ExpectRequest(MethodThreadResume, CaptureID(MethodThreadResume)),
		SendNotification(MethodThreadStarted, ThreadStartedParams(threadID)),
		SendResponseFor(MethodThreadResume, ThreadResumeResult(threadID)),
	}
}

// TurnStart expects turn/start, emits turn/started, then responds.
func TurnStart(threadID string, turnID string) []Step {
	return []Step{
		ExpectRequest(MethodTurnStart, CaptureID(MethodTurnStart), CheckMessage(func(message Message) error {
			return requireTurnStartParams(message, threadID)
		})),
		SendNotification(MethodTurnStarted, TurnStartedParams(threadID, turnID)),
		SendResponseFor(MethodTurnStart, TurnResult(turnID, "running")),
	}
}

// TurnInterrupt expects turn/interrupt and responds with an interrupted turn.
func TurnInterrupt(threadID string, turnID string) []Step {
	return []Step{
		ExpectRequest(MethodTurnInterrupt, CaptureID(MethodTurnInterrupt), CheckMessage(func(message Message) error {
			return requireTurnInterruptParams(message, threadID, turnID)
		})),
		SendResponseFor(MethodTurnInterrupt, TurnResult(turnID, "interrupted")),
	}
}

func requireTurnInterruptParams(message Message, threadID string, turnID string) error {
	value, err := decodeJSONValue(message.Params)
	if err != nil {
		return fmt.Errorf("turn/interrupt params: %w", err)
	}
	params, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("turn/interrupt params must be object")
	}
	threadIDValue, ok := params["threadId"]
	if !ok {
		return fmt.Errorf("turn/interrupt params.threadId missing")
	}
	actualThreadID, ok := threadIDValue.(string)
	if !ok {
		return fmt.Errorf("turn/interrupt params.threadId = %T, want string", threadIDValue)
	}
	if actualThreadID != threadID {
		return fmt.Errorf("turn/interrupt params.threadId = %q, want %q", actualThreadID, threadID)
	}
	turnIDValue, ok := params["turnId"]
	if !ok {
		return fmt.Errorf("turn/interrupt params.turnId missing")
	}
	actualTurnID, ok := turnIDValue.(string)
	if !ok {
		return fmt.Errorf("turn/interrupt params.turnId = %T, want string", turnIDValue)
	}
	if actualTurnID != turnID {
		return fmt.Errorf("turn/interrupt params.turnId = %q, want %q", actualTurnID, turnID)
	}
	return nil
}

func requireTurnStartParams(message Message, threadID string) error {
	value, err := decodeJSONValue(message.Params)
	if err != nil {
		return fmt.Errorf("turn/start params: %w", err)
	}
	params, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("turn/start params must be object")
	}
	threadIDValue, ok := params["threadId"]
	if !ok {
		return fmt.Errorf("turn/start params.threadId missing")
	}
	actualThreadID, ok := threadIDValue.(string)
	if !ok {
		return fmt.Errorf("turn/start params.threadId = %T, want string", threadIDValue)
	}
	if actualThreadID != threadID {
		return fmt.Errorf("turn/start params.threadId = %q, want %q", actualThreadID, threadID)
	}
	inputValue, ok := params["input"]
	if !ok {
		return fmt.Errorf("turn/start params.input missing")
	}
	input, ok := inputValue.([]any)
	if !ok {
		return fmt.Errorf("turn/start params.input = %T, want array", inputValue)
	}
	if len(input) != 1 {
		return fmt.Errorf("turn/start params.input length = %d, want 1", len(input))
	}
	item, ok := input[0].(map[string]any)
	if !ok {
		return fmt.Errorf("turn/start params.input[0] = %T, want object", input[0])
	}
	itemTypeValue, ok := item["type"]
	if !ok {
		return fmt.Errorf("turn/start params.input[0].type missing")
	}
	itemType, ok := itemTypeValue.(string)
	if !ok {
		return fmt.Errorf("turn/start params.input[0].type = %T, want string", itemTypeValue)
	}
	if itemType != "text" {
		return fmt.Errorf("turn/start params.input[0].type = %q, want %q", itemType, "text")
	}
	textValue, ok := item["text"]
	if !ok {
		return fmt.Errorf("turn/start params.input[0].text missing")
	}
	if _, ok := textValue.(string); !ok {
		return fmt.Errorf("turn/start params.input[0].text = %T, want string", textValue)
	}
	textElementsValue, ok := item["text_elements"]
	if !ok {
		return fmt.Errorf("turn/start params.input[0].text_elements missing")
	}
	textElements, ok := textElementsValue.([]any)
	if !ok {
		return fmt.Errorf("turn/start params.input[0].text_elements = %T, want array", textElementsValue)
	}
	if len(textElements) != 0 {
		return fmt.Errorf("turn/start params.input[0].text_elements length = %d, want 0", len(textElements))
	}
	return nil
}

// ThreadResult is a minimal app-server-like thread response payload.
func ThreadResult(threadID string) map[string]any {
	return map[string]any{
		"thread": map[string]any{
			"id": threadID,
		},
	}
}

// ThreadResumeResult is a minimal app-server-like thread/resume response payload.
func ThreadResumeResult(threadID string) map[string]any {
	result := ThreadResult(threadID)
	result["initialTurnsPage"] = map[string]any{
		"items":      []any{},
		"nextCursor": nil,
	}
	result["activePermissionProfile"] = nil
	return result
}

// ThreadStartedParams is a minimal thread/started notification payload.
func ThreadStartedParams(threadID string) map[string]any {
	return ThreadResult(threadID)
}

// TurnResult is a minimal app-server-like turn response payload.
func TurnResult(turnID string, status string) map[string]any {
	return map[string]any{
		"turn": map[string]any{
			"id":     turnID,
			"status": status,
		},
	}
}

// TurnStartedParams is a minimal turn/started notification payload.
func TurnStartedParams(threadID string, turnID string) map[string]any {
	params := TurnResult(turnID, "running")
	params["threadId"] = threadID
	return params
}

// TurnCompletedParams is a minimal turn/completed notification payload.
func TurnCompletedParams(threadID string, turnID string, status string) map[string]any {
	params := TurnResult(turnID, status)
	params["threadId"] = threadID
	return params
}

// SendCommandApprovalRequest writes a command approval server request.
func SendCommandApprovalRequest(id any, threadID string, turnID string, itemID string, options ...OutputOption) Step {
	return SendRequest(id, MethodCommandApprovalRequest, map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
		"item": map[string]any{
			"id":   itemID,
			"type": "commandExecution",
		},
		"command": []string{"echo", "hello"},
	}, options...)
}

// SendFileApprovalRequest writes a file-change approval server request.
func SendFileApprovalRequest(id any, threadID string, turnID string, itemID string, options ...OutputOption) Step {
	return SendRequest(id, MethodFileApprovalRequest, map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
		"item": map[string]any{
			"id":   itemID,
			"type": "fileChange",
		},
		"changes": []map[string]any{
			{
				"path":   "README.md",
				"action": "modify",
			},
		},
	}, options...)
}

// SendPermissionsRequest writes a permissions approval server request.
func SendPermissionsRequest(id any, threadID string, turnID string, requestID string, options ...OutputOption) Step {
	return SendRequest(id, MethodPermissionsRequest, map[string]any{
		"threadId":  threadID,
		"turnId":    turnID,
		"requestId": requestID,
		"permissions": map[string]any{
			"network": map[string]any{
				"enabled": true,
			},
		},
	}, options...)
}

// SendMcpElicitationRequest writes an MCP elicitation server request.
func SendMcpElicitationRequest(id any, threadID string, turnID string, requestID string, options ...OutputOption) Step {
	return SendRequest(id, MethodMcpElicitationRequest, map[string]any{
		"threadId":   threadID,
		"turnId":     turnID,
		"requestId":  requestID,
		"serverName": "test-mcp",
		"message":    "choose a value",
		"schema": map[string]any{
			"type": "object",
		},
	}, options...)
}

// SendToolUserInputRequest writes a tool user-input server request.
func SendToolUserInputRequest(id any, threadID string, turnID string, requestID string, options ...OutputOption) Step {
	return SendRequest(id, MethodToolUserInputRequest, map[string]any{
		"threadId":  threadID,
		"turnId":    turnID,
		"requestId": requestID,
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
	}, options...)
}
