package config

const (
	kib = 1024
	mib = 1024 * kib

	defaultGRPCInboundMessageBytes  = 4 * mib
	defaultGRPCOutboundMessageBytes = 4 * mib
	MaxGRPCMessageBytes             = 8 * mib
	hardCapGRPCMessageBytes         = MaxGRPCMessageBytes

	defaultEventLogEvents = 2_000
	hardCapEventLogEvents = 5_000
	defaultEventLogBytes  = 8 * mib
	hardCapEventLogBytes  = 32 * mib

	defaultReplayTTLMillis = 30 * 60 * 1000
	hardCapReplayTTLMillis = 2 * 60 * 60 * 1000

	defaultThreadBindings         = 1_000
	hardCapThreadBindings         = 10_000
	defaultThreadBindingTTLMillis = 24 * 60 * 60 * 1000
	hardCapThreadBindingTTLMillis = 7 * 24 * 60 * 60 * 1000

	defaultActivePendingRequests      = 32
	hardCapActivePendingRequests      = 64
	defaultPendingDisplayPayloadBytes = 32 * kib
	hardCapPendingDisplayPayloadBytes = 64 * kib
	defaultStatusNonPendingBudget     = 64 * kib
	hardCapStatusNonPendingBudget     = 256 * kib

	defaultCredentialProviderTimeoutMillis = 10 * 1000
	hardCapCredentialProviderTimeoutMillis = 30 * 1000
	defaultCredentialProviderStdoutBytes   = 16 * kib
	hardCapCredentialProviderStdoutBytes   = 64 * kib
	defaultCredentialProviderStderrBytes   = 8 * kib
	hardCapCredentialProviderStderrBytes   = 32 * kib
)

func applyReplayDefaults(limits *ReplayLimits) {
	if !limits.maxEventsSet {
		limits.MaxEvents = defaultEventLogEvents
	}
	if !limits.maxBytesSet {
		limits.MaxBytes = defaultEventLogBytes
	}
	if !limits.ttlSet {
		limits.TTLMillis = defaultReplayTTLMillis
	}
}

func applyThreadBindingDefaults(limits *ThreadBindingLimits) {
	if !limits.maxBindingsSet {
		limits.MaxBindings = defaultThreadBindings
	}
	if !limits.ttlSet {
		limits.TTLMillis = defaultThreadBindingTTLMillis
	}
}

func applyPendingDefaults(limits *PendingLimits) {
	if !limits.maxActiveRequestsSet {
		limits.MaxActiveRequests = defaultActivePendingRequests
	}
	if !limits.maxDisplayPayloadBytesSet {
		limits.MaxDisplayPayloadBytes = defaultPendingDisplayPayloadBytes
	}
	if !limits.statusNonPendingBudgetBytesSet {
		limits.StatusNonPendingBudgetBytes = defaultStatusNonPendingBudget
	}
}

func applyGRPCDefaults(limits *GRPCLimits) {
	if !limits.inboundSet {
		limits.InboundMessageBytes = defaultGRPCInboundMessageBytes
	}
	if !limits.outboundSet {
		limits.OutboundMessageBytes = defaultGRPCOutboundMessageBytes
	}
}

func applyProviderDefaults(provider *CredentialProvider) {
	if !provider.timeoutSet {
		provider.TimeoutMillis = defaultCredentialProviderTimeoutMillis
	}
	if !provider.stdoutSet {
		provider.StdoutBytes = defaultCredentialProviderStdoutBytes
	}
	if !provider.stderrSet {
		provider.StderrBytes = defaultCredentialProviderStderrBytes
	}
}
