# Plan: Stage 01 Paper Foundation

Type: implementation plan / pre-dev trace
Status: accepted for root-approved implementation; not Done or release approval
Owner: system_analyst
Last repaired: 2026-06-15
Related docs: `index.md`, `../BRS/feature.md`, `../BRS/nfr.md`, `../SRS/index.md`, `../SRS/feature.md`, `../SRS/contracts.md`, `../SRS/states-and-outcomes.md`, `../SRS/transient-correlations.md`, `../SRS/events-history.md`, `../SRS/sequences.md`, `../SRS/traceability.md`, `../SRS/rollout.md`, `../tech-design/tech-design.md`, `../tech-design/adr-001-dedicated-chat-first-service.md`, `../reviews/product-review.md`, `../reviews/security-privacy-data-review.md`, `../reviews/release-ops-review.md`, `../reviews/qa-readiness.md`, `../reviews/engineering-review.md`, `../reviews/documentation-sync-check.md`, `../tests/test-strategy.md`, `../tests/test-cases.md`, `../tests/regression.md`, `../tests/test-execution.md`
Trace IDs: PLAN-STAGE-001, SRS-FR-001, SRS-FR-016, SRS-FR-017, SRS-FR-018, SRS-FR-019

## Source Of Truth
This stage is retained as the accepted paper baseline and handoff guardrail. It is not an implementation slice, test-execution record, release/current-state record, or permission to code.

## Fixed Inputs For All Later Stages
1. Accepted BRS: local chat-first external runtime around installed Codex.
2. Accepted SRS: v1 `Chat == Codex Thread` and public `chat_id == Codex Thread.id`.
3. Accepted architecture: dedicated chat-first service plus existing task compatibility surface.
4. Accepted security/data stance: exact local bearer auth, validation and authorization before Codex side effects, data minimization, and redaction.
5. Accepted release/ops stance: local-only, independent chat disable path, readiness/health semantics, lazy app-server supervisor foundation before side-effecting Codex calls, no release/current-state activation from paper docs.
6. Accepted QA basis: `TC-001`..`TC-036`, `REG-001`..`REG-012`, `QA-EVID-001`..`QA-EVID-036`; `test-execution.md` remains not started.
7. Accepted docs sync: target/future docs and navigation pass paper sync; current-behavior docs sync remains deferred until implementation exists.

## Implementation Handoff Rules
1. Implement stages in the build order from `index.md`.
2. Treat `PLAN-STAGE-001` as a guardrail source, not as an executable implementation or QA anchor.
3. Keep `PLAN-STAGE-004` absent; do not rebuild a removed durable gateway store or persistence surface.
4. Keep `CodexControl` as task compatibility context; chat-first work targets the dedicated chat service.
5. Return typed unavailable/unknown/unsupported/narrowed outcomes when Codex or current-process evidence cannot prove a state.
6. Keep no-duplicate/idempotency guarantees scoped to current-process tracked side effects; after gateway restart, do not promise idempotent replay or duplicate suppression from a pre-restart key alone.
7. Keep all future examples, logs, diagnostics, tests, and QA evidence sanitized and free of raw internal app-server payloads, auth material, and private content.

## Acceptance / Checks
- Plan review can verify that accepted BRS/SRS/architecture/reviews/tests are reflected without adding product scope.
- Every later stage carries its own SRS/QA trace instead of relying on this stage.
- The plan does not claim implementation readiness, QA execution, current-behavior docs sync, or release/current-state activation.

## Stop And Ask If
- A later stage changes public identity, history ownership, local-only boundary, Desktop visible/current-thread stance, or release/current-state stance.
- A later stage needs a durable gateway-owned store, retained conversation content, or cross-process replay/pending/idempotency recovery.
- A later stage needs behavior not already fixed by the accepted BRS/SRS/tech design/reviews.
