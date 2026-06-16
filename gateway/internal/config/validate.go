package config

import (
	"fmt"
	"strings"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

func (c *Config) Validate() (*ValidatedConfig, error) {
	if c == nil {
		return nil, fmt.Errorf("config is nil")
	}

	listen := c.Listen
	if listen == "" {
		listen = DefaultListenAddress
	}
	if err := ValidateListenAddress(listen); err != nil {
		return nil, err
	}
	chatRuntime := c.ChatRuntime
	applyChatRuntimeDefaults(&chatRuntime)

	codexBinary, err := resolveExecutable(c.CodexBinary, "codex_binary")
	if err != nil {
		return nil, err
	}

	clientAuthToken, err := validateAndLoadToken(c.ClientAuthTokenSource)
	if err != nil {
		return nil, err
	}

	secretEnvSources := map[string]struct{}{}
	if c.ClientAuthTokenSource.Env != "" {
		secretEnvSources[envNameKey(c.ClientAuthTokenSource.Env)] = struct{}{}
	}

	providers, providerIDs, err := validateCredentialProviders(c.CredentialProviders, secretEnvSources)
	if err != nil {
		return nil, err
	}

	if err := validateForbiddenEnvSourcesDoNotReachChild(secretEnvSources); err != nil {
		return nil, err
	}
	childEnvPolicy, err := newChildEnvPolicy(c.ChildEnvAllowlist, secretEnvSources)
	if err != nil {
		return nil, err
	}

	groups, groupsByID, err := validateSessionGroups(c.SessionGroups, providerIDs)
	if err != nil {
		return nil, err
	}

	return &ValidatedConfig{
		CodexBinary:              codexBinary,
		Listen:                   listen,
		StrictSchemaVerification: c.StrictSchemaVerification,
		ChatRuntime:              chatRuntime,
		ChildEnvAllowlist:        append([]string(nil), childEnvPolicy.allowlist...),
		CredentialProviders:      providers,
		SessionGroups:            groups,
		sessionGroupsByID:        groupsByID,
		clientAuthToken:          clientAuthToken,
		childEnvPolicy:           childEnvPolicy,
	}, nil
}

func validateCredentialProviders(providers []CredentialProvider, secretEnvSources map[string]struct{}) ([]CredentialProvider, map[string]struct{}, error) {
	ids := map[string]struct{}{}
	validated := make([]CredentialProvider, 0, len(providers))
	for index, provider := range providers {
		if provider.ProviderID == "" {
			return nil, nil, fmt.Errorf("credential_providers[%d].provider_id is required", index)
		}
		if _, ok := ids[provider.ProviderID]; ok {
			return nil, nil, fmt.Errorf("duplicate credential provider id %q", provider.ProviderID)
		}
		ids[provider.ProviderID] = struct{}{}

		resolvedExecutable, err := resolveAbsoluteExecutable(provider.Executable, "credential provider executable")
		if err != nil {
			return nil, nil, fmt.Errorf("credential provider %q: %w", provider.ProviderID, err)
		}
		provider.CanonicalExecutable = resolvedExecutable

		if provider.Workdir != "" {
			canonicalWorkdir, err := canonicalizeExistingDir(provider.Workdir, "credential provider workdir")
			if err != nil {
				return nil, nil, fmt.Errorf("credential provider %q: %w", provider.ProviderID, err)
			}
			provider.CanonicalWorkdir = canonicalWorkdir.path
		}

		for _, name := range provider.EnvSources {
			if err := validateEnvName(name); err != nil {
				return nil, nil, fmt.Errorf("credential provider %q env source: %w", provider.ProviderID, err)
			}
			secretEnvSources[envNameKey(name)] = struct{}{}
		}

		applyProviderDefaults(&provider)
		if provider.TimeoutMillis <= 0 || provider.TimeoutMillis > hardCapCredentialProviderTimeoutMillis {
			return nil, nil, fmt.Errorf("credential provider %q timeout_millis is out of bounds", provider.ProviderID)
		}
		if provider.StdoutBytes <= 0 || provider.StdoutBytes > hardCapCredentialProviderStdoutBytes {
			return nil, nil, fmt.Errorf("credential provider %q stdout_bytes is out of bounds", provider.ProviderID)
		}
		if provider.StderrBytes <= 0 || provider.StderrBytes > hardCapCredentialProviderStderrBytes {
			return nil, nil, fmt.Errorf("credential provider %q stderr_bytes is out of bounds", provider.ProviderID)
		}

		validated = append(validated, provider)
	}
	return validated, ids, nil
}

func validateSessionGroups(groups []SessionGroup, providerIDs map[string]struct{}) ([]SessionGroup, map[string]int, error) {
	if len(groups) == 0 {
		return nil, nil, fmt.Errorf("at least one session group is required")
	}

	ids := map[string]int{}
	codexHomes := []canonicalPath{}
	validated := make([]SessionGroup, 0, len(groups))
	for index, group := range groups {
		if err := validateConfigPublicID(group.SessionGroupID, fmt.Sprintf("session_groups[%d].session_group_id", index)); err != nil {
			return nil, nil, err
		}
		if _, ok := ids[group.SessionGroupID]; ok {
			return nil, nil, fmt.Errorf("duplicate session group id %q", group.SessionGroupID)
		}
		ids[group.SessionGroupID] = index
		if err := validateConfigPublicID(group.WorkspaceID, fmt.Sprintf("session group %q workspace_id", group.SessionGroupID)); err != nil {
			return nil, nil, err
		}

		if len(group.forbiddenOverrides) > 0 {
			return nil, nil, fmt.Errorf("session group %q has profile-like override %q, which is out of MVP", group.SessionGroupID, group.forbiddenOverrides[0])
		}

		cwd, err := canonicalizeExistingDir(group.CWD, "cwd")
		if err != nil {
			return nil, nil, fmt.Errorf("session group %q: %w", group.SessionGroupID, err)
		}
		codexHome, err := canonicalizeExistingDir(group.CodexHome, "codex_home")
		if err != nil {
			return nil, nil, fmt.Errorf("session group %q: %w", group.SessionGroupID, err)
		}
		for _, existing := range codexHomes {
			if sameCanonicalPath(existing, codexHome) {
				return nil, nil, fmt.Errorf("duplicate canonical codex_home identity for session group %q", group.SessionGroupID)
			}
		}
		codexHomes = append(codexHomes, codexHome)
		group.CanonicalCWD = cwd.path
		group.CanonicalCodexHome = codexHome.path

		if group.CredentialProviderID != "" {
			if _, ok := providerIDs[group.CredentialProviderID]; !ok {
				return nil, nil, fmt.Errorf("session group %q references unknown credential provider %q", group.SessionGroupID, group.CredentialProviderID)
			}
		}

		if err := validateRuntimePolicy(&group.RuntimePolicy); err != nil {
			return nil, nil, fmt.Errorf("session group %q: %w", group.SessionGroupID, err)
		}
		if err := validateLimits(&group); err != nil {
			return nil, nil, fmt.Errorf("session group %q: %w", group.SessionGroupID, err)
		}

		validated = append(validated, group)
	}
	return validated, ids, nil
}

func validateConfigPublicID(id string, field string) error {
	if len(id) > domain.MaxPublicIDBytes {
		return fmt.Errorf("%s exceeds public id byte cap", field)
	}
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(trimmedID) > domain.MaxPublicIDBytes {
		return fmt.Errorf("%s exceeds public id byte cap", field)
	}
	if trimmedID != id {
		return fmt.Errorf("%s must not have leading or trailing whitespace", field)
	}
	return nil
}

func validateRuntimePolicy(policy *RuntimePolicy) error {
	if len(policy.forbiddenOverrides) > 0 {
		return fmt.Errorf("runtime policy override %q is out of MVP", policy.forbiddenOverrides[0])
	}

	switch policy.ApprovalPolicy {
	case ApprovalPolicyUntrusted, ApprovalPolicyOnFailure, ApprovalPolicyOnRequest:
	case "never":
		return fmt.Errorf("approval_policy=never is out of MVP")
	case "":
		return fmt.Errorf("approval_policy is required")
	default:
		return fmt.Errorf("approval_policy %q is unsupported", policy.ApprovalPolicy)
	}

	if policy.ApprovalsReviewer == "" {
		policy.ApprovalsReviewer = ApprovalsReviewerUser
	}
	if policy.ApprovalsReviewer != ApprovalsReviewerUser {
		return fmt.Errorf("approvals_reviewer must be user")
	}
	if policy.PermissionsProfileID != "" && strings.TrimSpace(policy.PermissionsProfileID) == "" {
		return fmt.Errorf("permissions_profile_id must not be blank")
	}

	hasSandbox := policy.SandboxMode != ""
	hasPermissions := policy.PermissionsProfileID != ""
	if hasSandbox == hasPermissions {
		return fmt.Errorf("exactly one of sandbox_mode or permissions_profile_id is required")
	}
	if hasSandbox {
		switch policy.SandboxMode {
		case SandboxReadOnly, SandboxWorkspaceWrite:
			if policy.DangerFullAccessReason != "" {
				return fmt.Errorf("danger_full_access_reason is only valid with danger-full-access")
			}
		case SandboxDangerFullAccess:
			if strings.TrimSpace(policy.DangerFullAccessReason) == "" {
				return fmt.Errorf("danger-full-access requires danger_full_access_reason")
			}
		default:
			return fmt.Errorf("sandbox_mode %q is unsupported", policy.SandboxMode)
		}
	}
	if hasPermissions && policy.DangerFullAccessReason != "" {
		return fmt.Errorf("danger_full_access_reason is only valid with danger-full-access")
	}
	return nil
}

func validateLimits(group *SessionGroup) error {
	applyReplayDefaults(&group.ReplayLimits)
	if group.ReplayLimits.MaxEvents <= 0 || group.ReplayLimits.MaxEvents > hardCapEventLogEvents {
		return fmt.Errorf("replay max_events is out of bounds")
	}
	if group.ReplayLimits.MaxBytes <= 0 || group.ReplayLimits.MaxBytes > hardCapEventLogBytes {
		return fmt.Errorf("replay max_bytes is out of bounds")
	}
	if group.ReplayLimits.TTLMillis <= 0 || group.ReplayLimits.TTLMillis > hardCapReplayTTLMillis {
		return fmt.Errorf("replay ttl_millis is out of bounds")
	}

	applyThreadBindingDefaults(&group.ThreadBindingLimits)
	if group.ThreadBindingLimits.MaxBindings <= 0 || group.ThreadBindingLimits.MaxBindings > hardCapThreadBindings {
		return fmt.Errorf("thread_binding max_bindings is out of bounds")
	}
	if group.ThreadBindingLimits.TTLMillis <= 0 || group.ThreadBindingLimits.TTLMillis > hardCapThreadBindingTTLMillis {
		return fmt.Errorf("thread_binding ttl_millis is out of bounds")
	}

	applyPendingDefaults(&group.PendingLimits)
	if group.PendingLimits.MaxActiveRequests <= 0 || group.PendingLimits.MaxActiveRequests > hardCapActivePendingRequests {
		return fmt.Errorf("pending max_active_requests is out of bounds")
	}
	if group.PendingLimits.MaxDisplayPayloadBytes <= 0 || group.PendingLimits.MaxDisplayPayloadBytes > hardCapPendingDisplayPayloadBytes {
		return fmt.Errorf("pending max_display_payload_bytes is out of bounds")
	}
	if group.PendingLimits.StatusNonPendingBudgetBytes <= 0 || group.PendingLimits.StatusNonPendingBudgetBytes > hardCapStatusNonPendingBudget {
		return fmt.Errorf("pending status_non_pending_budget_bytes is out of bounds")
	}

	applyGRPCDefaults(&group.GRPCLimits)
	if group.GRPCLimits.InboundMessageBytes <= 0 || group.GRPCLimits.InboundMessageBytes > hardCapGRPCMessageBytes {
		return fmt.Errorf("grpc inbound_message_bytes is out of bounds")
	}
	if group.GRPCLimits.OutboundMessageBytes <= 0 || group.GRPCLimits.OutboundMessageBytes > hardCapGRPCMessageBytes {
		return fmt.Errorf("grpc outbound_message_bytes is out of bounds")
	}

	pendingBudget := int64(group.PendingLimits.MaxActiveRequests)*group.PendingLimits.MaxDisplayPayloadBytes + group.PendingLimits.StatusNonPendingBudgetBytes
	if pendingBudget > group.GRPCLimits.OutboundMessageBytes {
		return fmt.Errorf("pending limits exceed status-safe grpc outbound budget")
	}
	return nil
}
