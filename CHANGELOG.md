# Changelog

## v0.0.2 - 2026-06-17

Workflow runtime release for application-owned Codex workflows.

### Added

- Workflow Go SDK under `github.com/Dirard/codex-runtime/sdk/go`:
  - `client.InitWorkflow(ctx, codex.WorkflowDir{...})` and
    `codex.WorkflowZip{...}`.
  - deterministic workflow package fingerprints for folder and ZIP sources.
  - `workflow.Run`, `workflow.GetChat`, `workflow.GetStatus` and
    `workflow.Restart`.
  - typed workflow errors through `codex.AsError(err)` and stable
    `WorkflowCode` values.
- Gateway `WorkflowRuntimeService` for workflow init/get/status/run/restart and
  gateway-side delete RPC support.
- Per-workflow runtime materialization under `workflow_storage_dir`:
  - workflow `config.toml`, `agents/`, `skills/`, references and tools are
    mirrored into a project-local `cwd/.codex`;
  - Codex authentication still comes from the configured `codex_home`;
  - workflow `AGENTS.md` and `AGENTS.override.md` are placed at runtime
    workspace root so Codex loads them as normal project instructions.
- Workflow-scoped isolation for `AGENTS.md`, agents and skills, verified by
  paired `visibility-alpha` and `visibility-beta` real-Codex fixtures.
- Starter and verification examples:
  - `examples/workflows/plain-chat`
  - `examples/workflows/writer-notes`
  - `examples/workflows/visibility-alpha`
  - `examples/workflows/visibility-beta`
  - `examples/workflow-scaffold`
  - `examples/workflow-smoke`
  - `examples/workflow-probe`
  - `examples/full-e2e`
- Consumer documentation in `docs/consumer-guide.ru.md`.

### Changed

- The Go SDK source now lives under `sdk/go/`, leaving room for future SDKs
  such as `sdk/ts/`.
- Workflow packages are validated and revalidated for path traversal,
  duplicate/case-conflicting paths, reserved path segments, size limits and
  secret-like literals.
- Gateway workflow runtime uses generated per-workflow session/workspace ids
  instead of reusing the caller's base session group.
- Full e2e now creates a clean temporary `CODEX_HOME` containing only copied
  `auth.json` and minimal model/provider config by default, reducing accidental
  influence from the user's global agents/skills/config.

### Verified

- `go test -count=1 ./...`
- `go vet ./...`
- `goreleaser check`
- `goreleaser build --snapshot --clean`
- `git diff --check`
- Real Codex full e2e:
  `powershell -NoProfile -ExecutionPolicy Bypass -File .\examples\full-e2e\run-real-codex.ps1`
  - SDK client setup, default client and typed local errors.
  - `WorkflowDir` and `WorkflowZip` fingerprint equivalence.
  - plain chat start/stream/history/continuation.
  - workflow init/status/run/history/continuation/restart/delete.
  - per-workflow `AGENTS.md`, agent and skill isolation.
  - expected MCP policy denial.
  - legacy `CodexControl` gateway compatibility path.

### Known Limits

- Release artifacts contain `codex-runtime-gateway` only; Codex itself is not
  bundled.
- Gateway remains private/local and must be protected by the configured bearer
  token source.
- Applications own durable chat listing/indexing. Store `chat_id` and workflow
  identity in the application database if a UI needs a list of chats.
- Workflow storage is filesystem-backed; stream replay, pending request
  correlation and idempotency are still process-local unless a future durable
  store is added.
- Gateway default-denies workflow MCP reload unless explicitly supported by the
  runtime configuration.

## v0.0.1 - 2026-06-16

Initial local Codex Runtime release.

### Added

- Chat-first Go SDK:
  - `codex.Run(ctx, prompt)` for starting a new Codex-backed chat.
  - `codex.GetChat(ctx, chatID)` for reconnecting to an existing Codex thread id.
  - `chat.Run(ctx, prompt)` for sending a new turn to the same chat.
  - `chat.GetHistory(ctx)`, `chat.GetStatus(ctx)`, `chat.GetEventsStream(ctx)`.
  - pending request response and interrupt helpers.
- Local `codex-runtime-gateway` command under `gateway/cmd/codex-runtime-gateway`.
- Chat runtime gRPC service over the local gateway.
- Gateway bridge from Codex app-server notifications to chat stream events:
  assistant deltas, assistant message completion, and terminal turn completion.
- Persistent external identity model where `chat_id` is the Codex `Thread.id`.
- Examples:
  - `examples/e2e-chat` for direct SDK usage.
  - `examples/api-handler` for an API-handler style integration.
- Reproducible proto generation script using the Dockerized compiler image.
- GoReleaser-based GitHub release automation for `codex-runtime-gateway`
  archives, checksums, and tag-triggered publishing.
- Product, BRS/SRS, plan, test, and review documentation for the chat-first runtime.

### Verified

- `GOPROXY=off go test -count=1 ./...`
- `GOPROXY=off go vet ./...`
- `goreleaser check`
- `goreleaser build --snapshot --clean`
- `git diff --check`
- Live e2e against `codex-cli 0.137.0` with a real local gateway:
  - first chat turn streamed assistant text and completed;
  - second turn reused the same chat and completed;
  - history returned two Codex turns.

### Known Limits

- Gateway is local-only and authenticated with a local bearer token.
- Release artifacts contain the gateway binary only; Codex itself must be
  installed and configured separately through `codex_binary`.
- Gateway starts and supervises its own `codex.exe app-server` child process per
  configured session group. It does not attach to an already-running Desktop
  Codex process or to an arbitrary external app-server process.
- SDK-created chats are Codex threads, but they are not promised to appear as the
  currently visible Desktop UI thread.
- Gateway keeps only process-local transient stream, pending, and idempotency
  correlation state. It does not use SQLite in this release.
- History and execution behavior belong to Codex; the gateway exposes supported
  Codex-backed views and does not invent item-level history semantics.
