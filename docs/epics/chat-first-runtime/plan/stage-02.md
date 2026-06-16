# Plan: Stage 02 Chat Runtime Contract

Type: implementation plan slice
Status: implemented locally; product and engineering review passed; not Done or release approval
Owner: system_analyst
Last repaired: 2026-06-15
Related docs: `index.md`, `../SRS/index.md`, `../SRS/feature.md`, `../SRS/contracts.md`, `../SRS/states-and-outcomes.md`, `../SRS/sequences.md`, `../SRS/rollout.md`, `../SRS/traceability.md`, `../tech-design/adr-001-dedicated-chat-first-service.md`, `../tech-design/tech-design.md`, `../tests/test-cases.md`, `../tests/regression.md`
Trace IDs: PLAN-STAGE-002, SRS-FR-001, SRS-FR-003, SRS-FR-004, SRS-FR-005, SRS-FR-006, SRS-FR-007, SRS-FR-008, SRS-FR-009, SRS-FR-010, SRS-FR-011, SRS-FR-012, SRS-FR-013, SRS-FR-014, SRS-FR-016, SRS-FR-017, SRS-FR-018, SRS-FR-019, SRS-NFR-006, SRS-NFR-008, SRS-NFR-009

## Goal
Introduce the dedicated chat-first gRPC/proto and generated Go contract surface without breaking or renaming the existing task compatibility behavior.

## Scope
- Dedicated `ChatRuntimeService` for chat-shaped operations.
- Generated Go service/client/message surface for SDK and gateway implementation.
- Request/response and stream DTOs for chat identity, run identity, status, history, events, pending, interrupt, idempotency, and typed outcomes.
- Coexistence with current `codex.control.v1.CodexControl`.
- Public absence of raw internal app-server protocol payload escape hatches.

## Implementation Steps
1. Add the dedicated chat-first proto/service in the repo's accepted gRPC namespace and generated Go package for the runtime API.
2. Define chat-first operations: `StartChatRun`, `GetChat`, `RunChatTurn`, `GetChatStatus`, `GetChatHistory`, `StreamChatEvents`, `RespondChatPending`, and `InterruptChatRun`.
3. Make `chat_id` the public Codex Thread id and keep `run_id`, task compatibility IDs, event cursors, pending request IDs, and idempotency keys separate.
4. Represent request context needed for local session/workspace routing without exposing local private paths or auth material.
5. Encode typed outcome categories from SRS: invalid, unauthenticated, permission denied, not found, failed precondition/conflict, aborted/idempotency conflict, unsupported, unavailable, unknown, out of range, narrowed, deadline, cancelled, and internal-after-redaction.
6. Encode status layers: thread lookup, Codex thread lifecycle, current/last turn lifecycle, capability depth, and gateway-local availability.
7. Encode stream events as normalized gateway events with safe IDs, order/cursor metadata, pending notices, status updates, terminal events, and redacted warnings.
8. Encode history as Codex-owned turn summary/projection metadata only, with typed unsupported/unavailable/unknown/narrowed outcomes for unsupported depth.
9. Add explicit service split/coexistence tests or compile-time checks proving task RPC DTOs are not the primary chat-first contract.
10. Regenerate Go artifacts and ensure generated code is the only generated output expected from the proto change.

## Acceptance / Checks
- `ChatRuntimeService` is separate from `CodexControl`; task clients are not forced onto chat semantics.
- Public contract has no field that exposes raw internal app-server protocol payloads or asks SDK callers to parse them.
- Public `chat_id` is unambiguously Codex Thread id.
- `StartChatRun` response cannot represent success before first-turn acceptance/correlation.
- The contract supports all QA case families: `TC-001`..`TC-036`, with service-split regression `REG-001` and identity regression `REG-002`.

## Traceability
- SRS: `SRS-FR-001`, `SRS-FR-003`..`SRS-FR-014`, `SRS-FR-016`..`SRS-FR-019`.
- QA: `TC-001`..`TC-036`, especially `TC-001`, `TC-004`, `TC-014`, `TC-019`, `TC-021`, `TC-032`, `TC-034`.
- Regression: `REG-001`, `REG-002`, `REG-011`, `REG-012`.

## Stop And Ask If
- The proto/service name or package direction conflicts with the accepted dedicated chat-first service architecture.
- A contract field would expose raw internal payloads, retained conversation content, task identity as chat identity, or thread-only success.
- Existing task RPC behavior would change without a separate approved migration.
