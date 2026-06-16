package grpcapi

import (
	"strings"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

const overCapErrorDisplayMessage = "error display message exceeds outbound cap"
const internalGatewayErrorDisplayMessage = "internal_gateway_error"

func replayNoticeMappingFailure(notice domain.ReplayNotice) *MappingFailure {
	if _, ok := ReplayNoticeCodeToProto(notice.Code); !ok {
		return invalidMappingFailure("replay notice has invalid code")
	}
	return outboundStringMappingFailure(outboundStringCheck{
		field:    "replay notice message",
		value:    notice.Message,
		capBytes: domain.MaxOutboundErrorDisplayMessageBytes,
	})
}

func gatewayErrorDetailsMappingFailure(details domain.GatewayErrorDetails) *MappingFailure {
	if failure := outboundStringMappingFailure(outboundStringCheck{
		field:    "gateway error display message",
		value:    details.DisplayMessage,
		capBytes: domain.MaxOutboundErrorDisplayMessageBytes,
	}); failure != nil {
		return failure
	}
	return outboundPublicIDMappingFailure(
		outboundPublicIDCheck{field: "gateway error task_id", value: details.TaskID},
		outboundPublicIDCheck{field: "gateway error session_group_id", value: details.SessionGroupID},
		outboundPublicIDCheck{field: "gateway error client_message_id", value: details.ClientMessageID},
		outboundPublicIDCheck{field: "gateway error client_response_id", value: details.ClientResponseID},
		outboundPublicIDCheck{field: "gateway error pending_request_id", value: details.PendingRequestID},
		outboundPublicIDCheck{field: "gateway error thread_id", value: details.ThreadID},
	)
}

func safeGatewayErrorDetails(details domain.GatewayErrorDetails, failure *MappingFailure) domain.GatewayErrorDetails {
	details = clearUnsafeGatewayErrorPublicIDs(details)
	if failure != nil && failure.Reason == domain.ReasonResourceExhausted {
		details.Reason = domain.ReasonResourceExhausted
		details.DisplayMessage = overCapErrorDisplayMessage
		return details
	}
	details.Reason = domain.ReasonInternalGatewayError
	details.DisplayMessage = internalGatewayErrorDisplayMessage
	return details
}

func clearUnsafeGatewayErrorPublicIDs(details domain.GatewayErrorDetails) domain.GatewayErrorDetails {
	if unsafeOutboundPublicID(details.TaskID) {
		details.TaskID = ""
	}
	if unsafeOutboundPublicID(details.SessionGroupID) {
		details.SessionGroupID = ""
	}
	if unsafeOutboundPublicID(details.ClientMessageID) {
		details.ClientMessageID = ""
	}
	if unsafeOutboundPublicID(details.ClientResponseID) {
		details.ClientResponseID = ""
	}
	if unsafeOutboundPublicID(details.PendingRequestID) {
		details.PendingRequestID = ""
	}
	if unsafeOutboundPublicID(details.ThreadID) {
		details.ThreadID = ""
	}
	return details
}

func taskLifecycleEventMappingFailure(event domain.TaskLifecycleEvent) *MappingFailure {
	return outboundStringMappingFailure(
		outboundStringCheck{field: "lifecycle reason_code", value: event.ReasonCode, capBytes: domain.MaxSourceLabelBytes},
		outboundStringCheck{field: "lifecycle display message", value: event.DisplayMessage, capBytes: domain.MaxOutboundErrorDisplayMessageBytes},
	)
}

func assistantDeltaEventMappingFailure(event domain.AssistantDeltaEvent) *MappingFailure {
	return outboundStringMappingFailure(outboundStringCheck{
		field:    "assistant text delta",
		value:    event.TextDelta,
		capBytes: domain.MaxOutboundAssistantTextBytes,
	})
}

func assistantMessageCompletedEventMappingFailure(event domain.AssistantMessageCompletedEvent) *MappingFailure {
	return outboundStringMappingFailure(outboundStringCheck{
		field:    "assistant completed message",
		value:    event.Message,
		capBytes: domain.MaxOutboundAssistantTextBytes,
	})
}

func planUpdatedMappingFailure(event domain.PlanUpdatedEvent) *MappingFailure {
	if len(event.Steps) > domain.MaxPlanSteps {
		return resourceExhaustedMappingFailure("plan step count exceeds outbound cap")
	}
	if failure := outboundStringMappingFailure(outboundStringCheck{
		field:    "plan explanation",
		value:    event.Explanation,
		capBytes: domain.MaxOutboundPendingDisplayStringBytes,
	}); failure != nil {
		return failure
	}
	for _, step := range event.Steps {
		if failure := outboundStringMappingFailure(
			outboundStringCheck{field: "plan step", value: step.Step, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
			outboundStringCheck{field: "plan step status", value: step.Status, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
		); failure != nil {
			return failure
		}
	}
	return nil
}

func toolProgressEventMappingFailure(event domain.ToolProgressEvent) *MappingFailure {
	if event.ItemID == "" {
		return invalidMappingFailure("tool progress event is missing item_id")
	}
	if failure := outboundPublicIDMappingFailure(outboundPublicIDCheck{field: "tool progress item_id", value: event.ItemID}); failure != nil {
		return failure
	}
	return outboundStringMappingFailure(
		outboundStringCheck{field: "tool name", value: event.ToolName, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
		outboundStringCheck{field: "tool progress summary", value: event.Summary, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
	)
}

func commandStartedEventMappingFailure(event domain.CommandStartedEvent) *MappingFailure {
	if event.ItemID == "" {
		return invalidMappingFailure("command started event is missing item_id")
	}
	if failure := outboundPublicIDMappingFailure(outboundPublicIDCheck{field: "command started item_id", value: event.ItemID}); failure != nil {
		return failure
	}
	return outboundStringMappingFailure(
		outboundStringCheck{field: "command display", value: event.CommandDisplay, capBytes: domain.MaxOutboundCommandDisplayBytes},
		outboundStringCheck{field: "workspace label", value: event.WorkspaceLabel, capBytes: domain.MaxSourceLabelBytes},
	)
}

func commandOutputDeltaEventMappingFailure(event domain.CommandOutputDeltaEvent) *MappingFailure {
	if event.ItemID == "" {
		return invalidMappingFailure("command output delta event is missing item_id")
	}
	if failure := outboundPublicIDMappingFailure(outboundPublicIDCheck{field: "command output delta item_id", value: event.ItemID}); failure != nil {
		return failure
	}
	return outboundStringMappingFailure(outboundStringCheck{
		field:    "command output delta",
		value:    event.Delta,
		capBytes: domain.MaxOutboundCommandOutputDeltaBytes,
	})
}

func fileDiffUpdatedEventMappingFailure(event domain.FileDiffUpdatedEvent) *MappingFailure {
	if event.ItemID == "" {
		return invalidMappingFailure("file diff updated event is missing item_id")
	}
	if failure := outboundPublicIDMappingFailure(outboundPublicIDCheck{field: "file diff updated item_id", value: event.ItemID}); failure != nil {
		return failure
	}
	return outboundStringMappingFailure(
		outboundStringCheck{field: "file label", value: event.FileLabel, capBytes: domain.MaxSourceLabelBytes},
		outboundStringCheck{field: "change kind", value: event.ChangeKind, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
		outboundStringCheck{field: "file diff summary", value: event.DiffSummary, capBytes: domain.MaxOutboundDiffDisplayBytes},
		outboundStringCheck{field: "file diff unified", value: event.DiffUnified, capBytes: domain.MaxOutboundDiffDisplayBytes},
	)
}

func turnDiffUpdatedEventMappingFailure(event domain.TurnDiffUpdatedEvent) *MappingFailure {
	return outboundStringMappingFailure(
		outboundStringCheck{field: "turn diff summary", value: event.DiffSummary, capBytes: domain.MaxOutboundDiffDisplayBytes},
		outboundStringCheck{field: "turn diff unified", value: event.DiffUnified, capBytes: domain.MaxOutboundDiffDisplayBytes},
	)
}

func pendingRequestCreatedEventMappingFailure(event domain.PendingRequestCreatedEvent) *MappingFailure {
	if event.PendingRequestID == "" {
		return invalidMappingFailure("pending request created event is missing pending_request_id")
	}
	if failure := outboundPublicIDMappingFailure(outboundPublicIDCheck{field: "pending request created pending_request_id", value: event.PendingRequestID}); failure != nil {
		return failure
	}
	if _, ok := PendingTypeToProto(event.PendingType); !ok {
		return invalidMappingFailure("pending request created event has invalid pending_type")
	}
	return pendingRequestDisplayMappingFailure(event.Display)
}

func pendingRequestResolvedEventMappingFailure(event domain.PendingRequestResolvedEvent) *MappingFailure {
	if event.PendingRequestID == "" {
		return invalidMappingFailure("pending request resolved event is missing pending_request_id")
	}
	if failure := outboundPublicIDMappingFailure(outboundPublicIDCheck{field: "pending request resolved pending_request_id", value: event.PendingRequestID}); failure != nil {
		return failure
	}
	if _, ok := PendingTypeToProto(event.PendingType); !ok {
		return invalidMappingFailure("pending request resolved event has invalid pending_type")
	}
	if _, ok := PendingResolutionToProto(event.Resolution); !ok {
		return invalidMappingFailure("pending request resolved event has invalid resolution")
	}
	return outboundStringMappingFailure(outboundStringCheck{
		field:    "pending resolved display message",
		value:    event.DisplayMessage,
		capBytes: domain.MaxOutboundErrorDisplayMessageBytes,
	})
}

func taskTerminalEventMappingFailure(event domain.TaskTerminalEvent) *MappingFailure {
	if _, ok := TerminalStateToProto(event.TerminalState); !ok {
		return invalidMappingFailure("terminal event has invalid state")
	}
	return outboundStringMappingFailure(
		outboundStringCheck{field: "terminal result summary", value: event.ResultSummary, capBytes: domain.MaxOutboundErrorDisplayMessageBytes},
		outboundStringCheck{field: "terminal error message", value: event.ErrorMessage, capBytes: domain.MaxOutboundErrorDisplayMessageBytes},
	)
}

func gatewayWarningEventMappingFailure(event domain.GatewayWarningEvent) *MappingFailure {
	return outboundStringMappingFailure(
		outboundStringCheck{field: "gateway warning code", value: event.Code, capBytes: domain.MaxSourceLabelBytes},
		outboundStringCheck{field: "gateway warning message", value: event.Message, capBytes: domain.MaxOutboundErrorDisplayMessageBytes},
		outboundStringCheck{field: "gateway warning request type", value: event.RequestType, capBytes: domain.MaxSourceLabelBytes},
		outboundStringCheck{field: "gateway warning auto resolution", value: event.AutoResolution, capBytes: domain.MaxSourceLabelBytes},
		outboundStringCheck{field: "gateway warning limit reason", value: event.LimitReason, capBytes: domain.MaxSourceLabelBytes},
	)
}

func pendingRequestDisplayMappingFailure(display domain.PendingRequestDisplay) *MappingFailure {
	budget := outboundStringBudget{capBytes: domain.MaxOutboundPendingDisplayPayloadBytes}
	switch payload := display.(type) {
	case domain.CommandApprovalDisplay:
		return commandApprovalDisplayMappingFailure(payload, &budget)
	case *domain.CommandApprovalDisplay:
		if payload == nil {
			return invalidMappingFailure("pending request display is nil")
		}
		return commandApprovalDisplayMappingFailure(*payload, &budget)
	case domain.FileChangeApprovalDisplay:
		return fileChangeApprovalDisplayMappingFailure(payload, &budget)
	case *domain.FileChangeApprovalDisplay:
		if payload == nil {
			return invalidMappingFailure("pending request display is nil")
		}
		return fileChangeApprovalDisplayMappingFailure(*payload, &budget)
	case domain.PermissionsApprovalDisplay:
		return permissionsApprovalDisplayMappingFailure(payload, &budget)
	case *domain.PermissionsApprovalDisplay:
		if payload == nil {
			return invalidMappingFailure("pending request display is nil")
		}
		return permissionsApprovalDisplayMappingFailure(*payload, &budget)
	case domain.McpElicitationDisplay:
		return mcpElicitationDisplayMappingFailure(payload, &budget)
	case *domain.McpElicitationDisplay:
		if payload == nil {
			return invalidMappingFailure("pending request display is nil")
		}
		return mcpElicitationDisplayMappingFailure(*payload, &budget)
	case domain.ToolUserInputDisplay:
		return toolUserInputDisplayMappingFailure(payload, &budget)
	case *domain.ToolUserInputDisplay:
		if payload == nil {
			return invalidMappingFailure("pending request display is nil")
		}
		return toolUserInputDisplayMappingFailure(*payload, &budget)
	default:
		return invalidMappingFailure("pending request display has invalid type")
	}
}

func commandApprovalDisplayMappingFailure(display domain.CommandApprovalDisplay, budget *outboundStringBudget) *MappingFailure {
	if failure := approvalDecisionOptionsMappingFailure(display.DecisionOptions, budget, approvalDecisionOptionPolicy{
		requireSelectableSafeDirectDecision: true,
		rejectSelectableApprove:             display.ApprovalSecurity != nil && display.ApprovalSecurity.BlockingReason != "",
	}); failure != nil {
		return failure
	}
	if failure := budget.add(
		outboundStringCheck{field: "command approval command display", value: display.CommandDisplay, capBytes: domain.MaxOutboundCommandDisplayBytes},
		outboundStringCheck{field: "command approval workspace label", value: display.WorkspaceLabel, capBytes: domain.MaxSourceLabelBytes},
		outboundStringCheck{field: "command approval reason", value: display.Reason, capBytes: domain.MaxOutboundErrorDisplayMessageBytes},
	); failure != nil {
		return failure
	}
	if display.ApprovalSecurity == nil {
		return nil
	}
	entries := len(display.ApprovalSecurity.AdditionalFilesystemEntries) + len(display.ApprovalSecurity.NetworkPolicyAmendmentSummaries)
	if entries > domain.MaxApprovalSecurityMetadataEntries {
		return resourceExhaustedMappingFailure("approval security metadata entry count exceeds outbound cap")
	}
	return approvalSecurityMetadataMappingFailure(display.ApprovalSecurity, budget)
}

func fileChangeApprovalDisplayMappingFailure(display domain.FileChangeApprovalDisplay, budget *outboundStringBudget) *MappingFailure {
	if failure := approvalDecisionOptionsMappingFailure(display.DecisionOptions, budget, approvalDecisionOptionPolicy{
		requireSelectableSafeDirectDecision: true,
		rejectSelectableApprove:             display.GrantRoot != nil && display.GrantRoot.Present && !display.GrantRoot.Approvable,
	}); failure != nil {
		return failure
	}
	if failure := budget.add(
		outboundStringCheck{field: "file approval file label", value: display.FileLabel, capBytes: domain.MaxSourceLabelBytes},
		outboundStringCheck{field: "file approval change kind", value: display.ChangeKind, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
		outboundStringCheck{field: "file approval diff summary", value: display.DiffSummary, capBytes: domain.MaxOutboundDiffDisplayBytes},
		outboundStringCheck{field: "file approval diff unified", value: display.DiffUnified, capBytes: domain.MaxOutboundDiffDisplayBytes},
	); failure != nil {
		return failure
	}
	return fileGrantRootDisplayMappingFailure(display.GrantRoot, budget)
}

func permissionsApprovalDisplayMappingFailure(display domain.PermissionsApprovalDisplay, budget *outboundStringBudget) *MappingFailure {
	if display.RecommendedScope != "" {
		if _, ok := PermissionScopeToProto(display.RecommendedScope); !ok {
			return invalidMappingFailure("permissions approval display has invalid recommended scope")
		}
	}
	if len(display.RequestedPermissions) > domain.MaxPermissionAtoms {
		return resourceExhaustedMappingFailure("permission atom count exceeds outbound cap")
	}
	if failure := budget.add(outboundStringCheck{
		field:    "permissions approval reason",
		value:    display.Reason,
		capBytes: domain.MaxOutboundErrorDisplayMessageBytes,
	}); failure != nil {
		return failure
	}
	seenPermissionIDs := make(map[string]struct{}, len(display.RequestedPermissions))
	for _, permission := range display.RequestedPermissions {
		if permission.PermissionID == "" {
			return invalidMappingFailure("permission atom is missing permission_id")
		}
		if failure := outboundPublicIDMappingFailure(outboundPublicIDCheck{field: "permission atom permission_id", value: permission.PermissionID}); failure != nil {
			return failure
		}
		if _, ok := seenPermissionIDs[permission.PermissionID]; ok {
			return invalidMappingFailure("permission atom has duplicate permission_id")
		}
		seenPermissionIDs[permission.PermissionID] = struct{}{}
		if failure := budget.add(
			outboundStringCheck{field: "permission kind", value: permission.Kind, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
			outboundStringCheck{field: "permission display label", value: permission.DisplayLabel, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
			outboundStringCheck{field: "permission scope label", value: permission.ScopeLabel, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
			outboundStringCheck{field: "permission ungrantable reason", value: permission.UngrantableReason, capBytes: domain.MaxOutboundErrorDisplayMessageBytes},
		); failure != nil {
			return failure
		}
	}
	return nil
}

func mcpElicitationDisplayMappingFailure(display domain.McpElicitationDisplay, budget *outboundStringBudget) *MappingFailure {
	if _, ok := ElicitationModeToProto(display.Mode); !ok {
		return invalidMappingFailure("mcp elicitation display has invalid mode")
	}
	return budget.add(
		outboundStringCheck{field: "mcp elicitation message", value: display.Message, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
		outboundStringCheck{field: "mcp elicitation form schema JSON", value: display.FormSchemaJSON, capBytes: domain.MaxOutboundMcpFormSchemaBytes},
		outboundStringCheck{field: "mcp elicitation url", value: display.URL, capBytes: domain.MaxSourceURIBytes},
		outboundStringCheck{field: "mcp elicitation submit label", value: display.SubmitLabel, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
	)
}

func toolUserInputDisplayMappingFailure(display domain.ToolUserInputDisplay, budget *outboundStringBudget) *MappingFailure {
	if len(display.Questions) > domain.MaxToolUserInputQuestions {
		return resourceExhaustedMappingFailure("tool user input question count exceeds outbound cap")
	}
	seenQuestionIDs := make(map[string]struct{}, len(display.Questions))
	for _, question := range display.Questions {
		if question.ID == "" {
			return invalidMappingFailure("tool user input question is missing id")
		}
		if failure := outboundPublicIDMappingFailure(outboundPublicIDCheck{field: "tool user input question id", value: question.ID}); failure != nil {
			return failure
		}
		if _, ok := seenQuestionIDs[question.ID]; ok {
			return invalidMappingFailure("tool user input question has duplicate id")
		}
		seenQuestionIDs[question.ID] = struct{}{}
		if len(question.Options) > domain.MaxToolUserInputOptionsPerQuestion {
			return resourceExhaustedMappingFailure("tool user input option count exceeds outbound cap")
		}
		if failure := budget.add(
			outboundStringCheck{field: "tool user input header", value: question.Header, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
			outboundStringCheck{field: "tool user input question", value: question.Question, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
		); failure != nil {
			return failure
		}
		for _, option := range question.Options {
			if failure := budget.add(outboundStringCheck{
				field:    "tool user input option",
				value:    option,
				capBytes: domain.MaxOutboundPendingDisplayStringBytes,
			}); failure != nil {
				return failure
			}
		}
	}
	return nil
}

func approvalSecurityMetadataMappingFailure(metadata *domain.ApprovalSecurityMetadata, budget *outboundStringBudget) *MappingFailure {
	if metadata.NetworkContext != nil {
		if failure := budget.add(
			outboundStringCheck{field: "approval security host label", value: metadata.NetworkContext.HostLabel, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
			outboundStringCheck{field: "approval security protocol", value: metadata.NetworkContext.Protocol, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
		); failure != nil {
			return failure
		}
	}
	for _, entry := range metadata.AdditionalFilesystemEntries {
		if entry.EntryID == "" {
			return invalidMappingFailure("approval security filesystem entry is missing entry_id")
		}
		if failure := outboundPublicIDMappingFailure(outboundPublicIDCheck{field: "approval security filesystem entry_id", value: entry.EntryID}); failure != nil {
			return failure
		}
		if failure := budget.add(
			outboundStringCheck{field: "approval security filesystem access", value: entry.Access, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
			outboundStringCheck{field: "approval security filesystem path label", value: entry.PathLabel, capBytes: domain.MaxSourceLabelBytes},
			outboundStringCheck{field: "approval security filesystem unapprovable reason", value: entry.UnapprovableReason, capBytes: domain.MaxOutboundErrorDisplayMessageBytes},
		); failure != nil {
			return failure
		}
	}
	if metadata.ExecPolicyAmendmentSummary != nil {
		if failure := budget.add(outboundStringCheck{
			field:    "approval security exec policy command display",
			value:    metadata.ExecPolicyAmendmentSummary.CommandDisplay,
			capBytes: domain.MaxOutboundCommandDisplayBytes,
		}); failure != nil {
			return failure
		}
	}
	for _, summary := range metadata.NetworkPolicyAmendmentSummaries {
		if summary.Approvable {
			return invalidMappingFailure("network policy amendment summary must not be approvable")
		}
		if failure := budget.add(
			outboundStringCheck{field: "approval security network host label", value: summary.HostLabel, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
			outboundStringCheck{field: "approval security network action", value: summary.Action, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
		); failure != nil {
			return failure
		}
	}
	return budget.add(outboundStringCheck{
		field:    "approval security blocking reason",
		value:    metadata.BlockingReason,
		capBytes: domain.MaxOutboundErrorDisplayMessageBytes,
	})
}

func fileGrantRootDisplayMappingFailure(display *domain.FileGrantRootDisplay, budget *outboundStringBudget) *MappingFailure {
	if display == nil {
		return nil
	}
	if display.Present && display.Approvable && !display.UnderConfiguredCWD {
		return invalidMappingFailure("file grant root must not be approvable outside configured cwd")
	}
	if display.Present && display.Approvable && strings.TrimSpace(display.RootLabel) == "" {
		return invalidMappingFailure("approvable file grant root is missing root label")
	}
	return budget.add(
		outboundStringCheck{field: "file grant root label", value: display.RootLabel, capBytes: domain.MaxSourceLabelBytes},
		outboundStringCheck{field: "file grant root unapprovable reason", value: display.UnapprovableReason, capBytes: domain.MaxOutboundErrorDisplayMessageBytes},
	)
}

type approvalDecisionOptionPolicy struct {
	requireSelectableSafeDirectDecision bool
	rejectSelectableApprove             bool
}

func approvalDecisionOptionsMappingFailure(
	options []domain.ApprovalDecisionOption,
	budget *outboundStringBudget,
	policy approvalDecisionOptionPolicy,
) *MappingFailure {
	if len(options) > domain.MaxApprovalDecisionOptions {
		return resourceExhaustedMappingFailure("approval decision option count exceeds outbound cap")
	}
	if policy.requireSelectableSafeDirectDecision && len(options) == 0 {
		return invalidMappingFailure("approval display is missing a selectable safe direct decision option")
	}
	hasSelectableSafeDirectDecision := false
	seenDecisionIDs := make(map[string]struct{}, len(options))
	for _, option := range options {
		if option.DecisionID == "" {
			return invalidMappingFailure("approval decision option is missing decision_id")
		}
		if failure := outboundPublicIDMappingFailure(outboundPublicIDCheck{field: "approval decision option decision_id", value: option.DecisionID}); failure != nil {
			return failure
		}
		if _, ok := seenDecisionIDs[option.DecisionID]; ok {
			return invalidMappingFailure("approval decision option has duplicate decision_id")
		}
		seenDecisionIDs[option.DecisionID] = struct{}{}
		if option.WireDecision == "" {
			if option.Selectable {
				return invalidMappingFailure("selectable approval decision option is missing wire_decision")
			}
		} else if _, ok := ApprovalWireDecisionToProto(option.WireDecision); !ok {
			return invalidMappingFailure("approval decision option has invalid wire_decision")
		}
		if policy.rejectSelectableApprove && option.Selectable {
			switch option.WireDecision {
			case domain.ApprovalWireDecisionAccept, domain.ApprovalWireDecisionAcceptForSession:
				return invalidMappingFailure("selectable approve decision option is unsafe")
			case domain.ApprovalWireDecisionDecline, domain.ApprovalWireDecisionCancel, "":
			}
		}
		if option.Selectable {
			switch option.WireDecision {
			case domain.ApprovalWireDecisionDecline, domain.ApprovalWireDecisionCancel:
				hasSelectableSafeDirectDecision = true
			case domain.ApprovalWireDecisionAccept, domain.ApprovalWireDecisionAcceptForSession, "":
			}
		}
		if failure := budget.add(
			outboundStringCheck{field: "approval decision display label", value: option.DisplayLabel, capBytes: domain.MaxOutboundPendingDisplayStringBytes},
			outboundStringCheck{field: "approval decision summary", value: option.Summary, capBytes: domain.MaxOutboundErrorDisplayMessageBytes},
			outboundStringCheck{field: "approval decision unsupported reason", value: option.UnsupportedReason, capBytes: domain.MaxOutboundErrorDisplayMessageBytes},
		); failure != nil {
			return failure
		}
	}
	if policy.requireSelectableSafeDirectDecision && !hasSelectableSafeDirectDecision {
		return invalidMappingFailure("approval display is missing a selectable safe direct decision option")
	}
	return nil
}

type outboundStringCheck struct {
	field    string
	value    string
	capBytes int
}

type outboundPublicIDCheck struct {
	field string
	value string
}

func outboundPublicIDMappingFailure(checks ...outboundPublicIDCheck) *MappingFailure {
	for _, check := range checks {
		if len(check.value) > domain.MaxPublicIDBytes {
			return resourceExhaustedMappingFailure(check.field + " exceeds outbound public id cap")
		}
		if hasInvalidOutboundPublicIDShape(check.value) {
			return invalidMappingFailure(check.field + " must not have leading or trailing whitespace")
		}
	}
	return nil
}

func unsafeOutboundPublicID(value string) bool {
	return len(value) > domain.MaxPublicIDBytes || hasInvalidOutboundPublicIDShape(value)
}

func hasInvalidOutboundPublicIDShape(value string) bool {
	return value != "" && strings.TrimSpace(value) != value
}

func outboundStringMappingFailure(checks ...outboundStringCheck) *MappingFailure {
	for _, check := range checks {
		if len(check.value) > check.capBytes {
			return resourceExhaustedMappingFailure(check.field + " exceeds outbound cap")
		}
	}
	return nil
}

type outboundStringBudget struct {
	usedBytes int
	capBytes  int
}

func (budget *outboundStringBudget) add(checks ...outboundStringCheck) *MappingFailure {
	for _, check := range checks {
		if failure := outboundStringMappingFailure(check); failure != nil {
			return failure
		}
		budget.usedBytes += len(check.value)
		if budget.usedBytes > budget.capBytes {
			return resourceExhaustedMappingFailure("pending display payload exceeds outbound cap")
		}
	}
	return nil
}
