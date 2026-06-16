# QA Readiness: Chat-First Runtime

Type: QA readiness
Artifact purpose / consumer: pre-implementation QA readiness decision for the accepted chat-first runtime paper package.
Status: pass for pre-implementation paper QA readiness
Reviewer: qa_engineer
Last repaired: 2026-06-15
Related docs: `../BRS/feature.md`, `../BRS/nfr.md`, `product-review.md`, `security-privacy-data-review.md`, `release-ops-review.md`, `../SRS/index.md`, `../SRS/feature.md`, `../SRS/contracts.md`, `../SRS/states-and-outcomes.md`, `../SRS/transient-correlations.md`, `../SRS/events-history.md`, `../SRS/sequences.md`, `../SRS/nfr-mapping.md`, `../SRS/traceability.md`, `../SRS/rollout.md`, `../tech-design/tech-design.md`, `../tech-design/adr-001-dedicated-chat-first-service.md`, `../tests/test-strategy.md`, `../tests/test-cases.md`, `../tests/regression.md`, `../tests/test-execution.md`, `../../../product-docs/sdk/chat-first-go-sdk.md`, `../../../product-docs/grpc/codex-runtime-gateway.md`, `../../../product-docs/domain/chat-runtime.md`, `../../../product-docs/security/local-runtime-boundary.md`, `../../../product-docs/configuration/gateway-runtime-config.md`, `../../../product-docs/observability/event-stream-observability.md`, `../../../product-docs/operations/local-gateway-runbook.md`
Trace IDs: BRS-GOAL-001, BRS-REQ-001..BRS-REQ-007, BRS-NFR-001..BRS-NFR-009, DOC-SDK-001, DOC-GRPC-001, DOC-DOMAIN-001, DOC-SEC-001, DOC-CONFIG-001, DOC-OBS-001, DOC-OPS-001, SRS-FR-001..SRS-FR-019, SRS-NFR-001..SRS-NFR-009, TC-001..TC-036, QA-EVID-001..QA-EVID-036, REG-001..REG-012

## Source Of Truth
This review owns the pre-implementation QA readiness verdict for the accepted paper package. It does not claim test execution, implementation readiness, code correctness, or Done.

## Scope Reviewed
- Accepted BRS and NFR baseline.
- Product-reviewed SRS bundle.
- Accepted paper architecture and ADR.
- Security/privacy/data and release/ops paper review outcomes.
- Relevant future target product-docs for SDK, gRPC, domain, security, configuration, observability, and operations.
- Repaired QA strategy, test cases, regression scope, and not-started execution register.

## Decision
pass

Paper QA readiness passes because the repaired test basis now:
- covers the required high-risk behaviors from the accepted package, including identity, no-empty-chat, first-turn acceptance, `GetChat`, `RunChatTurn`, typed status, turn-summary-only history, no item-level history promise, live stream, replay loss after restart, pending, interrupt, current-process idempotency, restart-loss honesty where a pre-restart key alone proves nothing, Codex-proven-only `chat_id` recovery, auth/authz/validation before side effects, redaction, local-only config/listen/token-source, disable/coexistence, supervisor/backoff/readiness, inactive release stance, Desktop UI non-promise, and no SQLite/durable mapping/content store;
- traces every required behavior to BRS/SRS and the future target product-doc surfaces;
- uses accepted review anchors from product, security/privacy/data, and release/ops decisions;
- removes unaccepted implementation-plan dependency from the QA basis;
- keeps `test-execution.md` explicitly not executed.

## Coverage Summary
- Identity and lifecycle: `TC-001`..`TC-008`, `REG-002`..`REG-004`.
- Status, history, stream, replay, and restart loss with Codex-proven-only `chat_id` recovery: `TC-009`..`TC-018`, `TC-027`, `REG-005`..`REG-009`.
- Pending, interrupt, current-process idempotency, and post-restart key-only honesty: `TC-019`..`TC-027`, `REG-008`..`REG-009`.
- Auth, authz, validation-before-side-effect, redaction, and no raw JSONL: `TC-028`..`TC-032`, `REG-010`..`REG-011`.
- Config, disable/readiness, supervisor/backoff, coexistence, and non-promises: `TC-033`..`TC-036`, `REG-001`, `REG-012`.

## Findings
- No blocker remains in the paper test basis after repair.
- `test-execution.md` correctly remains not started; this is required and is not a failure.

## Blockers / Questions
- None for pre-implementation paper QA readiness.

## Residual Risks
- The paper basis is ready, but implementation must still prove that every public RPC enforces auth/authz/validation before any Codex side effect.
- Idempotency, replay, pending, and interrupt safety remain high-risk because they depend on precise current-process state handling and restart-loss behavior.
- Raw app-server JSONL can contain private or secret-like content; adapter and observability implementation must prove normalization and redaction on every public and diagnostic surface.
- The paper package fixes readiness expectations, but implementation still has to choose and verify the concrete "equivalent disabled state" shape for chat-service readiness when `chat_runtime.enabled=false`.
- Bound values from `../SRS/nfr-mapping.md` still need future implementation-level drift checks against actual constants/config.

## Not In Scope For This Pass
- Test execution.
- Code/proto review.
- Release readiness.
- Current-behavior documentation sync completion.

## Next Gate
Original paper-readiness next gate: docs sync and implementation planning could proceed using this repaired QA basis. Current implementation status is tracked in `../tests/test-execution.md`; full QA execution remains pending until the final implementation baseline is ready.

## Forbidden Content
- Any claim that this pass means implementation, execution, release, or Done.
- Any reintroduction of unaccepted implementation-plan dependency as a QA readiness prerequisite.
- Raw secrets, auth headers, token values, raw JSONL, or private data.

## Stage 08 SDK Implementation QA Recheck Addendum
Decision: pass for the targeted Stage 08 SDK/adoption implementation QA recheck after repairs; not full QA execution, Done, release readiness, or current-behavior docs sync.

Review date: 2026-06-16.

Reviewed QA focus:
- SDK `Run`, `GetChat`, `chat.Run`, status, history, event stream, pending response helpers, interrupt helpers, typed error mapping, auth metadata behavior, stream cancellation, generated IDs, and sanitized API-handler example behavior.
- Evidence recorded in `../tests/test-execution.md` and tests in `../../../../client_test.go` plus `../../../../examples/api-handler/main_test.go`.

QA recheck verdict:
- The targeted Stage 08 tests cover the owner-found gaps: SDK-side empty prompt rejection before RPC, malformed start/turn response rejection, generated required IDs, stream close without interrupt, metadata stripping, token redaction, pending payload variants, status/history behavior, typed error details, and local example safeguards.
- Full `TC-001`..`TC-036` execution remains pending until the final implementation baseline is ready.

## Stage 09 Integration QA/Evidence Recheck Addendum
Decision: pass for the targeted Stage 09 QA/evidence recheck after repairs; not full QA execution, Done, release readiness, or current-behavior docs sync.

Review date: 2026-06-16.

Reviewed QA focus:
- Stage 09 evidence wording for scoped guardrail checks.
- Active-run capacity reservation before Codex side effects and focused tests.
- Proto generation sync evidence.
- Remaining gate honesty across `../plan/stage-09.md`, `../tests/test-execution.md`, and `../README.md`.

QA recheck verdict:
- The previous Stage 09 evidence overclaim is closed: legacy-name/import guardrails are scoped to Go/source-code implementation references instead of repo-wide docs.
- The repair has code and test anchors for capacity exhaustion before any Codex side effect.
- Full `TC-001`..`TC-036` execution, current-behavior docs sync, and release/current-state activation remain pending.
