# Plan: Stage 10 Volatile Gateway Correlations

Type: implementation plan slice
Status: implemented locally with product, engineering, and security/data re-review pass; not full QA, Done, current-docs sync, or release approval
Owner: system_analyst
Purpose: define process-local run, stream, pending, idempotency, diagnostics, and restart semantics after the removed gateway store direction.
Last repaired: 2026-06-16
Related docs: `index.md`, `../SRS/transient-correlations.md`, `../SRS/contracts.md`, `../SRS/states-and-outcomes.md`, `../SRS/events-history.md`, `../SRS/sequences.md`, `../SRS/rollout.md`, `../SRS/nfr-mapping.md`, `../tech-design/tech-design.md`, `../reviews/security-privacy-data-review.md`, `../reviews/release-ops-review.md`, `../tests/test-cases.md`, `../tests/regression.md`
Trace IDs: PLAN-STAGE-010, SRS-FR-006, SRS-FR-008, SRS-FR-009, SRS-FR-011, SRS-FR-018, SRS-FR-019, SRS-NFR-003, SRS-NFR-007, SRS-NFR-009

## Source Of Truth
SRS owns transient correlation behavior and restart honesty. This stage exists so implementers do not recreate the removed durable gateway store as an implementation convenience.

Build position: implement this after Stage 03 and before side-effecting chat behavior in Stage 05/06/07.

## Goal
Keep gateway-local runtime state useful for the current process without turning it into durable product state.

## Scope
- Active run/turn correlation for the current process.
- In-memory replay buffers and stream cursors.
- Process-local pending request correlation.
- Current-process idempotency and in-flight side-effect control.
- Safe diagnostics correlation.
- Restart loss semantics and typed post-restart outcomes.

## Out Of Scope
- Gateway-created durable chat identity or identity translation storage.
- Conversation history/content retention in gateway.
- Raw internal app-server payload retention, raw request/response retention, prompt/response hashes, content digests, raw auth material, or private-data retention.
- Legacy Desktop history backfill or arbitrary history import.
- Durable event replay payloads.

## Implementation Steps
1. Define an in-memory correlation store scoped to the gateway process epoch.
2. Represent active run/turn state with `chat_id`, `run_id` where known, session/workspace scope, lifecycle, and safe request IDs only.
3. Represent replay buffers in memory, scoped to `chat_id`, `run_id`, and process epoch, with SRS-defined event/time/size bounds.
4. Represent pending request correlation in memory with `chat_id`, `run_id`, `pending_request_id`, status, expiry, and safe reason/category metadata.
5. Represent idempotency reservations/results as current-process safe scope/result references only; never store prompt, response, event, history, raw payload, content hash, or content digest.
6. Represent diagnostics correlation with safe IDs, state classes, and redacted reason codes.
7. Clear active run, replay, pending, idempotency, and diagnostics state on gateway restart.
8. After restart, make `GetChat` use `chat_id` directly as Codex Thread id and ask Codex for provable thread/status/history state.
9. After restart, treat pre-restart idempotency keys as unrecognized for replay/recovery; never return prior result, in-progress, terminal, or no-duplicate state solely from the key.
10. Return typed replay-unavailable, pending-unavailable, idempotency-unavailable, unknown, unavailable, out-of-range, or narrowed outcomes when pre-restart state cannot be proven; app-stored `chat_id` is usable only as Codex Thread id where Codex proves state.
11. Ensure ambiguous prior side effects do not cause duplicate Codex calls only while the same gateway process still has the current-process idempotency reservation/evidence.

## Acceptance / Checks
- Gateway restart clears replay, pending, idempotency, active-run, and diagnostics memory.
- App-stored `chat_id` can be reused only as Codex Thread id.
- History/status after restart come from Codex or typed unavailable/unknown/narrowed outcomes.
- Replay after restart is typed unavailable or narrowed to live/status/history.
- Idempotency keys from before restart are unrecognized as prior requests and do not provide replay, recovery, exactly-once delivery, or no-duplicate guarantees.
- No implementation creates a gateway-owned durable identity/content store.
- No process-local record retains private content, raw internal payloads, auth material, or content hashes/digests.

## Traceability
- SRS: `SRS-FR-006`, `SRS-FR-008`, `SRS-FR-009`, `SRS-FR-011`, `SRS-FR-018`, `SRS-FR-019`, `SRS-NFR-003`, `SRS-NFR-007`, `SRS-NFR-009`.
- QA: `TC-015`, `TC-016`, `TC-017`, `TC-020`, `TC-024`, `TC-025`, `TC-026`, `TC-027`, `TC-032`, `TC-035`.
- Regression: `REG-002`, `REG-007`, `REG-008`, `REG-009`, `REG-011`, `REG-012`.

## Stop And Ask If
- Product or implementation asks for gateway-owned durable chat identity.
- Restart requirements require recovering pending/replay/idempotency state without Codex evidence.
- Restart requirements require no-duplicate suppression or exactly-once delivery from a pre-restart gateway idempotency key alone.
- Any transient state needs to retain prompt/message/event/history content, raw internal payloads, or auth material.
