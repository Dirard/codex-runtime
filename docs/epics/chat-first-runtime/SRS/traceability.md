# SRS Traceability: Chat-First Runtime

Type: software requirements specification
Status: paper-ready after accepted targeted product/engineering re-reviews; root implementation approval received
Owner: system_analyst
Requested mode: full
Required mode floor: full
Approved mode: full
Lane: High-risk
Risk uplifts: API/gRPC contract, local auth/security/privacy, config/ops, observability, restart/replay/pending/interrupt semantics
Docs readiness: accepted BRS read; linked product-docs are draft target/future surfaces and not current-behavior claims
Last repaired: 2026-06-15
Related docs: `index.md`, `feature.md`, `contracts.md`, `states-and-outcomes.md`, `transient-correlations.md`, `events-history.md`, `sequences.md`, `nfr-mapping.md`, `rollout.md`, `../BRS/feature.md`, `../BRS/nfr.md`, `../../../product-docs/sdk/chat-first-go-sdk.md`, `../../../product-docs/grpc/codex-runtime-gateway.md`, `../../../product-docs/domain/chat-runtime.md`, `../../../product-docs/security/local-runtime-boundary.md`, `../../../product-docs/configuration/gateway-runtime-config.md`, `../../../product-docs/observability/event-stream-observability.md`, `../../../product-docs/operations/local-gateway-runbook.md`
Trace IDs: SRS-FR-001..SRS-FR-019, SRS-NFR-001..SRS-NFR-009
Stop-if: implementation, release/current-state, remote/multi-tenant scope, Desktop UI synchronization, durable gateway identity/storage, or gateway-owned history/content retention enters scope before the owning requirement gate is reopened.

## Supersession Ledger
| Superseded ID | Replacement | Reason |
| --- | --- | --- |
| `SRS-FR-002` | `SRS-FR-018` | Public `chat_id` is Codex `Thread.id`; no separate durable chat identity requirement remains in v1. |
| `SRS-FR-015` | `SRS-FR-019` | Durable gateway identity/state storage is removed from v1; gateway state is process-local only. |
| `SRS-NFR-003` old meaning | `SRS-NFR-003` repaired meaning | Restart continuity is limited to Codex Thread id reuse; process-local gateway state is not durable. |

## BRS To SRS Mapping
| BRS ID | SRS coverage | Product-doc refs | Review / test implication |
| --- | --- | --- | --- |
| `BRS-GOAL-001` | `SRS-FR-001`..`SRS-FR-019`, `SRS-NFR-001`..`SRS-NFR-009` | all linked target product-docs | Product owner verifies chat-first value and Codex-backed boundary. |
| `BRS-REQ-001` | `SRS-FR-001`, `SRS-FR-006`, `SRS-FR-018`, `states-and-outcomes.md` | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001` | Chat is explicit; v1 Chat is Codex Thread. |
| `BRS-REQ-002` | `SRS-FR-004`, `SRS-FR-005`, `SRS-FR-008`, `sequences.md` | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001`, `DOC-OBS-001` | Start chat and existing-chat turn behavior is testable. |
| `BRS-REQ-003` | `SRS-FR-001`, `SRS-FR-014`, `contracts.md` | `DOC-GRPC-001`, `DOC-SDK-001` | Stable local SDK/gRPC boundary; no raw JSONL contract. |
| `BRS-REQ-004` | `SRS-FR-018`, `contracts.md`, `transient-correlations.md` | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001` | `chat_id == Codex Thread.id`; no gateway-created durable chat id. |
| `BRS-REQ-005` | `SRS-FR-019`, `transient-correlations.md`, `events-history.md`, `rollout.md` | `DOC-CONFIG-001`, `DOC-OBS-001`, `DOC-OPS-001`, `DOC-SEC-001` | Gateway state is process-local only and lost on restart. |
| `BRS-REQ-006` | `SRS-FR-013`, `SRS-NFR-002`, `SRS-NFR-007`, `states-and-outcomes.md` | `DOC-SEC-001`, `DOC-CONFIG-001`, `DOC-OBS-001` | Security/privacy/data review covers auth, validation before side effects, redaction, and data minimization. |
| `BRS-REQ-007` | `SRS-FR-006`..`SRS-FR-014`, `events-history.md`, `states-and-outcomes.md`, `sequences.md` | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001`, `DOC-OBS-001`, `DOC-SEC-001` | Status/history/events/pending/interrupt/error behavior is Codex-backed and typed. |
| `BRS-NFR-001` | `SRS-NFR-001`, `events-history.md`, `nfr-mapping.md` | `DOC-GRPC-001`, `DOC-SDK-001`, `DOC-OBS-001` | Local interactive paths avoid unbounded gateway work; Codex execution time remains outside gateway control. |
| `BRS-NFR-002` | `SRS-NFR-002`, `SRS-FR-013`, `contracts.md`, `states-and-outcomes.md` | `DOC-SEC-001`, `DOC-CONFIG-001` | Local auth and redaction are mandatory before Codex side effects. |
| `BRS-NFR-003` | `SRS-NFR-003`, `SRS-FR-019`, `transient-correlations.md`, `rollout.md` | `DOC-OPS-001`, `DOC-OBS-001`, `DOC-CONFIG-001` | Restart tests must prove no durable replay/pending/idempotency state and no fabricated cross-restart idempotent replay/no-duplicate guarantee. |
| `BRS-NFR-004` | `SRS-NFR-004`, `contracts.md`, `states-and-outcomes.md` | `DOC-OBS-001`, `DOC-OPS-001` | Diagnostics are typed and safe without raw JSONL/private content. |
| `BRS-NFR-005` | `SRS-NFR-005`, `SRS-FR-009`, `SRS-FR-010`, `states-and-outcomes.md` | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-SEC-001` | Pending and interrupt negative paths are explicit. |
| `BRS-NFR-006` | `SRS-NFR-006`, `SRS-FR-006`, `SRS-FR-011` | `DOC-DOMAIN-001`, `DOC-SDK-001`, `DOC-GRPC-001` | API/UI handlers can map stable status/error categories. |
| `BRS-NFR-007` | `SRS-NFR-007`, `SRS-FR-013`, `SRS-FR-014`, `SRS-FR-019` | `DOC-OBS-001`, `DOC-SEC-001` | Safe local audit trail uses IDs and reason codes only. |
| `BRS-NFR-008` | `SRS-NFR-008`, `SRS-FR-017`, `rollout.md` | `DOC-OPS-001`, `DOC-GRPC-001`, `DOC-CONFIG-001` | Disable/rollback preserves task RPC compatibility and Codex-owned state. |
| `BRS-NFR-009` | `SRS-NFR-009`, `SRS-FR-011`, `SRS-FR-014`, `contracts.md` | `DOC-GRPC-001`, `DOC-SDK-001`, `DOC-OBS-001` | Unsupported/narrowed installed-Codex capabilities are typed, not fabricated. |

## SRS To Test Basis Mapping
| SRS ID | Requirement summary | Primary test basis |
| --- | --- | --- |
| `SRS-FR-001` | Public SDK/gRPC contract is chat-first and hides raw JSONL/task-only identities. | SDK/gRPC contract inspection, task identity ambiguity checks, raw JSONL absence checks. |
| `SRS-FR-002` | Withdrawn separate durable chat identity requirement. | Guardrail check that no gateway-created durable chat id is required or documented. |
| `SRS-FR-003` | `codex.GetChat(ctx, chatID)` resolves Codex thread id. | Lookup success, malformed id, not-found, unavailable, auth/authz preflight. |
| `SRS-FR-004` | `codex.Run(ctx, prompt)` starts a new Codex-backed chat with first prompt. | Non-empty prompt, first-turn acceptance, `chat_id == Thread.id`, no empty-chat success, ambiguous delivery. |
| `SRS-FR-005` | `chat.Run(ctx, prompt)` starts a Codex-backed turn in an existing chat. | Existing chat continuation, continuation unavailable, active-run conflict, no unrelated-thread fallback. |
| `SRS-FR-006` | Status returns typed thread/turn/runtime state. | Thread lifecycle, current/last turn lifecycle, pending states, invalid/not-found/unavailable/unknown. |
| `SRS-FR-007` | `chat.GetHistory(ctx)` returns Codex-owned turn summary projection where supported. | Turn summary projection, unsupported item-level history, unmaterialized/unavailable/narrowed history. |
| `SRS-FR-008` | `chat.GetEventsStream(ctx)` returns normalized live stream and typed replay/narrowing outcomes. | Live event order, current-process replay, out-of-range cursor, post-restart replay unavailable, stream cancellation without interrupt. |
| `SRS-FR-009` | Pending requests are Codex-backed states/actions scoped to active turn. | Stale, duplicate, mismatched, expired, terminal, unavailable-after-restart pending response cases. |
| `SRS-FR-010` | Interrupt is explicit, active-turn scoped, Codex-backed, and typed. | Active interrupt, no-active, startup-before-turn, already-terminal, already-interrupting, unavailable cases. |
| `SRS-FR-011` | Unsupported/missing/partial Codex capabilities produce typed outcomes. | Unsupported, unavailable, unknown, narrowed, stale, duplicate, terminal negative-path matrix. |
| `SRS-FR-012` | v1 serializes active run/turn per `chat_id`. | Same-chat concurrent run conflict and no duplicate Codex turn call. |
| `SRS-FR-013` | Local auth, authorization, validation, and redaction apply to every operation. | Unauthenticated, duplicate metadata, invalid session/workspace, unauthorized scope, validation-before-side-effect, redaction. |
| `SRS-FR-014` | Raw JSONL is not public SDK/gRPC/log/test/docs contract. | Public response inspection, logs/diagnostics/examples review, adapter error redaction. |
| `SRS-FR-015` | Withdrawn gateway-owned durable state requirement. | Guardrail check that no durable gateway identity/content store is required for v1. |
| `SRS-FR-016` | SDK-created chats are not promised as current visible Desktop UI threads. | Docs/behavior review proving no Desktop UI current-thread synchronization promise. |
| `SRS-FR-017` | Release/current-state artifacts remain inactive until final implementation and explicit release readiness. | Release gate review proving no release/current-state/current-behavior claim. |
| `SRS-FR-018` | `chat_id == Codex Thread.id`; `run_id == Codex turn id` where provided. | Identity equality, no task/run/cursor/pending/idempotency substitution for `chat_id`. |
| `SRS-FR-019` | Gateway state is process-local only and lost on restart. | Restart-lost active run/replay/pending/idempotency diagnostics; post-restart `GetChat` only where Codex proves state; idempotency key alone is unrecognized as prior request after restart. |
| `SRS-NFR-001`..`SRS-NFR-009` | Non-functional local performance, security, reliability, diagnostics, pending/interrupt safety, API consumability, auditability, disable/rollback, compatibility. | NFR mapping checks in `nfr-mapping.md` plus owner review evidence for security/privacy/data, release/ops, QA readiness, and product review. |

## Product-Doc Surface Mapping
| Product-doc | SRS coverage | Status requirement |
| --- | --- | --- |
| `DOC-SDK-001` | SDK methods, identity, history/stream, pending/status/interrupt, errors, idempotency. | Draft target/future; not current behavior until implementation and docs_writer sync. |
| `DOC-GRPC-001` | Chat-first gRPC operations, request/response semantics, errors, streaming, compatibility. | Draft target/future; exact proto shape remains downstream review. |
| `DOC-DOMAIN-001` | Chat domain lifecycle, transitions, permissions, and edge cases. | Draft target/future; no Desktop UI synchronization claim. |
| `DOC-SEC-001` | Local auth, validation-before-side-effect, data boundaries, secret handling, privacy, audit. | Draft target/future; security/privacy/data review required. |
| `DOC-CONFIG-001` | Local config, token source, loopback, limits, disable path, drift checks. | Draft target/future; release_ops and security review required. |
| `DOC-OBS-001` | Logs, safe IDs, readiness, failure signals, redaction, no production SLOs. | Draft target/future; not release/current-state evidence. |
| `DOC-OPS-001` | Local runbook, troubleshooting, disable path, restart expectations. | Draft target/future; not release plan or current behavior. |

## Test Basis Implications
QA coverage must include:
- new chat start with first prompt and `chat_id == Codex Thread.id`;
- no public chat success after Codex thread creation/loading without accepted first turn;
- `GetChat` by Codex thread id, not-found/unavailable/unknown outcomes;
- existing chat new turn and v1 active-run conflict;
- Codex-backed status for thread and turn states;
- turn summary projection, unsupported item-level history, history-before-materialization typed errors;
- live event stream, replay unavailable/narrowed after restart, stream cancellation without interrupt;
- pending request duplicate/stale/mismatched/terminal/unavailable-after-restart outcomes;
- post-restart retry with only a pre-restart idempotency key does not receive fabricated replay/no-duplicate recovery;
- interrupt active, no-active, startup-before-turn, already-terminal, already-interrupting, unavailable outcomes;
- validation/auth/authz before any Codex side effect for every public chat operation;
- redaction for errors, logs, examples, docs, diagnostics, and QA evidence;
- `chat_runtime.enabled=false` disables the chat-first service while existing task RPCs continue when otherwise healthy;
- target chat-first service identity and absence of raw JSONL escape hatch in the chat-first contract.

## Open Questions
None.

## Completion Criteria
- Every accepted BRS requirement and NFR maps to SRS requirements and draft target product-doc surfaces.
- Every functional requirement has a test-basis implication.
- Withdrawn IDs are marked and not reused for new meaning.
- Traceability contains no implementation plan dependency and does not assert code/proto/release readiness.
