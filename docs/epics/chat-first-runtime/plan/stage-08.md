# Plan: Stage 08 Go SDK And Adoption Surface

Type: implementation plan slice
Status: implemented with local checks and targeted owner re-review pass recorded; not Done or release approval
Owner: system_analyst
Last repaired: 2026-06-16
Related docs: `index.md`, `../SRS/feature.md`, `../SRS/contracts.md`, `../SRS/events-history.md`, `../SRS/states-and-outcomes.md`, `../SRS/rollout.md`, `../tech-design/tech-design.md`, `../reviews/product-review.md`, `../tests/test-cases.md`, `../tests/regression.md`
Trace IDs: PLAN-STAGE-008, SRS-FR-001, SRS-FR-003, SRS-FR-004, SRS-FR-005, SRS-FR-006, SRS-FR-007, SRS-FR-008, SRS-FR-009, SRS-FR-010, SRS-FR-011, SRS-FR-013, SRS-FR-014, SRS-FR-016, SRS-FR-018, SRS-FR-019, SRS-NFR-001, SRS-NFR-002, SRS-NFR-006, SRS-NFR-009

## Goal
Expose a stable chat-first Go SDK surface that lets an API handler call the local gateway without depending on task-first names, parsing internal payloads, or promising Desktop visible/current-thread behavior.

## Scope
- SDK `Run`, `GetChat`, `chat.Run`, status, history, event stream, pending response, and interrupt helpers or equivalents.
- SDK mapping of gRPC typed outcomes to idiomatic Go errors/status.
- Context cancellation behavior that closes requests/streams without interrupt.
- Sanitized examples for `web app -> API handler with Go SDK -> local gateway -> codex.exe app-server`.
- Documentation/adoption examples that preserve local-only and Desktop non-promise.

## Implemented Evidence
- Root SDK package: `client.go`, `chat.go`, `history.go`, `stream.go`, `pending.go`, `types.go`.
- SDK regression tests: `client_test.go`.
- Sanitized API handler example: `examples/api-handler/main.go`, `examples/api-handler/main_test.go`.
- Review/evidence registers updated in `../reviews/*` and `../tests/test-execution.md`.

## Executed Checks
- `GOPROXY=off go test -count=1 .`
- `GOPROXY=off go test -count=1 .\examples\api-handler`
- `GOPROXY=off go test -count=1 ./...`
- `GOPROXY=off go list ./...`
- `GOPROXY=off go vet ./...`
- `git diff --check`

## Implementation Steps
1. Build the SDK client around `ChatRuntimeService`, not task compatibility RPCs.
2. Expose `codex.Run(ctx, prompt)` as a convenience that requires a non-empty first prompt and returns only after first-turn acceptance/correlation.
3. Expose `codex.GetChat(ctx, chatID)` as a handle resolver for Codex Thread id; do not create chats from lookup.
4. Expose `chat.Run(ctx, prompt)` for existing-chat turns with active-run conflict and continuation-unavailable typed behavior.
5. Expose `chat.GetStatus`, `chat.GetHistory`, and `chat.GetEventsStream` or equivalent helpers with typed unsupported/unavailable/unknown/narrowed outcomes.
6. Expose pending response and interrupt helpers with idempotency support and typed stale/duplicate/terminal/no-active outcomes.
7. Map gRPC status/details into stable Go errors/status structs without requiring callers to inspect internal app-server payloads.
8. Make context/request cancellation close the SDK/gRPC request or stream only; document that interrupt requires explicit SDK call.
9. Keep SDK examples sanitized: no auth values, no private prompts/history/events, no internal protocol dumps, no Desktop visible/current-thread promise, and no release/current-state claim.
10. Show API-handler usage where the application stores `chat_id` as Codex Thread id and owns its own app/business data.

## Acceptance / Checks
- SDK primary identity is `chat_id`; no task/run/cursor/pending/idempotency ID is stored as chat identity.
- SDK `Run` does not create public thread-only chat success.
- SDK errors/status are typed and UI/API-handler consumable without parsing internal payloads.
- SDK cancellation and interrupt are distinct.
- Examples preserve local-only v1, Codex-owned history/behavior, process-local gateway correlations, and no Desktop visible/current-thread promise.

## Traceability
- SRS: `SRS-FR-001`, `SRS-FR-003`..`SRS-FR-011`, `SRS-FR-013`, `SRS-FR-014`, `SRS-FR-016`, `SRS-FR-018`, `SRS-FR-019`, `SRS-NFR-001`, `SRS-NFR-002`, `SRS-NFR-006`, `SRS-NFR-009`.
- QA: `TC-001`..`TC-036`, especially `TC-001`, `TC-004`, `TC-006`, `TC-018`, `TC-020`, `TC-022`, `TC-032`, `TC-036`.
- Regression: `REG-002`..`REG-012`.

## Stop And Ask If
- API handler requires shared gateway exposure outside local v1 scope.
- SDK examples need to promise Desktop visible/current-thread behavior.
- SDK design would store conversation content or long-lived chat identity state inside the gateway.
