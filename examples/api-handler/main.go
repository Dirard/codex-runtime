package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	codex "github.com/Dirard/codex-runtime/sdk/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
)

const defaultGatewayAddress = "127.0.0.1:5575"

type serverConfig struct {
	gatewayAddr    string
	tokenSource    string
	sessionGroupID string
	workspaceID    string
	namespace      string
	workflowID     string
	workflowDir    string
	timeout        time.Duration
}

type workflowHandler struct {
	workflow *codex.Workflow
	timeout  time.Duration

	mu         sync.Mutex
	lastChatID string
}

func main() {
	cfg := readServerConfig()
	if err := cfg.validate(); err != nil {
		log.Fatal(err)
	}
	token, err := readTokenSource(cfg.tokenSource)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := grpc.NewClient(cfg.gatewayAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	client, err := codex.New(
		conn,
		codex.WithBearerToken(token),
		codex.WithSessionGroupID(cfg.sessionGroupID),
		codex.WithWorkspaceID(cfg.workspaceID),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	workflow, err := client.InitWorkflow(ctx, codex.WorkflowDir{
		Namespace: cfg.namespace,
		ID:        cfg.workflowID,
		Path:      cfg.workflowDir,
	}, codex.WithWorkflowMCPReload(true))
	if err != nil {
		log.Fatal(safeError(err))
	}

	handler := &workflowHandler{workflow: workflow, timeout: cfg.timeout}
	http.HandleFunc("/workflow", handler.runWorkflow)
	log.Fatal(http.ListenAndServe("127.0.0.1:8080", nil))
}

func readServerConfig() serverConfig {
	return serverConfig{
		gatewayAddr:    firstNonEmpty(os.Getenv("CODEX_RUNTIME_GATEWAY_ADDR"), defaultGatewayAddress),
		tokenSource:    firstNonEmpty(os.Getenv("CODEX_RUNTIME_TOKEN_SOURCE"), ".\\.local\\gateway.token"),
		sessionGroupID: firstNonEmpty(os.Getenv("CODEX_RUNTIME_SESSION_GROUP"), "workflow-smoke-session"),
		workspaceID:    firstNonEmpty(os.Getenv("CODEX_RUNTIME_WORKSPACE"), "workflow-smoke-workspace"),
		namespace:      firstNonEmpty(os.Getenv("CODEX_WORKFLOW_NAMESPACE"), "examples"),
		workflowID:     firstNonEmpty(os.Getenv("CODEX_WORKFLOW_ID"), "writer-notes"),
		workflowDir:    firstNonEmpty(os.Getenv("CODEX_WORKFLOW_DIR"), ".\\examples\\workflows\\writer-notes"),
		timeout:        2 * time.Minute,
	}
}

func (cfg serverConfig) validate() error {
	if err := validateLocalGatewayAddr(cfg.gatewayAddr); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.tokenSource) == "" {
		return errors.New("CODEX_RUNTIME_TOKEN_SOURCE is required")
	}
	if strings.TrimSpace(cfg.sessionGroupID) == "" {
		return errors.New("CODEX_RUNTIME_SESSION_GROUP is required")
	}
	if strings.TrimSpace(cfg.workspaceID) == "" {
		return errors.New("CODEX_RUNTIME_WORKSPACE is required")
	}
	if strings.TrimSpace(cfg.namespace) == "" {
		return errors.New("CODEX_WORKFLOW_NAMESPACE is required")
	}
	if strings.TrimSpace(cfg.workflowID) == "" {
		return errors.New("CODEX_WORKFLOW_ID is required")
	}
	if strings.TrimSpace(cfg.workflowDir) == "" {
		return errors.New("CODEX_WORKFLOW_DIR is required")
	}
	return nil
}

func (h *workflowHandler) runWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	prompt := strings.TrimSpace(r.FormValue("prompt"))
	if prompt == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}

	chat, events, err := h.startOrContinue(ctx, r.FormValue("chat_id"), prompt, r)
	if err != nil {
		writeSafeHTTPError(w, err)
		return
	}
	defer events.Close()

	h.rememberChat(chat.ID)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "chat_id=%s\nassistant:\n", chat.ID)
	if err := writeAssistantStream(ctx, w, events); err != nil {
		fmt.Fprintf(w, "\nstream_error=%s\n", safeError(err))
	}
}

func (h *workflowHandler) startOrContinue(ctx context.Context, chatID string, prompt string, r *http.Request) (*codex.Chat, *codex.EventStream, error) {
	opts := []codex.RequestOption{
		codex.WithClientMessageID(r.Header.Get("X-Client-Message-ID")),
		codex.WithIdempotencyKey(r.Header.Get("Idempotency-Key")),
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return h.workflow.Run(ctx, prompt, opts...)
	}
	chat, err := h.workflow.GetChat(ctx, chatID)
	if err != nil {
		return nil, nil, err
	}
	result, err := chat.Run(ctx, prompt, opts...)
	if err != nil {
		return nil, nil, err
	}
	events, err := chat.GetEventsStream(ctx, streamAfterRun(result))
	if err != nil {
		return nil, nil, err
	}
	return chat, events, nil
}

func (h *workflowHandler) rememberChat(chatID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastChatID = chatID
}

func writeAssistantStream(ctx context.Context, w http.ResponseWriter, events *codex.EventStream) error {
	flusher, _ := w.(http.Flusher)
	printedDelta := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		message, err := events.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if narrowed := message.GetNarrowed(); narrowed != nil {
			return fmt.Errorf("event stream narrowed: %s", narrowed.GetReason())
		}
		event := message.GetEvent()
		if event == nil {
			continue
		}
		if delta := event.GetAssistantDelta(); delta != nil {
			printedDelta = true
			fmt.Fprint(w, delta.GetTextDelta())
			if flusher != nil {
				flusher.Flush()
			}
		}
		if completed := event.GetAssistantMessageCompleted(); completed != nil {
			if !printedDelta && completed.GetMessage() != "" {
				fmt.Fprint(w, completed.GetMessage())
			}
			fmt.Fprintln(w)
			return nil
		}
		if event.GetTerminal() != nil {
			return nil
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

func writeSafeHTTPError(w http.ResponseWriter, err error) {
	http.Error(w, safeError(err), safeHTTPStatus(err))
}

func safeError(err error) string {
	if sdkErr, ok := codex.AsError(err); ok {
		parts := []string{
			"codex runtime request failed",
			"code=" + sdkErr.Code.String(),
		}
		if sdkErr.WorkflowCode != "" {
			parts = append(parts, "workflow_code="+string(sdkErr.WorkflowCode))
		}
		if sdkErr.Reason != "" {
			parts = append(parts, "reason="+sdkErr.Reason)
		}
		if sdkErr.NextAction != "" {
			parts = append(parts, "next_action="+sdkErr.NextAction)
		}
		return strings.Join(parts, " ")
	}
	return "codex runtime request failed"
}

func safeHTTPStatus(err error) int {
	if sdkErr, ok := codex.AsError(err); ok {
		switch sdkErr.Code {
		case codes.InvalidArgument:
			return http.StatusBadRequest
		case codes.Unauthenticated:
			return http.StatusUnauthorized
		case codes.PermissionDenied:
			return http.StatusForbidden
		case codes.NotFound:
			return http.StatusNotFound
		case codes.FailedPrecondition, codes.Aborted:
			return http.StatusConflict
		case codes.Unavailable, codes.DeadlineExceeded:
			return http.StatusServiceUnavailable
		}
	}
	return http.StatusBadGateway
}

func readTokenSource(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read token source: %w", err)
	}
	token := strings.TrimSpace(string(contents))
	if token == "" {
		return "", errors.New("token source is empty")
	}
	return token, nil
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
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
