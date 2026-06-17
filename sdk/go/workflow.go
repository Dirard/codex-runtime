package codex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

type Workflow struct {
	Namespace                  string
	ID                         string
	StorageKey                 string
	ActivePackageFingerprint   string
	PendingPackageFingerprint  string
	PreviousPackageFingerprint string
	RestartRequired            bool
	CreatedAtUnixMs            int64
	UpdatedAtUnixMs            int64

	mu           sync.RWMutex
	status       *pb.WorkflowStatus
	capabilities *pb.WorkflowCapabilitySet
	client       *Client
}

type WorkflowStatus struct {
	Namespace                  string
	ID                         string
	ActivePackageFingerprint   string
	PendingPackageFingerprint  string
	PreviousPackageFingerprint string
	RestartRequired            bool
	ProcessEpoch               string
}

type WorkflowOption func(*workflowOptions)

type workflowOptions struct {
	clientRequestID string
	idempotencyKey  string
	allowMCPReload  bool
	packageOptions  []WorkflowPackageOption
}

type WorkflowRestartOption func(*workflowRestartOptions)

type workflowRestartOptions struct {
	force           bool
	clientRequestID string
	idempotencyKey  string
}

func WithWorkflowClientRequestID(id string) WorkflowOption {
	return func(opts *workflowOptions) {
		opts.clientRequestID = id
	}
}

func WithWorkflowIdempotencyKey(key string) WorkflowOption {
	return func(opts *workflowOptions) {
		opts.idempotencyKey = key
	}
}

func WithWorkflowMCPReload(allow bool) WorkflowOption {
	return func(opts *workflowOptions) {
		opts.allowMCPReload = allow
	}
}

func WithWorkflowPackageOptions(packageOptions ...WorkflowPackageOption) WorkflowOption {
	return func(opts *workflowOptions) {
		opts.packageOptions = append(opts.packageOptions, packageOptions...)
	}
}

func WithWorkflowForceRestart(force bool) WorkflowRestartOption {
	return func(opts *workflowRestartOptions) {
		opts.force = force
	}
}

func WithWorkflowRestartClientRequestID(id string) WorkflowRestartOption {
	return func(opts *workflowRestartOptions) {
		opts.clientRequestID = id
	}
}

func WithWorkflowRestartIdempotencyKey(key string) WorkflowRestartOption {
	return func(opts *workflowRestartOptions) {
		opts.idempotencyKey = key
	}
}

func (c *Client) InitWorkflow(ctx context.Context, source WorkflowSource, opts ...WorkflowOption) (*Workflow, error) {
	if c == nil || c.workflow == nil {
		return nil, workflowUnavailableError()
	}
	applied := applyWorkflowOptions(opts)
	if err := ensureWorkflowSideEffectIDs(&applied.clientRequestID, &applied.idempotencyKey, "workflow-init"); err != nil {
		return nil, err
	}
	pkg, err := BuildWorkflowPackage(source, applied.packageOptions...)
	if err != nil {
		return nil, workflowPackageBuildError(err)
	}
	rpcCtx, err := c.authenticatedContext(ctx)
	if err != nil {
		return nil, err
	}
	response, err := c.workflow.InitWorkflow(rpcCtx, &pb.InitWorkflowRequest{
		WorkflowPackage: pkg.Proto(),
		ClientRequestId: applied.clientRequestID,
		IdempotencyKey:  applied.idempotencyKey,
		AllowMcpReload:  applied.allowMCPReload,
	}, c.callOptions...)
	if err != nil {
		return nil, normalizeError(err)
	}
	return workflowFromInitResponse(c, response)
}

func (c *Client) GetWorkflow(ctx context.Context, namespace string, workflowID string) (*Workflow, error) {
	if c == nil || c.workflow == nil {
		return nil, workflowUnavailableError()
	}
	selector, err := newWorkflowSelector(namespace, workflowID)
	if err != nil {
		return nil, err
	}
	rpcCtx, err := c.authenticatedContext(ctx)
	if err != nil {
		return nil, err
	}
	response, err := c.workflow.GetWorkflow(rpcCtx, &pb.GetWorkflowRequest{Workflow: selector}, c.callOptions...)
	if err != nil {
		return nil, normalizeError(err)
	}
	return workflowFromProto(c, response.GetWorkflow(), response.GetStatus())
}

func (w *Workflow) Run(ctx context.Context, prompt string, opts ...RequestOption) (*Chat, *EventStream, error) {
	if err := w.ready(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, nil, invalidWorkflowPromptError(w)
	}
	applied := applyRequestOptions(opts)
	if err := applied.ensureRunIDs(); err != nil {
		return nil, nil, err
	}
	rpcCtx, err := w.client.authenticatedContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	response, err := w.client.workflow.StartWorkflowChatRun(rpcCtx, &pb.StartWorkflowChatRunRequest{
		Workflow:              w.selector(),
		Prompt:                prompt,
		ContextBlocks:         applied.protoContextBlocks(),
		ClientMessageId:       applied.clientMessageID,
		IdempotencyKey:        applied.idempotencyKey,
		UiCorrelationMetadata: copyStringMap(applied.uiCorrelationMetadata),
	}, w.client.callOptions...)
	if err != nil {
		return nil, nil, normalizeError(err)
	}
	chat, err := chatFromWorkflowStartResponse(w, response)
	if err != nil {
		return nil, nil, err
	}
	events, err := chat.GetEventsStream(ctx, initialWorkflowStreamOptions(response, applied.initialStreamOptions)...)
	if err != nil {
		return chat, nil, err
	}
	return chat, events, nil
}

func (w *Workflow) GetChat(ctx context.Context, chatID string) (*Chat, error) {
	if err := w.ready(); err != nil {
		return nil, err
	}
	rpcCtx, err := w.client.authenticatedContext(ctx)
	if err != nil {
		return nil, err
	}
	response, err := w.client.workflow.GetWorkflowChat(rpcCtx, &pb.GetWorkflowChatRequest{
		Workflow: w.selector(),
		ChatId:   chatID,
	}, w.client.callOptions...)
	if err != nil {
		return nil, normalizeError(err)
	}
	return chatFromWorkflowGetChatResponse(w, response)
}

func (w *Workflow) GetStatus(ctx context.Context) (WorkflowStatus, error) {
	if err := w.ready(); err != nil {
		return WorkflowStatus{}, err
	}
	rpcCtx, err := w.client.authenticatedContext(ctx)
	if err != nil {
		return WorkflowStatus{}, err
	}
	response, err := w.client.workflow.GetWorkflowStatus(rpcCtx, &pb.GetWorkflowStatusRequest{
		Workflow: w.selector(),
	}, w.client.callOptions...)
	if err != nil {
		return WorkflowStatus{}, normalizeError(err)
	}
	return w.setStatus(response.GetStatus()), nil
}

func (w *Workflow) Restart(ctx context.Context, opts ...WorkflowRestartOption) (WorkflowStatus, error) {
	if err := w.ready(); err != nil {
		return WorkflowStatus{}, err
	}
	applied := applyWorkflowRestartOptions(opts)
	if err := ensureWorkflowSideEffectIDs(&applied.clientRequestID, &applied.idempotencyKey, "workflow-restart"); err != nil {
		return WorkflowStatus{}, err
	}
	rpcCtx, err := w.client.authenticatedContext(ctx)
	if err != nil {
		return WorkflowStatus{}, err
	}
	response, err := w.client.workflow.RestartWorkflow(rpcCtx, &pb.RestartWorkflowRequest{
		Workflow:        w.selector(),
		Force:           applied.force,
		ClientRequestId: applied.clientRequestID,
		IdempotencyKey:  applied.idempotencyKey,
	}, w.client.callOptions...)
	if err != nil {
		return WorkflowStatus{}, normalizeError(err)
	}
	return w.setStatus(response.GetStatus()), nil
}

func (w *Workflow) CachedStatus() WorkflowStatus {
	if w == nil {
		return WorkflowStatus{}
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.snapshotLocked()
}

func (w *Workflow) CachedCapabilities() *pb.WorkflowCapabilitySet {
	if w == nil {
		return nil
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return cloneWorkflowCapabilitySet(w.capabilities)
}

func (w *Workflow) ready() error {
	if w == nil || w.client == nil {
		return ErrNilClient
	}
	if w.client.workflow == nil {
		return workflowUnavailableError()
	}
	if strings.TrimSpace(w.Namespace) == "" || strings.TrimSpace(w.ID) == "" {
		return fmt.Errorf("%w: workflow namespace and id are required", ErrInvalidConfiguration)
	}
	return nil
}

func (w *Workflow) selector() *pb.WorkflowSelector {
	return &pb.WorkflowSelector{Namespace: w.Namespace, WorkflowId: w.ID}
}

func (w *Workflow) setStatus(status *pb.WorkflowStatus) WorkflowStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = cloneWorkflowStatus(status)
	if status != nil {
		w.applyStatusLocked(status)
	}
	return w.snapshotLocked()
}

func (w *Workflow) applyWorkflowLocked(workflow *pb.Workflow) {
	if workflow == nil {
		return
	}
	selector := workflow.GetWorkflow()
	if selector.GetNamespace() != "" {
		w.Namespace = selector.GetNamespace()
	}
	if selector.GetWorkflowId() != "" {
		w.ID = selector.GetWorkflowId()
	}
	w.StorageKey = workflow.GetStorageKey()
	w.ActivePackageFingerprint = workflow.GetActivePackageFingerprint()
	w.PendingPackageFingerprint = workflow.GetPendingPackageFingerprint()
	w.PreviousPackageFingerprint = workflow.GetPreviousPackageFingerprint()
	w.RestartRequired = workflow.GetRestartRequired()
	w.CreatedAtUnixMs = workflow.GetCreatedAtUnixMs()
	w.UpdatedAtUnixMs = workflow.GetUpdatedAtUnixMs()
	w.capabilities = cloneWorkflowCapabilitySet(workflow.GetCapabilities())
}

func (w *Workflow) applyStatusLocked(status *pb.WorkflowStatus) {
	selector := status.GetWorkflow()
	if selector.GetNamespace() != "" {
		w.Namespace = selector.GetNamespace()
	}
	if selector.GetWorkflowId() != "" {
		w.ID = selector.GetWorkflowId()
	}
	w.ActivePackageFingerprint = status.GetActivePackageFingerprint()
	w.PendingPackageFingerprint = status.GetPendingPackageFingerprint()
	w.PreviousPackageFingerprint = status.GetPreviousPackageFingerprint()
	w.RestartRequired = status.GetRestartRequired()
	if status.GetCapabilities() != nil {
		w.capabilities = cloneWorkflowCapabilitySet(status.GetCapabilities())
	}
}

func (w *Workflow) snapshotLocked() WorkflowStatus {
	snapshot := WorkflowStatus{
		Namespace:                  w.Namespace,
		ID:                         w.ID,
		ActivePackageFingerprint:   w.ActivePackageFingerprint,
		PendingPackageFingerprint:  w.PendingPackageFingerprint,
		PreviousPackageFingerprint: w.PreviousPackageFingerprint,
		RestartRequired:            w.RestartRequired,
	}
	if w.status != nil {
		snapshot.ProcessEpoch = w.status.GetProcessEpoch()
	}
	return snapshot
}

func workflowFromInitResponse(client *Client, response *pb.InitWorkflowResponse) (*Workflow, error) {
	if response == nil {
		return nil, fmt.Errorf("%w: init workflow response is nil", ErrInvalidConfiguration)
	}
	return workflowFromProto(client, response.GetWorkflow(), response.GetStatus())
}

func workflowFromProto(client *Client, protoWorkflow *pb.Workflow, status *pb.WorkflowStatus) (*Workflow, error) {
	if protoWorkflow == nil {
		return nil, invalidGatewayResponseError()
	}
	selector := protoWorkflow.GetWorkflow()
	if selector.GetNamespace() == "" || selector.GetWorkflowId() == "" {
		return nil, invalidGatewayResponseError()
	}
	workflow := &Workflow{client: client}
	workflow.mu.Lock()
	workflow.applyWorkflowLocked(protoWorkflow)
	workflow.status = cloneWorkflowStatus(status)
	if status != nil {
		workflow.applyStatusLocked(status)
	}
	workflow.mu.Unlock()
	return workflow, nil
}

func chatFromWorkflowStartResponse(workflow *Workflow, response *pb.StartWorkflowChatRunResponse) (*Chat, error) {
	if response == nil {
		return nil, fmt.Errorf("%w: start workflow chat response is nil", ErrInvalidConfiguration)
	}
	if response.GetChatId() == "" || response.GetRunId() == "" || !response.GetFirstTurnAccepted() {
		return nil, invalidGatewayResponseError()
	}
	return &Chat{
		ID:           response.GetChatId(),
		status:       cloneChatStatus(response.GetStatus()),
		capabilities: cloneCapabilitySet(response.GetCapabilities()),
		client:       workflow.client,
		workflow:     workflow,
	}, nil
}

func chatFromWorkflowGetChatResponse(workflow *Workflow, response *pb.GetWorkflowChatResponse) (*Chat, error) {
	if response == nil || response.GetChat() == nil {
		return nil, fmt.Errorf("%w: get workflow chat response is nil", ErrInvalidConfiguration)
	}
	chat := response.GetChat()
	return &Chat{
		ID:             chat.GetChatId(),
		SessionGroupID: chat.GetSessionGroupId(),
		WorkspaceID:    chat.GetWorkspaceId(),
		status:         cloneChatStatus(response.GetStatus()),
		capabilities:   cloneCapabilitySet(chat.GetCapabilities()),
		client:         workflow.client,
		workflow:       workflow,
	}, nil
}

func workflowHistoryToChatHistory(response *pb.GetWorkflowChatHistoryResponse) *pb.GetChatHistoryResponse {
	if response == nil {
		return nil
	}
	return &pb.GetChatHistoryResponse{
		ChatId:          response.GetChatId(),
		Turns:           append([]*pb.ChatTurnSummary(nil), response.GetTurns()...),
		NextCursor:      response.GetNextCursor(),
		BackwardsCursor: response.GetBackwardsCursor(),
		ReturnedDepth:   response.GetReturnedDepth(),
		Capability:      response.GetCapability(),
		Narrowed:        response.GetNarrowed(),
	}
}

func workflowPendingToChatPending(response *pb.RespondWorkflowChatPendingResponse) *pb.RespondChatPendingResponse {
	if response == nil {
		return nil
	}
	return &pb.RespondChatPendingResponse{
		ChatId:           response.GetChatId(),
		RunId:            response.GetRunId(),
		PendingRequestId: response.GetPendingRequestId(),
		ClientResponseId: response.GetClientResponseId(),
		Accepted:         response.GetAccepted(),
		AlreadyApplied:   response.GetAlreadyApplied(),
		LastEventId:      response.GetLastEventId(),
		Status:           response.GetStatus(),
	}
}

func workflowInterruptToChatInterrupt(response *pb.InterruptWorkflowChatRunResponse) *pb.InterruptChatRunResponse {
	if response == nil {
		return nil
	}
	return &pb.InterruptChatRunResponse{
		ChatId:              response.GetChatId(),
		RunId:               response.GetRunId(),
		Status:              response.GetStatus(),
		InterruptSent:       response.GetInterruptSent(),
		AlreadyInterrupting: response.GetAlreadyInterrupting(),
		AlreadyTerminal:     response.GetAlreadyTerminal(),
		LastEventId:         response.GetLastEventId(),
	}
}

func applyWorkflowPendingResponse(target *pb.RespondWorkflowChatPendingRequest, source *pb.RespondChatPendingRequest) {
	switch typed := source.GetResponse().(type) {
	case *pb.RespondChatPendingRequest_Approval:
		target.Response = &pb.RespondWorkflowChatPendingRequest_Approval{Approval: typed.Approval}
	case *pb.RespondChatPendingRequest_Permissions:
		target.Response = &pb.RespondWorkflowChatPendingRequest_Permissions{Permissions: typed.Permissions}
	case *pb.RespondChatPendingRequest_McpElicitation:
		target.Response = &pb.RespondWorkflowChatPendingRequest_McpElicitation{McpElicitation: typed.McpElicitation}
	case *pb.RespondChatPendingRequest_ToolUserInput:
		target.Response = &pb.RespondWorkflowChatPendingRequest_ToolUserInput{ToolUserInput: typed.ToolUserInput}
	}
}

func applyWorkflowOptions(opts []WorkflowOption) workflowOptions {
	applied := workflowOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&applied)
		}
	}
	return applied
}

func applyWorkflowRestartOptions(opts []WorkflowRestartOption) workflowRestartOptions {
	applied := workflowRestartOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&applied)
		}
	}
	return applied
}

func ensureWorkflowSideEffectIDs(clientRequestID *string, idempotencyKey *string, requestPrefix string) error {
	if strings.TrimSpace(*clientRequestID) == "" {
		id, err := newGeneratedPublicID(requestPrefix)
		if err != nil {
			return err
		}
		*clientRequestID = id
	}
	if strings.TrimSpace(*idempotencyKey) == "" {
		id, err := newGeneratedPublicID("idem")
		if err != nil {
			return err
		}
		*idempotencyKey = id
	}
	return nil
}

func newWorkflowSelector(namespace string, workflowID string) (*pb.WorkflowSelector, error) {
	namespace = strings.TrimSpace(namespace)
	workflowID = strings.TrimSpace(workflowID)
	if namespace == "" || workflowID == "" {
		return nil, fmt.Errorf("%w: workflow namespace and id are required", ErrInvalidConfiguration)
	}
	return &pb.WorkflowSelector{Namespace: namespace, WorkflowId: workflowID}, nil
}

func workflowPackageBuildError(err error) error {
	sdkErr := newWorkflowSDKError(codes.InvalidArgument, WorkflowErrorInvalidWorkflowPackage, "invalid_workflow_package", "invalid workflow package", false)
	sdkErr.err = err
	var packageErr *WorkflowPackageError
	if errors.As(err, &packageErr) {
		sdkErr.Reason = packageErr.Reason
		sdkErr.DisplayMessage = packageErr.Error()
		sdkErr.NextAction = packageErr.NextAction
		if packageErr.DisplayPath != "" {
			sdkErr.SafeMetadata = map[string]string{"display_path": packageErr.DisplayPath}
		}
	}
	return sdkErr
}

func workflowUnavailableError() *Error {
	return newWorkflowSDKError(codes.Unavailable, WorkflowErrorGatewayUnavailable, "workflow_gateway_unavailable", "workflow runtime service is unavailable", true)
}

func invalidWorkflowPromptError(workflow *Workflow) *Error {
	sdkErr := newWorkflowSDKError(codes.InvalidArgument, WorkflowErrorEmptyPrompt, "empty_prompt", "prompt is required", false)
	if workflow != nil {
		sdkErr.WorkflowNamespace = workflow.Namespace
		sdkErr.WorkflowID = workflow.ID
	}
	return sdkErr
}

func cloneWorkflowStatus(status *pb.WorkflowStatus) *pb.WorkflowStatus {
	if status == nil {
		return nil
	}
	return proto.Clone(status).(*pb.WorkflowStatus)
}

func cloneWorkflowCapabilitySet(capabilities *pb.WorkflowCapabilitySet) *pb.WorkflowCapabilitySet {
	if capabilities == nil {
		return nil
	}
	return proto.Clone(capabilities).(*pb.WorkflowCapabilitySet)
}
