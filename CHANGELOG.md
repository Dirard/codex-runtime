# Changelog

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
