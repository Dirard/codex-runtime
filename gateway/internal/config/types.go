package config

const (
	DefaultListenAddress = "127.0.0.1:0"

	ApprovalPolicyUntrusted = "untrusted"
	ApprovalPolicyOnFailure = "on-failure"
	ApprovalPolicyOnRequest = "on-request"

	ApprovalsReviewerUser = "user"

	SandboxReadOnly         = "read-only"
	SandboxWorkspaceWrite   = "workspace-write"
	SandboxDangerFullAccess = "danger-full-access"
)

type Config struct {
	CodexBinary              string
	Listen                   string
	StrictSchemaVerification bool
	ChatRuntime              ChatRuntimeConfig
	ClientAuthTokenSource    TokenSource
	ChildEnvAllowlist        []string
	CredentialProviders      []CredentialProvider
	SessionGroups            []SessionGroup
}

type ChatRuntimeConfig struct {
	Enabled bool

	enabledSet bool
}

type TokenSource struct {
	Env  string
	File string
}

type CredentialProvider struct {
	ProviderID    string
	Executable    string
	Args          []string
	Workdir       string
	EnvSources    []string
	TimeoutMillis int64
	StdoutBytes   int64
	StderrBytes   int64

	CanonicalExecutable string
	CanonicalWorkdir    string

	timeoutSet bool
	stdoutSet  bool
	stderrSet  bool
}

type SessionGroup struct {
	SessionGroupID       string
	WorkspaceID          string
	CWD                  string
	CodexHome            string
	CredentialProviderID string

	RuntimePolicy       RuntimePolicy
	ReplayLimits        ReplayLimits
	ThreadBindingLimits ThreadBindingLimits
	PendingLimits       PendingLimits
	GRPCLimits          GRPCLimits

	CanonicalCWD       string
	CanonicalCodexHome string

	forbiddenOverrides []string
}

type RuntimePolicy struct {
	ApprovalPolicy         string
	ApprovalsReviewer      string
	SandboxMode            string
	PermissionsProfileID   string
	DangerFullAccessReason string

	forbiddenOverrides []string
}

type ReplayLimits struct {
	MaxEvents int
	MaxBytes  int64
	TTLMillis int64

	maxEventsSet bool
	maxBytesSet  bool
	ttlSet       bool
}

type ThreadBindingLimits struct {
	MaxBindings int
	TTLMillis   int64

	maxBindingsSet bool
	ttlSet         bool
}

type PendingLimits struct {
	MaxActiveRequests           int
	MaxDisplayPayloadBytes      int64
	StatusNonPendingBudgetBytes int64

	maxActiveRequestsSet           bool
	maxDisplayPayloadBytesSet      bool
	statusNonPendingBudgetBytesSet bool
}

type GRPCLimits struct {
	InboundMessageBytes  int64
	OutboundMessageBytes int64

	inboundSet  bool
	outboundSet bool
}

type ValidatedConfig struct {
	CodexBinary              string
	Listen                   string
	StrictSchemaVerification bool
	ChatRuntime              ChatRuntimeConfig
	ChildEnvAllowlist        []string
	CredentialProviders      []CredentialProvider
	SessionGroups            []SessionGroup
	sessionGroupsByID        map[string]int
	clientAuthToken          string
	childEnvPolicy           ChildEnvPolicy
}

func (c *ValidatedConfig) SessionGroup(id string) (SessionGroup, bool) {
	if c == nil {
		return SessionGroup{}, false
	}
	index, ok := c.sessionGroupsByID[id]
	if !ok {
		return SessionGroup{}, false
	}
	return c.SessionGroups[index], true
}

func (c *ValidatedConfig) ClientAuthTokenForAuth() string {
	if c == nil {
		return ""
	}
	return c.clientAuthToken
}

func (c *ValidatedConfig) StrictSchemaVerificationEnabled() bool {
	if c == nil {
		return false
	}
	return c.StrictSchemaVerification
}

func (c *ValidatedConfig) ChatRuntimeEnabled() bool {
	if c == nil {
		return false
	}
	return c.ChatRuntime.Enabled
}
