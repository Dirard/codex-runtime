# Plan: Stage 05 StartChatRun Boundary

Type: implementation plan slice
Status: implemented locally; owner reviews passed after idempotency repair; full QA, current-docs sync, and release approval pending
Owner: system_analyst
Last repaired: 2026-06-16
Related docs: `index.md`, `stage-03.md`, `stage-10.md`, `../SRS/feature.md`, `../SRS/contracts.md`, `../SRS/transient-correlations.md`, `../SRS/states-and-outcomes.md`, `../SRS/sequences.md`, `../SRS/rollout.md`, `../tech-design/tech-design.md`, `../tests/test-cases.md`, `../tests/regression.md`
Trace IDs: PLAN-STAGE-005, SRS-FR-004, SRS-FR-008, SRS-FR-011, SRS-FR-013, SRS-FR-018, SRS-FR-019, SRS-NFR-003, SRS-NFR-005, SRS-NFR-009

## Goal
Implement the gateway `StartChatRun` boundary that will back SDK `codex.Run`, so public start success is returned only after Codex thread identity and first-turn acceptance/correlation are proven.

## Scope
- Non-empty prompt validation and size limits.
- Auth, session/workspace authorization, current-process idempotency, and Stage 03 supervisor foundation before Codex side effects.
- Codex `thread/start` followed by first `turn/start`.
- First-turn acceptance/correlation observation.
- Typed failure, retry, unknown delivery, thread-only failure, and no thread-only chat success behavior.

## Implementation Steps
1. Receive gRPC `StartChatRun` start request with non-empty first prompt, session/workspace selectors, deadline, and idempotency key; future SDK `codex.Run` maps to this boundary.
2. Run Stage 03 preflight: authenticate, strip auth metadata, validate request/context/prompt/limits/idempotency, and authorize session/workspace.
3. Reserve current-process idempotency state using operation, session group, workspace, and safe request scope; do not store prompt text, content digest, raw request, or raw response.
4. Use the Stage 03 app-server supervisor foundation for the session group; if startup timeout, dependency failure, or backoff prevents availability, return typed unavailable/startup failure before `thread/start`.
5. Call Codex thread creation/loading capability. Current evidence maps this to `thread/start`.
6. Record only safe process-local correlation between the request and returned Codex Thread id.
7. Call Codex first-turn submission capability with the prompt. Current evidence maps this to `turn/start`.
8. Observe/derive first-turn acceptance/correlation and Codex turn id where provided.
9. Return success with `chat_id = Thread.id`, `run_id = Turn.id` where available, current status, and stream/cursor metadata only after step 8 is proven.
10. If validation/auth/authz/idempotency fails before Codex, return typed error and make no Codex call.
11. If thread creation/loading succeeds but first-turn submission or acceptance is not proven, return typed failure/unknown/unavailable and do not expose a successful chat.
12. On same-key retry in the current process, return prior safe result reference, in-progress, terminal, typed conflict, unknown, or unavailable according to proven state; do not duplicate Codex side effects.
13. After gateway restart, do not treat a pre-restart idempotency key as a recognized prior `StartChatRun`; if the caller lacks a usable Codex Thread id and Codex cannot prove prior state, do not claim idempotent replay, recovery, or cross-restart no-duplicate behavior.

## Acceptance / Checks
- Successful response always has `chat_id == Codex Thread.id`.
- No successful SDK-created chat exists without a non-empty first prompt and accepted/correlated first turn.
- Unknown delivery in the current process prefers typed unknown/unavailable over duplicate side effects.
- Post-restart idempotency key reuse alone is not a no-duplicate guarantee and is not reported as recovered replay.
- Failed/ambiguous starts do not create public thread-only success.
- Redacted diagnostics include safe IDs and reason classes only.

## Local Implementation Evidence
- Added `gateway/internal/chatruntime` service layer for `StartChatRun`.
- Wired `ChatRuntimeService.StartChatRun` through gRPC validation, chat-specific error details, and gateway composition.
- Uses `thread/start` followed by `turn/start`; success is returned only after Codex Thread id and Turn id are both observed.
- Uses process-local `chatstate` idempotency result references without prompt, response, raw JSONL, hash, or digest retention.
- Same-process successful retry returns the prior safe result; same-key retry after uncertain post-side-effect failure does not duplicate Codex calls.
- Same-key retry with a different `client_message_id` conflicts before duplicate Codex calls because `client_message_id` is part of the safe idempotency scope.
- Direct gRPC requires `idempotency_key` and `client_message_id`; future SDK may generate them for `codex.Run`.
- Local checks: `GOPROXY=off go test -count=1 ./gateway/internal/chatruntime ./gateway/internal/chatstate ./gateway/internal/grpcapi ./gateway/cmd/codex-runtime-gateway`; `GOPROXY=off go test ./...`; `git diff --check`.

## Traceability
- SRS: `SRS-FR-004`, `SRS-FR-008`, `SRS-FR-011`, `SRS-FR-013`, `SRS-FR-018`, `SRS-FR-019`, `SRS-NFR-003`, `SRS-NFR-005`, `SRS-NFR-009`.
- QA: `TC-001`, `TC-002`, `TC-003`, `TC-024`, `TC-026`, `TC-028`..`TC-031`, `TC-032`.
- Regression: `REG-002`, `REG-003`, `REG-009`, `REG-010`, `REG-011`.

## Stop And Ask If
- A caller-visible chat success is needed before first-turn acceptance/correlation.
- Thread-only starts need to become public successful chats.
- Safe reconciliation after unknown delivery requires behavior not already approved in SRS/tech design/reviews.
- Cross-restart no-duplicate behavior is required without Codex-proven thread state.
