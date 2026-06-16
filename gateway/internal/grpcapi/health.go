package grpcapi

import (
	"context"

	"github.com/Dirard/codex-runtime/gateway/internal/appserver"
	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

type gatewayHealthServer struct {
	healthpb.UnimplementedHealthServer
	chatRuntimeDisabled    bool
	chatRuntimeSupervisors []appserver.SupervisorStatusProvider
}

func newGatewayHealthServer(chatRuntimeDisabled bool, supervisors []appserver.SupervisorStatusProvider) healthpb.HealthServer {
	supervisorSnapshot := append([]appserver.SupervisorStatusProvider(nil), supervisors...)
	return &gatewayHealthServer{
		chatRuntimeDisabled:    chatRuntimeDisabled,
		chatRuntimeSupervisors: supervisorSnapshot,
	}
}

func (s *gatewayHealthServer) Check(_ context.Context, request *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	switch request.GetService() {
	case "", pb.CodexControl_ServiceDesc.ServiceName:
		return healthResponse(healthpb.HealthCheckResponse_SERVING), nil
	case pb.ChatRuntimeService_ServiceDesc.ServiceName:
		return healthResponse(s.chatRuntimeStatus()), nil
	default:
		return nil, status.Error(codes.NotFound, "service not found")
	}
}

func (s *gatewayHealthServer) Watch(request *healthpb.HealthCheckRequest, stream healthpb.Health_WatchServer) error {
	response, err := s.Check(stream.Context(), request)
	if err != nil {
		return err
	}
	if err := stream.Send(response); err != nil {
		return err
	}
	<-stream.Context().Done()
	return stream.Context().Err()
}

func (s *gatewayHealthServer) chatRuntimeStatus() healthpb.HealthCheckResponse_ServingStatus {
	if s.chatRuntimeDisabled {
		return healthpb.HealthCheckResponse_NOT_SERVING
	}
	for _, supervisor := range s.chatRuntimeSupervisors {
		if supervisor == nil {
			continue
		}
		status := supervisor.Status()
		switch status.State {
		case appserver.SupervisorStateBackoff, appserver.SupervisorStateClosed:
			return healthpb.HealthCheckResponse_NOT_SERVING
		}
	}
	return healthpb.HealthCheckResponse_SERVING
}

func healthResponse(servingStatus healthpb.HealthCheckResponse_ServingStatus) *healthpb.HealthCheckResponse {
	return &healthpb.HealthCheckResponse{Status: servingStatus}
}
