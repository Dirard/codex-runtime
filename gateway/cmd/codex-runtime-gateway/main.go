package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Dirard/codex-runtime/gateway/internal/appserver"
	"github.com/Dirard/codex-runtime/gateway/internal/chatruntime"
	"github.com/Dirard/codex-runtime/gateway/internal/chatstate"
	"github.com/Dirard/codex-runtime/gateway/internal/config"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	"github.com/Dirard/codex-runtime/gateway/internal/grpcapi"
	"github.com/Dirard/codex-runtime/gateway/internal/tasks"
	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc"
)

const shutdownWait = 5 * time.Second

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "source"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	return runContext(context.Background(), args, stdout, stderr, defaultRuntimeDependencies())
}

func runContext(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer, dependencies runtimeDependencies) int {
	if ctx == nil {
		ctx = context.Background()
	}
	dependencies = dependencies.withDefaults()

	var configPath string
	var listenOverride string
	var showVersion bool
	flags := flag.NewFlagSet("codex-runtime-gateway", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&configPath, "config", "", "trusted gateway TOML config path")
	flags.StringVar(&listenOverride, "listen", "", "optional loopback listen address override")
	flags.BoolVar(&showVersion, "version", false, "print version information and exit")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected positional argument: %s\n", flags.Arg(0))
		return 2
	}

	if showVersion {
		fmt.Fprintf(stdout, "codex-runtime-gateway %s (commit %s, built %s by %s)\n", version, commit, date, builtBy)
		return 0
	}

	if configPath == "" {
		fmt.Fprintln(stderr, "--config is required")
		return 2
	}

	options := []config.LoadOption{}
	if listenOverride != "" {
		options = append(options, config.WithListenOverride(listenOverride))
	}

	runtimeCtx, stopSignals := dependencies.signalContext(ctx)
	defer stopSignals()

	validated, err := dependencies.loadConfig(configPath, options...)
	if err != nil {
		fmt.Fprintf(stderr, "configuration invalid: %v\n", err)
		return 1
	}

	runtime, err := composeRuntime(runtimeCtx, validated, stderr, dependencies)
	if err != nil {
		fmt.Fprintf(stderr, "gateway startup failed: %v\n", err)
		return 1
	}
	return serveRuntime(runtimeCtx, runtime, stdout, stderr)
}

type runtimeSupervisor interface {
	tasks.AppServerSupervisor
	appserver.SupervisorStatusProvider
	Close() error
}

type runtimeServer interface {
	Serve() error
	Stop()
	Addr() net.Addr
}

type runtimeDependencies struct {
	loadConfig    func(string, ...config.LoadOption) (*config.ValidatedConfig, error)
	newSupervisor func(context.Context, *config.ValidatedConfig, config.SessionGroup, appserver.ProcessOptions) (runtimeSupervisor, string, error)
	newServer     func(*config.ValidatedConfig, grpcapi.TaskService, grpcapi.PendingService, pb.ChatRuntimeServiceServer, []appserver.SupervisorStatusProvider) (runtimeServer, error)
	signalContext func(context.Context) (context.Context, context.CancelFunc)
}

func defaultRuntimeDependencies() runtimeDependencies {
	return runtimeDependencies{
		loadConfig: config.LoadFile,
		newSupervisor: func(ctx context.Context, validated *config.ValidatedConfig, group config.SessionGroup, options appserver.ProcessOptions) (runtimeSupervisor, string, error) {
			return appserver.NewProcessSupervisor(ctx, validated, group, options)
		},
		newServer: func(
			validated *config.ValidatedConfig,
			taskService grpcapi.TaskService,
			pendingService grpcapi.PendingService,
			chatRuntime pb.ChatRuntimeServiceServer,
			supervisors []appserver.SupervisorStatusProvider,
		) (runtimeServer, error) {
			return grpcapi.NewServerFromConfigWithOptions(validated, taskService, pendingService, grpcapi.ServerOptions{
				ChatRuntimeService:     chatRuntime,
				ChatRuntimeSupervisors: supervisors,
			})
		},
		signalContext: signalContext,
	}
}

func (d runtimeDependencies) withDefaults() runtimeDependencies {
	defaults := defaultRuntimeDependencies()
	if d.loadConfig == nil {
		d.loadConfig = defaults.loadConfig
	}
	if d.newSupervisor == nil {
		d.newSupervisor = defaults.newSupervisor
	}
	if d.newServer == nil {
		d.newServer = defaults.newServer
	}
	if d.signalContext == nil {
		d.signalContext = defaults.signalContext
	}
	return d
}

func signalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}

type gatewayRuntime struct {
	server      runtimeServer
	supervisors []runtimeSupervisor
}

func composeRuntime(ctx context.Context, validated *config.ValidatedConfig, stderr io.Writer, dependencies runtimeDependencies) (*gatewayRuntime, error) {
	supervisors := make([]runtimeSupervisor, 0, len(validated.SessionGroups))
	chatSupervisors := make([]appserver.SupervisorStatusProvider, 0, len(validated.SessionGroups))
	sessions := make([]tasks.Session, 0, len(validated.SessionGroups))
	chatSessions := make([]chatruntime.Session, 0, len(validated.SessionGroups))
	for _, group := range validated.SessionGroups {
		supervisor, _, err := dependencies.newSupervisor(ctx, validated, group, appserver.ProcessOptions{
			StderrSink: stderrSink(stderr),
		})
		if err != nil {
			closeSupervisors(supervisors)
			return nil, fmt.Errorf("session group %q: %w", group.SessionGroupID, err)
		}
		supervisors = append(supervisors, supervisor)
		chatSupervisors = append(chatSupervisors, supervisor)
		sessions = append(sessions, tasks.Session{
			Group:      group,
			Supervisor: supervisor,
		})
		chatSessions = append(chatSessions, chatruntime.Session{
			Group:              group,
			ConnectionProvider: chatruntime.NewAppServerConnectionProvider(supervisor),
		})
	}

	service, err := tasks.NewService(sessions)
	if err != nil {
		closeSupervisors(supervisors)
		return nil, err
	}
	chatService, err := chatruntime.NewService(chatSessions, chatruntime.ServiceOptions{
		Store: chatstate.NewStore(chatstate.StoreOptions{}),
	})
	if err != nil {
		closeSupervisors(supervisors)
		return nil, err
	}
	maxRecvMessageBytes, maxSendMessageBytes, err := grpcMessageLimits(validated)
	if err != nil {
		closeSupervisors(supervisors)
		return nil, err
	}
	chatRuntimeGRPC := grpcapi.NewChatRuntimeService(grpcapi.ChatRuntimeServiceOptions{
		Enabled:             validated.ChatRuntimeEnabled(),
		MaxRecvMessageBytes: maxRecvMessageBytes,
		MaxSendMessageBytes: maxSendMessageBytes,
		SessionGroups:       sessionResolver(validated),
		Runtime:             chatService,
	})
	server, err := dependencies.newServer(validated, service, service, chatRuntimeGRPC, chatSupervisors)
	if err != nil {
		closeSupervisors(supervisors)
		return nil, err
	}
	return &gatewayRuntime{server: server, supervisors: supervisors}, nil
}

func sessionResolver(validated *config.ValidatedConfig) grpcapi.SessionGroupResolver {
	metadata := make(map[string]domain.SessionGroupMetadata, len(validated.SessionGroups))
	for _, group := range validated.SessionGroups {
		metadata[group.SessionGroupID] = domain.SessionGroupMetadata{
			SessionGroupID:           group.SessionGroupID,
			WorkspaceID:              group.WorkspaceID,
			GRPCInboundMessageBytes:  int(group.GRPCLimits.InboundMessageBytes),
			GRPCOutboundMessageBytes: int(group.GRPCLimits.OutboundMessageBytes),
		}
	}
	return grpcapi.SessionGroupResolverFunc(func(sessionGroupID string) (domain.SessionGroupMetadata, bool) {
		group, ok := metadata[sessionGroupID]
		return group, ok
	})
}

func grpcMessageLimits(validated *config.ValidatedConfig) (int, int, error) {
	maxRecvMessageBytes := 0
	maxSendMessageBytes := 0
	for _, group := range validated.SessionGroups {
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

func stderrSink(stderr io.Writer) func(string) {
	if stderr == nil {
		return nil
	}
	return func(chunk string) {
		_, _ = fmt.Fprint(stderr, chunk)
	}
}

func serveRuntime(ctx context.Context, runtime *gatewayRuntime, stdout io.Writer, stderr io.Writer) int {
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- runtime.server.Serve()
	}()
	if stdout != nil {
		fmt.Fprintf(stdout, "gateway listening on %s\n", runtime.server.Addr())
	}

	select {
	case err := <-serveErr:
		runtime.Close()
		if serveStopped(err) {
			return 0
		}
		fmt.Fprintf(stderr, "gateway serve failed: %v\n", err)
		return 1
	case <-ctx.Done():
		runtime.Close()
		if err := waitForServeStop(serveErr); err != nil && !serveStopped(err) {
			fmt.Fprintf(stderr, "gateway shutdown failed: %v\n", err)
			return 1
		}
		return 0
	}
}

func (r *gatewayRuntime) Close() {
	if r == nil {
		return
	}
	if r.server != nil {
		r.server.Stop()
	}
	closeSupervisors(r.supervisors)
}

func closeSupervisors(supervisors []runtimeSupervisor) {
	for _, supervisor := range supervisors {
		if supervisor != nil {
			_ = supervisor.Close()
		}
	}
}

func waitForServeStop(serveErr <-chan error) error {
	select {
	case err := <-serveErr:
		return err
	case <-time.After(shutdownWait):
		return errors.New("gRPC server did not stop before timeout")
	}
}

func serveStopped(err error) bool {
	return err == nil || errors.Is(err, grpc.ErrServerStopped)
}
