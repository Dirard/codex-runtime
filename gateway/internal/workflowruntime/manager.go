package workflowruntime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Dirard/codex-runtime/gateway/internal/appserver"
	"github.com/Dirard/codex-runtime/gateway/internal/chatruntime"
	"github.com/Dirard/codex-runtime/gateway/internal/config"
	"github.com/Dirard/codex-runtime/gateway/internal/grpcapi"
)

type Options struct {
	Config      *config.ValidatedConfig
	BaseSession config.SessionGroup
	ChatRuntime *chatruntime.Service
	StderrSink  func(string)

	newSupervisor func(context.Context, *config.ValidatedConfig, config.SessionGroup, appserver.ProcessOptions) (supervisor, string, error)
}

type Manager struct {
	config        *config.ValidatedConfig
	baseSession   config.SessionGroup
	chatRuntime   *chatruntime.Service
	stderrSink    func(string)
	newSupervisor func(context.Context, *config.ValidatedConfig, config.SessionGroup, appserver.ProcessOptions) (supervisor, string, error)

	mu      sync.Mutex
	closed  bool
	entries map[string]*entry
}

type entry struct {
	processEpoch string
	supervisor   supervisor
}

type supervisor interface {
	chatruntime.AppServerSupervisor
	Close() error
}

func NewManager(options Options) (*Manager, error) {
	if options.Config == nil {
		return nil, fmt.Errorf("validated config is required")
	}
	if options.ChatRuntime == nil {
		return nil, fmt.Errorf("chat runtime service is required")
	}
	if options.BaseSession.SessionGroupID == "" || options.BaseSession.WorkspaceID == "" {
		return nil, fmt.Errorf("base session group is required")
	}
	if options.BaseSession.CanonicalCodexHome == "" {
		return nil, fmt.Errorf("base session canonical codex_home is required")
	}
	if options.newSupervisor == nil {
		options.newSupervisor = func(ctx context.Context, cfg *config.ValidatedConfig, session config.SessionGroup, processOptions appserver.ProcessOptions) (supervisor, string, error) {
			return appserver.NewProcessSupervisor(ctx, cfg, session, processOptions)
		}
	}
	return &Manager{
		config:        options.Config,
		baseSession:   options.BaseSession,
		chatRuntime:   options.ChatRuntime,
		stderrSink:    options.StderrSink,
		newSupervisor: options.newSupervisor,
		entries:       map[string]*entry{},
	}, nil
}

func (m *Manager) EnsureWorkflowRuntime(ctx context.Context, launch grpcapi.WorkflowRuntimeLaunch) error {
	if m == nil {
		return fmt.Errorf("workflow runtime manager is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateLaunch(launch); err != nil {
		return err
	}
	if err := validateRuntimeWorkspace(launch); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("workflow runtime manager is closed")
	}
	if current := m.entries[launch.SessionGroupID]; current != nil && current.processEpoch == launch.ProcessEpoch {
		return nil
	}
	m.closeLocked(launch.SessionGroupID)

	session, err := m.sessionGroup(launch)
	if err != nil {
		return err
	}
	supervisor, _, err := m.newSupervisor(ctx, m.config, session, appserver.ProcessOptions{
		StderrSink: m.stderrSink,
	})
	if err != nil {
		return err
	}
	if err := m.chatRuntime.RegisterSession(chatruntime.Session{
		Group:              session,
		ConnectionProvider: chatruntime.NewAppServerConnectionProvider(supervisor),
	}); err != nil {
		_ = supervisor.Close()
		return err
	}
	m.entries[launch.SessionGroupID] = &entry{
		processEpoch: launch.ProcessEpoch,
		supervisor:   supervisor,
	}
	return nil
}

func (m *Manager) CloseWorkflowRuntime(ctx context.Context, launch grpcapi.WorkflowRuntimeLaunch) error {
	if m == nil {
		return nil
	}
	if err := validateLaunchIdentity(launch); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closeLocked(launch.SessionGroupID)
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	var firstErr error
	for sessionGroupID := range m.entries {
		if err := m.closeLocked(sessionGroupID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *Manager) closeLocked(sessionGroupID string) error {
	current := m.entries[sessionGroupID]
	if current == nil {
		return nil
	}
	delete(m.entries, sessionGroupID)
	m.chatRuntime.UnregisterSession(sessionGroupID)
	return current.supervisor.Close()
}

func (m *Manager) sessionGroup(launch grpcapi.WorkflowRuntimeLaunch) (config.SessionGroup, error) {
	canonicalCWD, err := config.CanonicalizeExistingDir(launch.CWD, "workflow runtime cwd")
	if err != nil {
		return config.SessionGroup{}, err
	}
	session := m.baseSession
	session.SessionGroupID = launch.SessionGroupID
	session.WorkspaceID = launch.WorkspaceID
	session.CWD = launch.CWD
	session.CanonicalCWD = canonicalCWD
	// Codex loads ChatGPT auth from CODEX_HOME; workflow config is project-local at cwd/.codex.
	session.CodexHome = m.baseSession.CodexHome
	session.CanonicalCodexHome = m.baseSession.CanonicalCodexHome
	return session, nil
}

func validateLaunch(launch grpcapi.WorkflowRuntimeLaunch) error {
	if err := validateLaunchIdentity(launch); err != nil {
		return err
	}
	if launch.CWD == "" {
		return fmt.Errorf("workflow runtime cwd is required")
	}
	if !filepath.IsAbs(launch.CWD) {
		return fmt.Errorf("workflow runtime cwd must be absolute")
	}
	if launch.ProcessEpoch == "" {
		return fmt.Errorf("workflow process epoch is required")
	}
	return nil
}

func validateLaunchIdentity(launch grpcapi.WorkflowRuntimeLaunch) error {
	if launch.SessionGroupID == "" {
		return fmt.Errorf("workflow session group id is required")
	}
	if launch.WorkspaceID == "" {
		return fmt.Errorf("workflow workspace id is required")
	}
	return nil
}

func validateRuntimeWorkspace(launch grpcapi.WorkflowRuntimeLaunch) error {
	configPath := filepath.Join(launch.CWD, ".codex", "config.toml")
	info, err := os.Stat(configPath)
	if err != nil {
		return fmt.Errorf("workflow runtime project config missing: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("workflow runtime project config must be a file")
	}
	return nil
}
