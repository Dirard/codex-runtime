package codex

import (
	"context"
	"sync"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
)

type EventStream struct {
	stream         pb.ChatRuntimeService_StreamChatEventsClient
	workflowStream pb.WorkflowRuntimeService_StreamWorkflowChatEventsClient
	cancel         context.CancelFunc
	closeOnce      sync.Once
}

type StreamOption func(*streamOptions)

type streamOptions struct {
	cursor       isStreamCursor
	subscriberID string
}

type isStreamCursor interface {
	apply(*pb.StreamChatEventsRequest)
	applyWorkflow(*pb.StreamWorkflowChatEventsRequest)
}

type fromStartCursor struct{}

func (fromStartCursor) apply(req *pb.StreamChatEventsRequest) {
	req.Cursor = &pb.StreamChatEventsRequest_FromStart{FromStart: &pb.ChatFromStartCursor{}}
}

func (fromStartCursor) applyWorkflow(req *pb.StreamWorkflowChatEventsRequest) {
	req.Cursor = &pb.StreamWorkflowChatEventsRequest_FromStart{FromStart: &pb.ChatFromStartCursor{}}
}

type afterEventIDCursor uint64

func (cursor afterEventIDCursor) apply(req *pb.StreamChatEventsRequest) {
	req.Cursor = &pb.StreamChatEventsRequest_AfterEventId{AfterEventId: uint64(cursor)}
}

func (cursor afterEventIDCursor) applyWorkflow(req *pb.StreamWorkflowChatEventsRequest) {
	req.Cursor = &pb.StreamWorkflowChatEventsRequest_AfterEventId{AfterEventId: uint64(cursor)}
}

type afterEventCursor string

func (cursor afterEventCursor) apply(req *pb.StreamChatEventsRequest) {
	req.Cursor = &pb.StreamChatEventsRequest_AfterEventCursor{AfterEventCursor: string(cursor)}
}

func (cursor afterEventCursor) applyWorkflow(req *pb.StreamWorkflowChatEventsRequest) {
	req.Cursor = &pb.StreamWorkflowChatEventsRequest_AfterEventCursor{AfterEventCursor: string(cursor)}
}

func FromStart() StreamOption {
	return func(opts *streamOptions) {
		opts.cursor = fromStartCursor{}
	}
}

func AfterEventID(eventID uint64) StreamOption {
	return func(opts *streamOptions) {
		opts.cursor = afterEventIDCursor(eventID)
	}
}

func AfterEventCursor(cursor string) StreamOption {
	return func(opts *streamOptions) {
		opts.cursor = afterEventCursor(cursor)
	}
}

func WithClientSubscriberID(id string) StreamOption {
	return func(opts *streamOptions) {
		opts.subscriberID = id
	}
}

func (chat *Chat) GetEventsStream(ctx context.Context, opts ...StreamOption) (*EventStream, error) {
	if err := chat.ready(); err != nil {
		return nil, err
	}
	applied := applyStreamOptions(opts)
	childCtx, cancel := context.WithCancel(ctx)
	rpcCtx, err := chat.client.authenticatedContext(childCtx)
	if err != nil {
		cancel()
		return nil, err
	}
	if chat.workflow != nil {
		if err := chat.workflow.ready(); err != nil {
			cancel()
			return nil, err
		}
		request := &pb.StreamWorkflowChatEventsRequest{
			Workflow:           chat.workflow.selector(),
			ChatId:             chat.ID,
			ClientSubscriberId: applied.subscriberID,
		}
		applied.cursor.applyWorkflow(request)
		stream, err := chat.client.workflow.StreamWorkflowChatEvents(rpcCtx, request, chat.client.callOptions...)
		if err != nil {
			cancel()
			return nil, normalizeError(err)
		}
		return &EventStream{workflowStream: stream, cancel: cancel}, nil
	}
	request := &pb.StreamChatEventsRequest{
		Context:            chat.client.runtimeContext(),
		ChatId:             chat.ID,
		ClientSubscriberId: applied.subscriberID,
	}
	applied.cursor.apply(request)
	stream, err := chat.client.rpc.StreamChatEvents(rpcCtx, request, chat.client.callOptions...)
	if err != nil {
		cancel()
		return nil, normalizeError(err)
	}
	return &EventStream{stream: stream, cancel: cancel}, nil
}

func (s *EventStream) Recv() (*pb.StreamChatEventsResponse, error) {
	if s == nil {
		return nil, ErrNilClient
	}
	if s.workflowStream != nil {
		message, err := s.workflowStream.Recv()
		if err != nil {
			return nil, normalizeError(err)
		}
		return workflowStreamResponseToChat(message), nil
	}
	if s.stream == nil {
		return nil, ErrNilClient
	}
	message, err := s.stream.Recv()
	if err != nil {
		return nil, normalizeError(err)
	}
	return message, nil
}

func (s *EventStream) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
	})
	return nil
}

func applyStreamOptions(opts []StreamOption) streamOptions {
	applied := streamOptions{cursor: fromStartCursor{}}
	for _, opt := range opts {
		if opt != nil {
			opt(&applied)
		}
	}
	return applied
}

func initialStreamOptions(response *pb.StartChatRunResponse, explicit []StreamOption) []StreamOption {
	opts := []StreamOption{AfterEventID(0)}
	if response != nil {
		if response.GetEventCursor() != "" {
			opts[0] = AfterEventCursor(response.GetEventCursor())
		} else {
			opts[0] = AfterEventID(response.GetLastEventId())
		}
	}
	opts = append(opts, explicit...)
	return opts
}

func initialWorkflowStreamOptions(response *pb.StartWorkflowChatRunResponse, explicit []StreamOption) []StreamOption {
	opts := []StreamOption{AfterEventID(0)}
	if response != nil {
		if response.GetEventCursor() != "" {
			opts[0] = AfterEventCursor(response.GetEventCursor())
		} else {
			opts[0] = AfterEventID(response.GetLastEventId())
		}
	}
	opts = append(opts, explicit...)
	return opts
}

func workflowStreamResponseToChat(message *pb.StreamWorkflowChatEventsResponse) *pb.StreamChatEventsResponse {
	if message == nil {
		return nil
	}
	switch payload := message.GetPayload().(type) {
	case *pb.StreamWorkflowChatEventsResponse_Event:
		return &pb.StreamChatEventsResponse{
			Payload: &pb.StreamChatEventsResponse_Event{Event: payload.Event},
		}
	case *pb.StreamWorkflowChatEventsResponse_ReplayNotice:
		return &pb.StreamChatEventsResponse{
			Payload: &pb.StreamChatEventsResponse_ReplayNotice{ReplayNotice: payload.ReplayNotice},
		}
	case *pb.StreamWorkflowChatEventsResponse_Narrowed:
		return &pb.StreamChatEventsResponse{
			Payload: &pb.StreamChatEventsResponse_Narrowed{Narrowed: payload.Narrowed},
		}
	default:
		return &pb.StreamChatEventsResponse{}
	}
}
