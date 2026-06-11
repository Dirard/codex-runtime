# Codex Control Gateway

Experimental Go control gateway for running Codex as an external child process via
`codex app-server --listen stdio://`.

The gateway loads a strict trusted configuration, starts one app-server
supervisor per configured session group, exposes the authenticated loopback
`codex.control.v1.CodexControl` gRPC service, and routes task, stream, status,
interrupt, and pending-response RPCs through the Go domain services. The command
does not expose raw JSON-RPC, config mutation, file-manager, plugin-manager, or
MCP admin RPCs.

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
env = "CODEX_CONTROL_GATEWAY_TOKEN"

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

Start the gateway:

```sh
go run ./cmd/codex-control-gateway --config ./gateway.toml
```

The process prints the concrete loopback address, then serves until it receives
SIGINT or SIGTERM. To use a stable local port during manual validation, pass a
loopback override:

```sh
go run ./cmd/codex-control-gateway --config ./gateway.toml --listen 127.0.0.1:39000
```

Every RPC must include `authorization: Bearer <token>`, where the token is read
from `[client_auth_token_source]` during startup. Use fixture tokens for local
tests; do not point validation at live credentials unless that smoke is
explicitly in scope.
