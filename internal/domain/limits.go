package domain

const (
	KiB = 1024
	MiB = 1024 * KiB

	MaxPromptBytes                  = 64 * KiB
	MaxContextBlocks                = 20
	MaxContextBlockContentBytes     = 256 * KiB
	MaxTotalContextBytes            = 1 * MiB
	MaxContextSourceLineBytes       = 32 * KiB
	MaxSourceLabelBytes             = 256
	MaxSourceURIBytes               = 2048
	MaxMimeTypeBytes                = 128
	MaxUICorrelationMetadataEntries = 20
	MaxUICorrelationMetadataKey     = 64
	MaxUICorrelationMetadataValue   = 512
	MaxPublicIDBytes                = 128
	MaxPendingResponseClientIDBytes = MaxPublicIDBytes

	MaxPlanSteps                       = 100
	MaxApprovalDecisionOptions         = 16
	MaxActivePendingRequests           = 64
	MaxPermissionAtoms                 = 128
	MaxApprovalSecurityMetadataEntries = 64
	MaxToolUserInputQuestions          = 20
	MaxToolUserInputOptionsPerQuestion = 20
	MaxToolUserInputAnswersPerQuestion = 10
	MaxToolUserInputAnswerValueBytes   = 8 * KiB
	MaxToolUserInputTotalAnswerBytes   = 64 * KiB
	MaxMcpElicitationContentJSONBytes  = 128 * KiB
	MaxMcpElicitationContentJSONDepth  = 16

	MaxOutboundUnknownRawJSONBytes        = 16 * KiB
	MaxOutboundCommandDisplayBytes        = 8 * KiB
	MaxOutboundCommandOutputDeltaBytes    = 32 * KiB
	MaxOutboundAssistantTextBytes         = MaxOutboundCommandOutputDeltaBytes
	MaxOutboundDiffDisplayBytes           = 64 * KiB
	MaxOutboundErrorDisplayMessageBytes   = 2 * KiB
	MaxOutboundMcpFormSchemaBytes         = 32 * KiB
	MaxOutboundPendingDisplayPayloadBytes = 64 * KiB
	MaxOutboundPendingDisplayStringBytes  = MaxOutboundPendingDisplayPayloadBytes
	MaxStreamRedactionCarryBytes          = 2 * KiB
)
