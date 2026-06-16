# Plan: Stage 06 Existing Chat Behavior

Type: implementation plan slice
Status: implemented locally with product, engineering, and security/data re-review pass; not full QA, Done, current-docs sync, or release approval
Owner: system_analyst
Last repaired: 2026-06-16
Related docs: `index.md`, `stage-03.md`, `stage-10.md`, `../SRS/contracts.md`, `../SRS/states-and-outcomes.md`, `../SRS/events-history.md`, `../SRS/sequences.md`, `../SRS/transient-correlations.md`, `../tech-design/tech-design.md`, `../tests/test-cases.md`, `../tests/regression.md`
Trace IDs: PLAN-STAGE-006, SRS-FR-003, SRS-FR-005, SRS-FR-006, SRS-FR-007, SRS-FR-008, SRS-FR-010, SRS-FR-011, SRS-FR-012, SRS-FR-014, SRS-FR-016, SRS-FR-018, SRS-FR-019, SRS-NFR-001, SRS-NFR-003, SRS-NFR-004, SRS-NFR-006, SRS-NFR-009

## Goal
Make `chat_id` usable for lookup, continuation, status, history, and events without fabricating Codex state or exposing raw internal app-server payloads.

## Scope
- `GetChat` lookup by Codex Thread id.
- `RunChatTurn` / SDK `chat.Run`.
- One active run/turn per `chat_id` in v1.
- Typed status across Codex thread, turn, pending, history-depth, replay-capability, and gateway-local availability.
- Codex-owned turn summary history where supported.
- Normalized live event streams and in-memory replay/narrowed outcomes.
- Stream cancellation behavior separate from interrupt.
- Restart behavior when Codex state is available, missing, unavailable, or unknown.

## Implementation Steps
1. For every existing-chat RPC, run Stage 03 preflight before Codex access or state disclosure.
2. Validate `chat_id` shape and treat it directly as Codex Thread id; never accept task IDs, run IDs, cursors, pending IDs, or idempotency keys as chat identity.
3. Implement `GetChat` as read/status lookup only; it must not create a chat, start a run, import arbitrary history, or attach unrelated state.
4. Implement `RunChatTurn` with non-empty prompt validation, current-process idempotency, and one-active-run check before calling Codex.
5. For `RunChatTurn`, call Codex continuation/turn submission against the same thread only when continuation is supported/proven.
6. Return typed conflict/precondition without Codex call when a run is already active or pending for the same `chat_id`.
7. Normalize `GetChatStatus` from Codex thread lifecycle, current/last turn lifecycle, pending state, history-depth capability, replay capability, and gateway-local state.
8. Implement `GetChatHistory` using Codex-supported turn summary APIs where available; represent unsupported item-level or unmaterialized/narrower history as typed outcomes.
9. Implement `StreamChatEvents` as normalized current live events plus current-process replay where buffer/cursor/epoch/scope are proven.
10. Scope cursors to `chat_id`, `run_id`, and gateway process epoch; reject cross-chat/out-of-range/evicted/unprovable cursors with typed outcomes.
11. Treat client stream cancellation as cancelling only that request/stream; do not interrupt Codex.
12. After restart, use `chat_id` only as Codex Thread id and return typed unavailable/unknown/narrowed outcomes for lost replay/pending/idempotency/current-run state.

## Acceptance / Checks
- `GetChat` never creates a chat, starts a run, imports Desktop-visible history, or exposes task-only IDs as primary identity.
- Concurrent `RunChatTurn` on the same `chat_id` returns typed conflict/precondition and does not call Codex.
- History is Codex-owned turn summary projection only and never invented message history.
- Event streams are normalized, ordered as far as observation supports, and scoped to chat/run/epoch.
- Replay after restart/eviction/cursor mismatch is typed unavailable/out-of-range/unknown/narrowed.
- Stream cancellation does not send interrupt.

## Traceability
- SRS: `SRS-FR-003`, `SRS-FR-005`, `SRS-FR-006`, `SRS-FR-007`, `SRS-FR-008`, `SRS-FR-010`, `SRS-FR-011`, `SRS-FR-012`, `SRS-FR-014`, `SRS-FR-016`, `SRS-FR-018`, `SRS-FR-019`.
- QA: `TC-004`..`TC-018`, `TC-027`, `TC-028`..`TC-032`.
- Regression: `REG-002`, `REG-004`, `REG-005`, `REG-006`, `REG-007`, `REG-009`, `REG-010`, `REG-011`.

## Stop And Ask If
- Continuation would need to silently start an unrelated Codex thread.
- Product needs concurrent runs per chat in v1.
- History or stream requirements require gateway-owned retained content or cross-process event replay.
