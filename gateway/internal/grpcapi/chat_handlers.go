package grpcapi

import (
	"context"
	"errors"
	"io"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type chatRuntimeService struct {
	pb.UnimplementedChatRuntimeServiceServer
	enabled             bool
	maxRecvMessageBytes int
	maxSendMessageBytes int
	sessionGroups       SessionGroupResolver
	runtime             ChatRuntimeService
}

func newChatRuntimeService(enabled bool, maxRecvMessageBytes int) *chatRuntimeService {
	return &chatRuntimeService{
		enabled:             enabled,
		maxRecvMessageBytes: maxRecvMessageBytes,
		maxSendMessageBytes: maxRecvMessageBytes,
	}
}

type ChatRuntimeServiceOptions struct {
	Enabled             bool
	MaxRecvMessageBytes int
	MaxSendMessageBytes int
	SessionGroups       SessionGroupResolver
	Runtime             ChatRuntimeService
}

func NewChatRuntimeService(options ChatRuntimeServiceOptions) pb.ChatRuntimeServiceServer {
	return &chatRuntimeService{
		enabled:             options.Enabled,
		maxRecvMessageBytes: options.MaxRecvMessageBytes,
		maxSendMessageBytes: options.MaxSendMessageBytes,
		sessionGroups:       options.SessionGroups,
		runtime:             options.Runtime,
	}
}

func (s *chatRuntimeService) StartChatRun(ctx context.Context, req *pb.StartChatRunRequest) (*pb.StartChatRunResponse, error) {
	if err := s.stage03Unavailable(req); err != nil {
		return nil, err
	}
	if s.runtime == nil {
		return nil, s.notImplemented()
	}
	command, requestError := ValidateStartChatRun(req, s.sessionGroups)
	if requestError != nil {
		return nil, chatRuntimeStatusErrorFromRequestError(requestError)
	}
	trustedMetadata, requestError := trustedSessionMetadataForSessionGroupID(command.SessionGroupID, s.sessionGroups)
	if requestError != nil {
		return nil, chatRuntimeStatusErrorFromRequestError(requestError)
	}
	response, err := s.runtime.StartChatRun(ctx, command)
	if err != nil {
		return nil, chatRuntimeStatusErrorFromServiceError(err)
	}
	protoResponse, err := startChatRunResponseToProto(response)
	if err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForTrustedSession(protoResponse, trustedMetadata, response.SessionGroupID); err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForProcess(protoResponse, s.maxSendMessageBytes); err != nil {
		return nil, err
	}
	return protoResponse, nil
}

func (s *chatRuntimeService) GetChat(ctx context.Context, req *pb.GetChatRequest) (*pb.GetChatResponse, error) {
	if err := s.stage03Unavailable(req); err != nil {
		return nil, err
	}
	if s.runtime == nil {
		return nil, s.notImplemented()
	}
	command, requestError := ValidateGetChat(req, s.sessionGroups)
	if requestError != nil {
		return nil, chatRuntimeStatusErrorFromRequestError(requestError)
	}
	trustedMetadata, requestError := trustedSessionMetadataForSessionGroupID(command.SessionGroupID, s.sessionGroups)
	if requestError != nil {
		return nil, chatRuntimeStatusErrorFromRequestError(requestError)
	}
	response, err := s.runtime.GetChat(ctx, command)
	if err != nil {
		return nil, chatRuntimeStatusErrorFromServiceError(err)
	}
	protoResponse, err := getChatResponseToProto(response)
	if err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForTrustedSession(protoResponse, trustedMetadata, response.Chat.SessionGroupID); err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForProcess(protoResponse, s.maxSendMessageBytes); err != nil {
		return nil, err
	}
	return protoResponse, nil
}

func (s *chatRuntimeService) RunChatTurn(ctx context.Context, req *pb.RunChatTurnRequest) (*pb.RunChatTurnResponse, error) {
	if err := s.stage03Unavailable(req); err != nil {
		return nil, err
	}
	if s.runtime == nil {
		return nil, s.notImplemented()
	}
	command, requestError := ValidateRunChatTurn(req, s.sessionGroups)
	if requestError != nil {
		return nil, chatRuntimeStatusErrorFromRequestError(requestError)
	}
	trustedMetadata, requestError := trustedSessionMetadataForSessionGroupID(command.SessionGroupID, s.sessionGroups)
	if requestError != nil {
		return nil, chatRuntimeStatusErrorFromRequestError(requestError)
	}
	response, err := s.runtime.RunChatTurn(ctx, command)
	if err != nil {
		return nil, chatRuntimeStatusErrorFromServiceError(err)
	}
	protoResponse, err := runChatTurnResponseToProto(response)
	if err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForTrustedSession(protoResponse, trustedMetadata, response.SessionGroupID); err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForProcess(protoResponse, s.maxSendMessageBytes); err != nil {
		return nil, err
	}
	return protoResponse, nil
}

func (s *chatRuntimeService) GetChatStatus(ctx context.Context, req *pb.GetChatStatusRequest) (*pb.GetChatStatusResponse, error) {
	if err := s.stage03Unavailable(req); err != nil {
		return nil, err
	}
	if s.runtime == nil {
		return nil, s.notImplemented()
	}
	command, requestError := ValidateGetChatStatus(req, s.sessionGroups)
	if requestError != nil {
		return nil, chatRuntimeStatusErrorFromRequestError(requestError)
	}
	trustedMetadata, requestError := trustedSessionMetadataForSessionGroupID(command.SessionGroupID, s.sessionGroups)
	if requestError != nil {
		return nil, chatRuntimeStatusErrorFromRequestError(requestError)
	}
	response, err := s.runtime.GetChatStatus(ctx, command)
	if err != nil {
		return nil, chatRuntimeStatusErrorFromServiceError(err)
	}
	protoResponse, err := getChatStatusResponseToProto(response)
	if err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForTrustedSession(protoResponse, trustedMetadata, response.Status.SessionGroupID); err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForProcess(protoResponse, s.maxSendMessageBytes); err != nil {
		return nil, err
	}
	return protoResponse, nil
}

func (s *chatRuntimeService) GetChatHistory(ctx context.Context, req *pb.GetChatHistoryRequest) (*pb.GetChatHistoryResponse, error) {
	if err := s.stage03Unavailable(req); err != nil {
		return nil, err
	}
	if s.runtime == nil {
		return nil, s.notImplemented()
	}
	command, requestError := ValidateGetChatHistory(req, s.sessionGroups)
	if requestError != nil {
		return nil, chatRuntimeStatusErrorFromRequestError(requestError)
	}
	trustedMetadata, requestError := trustedSessionMetadataForSessionGroupID(command.SessionGroupID, s.sessionGroups)
	if requestError != nil {
		return nil, chatRuntimeStatusErrorFromRequestError(requestError)
	}
	response, err := s.runtime.GetChatHistory(ctx, command)
	if err != nil {
		return nil, chatRuntimeStatusErrorFromServiceError(err)
	}
	protoResponse, err := getChatHistoryResponseToProto(response)
	if err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForTrustedSession(protoResponse, trustedMetadata, command.SessionGroupID); err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForProcess(protoResponse, s.maxSendMessageBytes); err != nil {
		return nil, err
	}
	return protoResponse, nil
}

func (s *chatRuntimeService) StreamChatEvents(req *pb.StreamChatEventsRequest, stream pb.ChatRuntimeService_StreamChatEventsServer) error {
	if err := s.stage03Unavailable(req); err != nil {
		return err
	}
	if s.runtime == nil {
		return s.notImplemented()
	}
	command, requestError := ValidateStreamChatEvents(req, s.sessionGroups)
	if requestError != nil {
		return chatRuntimeStatusErrorFromRequestError(requestError)
	}
	trustedMetadata, requestError := trustedSessionMetadataForSessionGroupID(command.SessionGroupID, s.sessionGroups)
	if requestError != nil {
		return chatRuntimeStatusErrorFromRequestError(requestError)
	}
	subscriber, err := s.runtime.StreamChatEvents(stream.Context(), command)
	if err != nil {
		return chatRuntimeStatusErrorFromServiceError(err)
	}
	defer func() {
		_ = subscriber.Close()
	}()
	for {
		message, err := subscriber.Next(stream.Context())
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return chatRuntimeStatusErrorFromServiceError(err)
		}
		protoMessage, err := streamChatEventsMessageToProto(message, trustedMetadata)
		if err != nil {
			return err
		}
		if err := validateOutboundMessageForProcess(protoMessage, s.maxSendMessageBytes); err != nil {
			return err
		}
		if err := stream.Send(protoMessage); err != nil {
			if stream.Context().Err() != nil {
				return chatRuntimeStatusErrorFromServiceError(stream.Context().Err())
			}
			return chatRuntimeStatusErrorFromServiceError(err)
		}
	}
}

func (s *chatRuntimeService) RespondChatPending(ctx context.Context, req *pb.RespondChatPendingRequest) (*pb.RespondChatPendingResponse, error) {
	if err := s.stage03Unavailable(req); err != nil {
		return nil, err
	}
	if s.runtime == nil {
		return nil, s.notImplemented()
	}
	command, requestError := ValidateRespondChatPending(req, s.sessionGroups)
	if requestError != nil {
		return nil, chatRuntimeStatusErrorFromRequestError(requestError)
	}
	trustedMetadata, requestError := trustedSessionMetadataForSessionGroupID(command.SessionGroupID, s.sessionGroups)
	if requestError != nil {
		return nil, chatRuntimeStatusErrorFromRequestError(requestError)
	}
	response, err := s.runtime.RespondChatPending(ctx, command)
	if err != nil {
		return nil, chatRuntimeStatusErrorFromServiceError(err)
	}
	protoResponse, err := respondChatPendingResponseToProto(response)
	if err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForTrustedSession(protoResponse, trustedMetadata, response.SessionGroupID); err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForProcess(protoResponse, s.maxSendMessageBytes); err != nil {
		return nil, err
	}
	return protoResponse, nil
}

func (s *chatRuntimeService) InterruptChatRun(ctx context.Context, req *pb.InterruptChatRunRequest) (*pb.InterruptChatRunResponse, error) {
	if err := s.stage03Unavailable(req); err != nil {
		return nil, err
	}
	if s.runtime == nil {
		return nil, s.notImplemented()
	}
	command, requestError := ValidateInterruptChatRun(req, s.sessionGroups)
	if requestError != nil {
		return nil, chatRuntimeStatusErrorFromRequestError(requestError)
	}
	trustedMetadata, requestError := trustedSessionMetadataForSessionGroupID(command.SessionGroupID, s.sessionGroups)
	if requestError != nil {
		return nil, chatRuntimeStatusErrorFromRequestError(requestError)
	}
	response, err := s.runtime.InterruptChatRun(ctx, command)
	if err != nil {
		return nil, chatRuntimeStatusErrorFromServiceError(err)
	}
	protoResponse, err := interruptChatRunResponseToProto(response)
	if err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForTrustedSession(protoResponse, trustedMetadata, response.SessionGroupID); err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForProcess(protoResponse, s.maxSendMessageBytes); err != nil {
		return nil, err
	}
	return protoResponse, nil
}

func (s *chatRuntimeService) stage03Unavailable(req proto.Message) error {
	if requestError := validateInboundMessageForProcess(req, s.maxRecvMessageBytes); requestError != nil {
		return StatusErrorFromRequestError(requestError)
	}
	if !s.enabled {
		return newChatRuntimeStatusError(
			codes.Unimplemented,
			pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNSUPPORTED,
			domain.ReasonChatRuntimeDisabled,
			"chat runtime disabled",
			false,
		)
	}
	return nil
}

func (s *chatRuntimeService) notImplemented() error {
	return newChatRuntimeStatusError(
		codes.Unimplemented,
		pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNSUPPORTED,
		domain.ReasonChatRuntimeNotImplemented,
		"chat runtime method not implemented",
		false,
	)
}

func newChatRuntimeStatusError(code codes.Code, outcome pb.ChatOutcomeCategory, reason domain.GatewayErrorReason, message string, retryable bool) error {
	if message == "" {
		message = string(reason)
	}
	st := status.New(code, message)
	stWithDetails, err := st.WithDetails(&pb.ChatRuntimeErrorDetails{
		Outcome:        outcome,
		Reason:         string(reason),
		DisplayMessage: message,
		Retryable:      retryable,
	})
	if err != nil {
		return status.Error(codes.Internal, "failed to attach chat runtime error details")
	}
	return stWithDetails.Err()
}

func chatRuntimeStatusErrorFromRequestError(err *RequestError) error {
	if err == nil {
		return nil
	}
	return newChatRuntimeStatusErrorFromDetails(err.Code, err.Details)
}

func chatRuntimeStatusErrorFromServiceError(err error) error {
	if err == nil {
		return nil
	}
	var requestError *RequestError
	if errors.As(err, &requestError) {
		return chatRuntimeStatusErrorFromRequestError(requestError)
	}
	var gatewayError *domain.GatewayError
	if errors.As(err, &gatewayError) {
		return newChatRuntimeStatusErrorFromDetails(codeFromDomainGatewayError(gatewayError.Code), gatewayError.Details)
	}
	if errors.Is(err, context.Canceled) {
		return newChatRuntimeStatusErrorFromDetails(codes.Canceled, domain.GatewayErrorDetails{
			Reason:         domain.ReasonCallerCanceled,
			DisplayMessage: "caller canceled",
		})
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newChatRuntimeStatusErrorFromDetails(codes.DeadlineExceeded, domain.GatewayErrorDetails{
			Reason:         domain.ReasonCallerDeadlineExceeded,
			DisplayMessage: "caller deadline exceeded",
		})
	}
	return newChatRuntimeStatusErrorFromDetails(codes.Internal, domain.GatewayErrorDetails{
		Reason:         domain.ReasonInternalGatewayError,
		DisplayMessage: "internal gateway error",
	})
}

func newChatRuntimeStatusErrorFromDetails(code codes.Code, details domain.GatewayErrorDetails) error {
	reason := details.Reason
	if reason == "" {
		reason = domain.ReasonInternalGatewayError
	}
	message := details.DisplayMessage
	if message == "" {
		message = string(reason)
	}
	outcome := chatOutcomeFromCode(code)
	return newChatRuntimeStatusErrorWithDetails(code, outcome, reason, message, details)
}

func newChatRuntimeStatusErrorWithDetails(code codes.Code, outcome pb.ChatOutcomeCategory, reason domain.GatewayErrorReason, message string, details domain.GatewayErrorDetails) error {
	st := status.New(code, message)
	stWithDetails, err := st.WithDetails(&pb.ChatRuntimeErrorDetails{
		Outcome:          outcome,
		Reason:           string(reason),
		DisplayMessage:   message,
		ChatId:           details.ThreadID,
		SessionGroupId:   details.SessionGroupID,
		PendingRequestId: details.PendingRequestID,
		Retryable:        details.Retryable,
	})
	if err != nil {
		return status.Error(codes.Internal, "failed to attach chat runtime error details")
	}
	return stWithDetails.Err()
}

func chatOutcomeFromCode(code codes.Code) pb.ChatOutcomeCategory {
	switch code {
	case codes.InvalidArgument:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_INVALID_ARGUMENT
	case codes.Unauthenticated:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNAUTHENTICATED
	case codes.PermissionDenied:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_PERMISSION_DENIED
	case codes.NotFound:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_NOT_FOUND
	case codes.FailedPrecondition:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_FAILED_PRECONDITION
	case codes.Aborted:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_ABORTED
	case codes.Unimplemented:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNSUPPORTED
	case codes.Unavailable:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNAVAILABLE
	case codes.Unknown:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNKNOWN
	case codes.OutOfRange:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_OUT_OF_RANGE
	case codes.DeadlineExceeded:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_DEADLINE_EXCEEDED
	case codes.Canceled:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_CANCELLED
	default:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_INTERNAL
	}
}

func startChatRunResponseToProto(response domain.StartChatRunResponse) (*pb.StartChatRunResponse, error) {
	if response.ChatID == "" || response.RunID == "" || response.SessionGroupID == "" || response.WorkspaceID == "" || !response.FirstTurnAccepted {
		return nil, newChatRuntimeStatusErrorFromDetails(codes.Internal, domain.GatewayErrorDetails{
			Reason:         domain.ReasonInternalGatewayError,
			DisplayMessage: "start chat run response is missing required fields",
			SessionGroupID: response.SessionGroupID,
			ThreadID:       response.ChatID,
		})
	}
	status := &pb.ChatStatus{
		ChatId:              response.ChatID,
		SessionGroupId:      response.SessionGroupID,
		WorkspaceId:         response.WorkspaceID,
		Lookup:              pb.ChatThreadLookupState_CHAT_THREAD_LOOKUP_STATE_VALID,
		ThreadLifecycle:     pb.ChatThreadLifecycle_CHAT_THREAD_LIFECYCLE_ACTIVE_RUNNING,
		CurrentRunLifecycle: pb.ChatTurnLifecycle_CHAT_TURN_LIFECYCLE_IN_PROGRESS,
		CurrentRunId:        response.RunID,
		LastRunId:           response.RunID,
		LastEventId:         response.LastEventID,
		Capabilities:        defaultChatCapabilitySet(),
		GatewayLocal: &pb.ChatGatewayLocalState{
			Signals: []pb.ChatGatewayLocalSignal{
				pb.ChatGatewayLocalSignal_CHAT_GATEWAY_LOCAL_SIGNAL_LIVE,
				pb.ChatGatewayLocalSignal_CHAT_GATEWAY_LOCAL_SIGNAL_REPLAY_AVAILABLE,
			},
			ProcessEpoch: response.ProcessEpoch,
		},
	}
	return &pb.StartChatRunResponse{
		ChatId:            response.ChatID,
		RunId:             response.RunID,
		SessionGroupId:    response.SessionGroupID,
		WorkspaceId:       response.WorkspaceID,
		Status:            status,
		LastEventId:       response.LastEventID,
		EventCursor:       response.EventCursor,
		FirstTurnAccepted: response.FirstTurnAccepted,
		Capabilities:      defaultChatCapabilitySet(),
	}, nil
}

func defaultChatCapabilitySet() *pb.ChatCapabilitySet {
	return &pb.ChatCapabilitySet{
		Status:      pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED,
		History:     pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED,
		EventStream: pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_NARROWED,
		Replay:      pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_NARROWED,
		Pending:     pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED,
		Interrupt:   pb.ChatCapabilityState_CHAT_CAPABILITY_STATE_SUPPORTED,
	}
}

func getChatResponseToProto(response domain.GetChatResponse) (*pb.GetChatResponse, error) {
	if response.Chat.ChatID == "" || !chatStatusMatches(response.Status, response.Chat.SessionGroupID, response.Chat.WorkspaceID, response.Chat.ChatID) {
		return nil, redactedInternalStatusError()
	}
	chat, err := chatToProto(response.Chat)
	if err != nil {
		return nil, err
	}
	status, err := chatStatusToProto(response.Status)
	if err != nil {
		return nil, err
	}
	return &pb.GetChatResponse{Chat: chat, Status: status}, nil
}

func runChatTurnResponseToProto(response domain.RunChatTurnResponse) (*pb.RunChatTurnResponse, error) {
	if response.ChatID == "" || response.RunID == "" || response.SessionGroupID == "" || response.WorkspaceID == "" || !response.TurnAccepted {
		return nil, newChatRuntimeStatusErrorFromDetails(codes.Internal, domain.GatewayErrorDetails{
			Reason:         domain.ReasonInternalGatewayError,
			DisplayMessage: "run chat turn response is missing required fields",
			SessionGroupID: response.SessionGroupID,
			ThreadID:       response.ChatID,
		})
	}
	if !chatStatusMatches(response.Status, response.SessionGroupID, response.WorkspaceID, response.ChatID) {
		return nil, redactedInternalStatusError()
	}
	status, err := chatStatusToProto(response.Status)
	if err != nil {
		return nil, err
	}
	return &pb.RunChatTurnResponse{
		ChatId:         response.ChatID,
		RunId:          response.RunID,
		SessionGroupId: response.SessionGroupID,
		WorkspaceId:    response.WorkspaceID,
		Status:         status,
		LastEventId:    response.LastEventID,
		EventCursor:    response.EventCursor,
		TurnAccepted:   response.TurnAccepted,
	}, nil
}

func getChatStatusResponseToProto(response domain.GetChatStatusResponse) (*pb.GetChatStatusResponse, error) {
	if response.Status.ChatID == "" || response.Status.SessionGroupID == "" || response.Status.WorkspaceID == "" {
		return nil, redactedInternalStatusError()
	}
	status, err := chatStatusToProto(response.Status)
	if err != nil {
		return nil, err
	}
	return &pb.GetChatStatusResponse{Status: status}, nil
}

func respondChatPendingResponseToProto(response domain.RespondChatPendingResponse) (*pb.RespondChatPendingResponse, error) {
	if response.ChatID == "" || response.RunID == "" || response.PendingRequestID == "" || response.ClientResponseID == "" || !response.Accepted {
		return nil, newChatRuntimeStatusErrorFromDetails(codes.Internal, domain.GatewayErrorDetails{
			Reason:         domain.ReasonInternalGatewayError,
			DisplayMessage: "respond chat pending response is missing required fields",
			SessionGroupID: response.SessionGroupID,
			ThreadID:       response.ChatID,
		})
	}
	if !chatStatusMatches(response.Status, response.SessionGroupID, response.WorkspaceID, response.ChatID) {
		return nil, redactedInternalStatusError()
	}
	status, err := chatStatusToProto(response.Status)
	if err != nil {
		return nil, err
	}
	return &pb.RespondChatPendingResponse{
		ChatId:           response.ChatID,
		RunId:            response.RunID,
		PendingRequestId: response.PendingRequestID,
		ClientResponseId: response.ClientResponseID,
		Accepted:         response.Accepted,
		AlreadyApplied:   response.AlreadyApplied,
		LastEventId:      response.LastEventID,
		Status:           status,
	}, nil
}

func interruptChatRunResponseToProto(response domain.InterruptChatRunResponse) (*pb.InterruptChatRunResponse, error) {
	if response.ChatID == "" || response.RunID == "" || (!response.InterruptSent && !response.AlreadyInterrupting && !response.AlreadyTerminal) {
		return nil, newChatRuntimeStatusErrorFromDetails(codes.Internal, domain.GatewayErrorDetails{
			Reason:         domain.ReasonInternalGatewayError,
			DisplayMessage: "interrupt chat run response is missing required fields",
			SessionGroupID: response.SessionGroupID,
			ThreadID:       response.ChatID,
		})
	}
	if !chatStatusMatches(response.Status, response.SessionGroupID, response.WorkspaceID, response.ChatID) {
		return nil, redactedInternalStatusError()
	}
	status, err := chatStatusToProto(response.Status)
	if err != nil {
		return nil, err
	}
	return &pb.InterruptChatRunResponse{
		ChatId:              response.ChatID,
		RunId:               response.RunID,
		Status:              status,
		InterruptSent:       response.InterruptSent,
		AlreadyInterrupting: response.AlreadyInterrupting,
		AlreadyTerminal:     response.AlreadyTerminal,
		LastEventId:         response.LastEventID,
	}, nil
}

func chatStatusMatches(status domain.ChatStatus, sessionGroupID string, workspaceID string, chatID string) bool {
	return status.SessionGroupID == sessionGroupID &&
		status.WorkspaceID == workspaceID &&
		status.ChatID == chatID
}

func getChatHistoryResponseToProto(response domain.GetChatHistoryResponse) (*pb.GetChatHistoryResponse, error) {
	depth, _ := ChatHistoryDepthToProto(response.ReturnedDepth)
	capability, _ := ChatCapabilityStateToProto(response.Capability)
	turns := make([]*pb.ChatTurnSummary, 0, len(response.Turns))
	for _, turn := range response.Turns {
		protoTurn, err := chatTurnSummaryToProto(turn)
		if err != nil {
			return nil, err
		}
		turns = append(turns, protoTurn)
	}
	return &pb.GetChatHistoryResponse{
		ChatId:          response.ChatID,
		Turns:           turns,
		NextCursor:      response.NextCursor,
		BackwardsCursor: response.BackwardsCursor,
		ReturnedDepth:   depth,
		Capability:      capability,
		Narrowed:        chatNarrowedOutcomeToProto(response.Narrowed, response.Capability),
	}, nil
}

func chatToProto(chat domain.Chat) (*pb.Chat, error) {
	lifecycle, _ := ChatThreadLifecycleToProto(chat.ThreadLifecycle)
	return &pb.Chat{
		ChatId:          chat.ChatID,
		SessionGroupId:  chat.SessionGroupID,
		WorkspaceId:     chat.WorkspaceID,
		ThreadLifecycle: lifecycle,
		CreatedAtUnixMs: chat.CreatedAtUnixMS,
		UpdatedAtUnixMs: chat.UpdatedAtUnixMS,
		Preview:         chat.Preview,
		Ephemeral:       chat.Ephemeral,
		Capabilities:    chatCapabilitySetToProto(chat.Capabilities),
	}, nil
}

func chatStatusToProto(status domain.ChatStatus) (*pb.ChatStatus, error) {
	threadLifecycle, _ := ChatThreadLifecycleToProto(status.ThreadLifecycle)
	runLifecycle, _ := ChatTurnLifecycleToProto(status.CurrentRunLifecycle)
	lookup := pb.ChatThreadLookupState_CHAT_THREAD_LOOKUP_STATE_UNKNOWN
	if status.LookupValid {
		lookup = pb.ChatThreadLookupState_CHAT_THREAD_LOOKUP_STATE_VALID
	}
	activePending := make([]*pb.ChatPendingRequest, 0, len(status.ActivePending))
	for _, request := range status.ActivePending {
		protoRequest, err := chatPendingRequestToProto(request)
		if err != nil {
			return nil, err
		}
		activePending = append(activePending, protoRequest)
	}
	terminal, err := chatTerminalToProto(status.Terminal)
	if err != nil {
		return nil, err
	}
	return &pb.ChatStatus{
		ChatId:                status.ChatID,
		SessionGroupId:        status.SessionGroupID,
		WorkspaceId:           status.WorkspaceID,
		Lookup:                lookup,
		ThreadLifecycle:       threadLifecycle,
		CurrentRunLifecycle:   runLifecycle,
		CurrentRunId:          status.CurrentRunID,
		LastRunId:             status.LastRunID,
		Capabilities:          chatCapabilitySetToProto(status.Capabilities),
		GatewayLocal:          chatGatewayLocalToProto(status.GatewayLocal),
		ActivePendingRequests: activePending,
		Terminal:              terminal,
		LastEventId:           status.LastEventID,
	}, nil
}

func chatPendingRequestToProto(request domain.ChatPendingRequest) (*pb.ChatPendingRequest, error) {
	if request.PendingRequestID == "" || request.ChatID == "" || request.RunID == "" {
		return nil, redactedInternalStatusError()
	}
	pendingType, ok := PendingTypeToProto(request.PendingType)
	if !ok {
		return nil, redactedInternalStatusError()
	}
	display, ok := PendingRequestDisplayToProto(request.Display)
	if !ok {
		return nil, redactedInternalStatusError()
	}
	return &pb.ChatPendingRequest{
		PendingRequestId: request.PendingRequestID,
		ChatId:           request.ChatID,
		RunId:            request.RunID,
		PendingType:      pendingType,
		CreatedAtUnixMs:  request.CreatedAtUnixMS,
		ItemId:           request.ItemID,
		Display:          display,
	}, nil
}

func chatPendingResolvedToProto(resolved domain.ChatPendingResolved) (*pb.ChatPendingResolvedEvent, error) {
	if resolved.PendingRequestID == "" {
		return nil, redactedInternalStatusError()
	}
	pendingType, ok := PendingTypeToProto(resolved.PendingType)
	if !ok {
		return nil, redactedInternalStatusError()
	}
	resolution, ok := PendingResolutionToProto(resolved.Resolution)
	if !ok {
		return nil, redactedInternalStatusError()
	}
	return &pb.ChatPendingResolvedEvent{
		PendingRequestId: resolved.PendingRequestID,
		PendingType:      pendingType,
		Resolution:       resolution,
		DisplayMessage:   resolved.DisplayMessage,
	}, nil
}

func chatTerminalToProto(terminal *domain.ChatTerminal) (*pb.ChatTerminal, error) {
	if terminal == nil {
		return nil, nil
	}
	lifecycle, ok := ChatTurnLifecycleToProto(terminal.State)
	if !ok {
		return nil, redactedInternalStatusError()
	}
	resultSummary := ""
	errorMessage := ""
	if terminal.State == domain.ChatTurnLifecycleCompleted {
		resultSummary = terminal.DisplayMessage
	} else {
		errorMessage = terminal.DisplayMessage
	}
	return &pb.ChatTerminal{
		TerminalLifecycle: lifecycle,
		ResultSummary:     resultSummary,
		ErrorMessage:      errorMessage,
	}, nil
}

func chatCapabilitySetToProto(capabilities domain.ChatCapabilitySet) *pb.ChatCapabilitySet {
	status, _ := ChatCapabilityStateToProto(capabilities.Status)
	history, _ := ChatCapabilityStateToProto(capabilities.History)
	eventStream, _ := ChatCapabilityStateToProto(capabilities.EventStream)
	replay, _ := ChatCapabilityStateToProto(capabilities.Replay)
	pending, _ := ChatCapabilityStateToProto(capabilities.Pending)
	interrupt, _ := ChatCapabilityStateToProto(capabilities.Interrupt)
	return &pb.ChatCapabilitySet{
		Status:      status,
		History:     history,
		EventStream: eventStream,
		Replay:      replay,
		Pending:     pending,
		Interrupt:   interrupt,
	}
}

func chatGatewayLocalToProto(local domain.ChatGatewayLocalState) *pb.ChatGatewayLocalState {
	var signals []pb.ChatGatewayLocalSignal
	if local.Live {
		signals = append(signals, pb.ChatGatewayLocalSignal_CHAT_GATEWAY_LOCAL_SIGNAL_LIVE)
	}
	if local.ReplayAvailable {
		signals = append(signals, pb.ChatGatewayLocalSignal_CHAT_GATEWAY_LOCAL_SIGNAL_REPLAY_AVAILABLE)
	}
	if local.ReplayUnavailable {
		signals = append(signals, pb.ChatGatewayLocalSignal_CHAT_GATEWAY_LOCAL_SIGNAL_REPLAY_UNAVAILABLE)
	}
	return &pb.ChatGatewayLocalState{
		Signals:      signals,
		ProcessEpoch: local.ProcessEpoch,
	}
}

func chatTurnSummaryToProto(turn domain.ChatTurnSummary) (*pb.ChatTurnSummary, error) {
	lifecycle, _ := ChatTurnLifecycleToProto(turn.Lifecycle)
	itemsView, _ := ChatTurnItemsViewToProto(turn.ItemsView)
	return &pb.ChatTurnSummary{
		RunId:             turn.RunID,
		Lifecycle:         lifecycle,
		ItemsView:         itemsView,
		StartedAtUnixMs:   turn.StartedAtUnixMS,
		CompletedAtUnixMs: turn.CompletedAtUnixMS,
		DurationMs:        turn.DurationMS,
		Summary:           turn.Summary,
		Truncated:         turn.Truncated,
		Error:             chatErrorSummaryToProto(turn.Error),
	}, nil
}

func chatErrorSummaryToProto(summary *domain.ChatErrorSummary) *pb.ChatErrorSummary {
	if summary == nil {
		return nil
	}
	return &pb.ChatErrorSummary{
		Reason:         summary.Code,
		DisplayMessage: summary.DisplayMessage,
		Retryable:      summary.Retryable,
	}
}

func chatNarrowedOutcomeToProto(outcome *domain.ChatNarrowedOutcome, returned domain.ChatCapabilityState) *pb.ChatNarrowedOutcome {
	if outcome == nil {
		return nil
	}
	returnedProto, _ := ChatCapabilityStateToProto(returned)
	return &pb.ChatNarrowedOutcome{
		Outcome:        pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_NARROWED,
		Reason:         string(outcome.Reason),
		DisplayMessage: outcome.DisplayMessage,
		Returned:       returnedProto,
		Retryable:      outcome.Retryable,
	}
}

func streamChatEventsMessageToProto(message StreamChatEventsMessage, trustedMetadata domain.SessionGroupMetadata) (*pb.StreamChatEventsResponse, error) {
	payloadCount := 0
	if message.Event != nil {
		payloadCount++
	}
	if message.ReplayNotice != nil {
		payloadCount++
	}
	if message.Narrowed != nil {
		payloadCount++
	}
	if payloadCount != 1 {
		return nil, redactedInternalStatusError()
	}
	if message.Event != nil {
		if message.SessionGroupID != "" && message.SessionGroupID != message.Event.SessionGroupID {
			return nil, redactedInternalStatusError()
		}
		event, err := chatEventToProto(*message.Event)
		if err != nil {
			return nil, err
		}
		response := &pb.StreamChatEventsResponse{
			Payload: &pb.StreamChatEventsResponse_Event{Event: event},
		}
		if err := validateOutboundMessageForTrustedSession(response, trustedMetadata, message.Event.SessionGroupID); err != nil {
			return nil, err
		}
		return response, nil
	}
	if message.ReplayNotice != nil {
		response := &pb.StreamChatEventsResponse{
			Payload: &pb.StreamChatEventsResponse_ReplayNotice{ReplayNotice: chatReplayNoticeToProto(*message.ReplayNotice)},
		}
		if err := validateOutboundMessageForTrustedSession(response, trustedMetadata, message.SessionGroupID); err != nil {
			return nil, err
		}
		return response, nil
	}
	response := &pb.StreamChatEventsResponse{
		Payload: &pb.StreamChatEventsResponse_Narrowed{Narrowed: chatNarrowedOutcomeToProto(message.Narrowed, domain.ChatCapabilityNarrowed)},
	}
	if err := validateOutboundMessageForTrustedSession(response, trustedMetadata, message.SessionGroupID); err != nil {
		return nil, err
	}
	return response, nil
}

func chatEventToProto(event domain.ChatEvent) (*pb.ChatEvent, error) {
	if event.EventID == 0 || event.EventCursor == "" || event.ChatID == "" || event.SessionGroupID == "" || event.WorkspaceID == "" || event.RunID == "" {
		return nil, redactedInternalStatusError()
	}
	protoEvent := &pb.ChatEvent{
		EventId:         event.EventID,
		EventCursor:     event.EventCursor,
		ChatId:          event.ChatID,
		SessionGroupId:  event.SessionGroupID,
		WorkspaceId:     event.WorkspaceID,
		RunId:           event.RunID,
		CreatedAtUnixMs: event.CreatedAtUnixMS,
	}
	payloadCount := 0
	if event.StatusUpdated != nil {
		payloadCount++
		if !chatStatusMatches(*event.StatusUpdated, event.SessionGroupID, event.WorkspaceID, event.ChatID) {
			return nil, redactedInternalStatusError()
		}
		status, err := chatStatusToProto(*event.StatusUpdated)
		if err != nil {
			return nil, err
		}
		protoEvent.Payload = &pb.ChatEvent_StatusUpdated{
			StatusUpdated: &pb.ChatStatusUpdatedEvent{Status: status},
		}
	}
	if event.AssistantDelta != nil {
		payloadCount++
		protoEvent.Payload = &pb.ChatEvent_AssistantDelta{AssistantDelta: &pb.AssistantDeltaEvent{
			TextDelta: event.AssistantDelta.TextDelta,
			Truncated: event.AssistantDelta.Truncated,
		}}
	}
	if event.AssistantMessageCompleted != nil {
		payloadCount++
		protoEvent.Payload = &pb.ChatEvent_AssistantMessageCompleted{AssistantMessageCompleted: &pb.AssistantMessageCompletedEvent{
			Message:   event.AssistantMessageCompleted.Message,
			Truncated: event.AssistantMessageCompleted.Truncated,
		}}
	}
	if event.PendingCreated != nil {
		payloadCount++
		pendingRequest, err := chatPendingRequestToProto(*event.PendingCreated)
		if err != nil {
			return nil, err
		}
		protoEvent.Payload = &pb.ChatEvent_PendingRequestCreated{
			PendingRequestCreated: &pb.ChatPendingCreatedEvent{PendingRequest: pendingRequest},
		}
	}
	if event.PendingResolved != nil {
		payloadCount++
		resolved, err := chatPendingResolvedToProto(*event.PendingResolved)
		if err != nil {
			return nil, err
		}
		protoEvent.Payload = &pb.ChatEvent_PendingRequestResolved{PendingRequestResolved: resolved}
	}
	if event.Terminal != nil {
		payloadCount++
		terminal, err := chatTerminalToProto(event.Terminal)
		if err != nil {
			return nil, err
		}
		protoEvent.Payload = &pb.ChatEvent_Terminal{Terminal: &pb.ChatTerminalEvent{Terminal: terminal}}
	}
	if payloadCount != 1 || protoEvent.Payload == nil {
		return nil, redactedInternalStatusError()
	}
	return protoEvent, nil
}

func chatReplayNoticeToProto(notice domain.ChatReplayNotice) *pb.ChatReplayNotice {
	code, _ := ChatReplayNoticeCodeToProto(notice.Code)
	return &pb.ChatReplayNotice{
		Code:                      code,
		Message:                   notice.Message,
		OldestBufferedEventId:     notice.OldestBufferedEventID,
		NewestBufferedEventId:     notice.NewestBufferedEventID,
		FromStartAvailable:        notice.FromStartAvailable,
		StartEvictedBeforeEventId: notice.StartEvictedBeforeEventID,
		ProcessEpoch:              notice.ProcessEpoch,
	}
}
