package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	codex "github.com/Dirard/codex-runtime/sdk/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

const defaultGatewayAddress = "127.0.0.1:5575"
const streamTextCaptureLimit = 64 * 1024

type config struct {
	gatewayAddr            string
	tokenSource            string
	sessionGroupID         string
	workspaceID            string
	namespace              string
	workflowID             string
	workflowDir            string
	mcpWorkflowID          string
	mcpWorkflowDir         string
	visibilityAlphaID      string
	visibilityAlphaDir     string
	visibilityBetaID       string
	visibilityBetaDir      string
	plainPrompt            string
	plainContinuePrompt    string
	workflowPrompt         string
	workflowContinuePrompt string
	legacyTaskPrompt       string
	interruptPrompt        string
	timeout                time.Duration
	streamTimeout          time.Duration
	skipLegacyTasks        bool
	skipMCPNegative        bool
	skipInterrupt          bool
	skipWorkflowVisibility bool
	skipDelete             bool
}

type suite struct {
	cfg            config
	token          string
	conn           *grpc.ClientConn
	client         *codex.Client
	workflowClient pb.WorkflowRuntimeServiceClient
	controlClient  pb.CodexControlClient
}

type streamSummary struct {
	events             int
	pending            int
	assistantCompleted bool
	terminal           bool
	doneBy             string
	maxEventID         uint64
	eventCursor        string
	textBytes          int
	text               string
}

type legacyStreamSummary struct {
	events        int
	pending       int
	terminal      bool
	doneBy        string
	maxEventID    uint64
	textBytes     int
	terminalState string
}

type visibilityProbe struct {
	label           string
	workflowID      string
	workflowDir     string
	agentRole       string
	taskName        string
	expectedAgentID string
	expectedAgents  string
	forbiddenAgents string
	expectedSkill   string
	forbiddenSkill  string
	expectedAgent   string
	forbiddenAgent  string
	expectedHello   string
	forbiddenHello  string
}

func main() {
	cfg := readConfig()
	if err := cfg.validate(); err != nil {
		printSafeError("config", err)
		os.Exit(2)
	}
	if err := run(cfg); err != nil {
		printSafeError("full-e2e", err)
		os.Exit(1)
	}
}

func readConfig() config {
	cfg := config{
		gatewayAddr:            firstNonEmpty(os.Getenv("CODEX_RUNTIME_GATEWAY_ADDR"), defaultGatewayAddress),
		tokenSource:            firstNonEmpty(os.Getenv("CODEX_RUNTIME_TOKEN_FILE"), os.Getenv("CODEX_RUNTIME_TOKEN_SOURCE")),
		sessionGroupID:         firstNonEmpty(os.Getenv("CODEX_RUNTIME_SESSION_GROUP"), "full-e2e-session"),
		workspaceID:            firstNonEmpty(os.Getenv("CODEX_RUNTIME_WORKSPACE"), "full-e2e-workspace"),
		namespace:              firstNonEmpty(os.Getenv("CODEX_WORKFLOW_NAMESPACE"), "examples-full-e2e"),
		workflowID:             firstNonEmpty(os.Getenv("CODEX_WORKFLOW_ID"), "plain-chat"),
		workflowDir:            firstNonEmpty(os.Getenv("CODEX_WORKFLOW_DIR"), filepath.FromSlash("examples/workflows/plain-chat")),
		mcpWorkflowID:          firstNonEmpty(os.Getenv("CODEX_MCP_WORKFLOW_ID"), "writer-notes-mcp-deny"),
		mcpWorkflowDir:         firstNonEmpty(os.Getenv("CODEX_MCP_WORKFLOW_DIR"), filepath.FromSlash("examples/workflows/writer-notes")),
		visibilityAlphaID:      firstNonEmpty(os.Getenv("CODEX_VISIBILITY_ALPHA_WORKFLOW_ID"), "visibility-alpha"),
		visibilityAlphaDir:     firstNonEmpty(os.Getenv("CODEX_VISIBILITY_ALPHA_WORKFLOW_DIR"), filepath.FromSlash("examples/workflows/visibility-alpha")),
		visibilityBetaID:       firstNonEmpty(os.Getenv("CODEX_VISIBILITY_BETA_WORKFLOW_ID"), "visibility-beta"),
		visibilityBetaDir:      firstNonEmpty(os.Getenv("CODEX_VISIBILITY_BETA_WORKFLOW_DIR"), filepath.FromSlash("examples/workflows/visibility-beta")),
		plainPrompt:            firstNonEmpty(os.Getenv("CODEX_FULL_E2E_PLAIN_PROMPT"), "Reply with exactly one short sentence confirming plain chat e2e is connected. Do not inspect files or run commands."),
		plainContinuePrompt:    firstNonEmpty(os.Getenv("CODEX_FULL_E2E_PLAIN_CONTINUE_PROMPT"), "Continue in the same chat with one more short sentence."),
		workflowPrompt:         firstNonEmpty(os.Getenv("CODEX_FULL_E2E_WORKFLOW_PROMPT"), "Reply with exactly one short sentence confirming workflow e2e is connected. Do not inspect files or run commands."),
		workflowContinuePrompt: firstNonEmpty(os.Getenv("CODEX_FULL_E2E_WORKFLOW_CONTINUE_PROMPT"), "Continue in the same workflow chat with one more short sentence."),
		legacyTaskPrompt:       firstNonEmpty(os.Getenv("CODEX_FULL_E2E_TASK_PROMPT"), "Reply with exactly one short sentence confirming legacy gateway task e2e is connected. Do not inspect files or run commands."),
		interruptPrompt:        firstNonEmpty(os.Getenv("CODEX_FULL_E2E_INTERRUPT_PROMPT"), "Write a deliberately long numbered list from 1 to 200, one item per line, so the caller can interrupt the run."),
		timeout:                12 * time.Minute,
		streamTimeout:          2 * time.Minute,
	}
	flag.StringVar(&cfg.gatewayAddr, "gateway", cfg.gatewayAddr, "loopback Codex runtime gateway address")
	flag.StringVar(&cfg.tokenSource, "token-source", cfg.tokenSource, "path to a file containing the gateway bearer token")
	flag.StringVar(&cfg.sessionGroupID, "session-group", cfg.sessionGroupID, "gateway session group id")
	flag.StringVar(&cfg.workspaceID, "workspace", cfg.workspaceID, "gateway workspace id")
	flag.StringVar(&cfg.namespace, "namespace", cfg.namespace, "workflow namespace")
	flag.StringVar(&cfg.workflowID, "workflow-id", cfg.workflowID, "plain workflow id")
	flag.StringVar(&cfg.workflowDir, "workflow-dir", cfg.workflowDir, "plain workflow directory")
	flag.StringVar(&cfg.mcpWorkflowID, "mcp-workflow-id", cfg.mcpWorkflowID, "MCP workflow id used for expected-deny check")
	flag.StringVar(&cfg.mcpWorkflowDir, "mcp-workflow-dir", cfg.mcpWorkflowDir, "MCP workflow directory used for expected-deny check")
	flag.StringVar(&cfg.visibilityAlphaID, "visibility-alpha-workflow-id", cfg.visibilityAlphaID, "alpha workflow id used for agent/skill visibility isolation check")
	flag.StringVar(&cfg.visibilityAlphaDir, "visibility-alpha-workflow-dir", cfg.visibilityAlphaDir, "alpha workflow directory used for agent/skill visibility isolation check")
	flag.StringVar(&cfg.visibilityBetaID, "visibility-beta-workflow-id", cfg.visibilityBetaID, "beta workflow id used for agent/skill visibility isolation check")
	flag.StringVar(&cfg.visibilityBetaDir, "visibility-beta-workflow-dir", cfg.visibilityBetaDir, "beta workflow directory used for agent/skill visibility isolation check")
	flag.StringVar(&cfg.plainPrompt, "plain-prompt", cfg.plainPrompt, "first plain chat prompt")
	flag.StringVar(&cfg.plainContinuePrompt, "plain-continue-prompt", cfg.plainContinuePrompt, "second plain chat prompt")
	flag.StringVar(&cfg.workflowPrompt, "workflow-prompt", cfg.workflowPrompt, "first workflow prompt")
	flag.StringVar(&cfg.workflowContinuePrompt, "workflow-continue-prompt", cfg.workflowContinuePrompt, "second workflow prompt")
	flag.StringVar(&cfg.legacyTaskPrompt, "legacy-task-prompt", cfg.legacyTaskPrompt, "legacy CodexControl task prompt")
	flag.StringVar(&cfg.interruptPrompt, "interrupt-prompt", cfg.interruptPrompt, "long prompt used for interrupt checks")
	flag.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "whole e2e timeout")
	flag.DurationVar(&cfg.streamTimeout, "stream-timeout", cfg.streamTimeout, "per-stream timeout")
	flag.BoolVar(&cfg.skipLegacyTasks, "skip-legacy-tasks", cfg.skipLegacyTasks, "skip legacy CodexControl RPC checks")
	flag.BoolVar(&cfg.skipMCPNegative, "skip-mcp-negative", cfg.skipMCPNegative, "skip expected MCP-unavailable workflow package check")
	flag.BoolVar(&cfg.skipInterrupt, "skip-interrupt", cfg.skipInterrupt, "skip long-run interrupt checks")
	flag.BoolVar(&cfg.skipWorkflowVisibility, "skip-workflow-visibility", cfg.skipWorkflowVisibility, "skip workflow-scoped agents/skills visibility isolation check")
	flag.BoolVar(&cfg.skipDelete, "skip-delete", cfg.skipDelete, "skip direct gateway DeleteWorkflow cleanup/check")
	flag.Parse()
	return cfg
}

func (cfg config) validate() error {
	if err := validateLocalGatewayAddr(cfg.gatewayAddr); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.tokenSource) == "" {
		return errors.New("token-source is required")
	}
	if strings.TrimSpace(cfg.sessionGroupID) == "" {
		return errors.New("session-group is required")
	}
	if strings.TrimSpace(cfg.workspaceID) == "" {
		return errors.New("workspace is required")
	}
	if strings.TrimSpace(cfg.namespace) == "" || strings.TrimSpace(cfg.workflowID) == "" {
		return errors.New("namespace and workflow-id are required")
	}
	if strings.TrimSpace(cfg.workflowDir) == "" {
		return errors.New("workflow-dir is required")
	}
	if !cfg.skipWorkflowVisibility {
		if strings.TrimSpace(cfg.visibilityAlphaID) == "" || strings.TrimSpace(cfg.visibilityAlphaDir) == "" ||
			strings.TrimSpace(cfg.visibilityBetaID) == "" || strings.TrimSpace(cfg.visibilityBetaDir) == "" {
			return errors.New("visibility workflow ids and dirs are required unless skip-workflow-visibility is set")
		}
	}
	if strings.TrimSpace(cfg.plainPrompt) == "" || strings.TrimSpace(cfg.workflowPrompt) == "" {
		return errors.New("plain-prompt and workflow-prompt are required")
	}
	if cfg.timeout <= 0 || cfg.streamTimeout <= 0 {
		return errors.New("timeout and stream-timeout must be positive")
	}
	return nil
}

func run(cfg config) error {
	token, err := readTokenSource(cfg.tokenSource)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	conn, err := grpc.NewClient(cfg.gatewayAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := codex.New(
		conn,
		codex.WithBearerToken(token),
		codex.WithSessionGroupID(cfg.sessionGroupID),
		codex.WithWorkspaceID(cfg.workspaceID),
		codex.WithCallOptions(grpc.WaitForReady(false)),
	)
	if err != nil {
		return err
	}
	s := suite{
		cfg:            cfg,
		token:          token,
		conn:           conn,
		client:         client,
		workflowClient: pb.NewWorkflowRuntimeServiceClient(conn),
		controlClient:  pb.NewCodexControlClient(conn),
	}

	fmt.Println("full-e2e: starting")
	fmt.Printf("gateway: %s\n", cfg.gatewayAddr)
	fmt.Printf("token_source: configured (redacted)\n")
	fmt.Printf("session_group: %s workspace: %s\n", cfg.sessionGroupID, cfg.workspaceID)
	if err := s.step("sdk local configuration", s.checkSDKConfiguration); err != nil {
		return err
	}
	if err := s.step("gateway health", func() error { return s.checkGatewayHealth(ctx) }); err != nil {
		return err
	}
	if err := s.step("workflow package build", s.checkWorkflowPackageBuild); err != nil {
		return err
	}
	if err := s.step("plain chat SDK", func() error { return s.checkPlainChat(ctx) }); err != nil {
		return err
	}
	if err := s.step("workflow SDK", func() error { return s.checkWorkflow(ctx) }); err != nil {
		return err
	}
	if !cfg.skipWorkflowVisibility {
		if err := s.step("workflow visibility isolation", func() error { return s.checkWorkflowVisibility(ctx) }); err != nil {
			return err
		}
	}
	if !cfg.skipInterrupt {
		if err := s.step("interrupt SDK", func() error { return s.checkInterrupt(ctx) }); err != nil {
			return err
		}
	}
	if !cfg.skipMCPNegative {
		if err := s.step("MCP policy expected deny", func() error { return s.checkMCPNegative(ctx) }); err != nil {
			return err
		}
	}
	if !cfg.skipLegacyTasks {
		if err := s.step("legacy CodexControl gateway", func() error { return s.checkLegacyCodexControl(ctx) }); err != nil {
			return err
		}
	}
	fmt.Println("full-e2e: PASS")
	return nil
}

func (s *suite) step(name string, fn func() error) error {
	fmt.Printf("\n== %s ==\n", name)
	if err := fn(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	fmt.Printf("PASS: %s\n", name)
	return nil
}

func (s *suite) checkSDKConfiguration() error {
	codex.SetDefaultClient(nil)
	if _, _, err := codex.Run(context.Background(), "this should not run"); !errors.Is(err, codex.ErrDefaultClientNotConfigured) {
		return fmt.Errorf("package Run without default client: expected ErrDefaultClientNotConfigured, got %v", err)
	}
	chatOnly, err := codex.NewWithClient(
		pb.NewChatRuntimeServiceClient(s.conn),
		codex.WithBearerToken(s.token),
		codex.WithSessionGroupID(s.cfg.sessionGroupID),
		codex.WithWorkspaceID(s.cfg.workspaceID),
	)
	if err != nil {
		return err
	}
	if _, err := chatOnly.InitWorkflow(context.Background(), codex.WorkflowDir{
		Namespace: s.cfg.namespace,
		ID:        s.cfg.workflowID,
		Path:      s.cfg.workflowDir,
	}); !expectWorkflowError("chat-only InitWorkflow", err, codex.WorkflowErrorGatewayUnavailable) {
		return err
	}
	if _, err := codex.NewWithClients(
		pb.NewChatRuntimeServiceClient(s.conn),
		pb.NewWorkflowRuntimeServiceClient(s.conn),
		codex.WithBearerToken(s.token),
		codex.WithSessionGroupID(s.cfg.sessionGroupID),
		codex.WithWorkspaceID(s.cfg.workspaceID),
	); err != nil {
		return err
	}
	if strings.Contains(fmt.Sprintf("%#v", s.client), s.token) {
		return errors.New("client string representation leaked bearer token")
	}
	codex.SetDefaultClient(s.client)
	if _, ok := codex.DefaultClient(); !ok {
		return errors.New("default client was not configured")
	}
	fmt.Println("default_client: configured")
	fmt.Printf("client: %s\n", s.client)
	return nil
}

func (s *suite) checkGatewayHealth(ctx context.Context) error {
	health, err := healthpb.NewHealthClient(s.conn).Check(s.authenticatedContext(ctx), &healthpb.HealthCheckRequest{})
	if err != nil {
		return err
	}
	fmt.Printf("health: %s\n", health.GetStatus())
	return nil
}

func (s *suite) checkWorkflowPackageBuild() error {
	dirPkg, err := codex.BuildWorkflowPackage(codex.WorkflowDir{
		Namespace: s.cfg.namespace,
		ID:        s.cfg.workflowID,
		Path:      s.cfg.workflowDir,
	})
	if err != nil {
		return err
	}
	zipBytes, err := zipWorkflowDir(s.cfg.workflowDir)
	if err != nil {
		return err
	}
	zipPkg, err := codex.BuildWorkflowPackage(codex.WorkflowZip{
		Namespace: s.cfg.namespace,
		ID:        s.cfg.workflowID,
		Reader:    bytes.NewReader(zipBytes),
	})
	if err != nil {
		return err
	}
	if dirPkg.PackageFingerprint != zipPkg.PackageFingerprint {
		return fmt.Errorf("dir and zip package fingerprints differ")
	}
	fmt.Printf("package: files=%d bytes=%d fingerprint=%s\n", len(dirPkg.Files), dirPkg.TotalSizeBytes, shortID(dirPkg.PackageFingerprint))
	fmt.Printf("zip_equivalence: fingerprint=%s\n", shortID(zipPkg.PackageFingerprint))
	return nil
}

func (s *suite) checkPlainChat(ctx context.Context) error {
	codex.SetDefaultClient(s.client)
	chat, events, err := codex.Run(
		ctx,
		s.cfg.plainPrompt,
		codex.WithClientMessageID(mustID("plain-msg")),
		codex.WithIdempotencyKey(mustID("plain-idem")),
		codex.WithContextBlocks(codex.ContextBlock{
			Kind:        pb.ContextBlockKind_CONTEXT_BLOCK_KIND_APPLICATION,
			SourceLabel: "full-e2e",
			SourceURI:   "https://example.invalid/full-e2e/plain-chat",
			MIMEType:    "text/plain",
			Content:     "This is a reproducible SDK/gateway e2e check.",
		}),
		codex.WithUICorrelationMetadata(map[string]string{"example": "full-e2e", "surface": "plain-chat"}),
		codex.WithInitialStreamOptions(codex.WithClientSubscriberID(mustID("plain-start-sub"))),
	)
	if err != nil {
		return err
	}
	defer events.Close()
	fmt.Printf("chat_id: %s\n", chat.ID)
	first, err := drainChatStream(ctx, events, s.cfg.streamTimeout, true)
	if err != nil {
		return err
	}
	fmt.Printf("plain_first: events=%d pending=%d done_by=%s cursor=%s\n", first.events, first.pending, first.doneBy, shortID(first.eventCursor))

	byPackage, err := codex.GetChat(ctx, chat.ID)
	if err != nil {
		return err
	}
	byClient, err := s.client.GetChat(ctx, chat.ID)
	if err != nil {
		return err
	}
	if byPackage.ID != chat.ID || byClient.ID != chat.ID {
		return errors.New("GetChat returned an unexpected chat id")
	}
	if err := printChatSnapshot(ctx, byClient, "plain"); err != nil {
		return err
	}
	if byClient.CachedStatus() == nil || byClient.CachedCapabilities() == nil {
		return errors.New("chat cached status/capabilities were not populated")
	}
	if err := expectPendingResponseErrors(ctx, byClient, "plain"); err != nil {
		return err
	}

	replay, err := byClient.GetEventsStream(ctx, codex.FromStart(), codex.WithClientSubscriberID(mustID("plain-replay-sub")))
	if err != nil {
		return err
	}
	replaySummary, err := drainReplayStream(ctx, replay, s.cfg.streamTimeout, 3)
	_ = replay.Close()
	if err != nil {
		return err
	}
	fmt.Printf("plain_replay_from_start: events=%d cursor=%s\n", replaySummary.events, shortID(replaySummary.eventCursor))

	result, err := byClient.Run(
		ctx,
		s.cfg.plainContinuePrompt,
		codex.WithClientMessageID(mustID("plain-continue-msg")),
		codex.WithIdempotencyKey(mustID("plain-continue-idem")),
		codex.WithContextBlocks(codex.ContextBlock{
			Kind:        pb.ContextBlockKind_CONTEXT_BLOCK_KIND_UNTRUSTED,
			SourceLabel: "user-note",
			SourceURI:   "https://example.invalid/full-e2e/user-note",
			MIMEType:    "text/plain",
			Content:     "Keep this continuation short.",
		}),
	)
	if err != nil {
		return err
	}
	continued, err := byClient.GetEventsStream(ctx, streamAfterRun(result), codex.WithClientSubscriberID(mustID("plain-continue-sub")))
	if err != nil {
		return err
	}
	second, err := drainChatStream(ctx, continued, s.cfg.streamTimeout, true)
	_ = continued.Close()
	if err != nil {
		return err
	}
	fmt.Printf("plain_second: events=%d pending=%d done_by=%s cursor=%s\n", second.events, second.pending, second.doneBy, shortID(second.eventCursor))

	replayAfterID, err := byClient.GetEventsStream(ctx, codex.AfterEventID(0), codex.WithClientSubscriberID(mustID("plain-after-id-sub")))
	if err != nil {
		return err
	}
	afterIDSummary, err := drainReplayStream(ctx, replayAfterID, s.cfg.streamTimeout, 2)
	_ = replayAfterID.Close()
	if err != nil {
		return err
	}
	fmt.Printf("plain_replay_after_event_id: events=%d\n", afterIDSummary.events)
	return nil
}

func (s *suite) checkWorkflow(ctx context.Context) error {
	if _, err := s.client.GetWorkflow(ctx, s.cfg.namespace, "missing-workflow"); !expectWorkflowError("missing workflow", err, codex.WorkflowErrorWorkflowNotFound) {
		return err
	}
	if _, err := s.client.InitWorkflow(ctx, codex.WorkflowDir{
		Namespace: "",
		ID:        s.cfg.workflowID,
		Path:      s.cfg.workflowDir,
	}); !expectWorkflowError("invalid workflow package", err, codex.WorkflowErrorInvalidWorkflowPackage) {
		return err
	}
	workflow, err := s.client.InitWorkflow(
		ctx,
		codex.WorkflowDir{Namespace: s.cfg.namespace, ID: s.cfg.workflowID, Path: s.cfg.workflowDir},
		codex.WithWorkflowClientRequestID(mustID("workflow-init")),
		codex.WithWorkflowIdempotencyKey(mustID("workflow-init-idem")),
		codex.WithWorkflowPackageOptions(codex.WithWorkflowPackageMaxBytes(codex.DefaultWorkflowPackageMaxBytes)),
	)
	if err != nil {
		return err
	}
	fmt.Printf("workflow: %s/%s storage=%s restart_required=%v fingerprint=%s\n", workflow.Namespace, workflow.ID, shortID(workflow.StorageKey), workflow.RestartRequired, shortID(workflow.ActivePackageFingerprint))
	noOp, err := s.client.InitWorkflow(
		ctx,
		codex.WorkflowDir{Namespace: s.cfg.namespace, ID: s.cfg.workflowID, Path: s.cfg.workflowDir},
		codex.WithWorkflowClientRequestID(mustID("workflow-init-noop")),
		codex.WithWorkflowIdempotencyKey(mustID("workflow-init-noop-idem")),
	)
	if err != nil {
		return err
	}
	if noOp.StorageKey != workflow.StorageKey {
		return errors.New("second InitWorkflow returned a different storage key")
	}
	lookedUp, err := s.client.GetWorkflow(ctx, s.cfg.namespace, s.cfg.workflowID)
	if err != nil {
		return err
	}
	status, err := lookedUp.GetStatus(ctx)
	if err != nil {
		return err
	}
	if lookedUp.CachedCapabilities() == nil || lookedUp.CachedStatus().Namespace == "" {
		return errors.New("workflow cached status/capabilities were not populated")
	}
	fmt.Printf("workflow_status: restart_required=%v process_epoch=%s active_fingerprint=%s\n", status.RestartRequired, shortID(status.ProcessEpoch), shortID(status.ActivePackageFingerprint))

	chat, events, err := lookedUp.Run(
		ctx,
		s.cfg.workflowPrompt,
		codex.WithClientMessageID(mustID("workflow-msg")),
		codex.WithIdempotencyKey(mustID("workflow-idem")),
		codex.WithContextBlocks(codex.ContextBlock{
			Kind:        pb.ContextBlockKind_CONTEXT_BLOCK_KIND_APPLICATION,
			SourceLabel: "full-e2e",
			SourceURI:   "https://example.invalid/full-e2e/workflow",
			MIMEType:    "text/plain",
			Content:     "Workflow e2e context block.",
		}),
		codex.WithUICorrelationMetadata(map[string]string{"example": "full-e2e", "surface": "workflow"}),
		codex.WithInitialStreamOptions(codex.WithClientSubscriberID(mustID("workflow-start-sub"))),
	)
	if err != nil {
		return err
	}
	defer events.Close()
	fmt.Printf("workflow_chat_id: %s\n", chat.ID)
	first, err := drainChatStream(ctx, events, s.cfg.streamTimeout, true)
	if err != nil {
		return err
	}
	fmt.Printf("workflow_first: events=%d pending=%d done_by=%s cursor=%s\n", first.events, first.pending, first.doneBy, shortID(first.eventCursor))

	workflowChat, err := lookedUp.GetChat(ctx, chat.ID)
	if err != nil {
		return err
	}
	if err := printChatSnapshot(ctx, workflowChat, "workflow"); err != nil {
		return err
	}
	if err := expectPendingResponseErrors(ctx, workflowChat, "workflow"); err != nil {
		return err
	}
	replay, err := workflowChat.GetEventsStream(ctx, codex.FromStart(), codex.WithClientSubscriberID(mustID("workflow-replay-sub")))
	if err != nil {
		return err
	}
	replaySummary, err := drainReplayStream(ctx, replay, s.cfg.streamTimeout, 3)
	_ = replay.Close()
	if err != nil {
		return err
	}
	fmt.Printf("workflow_replay_from_start: events=%d cursor=%s\n", replaySummary.events, shortID(replaySummary.eventCursor))

	result, err := workflowChat.Run(
		ctx,
		s.cfg.workflowContinuePrompt,
		codex.WithClientMessageID(mustID("workflow-continue-msg")),
		codex.WithIdempotencyKey(mustID("workflow-continue-idem")),
	)
	if err != nil {
		return err
	}
	continued, err := workflowChat.GetEventsStream(ctx, streamAfterRun(result), codex.WithClientSubscriberID(mustID("workflow-continue-sub")))
	if err != nil {
		return err
	}
	second, err := drainChatStream(ctx, continued, s.cfg.streamTimeout, true)
	_ = continued.Close()
	if err != nil {
		return err
	}
	fmt.Printf("workflow_second: events=%d pending=%d done_by=%s cursor=%s\n", second.events, second.pending, second.doneBy, shortID(second.eventCursor))

	status, err = lookedUp.Restart(
		ctx,
		codex.WithWorkflowForceRestart(true),
		codex.WithWorkflowRestartClientRequestID(mustID("workflow-restart")),
		codex.WithWorkflowRestartIdempotencyKey(mustID("workflow-restart-idem")),
	)
	if err != nil {
		return err
	}
	fmt.Printf("workflow_restart: process_epoch=%s restart_required=%v\n", shortID(status.ProcessEpoch), status.RestartRequired)
	status, err = lookedUp.GetStatus(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("workflow_status_after_restart: process_epoch=%s active_fingerprint=%s\n", shortID(status.ProcessEpoch), shortID(status.ActivePackageFingerprint))

	postRestartChat, postRestartEvents, err := lookedUp.Run(ctx, "Reply with one short sentence after workflow restart.", codex.WithClientMessageID(mustID("workflow-post-restart-msg")), codex.WithIdempotencyKey(mustID("workflow-post-restart-idem")))
	if err != nil {
		return err
	}
	postRestartSummary, err := drainChatStream(ctx, postRestartEvents, s.cfg.streamTimeout, true)
	_ = postRestartEvents.Close()
	if err != nil {
		return err
	}
	fmt.Printf("workflow_post_restart: chat_id=%s events=%d done_by=%s\n", postRestartChat.ID, postRestartSummary.events, postRestartSummary.doneBy)

	if !s.cfg.skipDelete {
		if err := s.deleteWorkflow(ctx, lookedUp); err != nil {
			return err
		}
		if _, err := s.client.GetWorkflow(ctx, s.cfg.namespace, s.cfg.workflowID); !expectWorkflowError("deleted workflow lookup", err, codex.WorkflowErrorWorkflowNotFound) {
			return err
		}
	}
	return nil
}

func (s *suite) checkWorkflowVisibility(ctx context.Context) error {
	probes := []visibilityProbe{
		{
			label:           "alpha",
			workflowID:      s.cfg.visibilityAlphaID,
			workflowDir:     s.cfg.visibilityAlphaDir,
			agentRole:       "visibility-alpha-agent",
			taskName:        "visibility_alpha_probe",
			expectedAgentID: "/root/visibility_alpha_probe",
			expectedAgents:  "VISIBILITY_AGENTS_ALPHA_7A91",
			forbiddenAgents: "VISIBILITY_AGENTS_BETA_3D86",
			expectedSkill:   "VISIBILITY_SKILL_ALPHA_9271",
			forbiddenSkill:  "VISIBILITY_SKILL_BETA_5D04",
			expectedAgent:   "VISIBILITY_AGENT_ALPHA_4B62",
			forbiddenAgent:  "VISIBILITY_AGENT_BETA_8F31",
			expectedHello:   "HELLO_FROM_ALPHA_SUBAGENT VISIBILITY_AGENT_ALPHA_4B62",
			forbiddenHello:  "HELLO_FROM_BETA_SUBAGENT VISIBILITY_AGENT_BETA_8F31",
		},
		{
			label:           "beta",
			workflowID:      s.cfg.visibilityBetaID,
			workflowDir:     s.cfg.visibilityBetaDir,
			agentRole:       "visibility-beta-agent",
			taskName:        "visibility_beta_probe",
			expectedAgentID: "/root/visibility_beta_probe",
			expectedAgents:  "VISIBILITY_AGENTS_BETA_3D86",
			forbiddenAgents: "VISIBILITY_AGENTS_ALPHA_7A91",
			expectedSkill:   "VISIBILITY_SKILL_BETA_5D04",
			forbiddenSkill:  "VISIBILITY_SKILL_ALPHA_9271",
			expectedAgent:   "VISIBILITY_AGENT_BETA_8F31",
			forbiddenAgent:  "VISIBILITY_AGENT_ALPHA_4B62",
			expectedHello:   "HELLO_FROM_BETA_SUBAGENT VISIBILITY_AGENT_BETA_8F31",
			forbiddenHello:  "HELLO_FROM_ALPHA_SUBAGENT VISIBILITY_AGENT_ALPHA_4B62",
		},
	}
	for _, probe := range probes {
		if err := s.checkVisibilityProbe(ctx, probe); err != nil {
			return err
		}
	}
	return nil
}

func (s *suite) checkVisibilityProbe(ctx context.Context, probe visibilityProbe) error {
	workflow, err := s.client.InitWorkflow(
		ctx,
		codex.WorkflowDir{Namespace: s.cfg.namespace, ID: probe.workflowID, Path: probe.workflowDir},
		codex.WithWorkflowClientRequestID(mustID("visibility-"+probe.label+"-init")),
		codex.WithWorkflowIdempotencyKey(mustID("visibility-"+probe.label+"-init-idem")),
		codex.WithWorkflowPackageOptions(codex.WithWorkflowPackageMaxBytes(codex.DefaultWorkflowPackageMaxBytes)),
	)
	if err != nil {
		return err
	}
	prompt := visibilityProbePrompt(probe)
	chat, events, err := workflow.Run(
		ctx,
		prompt,
		codex.WithClientMessageID(mustID("visibility-"+probe.label+"-msg")),
		codex.WithIdempotencyKey(mustID("visibility-"+probe.label+"-idem")),
		codex.WithInitialStreamOptions(codex.WithClientSubscriberID(mustID("visibility-"+probe.label+"-sub"))),
	)
	if err != nil {
		return err
	}
	summary, err := drainChatStream(ctx, events, s.cfg.streamTimeout, true)
	_ = events.Close()
	if err != nil {
		return err
	}
	if err := assertVisibilityOutput(probe, summary.text); err != nil {
		return err
	}
	fmt.Printf("workflow_visibility_%s: chat_id=%s events=%d done_by=%s agent_id=%s agents_marker=%s skill_marker=%s agent_marker=%s\n", probe.label, chat.ID, summary.events, summary.doneBy, probe.expectedAgentID, probe.expectedAgents, probe.expectedSkill, probe.expectedAgent)
	if !s.cfg.skipDelete {
		if err := s.deleteWorkflow(ctx, workflow); err != nil {
			return err
		}
		if _, err := s.client.GetWorkflow(ctx, s.cfg.namespace, probe.workflowID); !expectWorkflowError("deleted visibility workflow lookup", err, codex.WorkflowErrorWorkflowNotFound) {
			return err
		}
	}
	return nil
}

func visibilityProbePrompt(probe visibilityProbe) string {
	return fmt.Sprintf(`Workflow visibility probe for %s.
Do not inspect files, run shell commands, or use browser/network tools.
Use the visible skills context to find the workflow visibility skill description and copy only its visibility marker.
Use the loaded AGENTS.md project instructions to find the workflow AGENTS marker and copy only that marker.
Use the spawn_agent subagent tool exactly once with agent_type %q and task_name %q. Ask that subagent to reply using its own visibility hello instruction, then wait for the subagent result.
Return exactly these four lines:
AGENTS_MARKER: <marker from the loaded workflow AGENTS.md project instructions>
SKILL_MARKER: <marker from the visible workflow skill description>
SUBAGENT_ID: <child agent identifier; for Codex v2 this is the /root/<task_name> agent path>
SUBAGENT_HELLO: <subagent hello text>
`, probe.label, probe.agentRole, probe.taskName)
}

func assertVisibilityOutput(probe visibilityProbe, output string) error {
	if !strings.Contains(output, probe.expectedAgents) {
		return fmt.Errorf("visibility %s: missing expected workflow AGENTS marker %s in assistant output", probe.label, probe.expectedAgents)
	}
	if strings.Contains(output, probe.forbiddenAgents) {
		return fmt.Errorf("visibility %s: assistant output leaked forbidden workflow AGENTS marker %s", probe.label, probe.forbiddenAgents)
	}
	if !strings.Contains(output, probe.expectedSkill) {
		return fmt.Errorf("visibility %s: missing expected workflow skill marker %s in assistant output", probe.label, probe.expectedSkill)
	}
	if strings.Contains(output, probe.forbiddenSkill) {
		return fmt.Errorf("visibility %s: assistant output leaked forbidden workflow skill marker %s", probe.label, probe.forbiddenSkill)
	}
	if !strings.Contains(output, probe.expectedAgent) {
		return fmt.Errorf("visibility %s: missing expected workflow agent marker %s in assistant output", probe.label, probe.expectedAgent)
	}
	if strings.Contains(output, probe.forbiddenAgent) {
		return fmt.Errorf("visibility %s: assistant output leaked forbidden workflow agent marker %s", probe.label, probe.forbiddenAgent)
	}
	if !strings.Contains(output, probe.expectedHello) {
		return fmt.Errorf("visibility %s: missing expected subagent hello %q in assistant output", probe.label, probe.expectedHello)
	}
	if strings.Contains(output, probe.forbiddenHello) {
		return fmt.Errorf("visibility %s: assistant output leaked forbidden subagent hello %q", probe.label, probe.forbiddenHello)
	}
	agentID := visibilityLineValue(output, "SUBAGENT_ID")
	if agentID != probe.expectedAgentID {
		return fmt.Errorf("visibility %s: SUBAGENT_ID = %q, want %q", probe.label, agentID, probe.expectedAgentID)
	}
	return nil
}

func visibilityLineValue(output string, key string) string {
	for _, line := range strings.Split(output, "\n") {
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), key) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *suite) checkInterrupt(ctx context.Context) error {
	chat, events, err := s.client.Run(
		ctx,
		s.cfg.interruptPrompt,
		codex.WithClientMessageID(mustID("interrupt-msg")),
		codex.WithIdempotencyKey(mustID("interrupt-idem")),
		codex.WithInitialStreamOptions(codex.WithClientSubscriberID(mustID("interrupt-sub"))),
	)
	if err != nil {
		if chat != nil {
			fmt.Printf("interrupt_run_partial_chat: chat_id=%s\n", shortID(chat.ID))
		}
		return annotateSDKError("interrupt run", err)
	}
	defer events.Close()
	time.Sleep(750 * time.Millisecond)
	response, err := chat.Interrupt(ctx, codex.WithClientRequestID(mustID("interrupt-request")), codex.WithIdempotencyKey(mustID("interrupt-request-idem")))
	if err != nil {
		if sdkErr, ok := codex.AsError(err); ok && strings.Contains(strings.ToLower(sdkErr.Reason), "active") {
			fmt.Printf("interrupt: run already completed before interrupt reason=%s\n", sdkErr.Reason)
			return nil
		}
		return annotateSDKError("interrupt request", err)
	}
	fmt.Printf("interrupt: sent=%v already_interrupting=%v already_terminal=%v run_id=%s\n", response.GetInterruptSent(), response.GetAlreadyInterrupting(), response.GetAlreadyTerminal(), shortID(response.GetRunId()))
	summary, err := drainReplayStream(ctx, events, s.cfg.streamTimeout, 8)
	if err != nil {
		return annotateSDKError("interrupt stream", err)
	}
	fmt.Printf("interrupt_stream: events=%d terminal=%v done_by=%s\n", summary.events, summary.terminal, summary.doneBy)
	return nil
}

func annotateSDKError(stage string, err error) error {
	if err == nil {
		return nil
	}
	if sdkErr, ok := codex.AsError(err); ok {
		return fmt.Errorf("%s: code=%s reason=%s message=%s chat=%s run=%s retryable=%v: %w", stage, sdkErr.Code, sdkErr.Reason, sdkErr.DisplayMessage, shortID(sdkErr.ChatID), shortID(sdkErr.RunID), sdkErr.Retryable, err)
	}
	return fmt.Errorf("%s: %w", stage, err)
}

func (s *suite) checkMCPNegative(ctx context.Context) error {
	_, err := s.client.InitWorkflow(
		ctx,
		codex.WorkflowDir{Namespace: s.cfg.namespace, ID: s.cfg.mcpWorkflowID, Path: s.cfg.mcpWorkflowDir},
		codex.WithWorkflowClientRequestID(mustID("mcp-init")),
		codex.WithWorkflowIdempotencyKey(mustID("mcp-init-idem")),
		codex.WithWorkflowMCPReload(true),
	)
	if !expectWorkflowError("MCP workflow init", err, codex.WorkflowErrorMCPUnavailable) {
		return err
	}
	return nil
}

func (s *suite) checkLegacyCodexControl(ctx context.Context) error {
	rpcCtx := s.authenticatedContext(ctx)
	clientMessageID := mustID("legacy-msg")
	start, err := s.controlClient.StartTask(rpcCtx, &pb.StartTaskRequest{
		SessionGroupId:        s.cfg.sessionGroupID,
		WorkspaceId:           s.cfg.workspaceID,
		Prompt:                s.cfg.legacyTaskPrompt,
		ClientMessageId:       clientMessageID,
		UiCorrelationMetadata: map[string]string{"example": "full-e2e", "surface": "legacy-task"},
	})
	if err != nil {
		return normalizeExpectedError("StartTask", err)
	}
	fmt.Printf("legacy_task: task_id=%s thread_id=%s state=%s\n", start.GetTaskId(), shortID(start.GetThreadId()), start.GetState())
	stream, err := s.controlClient.StreamTask(rpcCtx, &pb.StreamTaskRequest{
		TaskId:             start.GetTaskId(),
		Cursor:             &pb.StreamTaskRequest_FromStart{FromStart: &pb.FromStartCursor{}},
		ClientSubscriberId: mustID("legacy-sub"),
	})
	if err != nil {
		return err
	}
	legacySummary, err := drainLegacyStream(ctx, stream, s.cfg.streamTimeout, true)
	if err != nil {
		return err
	}
	fmt.Printf("legacy_stream: events=%d pending=%d done_by=%s terminal=%s\n", legacySummary.events, legacySummary.pending, legacySummary.doneBy, legacySummary.terminalState)
	status, err := s.controlClient.GetTaskStatus(rpcCtx, &pb.GetTaskStatusRequest{
		Locator: &pb.GetTaskStatusRequest_TaskId{TaskId: start.GetTaskId()},
	})
	if err != nil {
		return err
	}
	fmt.Printf("legacy_status: state=%s pending=%d last_event=%d\n", status.GetState(), len(status.GetActivePendingRequests()), status.GetLastEventId())
	interrupt, err := s.controlClient.InterruptTask(rpcCtx, &pb.InterruptTaskRequest{
		Locator:         &pb.InterruptTaskRequest_TaskId{TaskId: start.GetTaskId()},
		ClientRequestId: mustID("legacy-interrupt"),
	})
	if err != nil {
		return err
	}
	fmt.Printf("legacy_interrupt_after_terminal: already_terminal=%v state=%s\n", interrupt.GetAlreadyTerminal(), interrupt.GetState())
	_, err = s.controlClient.RespondPendingRequest(rpcCtx, &pb.RespondPendingRequestRequest{
		TaskId:           start.GetTaskId(),
		PendingRequestId: "missing-pending",
		ClientResponseId: mustID("legacy-pending-response"),
		Response: &pb.RespondPendingRequestRequest_Approval{
			Approval: &pb.ApprovalPendingResponse{DecisionId: "decline"},
		},
	})
	if err == nil {
		return errors.New("legacy RespondPendingRequest with missing pending id unexpectedly succeeded")
	}
	fmt.Printf("legacy_respond_pending_expected_error: %s\n", safeStatusLine(err))
	return nil
}

func (s *suite) deleteWorkflow(ctx context.Context, workflow *codex.Workflow) error {
	response, err := s.workflowClient.DeleteWorkflow(s.authenticatedContext(ctx), &pb.DeleteWorkflowRequest{
		Workflow:                &pb.WorkflowSelector{Namespace: workflow.Namespace, WorkflowId: workflow.ID},
		Force:                   true,
		DeleteMaterializedState: true,
		CleanupRuntime:          true,
		ClientRequestId:         mustID("workflow-delete"),
		IdempotencyKey:          mustID("workflow-delete-idem"),
	})
	if err != nil {
		return err
	}
	fmt.Printf("workflow_delete: deleted=%v cleanup=%v lifecycle=%s\n", response.GetDeleted(), response.GetCleanupScheduled(), response.GetStatus().GetLifecycle())
	return nil
}

func (s *suite) authenticatedContext(ctx context.Context) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+s.token))
}

func expectPendingResponseErrors(ctx context.Context, chat *codex.Chat, label string) error {
	pendingID := "missing-pending"
	checks := []struct {
		name string
		call func() error
	}{
		{
			name: "approval",
			call: func() error {
				_, err := chat.RespondApproval(ctx, pendingID, "decline", codex.WithClientResponseID(mustID(label+"-pending-approval")), codex.WithIdempotencyKey(mustID(label+"-pending-approval-idem")))
				return err
			},
		},
		{
			name: "permissions",
			call: func() error {
				_, err := chat.RespondPermissions(ctx, pendingID, []string{"example.permission"}, pb.PermissionScope_PERMISSION_SCOPE_TURN, true, codex.WithClientResponseID(mustID(label+"-pending-permissions")), codex.WithIdempotencyKey(mustID(label+"-pending-permissions-idem")))
				return err
			},
		},
		{
			name: "mcp_elicitation",
			call: func() error {
				_, err := chat.RespondMcpElicitation(ctx, pendingID, pb.McpElicitationAction_MCP_ELICITATION_ACTION_DECLINE, "", codex.WithClientResponseID(mustID(label+"-pending-mcp")), codex.WithIdempotencyKey(mustID(label+"-pending-mcp-idem")))
				return err
			},
		},
		{
			name: "tool_user_input",
			call: func() error {
				_, err := chat.RespondToolUserInput(ctx, pendingID, []*pb.ToolUserInputAnswer{{QuestionId: "example", Answers: []string{"skip"}}}, codex.WithClientResponseID(mustID(label+"-pending-input")), codex.WithIdempotencyKey(mustID(label+"-pending-input-idem")))
				return err
			},
		},
	}
	for _, check := range checks {
		err := check.call()
		if err == nil {
			return fmt.Errorf("%s Respond%s unexpectedly succeeded", label, check.name)
		}
		fmt.Printf("%s_pending_%s_expected_error: %s\n", label, check.name, safeStatusLine(err))
	}
	return nil
}

func printChatSnapshot(ctx context.Context, chat *codex.Chat, label string) error {
	status, err := chat.GetStatus(ctx)
	if err != nil {
		return err
	}
	history, err := chat.GetHistory(
		ctx,
		codex.WithHistoryDepth(pb.ChatHistoryDepth_CHAT_HISTORY_DEPTH_TURN_SUMMARY),
		codex.WithHistoryLimit(20),
		codex.WithHistorySortDirection(pb.ChatHistorySortDirection_CHAT_HISTORY_SORT_DIRECTION_ASCENDING),
	)
	if err != nil {
		return err
	}
	fmt.Printf("%s_status: lookup=%s thread=%s run=%s pending=%d\n", label, status.GetLookup(), status.GetThreadLifecycle(), status.GetCurrentRunLifecycle(), len(status.GetActivePendingRequests()))
	fmt.Printf("%s_history: turns=%d returned_depth=%s\n", label, len(history.GetTurns()), history.GetReturnedDepth())
	return nil
}

func drainChatStream(ctx context.Context, events *codex.EventStream, timeout time.Duration, requireTerminal bool) (streamSummary, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	var summary streamSummary
	for {
		message, err := recvChatStream(ctx, events, deadline.C)
		if err != nil {
			if errors.Is(err, io.EOF) && (summary.terminal || summary.assistantCompleted) {
				return summary, nil
			}
			if (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) && summary.events > 0 && !requireTerminal {
				return summary, nil
			}
			return summary, err
		}
		consumeChatEvent(message, &summary)
		if summary.terminal {
			return summary, nil
		}
		if !requireTerminal && summary.events > 0 {
			return summary, nil
		}
	}
}

func drainReplayStream(ctx context.Context, events *codex.EventStream, timeout time.Duration, maxEvents int) (streamSummary, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	var summary streamSummary
	for summary.events < maxEvents {
		message, err := recvChatStream(ctx, events, deadline.C)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return summary, nil
			}
			if (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) && summary.events > 0 {
				return summary, nil
			}
			return summary, err
		}
		consumeChatEvent(message, &summary)
		if summary.terminal {
			return summary, nil
		}
	}
	return summary, nil
}

type chatRecvResult struct {
	message *pb.StreamChatEventsResponse
	err     error
}

func recvChatStream(ctx context.Context, events *codex.EventStream, deadline <-chan time.Time) (*pb.StreamChatEventsResponse, error) {
	received := make(chan chatRecvResult, 1)
	go func() {
		message, err := events.Recv()
		received <- chatRecvResult{message: message, err: err}
	}()
	select {
	case result := <-received:
		return result.message, result.err
	case <-deadline:
		_ = events.Close()
		return nil, context.DeadlineExceeded
	case <-ctx.Done():
		_ = events.Close()
		return nil, ctx.Err()
	}
}

func consumeChatEvent(message *pb.StreamChatEventsResponse, summary *streamSummary) {
	if message == nil {
		return
	}
	if event := message.GetEvent(); event != nil {
		summary.events++
		if event.GetEventId() > summary.maxEventID {
			summary.maxEventID = event.GetEventId()
		}
		if event.GetEventCursor() != "" {
			summary.eventCursor = event.GetEventCursor()
		}
		if delta := event.GetAssistantDelta(); delta != nil {
			appendSummaryText(summary, delta.GetTextDelta())
		}
		if completed := event.GetAssistantMessageCompleted(); completed != nil {
			summary.assistantCompleted = true
			if completed.GetMessage() != "" {
				appendSummaryText(summary, completed.GetMessage())
			}
			if summary.doneBy == "" {
				summary.doneBy = "assistant_message_completed"
			}
		}
		if pending := event.GetPendingRequestCreated(); pending != nil {
			summary.pending++
		}
		if terminal := event.GetTerminal(); terminal != nil {
			summary.terminal = true
			summary.doneBy = "terminal:" + terminal.GetTerminal().GetTerminalLifecycle().String()
		}
		return
	}
	if message.GetReplayNotice() != nil {
		summary.doneBy = "replay_notice"
	}
}

func appendSummaryText(summary *streamSummary, text string) {
	if text == "" {
		return
	}
	summary.textBytes += len(text)
	if len(summary.text) >= streamTextCaptureLimit {
		return
	}
	remaining := streamTextCaptureLimit - len(summary.text)
	if len(text) > remaining {
		text = text[:remaining]
	}
	summary.text += text
}

func drainLegacyStream(ctx context.Context, stream pb.CodexControl_StreamTaskClient, timeout time.Duration, requireTerminal bool) (legacyStreamSummary, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	var summary legacyStreamSummary
	for {
		message, err := recvLegacyStream(ctx, stream, deadline.C)
		if err != nil {
			if errors.Is(err, io.EOF) && summary.terminal {
				return summary, nil
			}
			return summary, err
		}
		if event := message.GetEvent(); event != nil {
			summary.events++
			if event.GetEventId() > summary.maxEventID {
				summary.maxEventID = event.GetEventId()
			}
			if delta := event.GetAssistantDelta(); delta != nil {
				summary.textBytes += len(delta.GetTextDelta())
			}
			if completed := event.GetAssistantMessageCompleted(); completed != nil {
				if completed.GetMessage() != "" {
					summary.textBytes += len(completed.GetMessage())
				}
				if summary.doneBy == "" {
					summary.doneBy = "assistant_message_completed"
				}
			}
			if event.GetPendingRequestCreated() != nil {
				summary.pending++
			}
			if terminal := event.GetTerminal(); terminal != nil {
				summary.terminal = true
				summary.terminalState = terminal.GetTerminalState().String()
				summary.doneBy = "terminal:" + summary.terminalState
				return summary, nil
			}
		}
		if !requireTerminal && summary.events > 0 {
			return summary, nil
		}
	}
}

type legacyRecvResult struct {
	message *pb.StreamTaskResponse
	err     error
}

func recvLegacyStream(ctx context.Context, stream pb.CodexControl_StreamTaskClient, deadline <-chan time.Time) (*pb.StreamTaskResponse, error) {
	received := make(chan legacyRecvResult, 1)
	go func() {
		message, err := stream.Recv()
		received <- legacyRecvResult{message: message, err: err}
	}()
	select {
	case result := <-received:
		return result.message, result.err
	case <-deadline:
		closeLegacyStream(stream)
		return nil, context.DeadlineExceeded
	case <-ctx.Done():
		closeLegacyStream(stream)
		return nil, ctx.Err()
	}
}

func closeLegacyStream(stream pb.CodexControl_StreamTaskClient) {
	type closeSender interface {
		CloseSend() error
	}
	if closer, ok := any(stream).(closeSender); ok {
		_ = closer.CloseSend()
	}
}

func streamAfterRun(result *codex.RunResult) codex.StreamOption {
	if result != nil && result.EventCursor != "" {
		return codex.AfterEventCursor(result.EventCursor)
	}
	if result != nil {
		return codex.AfterEventID(result.LastEventID)
	}
	return codex.AfterEventID(0)
}

func expectWorkflowError(label string, err error, code codex.WorkflowErrorCode) bool {
	if err == nil {
		fmt.Printf("%s_expected_error: missing\n", label)
		return false
	}
	sdkErr, ok := codex.AsError(err)
	if !ok || !sdkErr.IsWorkflow(code) {
		fmt.Printf("%s_expected_error: got=%s\n", label, safeStatusLine(err))
		return false
	}
	fmt.Printf("%s_expected_error: workflow_code=%s reason=%s next_action=%s\n", label, sdkErr.WorkflowCode, sdkErr.Reason, sdkErr.NextAction)
	return true
}

func normalizeExpectedError(label string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %s", label, safeStatusLine(err))
}

func safeStatusLine(err error) string {
	if sdkErr, ok := codex.AsError(err); ok {
		stage := safeErrorStage(err)
		message := sdkErr.DisplayMessage
		if message != "" && message != sdkErr.Reason {
			message = " message=" + message
		} else {
			message = ""
		}
		locator := ""
		if sdkErr.ChatID != "" {
			locator += " chat=" + shortID(sdkErr.ChatID)
		}
		if sdkErr.RunID != "" {
			locator += " run=" + shortID(sdkErr.RunID)
		}
		if sdkErr.WorkflowCode != "" {
			return fmt.Sprintf("%scode=%s workflow_code=%s reason=%s%s%s retryable=%v", stage, sdkErr.Code, sdkErr.WorkflowCode, sdkErr.Reason, message, locator, sdkErr.Retryable)
		}
		return fmt.Sprintf("%scode=%s reason=%s%s%s retryable=%v", stage, sdkErr.Code, sdkErr.Reason, message, locator, sdkErr.Retryable)
	}
	return err.Error()
}

func safeErrorStage(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	idx := strings.Index(text, ": code=")
	if idx <= 0 || idx > 80 || strings.ContainsAny(text[:idx], "\r\n") {
		return ""
	}
	return text[:idx] + ": "
}

func zipWorkflowDir(root string) ([]byte, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		left, _ := filepath.Rel(root, files[i])
		right, _ := filepath.Rel(root, files[j])
		return filepath.ToSlash(left) < filepath.ToSlash(right)
	})
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, file := range files {
		rel, err := filepath.Rel(root, file)
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		writer, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		contents, err := os.ReadFile(file)
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		if _, err := writer.Write(contents); err != nil {
			_ = zw.Close()
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func readTokenSource(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read token-source: %w", err)
	}
	token := strings.TrimSpace(string(contents))
	if token == "" {
		return "", errors.New("token-source is empty")
	}
	return token, nil
}

func mustID(prefix string) string {
	id, err := randomID(prefix)
	if err != nil {
		panic(err)
	}
	return id
}

func randomID(prefix string) (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(raw[:]), nil
}

func shortID(value string) string {
	if value == "" {
		return "-"
	}
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func validateLocalGatewayAddr(target string) error {
	host := gatewayHost(target)
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("gateway address must be loopback when using insecure local credentials")
}

func gatewayHost(target string) string {
	trimmed := strings.TrimSpace(target)
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Scheme != "" {
		switch {
		case parsed.Host != "":
			trimmed = parsed.Host
		case parsed.Path != "":
			trimmed = strings.TrimLeft(parsed.Path, "/")
		}
	}
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(trimmed, "[]")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func printSafeError(label string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", label, safeStatusLine(err))
}
