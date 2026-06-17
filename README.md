# Codex Runtime

Go SDK and private local gateway for running Codex workflows from an app
backend. The blessed path is:

1. own a small workflow folder (`config.toml`, `agents/`, optional
   `AGENTS.md`, `skills/`, references and MCP config);
2. start the private gateway on loopback with one token-source file;
3. use the Go SDK from the backend to `InitWorkflow`, stream the answer, store
   `chat.ID`, and continue the same chat later.

The browser never calls the gateway directly and never receives gateway
credentials. A web app exposes its own HTTP/SSE/WebSocket surface and keeps the
gateway behind the backend.

## Start Here

If you want to use Codex Runtime from an application, start with the simple
consumer guide:

- `docs/consumer-guide.ru.md`: gateway setup, `auth.json`, workflow isolation,
  Go SDK calls, chat continuation and runnable examples.

## Workflow Quickstart

Prerequisites: Go, a local Codex binary, one gateway config file, and one local
token-source file. Put the token value in `.local\gateway.token` without printing
it in shell history or docs, then create `.local\gateway.workflow.toml` with
your local paths:

```powershell
New-Item -ItemType Directory -Force .\.local | Out-Null

$codexBinary = "<absolute-path-to-codex.exe>"
$codexHome = "<absolute-path-to-codex-home>"
$workspace = (Get-Location).Path.Replace("\", "/")
$workflowState = (Join-Path (Get-Location) ".workflow-state").Replace("\", "/")
$tokenSource = (Join-Path (Get-Location) ".local\gateway.token").Replace("\", "/")

@"
codex_binary = "$codexBinary"
listen = "127.0.0.1:5575"
workflow_storage_dir = "$workflowState"

[client_auth_token_source]
file = "$tokenSource"

[[session_groups]]
session_group_id = "workflow-smoke-session"
workspace_id = "workflow-smoke-workspace"
cwd = "$workspace"
codex_home = "$codexHome"

[session_groups.runtime_policy]
approval_policy = "on-request"
approvals_reviewer = "user"
sandbox_mode = "workspace-write"
"@ | Set-Content -Encoding UTF8 .\.local\gateway.workflow.toml
```

After that setup, the quickstart is 3 documented commands:

```powershell
go run .\examples\workflow-scaffold -source .\examples\workflows\writer-notes -target .\.local\workflows\writer-notes
go run .\gateway\cmd\codex-runtime-gateway --config .\.local\gateway.workflow.toml
powershell -NoProfile -ExecutionPolicy Bypass -File .\examples\workflow-smoke\run-local.ps1 -TokenSource .\.local\gateway.token -WorkflowDir .\.local\workflows\writer-notes
```

Run the gateway command in one terminal and the smoke command in another. The
smoke prints gateway readiness, workflow init, streamed assistant text,
`chat_id`, history and same-chat continuation. The `writer-notes` workflow also
prints either deterministic shipped sources such as
`writer-notes://harbor-fire` or explicit `no sources`.

## Starter Workflows

- `examples/workflows/plain-chat`: smallest assistant workflow.
- `examples/workflows/writer-notes`: local deterministic notes workflow with a
  tiny stdio MCP fixture and shipped non-secret references.
- `examples/workflows/visibility-alpha` and
  `examples/workflows/visibility-beta`: full-e2e fixtures that prove
  workflow-scoped `AGENTS.md`, agent and skill visibility is isolated per
  workflow.
- `examples/workflow-scaffold`: copies a starter workflow to an app-owned
  folder without reading or mutating global `CODEX_HOME`.
- `examples/workflow-probe`: token-safe gateway readiness probe.
- `examples/workflow-smoke`: token-safe lifecycle smoke for
  `InitWorkflow -> first response -> continuation`.
- `examples/full-e2e`: reproducible real-Codex SDK/gateway e2e runner that
  starts a local gateway and covers chat, workflow, legacy gateway and cleanup
  paths without printing secrets.
- `examples/api-handler`: backend-only HTTP example that initializes a workflow,
  streams the assistant answer, returns `chat_id`, accepts `chat_id` for
  continuation, and maps typed SDK errors to safe HTTP responses.

## SDK Shape

The Go SDK import path is `github.com/Dirard/codex-runtime/sdk/go`.

```go
workflow, err := client.InitWorkflow(ctx, codex.WorkflowDir{
    Namespace: "examples",
    ID:        "writer-notes",
    Path:      ".\\examples\\workflows\\writer-notes",
}, codex.WithWorkflowMCPReload(true))
if err != nil {
    return err
}

chat, events, err := workflow.Run(ctx, "Summarize the harbor note.")
_ = chat.ID // store this in the app database for continuation.
_ = events // stream normalized SDK events to the app's own client.
```

Use `workflow.GetChat(ctx, chatID)` and then `chat.Run(ctx, prompt)` to continue
the same chat. Application code should branch on `codex.AsError(err)` and the
stable `WorkflowCode` instead of parsing strings.

## Gateway Boundary

The gateway loads a strict trusted TOML config, starts Codex app-server behind a
private loopback gRPC service, and exposes SDK-facing chat/workflow RPCs. It does
not expose raw app-server traffic, gateway credentials, MCP admin mutation APIs,
or a public browser endpoint.

Configuration is loaded from a deliberately strict TOML subset. Duplicate keys,
dotted keys, inline tables, literal strings, multiline strings, dates, floats,
and other unsupported TOML features are rejected.

Troubleshooting anchors:

- Missing token source: create the configured file and restart the gateway.
- Invalid token: confirm the backend points at the same token-source file as the
  gateway config.
- Invalid workflow package: check for missing `config.toml`, unsafe paths,
  duplicate/case-conflicting paths, unsupported files or package size over
  10 MiB.
- Empty prompt: send a non-empty prompt before creating a chat.
- Unsafe gateway address: keep insecure local credentials on loopback only.
- Restart replay loss: old event cursors are process-local and may return
  `ReplayUnavailable` after restart.
- `restart_required`: call `workflow.Restart(ctx)` before starting new work.
- MCP unreachable: verify the gateway host can run the command or reach the MCP
  endpoint and has the allowed env references.
- `chat_runtime.enabled=false`: chat/workflow SDK calls are unavailable until
  chat runtime is enabled.

## Docs

- `docs/consumer-guide.ru.md`: simple consumer guide for app/backend teams.
- `examples/api-handler`: backend HTTP integration example.
- `examples/workflow-smoke`: quick local workflow lifecycle check.
- `examples/full-e2e`: real-Codex SDK/gateway e2e.

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

The release does not bundle Codex itself. Users still configure `codex_binary`,
`codex_home`, workspace, workflow storage, and token-source authentication in
the gateway TOML file.

Local release checks:

```sh
goreleaser check
goreleaser build --snapshot --clean
```

If GoReleaser is not installed locally, use the CI-compatible v2 runner:

```sh
go run github.com/goreleaser/goreleaser/v2@latest check
go run github.com/goreleaser/goreleaser/v2@latest build --snapshot --clean
```

Publishing a release is done by pushing the tag:

```sh
git tag v0.0.2
git push origin v0.0.2
```
