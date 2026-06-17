package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	codex "github.com/Dirard/codex-runtime/sdk/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const defaultGatewayAddress = "127.0.0.1:5575"

type config struct {
	gatewayAddr    string
	token          string
	tokenSource    string
	sessionGroupID string
	workspaceID    string
	prompt         string
	continuePrompt string
	timeout        time.Duration
}

type streamSummary struct {
	events             int
	assistantCompleted bool
	doneBy             string
	terminalLifecycle  string
	maxEventID         uint64
}

func main() {
	cfg := readConfig()
	if err := cfg.validate(); err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	conn, err := grpc.NewClient(cfg.gatewayAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	token, err := cfg.bearerToken()
	if err != nil {
		log.Fatal(err)
	}
	client, err := codex.New(
		conn,
		codex.WithBearerToken(token),
		codex.WithSessionGroupID(cfg.sessionGroupID),
		codex.WithWorkspaceID(cfg.workspaceID),
	)
	if err != nil {
		log.Fatal(err)
	}
	codex.SetDefaultClient(client)

	chat, events, err := codex.Run(ctx, cfg.prompt)
	if err != nil {
		log.Fatal(err)
	}
	defer events.Close()

	fmt.Printf("chat_id: %s\n", chat.ID)
	fmt.Println("assistant:")
	startSummary, err := printStream(ctx, os.Stdout, events)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("first_turn: events=%d done_by=%s terminal=%s\n", startSummary.events, startSummary.doneBy, startSummary.terminalLifecycle)

	chat, err = codex.GetChat(ctx, chat.ID)
	if err != nil {
		log.Fatal(err)
	}
	if err := printChatSnapshot(ctx, chat); err != nil {
		log.Fatal(err)
	}

	if strings.TrimSpace(cfg.continuePrompt) == "" {
		return
	}

	result, err := chat.Run(ctx, cfg.continuePrompt)
	if err != nil {
		log.Fatal(err)
	}
	events, err = chat.GetEventsStream(ctx, streamAfterRun(result))
	if err != nil {
		log.Fatal(err)
	}
	defer events.Close()

	fmt.Println()
	fmt.Println("assistant:")
	continueSummary, err := printStream(ctx, os.Stdout, events)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("second_turn: events=%d done_by=%s terminal=%s\n", continueSummary.events, continueSummary.doneBy, continueSummary.terminalLifecycle)

	if err := printChatSnapshot(ctx, chat); err != nil {
		log.Fatal(err)
	}
}

func readConfig() config {
	cfg := config{
		gatewayAddr:    firstNonEmpty(os.Getenv("CODEX_RUNTIME_GATEWAY_ADDR"), defaultGatewayAddress),
		token:          os.Getenv("CODEX_RUNTIME_TOKEN"),
		tokenSource:    os.Getenv("CODEX_RUNTIME_TOKEN_SOURCE"),
		sessionGroupID: os.Getenv("CODEX_RUNTIME_SESSION_GROUP"),
		workspaceID:    os.Getenv("CODEX_RUNTIME_WORKSPACE"),
		prompt: firstNonEmpty(
			os.Getenv("CODEX_RUNTIME_PROMPT"),
			"Reply with one short sentence that confirms the Codex SDK e2e example is connected. Do not inspect files, run commands, or modify files.",
		),
		continuePrompt: os.Getenv("CODEX_RUNTIME_CONTINUE_PROMPT"),
		timeout:        10 * time.Minute,
	}

	flag.StringVar(&cfg.gatewayAddr, "gateway", cfg.gatewayAddr, "loopback Codex runtime gateway address")
	flag.StringVar(&cfg.token, "token", cfg.token, "gateway bearer token")
	flag.StringVar(&cfg.tokenSource, "token-source", cfg.tokenSource, "path to a file containing the gateway bearer token")
	flag.StringVar(&cfg.sessionGroupID, "session-group", cfg.sessionGroupID, "gateway session group id")
	flag.StringVar(&cfg.workspaceID, "workspace", cfg.workspaceID, "gateway workspace id")
	flag.StringVar(&cfg.prompt, "prompt", cfg.prompt, "first prompt to send to Codex")
	flag.StringVar(&cfg.continuePrompt, "continue-prompt", cfg.continuePrompt, "optional second prompt for the same chat")
	flag.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "end-to-end request timeout")
	flag.Parse()

	return cfg
}

func (cfg config) validate() error {
	if err := validateLocalGatewayAddr(cfg.gatewayAddr); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.token) == "" && strings.TrimSpace(cfg.tokenSource) == "" {
		return errors.New("CODEX_RUNTIME_TOKEN_SOURCE/-token-source or CODEX_RUNTIME_TOKEN/-token is required")
	}
	if strings.TrimSpace(cfg.sessionGroupID) == "" {
		return errors.New("CODEX_RUNTIME_SESSION_GROUP or -session-group is required")
	}
	if strings.TrimSpace(cfg.workspaceID) == "" {
		return errors.New("CODEX_RUNTIME_WORKSPACE or -workspace is required")
	}
	if strings.TrimSpace(cfg.prompt) == "" {
		return errors.New("prompt is required")
	}
	if cfg.timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	return nil
}

func (cfg config) bearerToken() (string, error) {
	if strings.TrimSpace(cfg.token) != "" {
		return cfg.token, nil
	}
	contents, err := os.ReadFile(cfg.tokenSource)
	if err != nil {
		return "", fmt.Errorf("read token source: %w", err)
	}
	token := strings.TrimSpace(string(contents))
	if token == "" {
		return "", errors.New("token source is empty")
	}
	return token, nil
}

func printStream(ctx context.Context, w io.Writer, events *codex.EventStream) (streamSummary, error) {
	var summary streamSummary
	printedDelta := false

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
		if notice := message.GetReplayNotice(); notice != nil {
			fmt.Fprintf(w, "\n[replay notice: %s]\n", notice.GetCode())
			continue
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

		if delta := event.GetAssistantDelta(); delta != nil {
			printedDelta = true
			fmt.Fprint(w, delta.GetTextDelta())
		}
		if completed := event.GetAssistantMessageCompleted(); completed != nil {
			summary.assistantCompleted = true
			if !printedDelta && completed.GetMessage() != "" {
				fmt.Fprint(w, completed.GetMessage())
			}
			fmt.Fprintln(w)
			if summary.doneBy == "" {
				summary.doneBy = "assistant_message_completed"
			}
			continue
		}
		if pending := event.GetPendingRequestCreated(); pending != nil {
			fmt.Fprintf(w, "\n[pending: %s]\n", pending.GetPendingRequest().GetPendingType())
		}
		if terminal := event.GetTerminal(); terminal != nil {
			summary.doneBy = "terminal"
			summary.terminalLifecycle = terminal.GetTerminal().GetTerminalLifecycle().String()
			if summary.terminalLifecycle == "" {
				summary.terminalLifecycle = "terminal"
			}
			return summary, nil
		}
	}
}

func printChatSnapshot(ctx context.Context, chat *codex.Chat) error {
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

	fmt.Printf(
		"status: lookup=%s thread=%s current_run=%s pending=%d\n",
		status.GetLookup(),
		status.GetThreadLifecycle(),
		status.GetCurrentRunLifecycle(),
		len(status.GetActivePendingRequests()),
	)
	fmt.Printf("history: turns=%d returned_depth=%s\n", len(history.GetTurns()), history.GetReturnedDepth())
	return nil
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
