# Plan: Stage 07 Pending, Interrupt, And Runtime Recovery

Type: implementation plan slice
Status: targeted owner re-review pass recorded after repairs; not Done or release approval
Owner: system_analyst
Last repaired: 2026-06-16
Related docs: `index.md`, `stage-03.md`, `stage-10.md`, `../SRS/contracts.md`, `../SRS/states-and-outcomes.md`, `../SRS/events-history.md`, `../SRS/sequences.md`, `../SRS/rollout.md`, `../SRS/transient-correlations.md`, `../SRS/nfr-mapping.md`, `../tech-design/tech-design.md`, `../reviews/security-privacy-data-review.md`, `../reviews/release-ops-review.md`, `../tests/test-cases.md`, `../tests/regression.md`
Trace IDs: PLAN-STAGE-007, SRS-FR-006, SRS-FR-009, SRS-FR-010, SRS-FR-011, SRS-FR-012, SRS-FR-013, SRS-FR-019, SRS-NFR-003, SRS-NFR-004, SRS-NFR-005, SRS-NFR-007, SRS-NFR-009

## Goal
Handle Codex pending states, explicit interrupts, supervisor-foundation usage, shutdown, and recovery without stale approvals, duplicate side effects, or fabricated completion.

## Scope
- Pending approval/user-input state correlation.
- `RespondChatPending` validation and idempotency.
- `InterruptChatRun` validation, idempotency, and lifecycle outcomes.
- Use the Stage 03 lazy `codex.exe app-server` supervisor foundation for pending response and interrupt Codex calls.
- Pending/interrupt-specific dependency failure, shutdown, and restart behavior.
- Restart/shutdown recovery for active, pending, interrupting, and unknown work.

## Implemented Slice Evidence
- Added a single app-server forwarded-request claim hook before the legacy task compatibility request channel, so chat pending handling does not race with a second app-server request reader.
- Added chat-runtime pending correlation, `RespondChatPending`, and `InterruptChatRun` behavior over Codex-backed thread/run state and the Stage 03 supervisor connection.
- Added typed domain payloads for active pending requests, pending-created/resolved stream events, terminal event payloads, and interrupting run state.
- Added gRPC validation and response mapping for chat pending response and chat interrupt requests.
- Added targeted runtime and gRPC tests for active-run pending claim, pending response write/idempotent retry, unavailable-after-run-loss behavior, and Codex interrupt forwarding without fabricated terminal completion.
- Repaired owner-review findings: pending expiry is claimed in `chatstate.Store` before Codex write, `pending_request_id` is included in respond-pending idempotency scope, pending response content fingerprints are not retained in chat runtime state, accepted/expired pending records are cleaned from process-local raw request memory, and pending completion preserves an already-interrupting run state.
- Repaired targeted re-review findings: pending completion restores `running` only through a store-level conditional transition that preserves a concurrent `interrupting` state under the store mutex, and expired pending is claimed/pruned before response-type validation so stale wrong-shape responses do not disclose type mismatch or retain raw pending request memory.
- Repaired third-review findings: pending registration and transition to `pending` now happen through one store operation that refuses to claim if `interrupting` already won, and stale process-local raw pending request records are pruned on lazy bounded triggers such as a later forwarded pending request or active-pending status read.

## Implementation Steps
1. Detect Codex waiting-on-approval and waiting-on-user-input states from Codex-backed status/events only.
2. Record pending correlations in process memory with `chat_id`, `run_id`, `pending_request_id`, status, timestamps, expiry, and safe reason/category metadata.
3. Deliver pending display data only as transient authorized status/stream data; do not retain it in logs, diagnostics, examples, or QA evidence.
4. For `RespondChatPending`, run Stage 03 preflight and validate `chat_id`, active run, pending ID, response shape, idempotency key, session/workspace, and expiry.
5. Forward a pending response to Codex only when the active pending correlation is proven for the same chat/run/pending request.
6. Reject stale, duplicate, mismatched, expired, terminal, unavailable-after-restart, already-resolved, or unknown pending responses with typed outcomes.
7. For `InterruptChatRun`, require explicit caller action; validate active run/turn and idempotency before sending Codex interrupt.
8. Report interrupting, interrupted, failed, already-terminal, already-interrupting, no-active, unavailable, and unknown outcomes according to observed/provable state.
9. Treat stream cancellation and SDK context cancellation as request/stream cancellation only.
10. Before forwarding pending response or interrupt to Codex, use the Stage 03 supervisor foundation; if it is unavailable, starting, timed out, or in cooldown, return typed dependency failure without fabricating pending or interrupt success.
11. On shutdown/restart, clear process-local active/pending/interrupt/idempotency/replay/diagnostics state and return typed unavailable/unknown until Codex state is proven again.
12. After restart, treat pre-restart idempotency keys as unrecognized for pending/interrupt replay; accept a pending response or interrupt only when Codex and current-process correlation prove the target active state.

## Acceptance / Checks
- Pending response cannot approve the wrong run, stale request, duplicate request, expired request, or terminal state.
- Interrupt targets only the active Codex run/turn and never another chat.
- Stream cancellation does not send interrupt.
- Supervisor failure/backoff from the Stage 03 foundation is typed and redacted.
- Shutdown and restart never fabricate completion, pending state, interrupt success, or recovered idempotency/no-duplicate state.
- Disable/coexistence behavior remains compatible with existing task RPCs when otherwise healthy.

## Executed Checks
- `GOPROXY=off go test -count=1 .\gateway\internal\appserver .\gateway\internal\chatstate .\gateway\internal\chatruntime .\gateway\internal\grpcapi`
- `GOPROXY=off go test -count=1 ./...`
- `git diff --check`
- Repair-specific checks added: expired pending before Codex write, same idempotency key against a different `pending_request_id`, process-local pending record cleanup, and pending response after interrupt preserving `interrupting`.
- Second repair-specific checks added: expired pending with wrong response shape returns stale/unavailable before type disclosure and cleans process-local raw pending memory; pending response write interleaving with an interrupting state does not restore the run to `running`.
- Third repair-specific checks added: `RegisterPendingForActiveRun` does not overwrite an interrupting active run or register a new pending after interrupt wins; a later forwarded pending request prunes expired raw pending request memory before storing the fresh pending record.

## Current Limitations
- This slice does not complete the Go SDK, integration hardening, full QA execution, current-behavior docs sync, or release/current-state readiness.
- Targeted product, engineering, and security/privacy/data owner re-review pass is recorded for this slice; full QA execution, current-behavior docs sync, SDK acceptance, and release/current-state readiness remain pending.

## Traceability
- SRS: `SRS-FR-006`, `SRS-FR-009`, `SRS-FR-010`, `SRS-FR-011`, `SRS-FR-012`, `SRS-FR-013`, `SRS-FR-019`, `SRS-NFR-003`, `SRS-NFR-004`, `SRS-NFR-005`, `SRS-NFR-007`, `SRS-NFR-009`.
- QA: `TC-019`..`TC-027`, `TC-033`, `TC-035`, `TC-028`..`TC-032`.
- Regression: `REG-008`, `REG-009`, `REG-010`, `REG-011`, `REG-012`.

## Stop And Ask If
- Pending decisions require new product/security approval semantics.
- Interrupt needs to report success before Codex turn/run correlation or terminal state can be proven.
- Runtime requires per-chat processes, runtime config reload promises, new supervisor semantics beyond Stage 03, or recovered process-local state after restart.
