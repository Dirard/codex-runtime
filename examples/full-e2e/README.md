# Full E2E

This example runs a real local `codex-runtime-gateway` against a real Codex
binary and exercises the SDK/gateway surface from an app backend point of view.
It keeps gateway credentials local and never prints token contents or
`auth.json`.

Run from the repo root:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\examples\full-e2e\run-real-codex.ps1 -WhatIf
powershell -NoProfile -ExecutionPolicy Bypass -File .\examples\full-e2e\run-real-codex.ps1
```

If Codex is not on `PATH`, pass it explicitly:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\examples\full-e2e\run-real-codex.ps1 `
  -CodexBinary "D:\ai-apps\codex\codex-rs\target\debug\codex.exe" `
  -CodexHome "$env:USERPROFILE\.codex"
```

The script creates `.local\full-e2e\gateway.token`,
`.local\full-e2e\gateway.toml`, a workspace folder, workflow storage, starts
the gateway on loopback, runs `go run .\examples\full-e2e`, then stops the
gateway. It checks that `auth.json` exists under `CodexHome`, but does not read
it. That is intentional: Codex authentication stays in the real `codex_home`;
workflow packages are materialized separately under gateway workflow storage.

Coverage:

- SDK client setup: `New`, `NewWithClient`, `NewWithClients`, default client,
  redacted client formatting, call options.
- SDK package API: `WorkflowDir`, `WorkflowZip`, deterministic fingerprint and
  invalid package typed error.
- Chat SDK/gateway: start, stream, get chat, status, history, replay from start,
  replay after event id/cursor, continuation, pending response methods with
  expected safe errors.
- Workflow SDK/gateway: init, idempotent init, get, status, run, get chat,
  history, replay, continuation, restart, post-restart run.
- Workflow visibility: paired workflows prove `AGENTS.md`, agents and skills do
  not bleed across workflow packages.
- Gateway-only workflow RPC: `DeleteWorkflow` through the generated gRPC client,
  because the public SDK does not currently expose `Workflow.Delete`.
- Gateway MCP policy: `writer-notes` init is expected to fail with
  `MCPUnavailable` while the gateway default-denies MCP.
- Legacy gateway RPCs: `CodexControl.StartTask`, `StreamTask`, `GetTaskStatus`,
  `InterruptTask`, and expected-error `RespondPendingRequest`.

The interrupt check starts a deliberately long answer and interrupts it. If the
model completes before the interrupt reaches the gateway, the runner reports
that as an already-completed interrupt path instead of treating it as an auth or
gateway failure.
