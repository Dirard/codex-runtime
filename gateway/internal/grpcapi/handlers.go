package grpcapi

import (
	"context"
	"io"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc"
)

type codexControlService struct {
	pb.UnimplementedCodexControlServer
	services            ControlServices
	maxRecvMessageBytes int
	maxSendMessageBytes int
}

func newCodexControlService(services ControlServices, maxRecvMessageBytes int, maxSendMessageBytes int) *codexControlService {
	return &codexControlService{
		services:            services,
		maxRecvMessageBytes: maxRecvMessageBytes,
		maxSendMessageBytes: maxSendMessageBytes,
	}
}

func (s *codexControlService) StartTask(ctx context.Context, req *pb.StartTaskRequest) (*pb.StartTaskResponse, error) {
	if requestError := validateInboundMessageForProcess(req, s.maxRecvMessageBytes); requestError != nil {
		return nil, StatusErrorFromRequestError(requestError)
	}
	command, requestError := ValidateStartTask(req, s.services.SessionGroups)
	if requestError != nil {
		return nil, StatusErrorFromRequestError(requestError)
	}
	trustedMetadata, requestError := trustedSessionMetadataForSessionGroupID(command.SessionGroupID, s.services.SessionGroups)
	if requestError != nil {
		return nil, StatusErrorFromRequestError(requestError)
	}
	response, err := s.services.Tasks.StartTask(ctx, command)
	if err != nil {
		return nil, statusErrorFromServiceError(err)
	}
	protoResponse, failure := StartTaskResponseToProtoWithFailure(response)
	if failure != nil {
		return nil, statusErrorFromMappingFailure(failure)
	}
	if err := validateOutboundMessageForTrustedSession(protoResponse, trustedMetadata, response.SessionGroupID); err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForProcess(protoResponse, s.maxSendMessageBytes); err != nil {
		return nil, err
	}
	return protoResponse, nil
}

func (s *codexControlService) StreamTask(req *pb.StreamTaskRequest, stream grpc.ServerStreamingServer[pb.StreamTaskResponse]) error {
	if requestError := validateInboundMessageForProcess(req, s.maxRecvMessageBytes); requestError != nil {
		return StatusErrorFromRequestError(requestError)
	}
	command, requestError := ValidateStreamTask(req)
	if requestError != nil {
		return StatusErrorFromRequestError(requestError)
	}
	subscriber, err := s.services.Tasks.StreamTask(stream.Context(), command)
	if err != nil {
		return statusErrorFromServiceError(err)
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
			return statusErrorFromServiceError(err)
		}
		protoMessage, err := streamMessageToProto(message, s.services.SessionGroups)
		if err != nil {
			return err
		}
		if err := validateOutboundMessageForProcess(protoMessage, s.maxSendMessageBytes); err != nil {
			return err
		}
		if err := stream.Send(protoMessage); err != nil {
			if stream.Context().Err() != nil {
				return statusErrorFromServiceError(stream.Context().Err())
			}
			return statusErrorFromServiceError(err)
		}
	}
}

func streamMessageToProto(message StreamTaskMessage, resolver SessionGroupResolver) (*pb.StreamTaskResponse, error) {
	hasEvent := message.Event != nil
	hasReplayNotice := message.ReplayNotice != nil
	if hasEvent == hasReplayNotice {
		return nil, redactedInternalStatusError()
	}
	if hasEvent {
		if message.SessionGroupID != "" && message.SessionGroupID != message.Event.SessionGroupID {
			return nil, redactedInternalStatusError()
		}
		protoMessage, failure := StreamTaskResponseEventToProtoWithFailure(*message.Event)
		if failure != nil {
			return nil, statusErrorFromMappingFailure(failure)
		}
		if err := validateOutboundMessageForSession(protoMessage, resolver, message.Event.SessionGroupID); err != nil {
			return nil, err
		}
		return protoMessage, nil
	}
	protoMessage, failure := StreamTaskResponseReplayNoticeToProtoWithFailure(*message.ReplayNotice)
	if failure != nil {
		return nil, statusErrorFromMappingFailure(failure)
	}
	if err := validateOutboundMessageForSession(protoMessage, resolver, message.SessionGroupID); err != nil {
		return nil, err
	}
	return protoMessage, nil
}

func (s *codexControlService) RespondPendingRequest(ctx context.Context, req *pb.RespondPendingRequestRequest) (*pb.RespondPendingRequestResponse, error) {
	if requestError := validateInboundMessageForProcess(req, s.maxRecvMessageBytes); requestError != nil {
		return nil, StatusErrorFromRequestError(requestError)
	}
	command, requestError := ValidateRespondPendingRequest(req)
	if requestError != nil {
		return nil, StatusErrorFromRequestError(requestError)
	}
	metadata, err := s.services.Pending.ResolvePendingRequestSession(ctx, command.TaskID, command.PendingRequestID)
	if err != nil {
		return nil, statusErrorFromServiceError(err)
	}
	if requestError := validateInboundMessageForSession(req, metadata); requestError != nil {
		return nil, StatusErrorFromRequestError(requestError)
	}
	response, err := s.services.Pending.RespondPendingRequest(ctx, command)
	if err != nil {
		return nil, statusErrorFromServiceError(err)
	}
	protoResponse, failure := RespondPendingRequestResponseToProtoWithFailure(response)
	if failure != nil {
		return nil, statusErrorFromMappingFailure(failure)
	}
	if err := validateOutboundMessageForTrustedSession(protoResponse, metadata, response.SessionGroupID); err != nil {
		return nil, err
	}
	if err := validateOutboundMessageForProcess(protoResponse, s.maxSendMessageBytes); err != nil {
		return nil, err
	}
	return protoResponse, nil
}

func (s *codexControlService) InterruptTask(ctx context.Context, req *pb.InterruptTaskRequest) (*pb.InterruptTaskResponse, error) {
	if requestError := validateInboundMessageForProcess(req, s.maxRecvMessageBytes); requestError != nil {
		return nil, StatusErrorFromRequestError(requestError)
	}
	command, requestError := ValidateInterruptTask(req)
	if requestError != nil {
		return nil, StatusErrorFromRequestError(requestError)
	}
	trustedMetadata, hasTrustedMetadata, requestError := trustedSessionMetadataForKnownLocator(command.Locator, s.services.SessionGroups)
	if requestError != nil {
		return nil, StatusErrorFromRequestError(requestError)
	}
	if hasTrustedMetadata {
		requestError = validateInboundMessageForSession(req, trustedMetadata)
	}
	if requestError != nil {
		return nil, StatusErrorFromRequestError(requestError)
	}
	response, err := s.services.Tasks.InterruptTask(ctx, command)
	if err != nil {
		return nil, statusErrorFromServiceError(err)
	}
	protoResponse, failure := InterruptTaskResponseToProtoWithFailure(response)
	if failure != nil {
		return nil, statusErrorFromMappingFailure(failure)
	}
	if hasTrustedMetadata {
		if err := validateOutboundMessageForTrustedSession(protoResponse, trustedMetadata, response.SessionGroupID); err != nil {
			return nil, err
		}
	} else {
		if err := validateOutboundMessageForSession(protoResponse, s.services.SessionGroups, response.SessionGroupID); err != nil {
			return nil, err
		}
	}
	if err := validateOutboundMessageForProcess(protoResponse, s.maxSendMessageBytes); err != nil {
		return nil, err
	}
	return protoResponse, nil
}

func (s *codexControlService) GetTaskStatus(ctx context.Context, req *pb.GetTaskStatusRequest) (*pb.GetTaskStatusResponse, error) {
	if requestError := validateInboundMessageForProcess(req, s.maxRecvMessageBytes); requestError != nil {
		return nil, StatusErrorFromRequestError(requestError)
	}
	command, requestError := ValidateGetTaskStatus(req)
	if requestError != nil {
		return nil, StatusErrorFromRequestError(requestError)
	}
	trustedMetadata, hasTrustedMetadata, requestError := trustedSessionMetadataForKnownLocator(command.Locator, s.services.SessionGroups)
	if requestError != nil {
		return nil, StatusErrorFromRequestError(requestError)
	}
	if hasTrustedMetadata {
		requestError = validateInboundMessageForSession(req, trustedMetadata)
	}
	if requestError != nil {
		return nil, StatusErrorFromRequestError(requestError)
	}
	response, err := s.services.Tasks.GetTaskStatus(ctx, command)
	if err != nil {
		return nil, statusErrorFromServiceError(err)
	}
	protoResponse, failure := GetTaskStatusResponseToProtoWithFailure(response)
	if failure != nil {
		return nil, statusErrorFromMappingFailure(failure)
	}
	if hasTrustedMetadata {
		if err := validateOutboundMessageForTrustedSession(protoResponse, trustedMetadata, response.SessionGroupID); err != nil {
			return nil, err
		}
	} else {
		if err := validateOutboundMessageForSession(protoResponse, s.services.SessionGroups, response.SessionGroupID); err != nil {
			return nil, err
		}
	}
	if err := validateOutboundMessageForProcess(protoResponse, s.maxSendMessageBytes); err != nil {
		return nil, err
	}
	return protoResponse, nil
}
