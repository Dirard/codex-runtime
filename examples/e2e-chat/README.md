# Codex SDK e2e chat example

This example is a small application that talks to a real local
`codex-runtime-gateway` through the public Go SDK. It does not know about the
app-server JSONL protocol.

The important application code is intentionally short:

```go
client, _ := codex.New(conn,
    codex.WithBearerToken(token),
    codex.WithSessionGroupID(sessionGroupID),
    codex.WithWorkspaceID(workspaceID),
)
codex.SetDefaultClient(client)

chat, events, _ := codex.Run(ctx, prompt)
chat, _ = codex.GetChat(ctx, chat.ID)
history, _ := chat.GetHistory(ctx)
result, _ := chat.Run(ctx, "continue in the same chat")
events, _ = chat.GetEventsStream(ctx, codex.AfterEventID(result.LastEventID))
```

Create a local gateway config:

```powershell
$token = "local-dev-token"
[System.IO.File]::WriteAllText(".\gateway.local.token", $token, [System.Text.UTF8Encoding]::new($false))

$codexBinary = "D:/OpenAI.Codex/app/resources/codex.exe"
$codexHome = "$env:USERPROFILE/.codex".Replace("\", "/")
$workspace = (Get-Location).Path.Replace("\", "/")

$config = @"
codex_binary = "$codexBinary"
listen = "127.0.0.1:5575"
child_env_allowlist = ["GATEWAY_SAFE_ENV"]

[client_auth_token_source]
file = "gateway.local.token"

[[session_groups]]
session_group_id = "local-main"
workspace_id = "workspace-main"
cwd = "$workspace"
codex_home = "$codexHome"

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
"@
[System.IO.File]::WriteAllText(".\gateway.local.toml", $config, [System.Text.UTF8Encoding]::new($false))
```

Run that gateway once and keep it running:

```powershell
go run .\gateway\cmd\codex-runtime-gateway --config .\gateway.local.toml
```

Then run the example in another terminal. It connects to the existing gateway;
it does not start a gateway or Codex by itself:

```powershell
$env:CODEX_RUNTIME_GATEWAY_ADDR = "127.0.0.1:5575"
$env:CODEX_RUNTIME_TOKEN = "local-dev-token"
$env:CODEX_RUNTIME_SESSION_GROUP = "local-main"
$env:CODEX_RUNTIME_WORKSPACE = "workspace-main"

go run ./examples/e2e-chat `
  -prompt "Reply with one short sentence." `
  -continue-prompt "Continue in the same chat with one more short sentence."
```

The output prints the `chat_id`, assistant text from the stream, status, and
history turn count. Stream reading stops when the assistant message is completed
or when a terminal event arrives, then the app asks the gateway for the latest
Codex-backed status/history. The `chat_id` is the Codex thread id returned by
Codex.
