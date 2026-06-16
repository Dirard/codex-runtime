package grpcapi

import (
	"fmt"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
)

func ContextBlockKindFromProto(kind pb.ContextBlockKind) (domain.ContextBlockKind, bool) {
	switch kind {
	case pb.ContextBlockKind_CONTEXT_BLOCK_KIND_APPLICATION:
		return domain.ContextBlockKindApplication, true
	case pb.ContextBlockKind_CONTEXT_BLOCK_KIND_UNTRUSTED:
		return domain.ContextBlockKindUntrusted, true
	case pb.ContextBlockKind_CONTEXT_BLOCK_KIND_UNSPECIFIED:
		return "", false
	default:
		return "", false
	}
}

func ContextBlockKindToProto(kind domain.ContextBlockKind) (pb.ContextBlockKind, bool) {
	switch kind {
	case domain.ContextBlockKindApplication:
		return pb.ContextBlockKind_CONTEXT_BLOCK_KIND_APPLICATION, true
	case domain.ContextBlockKindUntrusted:
		return pb.ContextBlockKind_CONTEXT_BLOCK_KIND_UNTRUSTED, true
	default:
		return pb.ContextBlockKind_CONTEXT_BLOCK_KIND_UNSPECIFIED, false
	}
}

func ChatThreadLifecycleToProto(lifecycle domain.ChatThreadLifecycle) (pb.ChatThreadLifecycle, bool) {
	switch lifecycle {
	case domain.ChatThreadLifecycleNotLoaded:
		return pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_NOT_LOADED, true
	case domain.ChatThreadLifecycleIdle:
		return pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_IDLE, true
	case domain.ChatThreadLifecycleActiveRunning:
		return pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_ACTIVE_RUNNING, true
	case domain.ChatThreadLifecycleWaitingOnApproval:
		return pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_WAITING_ON_APPROVAL, true
	case domain.ChatThreadLifecycleWaitingOnUserInput:
		return pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_WAITING_ON_USER_INPUT, true
	case domain.ChatThreadLifecycleSystemError:
		return pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_SYSTEM_ERROR, true
	case domain.ChatThreadLifecycleUnknown:
		return pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_UNKNOWN, true
	default:
		return pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_UNSPECIFIED, false
	}
}

func ChatTurnLifecycleToProto(lifecycle domain.ChatTurnLifecycle) (pb.ChatTurnLifecycle, bool) {
	switch lifecycle {
	case domain.ChatTurnLifecycleInProgress:
		return pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_IN_PROGRESS, true
	case domain.ChatTurnLifecycleCompleted:
		return pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_COMPLETED, true
	case domain.ChatTurnLifecycleInterrupted:
		return pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_INTERRUPTED, true
	case domain.ChatTurnLifecycleFailed:
		return pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_FAILED, true
	case domain.ChatTurnLifecycleUnknown:
		return pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_TURN_UNKNOWN, true
	case domain.ChatTurnLifecycleUnavailable:
		return pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_TURN_UNAVAILABLE, true
	default:
		return pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_UNSPECIFIED, false
	}
}

func ChatCapabilityStateToProto(state domain.ChatCapabilityState) (pb.ChatCapabilityState, bool) {
	switch state {
	case domain.ChatCapabilitySupported:
		return pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED, true
	case domain.ChatCapabilityUnsupported:
		return pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_UNSUPPORTED, true
	case domain.ChatCapabilityUnavailable:
		return pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_UNAVAILABLE, true
	case domain.ChatCapabilityUnknown:
		return pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_UNKNOWN, true
	case domain.ChatCapabilityNarrowed:
		return pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_NARROWED, true
	default:
		return pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_UNSPECIFIED, false
	}
}

func ChatHistoryDepthFromProto(depth pb.ChatHistoryDepth) (domain.ChatHistoryDepth, bool) {
	switch depth {
	case pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_TURN_SUMMARY:
		return domain.ChatHistoryDepthTurnSummary, true
	case pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_ITEM_LEVEL:
		return domain.ChatHistoryDepthItemLevel, true
	case pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_UNSPECIFIED:
		return "", false
	default:
		return "", false
	}
}

func ChatHistoryDepthToProto(depth domain.ChatHistoryDepth) (pb.ChatHistoryDepth, bool) {
	switch depth {
	case domain.ChatHistoryDepthTurnSummary:
		return pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_TURN_SUMMARY, true
	case domain.ChatHistoryDepthItemLevel:
		return pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_ITEM_LEVEL, true
	default:
		return pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_UNSPECIFIED, false
	}
}

func ChatHistorySortDirectionFromProto(direction pb.ChatHistorySortDirection) (domain.ChatHistorySortDirection, bool) {
	switch direction {
	case pb.ChatHistorySortDirection_CHAT_HISTORY_SORT_DIRECTION_ASCENDING:
		return domain.ChatHistorySortAscending, true
	case pb.ChatHistorySortDirection_CHAT_HISTORY_SORT_DIRECTION_DESCENDING:
		return domain.ChatHistorySortDescending, true
	case pb.ChatHistorySortDirection_CHAT_HISTORY_SORT_DIRECTION_UNSPECIFIED:
		return "", false
	default:
		return "", false
	}
}

func ChatTurnItemsViewToProto(view domain.ChatTurnItemsView) (pb.ChatTurnItemsView, bool) {
	switch view {
	case domain.ChatTurnItemsViewNotLoaded:
		return pb.ChatTurnItemsView_CHAT_TURN_ITEMS_VIEW_NOT_LOADED, true
	case domain.ChatTurnItemsViewSummary:
		return pb.ChatTurnItemsView_CHAT_TURN_ITEMS_VIEW_SUMMARY, true
	case domain.ChatTurnItemsViewFullUnsupported:
		return pb.ChatTurnItemsView_CHAT_TURN_ITEMS_VIEW_FULL_UNSUPPORTED, true
	default:
		return pb.ChatTurnItemsView_CHAT_TURN_ITEMS_VIEW_UNSPECIFIED, false
	}
}

func TaskStateToProto(state domain.TaskState) (pb.TaskState, bool) {
	switch state {
	case domain.TaskStateStarting:
		return pb.TaskState_TASK_STATE_STARTING, true
	case domain.TaskStateRunning:
		return pb.TaskState_TASK_STATE_RUNNING, true
	case domain.TaskStateWaitingForPendingRequest:
		return pb.TaskState_TASK_STATE_WAITING_FOR_PENDING_REQUEST, true
	case domain.TaskStateInterrupting:
		return pb.TaskState_TASK_STATE_INTERRUPTING, true
	case domain.TaskStateCompleted:
		return pb.TaskState_TASK_STATE_COMPLETED, true
	case domain.TaskStateFailed:
		return pb.TaskState_TASK_STATE_FAILED, true
	case domain.TaskStateInterrupted:
		return pb.TaskState_TASK_STATE_INTERRUPTED, true
	default:
		return pb.TaskState_TASK_STATE_UNSPECIFIED, false
	}
}

func TaskLifecycleEventTypeToProto(event domain.TaskLifecycleEventType) (pb.TaskLifecycleEventType, bool) {
	switch event {
	case domain.TaskLifecycleEventTaskStarted:
		return pb.TaskLifecycleEventType_TASK_LIFECYCLE_EVENT_TYPE_TASK_STARTED, true
	case domain.TaskLifecycleEventThreadStarted:
		return pb.TaskLifecycleEventType_TASK_LIFECYCLE_EVENT_TYPE_THREAD_STARTED, true
	case domain.TaskLifecycleEventTurnStarted:
		return pb.TaskLifecycleEventType_TASK_LIFECYCLE_EVENT_TYPE_TURN_STARTED, true
	case domain.TaskLifecycleEventStateChanged:
		return pb.TaskLifecycleEventType_TASK_LIFECYCLE_EVENT_TYPE_STATE_CHANGED, true
	default:
		return pb.TaskLifecycleEventType_TASK_LIFECYCLE_EVENT_TYPE_UNSPECIFIED, false
	}
}

func ReplayNoticeCodeToProto(code domain.ReplayNoticeCode) (pb.ReplayNoticeCode, bool) {
	switch code {
	case domain.ReplayNoticeStartEvicted:
		return pb.ReplayNoticeCode_REPLAY_NOTICE_CODE_START_EVICTED, true
	case domain.ReplayNoticeCursorEvicted:
		return pb.ReplayNoticeCode_REPLAY_NOTICE_CODE_CURSOR_EVICTED, true
	default:
		return pb.ReplayNoticeCode_REPLAY_NOTICE_CODE_UNSPECIFIED, false
	}
}

func ChatReplayNoticeCodeToProto(code domain.ChatReplayNoticeCode) (pb.ChatReplayNoticeCode, bool) {
	switch code {
	case domain.ChatReplayNoticeStartEvicted:
		return pb.ChatReplayNoticeCode_CHAT_REPLAY_NOTICE_CODE_START_EVICTED, true
	case domain.ChatReplayNoticeCursorEvicted:
		return pb.ChatReplayNoticeCode_CHAT_REPLAY_NOTICE_CODE_CURSOR_EVICTED, true
	case domain.ChatReplayNoticeUnavailableAfterRestart:
		return pb.ChatReplayNoticeCode_CHAT_REPLAY_NOTICE_CODE_UNAVAILABLE_AFTER_RESTART, true
	case domain.ChatReplayNoticeNarrowedToLive:
		return pb.ChatReplayNoticeCode_CHAT_REPLAY_NOTICE_CODE_NARROWED_TO_LIVE, true
	default:
		return pb.ChatReplayNoticeCode_CHAT_REPLAY_NOTICE_CODE_UNSPECIFIED, false
	}
}

func ToolStateToProto(state domain.ToolState) (pb.ToolState, bool) {
	switch state {
	case domain.ToolStateStarted:
		return pb.ToolState_TOOL_STATE_STARTED, true
	case domain.ToolStateRunning:
		return pb.ToolState_TOOL_STATE_RUNNING, true
	case domain.ToolStateCompleted:
		return pb.ToolState_TOOL_STATE_COMPLETED, true
	case domain.ToolStateFailed:
		return pb.ToolState_TOOL_STATE_FAILED, true
	default:
		return pb.ToolState_TOOL_STATE_UNSPECIFIED, false
	}
}

func CommandOutputStreamToProto(stream domain.CommandOutputStream) (pb.CommandOutputStream, bool) {
	switch stream {
	case domain.CommandOutputStreamCombined:
		return pb.CommandOutputStream_COMMAND_OUTPUT_STREAM_COMBINED, true
	default:
		return pb.CommandOutputStream_COMMAND_OUTPUT_STREAM_UNSPECIFIED, false
	}
}

func PendingTypeToProto(pendingType domain.PendingType) (pb.PendingType, bool) {
	switch pendingType {
	case domain.PendingTypeCommandApproval:
		return pb.PendingType_PENDING_TYPE_COMMAND_APPROVAL, true
	case domain.PendingTypeFileChangeApproval:
		return pb.PendingType_PENDING_TYPE_FILE_CHANGE_APPROVAL, true
	case domain.PendingTypePermissionsApproval:
		return pb.PendingType_PENDING_TYPE_PERMISSIONS_APPROVAL, true
	case domain.PendingTypeMcpElicitation:
		return pb.PendingType_PENDING_TYPE_MCP_ELICITATION, true
	case domain.PendingTypeToolUserInput:
		return pb.PendingType_PENDING_TYPE_TOOL_USER_INPUT, true
	default:
		return pb.PendingType_PENDING_TYPE_UNSPECIFIED, false
	}
}

func PendingResolutionToProto(resolution domain.PendingResolution) (pb.PendingResolution, bool) {
	switch resolution {
	case domain.PendingResolutionAccepted:
		return pb.PendingResolution_PENDING_RESOLUTION_ACCEPTED, true
	case domain.PendingResolutionDeclined:
		return pb.PendingResolution_PENDING_RESOLUTION_DECLINED, true
	case domain.PendingResolutionCancelled:
		return pb.PendingResolution_PENDING_RESOLUTION_CANCELLED, true
	case domain.PendingResolutionGranted:
		return pb.PendingResolution_PENDING_RESOLUTION_GRANTED, true
	case domain.PendingResolutionDenied:
		return pb.PendingResolution_PENDING_RESOLUTION_DENIED, true
	case domain.PendingResolutionAnswered:
		return pb.PendingResolution_PENDING_RESOLUTION_ANSWERED, true
	case domain.PendingResolutionExpired:
		return pb.PendingResolution_PENDING_RESOLUTION_EXPIRED, true
	case domain.PendingResolutionCleared:
		return pb.PendingResolution_PENDING_RESOLUTION_CLEARED, true
	case domain.PendingResolutionFailed:
		return pb.PendingResolution_PENDING_RESOLUTION_FAILED, true
	default:
		return pb.PendingResolution_PENDING_RESOLUTION_UNSPECIFIED, false
	}
}

func TerminalStateToProto(state domain.TerminalState) (pb.TerminalState, bool) {
	switch state {
	case domain.TerminalStateCompleted:
		return pb.TerminalState_TERMINAL_STATE_COMPLETED, true
	case domain.TerminalStateFailed:
		return pb.TerminalState_TERMINAL_STATE_FAILED, true
	case domain.TerminalStateInterrupted:
		return pb.TerminalState_TERMINAL_STATE_INTERRUPTED, true
	default:
		return pb.TerminalState_TERMINAL_STATE_UNSPECIFIED, false
	}
}

func ApprovalWireDecisionToProto(decision domain.ApprovalWireDecision) (pb.ApprovalWireDecision, bool) {
	switch decision {
	case domain.ApprovalWireDecisionAccept:
		return pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT, true
	case domain.ApprovalWireDecisionAcceptForSession:
		return pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT_FOR_SESSION, true
	case domain.ApprovalWireDecisionDecline:
		return pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_DECLINE, true
	case domain.ApprovalWireDecisionCancel:
		return pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_CANCEL, true
	default:
		return pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_UNSPECIFIED, false
	}
}

func ApprovalWireDecisionFromProto(decision pb.ApprovalWireDecision) (domain.ApprovalWireDecision, bool) {
	switch decision {
	case pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT:
		return domain.ApprovalWireDecisionAccept, true
	case pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_ACCEPT_FOR_SESSION:
		return domain.ApprovalWireDecisionAcceptForSession, true
	case pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_DECLINE:
		return domain.ApprovalWireDecisionDecline, true
	case pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_CANCEL:
		return domain.ApprovalWireDecisionCancel, true
	case pb.ApprovalWireDecision_APPROVAL_WIRE_DECISION_UNSPECIFIED:
		return "", false
	default:
		return "", false
	}
}

func ApprovalDecisionAppServerWire(decision pb.ApprovalWireDecision) (string, error) {
	domainDecision, ok := ApprovalWireDecisionFromProto(decision)
	if !ok {
		return "", fmt.Errorf("unsupported approval wire decision %s", decision.String())
	}
	wire, ok := domainDecision.AppServerWireValue()
	if !ok {
		return "", fmt.Errorf("unsupported approval domain decision %q", domainDecision)
	}
	return wire, nil
}

func PermissionScopeToProto(scope domain.PermissionScope) (pb.PermissionScope, bool) {
	switch scope {
	case domain.PermissionScopeTurn:
		return pb.PermissionScope_PERMISSION_SCOPE_TURN, true
	case domain.PermissionScopeSession:
		return pb.PermissionScope_PERMISSION_SCOPE_SESSION, true
	default:
		return pb.PermissionScope_PERMISSION_SCOPE_UNSPECIFIED, false
	}
}

func PermissionScopeFromProto(scope pb.PermissionScope) (domain.PermissionScope, bool) {
	switch scope {
	case pb.PermissionScope_PERMISSION_SCOPE_TURN:
		return domain.PermissionScopeTurn, true
	case pb.PermissionScope_PERMISSION_SCOPE_SESSION:
		return domain.PermissionScopeSession, true
	case pb.PermissionScope_PERMISSION_SCOPE_UNSPECIFIED:
		return "", false
	default:
		return "", false
	}
}

func ElicitationModeToProto(mode domain.ElicitationMode) (pb.ElicitationMode, bool) {
	switch mode {
	case domain.ElicitationModeForm:
		return pb.ElicitationMode_ELICITATION_MODE_FORM, true
	case domain.ElicitationModeURL:
		return pb.ElicitationMode_ELICITATION_MODE_URL, true
	default:
		return pb.ElicitationMode_ELICITATION_MODE_UNSPECIFIED, false
	}
}

func McpElicitationActionFromProto(action pb.McpElicitationAction) (domain.McpElicitationAction, bool) {
	switch action {
	case pb.McpElicitationAction_MCP_ELICITATION_ACTION_ACCEPT:
		return domain.McpElicitationActionAccept, true
	case pb.McpElicitationAction_MCP_ELICITATION_ACTION_DECLINE:
		return domain.McpElicitationActionDecline, true
	case pb.McpElicitationAction_MCP_ELICITATION_ACTION_CANCEL:
		return domain.McpElicitationActionCancel, true
	case pb.McpElicitationAction_MCP_ELICITATION_ACTION_UNSPECIFIED:
		return "", false
	default:
		return "", false
	}
}
