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
	eventCursor        string
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

	result, events, err := chat.RunWithEvents(ctx, cfg.continuePrompt)
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
	fmt.Printf("second_turn: run_id=%s events=%d done_by=%s terminal=%s\n", result.RunID, continueSummary.events, continueSummary.doneBy, continueSummary.terminalLifecycle)

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
		event, err := events.NextEvent(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) && summary.assistantCompleted {
				return summary, nil
			}
			return summary, err
		}
		summary.events++
		if event.Meta().ID > summary.maxEventID {
			summary.maxEventID = event.Meta().ID
		}
		if event.Meta().Cursor != "" {
			summary.eventCursor = event.Meta().Cursor
		}

		switch typed := event.(type) {
		case *codex.ReplayNotice:
			fmt.Fprintf(w, "\n[replay notice: %s]\n", typed.Code())
		case *codex.StreamNarrowed:
			return summary, fmt.Errorf("event stream narrowed: %s", typed.Reason())
		case *codex.AssistantTextDelta:
			printedDelta = true
			fmt.Fprint(w, typed.TextDelta())
		case *codex.AssistantMessageCompleted:
			summary.assistantCompleted = true
			if !printedDelta && typed.Message() != "" {
				fmt.Fprint(w, typed.Message())
			}
			fmt.Fprintln(w)
			if summary.doneBy == "" {
				summary.doneBy = "assistant_message_completed"
			}
		case codex.PendingAction:
			display := typed.Display()
			fmt.Fprintf(w, "\n[pending: %s] %s\n", typed.PendingKind(), display.Title)
		case *codex.CommandStarted:
			command := typed.Command()
			fmt.Fprintf(w, "\n[command: %s]\n", command.Display)
		case *codex.CommandOutput:
			command := typed.Command()
			if command.Known {
				fmt.Fprintf(w, "\n[command output %s %s] %s", command.ID, typed.Stream(), typed.Delta())
			} else {
				fmt.Fprintf(w, "\n[command output unknown %s] %s", typed.Stream(), typed.Delta())
			}
		case *codex.Warning:
			fmt.Fprintf(w, "\n[warning] %s\n", typed.Message())
		case codex.TerminalEvent:
			summary.doneBy = "terminal"
			summary.terminalLifecycle = string(typed.Result().State)
			if summary.terminalLifecycle == "" {
				summary.terminalLifecycle = "terminal"
			}
			return summary, nil
		}
	}
}

func printChatSnapshot(ctx context.Context, chat *codex.Chat) error {
	status, err := chat.GetStatusSnapshot(ctx)
	if err != nil {
		return err
	}
	history, err := chat.GetHistoryPage(
		ctx,
		codex.WithHistoryPageDepth(codex.HistoryDepthTurnSummary),
		codex.WithHistoryPageLimit(20),
		codex.WithHistoryPageSort(codex.HistorySortAscending),
	)
	if err != nil {
		return err
	}

	fmt.Printf(
		"status: lookup=%s thread=%s current_run=%s pending=%d\n",
		status.ChatID,
		status.ThreadLifecycle,
		status.RunLifecycle,
		len(status.Pending),
	)
	fmt.Printf("history: turns=%d returned_depth=%s\n", len(history.Turns), history.ReturnedDepth)
	return nil
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
