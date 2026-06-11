package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"github.com/Dirard/codex-runtime/internal/appserver"
	"github.com/Dirard/codex-runtime/internal/config"
	"github.com/Dirard/codex-runtime/internal/grpcapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestRunRejectsUnexpectedPositionalArgs(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"unexpected.toml"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run() exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unexpected positional argument") {
		t.Fatalf("stderr = %q, want unexpected positional argument", stderr.String())
	}
}

func TestRunContextComposesGatewayAndBlocksUntilShutdown(t *testing.T) {
	configPath, _ := writeGatewayConfig(t, []gatewaySessionSpec{
		{id: "sg-1", workspace: "ws-1"},
		{id: "sg-2", workspace: "ws-2"},
	})
	server := newBlockingRuntimeServer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var supervisorGroups []string
	var supervisors []*fakeRuntimeSupervisor
	var serverFactoryCalled atomic.Bool
	dependencies := runtimeDependencies{
		newSupervisor: func(ctx context.Context, validated *config.ValidatedConfig, group config.SessionGroup, options appserver.ProcessOptions) (runtimeSupervisor, string, error) {
			if options.StderrSink == nil {
				return nil, "", fmt.Errorf("NewProcessSupervisor options missing stderr sink")
			}
			supervisor := &fakeRuntimeSupervisor{}
			supervisorGroups = append(supervisorGroups, group.SessionGroupID)
			supervisors = append(supervisors, supervisor)
			return supervisor, "fixture-version", nil
		},
		newServer: func(validated *config.ValidatedConfig, taskService grpcapi.TaskService, pendingService grpcapi.PendingService) (runtimeServer, error) {
			serverFactoryCalled.Store(true)
			if len(validated.SessionGroups) != 2 {
				return nil, fmt.Errorf("NewServerFromConfig session group count = %d, want 2", len(validated.SessionGroups))
			}
			if reflect.ValueOf(taskService).Pointer() != reflect.ValueOf(pendingService).Pointer() {
				return nil, fmt.Errorf("task and pending services should be the same runtime service")
			}
			return server, nil
		},
		signalContext: noSignalContext,
	}
	done := make(chan int, 1)
	go func() {
		done <- runContext(ctx, []string{"--config", configPath}, &stdout, &stderr, dependencies)
	}()
	waitForChannelClosed(t, server.started, "server start")

	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("runContext() exit code = %d, stderr = %q", code, stderr.String())
		}
	case <-time.After(time.Second):
		t.Fatal("runContext() did not exit after context cancellation")
	}
	if !serverFactoryCalled.Load() {
		t.Fatal("server factory was not called")
	}
	wantGroups := []string{"sg-1", "sg-2"}
	if !reflect.DeepEqual(supervisorGroups, wantGroups) {
		t.Fatalf("supervisor groups = %#v, want %#v", supervisorGroups, wantGroups)
	}
	for index, supervisor := range supervisors {
		if !supervisor.closed.Load() {
			t.Fatalf("supervisor %d was not closed on shutdown", index)
		}
	}
	if !strings.Contains(stdout.String(), "gateway listening on 127.0.0.1:40001") {
		t.Fatalf("stdout = %q, want listening address", stdout.String())
	}
	if strings.Contains(stdout.String(), "configuration valid") {
		t.Fatalf("stdout = %q, must not report config-only success", stdout.String())
	}
}

func TestRunContextStartsAuthenticatedGRPCGateway(t *testing.T) {
	configPath, token := writeGatewayConfig(t, []gatewaySessionSpec{{id: "sg-1", workspace: "ws-1"}})
	address := freeLoopbackAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	dependencies := runtimeDependencies{
		newSupervisor: func(ctx context.Context, validated *config.ValidatedConfig, group config.SessionGroup, options appserver.ProcessOptions) (runtimeSupervisor, string, error) {
			return &fakeRuntimeSupervisor{}, "fixture-version", nil
		},
		signalContext: noSignalContext,
	}
	done := make(chan int, 1)
	go func() {
		done <- runContext(ctx, []string{"--config", configPath, "--listen", address}, &stdout, &stderr, dependencies)
	}()

	waitForGatewayServing(t, address, token, &stderr)
	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("runContext() exit code = %d, stderr = %q", code, stderr.String())
		}
	case <-time.After(time.Second):
		t.Fatal("runContext() did not stop after context cancellation")
	}
	if !strings.Contains(stdout.String(), "gateway listening on "+address) {
		t.Fatalf("stdout = %q, want concrete listen address %q", stdout.String(), address)
	}
	if strings.Contains(stdout.String(), "configuration valid") {
		t.Fatalf("stdout = %q, must not report config-only success", stdout.String())
	}
}

type fakeRuntimeSupervisor struct {
	closed atomic.Bool
}

func (s *fakeRuntimeSupervisor) Connection(ctx context.Context) (*appserver.Connection, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *fakeRuntimeSupervisor) MarkClosed(*appserver.Connection) {}

func (s *fakeRuntimeSupervisor) Close() error {
	s.closed.Store(true)
	return nil
}

type blockingRuntimeServer struct {
	started chan struct{}
	stopped chan struct{}
	once    sync.Once
	addr    net.Addr
}

func newBlockingRuntimeServer() *blockingRuntimeServer {
	return &blockingRuntimeServer{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
		addr:    fakeAddr("127.0.0.1:40001"),
	}
}

func (s *blockingRuntimeServer) Serve() error {
	close(s.started)
	<-s.stopped
	return grpc.ErrServerStopped
}

func (s *blockingRuntimeServer) Stop() {
	s.once.Do(func() {
		close(s.stopped)
	})
}

func (s *blockingRuntimeServer) Addr() net.Addr {
	return s.addr
}

type fakeAddr string

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return string(a) }

type gatewaySessionSpec struct {
	id        string
	workspace string
}

func writeGatewayConfig(t *testing.T, specs []gatewaySessionSpec) (string, string) {
	t.Helper()
	tempDir := t.TempDir()
	token := "runtime-fixture-token"
	tokenFile := filepath.Join(tempDir, "token.txt")
	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(token) error = %v", err)
	}
	codexBinary := filepath.Join(tempDir, codexExecutableName())
	if err := os.WriteFile(codexBinary, []byte("fixture executable"), 0o555); err != nil {
		t.Fatalf("WriteFile(codex binary) error = %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(codexBinary, 0o555); err != nil {
			t.Fatalf("Chmod(codex binary) error = %v", err)
		}
	}

	var groups strings.Builder
	for _, spec := range specs {
		cwd := filepath.Join(tempDir, spec.id+"-cwd")
		codexHome := filepath.Join(tempDir, spec.id+"-codex-home")
		if err := os.Mkdir(cwd, 0o755); err != nil {
			t.Fatalf("Mkdir(cwd) error = %v", err)
		}
		if err := os.Mkdir(codexHome, 0o755); err != nil {
			t.Fatalf("Mkdir(codex_home) error = %v", err)
		}
		fmt.Fprintf(&groups, `[[session_groups]]
session_group_id = %s
workspace_id = %s
cwd = %s
codex_home = %s

[session_groups.runtime_policy]
approval_policy = "on-request"
approvals_reviewer = "user"
sandbox_mode = "workspace-write"

[session_groups.replay_limits]
max_events = 2000
max_bytes = 8388608
ttl_millis = 1800000

[session_groups.thread_binding_limits]
max_bindings = 1000
ttl_millis = 86400000

[session_groups.pending_limits]
max_active_requests = 32
max_display_payload_bytes = 32768
status_non_pending_budget_bytes = 65536

[session_groups.grpc_limits]
inbound_message_bytes = 4194304
outbound_message_bytes = 4194304
`, strconv.Quote(spec.id), strconv.Quote(spec.workspace), strconv.Quote(cwd), strconv.Quote(codexHome))
	}
	configPath := filepath.Join(tempDir, "gateway.toml")
	configText := fmt.Sprintf(`codex_binary = %s
listen = "127.0.0.1:0"

[client_auth_token_source]
file = %s

%s`, strconv.Quote(codexBinary), strconv.Quote(tokenFile), groups.String())
	if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	return configPath, token
}

func codexExecutableName() string {
	if runtime.GOOS == "windows" {
		return "codex.exe"
	}
	return "codex"
}

func noSignalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return ctx, func() {}
}

func freeLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen(127.0.0.1:0) error = %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("Close(free listener) error = %v", err)
	}
	return address
}

func waitForGatewayServing(t *testing.T, address string, token string, stderr fmt.Stringer) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("grpc.NewClient() error = %v", err)
		}
		requestCtx, cancel := context.WithTimeout(authenticatedContext(context.Background(), token), 100*time.Millisecond)
		_, rpcErr := pb.NewCodexControlClient(conn).GetTaskStatus(requestCtx, &pb.GetTaskStatusRequest{
			Locator: &pb.GetTaskStatusRequest_TaskId{TaskId: "missing-task"},
		})
		cancel()
		_ = conn.Close()
		if status.Code(rpcErr) == codes.NotFound {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("gateway did not serve before timeout: last error = %v, stderr = %q", rpcErr, stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func authenticatedContext(ctx context.Context, token string) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))
}

func waitForChannelClosed(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}
