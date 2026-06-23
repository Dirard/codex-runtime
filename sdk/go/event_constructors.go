package codex

// NewAssistantTextDeltaEvent constructs a friendly assistant text event for tests and custom adapters.
func NewAssistantTextDeltaEvent(meta EventMeta, textDelta string) *AssistantTextDelta {
	meta.Source = EventSourceChatStream
	return &AssistantTextDelta{
		baseEvent: baseEvent{
			kind: EventKindAssistantTextDelta,
			meta: meta,
			raw:  newRawEvent(EventKindAssistantTextDelta, meta.Source, meta.ID, meta.Cursor, false, nil),
		},
		textDelta: textDelta,
	}
}

// NewApprovalRequestedEvent constructs a friendly approval request event for tests and custom adapters.
func NewApprovalRequestedEvent(meta EventMeta, pendingID string, display ActionDisplay, decisions []ApprovalDecision) *ApprovalRequested {
	meta.Source = EventSourceChatStream
	return &ApprovalRequested{
		pendingBase: pendingBase{
			baseEvent: baseEvent{
				kind: EventKindApprovalRequested,
				meta: meta,
				raw:  newRawEvent(EventKindApprovalRequested, meta.Source, meta.ID, meta.Cursor, false, nil),
			},
			pendingID:   pendingID,
			pendingKind: PendingKindApproval,
			display:     display,
		},
		subject:   ApprovalSubjectCommand,
		decisions: append([]ApprovalDecision(nil), decisions...),
	}
}

// NewRunCompletedEvent constructs a friendly completed terminal event for tests and custom adapters.
func NewRunCompletedEvent(meta EventMeta, summary string) *RunCompleted {
	meta.Source = EventSourceChatStream
	return &RunCompleted{
		baseEvent: baseEvent{
			kind: EventKindRunCompleted,
			meta: meta,
			raw:  newRawEvent(EventKindRunCompleted, meta.Source, meta.ID, meta.Cursor, false, nil),
		},
		result: TerminalResult{State: TerminalStateCompleted, Summary: summary},
	}
}
