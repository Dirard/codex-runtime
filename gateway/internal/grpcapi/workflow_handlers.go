package grpcapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	"github.com/Dirard/codex-runtime/gateway/internal/workflowstorage"
	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const workflowRuntimeMessageLimitBytes = 16 * 1024 * 1024

type WorkflowRuntimeServiceOptions struct {
	Enabled     bool
	Storage     *workflowstorage.Manager
	Runtime     ChatRuntimeService
	Launcher    WorkflowRuntimeLauncher
	AllowMCP    bool
	MCPReloader WorkflowMCPReloader
}

type WorkflowRuntimeLauncher interface {
	EnsureWorkflowRuntime(context.Context, WorkflowRuntimeLaunch) error
	CloseWorkflowRuntime(context.Context, WorkflowRuntimeLaunch) error
}

type WorkflowRuntimeLaunch struct {
	Namespace      string
	WorkflowID     string
	StorageKey     string
	Root           string
	SessionGroupID string
	WorkspaceID    string
	RuntimeHome    string
	CWD            string
	ProcessEpoch   string
}

type WorkflowMCPReloader interface {
	ReloadWorkflowMCP(context.Context, WorkflowMCPReloadCommand) error
}

type WorkflowMCPReloadCommand struct {
	Namespace  string
	WorkflowID string
	StorageKey string
	Root       string
}

type workflowRuntimeService struct {
	pb.UnimplementedWorkflowRuntimeServiceServer

	enabled     bool
	storage     *workflowstorage.Manager
	runtime     ChatRuntimeService
	launcher    WorkflowRuntimeLauncher
	allowMCP    bool
	mcpReloader WorkflowMCPReloader

	mu      sync.Mutex
	locks   map[string]*sync.Mutex
	records map[string]*workflowRuntimeRecord
}

type workflowRuntimeRecord struct {
	namespace                  string
	workflowID                 string
	storageKey                 string
	root                       string
	activePackageFingerprint   string
	pendingPackageFingerprint  string
	previousPackageFingerprint string
	restartRequired            bool
	mcpReloadState             pb.WorkflowMcpReloadState
	sessionGroupID             string
	workspaceID                string
	runtimeHome                string
	cwd                        string
	processEpoch               string
	runtimeStarts              int
	activeRuns                 map[string]workflowActiveRun
	lastError                  *pb.WorkflowErrorDetails
	createdAtUnixMS            int64
	updatedAtUnixMS            int64
}

type workflowActiveRun struct {
	chatID    string
	runID     string
	lifecycle pb.ChatTurnLifecycle
}

type workflowUpdateClass int

const (
	workflowUpdateRestartRequired workflowUpdateClass = iota
	workflowUpdateHot
	workflowUpdateMCPOnly
)

func newWorkflowRuntimeService() pb.WorkflowRuntimeServiceServer {
	return NewWorkflowRuntimeService(WorkflowRuntimeServiceOptions{})
}

func NewWorkflowRuntimeService(options WorkflowRuntimeServiceOptions) pb.WorkflowRuntimeServiceServer {
	return &workflowRuntimeService{
		enabled:     options.Enabled,
		storage:     options.Storage,
		runtime:     options.Runtime,
		launcher:    options.Launcher,
		allowMCP:    options.AllowMCP,
		mcpReloader: options.MCPReloader,
		locks:       map[string]*sync.Mutex{},
		records:     map[string]*workflowRuntimeRecord{},
	}
}

func (s *workflowRuntimeService) InitWorkflow(ctx context.Context, req *pb.InitWorkflowRequest) (*pb.InitWorkflowResponse, error) {
	if err := s.requireConfigured(req.GetWorkflowPackage().GetWorkflow()); err != nil {
		return nil, err
	}
	pkg, err := s.storage.Validate(req.GetWorkflowPackage())
	selector := workflowSelectorFromPackage(pkg, req.GetWorkflowPackage())
	if err != nil {
		statusErr := workflowPackageStatusError(err, selector)
		s.recordWorkflowError(selector, statusErr)
		return nil, statusErr
	}
	hasMCP := workflowPackageDeclaresMCP(pkg)
	if hasMCP {
		if !s.allowMCP {
			statusErr := newWorkflowStatusError(codes.FailedPrecondition, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_MCP_UNAVAILABLE, "mcp_reload_unsupported", "workflow MCP servers are disabled for this runtime phase", "remove mcp_servers from the workflow package or wait for MCP reload support", selector, "", "", map[string]string{"phase": "5"})
			s.recordWorkflowError(selector, statusErr)
			return nil, statusErr
		}
		if err := validateWorkflowMCPPolicy(pkg, selector); err != nil {
			s.recordWorkflowError(selector, err)
			return nil, err
		}
	}

	key := workflowstorage.SafeStorageKey(pkg.Namespace, pkg.WorkflowID)
	lock := s.lockFor(key)
	lock.Lock()
	defer lock.Unlock()

	if existing, ok := s.recordByKey(key); ok && existing.activePackageFingerprint == pkg.PackageFingerprint {
		if err := s.ensureRuntime(ctx, existing); err != nil {
			return nil, err
		}
		return &pb.InitWorkflowResponse{
			Workflow:        workflowFromRecord(existing),
			Status:          workflowStatusFromRecord(existing),
			NoOp:            true,
			RestartRequired: existing.restartRequired,
		}, nil
	}

	previous, hadPrevious := s.recordByKey(key)
	if hadPrevious {
		classification, err := classifyWorkflowUpdate(previous, pkg)
		if err != nil {
			s.recordWorkflowError(selector, err)
			return nil, err
		}
		switch classification {
		case workflowUpdateHot:
			stored, err := s.storage.Materialize(ctx, req.GetWorkflowPackage())
			if err != nil {
				statusErr := workflowPackageStatusError(err, selector)
				s.recordWorkflowError(selector, statusErr)
				return nil, statusErr
			}
			record := workflowRuntimeRecordFromStorage(stored, previous.runtimeStarts)
			record.activeRuns = cloneWorkflowActiveRuns(previous.activeRuns)
			record.processEpoch = previous.processEpoch
			record.mcpReloadState = previous.mcpReloadState
			s.putRecord(record)
			if err := s.ensureRuntime(ctx, record); err != nil {
				return nil, err
			}
			return &pb.InitWorkflowResponse{
				Workflow:        workflowFromRecord(record),
				Status:          workflowStatusFromRecord(record),
				Updated:         true,
				RestartRequired: false,
			}, nil
		case workflowUpdateMCPOnly:
			if s.mcpReloader != nil && req.GetAllowMcpReload() {
				staged, err := s.storage.Stage(ctx, req.GetWorkflowPackage())
				if err != nil {
					statusErr := workflowPackageStatusError(err, selector)
					s.recordWorkflowError(selector, statusErr)
					return nil, statusErr
				}
				if err := s.mcpReloader.ReloadWorkflowMCP(ctx, WorkflowMCPReloadCommand{
					Namespace:  staged.Metadata.Namespace,
					WorkflowID: staged.Metadata.WorkflowID,
					StorageKey: staged.StorageKey,
					Root:       staged.Root,
				}); err != nil {
					statusErr := workflowMCPReloadFailedStatusError(err, selector)
					s.recordWorkflowError(selector, statusErr)
					return nil, statusErr
				}
				stored, err := s.storage.PromotePending(ctx, previous.namespace, previous.workflowID)
				if err != nil {
					statusErr := workflowPackageStatusError(err, selector)
					s.recordWorkflowError(selector, statusErr)
					return nil, statusErr
				}
				record := workflowRuntimeRecordFromStorage(stored, previous.runtimeStarts)
				record.activeRuns = cloneWorkflowActiveRuns(previous.activeRuns)
				record.processEpoch = previous.processEpoch
				record.mcpReloadState = pb.WorkflowMcpReloadState_WORKFLOW_MCP_RELOAD_STATE_APPLIED
				s.putRecord(record)
				if err := s.ensureRuntime(ctx, record); err != nil {
					return nil, err
				}
				return &pb.InitWorkflowResponse{
					Workflow:         workflowFromRecord(record),
					Status:           workflowStatusFromRecord(record),
					Updated:          true,
					McpReloadApplied: true,
				}, nil
			}
			stored, err := s.storage.Stage(ctx, req.GetWorkflowPackage())
			if err != nil {
				statusErr := workflowPackageStatusError(err, selector)
				s.recordWorkflowError(selector, statusErr)
				return nil, statusErr
			}
			record := workflowRuntimeRecordFromStorage(stored, previous.runtimeStarts)
			record.activeRuns = cloneWorkflowActiveRuns(previous.activeRuns)
			record.processEpoch = previous.processEpoch
			record.restartRequired = true
			record.mcpReloadState = pb.WorkflowMcpReloadState_WORKFLOW_MCP_RELOAD_STATE_UNSUPPORTED
			s.putRecord(record)
			return &pb.InitWorkflowResponse{
				Workflow:        workflowFromRecord(record),
				Status:          workflowStatusFromRecord(record),
				Updated:         true,
				RestartRequired: true,
			}, nil
		default:
			stored, err := s.storage.Stage(ctx, req.GetWorkflowPackage())
			if err != nil {
				statusErr := workflowPackageStatusError(err, selector)
				s.recordWorkflowError(selector, statusErr)
				return nil, statusErr
			}
			record := workflowRuntimeRecordFromStorage(stored, previous.runtimeStarts)
			record.activeRuns = cloneWorkflowActiveRuns(previous.activeRuns)
			record.processEpoch = previous.processEpoch
			record.restartRequired = true
			s.putRecord(record)
			return &pb.InitWorkflowResponse{
				Workflow:        workflowFromRecord(record),
				Status:          workflowStatusFromRecord(record),
				Updated:         true,
				RestartRequired: true,
			}, nil
		}
	}

	stored, err := s.storage.Materialize(ctx, req.GetWorkflowPackage())
	if err != nil {
		statusErr := workflowPackageStatusError(err, selector)
		s.recordWorkflowError(selector, statusErr)
		return nil, statusErr
	}
	runtimeStarts := 1
	record := workflowRuntimeRecordFromStorage(stored, runtimeStarts)
	s.putRecord(record)
	if err := s.ensureRuntime(ctx, record); err != nil {
		return nil, err
	}

	return &pb.InitWorkflowResponse{
		Workflow:         workflowFromRecord(record),
		Status:           workflowStatusFromRecord(record),
		Created:          true,
		RestartRequired:  false,
		McpReloadApplied: false,
	}, nil
}

func (s *workflowRuntimeService) GetWorkflow(_ context.Context, req *pb.GetWorkflowRequest) (*pb.GetWorkflowResponse, error) {
	record, err := s.requireRecord(req.GetWorkflow())
	if err != nil {
		return nil, err
	}
	return &pb.GetWorkflowResponse{
		Workflow: workflowFromRecord(record),
		Status:   workflowStatusFromRecord(record),
	}, nil
}

func (s *workflowRuntimeService) GetWorkflowStatus(_ context.Context, req *pb.GetWorkflowStatusRequest) (*pb.GetWorkflowStatusResponse, error) {
	record, err := s.requireRecord(req.GetWorkflow())
	if err != nil {
		return nil, err
	}
	return &pb.GetWorkflowStatusResponse{Status: workflowStatusFromRecord(record)}, nil
}

func (s *workflowRuntimeService) RestartWorkflow(ctx context.Context, req *pb.RestartWorkflowRequest) (*pb.RestartWorkflowResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	record, err := s.requireRecord(req.GetWorkflow())
	if err != nil {
		return nil, err
	}
	lock := s.lockFor(record.storageKey)
	lock.Lock()
	defer lock.Unlock()

	current, ok := s.recordByKey(record.storageKey)
	if !ok {
		return nil, workflowNotFoundStatusError(req.GetWorkflow())
	}
	activeInterrupted := false
	if req.GetForce() {
		if err := s.ensureRuntime(ctx, current); err != nil {
			return nil, err
		}
		for _, active := range current.activeRunSnapshot() {
			_, err := s.runtime.InterruptChatRun(ctx, domain.InterruptChatRunCommand{
				SessionGroupID:  current.sessionGroupID,
				WorkspaceID:     current.workspaceID,
				ChatID:          active.chatID,
				RunID:           active.runID,
				ClientRequestID: "workflow-restart-" + current.processEpoch,
				IdempotencyKey:  "workflow-restart-" + current.processEpoch + "-" + active.runID,
			})
			if err != nil {
				return nil, workflowRuntimeStatusError(err, current.selector(), active.chatID)
			}
			activeInterrupted = true
		}
		current.activeRuns = map[string]workflowActiveRun{}
	} else {
		for current.hasActiveRuns() {
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, workflowGracefulRestartDeadlineStatusError(current.selector())
			case <-timer.C:
			}
			refreshed, ok := s.recordByKey(record.storageKey)
			if !ok {
				return nil, workflowNotFoundStatusError(req.GetWorkflow())
			}
			current = refreshed
		}
	}

	var updated *workflowRuntimeRecord
	if current.pendingPackageFingerprint != "" {
		stored, err := s.storage.PromotePending(ctx, current.namespace, current.workflowID)
		if err != nil {
			return nil, workflowPackageStatusError(err, current.selector())
		}
		updated = workflowRuntimeRecordFromStorage(stored, current.runtimeStarts+1)
	} else {
		copy := *current
		updated = &copy
		updated.runtimeStarts++
		updated.processEpoch = workflowProcessEpoch(updated.storageKey, updated.runtimeStarts)
		updated.updatedAtUnixMS = time.Now().UnixMilli()
	}
	updated.restartRequired = false
	updated.pendingPackageFingerprint = ""
	updated.activeRuns = map[string]workflowActiveRun{}
	updated.mcpReloadState = pb.WorkflowMcpReloadState_WORKFLOW_MCP_RELOAD_STATE_NOT_REQUESTED
	s.putRecord(updated)
	if err := s.ensureRuntime(ctx, updated); err != nil {
		return nil, err
	}

	return &pb.RestartWorkflowResponse{
		Status:                workflowStatusFromRecord(updated),
		RestartStarted:        true,
		RestartCompleted:      true,
		ActiveWorkInterrupted: activeInterrupted,
	}, nil
}

func (s *workflowRuntimeService) DeleteWorkflow(ctx context.Context, req *pb.DeleteWorkflowRequest) (*pb.DeleteWorkflowResponse, error) {
	record, err := s.requireRecord(req.GetWorkflow())
	if err != nil {
		return nil, err
	}
	lock := s.lockFor(record.storageKey)
	lock.Lock()
	defer lock.Unlock()

	if current, ok := s.recordByKey(record.storageKey); ok {
		record = current
	}
	if record.hasActiveRuns() && !req.GetForce() {
		return nil, workflowActiveWorkRefusedStatusError(record.selector())
	}
	if req.GetForce() {
		if err := s.ensureRuntime(ctx, record); err != nil {
			return nil, err
		}
		for _, active := range record.activeRunSnapshot() {
			_, err := s.runtime.InterruptChatRun(ctx, domain.InterruptChatRunCommand{
				SessionGroupID:  record.sessionGroupID,
				WorkspaceID:     record.workspaceID,
				ChatID:          active.chatID,
				RunID:           active.runID,
				ClientRequestID: "workflow-delete-" + record.processEpoch,
				IdempotencyKey:  "workflow-delete-" + record.processEpoch + "-" + active.runID,
			})
			if err != nil {
				return nil, workflowRuntimeStatusError(err, record.selector(), active.chatID)
			}
		}
	}
	if req.GetCleanupRuntime() {
		if err := s.closeRuntime(ctx, record); err != nil {
			return nil, err
		}
	}
	if req.GetDeleteMaterializedState() {
		if err := s.storage.Delete(ctx, record.namespace, record.workflowID, record.hasActiveRuns(), req.GetForce()); err != nil {
			return nil, workflowPackageStatusError(err, record.selector())
		}
	}
	s.deleteRecord(record.storageKey)
	status := workflowStatusFromRecord(record)
	status.Lifecycle = pb.WorkflowLifecycle_WORKFLOW_LIFECYCLE_NOT_FOUND
	return &pb.DeleteWorkflowResponse{
		Status:           status,
		Deleted:          true,
		CleanupScheduled: req.GetCleanupRuntime(),
	}, nil
}

func (s *workflowRuntimeService) StartWorkflowChatRun(ctx context.Context, req *pb.StartWorkflowChatRunRequest) (*pb.StartWorkflowChatRunResponse, error) {
	record, err := s.requireRecord(req.GetWorkflow())
	if err != nil {
		return nil, err
	}
	if record.restartRequired {
		return nil, workflowRestartRequiredStatusError(record.selector(), "")
	}
	if err := s.ensureRuntime(ctx, record); err != nil {
		return nil, err
	}
	command, err := s.startChatRunCommand(record, req)
	if err != nil {
		return nil, err
	}
	response, err := s.runtime.StartChatRun(ctx, command)
	if err != nil {
		return nil, workflowRuntimeStatusError(err, record.selector(), "")
	}
	chatResponse, err := startChatRunResponseToProto(response)
	if err != nil {
		return nil, err
	}
	s.markWorkflowActive(record.storageKey, chatResponse.GetChatId(), chatResponse.GetRunId(), pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_IN_PROGRESS)
	return &pb.StartWorkflowChatRunResponse{
		Workflow:          record.selector(),
		ChatId:            chatResponse.GetChatId(),
		RunId:             chatResponse.GetRunId(),
		Status:            chatResponse.GetStatus(),
		LastEventId:       chatResponse.GetLastEventId(),
		EventCursor:       workflowEventCursor(record.processEpoch, chatResponse.GetEventCursor()),
		FirstTurnAccepted: chatResponse.GetFirstTurnAccepted(),
		Capabilities:      chatResponse.GetCapabilities(),
	}, nil
}

func (s *workflowRuntimeService) RunWorkflowChatTurn(ctx context.Context, req *pb.RunWorkflowChatTurnRequest) (*pb.RunWorkflowChatTurnResponse, error) {
	record, err := s.requireRecord(req.GetWorkflow())
	if err != nil {
		return nil, err
	}
	if record.restartRequired {
		return nil, workflowRestartRequiredStatusError(record.selector(), req.GetChatId())
	}
	if err := s.ensureRuntime(ctx, record); err != nil {
		return nil, err
	}
	command, err := s.runChatTurnCommand(record, req)
	if err != nil {
		return nil, err
	}
	response, err := s.runtime.RunChatTurn(ctx, command)
	if err != nil {
		return nil, workflowRuntimeStatusError(err, record.selector(), req.GetChatId())
	}
	chatResponse, err := runChatTurnResponseToProto(response)
	if err != nil {
		return nil, err
	}
	s.updateWorkflowActiveFromStatus(record.storageKey, chatResponse.GetStatus())
	return &pb.RunWorkflowChatTurnResponse{
		Workflow:     record.selector(),
		ChatId:       chatResponse.GetChatId(),
		RunId:        chatResponse.GetRunId(),
		Status:       chatResponse.GetStatus(),
		LastEventId:  chatResponse.GetLastEventId(),
		EventCursor:  workflowEventCursor(record.processEpoch, chatResponse.GetEventCursor()),
		TurnAccepted: chatResponse.GetTurnAccepted(),
	}, nil
}

func (s *workflowRuntimeService) GetWorkflowChat(ctx context.Context, req *pb.GetWorkflowChatRequest) (*pb.GetWorkflowChatResponse, error) {
	record, err := s.requireRecord(req.GetWorkflow())
	if err != nil {
		return nil, err
	}
	if err := s.ensureRuntime(ctx, record); err != nil {
		return nil, err
	}
	command, err := s.getChatCommand(record, req)
	if err != nil {
		return nil, err
	}
	response, err := s.runtime.GetChat(ctx, command)
	if err != nil {
		return nil, workflowRuntimeStatusError(err, record.selector(), req.GetChatId())
	}
	chatResponse, err := getChatResponseToProto(response)
	if err != nil {
		return nil, err
	}
	return &pb.GetWorkflowChatResponse{
		Workflow: record.selector(),
		Chat:     chatResponse.GetChat(),
		Status:   chatResponse.GetStatus(),
	}, nil
}

func (s *workflowRuntimeService) GetWorkflowChatHistory(ctx context.Context, req *pb.GetWorkflowChatHistoryRequest) (*pb.GetWorkflowChatHistoryResponse, error) {
	record, err := s.requireRecord(req.GetWorkflow())
	if err != nil {
		return nil, err
	}
	if err := s.ensureRuntime(ctx, record); err != nil {
		return nil, err
	}
	command, err := s.getChatHistoryCommand(record, req)
	if err != nil {
		return nil, err
	}
	response, err := s.runtime.GetChatHistory(ctx, command)
	if err != nil {
		return nil, workflowRuntimeStatusError(err, record.selector(), req.GetChatId())
	}
	chatResponse, err := getChatHistoryResponseToProto(response)
	if err != nil {
		return nil, err
	}
	return &pb.GetWorkflowChatHistoryResponse{
		Workflow:        record.selector(),
		ChatId:          chatResponse.GetChatId(),
		Turns:           chatResponse.GetTurns(),
		NextCursor:      chatResponse.GetNextCursor(),
		BackwardsCursor: chatResponse.GetBackwardsCursor(),
		ReturnedDepth:   chatResponse.GetReturnedDepth(),
		Capability:      chatResponse.GetCapability(),
		Narrowed:        chatResponse.GetNarrowed(),
	}, nil
}

func (s *workflowRuntimeService) StreamWorkflowChatEvents(req *pb.StreamWorkflowChatEventsRequest, stream grpc.ServerStreamingServer[pb.StreamWorkflowChatEventsResponse]) error {
	record, err := s.requireRecord(req.GetWorkflow())
	if err != nil {
		return err
	}
	if err := s.ensureRuntime(stream.Context(), record); err != nil {
		return err
	}
	command, err := s.streamChatEventsCommand(record, req)
	if err != nil {
		return err
	}
	eventStream, err := s.runtime.StreamChatEvents(stream.Context(), command)
	if err != nil {
		return workflowRuntimeStatusError(err, record.selector(), req.GetChatId())
	}
	defer eventStream.Close()

	trustedMetadata := record.sessionMetadata()
	for {
		message, err := eventStream.Next(stream.Context())
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return workflowRuntimeStatusError(err, record.selector(), req.GetChatId())
		}
		if message.SessionGroupID == "" {
			message.SessionGroupID = record.sessionGroupID
		}
		chatResponse, err := streamChatEventsMessageToProto(message, trustedMetadata)
		if err != nil {
			return err
		}
		workflowResponse, err := workflowStreamChatEventsResponse(record, chatResponse)
		if err != nil {
			return err
		}
		if event := chatResponse.GetEvent(); event != nil {
			s.updateWorkflowActiveFromStatus(record.storageKey, event.GetStatusUpdated().GetStatus())
			if event.GetTerminal() != nil {
				s.clearWorkflowActive(record.storageKey, event.GetChatId())
			}
		}
		if err := stream.Send(workflowResponse); err != nil {
			return err
		}
	}
}

func (s *workflowRuntimeService) RespondWorkflowChatPending(ctx context.Context, req *pb.RespondWorkflowChatPendingRequest) (*pb.RespondWorkflowChatPendingResponse, error) {
	record, err := s.requireRecord(req.GetWorkflow())
	if err != nil {
		return nil, err
	}
	if err := s.ensureRuntime(ctx, record); err != nil {
		return nil, err
	}
	command, err := s.respondChatPendingCommand(record, req)
	if err != nil {
		return nil, err
	}
	response, err := s.runtime.RespondChatPending(ctx, command)
	if err != nil {
		return nil, workflowRuntimeStatusError(err, record.selector(), req.GetChatId())
	}
	chatResponse, err := respondChatPendingResponseToProto(response)
	if err != nil {
		return nil, err
	}
	s.updateWorkflowActiveFromStatus(record.storageKey, chatResponse.GetStatus())
	return &pb.RespondWorkflowChatPendingResponse{
		Workflow:         record.selector(),
		ChatId:           chatResponse.GetChatId(),
		RunId:            chatResponse.GetRunId(),
		PendingRequestId: chatResponse.GetPendingRequestId(),
		ClientResponseId: chatResponse.GetClientResponseId(),
		Accepted:         chatResponse.GetAccepted(),
		AlreadyApplied:   chatResponse.GetAlreadyApplied(),
		LastEventId:      chatResponse.GetLastEventId(),
		Status:           chatResponse.GetStatus(),
	}, nil
}

func (s *workflowRuntimeService) InterruptWorkflowChatRun(ctx context.Context, req *pb.InterruptWorkflowChatRunRequest) (*pb.InterruptWorkflowChatRunResponse, error) {
	record, err := s.requireRecord(req.GetWorkflow())
	if err != nil {
		return nil, err
	}
	if err := s.ensureRuntime(ctx, record); err != nil {
		return nil, err
	}
	command, err := s.interruptChatRunCommand(record, req)
	if err != nil {
		return nil, err
	}
	response, err := s.runtime.InterruptChatRun(ctx, command)
	if err != nil {
		return nil, workflowRuntimeStatusError(err, record.selector(), req.GetChatId())
	}
	chatResponse, err := interruptChatRunResponseToProto(response)
	if err != nil {
		return nil, err
	}
	if chatResponse.GetAlreadyTerminal() || chatResponse.GetInterruptSent() {
		s.clearWorkflowActive(record.storageKey, chatResponse.GetChatId())
	}
	return &pb.InterruptWorkflowChatRunResponse{
		Workflow:            record.selector(),
		ChatId:              chatResponse.GetChatId(),
		RunId:               chatResponse.GetRunId(),
		Status:              chatResponse.GetStatus(),
		InterruptSent:       chatResponse.GetInterruptSent(),
		AlreadyInterrupting: chatResponse.GetAlreadyInterrupting(),
		AlreadyTerminal:     chatResponse.GetAlreadyTerminal(),
		LastEventId:         chatResponse.GetLastEventId(),
	}, nil
}

func (s *workflowRuntimeService) startChatRunCommand(record *workflowRuntimeRecord, req *pb.StartWorkflowChatRunRequest) (domain.StartChatRunCommand, error) {
	chatReq := &pb.StartChatRunRequest{
		Context:               record.chatRuntimeContext(),
		Prompt:                req.GetPrompt(),
		ContextBlocks:         req.GetContextBlocks(),
		ClientMessageId:       req.GetClientMessageId(),
		IdempotencyKey:        req.GetIdempotencyKey(),
		UiCorrelationMetadata: req.GetUiCorrelationMetadata(),
	}
	command, requestError := ValidateStartChatRun(chatReq, workflowSessionResolver{metadata: record.sessionMetadata()})
	if requestError != nil {
		return domain.StartChatRunCommand{}, workflowRequestStatusError(requestError, record.selector(), "")
	}
	return command, nil
}

func (s *workflowRuntimeService) runChatTurnCommand(record *workflowRuntimeRecord, req *pb.RunWorkflowChatTurnRequest) (domain.RunChatTurnCommand, error) {
	chatReq := &pb.RunChatTurnRequest{
		Context:         record.chatRuntimeContext(),
		ChatId:          req.GetChatId(),
		Prompt:          req.GetPrompt(),
		ContextBlocks:   req.GetContextBlocks(),
		ClientMessageId: req.GetClientMessageId(),
		IdempotencyKey:  req.GetIdempotencyKey(),
	}
	command, requestError := ValidateRunChatTurn(chatReq, workflowSessionResolver{metadata: record.sessionMetadata()})
	if requestError != nil {
		return domain.RunChatTurnCommand{}, workflowRequestStatusError(requestError, record.selector(), req.GetChatId())
	}
	return command, nil
}

func (s *workflowRuntimeService) getChatCommand(record *workflowRuntimeRecord, req *pb.GetWorkflowChatRequest) (domain.GetChatCommand, error) {
	chatReq := &pb.GetChatRequest{
		Context: record.chatRuntimeContext(),
		ChatId:  req.GetChatId(),
	}
	command, requestError := ValidateGetChat(chatReq, workflowSessionResolver{metadata: record.sessionMetadata()})
	if requestError != nil {
		return domain.GetChatCommand{}, workflowRequestStatusError(requestError, record.selector(), req.GetChatId())
	}
	return command, nil
}

func (s *workflowRuntimeService) getChatHistoryCommand(record *workflowRuntimeRecord, req *pb.GetWorkflowChatHistoryRequest) (domain.GetChatHistoryCommand, error) {
	chatReq := &pb.GetChatHistoryRequest{
		Context:        record.chatRuntimeContext(),
		ChatId:         req.GetChatId(),
		RequestedDepth: req.GetRequestedDepth(),
		Cursor:         req.GetCursor(),
		Limit:          req.GetLimit(),
		SortDirection:  req.GetSortDirection(),
	}
	command, requestError := ValidateGetChatHistory(chatReq, workflowSessionResolver{metadata: record.sessionMetadata()})
	if requestError != nil {
		return domain.GetChatHistoryCommand{}, workflowRequestStatusError(requestError, record.selector(), req.GetChatId())
	}
	return command, nil
}

func (s *workflowRuntimeService) streamChatEventsCommand(record *workflowRuntimeRecord, req *pb.StreamWorkflowChatEventsRequest) (domain.StreamChatEventsCommand, error) {
	chatReq := &pb.StreamChatEventsRequest{
		Context:            record.chatRuntimeContext(),
		ChatId:             req.GetChatId(),
		ClientSubscriberId: req.GetClientSubscriberId(),
	}
	switch cursor := req.GetCursor().(type) {
	case *pb.StreamWorkflowChatEventsRequest_FromStart:
		chatReq.Cursor = &pb.StreamChatEventsRequest_FromStart{FromStart: cursor.FromStart}
	case *pb.StreamWorkflowChatEventsRequest_AfterEventId:
		chatReq.Cursor = &pb.StreamChatEventsRequest_AfterEventId{AfterEventId: cursor.AfterEventId}
	case *pb.StreamWorkflowChatEventsRequest_AfterEventCursor:
		rawCursor, err := workflowRawEventCursor(record.processEpoch, cursor.AfterEventCursor, record.selector(), req.GetChatId())
		if err != nil {
			return domain.StreamChatEventsCommand{}, err
		}
		chatReq.Cursor = &pb.StreamChatEventsRequest_AfterEventCursor{AfterEventCursor: rawCursor}
	}
	command, requestError := ValidateStreamChatEvents(chatReq, workflowSessionResolver{metadata: record.sessionMetadata()})
	if requestError != nil {
		return domain.StreamChatEventsCommand{}, workflowRequestStatusError(requestError, record.selector(), req.GetChatId())
	}
	return command, nil
}

func (s *workflowRuntimeService) respondChatPendingCommand(record *workflowRuntimeRecord, req *pb.RespondWorkflowChatPendingRequest) (domain.RespondChatPendingCommand, error) {
	chatReq := &pb.RespondChatPendingRequest{
		Context:          record.chatRuntimeContext(),
		ChatId:           req.GetChatId(),
		PendingRequestId: req.GetPendingRequestId(),
		ClientResponseId: req.GetClientResponseId(),
		IdempotencyKey:   req.GetIdempotencyKey(),
	}
	switch response := req.GetResponse().(type) {
	case *pb.RespondWorkflowChatPendingRequest_Approval:
		chatReq.Response = &pb.RespondChatPendingRequest_Approval{Approval: response.Approval}
	case *pb.RespondWorkflowChatPendingRequest_Permissions:
		chatReq.Response = &pb.RespondChatPendingRequest_Permissions{Permissions: response.Permissions}
	case *pb.RespondWorkflowChatPendingRequest_McpElicitation:
		chatReq.Response = &pb.RespondChatPendingRequest_McpElicitation{McpElicitation: response.McpElicitation}
	case *pb.RespondWorkflowChatPendingRequest_ToolUserInput:
		chatReq.Response = &pb.RespondChatPendingRequest_ToolUserInput{ToolUserInput: response.ToolUserInput}
	}
	command, requestError := ValidateRespondChatPending(chatReq, workflowSessionResolver{metadata: record.sessionMetadata()})
	if requestError != nil {
		return domain.RespondChatPendingCommand{}, workflowRequestStatusError(requestError, record.selector(), req.GetChatId())
	}
	return command, nil
}

func (s *workflowRuntimeService) interruptChatRunCommand(record *workflowRuntimeRecord, req *pb.InterruptWorkflowChatRunRequest) (domain.InterruptChatRunCommand, error) {
	chatReq := &pb.InterruptChatRunRequest{
		Context:         record.chatRuntimeContext(),
		ChatId:          req.GetChatId(),
		RunId:           req.GetRunId(),
		ClientRequestId: req.GetClientRequestId(),
		IdempotencyKey:  req.GetIdempotencyKey(),
	}
	command, requestError := ValidateInterruptChatRun(chatReq, workflowSessionResolver{metadata: record.sessionMetadata()})
	if requestError != nil {
		return domain.InterruptChatRunCommand{}, workflowRequestStatusError(requestError, record.selector(), req.GetChatId())
	}
	return command, nil
}

func (s *workflowRuntimeService) requireConfigured(selector *pb.WorkflowSelector) error {
	if s == nil || !s.enabled || s.storage == nil || s.runtime == nil {
		return newWorkflowStatusError(codes.Unavailable, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_GATEWAY_UNAVAILABLE, "workflow_runtime_unavailable", "workflow runtime is not enabled", "configure workflow storage and chat runtime before using workflows", selector, "", "", nil)
	}
	return nil
}

func (s *workflowRuntimeService) ensureRuntime(ctx context.Context, record *workflowRuntimeRecord) error {
	if s == nil || s.launcher == nil || record == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	launch := workflowRuntimeLaunchFromRecord(record)
	if err := s.launcher.EnsureWorkflowRuntime(ctx, launch); err != nil {
		statusErr := workflowRuntimeStatusError(err, record.selector(), "")
		s.recordWorkflowError(record.selector(), statusErr)
		return statusErr
	}
	return nil
}

func (s *workflowRuntimeService) closeRuntime(ctx context.Context, record *workflowRuntimeRecord) error {
	if s == nil || s.launcher == nil || record == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.launcher.CloseWorkflowRuntime(ctx, workflowRuntimeLaunchFromRecord(record)); err != nil {
		return workflowRuntimeStatusError(err, record.selector(), "")
	}
	return nil
}

func (s *workflowRuntimeService) requireRecord(selector *pb.WorkflowSelector) (*workflowRuntimeRecord, error) {
	if err := s.requireConfigured(selector); err != nil {
		return nil, err
	}
	normalized, err := validateWorkflowSelector(selector)
	if err != nil {
		return nil, err
	}
	key := workflowstorage.SafeStorageKey(normalized.GetNamespace(), normalized.GetWorkflowId())
	record, ok := s.recordByKey(key)
	if !ok {
		return nil, workflowNotFoundStatusError(normalized)
	}
	return record, nil
}

func (s *workflowRuntimeService) lockFor(key string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	lock := s.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		s.locks[key] = lock
	}
	return lock
}

func (s *workflowRuntimeService) recordByKey(key string) (*workflowRuntimeRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[key]
	if !ok {
		return nil, false
	}
	copy := *record
	copy.activeRuns = cloneWorkflowActiveRuns(record.activeRuns)
	copy.lastError = cloneWorkflowErrorDetails(record.lastError)
	return &copy, true
}

func (s *workflowRuntimeService) putRecord(record *workflowRuntimeRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *record
	copy.activeRuns = cloneWorkflowActiveRuns(record.activeRuns)
	copy.lastError = cloneWorkflowErrorDetails(record.lastError)
	s.records[record.storageKey] = &copy
}

func (s *workflowRuntimeService) deleteRecord(storageKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, storageKey)
}

func (s *workflowRuntimeService) markWorkflowActive(storageKey string, chatID string, runID string, lifecycle pb.ChatTurnLifecycle) {
	if chatID == "" || runID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.records[storageKey]
	if record == nil {
		return
	}
	if record.activeRuns == nil {
		record.activeRuns = map[string]workflowActiveRun{}
	}
	record.activeRuns[chatID] = workflowActiveRun{chatID: chatID, runID: runID, lifecycle: lifecycle}
}

func (s *workflowRuntimeService) updateWorkflowActiveFromStatus(storageKey string, status *pb.ChatStatus) {
	if status == nil || status.GetChatId() == "" {
		return
	}
	if workflowChatLifecycleTerminal(status.GetCurrentRunLifecycle()) {
		s.clearWorkflowActive(storageKey, status.GetChatId())
		return
	}
	s.markWorkflowActive(storageKey, status.GetChatId(), status.GetCurrentRunId(), status.GetCurrentRunLifecycle())
}

func (s *workflowRuntimeService) clearWorkflowActive(storageKey string, chatID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.records[storageKey]
	if record == nil || record.activeRuns == nil {
		return
	}
	delete(record.activeRuns, chatID)
}

func (s *workflowRuntimeService) recordWorkflowError(selector *pb.WorkflowSelector, err error) {
	details := workflowDetailsFromError(err)
	if details == nil {
		return
	}
	normalized, normalizeErr := validateWorkflowSelector(selector)
	if normalizeErr != nil {
		return
	}
	key := workflowstorage.SafeStorageKey(normalized.GetNamespace(), normalized.GetWorkflowId())
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.records[key]
	if record == nil {
		return
	}
	record.lastError = cloneWorkflowErrorDetails(details)
}

func validateWorkflowSelector(selector *pb.WorkflowSelector) (*pb.WorkflowSelector, error) {
	namespace, requestError := validateRequiredPublicID(selector.GetNamespace(), "workflow.namespace", domain.ReasonInvalidLocator)
	if requestError != nil {
		return nil, workflowRequestStatusError(requestError, selector, "")
	}
	workflowID, requestError := validateRequiredPublicID(selector.GetWorkflowId(), "workflow.workflow_id", domain.ReasonInvalidLocator)
	if requestError != nil {
		return nil, workflowRequestStatusError(requestError, selector, "")
	}
	return &pb.WorkflowSelector{Namespace: namespace, WorkflowId: workflowID}, nil
}

func workflowRuntimeRecordFromStorage(stored *workflowstorage.Record, runtimeStarts int) *workflowRuntimeRecord {
	hash := workflowShortHash(stored.StorageKey)
	return &workflowRuntimeRecord{
		namespace:                  stored.Metadata.Namespace,
		workflowID:                 stored.Metadata.WorkflowID,
		storageKey:                 stored.StorageKey,
		root:                       stored.Root,
		activePackageFingerprint:   stored.Metadata.ActivePackageFingerprint,
		pendingPackageFingerprint:  stored.Metadata.PendingPackageFingerprint,
		previousPackageFingerprint: stored.Metadata.PreviousPackageFingerprint,
		restartRequired:            stored.Metadata.RestartRequired,
		mcpReloadState:             pb.WorkflowMcpReloadState_WORKFLOW_MCP_RELOAD_STATE_NOT_REQUESTED,
		sessionGroupID:             "wf-" + hash,
		workspaceID:                "wf-ws-" + hash,
		runtimeHome:                filepath.Join(stored.Root, "runtime", "codex-home"),
		cwd:                        filepath.Join(stored.Root, "runtime", "workspace"),
		processEpoch:               workflowProcessEpoch(stored.StorageKey, runtimeStarts),
		runtimeStarts:              runtimeStarts,
		activeRuns:                 map[string]workflowActiveRun{},
		createdAtUnixMS:            stored.Metadata.CreatedAtUnixMS,
		updatedAtUnixMS:            stored.Metadata.UpdatedAtUnixMS,
	}
}

func workflowRuntimeLaunchFromRecord(record *workflowRuntimeRecord) WorkflowRuntimeLaunch {
	if record == nil {
		return WorkflowRuntimeLaunch{}
	}
	return WorkflowRuntimeLaunch{
		Namespace:      record.namespace,
		WorkflowID:     record.workflowID,
		StorageKey:     record.storageKey,
		Root:           record.root,
		SessionGroupID: record.sessionGroupID,
		WorkspaceID:    record.workspaceID,
		RuntimeHome:    record.runtimeHome,
		CWD:            record.cwd,
		ProcessEpoch:   record.processEpoch,
	}
}

func workflowProcessEpoch(storageKey string, runtimeStarts int) string {
	return "wf-" + workflowShortHash(storageKey) + "-" + strconv.Itoa(runtimeStarts)
}

func workflowShortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func workflowPackageDeclaresMCP(pkg *workflowstorage.Package) bool {
	if pkg == nil {
		return false
	}
	for _, file := range pkg.Files {
		if !strings.EqualFold(file.Path, "config.toml") && !strings.HasSuffix(strings.ToLower(file.Path), "/config.toml") {
			continue
		}
		if strings.Contains(string(file.Contents), "[mcp_servers") {
			return true
		}
	}
	return false
}

func workflowSelectorFromPackage(pkg *workflowstorage.Package, raw *pb.WorkflowPackage) *pb.WorkflowSelector {
	if pkg != nil {
		return &pb.WorkflowSelector{Namespace: pkg.Namespace, WorkflowId: pkg.WorkflowID}
	}
	if raw != nil {
		return raw.GetWorkflow()
	}
	return nil
}

func workflowFromRecord(record *workflowRuntimeRecord) *pb.Workflow {
	lifecycle := pb.WorkflowLifecycle_WORKFLOW_LIFECYCLE_READY
	if record.restartRequired {
		lifecycle = pb.WorkflowLifecycle_WORKFLOW_LIFECYCLE_UPDATE_STAGED
	}
	return &pb.Workflow{
		Workflow:                   record.selector(),
		StorageKey:                 record.storageKey,
		ActivePackageFingerprint:   record.activePackageFingerprint,
		PendingPackageFingerprint:  record.pendingPackageFingerprint,
		PreviousPackageFingerprint: record.previousPackageFingerprint,
		Lifecycle:                  lifecycle,
		RestartRequired:            record.restartRequired,
		CreatedAtUnixMs:            record.createdAtUnixMS,
		UpdatedAtUnixMs:            record.updatedAtUnixMS,
		Capabilities:               defaultWorkflowCapabilitySet(),
	}
}

func workflowStatusFromRecord(record *workflowRuntimeRecord) *pb.WorkflowStatus {
	lifecycle := pb.WorkflowLifecycle_WORKFLOW_LIFECYCLE_READY
	if record.restartRequired {
		lifecycle = pb.WorkflowLifecycle_WORKFLOW_LIFECYCLE_UPDATE_STAGED
	}
	mcpReloadState := record.mcpReloadState
	if mcpReloadState == pb.WorkflowMcpReloadState_WORKFLOW_MCP_RELOAD_STATE_UNSPECIFIED {
		mcpReloadState = pb.WorkflowMcpReloadState_WORKFLOW_MCP_RELOAD_STATE_NOT_REQUESTED
	}
	return &pb.WorkflowStatus{
		Workflow:                   record.selector(),
		Lifecycle:                  lifecycle,
		ActivePackageFingerprint:   record.activePackageFingerprint,
		PendingPackageFingerprint:  record.pendingPackageFingerprint,
		PreviousPackageFingerprint: record.previousPackageFingerprint,
		RestartRequired:            record.restartRequired,
		McpReloadState:             mcpReloadState,
		ProcessEpoch:               record.processEpoch,
		ActiveWork:                 workflowActiveWorkToProto(record.activeRunSnapshot()),
		LastError:                  cloneWorkflowErrorDetails(record.lastError),
		Capabilities:               defaultWorkflowCapabilitySet(),
	}
}

func defaultWorkflowCapabilitySet() *pb.WorkflowCapabilitySet {
	return &pb.WorkflowCapabilitySet{
		Init:        pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED,
		Status:      pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED,
		Restart:     pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED,
		Delete:      pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED,
		Run:         pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED,
		History:     pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED,
		EventStream: pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED,
		McpReload:   pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_UNSUPPORTED,
	}
}

func (r *workflowRuntimeRecord) selector() *pb.WorkflowSelector {
	return &pb.WorkflowSelector{Namespace: r.namespace, WorkflowId: r.workflowID}
}

func (r *workflowRuntimeRecord) chatRuntimeContext() *pb.ChatRuntimeContext {
	return &pb.ChatRuntimeContext{SessionGroupId: r.sessionGroupID, WorkspaceId: r.workspaceID}
}

func (r *workflowRuntimeRecord) sessionMetadata() domain.SessionGroupMetadata {
	return domain.SessionGroupMetadata{
		SessionGroupID:           r.sessionGroupID,
		WorkspaceID:              r.workspaceID,
		GRPCInboundMessageBytes:  workflowRuntimeMessageLimitBytes,
		GRPCOutboundMessageBytes: workflowRuntimeMessageLimitBytes,
	}
}

type workflowSessionResolver struct {
	metadata domain.SessionGroupMetadata
}

func (r workflowSessionResolver) ResolveSessionGroup(sessionGroupID string) (domain.SessionGroupMetadata, bool) {
	if sessionGroupID != r.metadata.SessionGroupID {
		return domain.SessionGroupMetadata{}, false
	}
	return r.metadata, true
}

func classifyWorkflowUpdate(record *workflowRuntimeRecord, pkg *workflowstorage.Package) (workflowUpdateClass, error) {
	changed, err := changedWorkflowPackagePaths(record, pkg)
	if err != nil {
		return workflowUpdateRestartRequired, err
	}
	if len(changed) == 0 {
		return workflowUpdateHot, nil
	}
	if workflowPackageDeclaresMCP(pkg) && onlyWorkflowConfigChanged(changed) {
		return workflowUpdateMCPOnly, nil
	}
	if onlyWorkflowReferencesChanged(changed) {
		return workflowUpdateHot, nil
	}
	return workflowUpdateRestartRequired, nil
}

func changedWorkflowPackagePaths(record *workflowRuntimeRecord, pkg *workflowstorage.Package) (map[string]struct{}, error) {
	changed := map[string]struct{}{}
	currentRoot := filepath.Join(record.root, "current")
	seen := map[string]struct{}{}
	for _, file := range pkg.Files {
		seen[file.Path] = struct{}{}
		current, err := os.ReadFile(filepath.Join(currentRoot, filepath.FromSlash(file.Path)))
		if errors.Is(err, os.ErrNotExist) {
			changed[file.Path] = struct{}{}
			continue
		}
		if err != nil {
			return nil, workflowPackageStatusError(err, record.selector())
		}
		if string(current) != string(file.Contents) {
			changed[file.Path] = struct{}{}
		}
	}
	if err := filepath.WalkDir(currentRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(currentRoot, path)
		if err != nil {
			return err
		}
		packagePath := filepath.ToSlash(relative)
		if _, ok := seen[packagePath]; !ok {
			changed[packagePath] = struct{}{}
		}
		return nil
	}); err != nil {
		return nil, workflowPackageStatusError(err, record.selector())
	}
	return changed, nil
}

func onlyWorkflowConfigChanged(changed map[string]struct{}) bool {
	if len(changed) != 1 {
		return false
	}
	_, ok := changed["config.toml"]
	return ok
}

func onlyWorkflowReferencesChanged(changed map[string]struct{}) bool {
	for path := range changed {
		if !strings.HasPrefix(path, "references/") {
			return false
		}
	}
	return len(changed) > 0
}

func validateWorkflowMCPPolicy(pkg *workflowstorage.Package, selector *pb.WorkflowSelector) error {
	config := workflowPackageConfig(pkg)
	if config == "" {
		return nil
	}
	for _, rawLine := range strings.Split(config, "\n") {
		line := strings.TrimSpace(strings.Split(rawLine, "#")[0])
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "command"):
			value := unquoteWorkflowTOMLValue(line)
			if value == "" || strings.Contains(value, "..") || filepath.IsAbs(value) {
				return workflowMCPPolicyStatusError("mcp_stdio_command_rejected", "use a named command on the gateway allowlist or a materialized workflow helper path", selector)
			}
			helperPath := strings.TrimPrefix(filepath.ToSlash(value), "./")
			if strings.ContainsAny(value, `/\`) && !strings.HasPrefix(helperPath, "tools/") && !strings.HasPrefix(helperPath, "bin/") {
				return workflowMCPPolicyStatusError("mcp_stdio_command_rejected", "use a named command on the gateway allowlist or a materialized workflow helper path", selector)
			}
		case strings.HasPrefix(lower, "cwd"):
			value := unquoteWorkflowTOMLValue(line)
			if value == "" || strings.Contains(value, "..") || filepath.IsAbs(value) {
				return workflowMCPPolicyStatusError("mcp_cwd_rejected", "keep MCP cwd inside the materialized workflow runtime directory", selector)
			}
		case strings.HasPrefix(lower, "env"):
			if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "codex_home") || strings.Contains(lower, "client_auth") {
				return workflowMCPPolicyStatusError("mcp_env_ref_rejected", "use approved server-side env/file references without gateway auth or CODEX_HOME values", selector)
			}
		case strings.HasPrefix(lower, "url"), strings.HasPrefix(lower, "http_url"):
			value := strings.ToLower(unquoteWorkflowTOMLValue(line))
			if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
				if !strings.HasPrefix(value, "http://127.0.0.1") && !strings.HasPrefix(value, "http://localhost") && !strings.HasPrefix(value, "https://localhost") {
					return workflowMCPPolicyStatusError("mcp_http_endpoint_rejected", "use an approved MCP HTTP scheme and host pattern", selector)
				}
			}
		}
	}
	return nil
}

func workflowPackageConfig(pkg *workflowstorage.Package) string {
	if pkg == nil {
		return ""
	}
	for _, file := range pkg.Files {
		if file.Path == "config.toml" {
			return string(file.Contents)
		}
	}
	return ""
}

func unquoteWorkflowTOMLValue(line string) string {
	_, value, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"`)
	return value
}

func workflowEventCursor(processEpoch string, rawCursor string) string {
	if rawCursor == "" {
		return ""
	}
	return processEpoch + ":" + rawCursor
}

func workflowRawEventCursor(processEpoch string, eventCursor string, selector *pb.WorkflowSelector, chatID string) (string, error) {
	prefix, rawCursor, ok := strings.Cut(eventCursor, ":")
	if !ok || prefix == "" || rawCursor == "" {
		return "", workflowReplayUnavailableStatusError(selector, chatID)
	}
	if prefix != processEpoch {
		return "", workflowReplayUnavailableStatusError(selector, chatID)
	}
	return rawCursor, nil
}

func workflowChatLifecycleTerminal(lifecycle pb.ChatTurnLifecycle) bool {
	switch lifecycle {
	case pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_COMPLETED,
		pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_INTERRUPTED,
		pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_FAILED:
		return true
	default:
		return false
	}
}

func (r *workflowRuntimeRecord) hasActiveRuns() bool {
	return len(r.activeRuns) > 0
}

func (r *workflowRuntimeRecord) activeRunSnapshot() []workflowActiveRun {
	if len(r.activeRuns) == 0 {
		return nil
	}
	active := make([]workflowActiveRun, 0, len(r.activeRuns))
	for _, run := range r.activeRuns {
		active = append(active, run)
	}
	return active
}

func cloneWorkflowActiveRuns(active map[string]workflowActiveRun) map[string]workflowActiveRun {
	if len(active) == 0 {
		return map[string]workflowActiveRun{}
	}
	clone := make(map[string]workflowActiveRun, len(active))
	for key, value := range active {
		clone[key] = value
	}
	return clone
}

func workflowActiveWorkToProto(active []workflowActiveRun) []*pb.WorkflowActiveWork {
	if len(active) == 0 {
		return nil
	}
	out := make([]*pb.WorkflowActiveWork, 0, len(active))
	for _, run := range active {
		out = append(out, &pb.WorkflowActiveWork{
			ChatId:       run.chatID,
			RunId:        run.runID,
			RunLifecycle: run.lifecycle,
		})
	}
	return out
}

func workflowStreamChatEventsResponse(record *workflowRuntimeRecord, chatResponse *pb.StreamChatEventsResponse) (*pb.StreamWorkflowChatEventsResponse, error) {
	response := &pb.StreamWorkflowChatEventsResponse{Workflow: record.selector()}
	switch payload := chatResponse.GetPayload().(type) {
	case *pb.StreamChatEventsResponse_Event:
		if payload.Event != nil {
			payload.Event.EventCursor = workflowEventCursor(record.processEpoch, payload.Event.GetEventCursor())
		}
		response.Payload = &pb.StreamWorkflowChatEventsResponse_Event{Event: payload.Event}
	case *pb.StreamChatEventsResponse_ReplayNotice:
		response.Payload = &pb.StreamWorkflowChatEventsResponse_ReplayNotice{ReplayNotice: payload.ReplayNotice}
	case *pb.StreamChatEventsResponse_Narrowed:
		response.Payload = &pb.StreamWorkflowChatEventsResponse_Narrowed{Narrowed: payload.Narrowed}
	default:
		return nil, redactedInternalStatusError()
	}
	return response, nil
}

func workflowPackageStatusError(err error, selector *pb.WorkflowSelector) error {
	if errors.Is(err, context.Canceled) {
		return newWorkflowStatusError(codes.Canceled, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_GATEWAY_UNAVAILABLE, "request_cancelled", "workflow request was cancelled", "retry when the client is ready", selector, "", "", nil)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newWorkflowStatusError(codes.DeadlineExceeded, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_GATEWAY_UNAVAILABLE, "deadline_exceeded", "workflow request deadline exceeded", "retry with a longer deadline", selector, "", "", nil)
	}
	var packageError *workflowstorage.PackageError
	if errors.As(err, &packageError) {
		code := codes.InvalidArgument
		errorCode := pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_INVALID_WORKFLOW_PACKAGE
		message := "workflow package is invalid"
		if packageError.Reason == "file_too_large" || packageError.Reason == "package_too_large" {
			code = codes.ResourceExhausted
			errorCode = pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_PACKAGE_LIMIT_EXCEEDED
			message = "workflow package exceeds configured limits"
		}
		metadata := map[string]string{}
		if packageError.DisplayPath != "" {
			metadata["path"] = packageError.DisplayPath
		}
		return newWorkflowStatusError(code, errorCode, packageError.Reason, message, packageError.NextAction, selector, "", "", metadata)
	}
	return newWorkflowStatusError(codes.Internal, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_GATEWAY_UNAVAILABLE, "workflow_storage_error", "workflow storage operation failed", "retry after checking gateway storage health", selector, "", "", nil)
}

func workflowRequestStatusError(requestError *RequestError, selector *pb.WorkflowSelector, chatID string) error {
	if requestError == nil {
		return nil
	}
	reason := string(requestError.Details.Reason)
	message := requestError.Details.DisplayMessage
	if message == "" {
		message = reason
	}
	errorCode := pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_INVALID_WORKFLOW_PACKAGE
	if strings.Contains(message, "prompt") {
		errorCode = pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_EMPTY_PROMPT
	}
	if requestError.Code == codes.NotFound {
		errorCode = pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_WORKFLOW_NOT_FOUND
	}
	if requestError.Code == codes.ResourceExhausted {
		errorCode = pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_PACKAGE_LIMIT_EXCEEDED
	}
	return newWorkflowStatusError(requestError.Code, errorCode, reason, message, "", selector, chatID, "", nil)
}

func workflowRuntimeStatusError(err error, selector *pb.WorkflowSelector, chatID string) error {
	code := status.Code(err)
	if code == codes.OK || code == codes.Unknown {
		code = codes.Unavailable
	}
	return newWorkflowStatusError(code, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_GATEWAY_UNAVAILABLE, "workflow_chat_runtime_unavailable", "workflow chat runtime is unavailable", "retry after the workflow runtime is ready", selector, chatID, "", nil)
}

func workflowNotFoundStatusError(selector *pb.WorkflowSelector) error {
	return newWorkflowStatusError(codes.NotFound, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_WORKFLOW_NOT_FOUND, "workflow_not_found", "workflow is not initialized", "initialize the workflow before using it", selector, "", "", nil)
}

func workflowRestartRequiredStatusError(selector *pb.WorkflowSelector, chatID string) error {
	return newWorkflowStatusError(codes.FailedPrecondition, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_RESTART_REQUIRED, "restart_required", "workflow update requires restart before new runs", "call RestartWorkflow before starting another workflow run", selector, chatID, "", nil)
}

func workflowReplayUnavailableStatusError(selector *pb.WorkflowSelector, chatID string) error {
	return newWorkflowStatusError(codes.FailedPrecondition, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_REPLAY_UNAVAILABLE, "replay_unavailable", "workflow event cursor belongs to an older runtime epoch", "start a fresh event stream from the current workflow chat state", selector, chatID, "", nil)
}

func workflowActiveWorkRefusedStatusError(selector *pb.WorkflowSelector) error {
	return newWorkflowStatusError(codes.FailedPrecondition, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_ACTIVE_WORK_REFUSED, "active_work_refused", "workflow has active work", "wait for active work to finish or pass force explicitly", selector, "", "", nil)
}

func workflowGracefulRestartDeadlineStatusError(selector *pb.WorkflowSelector) error {
	return newWorkflowStatusError(codes.DeadlineExceeded, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_ACTIVE_WORK_REFUSED, "active_work_deadline", "workflow restart timed out while waiting for active work", "retry when active work finishes or use force explicitly", selector, "", "", nil)
}

func workflowMCPPolicyStatusError(reason string, nextAction string, selector *pb.WorkflowSelector) error {
	return newWorkflowStatusError(codes.PermissionDenied, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_MCP_UNAVAILABLE, reason, "workflow MCP policy rejected this configuration", nextAction, selector, "", "", nil)
}

func workflowMCPReloadFailedStatusError(err error, selector *pb.WorkflowSelector) error {
	code := status.Code(err)
	if code == codes.OK || code == codes.Unknown {
		code = codes.FailedPrecondition
	}
	return newWorkflowStatusError(code, pb.WorkflowErrorCode_WORKFLOW_ERROR_CODE_MCP_NOT_REACHABLE, "mcp_reload_failed", "workflow MCP reload failed on the gateway", "check gateway-side MCP command, network and env setup or restart the workflow", selector, "", "", nil)
}

func workflowDetailsFromError(err error) *pb.WorkflowErrorDetails {
	st, ok := status.FromError(err)
	if !ok {
		return nil
	}
	for _, detail := range st.Details() {
		if workflowDetails, ok := detail.(*pb.WorkflowErrorDetails); ok {
			return workflowDetails
		}
	}
	return nil
}

func newWorkflowStatusError(code codes.Code, errorCode pb.WorkflowErrorCode, reason string, message string, nextAction string, selector *pb.WorkflowSelector, chatID string, runID string, safeMetadata map[string]string) error {
	st := status.New(code, message)
	stWithDetails, err := st.WithDetails(&pb.WorkflowErrorDetails{
		Outcome:        workflowOutcomeFromCode(code),
		Code:           errorCode,
		Reason:         reason,
		DisplayMessage: message,
		Retryable:      code == codes.Unavailable || code == codes.Aborted || code == codes.DeadlineExceeded,
		NextAction:     nextAction,
		Workflow:       cloneWorkflowSelector(selector),
		ChatId:         chatID,
		RunId:          runID,
		SafeMetadata:   safeMetadata,
	})
	if err != nil {
		return status.Error(codes.Internal, "failed to attach workflow error details")
	}
	return stWithDetails.Err()
}

func workflowOutcomeFromCode(code codes.Code) pb.WorkflowOutcomeCategory {
	switch code {
	case codes.Canceled:
		return pb.WorkflowOutcomeCategory_WORKFLOW_OUTCOME_CATEGORY_CANCELLED
	case codes.DeadlineExceeded:
		return pb.WorkflowOutcomeCategory_WORKFLOW_OUTCOME_CATEGORY_DEADLINE_EXCEEDED
	case codes.InvalidArgument:
		return pb.WorkflowOutcomeCategory_WORKFLOW_OUTCOME_CATEGORY_INVALID_ARGUMENT
	case codes.NotFound:
		return pb.WorkflowOutcomeCategory_WORKFLOW_OUTCOME_CATEGORY_NOT_FOUND
	case codes.AlreadyExists, codes.Aborted:
		return pb.WorkflowOutcomeCategory_WORKFLOW_OUTCOME_CATEGORY_ABORTED
	case codes.PermissionDenied:
		return pb.WorkflowOutcomeCategory_WORKFLOW_OUTCOME_CATEGORY_PERMISSION_DENIED
	case codes.Unauthenticated:
		return pb.WorkflowOutcomeCategory_WORKFLOW_OUTCOME_CATEGORY_UNAUTHENTICATED
	case codes.FailedPrecondition:
		return pb.WorkflowOutcomeCategory_WORKFLOW_OUTCOME_CATEGORY_FAILED_PRECONDITION
	case codes.Unimplemented:
		return pb.WorkflowOutcomeCategory_WORKFLOW_OUTCOME_CATEGORY_UNSUPPORTED
	case codes.Unavailable:
		return pb.WorkflowOutcomeCategory_WORKFLOW_OUTCOME_CATEGORY_UNAVAILABLE
	case codes.OutOfRange, codes.ResourceExhausted:
		return pb.WorkflowOutcomeCategory_WORKFLOW_OUTCOME_CATEGORY_OUT_OF_RANGE
	case codes.Internal, codes.DataLoss:
		return pb.WorkflowOutcomeCategory_WORKFLOW_OUTCOME_CATEGORY_INTERNAL
	default:
		return pb.WorkflowOutcomeCategory_WORKFLOW_OUTCOME_CATEGORY_UNKNOWN
	}
}

func cloneWorkflowSelector(selector *pb.WorkflowSelector) *pb.WorkflowSelector {
	if selector == nil {
		return nil
	}
	return &pb.WorkflowSelector{
		Namespace:  selector.GetNamespace(),
		WorkflowId: selector.GetWorkflowId(),
	}
}

func cloneWorkflowErrorDetails(details *pb.WorkflowErrorDetails) *pb.WorkflowErrorDetails {
	if details == nil {
		return nil
	}
	clone := &pb.WorkflowErrorDetails{
		Outcome:        details.GetOutcome(),
		Code:           details.GetCode(),
		Reason:         details.GetReason(),
		DisplayMessage: details.GetDisplayMessage(),
		Retryable:      details.GetRetryable(),
		NextAction:     details.GetNextAction(),
		Workflow:       cloneWorkflowSelector(details.GetWorkflow()),
		ChatId:         details.GetChatId(),
		RunId:          details.GetRunId(),
	}
	if details.GetSafeMetadata() != nil {
		clone.SafeMetadata = make(map[string]string, len(details.GetSafeMetadata()))
		for key, value := range details.GetSafeMetadata() {
			clone.SafeMetadata[key] = value
		}
	}
	return clone
}
