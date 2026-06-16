package codex

import (
	"context"
	"sync"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
)

type EventStream struct {
	stream    pb.ChatRuntimeService_StreamChatEventsClient
	cancel    context.CancelFunc
	closeOnce sync.Once
}

type StreamOption func(*streamOptions)

type streamOptions struct {
	cursor       isStreamCursor
	subscriberID string
}

type isStreamCursor interface {
	apply(*pb.StreamChatEventsRequest)
}

type fromStartCursor struct{}

func (fromStartCursor) apply(req *pb.StreamChatEventsRequest) {
	req.Cursor = &pb.StreamChatEventsRequest_FromStart{FromStart: &pb.ChatFromStartCursor{}}
}

type afterEventIDCursor uint64

func (cursor afterEventIDCursor) apply(req *pb.StreamChatEventsRequest) {
	req.Cursor = &pb.StreamChatEventsRequest_AfterEventId{AfterEventId: uint64(cursor)}
}

type afterEventCursor string

func (cursor afterEventCursor) apply(req *pb.StreamChatEventsRequest) {
	req.Cursor = &pb.StreamChatEventsRequest_AfterEventCursor{AfterEventCursor: string(cursor)}
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
	if s == nil || s.stream == nil {
		return nil, ErrNilClient
	}
	message, err := s.stream.Recv()
	if err != nil {
		return nil, normalizeError(err)
	}
	return message, nil
}

func (s *EventStream) Close() error {
	if s == nil || s.stream == nil {
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
