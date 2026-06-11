package pending

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dirard/codex-runtime/internal/domain"
	"github.com/Dirard/codex-runtime/internal/redact"
)

func TestBuildMcpElicitationPreservesHTTPURLDisplay(t *testing.T) {
	params := mustJSONRawMessage(t, map[string]any{
		"requestId": "mcp-url",
		"message":   "open this page",
		"url":       "https://example.com/docs",
	})

	result, err := Build(MethodMcpElicitation, params, json.RawMessage(`"jsonrpc-1"`), BuildInput{
		TaskID:          "task-1",
		ThreadID:        "thread-1",
		TurnID:          "turn-1",
		CreatedAtUnixMS: 123,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	display, ok := result.Record.Pending.Display.(domain.McpElicitationDisplay)
	if !ok {
		t.Fatalf("display = %T, want McpElicitationDisplay", result.Record.Pending.Display)
	}
	if display.Mode != domain.ElicitationModeURL || display.URL != "https://example.com/docs" {
		t.Fatalf("display = %#v, want URL mode with safe URL", display)
	}
}

func TestBuildMcpElicitationRejectsSecretBearingURLComponentsBeforeDisplay(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
	}{
		{
			name:   "query",
			rawURL: "https://example.com/docs?token=secret",
		},
		{
			name:   "bare query marker",
			rawURL: "https://example.com/docs?",
		},
		{
			name:   "fragment",
			rawURL: "https://example.com/docs#token",
		},
		{
			name:   "userinfo",
			rawURL: "https://user:pass@example.com/docs",
		},
		{
			name:   "over cap",
			rawURL: "https://example.com/" + strings.Repeat("a", domain.MaxSourceURIBytes),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := mustJSONRawMessage(t, map[string]any{
				"requestId": "mcp-url",
				"message":   "open this page",
				"url":       tt.rawURL,
			})

			result, err := Build(MethodMcpElicitation, params, json.RawMessage(`"jsonrpc-1"`), BuildInput{
				TaskID:          "task-1",
				ThreadID:        "thread-1",
				TurnID:          "turn-1",
				CreatedAtUnixMS: 123,
				RedactString: func(value string, maxBytes int, fallback string) string {
					t.Fatalf("RedactString called for unsafe URL display value %q", value)
					return fallback
				},
			})
			if !errors.Is(err, ErrOverLimit) {
				t.Fatalf("Build() error = %v, want ErrOverLimit", err)
			}
			if result.LimitReason != LimitReasonDisplayPayloadTooLarge ||
				result.RequestType != RequestTypeMcpElicitation ||
				result.Record.Pending.Display != nil {
				t.Fatalf("Build() result = %#v, want display-payload-too-large mcp elicitation without display", result)
			}
		})
	}
}

func TestBuildMcpElicitationRejectsTraversalURLBeforeDisplay(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
	}{
		{
			name:   "raw traversal",
			rawURL: "https://example.com/../x",
		},
		{
			name:   "encoded traversal",
			rawURL: "https://example.com/%2e%2e/x",
		},
		{
			name:   "double encoded traversal",
			rawURL: "https://example.com/%252e%252e/x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := mustJSONRawMessage(t, map[string]any{
				"requestId": "mcp-url",
				"message":   "open this page",
				"url":       tt.rawURL,
			})

			result, err := Build(MethodMcpElicitation, params, json.RawMessage(`"jsonrpc-1"`), BuildInput{
				TaskID:          "task-1",
				ThreadID:        "thread-1",
				TurnID:          "turn-1",
				CreatedAtUnixMS: 123,
				RedactString: func(value string, maxBytes int, fallback string) string {
					t.Fatalf("RedactString called for unsafe URL display value %q", value)
					return fallback
				},
			})
			if !errors.Is(err, ErrOverLimit) {
				t.Fatalf("Build() error = %v, want ErrOverLimit", err)
			}
			if result.LimitReason != LimitReasonDisplayPayloadTooLarge ||
				result.RequestType != RequestTypeMcpElicitation ||
				result.Record.Pending.Display != nil {
				t.Fatalf("Build() result = %#v, want display-payload-too-large mcp elicitation without display", result)
			}
		})
	}
}

func TestBuildMcpElicitationRejectsLocalURLMetadata(t *testing.T) {
	for _, rawURL := range []string{
		"file:///C:/Users/alice/.ssh/config",
		`C:\Users\alice\.ssh\config`,
		"/home/alice/.ssh/config",
		"vscode://file/C:/Users/alice/.ssh/config",
		"http://localhost:8080/docs",
		"http://127.0.0.1:8080/docs",
		"http://0x7f.0.0.1/docs",
		"http://0177.0.0.1/docs",
		"http://2130706433/docs",
		"https://10.0.0.5/private",
		"example.com/docs",
		"ssh://10.0.0.5/private",
		"http://[::1]/docs",
		"http://[fe80::1]/docs",
		"https://[fc00::1]/private",
		"http://service.local/status",
		"http://intranet/status",
	} {
		t.Run(rawURL, func(t *testing.T) {
			params := mustJSONRawMessage(t, map[string]any{
				"requestId": "mcp-url",
				"message":   "open this page",
				"url":       rawURL,
			})

			result, err := Build(MethodMcpElicitation, params, json.RawMessage(`"jsonrpc-1"`), BuildInput{
				TaskID:          "task-1",
				ThreadID:        "thread-1",
				TurnID:          "turn-1",
				CreatedAtUnixMS: 123,
			})
			if !errors.Is(err, ErrOverLimit) {
				t.Fatalf("Build() error = %v, want ErrOverLimit", err)
			}
			if result.LimitReason != LimitReasonDisplayPayloadTooLarge ||
				result.RequestType != RequestTypeMcpElicitation {
				t.Fatalf("Build() result = %#v", result)
			}
			if result.Record.Pending.Display != nil {
				t.Fatalf("Build() created public display for local URL: %#v", result.Record.Pending.Display)
			}
		})
	}
}

func TestBuildCommandApprovalSanitizesApprovalSecurityHostLabels(t *testing.T) {
	params := mustJSONRawMessage(t, map[string]any{
		"threadId": "thread-1",
		"turnId":   "turn-1",
		"item": map[string]any{
			"id":   "item-command",
			"type": "commandExecution",
		},
		"command": []string{"curl", "https://example.invalid"},
		"networkApprovalContext": map[string]any{
			"url":    "https://10.0.0.5:8443/private",
			"scheme": "https",
		},
		"proposedNetworkPolicyAmendments": []map[string]any{
			{
				"host":   "db.internal.local",
				"action": "allow",
			},
		},
	})

	result, err := Build(MethodCommandApproval, params, json.RawMessage(`"jsonrpc-1"`), BuildInput{
		TaskID:          "task-1",
		ThreadID:        "thread-1",
		TurnID:          "turn-1",
		CreatedAtUnixMS: 123,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	display, ok := result.Record.Pending.Display.(domain.CommandApprovalDisplay)
	if !ok {
		t.Fatalf("display = %T, want CommandApprovalDisplay", result.Record.Pending.Display)
	}
	if display.ApprovalSecurity == nil || display.ApprovalSecurity.NetworkContext == nil {
		t.Fatalf("approval security = %#v, want network context", display.ApprovalSecurity)
	}
	if display.ApprovalSecurity.NetworkContext.HostLabel != "network" {
		t.Fatalf("network host label = %q, want opaque network label", display.ApprovalSecurity.NetworkContext.HostLabel)
	}
	if len(display.ApprovalSecurity.NetworkPolicyAmendmentSummaries) != 1 ||
		display.ApprovalSecurity.NetworkPolicyAmendmentSummaries[0].HostLabel != "network" {
		t.Fatalf("network amendments = %#v, want opaque host label", display.ApprovalSecurity.NetworkPolicyAmendmentSummaries)
	}
	rawDisplay, err := json.Marshal(result.Record.Pending.Display)
	if err != nil {
		t.Fatalf("json.Marshal(display) error = %v", err)
	}
	for _, forbidden := range []string{"10.0.0.5", "https://10.0.0.5:8443/private", "db.internal.local"} {
		if strings.Contains(string(rawDisplay), forbidden) {
			t.Fatalf("display leaked raw network target %q: %s", forbidden, rawDisplay)
		}
	}
}

func TestBuildCommandApprovalTreatsStructuredDecisionsAsAdvanced(t *testing.T) {
	params := mustJSONRawMessage(t, map[string]any{
		"requestId": "cmd-advanced-decision",
		"command":   []string{"echo", "hello"},
		"availableDecisions": []any{
			map[string]any{"value": "accept", "label": "Accept with policy"},
		},
	})

	result, err := Build(MethodCommandApproval, params, json.RawMessage(`"jsonrpc-1"`), BuildInput{
		TaskID:          "task-1",
		ThreadID:        "thread-1",
		TurnID:          "turn-1",
		CreatedAtUnixMS: 123,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	display, ok := result.Record.Pending.Display.(domain.CommandApprovalDisplay)
	if !ok {
		t.Fatalf("display = %T, want CommandApprovalDisplay", result.Record.Pending.Display)
	}

	advancedSeen := false
	declineSelectable := false
	cancelSelectable := false
	for _, option := range display.DecisionOptions {
		if option.WireDecision == domain.ApprovalWireDecisionAccept && option.Selectable {
			t.Fatalf("decision options = %#v, want structured accept to stay non-selectable", display.DecisionOptions)
		}
		if !option.Selectable && option.UnsupportedReason == UnsupportedReasonAdvancedDecision {
			advancedSeen = true
		}
		switch option.WireDecision {
		case domain.ApprovalWireDecisionDecline:
			declineSelectable = option.Selectable
		case domain.ApprovalWireDecisionCancel:
			cancelSelectable = option.Selectable
		}
	}
	if !advancedSeen {
		t.Fatalf("decision options = %#v, want non-selectable advanced option", display.DecisionOptions)
	}
	if !declineSelectable || !cancelSelectable {
		t.Fatalf("decision options = %#v, want selectable decline and cancel fallbacks", display.DecisionOptions)
	}
}

func TestBuildCommandApprovalRejectsDuplicateDirectDecisionsBeforeDisplay(t *testing.T) {
	params := mustJSONRawMessage(t, map[string]any{
		"requestId": "cmd-duplicate-decision",
		"command":   []string{"echo", "hello"},
		"availableDecisions": []string{
			"accept",
			"accept",
		},
	})

	result, err := Build(MethodCommandApproval, params, json.RawMessage(`"jsonrpc-1"`), BuildInput{
		TaskID:          "task-1",
		ThreadID:        "thread-1",
		TurnID:          "turn-1",
		CreatedAtUnixMS: 123,
	})
	if !errors.Is(err, ErrOverLimit) {
		t.Fatalf("Build() error = %v, want ErrOverLimit", err)
	}
	if result.LimitReason != LimitReasonControlsTooLarge ||
		result.RequestType != RequestTypeCommandApproval ||
		result.Record.Pending.Display != nil {
		t.Fatalf("Build() result = %#v, want controls-too-large command approval without display", result)
	}
}

func TestBuildToolUserInputOptionsExposeSendableValues(t *testing.T) {
	params := mustJSONRawMessage(t, map[string]any{
		"requestId": "tool-input-1",
		"questions": []map[string]any{{
			"id":       "choice",
			"question": "Pick one",
			"options": []map[string]any{
				{"label": "One", "value": "one"},
				{"label": "Two", "id": "two"},
				{"label": "Three"},
				{"label": "Docs", "value": "https://example.com/docs"},
			},
		}},
	})

	result, err := Build(MethodToolUserInput, params, json.RawMessage(`"jsonrpc-1"`), BuildInput{
		TaskID:          "task-1",
		ThreadID:        "thread-1",
		TurnID:          "turn-1",
		CreatedAtUnixMS: 123,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	display, ok := result.Record.Pending.Display.(domain.ToolUserInputDisplay)
	if !ok {
		t.Fatalf("display = %T, want ToolUserInputDisplay", result.Record.Pending.Display)
	}
	wantOptions := []string{"one", "two", "Three", "https://example.com/docs"}
	if len(display.Questions) != 1 || strings.Join(display.Questions[0].Options, ",") != strings.Join(wantOptions, ",") {
		t.Fatalf("tool options = %#v, want %#v", display.Questions, wantOptions)
	}
	for _, value := range wantOptions {
		if _, ok := result.Record.ToolQuestions["choice"].AllowedValues[value]; !ok {
			t.Fatalf("allowed values = %#v, missing %q", result.Record.ToolQuestions["choice"].AllowedValues, value)
		}
	}
	if _, ok := result.Record.ToolQuestions["choice"].AllowedValues["One"]; ok {
		t.Fatalf("allowed values accepted hidden label token: %#v", result.Record.ToolQuestions["choice"].AllowedValues)
	}
}

func TestBuildToolUserInputRejectsExplicitBlankOrNullOptionTokensBeforeLabelFallback(t *testing.T) {
	tests := []struct {
		name   string
		option map[string]any
	}{
		{
			name:   "blank value",
			option: map[string]any{"label": "LabelToken", "value": ""},
		},
		{
			name:   "null value",
			option: map[string]any{"label": "LabelToken", "value": nil},
		},
		{
			name:   "blank id",
			option: map[string]any{"label": "LabelToken", "id": ""},
		},
		{
			name:   "null id",
			option: map[string]any{"label": "LabelToken", "id": nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := mustJSONRawMessage(t, map[string]any{
				"requestId": "tool-input-1",
				"questions": []map[string]any{{
					"id":       "choice",
					"question": "Pick one",
					"options":  []map[string]any{tt.option},
				}},
			})

			result, err := Build(MethodToolUserInput, params, json.RawMessage(`"jsonrpc-1"`), BuildInput{
				TaskID:          "task-1",
				ThreadID:        "thread-1",
				TurnID:          "turn-1",
				CreatedAtUnixMS: 123,
			})
			if !errors.Is(err, ErrOverLimit) {
				t.Fatalf("Build() error = %v, want ErrOverLimit", err)
			}
			if result.LimitReason != LimitReasonControlsTooLarge ||
				result.RequestType != RequestTypeToolUserInput ||
				result.Record.Pending.Display != nil {
				t.Fatalf("Build() result = %#v, want controls-too-large tool input without display", result)
			}
			if _, ok := result.Record.ToolQuestions["choice"]; ok {
				t.Fatalf("tool question was stored after invalid explicit option token: %#v", result.Record.ToolQuestions)
			}
		})
	}
}

func TestBuildToolUserInputRejectsUnsafeOptionTokensBeforeDisplay(t *testing.T) {
	redactor := redact.New()
	redactFunc := func(value string, maxBytes int, fallback string) string {
		redacted, _ := truncate(redactor.RedactString(value), maxBytes)
		if redacted == "" {
			return fallback
		}
		return redacted
	}
	tests := []struct {
		name    string
		options []any
		redact  func(string, int, string) string
	}{
		{
			name: "over cap",
			options: []any{
				map[string]any{"label": "Long", "value": strings.Repeat("x", domain.MaxSourceLabelBytes+1)},
			},
		},
		{
			name: "redacted path",
			options: []any{
				map[string]any{"label": "Path", "value": "/home/alice/.ssh/config"},
			},
			redact: redactFunc,
		},
		{
			name: "traversal value",
			options: []any{
				map[string]any{"label": "Safe label", "value": "../secret"},
			},
		},
		{
			name: "double encoded traversal value",
			options: []any{
				map[string]any{"label": "Safe label", "value": "%252e%252e%252fsecret"},
			},
		},
		{
			name: "traversal id",
			options: []any{
				map[string]any{"label": "Safe label", "id": `..\secret`},
			},
		},
		{
			name: "traversal label fallback",
			options: []any{
				map[string]any{"label": `%2e%2e%5csecret`},
			},
		},
		{
			name: "file URL",
			options: []any{
				map[string]any{"label": "Safe label", "value": "file:///C:/Users/alice/.ssh/config"},
			},
		},
		{
			name: "UNC share",
			options: []any{
				map[string]any{"label": "Safe label", "value": "//host/share"},
			},
		},
		{
			name: "windows drive absolute",
			options: []any{
				map[string]any{"label": "Safe label", "value": `C:\Users\alice\.ssh\config`},
			},
		},
		{
			name: "windows drive relative",
			options: []any{
				map[string]any{"label": "Safe label", "value": `C:Users\alice\.ssh\config`},
			},
		},
		{
			name: "custom scheme",
			options: []any{
				map[string]any{"label": "Safe label", "value": "vscode://file/C:/Users/alice/.ssh/config"},
			},
		},
		{
			name: "local http URL",
			options: []any{
				map[string]any{"label": "Safe label", "value": "http://localhost:8080/docs"},
			},
		},
		{
			name: "legacy hex loopback URL",
			options: []any{
				map[string]any{"label": "Safe label", "value": "http://0x7f.0.0.1/"},
			},
		},
		{
			name: "legacy integer loopback URL",
			options: []any{
				map[string]any{"label": "Safe label", "value": "http://2130706433/"},
			},
		},
		{
			name: "scheme-less local URL",
			options: []any{
				map[string]any{"label": "Safe label", "value": "localhost:8080/docs"},
			},
		},
		{
			name: "unix relative path",
			options: []any{
				map[string]any{"label": "Safe label", "value": "folder/secret"},
			},
		},
		{
			name: "windows relative path",
			options: []any{
				map[string]any{"label": "Safe label", "value": `folder\secret`},
			},
		},
		{
			name: "duplicate",
			options: []any{
				map[string]any{"label": "One", "value": "same"},
				map[string]any{"label": "Two", "id": "same"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := mustJSONRawMessage(t, map[string]any{
				"requestId": "tool-input-1",
				"questions": []map[string]any{{
					"id":       "choice",
					"question": "Pick one",
					"options":  tt.options,
				}},
			})

			result, err := Build(MethodToolUserInput, params, json.RawMessage(`"jsonrpc-1"`), BuildInput{
				TaskID:          "task-1",
				ThreadID:        "thread-1",
				TurnID:          "turn-1",
				CreatedAtUnixMS: 123,
				RedactString:    tt.redact,
			})
			if !errors.Is(err, ErrOverLimit) {
				t.Fatalf("Build() error = %v, want ErrOverLimit", err)
			}
			if result.LimitReason != LimitReasonControlsTooLarge ||
				result.RequestType != RequestTypeToolUserInput ||
				result.Record.Pending.Display != nil {
				t.Fatalf("Build() result = %#v, want controls-too-large tool input without display", result)
			}
		})
	}
}

func TestBuildToolUserInputRejectsInvalidPublicQuestionIDsBeforeDisplay(t *testing.T) {
	tests := []struct {
		name      string
		questions []map[string]any
	}{
		{
			name: "duplicate",
			questions: []map[string]any{
				{"id": "choice", "question": "Pick one"},
				{"id": "choice", "question": "Pick again"},
			},
		},
		{
			name:      "padded",
			questions: []map[string]any{{"id": " choice ", "question": "Pick one"}},
		},
		{
			name:      "over cap",
			questions: []map[string]any{{"id": strings.Repeat("q", domain.MaxPublicIDBytes+1), "question": "Pick one"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := mustJSONRawMessage(t, map[string]any{
				"requestId": "tool-input-1",
				"questions": tt.questions,
			})

			result, err := Build(MethodToolUserInput, params, json.RawMessage(`"jsonrpc-1"`), BuildInput{
				TaskID:          "task-1",
				ThreadID:        "thread-1",
				TurnID:          "turn-1",
				CreatedAtUnixMS: 123,
			})
			if !errors.Is(err, ErrOverLimit) {
				t.Fatalf("Build() error = %v, want ErrOverLimit", err)
			}
			if result.LimitReason != LimitReasonControlsTooLarge || result.Record.Pending.Display != nil {
				t.Fatalf("Build() result = %#v, want controls-too-large without display", result)
			}
		})
	}
}

func TestBuildPermissionsApprovalSanitizesPathAtomsAndRejectsUngrantable(t *testing.T) {
	cwd := t.TempDir()
	insideDir := filepath.Join(cwd, "src")
	if err := os.MkdirAll(insideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(t.TempDir(), "secret.txt")
	configDir := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", configDir)
	sanitizer, err := redact.NewPathSanitizer(cwd)
	if err != nil {
		t.Fatal(err)
	}
	sanitize := func(raw string) (string, bool) {
		label := sanitizer.SanitizeLabel(raw)
		return label, label != redact.PathMarker
	}
	params := mustJSONRawMessage(t, map[string]any{
		"permissions": map[string]any{
			"fileSystem": map[string]any{
				"entries": []any{
					map[string]any{
						"path":   map[string]any{"type": "path", "path": filepath.Join(insideDir, "file.txt")},
						"access": "read",
					},
					map[string]any{
						"path":   map[string]any{"type": "path", "path": outsidePath},
						"access": "write",
					},
					map[string]any{
						"path":   map[string]any{"type": "opaque", "value": "workspace-token"},
						"access": "write",
					},
					map[string]any{
						"path":   map[string]any{"type": "pattern", "pattern": "src/**/*.go"},
						"access": "read",
					},
					map[string]any{
						"path":   filepath.Join(configDir, "codex", "config.toml"),
						"access": "read",
					},
					map[string]any{
						"path":   map[string]any{"type": "path", "path": 12345},
						"access": "read",
					},
					map[string]any{
						"path":   map[string]any{"type": "path", "path": "~/.codex/config.toml"},
						"access": "read",
					},
					map[string]any{
						"path":   map[string]any{"type": "path", "path": "file:///tmp/secret"},
						"access": "read",
					},
					map[string]any{
						"path":   map[string]any{"type": "path", "path": "https://host/path"},
						"access": "read",
					},
					map[string]any{
						"path":   map[string]any{"type": "path", "path": "C:foo"},
						"access": "read",
					},
					map[string]any{
						"path":   map[string]any{"type": "path", "path": `\\server\share\secret.txt`},
						"access": "read",
					},
					map[string]any{
						"path":   map[string]any{"type": "path", "path": `\\?\C:\Users\me\secret.txt`},
						"access": "read",
					},
					map[string]any{
						"path":   map[string]any{"type": "path", "path": "src/file?token=abc"},
						"access": "read",
					},
					map[string]any{
						"path":   map[string]any{"type": "path", "path": "src/file#fragment"},
						"access": "read",
					},
					map[string]any{
						"path":   map[string]any{"type": "path", "path": "user:pass@host/path"},
						"access": "read",
					},
				},
			},
		},
	})

	result, err := Build(MethodPermissionsApproval, params, json.RawMessage(`"jsonrpc-1"`), BuildInput{
		TaskID:            "task-1",
		ThreadID:          "thread-1",
		TurnID:            "turn-1",
		CreatedAtUnixMS:   123,
		SanitizePathLabel: sanitize,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	display, ok := result.Record.Pending.Display.(domain.PermissionsApprovalDisplay)
	if !ok {
		t.Fatalf("display = %T, want PermissionsApprovalDisplay", result.Record.Pending.Display)
	}
	if len(display.RequestedPermissions) != 15 {
		t.Fatalf("permission atoms = %#v, want 15", display.RequestedPermissions)
	}
	if atom := display.RequestedPermissions[0]; !atom.Grantable || atom.DisplayLabel != "src/file.txt" {
		t.Fatalf("inside atom = %#v, want grantable src/file.txt", atom)
	}
	for _, atom := range display.RequestedPermissions[1:] {
		if atom.Grantable || atom.DisplayLabel != redact.PathMarker || atom.UngrantableReason == "" {
			t.Fatalf("unsafe atom = %#v, want non-grantable redacted path", atom)
		}
	}
	if len(result.Record.PermissionGrants) != 1 {
		t.Fatalf("permission grants = %#v, want only the inside path grant", result.Record.PermissionGrants)
	}
	for _, atom := range display.RequestedPermissions[1:] {
		if _, err := ValidateResponse(result.Record, domain.PendingResponse{
			Permissions: &domain.PermissionsPendingResponse{PermissionIDs: []string{atom.PermissionID}},
		}, redact.NewRegistry()); err == nil {
			t.Fatalf("ValidateResponse() accepted non-grantable permission id %q", atom.PermissionID)
		}
	}
	if _, err := ValidateResponse(result.Record, domain.PendingResponse{
		Permissions: &domain.PermissionsPendingResponse{PermissionIDs: []string{"permission-1"}},
	}, redact.NewRegistry()); err != nil {
		t.Fatalf("ValidateResponse() rejected grantable permission id: %v", err)
	}
}

func mustJSONRawMessage(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
