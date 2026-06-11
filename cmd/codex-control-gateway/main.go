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

	"github.com/Dirard/codex-runtime/internal/appserver"
	"github.com/Dirard/codex-runtime/internal/config"
	"github.com/Dirard/codex-runtime/internal/grpcapi"
	"github.com/Dirard/codex-runtime/internal/tasks"
	"google.golang.org/grpc"
)

const shutdownWait = 5 * time.Second

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
	flags := flag.NewFlagSet("codex-control-gateway", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&configPath, "config", "", "trusted gateway TOML config path")
	flags.StringVar(&listenOverride, "listen", "", "optional loopback listen address override")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected positional argument: %s\n", flags.Arg(0))
		return 2
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
	newServer     func(*config.ValidatedConfig, grpcapi.TaskService, grpcapi.PendingService) (runtimeServer, error)
	signalContext func(context.Context) (context.Context, context.CancelFunc)
}

func defaultRuntimeDependencies() runtimeDependencies {
	return runtimeDependencies{
		loadConfig: config.LoadFile,
		newSupervisor: func(ctx context.Context, validated *config.ValidatedConfig, group config.SessionGroup, options appserver.ProcessOptions) (runtimeSupervisor, string, error) {
			return appserver.NewProcessSupervisor(ctx, validated, group, options)
		},
		newServer: func(validated *config.ValidatedConfig, taskService grpcapi.TaskService, pendingService grpcapi.PendingService) (runtimeServer, error) {
			return grpcapi.NewServerFromConfig(validated, taskService, pendingService)
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
	sessions := make([]tasks.Session, 0, len(validated.SessionGroups))
	for _, group := range validated.SessionGroups {
		supervisor, _, err := dependencies.newSupervisor(ctx, validated, group, appserver.ProcessOptions{
			StderrSink: stderrSink(stderr),
		})
		if err != nil {
			closeSupervisors(supervisors)
			return nil, fmt.Errorf("session group %q: %w", group.SessionGroupID, err)
		}
		supervisors = append(supervisors, supervisor)
		sessions = append(sessions, tasks.Session{
			Group:      group,
			Supervisor: supervisor,
		})
	}

	service, err := tasks.NewService(sessions)
	if err != nil {
		closeSupervisors(supervisors)
		return nil, err
	}
	server, err := dependencies.newServer(validated, service, service)
	if err != nil {
		closeSupervisors(supervisors)
		return nil, err
	}
	return &gatewayRuntime{server: server, supervisors: supervisors}, nil
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
