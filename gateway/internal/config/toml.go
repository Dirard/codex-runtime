package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type tomlContext int

const (
	tomlRoot tomlContext = iota
	tomlChatRuntime
	tomlTokenSource
	tomlCredentialProvider
	tomlSessionGroup
	tomlRuntimePolicy
	tomlReplayLimits
	tomlThreadBindingLimits
	tomlPendingLimits
	tomlGRPCLimits
)

// ParseTOML accepts the gateway config's deliberately strict TOML subset:
// table headers, array-table headers, key/value assignments, double-quoted
// basic strings with TOML escapes, decimal integers, booleans for rejected
// override syntax checks, and JSON-compatible string arrays. Other TOML
// features are rejected so security config cannot rely on ambiguous parsing.
func ParseTOML(data []byte) (*Config, error) {
	config := &Config{}
	context := tomlRoot
	var provider *CredentialProvider
	var session *SessionGroup
	providerIndex := -1
	sessionIndex := -1

	rootSeen := map[string]struct{}{}
	chatRuntimeSeen := map[string]struct{}{}
	tokenSeen := map[string]struct{}{}
	providerSeen := map[int]map[string]struct{}{}
	sessionSeen := map[int]map[tomlContext]map[string]struct{}{}
	currentSeen := rootSeen

	lines := strings.Split(string(data), "\n")
	for index, rawLine := range lines {
		lineNumber := index + 1
		line := strings.TrimSpace(stripTOMLComment(rawLine))
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[[") {
			if !strings.HasSuffix(line, "]]") {
				return nil, fmt.Errorf("line %d: malformed array table", lineNumber)
			}
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "[["), "]]"))
			switch name {
			case "credential_providers":
				config.CredentialProviders = append(config.CredentialProviders, CredentialProvider{})
				providerIndex = len(config.CredentialProviders) - 1
				provider = &config.CredentialProviders[providerIndex]
				providerSeen[providerIndex] = map[string]struct{}{}
				currentSeen = providerSeen[providerIndex]
				context = tomlCredentialProvider
			case "session_groups":
				config.SessionGroups = append(config.SessionGroups, SessionGroup{})
				sessionIndex = len(config.SessionGroups) - 1
				session = &config.SessionGroups[sessionIndex]
				sessionSeen[sessionIndex] = map[tomlContext]map[string]struct{}{
					tomlSessionGroup: {},
				}
				currentSeen = sessionSeen[sessionIndex][tomlSessionGroup]
				context = tomlSessionGroup
			default:
				return nil, fmt.Errorf("line %d: unknown array table %q", lineNumber, name)
			}
			continue
		}

		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return nil, fmt.Errorf("line %d: malformed table", lineNumber)
			}
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			switch name {
			case "chat_runtime":
				currentSeen = chatRuntimeSeen
				context = tomlChatRuntime
			case "client_auth_token_source":
				currentSeen = tokenSeen
				context = tomlTokenSource
			case "session_groups.runtime_policy":
				if session == nil {
					return nil, fmt.Errorf("line %d: session_groups.runtime_policy before session_groups", lineNumber)
				}
				currentSeen = seenKeysForSessionContext(sessionSeen, sessionIndex, tomlRuntimePolicy)
				context = tomlRuntimePolicy
			case "session_groups.replay_limits":
				if session == nil {
					return nil, fmt.Errorf("line %d: session_groups.replay_limits before session_groups", lineNumber)
				}
				currentSeen = seenKeysForSessionContext(sessionSeen, sessionIndex, tomlReplayLimits)
				context = tomlReplayLimits
			case "session_groups.thread_binding_limits":
				if session == nil {
					return nil, fmt.Errorf("line %d: session_groups.thread_binding_limits before session_groups", lineNumber)
				}
				currentSeen = seenKeysForSessionContext(sessionSeen, sessionIndex, tomlThreadBindingLimits)
				context = tomlThreadBindingLimits
			case "session_groups.pending_limits":
				if session == nil {
					return nil, fmt.Errorf("line %d: session_groups.pending_limits before session_groups", lineNumber)
				}
				currentSeen = seenKeysForSessionContext(sessionSeen, sessionIndex, tomlPendingLimits)
				context = tomlPendingLimits
			case "session_groups.grpc_limits":
				if session == nil {
					return nil, fmt.Errorf("line %d: session_groups.grpc_limits before session_groups", lineNumber)
				}
				currentSeen = seenKeysForSessionContext(sessionSeen, sessionIndex, tomlGRPCLimits)
				context = tomlGRPCLimits
			default:
				return nil, fmt.Errorf("line %d: unknown table %q", lineNumber, name)
			}
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected key = value", lineNumber)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return nil, fmt.Errorf("line %d: empty key or value", lineNumber)
		}
		if err := rejectDuplicateTOMLKey(currentSeen, key); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}

		if err := assignTOMLValue(config, provider, session, context, key, value); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
	}

	return config, nil
}

func seenKeysForSessionContext(sessionSeen map[int]map[tomlContext]map[string]struct{}, sessionIndex int, context tomlContext) map[string]struct{} {
	tables := sessionSeen[sessionIndex]
	if tables[context] == nil {
		tables[context] = map[string]struct{}{}
	}
	return tables[context]
}

func rejectDuplicateTOMLKey(seen map[string]struct{}, key string) error {
	if _, ok := seen[key]; ok {
		return fmt.Errorf("duplicate key %q", key)
	}
	seen[key] = struct{}{}
	return nil
}

func assignTOMLValue(config *Config, provider *CredentialProvider, session *SessionGroup, context tomlContext, key string, value string) error {
	switch context {
	case tomlRoot:
		return assignRoot(config, key, value)
	case tomlChatRuntime:
		return assignChatRuntime(&config.ChatRuntime, key, value)
	case tomlTokenSource:
		return assignTokenSource(&config.ClientAuthTokenSource, key, value)
	case tomlCredentialProvider:
		if provider == nil {
			return fmt.Errorf("credential provider key before provider table")
		}
		return assignCredentialProvider(provider, key, value)
	case tomlSessionGroup:
		if session == nil {
			return fmt.Errorf("session group key before session group table")
		}
		return assignSessionGroup(session, key, value)
	case tomlRuntimePolicy:
		return assignRuntimePolicy(&session.RuntimePolicy, key, value)
	case tomlReplayLimits:
		return assignReplayLimits(&session.ReplayLimits, key, value)
	case tomlThreadBindingLimits:
		return assignThreadBindingLimits(&session.ThreadBindingLimits, key, value)
	case tomlPendingLimits:
		return assignPendingLimits(&session.PendingLimits, key, value)
	case tomlGRPCLimits:
		return assignGRPCLimits(&session.GRPCLimits, key, value)
	default:
		return fmt.Errorf("unknown parser context")
	}
}

func assignRoot(config *Config, key string, value string) error {
	switch key {
	case "codex_binary":
		return parseStringInto(value, &config.CodexBinary)
	case "listen":
		return parseStringInto(value, &config.Listen)
	case "strict_schema_verification":
		return parseBoolInto(value, &config.StrictSchemaVerification)
	case "child_env_allowlist":
		return parseStringArrayInto(value, &config.ChildEnvAllowlist)
	default:
		return fmt.Errorf("unknown root key %q", key)
	}
}

func assignChatRuntime(runtime *ChatRuntimeConfig, key string, value string) error {
	switch key {
	case "enabled":
		runtime.enabledSet = true
		return parseBoolInto(value, &runtime.Enabled)
	default:
		return fmt.Errorf("unknown chat_runtime key %q", key)
	}
}

func assignTokenSource(source *TokenSource, key string, value string) error {
	switch key {
	case "env":
		return parseStringInto(value, &source.Env)
	case "file":
		return parseStringInto(value, &source.File)
	default:
		return fmt.Errorf("unknown client_auth_token_source key %q", key)
	}
}

func assignCredentialProvider(provider *CredentialProvider, key string, value string) error {
	switch key {
	case "provider_id":
		return parseStringInto(value, &provider.ProviderID)
	case "executable":
		return parseStringInto(value, &provider.Executable)
	case "args":
		return parseStringArrayInto(value, &provider.Args)
	case "workdir":
		return parseStringInto(value, &provider.Workdir)
	case "env_sources":
		return parseStringArrayInto(value, &provider.EnvSources)
	case "timeout_millis":
		provider.timeoutSet = true
		return parseInt64Into(value, &provider.TimeoutMillis)
	case "stdout_bytes":
		provider.stdoutSet = true
		return parseInt64Into(value, &provider.StdoutBytes)
	case "stderr_bytes":
		provider.stderrSet = true
		return parseInt64Into(value, &provider.StderrBytes)
	default:
		return fmt.Errorf("unknown credential_providers key %q", key)
	}
}

func assignSessionGroup(session *SessionGroup, key string, value string) error {
	switch key {
	case "session_group_id":
		return parseStringInto(value, &session.SessionGroupID)
	case "workspace_id":
		return parseStringInto(value, &session.WorkspaceID)
	case "cwd":
		return parseStringInto(value, &session.CWD)
	case "codex_home":
		return parseStringInto(value, &session.CodexHome)
	case "credential_provider_id":
		return parseStringInto(value, &session.CredentialProviderID)
	case "codex_profile", "profile":
		session.forbiddenOverrides = append(session.forbiddenOverrides, key)
		var ignored string
		return parseStringInto(value, &ignored)
	default:
		return fmt.Errorf("unknown session_groups key %q", key)
	}
}

func assignRuntimePolicy(policy *RuntimePolicy, key string, value string) error {
	switch key {
	case "approval_policy":
		return parseStringInto(value, &policy.ApprovalPolicy)
	case "approvals_reviewer":
		return parseStringInto(value, &policy.ApprovalsReviewer)
	case "sandbox_mode":
		return parseStringInto(value, &policy.SandboxMode)
	case "permissions_profile_id":
		return parseStringInto(value, &policy.PermissionsProfileID)
	case "danger_full_access_reason":
		return parseStringInto(value, &policy.DangerFullAccessReason)
	case "granular_approval_policy", "profile", "codex_profile", "runtime_workspace_roots", "environments", "model", "provider", "service_tier", "reasoning", "base_instructions", "developer_instructions", "instructions", "personality", "dynamic_tools", "config", "raw_config", "experimental_raw_events", "sandbox_policy", "output_schema":
		policy.forbiddenOverrides = append(policy.forbiddenOverrides, key)
		return validateForbiddenValueSyntax(key, value)
	default:
		return fmt.Errorf("unknown runtime_policy key %q", key)
	}
}

func assignReplayLimits(limits *ReplayLimits, key string, value string) error {
	switch key {
	case "max_events":
		limits.maxEventsSet = true
		return parseIntInto(value, &limits.MaxEvents)
	case "max_bytes":
		limits.maxBytesSet = true
		return parseInt64Into(value, &limits.MaxBytes)
	case "ttl_millis":
		limits.ttlSet = true
		return parseInt64Into(value, &limits.TTLMillis)
	default:
		return fmt.Errorf("unknown replay_limits key %q", key)
	}
}

func assignThreadBindingLimits(limits *ThreadBindingLimits, key string, value string) error {
	switch key {
	case "max_bindings":
		limits.maxBindingsSet = true
		return parseIntInto(value, &limits.MaxBindings)
	case "ttl_millis":
		limits.ttlSet = true
		return parseInt64Into(value, &limits.TTLMillis)
	default:
		return fmt.Errorf("unknown thread_binding_limits key %q", key)
	}
}

func assignPendingLimits(limits *PendingLimits, key string, value string) error {
	switch key {
	case "max_active_requests":
		limits.maxActiveRequestsSet = true
		return parseIntInto(value, &limits.MaxActiveRequests)
	case "max_display_payload_bytes":
		limits.maxDisplayPayloadBytesSet = true
		return parseInt64Into(value, &limits.MaxDisplayPayloadBytes)
	case "status_non_pending_budget_bytes":
		limits.statusNonPendingBudgetBytesSet = true
		return parseInt64Into(value, &limits.StatusNonPendingBudgetBytes)
	default:
		return fmt.Errorf("unknown pending_limits key %q", key)
	}
}

func assignGRPCLimits(limits *GRPCLimits, key string, value string) error {
	switch key {
	case "inbound_message_bytes":
		limits.inboundSet = true
		return parseInt64Into(value, &limits.InboundMessageBytes)
	case "outbound_message_bytes":
		limits.outboundSet = true
		return parseInt64Into(value, &limits.OutboundMessageBytes)
	default:
		return fmt.Errorf("unknown grpc_limits key %q", key)
	}
}

func validateForbiddenValueSyntax(key string, value string) error {
	switch key {
	case "runtime_workspace_roots", "environments", "dynamic_tools":
		var ignored []string
		return parseStringArrayInto(value, &ignored)
	case "experimental_raw_events":
		_, err := strconv.ParseBool(value)
		return err
	default:
		var ignored string
		return parseStringInto(value, &ignored)
	}
}

func parseStringInto(value string, out *string) error {
	if !strings.HasPrefix(value, `"`) || !strings.HasSuffix(value, `"`) {
		return fmt.Errorf("expected double-quoted basic string")
	}
	if err := validateBasicStringEscapes(value); err != nil {
		return err
	}
	parsed, err := strconv.Unquote(value)
	if err != nil {
		return fmt.Errorf("expected quoted string: %w", err)
	}
	*out = parsed
	return nil
}

func validateBasicStringEscapes(value string) error {
	for index := 1; index < len(value)-1; index++ {
		if value[index] != '\\' {
			continue
		}
		index++
		if index >= len(value)-1 {
			return fmt.Errorf("unterminated string escape")
		}

		switch value[index] {
		case 'b', 't', 'n', 'f', 'r', '"', '\\':
		case 'u':
			if err := validateHexEscape(value, index+1, 4); err != nil {
				return err
			}
			index += 4
		case 'U':
			if err := validateHexEscape(value, index+1, 8); err != nil {
				return err
			}
			index += 8
		default:
			return fmt.Errorf("unsupported TOML string escape \\%c", value[index])
		}
	}
	return nil
}

func validateHexEscape(value string, start int, count int) error {
	if start+count > len(value)-1 {
		return fmt.Errorf("short unicode escape")
	}
	for index := start; index < start+count; index++ {
		if !isHexDigit(value[index]) {
			return fmt.Errorf("invalid unicode escape")
		}
	}
	return nil
}

func isHexDigit(char byte) bool {
	return char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F'
}

func parseStringArrayInto(value string, out *[]string) error {
	var parsed []string
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return fmt.Errorf("expected string array: %w", err)
	}
	if parsed == nil {
		return fmt.Errorf("expected string array")
	}
	*out = parsed
	return nil
}

func parseBoolInto(value string, out *bool) error {
	switch value {
	case "true":
		*out = true
	case "false":
		*out = false
	default:
		return fmt.Errorf("expected boolean")
	}
	return nil
}

func parseIntInto(value string, out *int) error {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("expected integer: %w", err)
	}
	*out = parsed
	return nil
}

func parseInt64Into(value string, out *int64) error {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("expected integer: %w", err)
	}
	*out = parsed
	return nil
}

func stripTOMLComment(line string) string {
	inString := false
	escaped := false
	for index, char := range line {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == '"' {
				inString = false
			}
			continue
		}
		if char == '"' {
			inString = true
			continue
		}
		if char == '#' {
			return line[:index]
		}
	}
	return line
}
