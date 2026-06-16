# Plan Index: Chat-First Runtime

Type: implementation plan bundle index
Status: accepted for root-approved implementation; partial implementation evidence recorded through active stages and Stage 09 closeout; not Done or release approval
Owner: system_analyst
Purpose: self-contained implementation handoff map for the accepted chat-first runtime paper package.
Last repaired: 2026-06-16
Mode: full
Lane: High-risk
Related docs: `../BRS/feature.md`, `../BRS/nfr.md`, `../SRS/index.md`, `../SRS/feature.md`, `../SRS/contracts.md`, `../SRS/states-and-outcomes.md`, `../SRS/transient-correlations.md`, `../SRS/events-history.md`, `../SRS/sequences.md`, `../SRS/traceability.md`, `../SRS/rollout.md`, `../SRS/nfr-mapping.md`, `../tech-design/tech-design.md`, `../tech-design/adr-001-dedicated-chat-first-service.md`, `../reviews/product-review.md`, `../reviews/security-privacy-data-review.md`, `../reviews/release-ops-review.md`, `../reviews/qa-readiness.md`, `../reviews/engineering-review.md`, `../reviews/documentation-sync-check.md`, `../tests/test-strategy.md`, `../tests/test-cases.md`, `../tests/regression.md`, `../tests/test-execution.md`
Trace IDs: PLAN-STAGE-001..PLAN-STAGE-003, PLAN-STAGE-005..PLAN-STAGE-010

## Source Of Truth
This bundle owns only the implementation plan and stage handoff. BRS owns product intent; SRS owns required behavior; tech design and ADR own architecture constraints; QA artifacts own future test basis; reviews record paper-gate decisions.

Accepted inputs already passed:
- BRS product baseline and SRS product review.
- Paper architecture / ADR gate for the dedicated chat-first service.
- Security/privacy/data paper review.
- Release/ops paper review.
- QA readiness and QA-basis product sanity review.
- Paper docs sync check.
- Engineering review for the paper QA basis.
- Product plan review and targeted engineering recheck after plan/SRS/QA repairs.

Root approval to start implementation has been received after the paper gate. This bundle still does not approve release/current-state activation, executed QA, or Done.

## Stage Build Order
Use this order for implementation planning. Numeric IDs are preserved for traceability; the execution order intentionally places `PLAN-STAGE-010` before side-effecting chat behavior because transient correlation rules are cross-cutting.

| Order | Stage ID | Stage file | Purpose |
| --- | --- | --- | --- |
| 0 | `PLAN-STAGE-001` | `stage-01.md` | Accepted paper baseline, invariants, and handoff rules; not an implementation slice. |
| 1 | `PLAN-STAGE-002` | `stage-02.md` | Dedicated chat-first gRPC/proto and generated Go contract surface. |
| 2 | `PLAN-STAGE-003` | `stage-03.md` | Local config, auth, session/workspace validation, disabled behavior, readiness boundary, and minimal pre-side-effect app-server supervisor foundation. |
| 3 | `PLAN-STAGE-010` | `stage-10.md` | Process-local active run, replay/cursor, pending, idempotency, diagnostics, and restart semantics. |
| 4 | `PLAN-STAGE-005` | `stage-05.md` | Gateway `StartChatRun` new-chat path that later backs SDK `codex.Run`, with first prompt and first-turn acceptance. |
| 5 | `PLAN-STAGE-006` | `stage-06.md` | Existing-chat lookup, turn submission, status, history, stream, replay, and cancellation behavior. |
| 6 | `PLAN-STAGE-007` | `stage-07.md` | Pending response, interrupt, supervisor-foundation usage, shutdown, and recovery behavior. |
| 7 | `PLAN-STAGE-008` | `stage-08.md` | Go SDK chat-first API, examples/adoption surface, and API-handler usage. |
| 8 | `PLAN-STAGE-009` | `stage-09.md` | Integration hardening, implementation closeout checks, QA execution handoff, and non-activation of release/current-state. |

## Removed Stage Gap
`PLAN-STAGE-004` remains intentionally absent. It represented the deprecated durable gateway store direction and must not be recreated, replaced with a DB/persistence surface, or used as a hidden implementation dependency. The accepted direction is `chat_id == Codex Thread.id` plus process-local gateway correlations only.

## Cross-Stage Invariants
- Runtime chain stays `web app -> API handler with Go SDK -> local gateway -> codex.exe app-server`.
- Gateway remains a local compatibility wrapper over installed Codex; Codex owns identity, history, behavior, pending semantics, and interrupt truth.
- V1 `Chat == Codex Thread`; public `chat_id == Codex Thread.id`.
- Public `run_id` is Codex turn id where Codex provides one; task IDs, run IDs, cursors, pending IDs, and idempotency keys must not become primary chat identity.
- `codex.Run(ctx, prompt)` and `StartChatRun` start a new Codex-backed chat only with a non-empty first prompt and only return success after first-turn acceptance/correlation is proven.
- `GetChat`, `chat.Run`, history, events, status, pending response, and interrupt are Codex-backed or return typed unsupported/unavailable/unknown/narrowed outcomes.
- Gateway may keep only process-local correlations: active run/turn, replay buffer/cursor, pending, idempotency, and safe diagnostics; restart loses them.
- No-duplicate suppression is limited to side-effecting requests tracked in the current gateway process; after restart, idempotency keys are unrecognized as prior requests, and any recovery must come from Codex-proven state through a usable `chat_id`.
- Public SDK/gRPC surfaces must not expose raw internal app-server protocol payloads or caller-side parsing escape hatches.
- V1 is local-only and does not promise Desktop visible/current-thread behavior.
- `chat_runtime.enabled=false` disables the chat-first service independently while preserving existing task RPC compatibility when otherwise healthy.
- Release/current-state activation happens only after final implementation, current-behavior docs sync, QA execution, and explicit activation gate.

## Traceability Map
| Concern | Plan stage(s) | SRS refs | QA basis |
| --- | --- | --- | --- |
| Dedicated chat-first contract and task compatibility split | `PLAN-STAGE-002`, `PLAN-STAGE-009` | `SRS-FR-001`, `SRS-FR-014`, `SRS-NFR-008` | `TC-034`, `REG-001`, `TC-032` |
| Identity and no gateway-created durable chat id | `PLAN-STAGE-001`, `PLAN-STAGE-002`, `PLAN-STAGE-005`, `PLAN-STAGE-006`, `PLAN-STAGE-010` | `SRS-FR-003`, `SRS-FR-018`, `SRS-FR-019` | `TC-001`, `TC-004`, `TC-006`, `TC-027`, `REG-002` |
| New chat with first prompt and first-turn acceptance | `PLAN-STAGE-005` | `SRS-FR-004`, `SRS-FR-011`, `SRS-FR-018`, `SRS-FR-019` | `TC-001`..`TC-003`, `REG-003` |
| Existing chat, continuation, and active-run serialization | `PLAN-STAGE-006` | `SRS-FR-003`, `SRS-FR-005`, `SRS-FR-012` | `TC-004`..`TC-008`, `REG-004` |
| Status, history, streams, replay, and cancellation | `PLAN-STAGE-006`, `PLAN-STAGE-010` | `SRS-FR-006`, `SRS-FR-007`, `SRS-FR-008`, `SRS-FR-010`, `SRS-FR-011` | `TC-009`..`TC-018`, `REG-005`..`REG-007` |
| Pending, interrupt, idempotency, restart loss | `PLAN-STAGE-007`, `PLAN-STAGE-010` | `SRS-FR-009`, `SRS-FR-010`, `SRS-FR-011`, `SRS-FR-019`, `SRS-NFR-003`, `SRS-NFR-005` | `TC-019`..`TC-027`, `REG-008`, `REG-009` |
| Auth, authz, redaction, config, readiness, disable, supervisor dependency foundation | `PLAN-STAGE-003`, `PLAN-STAGE-007`, `PLAN-STAGE-009` | `SRS-FR-013`, `SRS-FR-014`, `SRS-FR-017`, `SRS-FR-019`, `SRS-NFR-002`, `SRS-NFR-004`, `SRS-NFR-007`, `SRS-NFR-008` | `TC-028`..`TC-036`, `REG-010`..`REG-012` |
| SDK adoption and examples | `PLAN-STAGE-008`, `PLAN-STAGE-009` | `SRS-FR-001`, `SRS-FR-003`..`SRS-FR-011`, `SRS-FR-016`, `SRS-FR-018` | `TC-001`..`TC-036`, docs/guardrail review |

## Plan Acceptance / Checks
- Every active stage has a small implementation sequence, acceptance/checks, stop-and-ask conditions, and SRS/QA trace.
- No active stage depends on `PLAN-STAGE-004`.
- No active stage introduces a gateway-owned conversation store, a thread identity translation layer, retained conversation content, thread-only chat success, Desktop visible/current-thread behavior, external/shared gateway scope, release/current-state activation, or raw internal payload escape hatch.
- Paper gates already passed are not listed as pending plan blockers.
- Remaining gates are limited to developer implementation closeout, post-implementation product/engineering review where touched, QA execution, current-behavior docs sync, and release/current-state activation.

## Stop And Ask If
- A stage needs a product, architecture, security/privacy/data, release/ops, QA, or docs decision not fixed in the accepted BRS/SRS/tech design/reviews.
- A stage needs gateway-owned durable identity, retained conversation content, or cross-process replay/pending/idempotency recovery.
- A stage needs thread-only chat success, Desktop visible/current-thread behavior, external/shared gateway exposure, current-state activation, or public raw internal app-server payload access.
