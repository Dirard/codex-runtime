package codex

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

const (
	initialStreamSubscribeAttempts = 3
	initialStreamSubscribeDelay    = 200 * time.Millisecond
)

type Chat struct {
	ID             string
	SessionGroupID string
	WorkspaceID    string

	mu           sync.RWMutex
	status       *pb.ChatStatus
	capabilities *pb.ChatCapabilitySet
	client       *Client
	workflow     *Workflow
}

type RunResult struct {
	ChatID         string
	RunID          string
	SessionGroupID string
	WorkspaceID    string
	Status         *pb.ChatStatus
	LastEventID    uint64
	EventCursor    string
	TurnAccepted   bool
}

type ContextBlock struct {
	Kind        pb.ContextBlockKind
	SourceLabel string
	SourceURI   string
	MIMEType    string
	Content     string
}

type RequestOption func(*requestOptions)

type requestOptions struct {
	clientMessageID       string
	clientResponseID      string
	clientRequestID       string
	idempotencyKey        string
	contextBlocks         []ContextBlock
	uiCorrelationMetadata map[string]string
	initialStreamOptions  []StreamOption
}

func WithClientMessageID(id string) RequestOption {
	return func(opts *requestOptions) {
		opts.clientMessageID = id
	}
}

func WithClientResponseID(id string) RequestOption {
	return func(opts *requestOptions) {
		opts.clientResponseID = id
	}
}

func WithClientRequestID(id string) RequestOption {
	return func(opts *requestOptions) {
		opts.clientRequestID = id
	}
}

func WithIdempotencyKey(key string) RequestOption {
	return func(opts *requestOptions) {
		opts.idempotencyKey = key
	}
}

func WithContextBlocks(blocks ...ContextBlock) RequestOption {
	return func(opts *requestOptions) {
		opts.contextBlocks = append(opts.contextBlocks, blocks...)
	}
}

func WithUICorrelationMetadata(values map[string]string) RequestOption {
	return func(opts *requestOptions) {
		if len(values) == 0 {
			return
		}
		if opts.uiCorrelationMetadata == nil {
			opts.uiCorrelationMetadata = make(map[string]string, len(values))
		}
		for key, value := range values {
			opts.uiCorrelationMetadata[key] = value
		}
	}
}

func WithInitialStreamOptions(streamOpts ...StreamOption) RequestOption {
	return func(opts *requestOptions) {
		opts.initialStreamOptions = append(opts.initialStreamOptions, streamOpts...)
	}
}

func applyRequestOptions(opts []RequestOption) requestOptions {
	applied := requestOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&applied)
		}
	}
	return applied
}

func (opts *requestOptions) ensureRunIDs() error {
	if strings.TrimSpace(opts.clientMessageID) == "" {
		id, err := newGeneratedPublicID("msg")
		if err != nil {
			return err
		}
		opts.clientMessageID = id
	}
	if strings.TrimSpace(opts.idempotencyKey) == "" {
		id, err := newGeneratedPublicID("idem")
		if err != nil {
			return err
		}
		opts.idempotencyKey = id
	}
	return nil
}

func (opts *requestOptions) ensurePendingIDs() error {
	if strings.TrimSpace(opts.clientResponseID) == "" {
		id, err := newGeneratedPublicID("pending-response")
		if err != nil {
			return err
		}
		opts.clientResponseID = id
	}
	if strings.TrimSpace(opts.idempotencyKey) == "" {
		id, err := newGeneratedPublicID("idem")
		if err != nil {
			return err
		}
		opts.idempotencyKey = id
	}
	return nil
}

func (opts *requestOptions) ensureInterruptIDs() error {
	if strings.TrimSpace(opts.clientRequestID) == "" {
		id, err := newGeneratedPublicID("interrupt")
		if err != nil {
			return err
		}
		opts.clientRequestID = id
	}
	if strings.TrimSpace(opts.idempotencyKey) == "" {
		id, err := newGeneratedPublicID("idem")
		if err != nil {
			return err
		}
		opts.idempotencyKey = id
	}
	return nil
}

func (opts requestOptions) protoContextBlocks() []*pb.ContextBlock {
	if len(opts.contextBlocks) == 0 {
		return nil
	}
	blocks := make([]*pb.ContextBlock, 0, len(opts.contextBlocks))
	for _, block := range opts.contextBlocks {
		blocks = append(blocks, &pb.ContextBlock{
			Kind:        block.Kind,
			SourceLabel: block.SourceLabel,
			SourceUri:   block.SourceURI,
			MimeType:    block.MIMEType,
			Content:     block.Content,
		})
	}
	return blocks
}

func (c *Client) Run(ctx context.Context, prompt string, opts ...RequestOption) (*Chat, *EventStream, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, nil, invalidPromptError()
	}
	applied := applyRequestOptions(opts)
	if err := applied.ensureRunIDs(); err != nil {
		return nil, nil, err
	}
	rpcCtx, err := c.authenticatedContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	response, err := c.rpc.StartChatRun(rpcCtx, &pb.StartChatRunRequest{
		Context:               c.runtimeContext(),
		Prompt:                prompt,
		ContextBlocks:         applied.protoContextBlocks(),
		ClientMessageId:       applied.clientMessageID,
		IdempotencyKey:        applied.idempotencyKey,
		UiCorrelationMetadata: copyStringMap(applied.uiCorrelationMetadata),
	}, c.callOptions...)
	if err != nil {
		return nil, nil, normalizeError(err)
	}
	chat, err := chatFromStartResponse(c, response)
	if err != nil {
		return nil, nil, err
	}
	streamOpts := initialStreamOptions(response, applied.initialStreamOptions)
	events, err := chat.getInitialEventsStreamWithRetry(ctx, streamOpts...)
	if err != nil {
		return chat, nil, err
	}
	return chat, events, nil
}

func (chat *Chat) getInitialEventsStreamWithRetry(ctx context.Context, streamOpts ...StreamOption) (*EventStream, error) {
	var lastErr error
	for attempt := 0; attempt < initialStreamSubscribeAttempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(initialStreamSubscribeDelay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return nil, normalizeError(ctx.Err())
			}
		}
		events, err := chat.GetEventsStream(ctx, streamOpts...)
		if err == nil {
			return events, nil
		}
		lastErr = err
		if !retryableInitialStreamSubscribeError(err) {
			break
		}
	}
	return nil, lastErr
}

func retryableInitialStreamSubscribeError(err error) bool {
	sdkErr, ok := AsError(err)
	return ok && sdkErr.Retryable && sdkErr.Reason == "dispatcher_unavailable"
}

func (c *Client) GetChat(ctx context.Context, chatID string) (*Chat, error) {
	rpcCtx, err := c.authenticatedContext(ctx)
	if err != nil {
		return nil, err
	}
	response, err := c.rpc.GetChat(rpcCtx, &pb.GetChatRequest{
		Context: c.runtimeContext(),
		ChatId:  chatID,
	}, c.callOptions...)
	if err != nil {
		return nil, normalizeError(err)
	}
	return chatFromGetChatResponse(c, response)
}

func (chat *Chat) Run(ctx context.Context, prompt string, opts ...RequestOption) (*RunResult, error) {
	if err := chat.ready(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(prompt) == "" {
		if chat.workflow != nil {
			return nil, invalidWorkflowPromptError(chat.workflow)
		}
		return nil, invalidPromptError()
	}
	applied := applyRequestOptions(opts)
	if err := applied.ensureRunIDs(); err != nil {
		return nil, err
	}
	if chat.workflow != nil {
		return chat.runWorkflowTurn(ctx, prompt, applied)
	}
	rpcCtx, err := chat.client.authenticatedContext(ctx)
	if err != nil {
		return nil, err
	}
	response, err := chat.client.rpc.RunChatTurn(rpcCtx, &pb.RunChatTurnRequest{
		Context:         chat.client.runtimeContext(),
		ChatId:          chat.ID,
		Prompt:          prompt,
		ContextBlocks:   applied.protoContextBlocks(),
		ClientMessageId: applied.clientMessageID,
		IdempotencyKey:  applied.idempotencyKey,
	}, chat.client.callOptions...)
	if err != nil {
		return nil, normalizeError(err)
	}
	if response.GetChatId() != chat.ID || response.GetRunId() == "" || !response.GetTurnAccepted() {
		return nil, invalidGatewayResponseError()
	}
	result := &RunResult{
		ChatID:         response.GetChatId(),
		RunID:          response.GetRunId(),
		SessionGroupID: response.GetSessionGroupId(),
		WorkspaceID:    response.GetWorkspaceId(),
		Status:         response.GetStatus(),
		LastEventID:    response.GetLastEventId(),
		EventCursor:    response.GetEventCursor(),
		TurnAccepted:   response.GetTurnAccepted(),
	}
	chat.setStatus(result.Status)
	return result, nil
}

func (chat *Chat) runWorkflowTurn(ctx context.Context, prompt string, applied requestOptions) (*RunResult, error) {
	if err := chat.workflow.ready(); err != nil {
		return nil, err
	}
	rpcCtx, err := chat.client.authenticatedContext(ctx)
	if err != nil {
		return nil, err
	}
	response, err := chat.client.workflow.RunWorkflowChatTurn(rpcCtx, &pb.RunWorkflowChatTurnRequest{
		Workflow:        chat.workflow.selector(),
		ChatId:          chat.ID,
		Prompt:          prompt,
		ContextBlocks:   applied.protoContextBlocks(),
		ClientMessageId: applied.clientMessageID,
		IdempotencyKey:  applied.idempotencyKey,
	}, chat.client.callOptions...)
	if err != nil {
		return nil, normalizeError(err)
	}
	if response.GetChatId() != chat.ID || response.GetRunId() == "" || !response.GetTurnAccepted() {
		return nil, invalidGatewayResponseError()
	}
	result := &RunResult{
		ChatID:       response.GetChatId(),
		RunID:        response.GetRunId(),
		Status:       response.GetStatus(),
		LastEventID:  response.GetLastEventId(),
		EventCursor:  response.GetEventCursor(),
		TurnAccepted: response.GetTurnAccepted(),
	}
	chat.setStatus(result.Status)
	return result, nil
}

func (chat *Chat) GetStatus(ctx context.Context) (*pb.ChatStatus, error) {
	if err := chat.ready(); err != nil {
		return nil, err
	}
	rpcCtx, err := chat.client.authenticatedContext(ctx)
	if err != nil {
		return nil, err
	}
	if chat.workflow != nil {
		if err := chat.workflow.ready(); err != nil {
			return nil, err
		}
		response, err := chat.client.workflow.GetWorkflowChat(rpcCtx, &pb.GetWorkflowChatRequest{
			Workflow: chat.workflow.selector(),
			ChatId:   chat.ID,
		}, chat.client.callOptions...)
		if err != nil {
			return nil, normalizeError(err)
		}
		if response.GetChat() != nil {
			chat.mu.Lock()
			chat.SessionGroupID = response.GetChat().GetSessionGroupId()
			chat.WorkspaceID = response.GetChat().GetWorkspaceId()
			chat.capabilities = cloneCapabilitySet(response.GetChat().GetCapabilities())
			chat.mu.Unlock()
		}
		return chat.setStatus(response.GetStatus()), nil
	}
	response, err := chat.client.rpc.GetChatStatus(rpcCtx, &pb.GetChatStatusRequest{
		Context: chat.client.runtimeContext(),
		ChatId:  chat.ID,
	}, chat.client.callOptions...)
	if err != nil {
		return nil, normalizeError(err)
	}
	return chat.setStatus(response.GetStatus()), nil
}

func (chat *Chat) GetHistory(ctx context.Context, opts ...HistoryOption) (*pb.GetChatHistoryResponse, error) {
	if err := chat.ready(); err != nil {
		return nil, err
	}
	applied := applyHistoryOptions(opts)
	rpcCtx, err := chat.client.authenticatedContext(ctx)
	if err != nil {
		return nil, err
	}
	if chat.workflow != nil {
		response, err := chat.client.workflow.GetWorkflowChatHistory(rpcCtx, &pb.GetWorkflowChatHistoryRequest{
			Workflow:       chat.workflow.selector(),
			ChatId:         chat.ID,
			RequestedDepth: applied.depth,
			Cursor:         applied.cursor,
			Limit:          applied.limit,
			SortDirection:  applied.sortDirection,
		}, chat.client.callOptions...)
		if err != nil {
			return nil, normalizeError(err)
		}
		return workflowHistoryToChatHistory(response), nil
	}
	response, err := chat.client.rpc.GetChatHistory(rpcCtx, &pb.GetChatHistoryRequest{
		Context:        chat.client.runtimeContext(),
		ChatId:         chat.ID,
		RequestedDepth: applied.depth,
		Cursor:         applied.cursor,
		Limit:          applied.limit,
		SortDirection:  applied.sortDirection,
	}, chat.client.callOptions...)
	if err != nil {
		return nil, normalizeError(err)
	}
	return response, nil
}

func (chat *Chat) ready() error {
	if chat == nil || chat.client == nil {
		return ErrNilClient
	}
	if chat.ID == "" {
		return fmt.Errorf("%w: chat id is required", ErrInvalidConfiguration)
	}
	return nil
}

func chatFromStartResponse(client *Client, response *pb.StartChatRunResponse) (*Chat, error) {
	if response == nil {
		return nil, fmt.Errorf("%w: start chat response is nil", ErrInvalidConfiguration)
	}
	if response.GetChatId() == "" || response.GetRunId() == "" || !response.GetFirstTurnAccepted() {
		return nil, invalidGatewayResponseError()
	}
	return &Chat{
		ID:             response.GetChatId(),
		SessionGroupID: response.GetSessionGroupId(),
		WorkspaceID:    response.GetWorkspaceId(),
		status:         cloneChatStatus(response.GetStatus()),
		capabilities:   cloneCapabilitySet(response.GetCapabilities()),
		client:         client,
	}, nil
}

func chatFromGetChatResponse(client *Client, response *pb.GetChatResponse) (*Chat, error) {
	if response == nil || response.GetChat() == nil {
		return nil, fmt.Errorf("%w: get chat response is nil", ErrInvalidConfiguration)
	}
	chat := response.GetChat()
	return &Chat{
		ID:             chat.GetChatId(),
		SessionGroupID: chat.GetSessionGroupId(),
		WorkspaceID:    chat.GetWorkspaceId(),
		status:         cloneChatStatus(response.GetStatus()),
		capabilities:   cloneCapabilitySet(chat.GetCapabilities()),
		client:         client,
	}, nil
}

func (chat *Chat) CachedStatus() *pb.ChatStatus {
	if chat == nil {
		return nil
	}
	chat.mu.RLock()
	defer chat.mu.RUnlock()
	return cloneChatStatus(chat.status)
}

func (chat *Chat) CachedCapabilities() *pb.ChatCapabilitySet {
	if chat == nil {
		return nil
	}
	chat.mu.RLock()
	defer chat.mu.RUnlock()
	return cloneCapabilitySet(chat.capabilities)
}

func (chat *Chat) setStatus(status *pb.ChatStatus) *pb.ChatStatus {
	cloned := cloneChatStatus(status)
	chat.mu.Lock()
	chat.status = cloned
	chat.mu.Unlock()
	return cloneChatStatus(cloned)
}

func cloneChatStatus(status *pb.ChatStatus) *pb.ChatStatus {
	if status == nil {
		return nil
	}
	return proto.Clone(status).(*pb.ChatStatus)
}

func cloneCapabilitySet(capabilities *pb.ChatCapabilitySet) *pb.ChatCapabilitySet {
	if capabilities == nil {
		return nil
	}
	return proto.Clone(capabilities).(*pb.ChatCapabilitySet)
}

func invalidPromptError() *Error {
	return newSDKError(codes.InvalidArgument, "invalid_prompt", "prompt is required", false)
}

func invalidGatewayResponseError() *Error {
	return newSDKError(codes.Internal, "invalid_gateway_response", "invalid gateway response", false)
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}
