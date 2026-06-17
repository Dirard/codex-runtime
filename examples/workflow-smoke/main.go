package main

import (
	"context"
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

	codex "github.com/Dirard/codex-runtime/sdk/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const defaultGatewayAddress = "127.0.0.1:5575"

type smokeConfig struct {
	gatewayAddr    string
	tokenSource    string
	sessionGroupID string
	workspaceID    string
	namespace      string
	workflowID     string
	workflowDir    string
	prompt         string
	continuePrompt string
	sourceQuery    string
	timeout        time.Duration
}

type streamSummary struct {
	events             int
	assistantCompleted bool
	doneBy             string
	maxEventID         uint64
	eventCursor        string
}

type localSource struct {
	Title     string
	URI       string
	LineStart int
	Excerpt   string
}

func main() {
	cfg := readSmokeConfig()
	if err := runSmoke(cfg); err != nil {
		printSafeError(err)
		os.Exit(1)
	}
}

func readSmokeConfig() smokeConfig {
	cfg := smokeConfig{
		gatewayAddr:    firstNonEmpty(os.Getenv("CODEX_RUNTIME_GATEWAY_ADDR"), defaultGatewayAddress),
		tokenSource:    os.Getenv("CODEX_RUNTIME_TOKEN_FILE"),
		sessionGroupID: firstNonEmpty(os.Getenv("CODEX_RUNTIME_SESSION_GROUP"), "workflow-smoke-session"),
		workspaceID:    firstNonEmpty(os.Getenv("CODEX_RUNTIME_WORKSPACE"), "workflow-smoke-workspace"),
		namespace:      firstNonEmpty(os.Getenv("CODEX_WORKFLOW_NAMESPACE"), "examples"),
		workflowID:     firstNonEmpty(os.Getenv("CODEX_WORKFLOW_ID"), "writer-notes"),
		workflowDir:    firstNonEmpty(os.Getenv("CODEX_WORKFLOW_DIR"), filepath.FromSlash("examples/workflows/writer-notes")),
		prompt:         firstNonEmpty(os.Getenv("CODEX_WORKFLOW_PROMPT"), "Summarize the harbor fire note in two sentences and cite the shipped source title."),
		continuePrompt: firstNonEmpty(os.Getenv("CODEX_WORKFLOW_CONTINUE_PROMPT"), "Now make it suitable for a city newsletter."),
		sourceQuery:    firstNonEmpty(os.Getenv("CODEX_WORKFLOW_SOURCE_QUERY"), "harbor"),
		timeout:        2 * time.Minute,
	}
	flag.StringVar(&cfg.gatewayAddr, "gateway", cfg.gatewayAddr, "loopback Codex runtime gateway address")
	flag.StringVar(&cfg.tokenSource, "token-source", cfg.tokenSource, "path to a file containing the gateway bearer token")
	flag.StringVar(&cfg.sessionGroupID, "session-group", cfg.sessionGroupID, "SDK session group id used by the local backend")
	flag.StringVar(&cfg.workspaceID, "workspace", cfg.workspaceID, "SDK workspace id used by the local backend")
	flag.StringVar(&cfg.namespace, "namespace", cfg.namespace, "workflow namespace to initialize")
	flag.StringVar(&cfg.workflowID, "workflow-id", cfg.workflowID, "workflow id to initialize")
	flag.StringVar(&cfg.workflowDir, "workflow-dir", cfg.workflowDir, "starter workflow directory to package")
	flag.StringVar(&cfg.prompt, "prompt", cfg.prompt, "first workflow prompt")
	flag.StringVar(&cfg.continuePrompt, "continue-prompt", cfg.continuePrompt, "second prompt for the same workflow chat")
	flag.StringVar(&cfg.sourceQuery, "source-query", cfg.sourceQuery, "local shipped-source proof query; empty disables source proof")
	flag.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "smoke timeout")
	flag.Parse()
	return cfg
}

func runSmoke(cfg smokeConfig) error {
	if err := validateSmokeConfig(cfg); err != nil {
		return err
	}
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
	)
	if err != nil {
		return err
	}
	workflow, err := client.InitWorkflow(ctx, codex.WorkflowDir{
		Namespace: cfg.namespace,
		ID:        cfg.workflowID,
		Path:      cfg.workflowDir,
	})
	if err != nil {
		return err
	}
	fmt.Printf("workflow: %s/%s restart_required=%v\n", workflow.Namespace, workflow.ID, workflow.RestartRequired)
	printLocalSourceProof(cfg.workflowDir, cfg.sourceQuery)

	chat, events, err := workflow.Run(ctx, cfg.prompt)
	if err != nil {
		return err
	}
	defer events.Close()
	fmt.Printf("chat_id: %s\n", chat.ID)
	first, err := printStream(ctx, events)
	if err != nil {
		return err
	}
	fmt.Printf("first_turn: events=%d done_by=%s cursor=%s\n", first.events, first.doneBy, first.eventCursor)

	history, err := chat.GetHistory(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("history: chat_id=%s turns=%d\n", history.GetChatId(), len(history.GetTurns()))

	if strings.TrimSpace(cfg.continuePrompt) == "" {
		return nil
	}
	result, err := chat.Run(ctx, cfg.continuePrompt)
	if err != nil {
		return err
	}
	events, err = chat.GetEventsStream(ctx, streamAfterRun(result))
	if err != nil {
		return err
	}
	defer events.Close()
	second, err := printStream(ctx, events)
	if err != nil {
		return err
	}
	fmt.Printf("second_turn: events=%d done_by=%s cursor=%s\n", second.events, second.doneBy, second.eventCursor)
	return nil
}

func printStream(ctx context.Context, events *codex.EventStream) (streamSummary, error) {
	var summary streamSummary
	for {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		message, err := events.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) && summary.assistantCompleted {
				return summary, nil
			}
			return summary, err
		}
		if narrowed := message.GetNarrowed(); narrowed != nil {
			return summary, fmt.Errorf("event stream narrowed: %s", narrowed.GetReason())
		}
		event := message.GetEvent()
		if event == nil {
			continue
		}
		summary.events++
		if event.GetEventId() > summary.maxEventID {
			summary.maxEventID = event.GetEventId()
		}
		if event.GetEventCursor() != "" {
			summary.eventCursor = event.GetEventCursor()
		}
		if delta := event.GetAssistantDelta(); delta != nil {
			fmt.Print(delta.GetTextDelta())
		}
		if completed := event.GetAssistantMessageCompleted(); completed != nil {
			summary.assistantCompleted = true
			if completed.GetMessage() != "" {
				fmt.Print(completed.GetMessage())
			}
			fmt.Println()
			if summary.doneBy == "" {
				summary.doneBy = "assistant_message_completed"
			}
		}
		if terminal := event.GetTerminal(); terminal != nil {
			if summary.doneBy == "" {
				summary.doneBy = "terminal"
			}
			return summary, nil
		}
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

func printLocalSourceProof(workflowDir string, query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		fmt.Println("source_proof: disabled")
		return
	}
	source, ok := findLocalSource(filepath.Join(workflowDir, "references"), query)
	if !ok {
		fmt.Println("source_proof: no shipped source matched")
		return
	}
	fmt.Printf("source_proof: title=%q uri=%s line=%d excerpt=%q\n", source.Title, source.URI, source.LineStart, source.Excerpt)
}

func findLocalSource(root string, query string) (localSource, bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return localSource{}, false
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	needle := strings.ToLower(query)
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		if source, ok := sourceFromMarkdown(entry.Name(), string(contents), needle); ok {
			return source, true
		}
	}
	return localSource{}, false
}

func sourceFromMarkdown(name string, markdown string, needle string) (localSource, bool) {
	lines := strings.Split(markdown, "\n")
	title := strings.TrimSuffix(name, filepath.Ext(name))
	uri := "writer-notes://" + strings.TrimSuffix(name, filepath.Ext(name))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "stable uri:") {
			uri = strings.TrimSpace(strings.TrimPrefix(trimmed, "Stable URI:"))
		}
	}
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(lower, "source id:") || strings.HasPrefix(lower, "stable uri:") {
			continue
		}
		if strings.Contains(lower, needle) {
			return localSource{Title: title, URI: uri, LineStart: index + 1, Excerpt: trimmed}, true
		}
	}
	return localSource{}, false
}

func validateSmokeConfig(cfg smokeConfig) error {
	if err := validateLocalGatewayAddr(cfg.gatewayAddr); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.tokenSource) == "" {
		return errors.New("token-source is required")
	}
	if strings.TrimSpace(cfg.namespace) == "" || strings.TrimSpace(cfg.workflowID) == "" {
		return errors.New("namespace and workflow-id are required")
	}
	if strings.TrimSpace(cfg.workflowDir) == "" {
		return errors.New("workflow-dir is required")
	}
	if strings.TrimSpace(cfg.prompt) == "" {
		return errors.New("prompt is required")
	}
	if cfg.timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	return nil
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

func printSafeError(err error) {
	if sdkErr, ok := codex.AsError(err); ok {
		fmt.Fprintf(os.Stderr, "workflow-smoke: code=%s workflow_code=%s reason=%s next_action=%s retryable=%v\n", sdkErr.Code, sdkErr.WorkflowCode, sdkErr.Reason, sdkErr.NextAction, sdkErr.Retryable)
		return
	}
	fmt.Fprintf(os.Stderr, "workflow-smoke: %v\n", err)
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
