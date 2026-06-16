# Test Strategy: Chat-First Runtime

Type: test strategy
Artifact purpose / consumer: pre-implementation QA basis for the accepted chat-first runtime paper package.
Status: repaired for accepted paper package; test execution not started
Owner: qa_engineer
Requested mode: full
Required mode floor: full
Approved mode: full
Lane: High-risk
Last repaired: 2026-06-15
Related docs: `../BRS/feature.md`, `../BRS/nfr.md`, `../SRS/index.md`, `../SRS/feature.md`, `../SRS/contracts.md`, `../SRS/states-and-outcomes.md`, `../SRS/transient-correlations.md`, `../SRS/events-history.md`, `../SRS/sequences.md`, `../SRS/nfr-mapping.md`, `../SRS/traceability.md`, `../SRS/rollout.md`, `../tech-design/tech-design.md`, `../tech-design/adr-001-dedicated-chat-first-service.md`, `../reviews/product-review.md`, `../reviews/security-privacy-data-review.md`, `../reviews/release-ops-review.md`, `../../../product-docs/sdk/chat-first-go-sdk.md`, `../../../product-docs/grpc/codex-runtime-gateway.md`, `../../../product-docs/domain/chat-runtime.md`, `../../../product-docs/security/local-runtime-boundary.md`, `../../../product-docs/configuration/gateway-runtime-config.md`, `../../../product-docs/observability/event-stream-observability.md`, `../../../product-docs/operations/local-gateway-runbook.md`, `test-cases.md`, `regression.md`, `test-execution.md`
Trace IDs: BRS-GOAL-001, BRS-REQ-001..BRS-REQ-007, BRS-RULE-001..BRS-RULE-014, BRS-NFR-001..BRS-NFR-009, DOC-SDK-001, DOC-GRPC-001, DOC-DOMAIN-001, DOC-SEC-001, DOC-CONFIG-001, DOC-OBS-001, DOC-OPS-001, SRS-FR-001..SRS-FR-019, SRS-NFR-001..SRS-NFR-009, TC-001..TC-036, QA-EVID-001..QA-EVID-036, REG-001..REG-012

## Source Of Truth
This strategy owns the QA approach and paper test basis for the accepted chat-first runtime package. It is pre-implementation readiness only. It does not claim code execution, local run validation, proto/code readiness, release readiness, or Done.

## Readiness Goal
Freeze a QA basis that is strong enough for documentation sync and later implementation planning without depending on unaccepted implementation-plan artifacts.

## Scope
- Dedicated chat-first service boundary separate from the existing `codex.control.v1.CodexControl` task compatibility surface.
- Identity rule `chat_id == Codex Thread.id` and `run_id == Codex Turn.id` where Codex provides it.
- No empty chat success; `StartChatRun` succeeds only after first-turn acceptance/correlation.
- `GetChat`, `RunChatTurn`, `GetChatStatus`, `GetChatHistory`, `StreamChatEvents`, `RespondChatPending`, and `InterruptChatRun`.
- Typed status, typed history depth, typed replay/restart loss, typed pending and interrupt safety, and typed unsupported/unavailable/unknown/narrowed outcomes.
- All-RPC auth/authz/validation before any Codex side effect or state disclosure.
- Redaction and data minimization: no raw JSONL, no secrets, no private prompt/message/event/history retention, no raw request/response payload retention, no content hashes/digests.
- Local-only config and operations: loopback listen, exact-one token source, `chat_runtime.enabled`, readiness, lazy supervisor, and restart backoff.
- Coexistence with task RPCs, inactive release stance, no Desktop UI visibility promise, and no SQLite/durable mapping/content store in v1.

## Out Of Scope
- Test execution.
- Code review or product-code repair.
- Release activation, current-state acceptance, or production proof.
- Remote, shared, multi-tenant, or internet-facing operation.
- Any requirement that depends on unaccepted implementation-planning artifacts.

## Main Regression Risks
- Public contract drifts back to task-first semantics while keeping chat-first names.
- `chat_id`, `run_id`, task IDs, cursor IDs, pending IDs, or idempotency keys become ambiguous.
- Gateway overpromises restart continuity, replay, pending recovery, or idempotent/no-duplicate recovery from a pre-restart key alone.
- Auth/authz/validation happens after a Codex call or leaks cross-workspace state.
- Logs, diagnostics, docs, tests, or future fixtures leak raw JSONL, secrets, or private content.
- Chat runtime disable/coexistence or shared app-server supervision regresses existing task RPCs.
- Paper package reintroduces Desktop UI promises, release promises, or durable storage assumptions.

## Test Basis
- Business baseline: `../BRS/feature.md`, `../BRS/nfr.md`.
- System baseline: `../SRS/**`.
- Architecture baseline: `../tech-design/tech-design.md`, `../tech-design/adr-001-dedicated-chat-first-service.md`.
- Owner review anchors: `../reviews/product-review.md`, `../reviews/security-privacy-data-review.md`, `../reviews/release-ops-review.md`.
- Future target docs to keep aligned: SDK, gRPC, domain, security, configuration, observability, and operations docs under `../../../product-docs/`.
- QA basis artifacts: `test-cases.md`, `regression.md`, `test-execution.md`.

## Coverage Model
### Identity And Lifecycle
- Prove `chat_id` equality to Codex `Thread.id`.
- Prove no empty chat success and no second durable chat identity.
- Prove `GetChat` resolves existing Codex thread state and does not create a chat.
- Prove `RunChatTurn` keeps the same `chat_id` and enforces one active run per chat.

### Status, History, And Streaming
- Prove typed status categories for thread, run, pending, replay, and history capability depth.
- Prove history is Codex-owned turn-summary projection only.
- Prove item-level history remains unsupported in v1.
- Prove event streaming is normalized, ordered, replay-bounded, and never raw JSONL.
- Prove restart loss of replay and cursor epoch safety.

### Pending, Interrupt, Idempotency, And Restart
- Prove pending response requires active `chat_id` plus `run_id` plus `pending_request_id` correlation.
- Prove interrupt is explicit and separate from stream cancellation.
- Prove current-process idempotency never duplicates a Codex side effect after ambiguous delivery while process-local idempotency evidence survives.
- Prove gateway restart loses active-run, replay, pending, idempotency, and diagnostics correlation; after restart, `chat_id` may be reused only where Codex re-proves thread state and a pre-restart idempotency key alone cannot prove replay, recovery, terminal state, prior result, or no-duplicate delivery.

### Security, Privacy, And Data Minimization
- Prove every public RPC authenticates exactly one bearer token and validates/authorizes before any side effect.
- Prove auth failures, unauthorized scope, and invalid session/workspace requests are generic and safe.
- Prove logs/errors/docs/examples/QA evidence never contain raw JSONL, raw auth material, private content, raw request/response payloads, or content hashes/digests.
- Prove no SQLite, local database, durable mapping store, or retained content/history store becomes part of v1.

### Config, Operations, And Guardrails
- Prove loopback-only listen, exact-one token source, strict config parsing, no non-secret env override, and no external credential-provider command path.
- Prove `chat_runtime.enabled=false` disables the chat-first service while healthy task RPCs may remain serving.
- Prove readiness does not require a prestarted app-server child and that dependency failures become typed `UNAVAILABLE`.
- Prove release/current-state remains inactive and Desktop UI visibility is not promised.

## Test Data Families
- `TD-CHAT-001`: valid non-empty first prompt.
- `TD-CHAT-002`: empty or whitespace-only prompt.
- `TD-AUTH-001`: missing bearer metadata.
- `TD-AUTH-002`: duplicate bearer metadata.
- `TD-AUTH-003`: malformed or wrong bearer metadata.
- `TD-CTX-001`: valid `session_group_id` and `workspace_id`.
- `TD-CTX-002`: invalid, not configured, or unauthorized session/workspace selectors.
- `TD-THREAD-001`: known Codex thread id.
- `TD-THREAD-002`: malformed, unknown, not-found, or unavailable Codex thread id.
- `TD-STATUS-001`: threads/runs across idle, active, pending, completed, failed, interrupted, unavailable, and unknown states.
- `TD-HISTORY-001`: supported turn-summary history.
- `TD-HISTORY-002`: unmaterialized, item-level, unsupported, or narrowed history requests.
- `TD-STREAM-001`: active run with ordered live events.
- `TD-STREAM-002`: valid current-process replay cursor.
- `TD-STREAM-003`: cross-chat, out-of-range, evicted, and post-restart cursor variants.
- `TD-PENDING-001`: active pending request.
- `TD-PENDING-002`: stale, duplicate, mismatched, expired, terminal, and post-restart pending variants.
- `TD-INT-001`: active interruptible run.
- `TD-INT-002`: no-active, already-terminal, already-interrupting, and startup-before-turn interrupt variants.
- `TD-IDEMP-001`: same-scope safe retry in the same process.
- `TD-IDEMP-002`: conflicting idempotency scope.
- `TD-IDEMP-003`: ambiguous prior delivery with same-process retry plus post-restart key-only retry.
- `TD-CONFIG-001`: defaults, TOML, `--listen`, strict duplicate/unknown key rejection, exact-one token source, and invalid token-source fixtures.
- `TD-OPS-001`: disabled chat runtime, app-server unavailable, supervisor backoff, readiness, and restart fixtures.
- `TD-REDACT-001`: secret-like token/header/payload fixtures and raw JSONL-like adapter payload fixtures.
- `TD-GUARD-001`: docs/examples/non-promise fixtures for Desktop UI, release stance, remote scope, and durable store guardrails.

## Evidence Expectations
- Every future executed check must map to one `TC-*` and one `QA-EVID-*`.
- Negative-path evidence must prove both the typed outcome and the absence of the forbidden side effect or disclosure.
- Allowed evidence forms: sanitized request/response transcript, redacted logs/diagnostics, call-count or spy proof for "no Codex call", readiness/health output, replay/pending/idempotency state snapshot, and traceability diff against accepted paper docs.
- Forbidden evidence: raw tokens, auth headers, raw JSONL, prompt/message/event/history content, raw request/response payloads, private dumps, content hashes/digests, or unredacted operator logs.

## Automation Split
- Automate first: identity, start/get/run, status/history/stream, pending/interrupt/idempotency, auth/authz matrix, config validation, disable/readiness/coexistence, replay/restart loss, and no-side-effect proofs.
- Manual or paired review: future docs sync wording, Desktop UI non-promise wording, release/current-state non-promise wording, and redaction review of examples and QA evidence templates.

## Threshold And Drift Checks
- Validate implementation constants/config against `../SRS/nfr-mapping.md` bounds for replay, pending, message size, status detail, readiness timeout, app-server startup timeout, and supervisor cooldown.
- Treat any drift in those values as a QA-basis update trigger, not as an implicit acceptance.

## Exit Criteria For Paper Readiness
- Every required BRS/SRS/product-doc concern in scope maps to at least one `TC-*`.
- Test basis explicitly covers the required high-risk behaviors listed in the accepted paper package.
- No case depends on unaccepted implementation-planning artifacts.
- `test-execution.md` stays not executed.
- Residual risks are stated without pretending they are already validated.

## Forbidden Content
- Claims that tests, code, or runtime behavior were already executed in this repair.
- Any dependence on unaccepted implementation-plan gates.
- Raw secrets, auth headers, token values, raw JSONL, or private data.
