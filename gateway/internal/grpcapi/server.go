package grpcapi

import (
	"fmt"
	"net"
	"sort"

	"github.com/Dirard/codex-runtime/gateway/internal/appserver"
	"github.com/Dirard/codex-runtime/gateway/internal/config"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type ServerOptions struct {
	ListenAddress          string
	AuthToken              string
	MaxRecvMessageBytes    int
	MaxSendMessageBytes    int
	Services               ControlServices
	ChatRuntimeDisabled    bool
	ChatRuntimeService     pb.ChatRuntimeServiceServer
	WorkflowRuntimeService pb.WorkflowRuntimeServiceServer
	ChatRuntimeSupervisors []appserver.SupervisorStatusProvider
}

type Server struct {
	listener            net.Listener
	grpcServer          *grpc.Server
	maxRecvMessageBytes int
	maxSendMessageBytes int
}

func NewServerFromConfig(validated *config.ValidatedConfig, tasks TaskService, pending PendingService) (*Server, error) {
	return NewServerFromConfigWithOptions(validated, tasks, pending, ServerOptions{})
}

func NewServerFromConfigWithOptions(validated *config.ValidatedConfig, tasks TaskService, pending PendingService, extra ServerOptions) (*Server, error) {
	if validated == nil {
		return nil, fmt.Errorf("validated config is required")
	}
	sessionMetadata, err := sessionMetadataFromConfig(validated)
	if err != nil {
		return nil, err
	}
	resolver := SessionGroupResolverFunc(func(sessionGroupID string) (domain.SessionGroupMetadata, bool) {
		metadata, ok := sessionMetadata[sessionGroupID]
		return metadata, ok
	})
	maxRecvMessageBytes, maxSendMessageBytes, err := effectiveMessageLimitsFromConfig(validated)
	if err != nil {
		return nil, err
	}
	options := ServerOptions{
		ListenAddress:       validated.Listen,
		AuthToken:           validated.ClientAuthTokenForAuth(),
		MaxRecvMessageBytes: maxRecvMessageBytes,
		MaxSendMessageBytes: maxSendMessageBytes,
		Services: ControlServices{
			SessionGroups: resolver,
			Tasks:         tasks,
			Pending:       pending,
		},
		ChatRuntimeDisabled:    !validated.ChatRuntimeEnabled(),
		ChatRuntimeService:     extra.ChatRuntimeService,
		WorkflowRuntimeService: extra.WorkflowRuntimeService,
	}
	if extra.ChatRuntimeDisabled {
		options.ChatRuntimeDisabled = true
	}
	if len(extra.ChatRuntimeSupervisors) > 0 {
		options.ChatRuntimeSupervisors = append([]appserver.SupervisorStatusProvider(nil), extra.ChatRuntimeSupervisors...)
	}
	return NewServer(options)
}

func NewServer(options ServerOptions) (*Server, error) {
	if err := config.ValidateListenAddress(options.ListenAddress); err != nil {
		return nil, fmt.Errorf("listen address: %w", err)
	}
	if options.AuthToken == "" {
		return nil, fmt.Errorf("auth token is required")
	}
	if options.MaxRecvMessageBytes <= 0 {
		return nil, fmt.Errorf("max receive message bytes must be positive")
	}
	if options.MaxRecvMessageBytes > config.MaxGRPCMessageBytes {
		return nil, fmt.Errorf("max receive message bytes exceeds hard cap")
	}
	if options.MaxSendMessageBytes <= 0 {
		return nil, fmt.Errorf("max send message bytes must be positive")
	}
	if options.MaxSendMessageBytes > config.MaxGRPCMessageBytes {
		return nil, fmt.Errorf("max send message bytes exceeds hard cap")
	}
	if err := validateControlServices(options.Services); err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", options.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(config.MaxGRPCMessageBytes),
		grpc.MaxSendMsgSize(config.MaxGRPCMessageBytes),
		grpc.ChainUnaryInterceptor(recoveryUnaryInterceptor(), authUnaryInterceptor(options.AuthToken)),
		grpc.ChainStreamInterceptor(recoveryStreamInterceptor(), authStreamInterceptor(options.AuthToken)),
	)
	pb.RegisterCodexControlServer(grpcServer, newCodexControlService(options.Services, options.MaxRecvMessageBytes, options.MaxSendMessageBytes))
	chatRuntime := options.ChatRuntimeService
	if options.ChatRuntimeDisabled || chatRuntime == nil {
		chatRuntime = newChatRuntimeService(!options.ChatRuntimeDisabled, options.MaxRecvMessageBytes)
	}
	pb.RegisterChatRuntimeServiceServer(grpcServer, chatRuntime)
	workflowRuntime := options.WorkflowRuntimeService
	if workflowRuntime == nil {
		workflowRuntime = newWorkflowRuntimeService()
	}
	pb.RegisterWorkflowRuntimeServiceServer(grpcServer, workflowRuntime)
	healthpb.RegisterHealthServer(grpcServer, newGatewayHealthServer(options.ChatRuntimeDisabled, options.ChatRuntimeSupervisors))
	return &Server{
		listener:            listener,
		grpcServer:          grpcServer,
		maxRecvMessageBytes: options.MaxRecvMessageBytes,
		maxSendMessageBytes: options.MaxSendMessageBytes,
	}, nil
}

func sessionMetadataFromConfig(validated *config.ValidatedConfig) (map[string]domain.SessionGroupMetadata, error) {
	if validated == nil {
		return nil, fmt.Errorf("validated config is required")
	}
	if len(validated.SessionGroups) == 0 {
		return nil, fmt.Errorf("at least one session group is required for gRPC message limits")
	}
	sessionMetadata := make(map[string]domain.SessionGroupMetadata, len(validated.SessionGroups))
	for _, group := range validated.SessionGroups {
		inboundLimit, err := int64ToPositiveInt(group.GRPCLimits.InboundMessageBytes, "inbound gRPC message limit")
		if err != nil {
			return nil, fmt.Errorf("session group %q: %w", group.SessionGroupID, err)
		}
		if inboundLimit > config.MaxGRPCMessageBytes {
			return nil, fmt.Errorf("session group %q inbound gRPC message limit exceeds hard cap", group.SessionGroupID)
		}
		outboundLimit, err := int64ToPositiveInt(group.GRPCLimits.OutboundMessageBytes, "outbound gRPC message limit")
		if err != nil {
			return nil, fmt.Errorf("session group %q: %w", group.SessionGroupID, err)
		}
		if outboundLimit > config.MaxGRPCMessageBytes {
			return nil, fmt.Errorf("session group %q outbound gRPC message limit exceeds hard cap", group.SessionGroupID)
		}
		sessionMetadata[group.SessionGroupID] = domain.SessionGroupMetadata{
			SessionGroupID:           group.SessionGroupID,
			WorkspaceID:              group.WorkspaceID,
			GRPCInboundMessageBytes:  inboundLimit,
			GRPCOutboundMessageBytes: outboundLimit,
		}
	}
	return sessionMetadata, nil
}

func effectiveMessageLimitsFromConfig(validated *config.ValidatedConfig) (int, int, error) {
	if validated == nil {
		return 0, 0, fmt.Errorf("validated config is required")
	}
	if len(validated.SessionGroups) == 0 {
		return 0, 0, fmt.Errorf("at least one session group is required for gRPC message limits")
	}
	maxRecvMessageBytes := 0
	maxSendMessageBytes := 0
	if validated.WorkflowGRPCMessageBytes > 0 {
		workflowLimit, err := int64ToPositiveInt(validated.WorkflowGRPCMessageBytes, "workflow gRPC message limit")
		if err != nil {
			return 0, 0, err
		}
		if workflowLimit > config.MaxGRPCMessageBytes {
			return 0, 0, fmt.Errorf("workflow gRPC message limit exceeds hard cap")
		}
		maxRecvMessageBytes = max(maxRecvMessageBytes, workflowLimit)
		maxSendMessageBytes = max(maxSendMessageBytes, workflowLimit)
	}
	for _, group := range validated.SessionGroups {
		if group.GRPCLimits.InboundMessageBytes <= 0 {
			return 0, 0, fmt.Errorf("session group %q inbound gRPC message limit must be positive", group.SessionGroupID)
		}
		if group.GRPCLimits.OutboundMessageBytes <= 0 {
			return 0, 0, fmt.Errorf("session group %q outbound gRPC message limit must be positive", group.SessionGroupID)
		}
		inboundLimit, err := int64ToPositiveInt(group.GRPCLimits.InboundMessageBytes, "inbound gRPC message limit")
		if err != nil {
			return 0, 0, fmt.Errorf("session group %q: %w", group.SessionGroupID, err)
		}
		outboundLimit, err := int64ToPositiveInt(group.GRPCLimits.OutboundMessageBytes, "outbound gRPC message limit")
		if err != nil {
			return 0, 0, fmt.Errorf("session group %q: %w", group.SessionGroupID, err)
		}
		maxRecvMessageBytes = max(maxRecvMessageBytes, inboundLimit)
		maxSendMessageBytes = max(maxSendMessageBytes, outboundLimit)
	}
	if maxRecvMessageBytes <= 0 || maxSendMessageBytes <= 0 {
		return 0, 0, fmt.Errorf("gRPC message limits must be positive")
	}
	return maxRecvMessageBytes, maxSendMessageBytes, nil
}

func int64ToPositiveInt(value int64, field string) (int, error) {
	maxInt := int64(^uint(0) >> 1)
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", field)
	}
	if value > maxInt {
		return 0, fmt.Errorf("%s exceeds platform int range", field)
	}
	return int(value), nil
}

func validateControlServices(services ControlServices) error {
	if services.SessionGroups == nil {
		return fmt.Errorf("session group resolver is required")
	}
	if services.Tasks == nil {
		return fmt.Errorf("task service is required")
	}
	if services.Pending == nil {
		return fmt.Errorf("pending service is required")
	}
	return nil
}

func (s *Server) Serve() error {
	return s.grpcServer.Serve(s.listener)
}

func (s *Server) Stop() {
	s.grpcServer.Stop()
	_ = s.listener.Close()
}

func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}

func (s *Server) ServiceNames() []string {
	serviceInfo := s.grpcServer.GetServiceInfo()
	names := make([]string, 0, len(serviceInfo))
	for name := range serviceInfo {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
