package workflowruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dirard/codex-runtime/gateway/internal/appserver"
	"github.com/Dirard/codex-runtime/gateway/internal/chatruntime"
	"github.com/Dirard/codex-runtime/gateway/internal/chatstate"
	"github.com/Dirard/codex-runtime/gateway/internal/config"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	"github.com/Dirard/codex-runtime/gateway/internal/grpcapi"
)

func TestManagerRegistersDynamicWorkflowSessionWithProjectCodexAndBaseAuthHome(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "runtime", "workspace")
	mustWriteFile(t, filepath.Join(workspace, ".codex", "config.toml"), []byte("model = \"test\"\n"))
	launchCWD := filepath.Join(root, "runtime", ".", "workspace")
	canonicalWorkspace, err := config.CanonicalizeExistingDir(launchCWD, "workflow runtime cwd")
	if err != nil {
		t.Fatalf("CanonicalizeExistingDir() error = %v", err)
	}
	baseCodexHome := filepath.Join(root, "base-codex-home")
	baseSession := config.SessionGroup{
		SessionGroupID:       "sg-base",
		WorkspaceID:          "ws-base",
		CWD:                  filepath.Join(root, "base-workspace"),
		CodexHome:            baseCodexHome,
		CanonicalCWD:         filepath.Join(root, "base-workspace"),
		CanonicalCodexHome:   baseCodexHome,
		CredentialProviderID: "provider-1",
	}
	chatRuntime := newTestChatRuntime(t, baseSession)
	var sessions []config.SessionGroup
	var supervisors []*fakeSupervisor
	manager, err := NewManager(Options{
		Config:      &config.ValidatedConfig{CodexBinary: filepath.Join(root, "codex")},
		BaseSession: baseSession,
		ChatRuntime: chatRuntime,
		newSupervisor: func(_ context.Context, _ *config.ValidatedConfig, session config.SessionGroup, _ appserver.ProcessOptions) (supervisor, string, error) {
			sessions = append(sessions, session)
			supervisor := &fakeSupervisor{}
			supervisors = append(supervisors, supervisor)
			return supervisor, "test-version", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	launch := grpcapi.WorkflowRuntimeLaunch{
		SessionGroupID: "wf-session",
		WorkspaceID:    "wf-workspace",
		Root:           root,
		CWD:            launchCWD,
		RuntimeHome:    filepath.Join(root, "runtime", "codex-home"),
		ProcessEpoch:   "wf-epoch-1",
	}
	if err := manager.EnsureWorkflowRuntime(context.Background(), launch); err != nil {
		t.Fatalf("EnsureWorkflowRuntime() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("supervisor sessions = %d, want 1", len(sessions))
	}
	if sessions[0].SessionGroupID != launch.SessionGroupID || sessions[0].WorkspaceID != launch.WorkspaceID || sessions[0].CanonicalCWD != canonicalWorkspace {
		t.Fatalf("workflow session = %#v, want launch ids and cwd", sessions[0])
	}
	if sessions[0].CanonicalCodexHome != baseCodexHome || sessions[0].CanonicalCodexHome == launch.RuntimeHome {
		t.Fatalf("workflow CODEX_HOME = %q, want inherited authenticated home %q", sessions[0].CanonicalCodexHome, baseCodexHome)
	}
	assertStartChatRunReason(t, chatRuntime, launch, domain.ReasonInvalidRequest)

	if err := manager.EnsureWorkflowRuntime(context.Background(), launch); err != nil {
		t.Fatalf("second EnsureWorkflowRuntime() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("same epoch created %d supervisors, want 1", len(sessions))
	}

	launch.ProcessEpoch = "wf-epoch-2"
	if err := manager.EnsureWorkflowRuntime(context.Background(), launch); err != nil {
		t.Fatalf("epoch change EnsureWorkflowRuntime() error = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("epoch change created %d supervisors, want 2", len(sessions))
	}
	if !supervisors[0].closed {
		t.Fatal("old workflow supervisor was not closed on epoch change")
	}

	if err := manager.CloseWorkflowRuntime(context.Background(), launch); err != nil {
		t.Fatalf("CloseWorkflowRuntime() error = %v", err)
	}
	if !supervisors[1].closed {
		t.Fatal("current workflow supervisor was not closed")
	}
	assertStartChatRunReason(t, chatRuntime, launch, domain.ReasonUnknownSessionGroup)
}

func TestManagerRejectsMissingRuntimeProjectConfig(t *testing.T) {
	root := t.TempDir()
	baseSession := config.SessionGroup{
		SessionGroupID:     "sg-base",
		WorkspaceID:        "ws-base",
		CodexHome:          filepath.Join(root, "codex-home"),
		CanonicalCodexHome: filepath.Join(root, "codex-home"),
		CWD:                filepath.Join(root, "cwd"),
		CanonicalCWD:       filepath.Join(root, "cwd"),
	}
	chatRuntime := newTestChatRuntime(t, baseSession)
	manager, err := NewManager(Options{
		Config:      &config.ValidatedConfig{CodexBinary: filepath.Join(root, "codex")},
		BaseSession: baseSession,
		ChatRuntime: chatRuntime,
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = manager.EnsureWorkflowRuntime(context.Background(), grpcapi.WorkflowRuntimeLaunch{
		SessionGroupID: "wf-session",
		WorkspaceID:    "wf-workspace",
		CWD:            filepath.Join(root, "runtime", "workspace"),
		ProcessEpoch:   "wf-epoch-1",
	})
	if err == nil || !strings.Contains(err.Error(), "project config missing") {
		t.Fatalf("EnsureWorkflowRuntime() error = %v, want missing project config", err)
	}
}

func newTestChatRuntime(t *testing.T, baseSession config.SessionGroup) *chatruntime.Service {
	t.Helper()
	service, err := chatruntime.NewService([]chatruntime.Session{
		{Group: baseSession, ConnectionProvider: fakeConnectionProvider{}},
	}, chatruntime.ServiceOptions{Store: chatstate.NewStore(chatstate.StoreOptions{Epoch: "epoch-1"})})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

type fakeConnectionProvider struct{}

func (fakeConnectionProvider) Connection(context.Context) (chatruntime.AppServerClient, error) {
	return nil, fmt.Errorf("connection should not be requested")
}

type fakeSupervisor struct {
	closed bool
}

func (s *fakeSupervisor) Connection(context.Context) (*appserver.Connection, error) {
	return nil, fmt.Errorf("connection should not be requested")
}

func (s *fakeSupervisor) Close() error {
	s.closed = true
	return nil
}

func assertStartChatRunReason(t *testing.T, service *chatruntime.Service, launch grpcapi.WorkflowRuntimeLaunch, reason domain.GatewayErrorReason) {
	t.Helper()
	_, err := service.StartChatRun(context.Background(), domain.StartChatRunCommand{
		SessionGroupID:  launch.SessionGroupID,
		WorkspaceID:     launch.WorkspaceID,
		Prompt:          " ",
		ClientMessageID: "client-message-1",
		IdempotencyKey:  "idem-1",
	})
	var gatewayErr *domain.GatewayError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("StartChatRun() error = %#v, want GatewayError", err)
	}
	if gatewayErr.Details.Reason != reason {
		t.Fatalf("StartChatRun() reason = %q, want %q", gatewayErr.Details.Reason, reason)
	}
}

func mustWriteFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}
