package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	codex "github.com/Dirard/codex-runtime/sdk/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

const defaultGatewayAddress = "127.0.0.1:5575"

type probeConfig struct {
	gatewayAddr    string
	tokenSource    string
	sessionGroupID string
	workspaceID    string
	timeout        time.Duration
}

func main() {
	cfg := readProbeConfig()
	if err := runProbe(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "workflow-probe: %v\n", err)
		os.Exit(1)
	}
}

func readProbeConfig() probeConfig {
	cfg := probeConfig{
		gatewayAddr:    firstNonEmpty(os.Getenv("CODEX_RUNTIME_GATEWAY_ADDR"), defaultGatewayAddress),
		tokenSource:    os.Getenv("CODEX_RUNTIME_TOKEN_FILE"),
		sessionGroupID: firstNonEmpty(os.Getenv("CODEX_RUNTIME_SESSION_GROUP"), "workflow-probe-session"),
		workspaceID:    firstNonEmpty(os.Getenv("CODEX_RUNTIME_WORKSPACE"), "workflow-probe-workspace"),
		timeout:        10 * time.Second,
	}
	flag.StringVar(&cfg.gatewayAddr, "gateway", cfg.gatewayAddr, "loopback Codex runtime gateway address")
	flag.StringVar(&cfg.tokenSource, "token-source", cfg.tokenSource, "path to a file containing the gateway bearer token")
	flag.StringVar(&cfg.sessionGroupID, "session-group", cfg.sessionGroupID, "SDK session group id used for local probing")
	flag.StringVar(&cfg.workspaceID, "workspace", cfg.workspaceID, "SDK workspace id used for local probing")
	flag.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "probe timeout")
	flag.Parse()
	return cfg
}

func runProbe(cfg probeConfig) error {
	if err := validateProbeConfig(cfg); err != nil {
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

	if _, err := codex.New(
		conn,
		codex.WithBearerToken(token),
		codex.WithSessionGroupID(cfg.sessionGroupID),
		codex.WithWorkspaceID(cfg.workspaceID),
	); err != nil {
		return err
	}
	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))
	health, err := healthpb.NewHealthClient(conn).Check(authCtx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return err
	}
	fmt.Printf("gateway: %s\n", cfg.gatewayAddr)
	fmt.Printf("token_source: configured (redacted)\n")
	fmt.Printf("sdk: configured\n")
	fmt.Printf("health: %s\n", health.GetStatus())
	return nil
}

func validateProbeConfig(cfg probeConfig) error {
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
