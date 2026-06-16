package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	codex "github.com/Dirard/codex-runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	gatewayAddr := os.Getenv("CODEX_RUNTIME_GATEWAY_ADDR")
	if gatewayAddr == "" {
		gatewayAddr = "127.0.0.1:5575"
	}
	if err := validateLocalGatewayAddr(gatewayAddr); err != nil {
		log.Fatal(err)
	}
	conn, err := grpc.NewClient(gatewayAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	client, err := codex.New(
		conn,
		codex.WithBearerToken(os.Getenv("CODEX_RUNTIME_TOKEN")),
		codex.WithSessionGroupID(os.Getenv("CODEX_RUNTIME_SESSION_GROUP")),
		codex.WithWorkspaceID(os.Getenv("CODEX_RUNTIME_WORKSPACE")),
	)
	if err != nil {
		log.Fatal(err)
	}
	codex.SetDefaultClient(client)

	http.HandleFunc("/chat", runChat)
	log.Fatal(http.ListenAndServe("127.0.0.1:8080", nil))
}

func runChat(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if strings.TrimSpace(r.FormValue("prompt")) == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}

	chat, events, err := codex.Run(
		ctx,
		r.FormValue("prompt"),
		codex.WithClientMessageID(r.Header.Get("X-Client-Message-ID")),
		codex.WithIdempotencyKey(r.Header.Get("Idempotency-Key")),
	)
	if err != nil {
		http.Error(w, safeError(err), http.StatusBadGateway)
		return
	}
	defer events.Close()

	fmt.Fprintf(w, "chat_id=%s\n", chat.ID)
}

func safeError(err error) string {
	return "codex runtime request failed"
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
