package codex

import pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"

type Status = pb.ChatStatus
type History = pb.GetChatHistoryResponse
type Event = pb.StreamChatEventsResponse
type EventPayload = pb.ChatEvent
type PendingRequest = pb.ChatPendingRequest
type PendingResponse = pb.RespondChatPendingResponse
type InterruptResponse = pb.InterruptChatRunResponse
type CapabilitySet = pb.ChatCapabilitySet
