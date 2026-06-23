package codex

import pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"

// CommandOutputStream names the command output channel reported by the runtime.
// Current chat runtime output is combined; stdout/stderr remain future
// source-backed extensions.
type CommandOutputStream string

const (
	CommandOutputStreamUnknown  CommandOutputStream = ""
	CommandOutputStreamCombined CommandOutputStream = "combined"
)

// CommandRef is the friendly command identity shared by command start and
// command output events. Known is false for limited orphan replay output where
// the stream has output bytes but no source-backed command snapshot.
type CommandRef struct {
	_ noUnkeyedLiterals

	ID             string
	Display        string
	WorkspaceLabel string
	Known          bool
}

// CommandStarted marks the beginning of a command-execution-shaped runtime
// item. It is not emitted for generic item lifecycle notifications.
type CommandStarted struct {
	baseEvent
	command CommandRef
}

// Command returns the source-backed command id and display string.
func (event *CommandStarted) Command() CommandRef { return event.command }

// Preview returns a source-backed command preview when the runtime has one.
// Current runtime command starts do not carry a separate preview.
func (event *CommandStarted) Preview() (string, bool) {
	return "", false
}

// WorkspaceLabel returns the source-backed workspace label when present.
func (event *CommandStarted) WorkspaceLabel() (string, bool) {
	return event.command.WorkspaceLabel, event.command.WorkspaceLabel != ""
}

// CommandOutput is a command output delta. Normal flow includes a known command
// context from a prior CommandStarted event; orphan replay keeps only stream and
// delta with OrphanReplay true.
type CommandOutput struct {
	baseEvent
	command      CommandRef
	stream       CommandOutputStream
	delta        string
	truncated    bool
	orphanReplay bool
}

// Command returns the known command context, or Known false for orphan replay.
func (event *CommandOutput) Command() CommandRef { return event.command }

// Stream returns the source-backed output stream identity.
func (event *CommandOutput) Stream() CommandOutputStream { return event.stream }

// Delta returns the command output bytes for this event.
func (event *CommandOutput) Delta() string { return event.delta }

// OrphanReplay reports whether output was replayed without a known command
// snapshot. In this case Command().Known is false and no synthetic id/display is
// exposed.
func (event *CommandOutput) OrphanReplay() bool { return event.orphanReplay }

// Truncated returns source-backed truncation state when the runtime can report
// it. Current wire presence is limited, so false/false means unknown rather than
// guaranteed untruncated.
func (event *CommandOutput) Truncated() (bool, bool) {
	return event.truncated, event.truncated
}

// Warning is a chat/run-owned runtime warning. Optional fields only report true
// in the second return value when they are source-backed by the gateway event.
type Warning struct {
	baseEvent
	code           string
	message        string
	requestType    string
	autoResolution string
	limitReason    string
}

// Code returns the optional source-backed warning code.
func (event *Warning) Code() (string, bool) {
	return event.code, event.code != ""
}

// Message returns the warning message.
func (event *Warning) Message() string { return event.message }

// RequestType returns the optional source-backed request type.
func (event *Warning) RequestType() (string, bool) {
	return event.requestType, event.requestType != ""
}

// AutoResolution returns the optional source-backed automatic resolution.
func (event *Warning) AutoResolution() (string, bool) {
	return event.autoResolution, event.autoResolution != ""
}

// LimitReason returns the optional source-backed limit or policy reason.
func (event *Warning) LimitReason() (string, bool) {
	return event.limitReason, event.limitReason != ""
}

func decodeCommandStarted(stream *EventStream, meta EventMeta, envelope *pb.StreamChatEventsResponse, started *pb.CommandStartedEvent) StreamEvent {
	command := CommandRef{
		ID:             started.GetItemId(),
		Display:        started.GetCommandDisplay(),
		WorkspaceLabel: started.GetWorkspaceLabel(),
		Known:          true,
	}
	if stream != nil && command.ID != "" {
		if stream.commandRefs == nil {
			stream.commandRefs = map[string]CommandRef{}
		}
		stream.commandRefs[command.ID] = command
	}
	base := newBase(EventKindCommandStarted, meta, envelope, true)
	return &CommandStarted{baseEvent: base, command: command}
}

func decodeCommandOutput(stream *EventStream, meta EventMeta, envelope *pb.StreamChatEventsResponse, output *pb.CommandOutputDeltaEvent) StreamEvent {
	itemID := output.GetItemId()
	var command CommandRef
	if stream != nil && itemID != "" {
		if known, ok := stream.commandRefs[itemID]; ok {
			command = known
		}
	}
	base := newBase(EventKindCommandOutput, meta, envelope, true)
	return &CommandOutput{
		baseEvent:    base,
		command:      command,
		stream:       commandOutputStreamFromProto(output.GetStream()),
		delta:        output.GetDelta(),
		truncated:    output.GetTruncated(),
		orphanReplay: !command.Known,
	}
}

func decodeWarning(meta EventMeta, envelope *pb.StreamChatEventsResponse, warning *pb.GatewayWarningEvent) StreamEvent {
	base := newBase(EventKindWarning, meta, envelope, true)
	return &Warning{
		baseEvent:      base,
		code:           warning.GetCode(),
		message:        warning.GetMessage(),
		requestType:    warning.GetRequestType(),
		autoResolution: warning.GetAutoResolution(),
		limitReason:    warning.GetLimitReason(),
	}
}

func commandOutputStreamFromProto(stream pb.CommandOutputStream) CommandOutputStream {
	switch stream {
	case pb.CommandOutputStream_COMMAND_OUTPUT_STREAM_COMBINED:
		return CommandOutputStreamCombined
	default:
		return CommandOutputStreamUnknown
	}
}
