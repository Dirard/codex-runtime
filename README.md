# Codex Runtime

Experimental chat-first Go SDK and local runtime gateway for running Codex as an
external child process via `codex app-server --listen stdio://`.

The gateway loads a strict trusted configuration, starts one app-server
supervisor per configured session group, exposes the authenticated loopback
`codex.control.v1.ChatRuntimeService` gRPC service for chat-first SDK calls, and
keeps the legacy task compatibility service available for existing callers. The
command does not expose raw JSON-RPC, config mutation, file-manager,
plugin-manager, or MCP admin RPCs.

Configuration is loaded from a deliberately strict TOML subset. Supported
syntax is limited to table headers, array-table headers, `key = value`
assignments, double-quoted basic strings with TOML escapes, decimal integers,
booleans for rejected override syntax checks, comments, and string arrays using
JSON-compatible syntax. Duplicate keys, dotted keys, inline tables, literal
strings, multiline strings, dates, floats, and other TOML features are rejected.

```toml
codex_binary = "C:\\path\\to\\codex.exe"
listen = "127.0.0.1:0"
child_env_allowlist = ["GATEWAY_SAFE_ENV"]

[client_auth_token_source]
env = "CODEX_RUNTIME_GATEWAY_TOKEN"

[[session_groups]]
session_group_id = "local-main"
workspace_id = "workspace-main"
cwd = "D:\\work\\project"
codex_home = "D:\\work\\codex-home"

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
```

Start the gateway as a long-running local process:

```sh
go run ./gateway/cmd/codex-runtime-gateway --config ./gateway.toml
```

The process prints the concrete loopback address, then serves until it receives
SIGINT or SIGTERM. To use a stable local port during manual validation, pass a
loopback override:

```sh
go run ./gateway/cmd/codex-runtime-gateway --config ./gateway.toml --listen 127.0.0.1:39000
```

Every RPC must include `authorization: Bearer <token>`, where the token is read
from `[client_auth_token_source]` during startup. Use fixture tokens for local
tests; do not point validation at live credentials unless that smoke is
explicitly in scope.

SDK applications connect to the already running gateway:

```sh
go run ./examples/e2e-chat \
  -gateway 127.0.0.1:39000 \
  -token "$CODEX_RUNTIME_GATEWAY_TOKEN" \
  -session-group local-main \
  -workspace workspace-main \
  -prompt "Say hello in one short sentence." \
  -continue-prompt "Say goodbye in one short sentence."
```

## Releases

GitHub releases are produced by GoReleaser from tags named `v*`.

The release publishes the `codex-runtime-gateway` binary for:

- Linux `amd64` and `arm64`
- macOS `amd64` and `arm64`
- Windows `amd64` and `arm64`

Linux and macOS artifacts are `tar.gz` archives. Windows artifacts are `zip`
archives. Every release also includes a SHA-256 checksum file. The gateway
binary supports:

```sh
codex-runtime-gateway --version
```

The release does not bundle Codex itself. Users still configure
`codex_binary`, `codex_home`, workspace, and local bearer-token authentication
in the gateway TOML file.

Local release checks:

```sh
goreleaser check
goreleaser build --snapshot --clean
```

Publishing `v0.0.1` is done by pushing the tag:

```sh
git tag v0.0.1
git push origin v0.0.1
```
