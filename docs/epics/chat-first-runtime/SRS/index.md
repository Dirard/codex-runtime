# SRS Index: Chat-First Runtime

Type: software requirements specification
Status: paper-ready after accepted targeted product/engineering re-reviews; root implementation approval received
Owner: system_analyst
Requested mode: full
Required mode floor: full
Approved mode: full
Lane: High-risk
Risk uplifts: API/gRPC contract, local auth/security/privacy, config/ops, observability, restart/replay/pending/interrupt semantics
Docs readiness: accepted BRS read; linked product-docs are draft target/future surfaces and not current-behavior claims
Role collapses: none for SRS authoring; required product_owner, security_privacy_data_owner, release_ops_owner, qa_engineer, architect, docs/plan paper reviews and targeted engineering recheck are complete for this SRS scope; root implementation approval has been received
Last repaired: 2026-06-14
Related BRS: `../BRS/feature.md`, `../BRS/nfr.md`
Related SRS: `feature.md`, `contracts.md`, `states-and-outcomes.md`, `transient-correlations.md`, `events-history.md`, `sequences.md`, `nfr-mapping.md`, `traceability.md`, `rollout.md`
Related draft target product-docs: `../../../product-docs/sdk/chat-first-go-sdk.md`, `../../../product-docs/grpc/codex-runtime-gateway.md`, `../../../product-docs/domain/chat-runtime.md`, `../../../product-docs/security/local-runtime-boundary.md`, `../../../product-docs/configuration/gateway-runtime-config.md`, `../../../product-docs/observability/event-stream-observability.md`, `../../../product-docs/operations/local-gateway-runbook.md`
Trace IDs: SRS-FR-001..SRS-FR-019, SRS-NFR-001..SRS-NFR-009
Stop-if: implementation scope, release/current-state scope, remote/multi-tenant scope, Desktop UI synchronization, durable gateway identity/storage, or gateway-owned history/content retention is introduced before the corresponding owner decision and SRS update.

## Scope
This SRS converts the repaired BRS into system behavior for the local chat-first gateway and Go SDK. The core product decision is fixed: public `chat_id` is Codex `Thread.id`; the gateway does not mint a separate durable chat identity and does not require a local identity database in v1.

Target runtime chain:

```text
web app -> API handler with Go SDK -> local gateway -> codex.exe app-server
```

The gateway is a local gRPC wrapper and compatibility layer over Codex app-server JSONL. It does not own Codex logic, prompt/message/event/history content, or Desktop UI state. Authorized SDK/gRPC callers may receive transient normalized projections of Codex-owned history, events, and pending display data where Codex supports them and after auth/authz.

## Bundle Map
| File | Purpose |
| --- | --- |
| `feature.md` | Functional requirements and affected surfaces summary. |
| `contracts.md` | SDK and gateway/gRPC contract requirements. |
| `states-and-outcomes.md` | Status, permissions, validation, errors, unsupported/unavailable/narrowed outcomes. |
| `transient-correlations.md` | Process-local run, pending, stream, idempotency, and restart semantics. |
| `events-history.md` | Live stream, replay, current stream, history depth, and Codex support boundaries. |
| `sequences.md` | Required behavior sequences and restart notes. |
| `nfr-mapping.md` | BRS NFR to SRS requirement mapping. |
| `traceability.md` | BRS-to-SRS-to-product-doc/test traceability and supersession ledger. |
| `rollout.md` | Compatibility, inactive release, disable, and future delivery-readiness constraints. |

## System Decisions Made In SRS
- Public identity: `chat_id == Codex Thread.id`.
- Public run identity: `run_id == Codex Turn.id` where Codex provides a turn id.
- Start behavior: `codex.Run(prompt)` and `StartChatRun` use installed-Codex thread plus first-turn facts; success is returned only after Codex thread identity and first-turn acceptance/correlation are proven.
- No empty chat: Codex thread creation/loading alone must not be exposed as a successful SDK-created chat.
- Runtime state: replay buffers, pending correlation, active run correlation, and idempotency are process-local in v1.
- Restart behavior: after gateway restart, callers may reuse `chat_id` only as a Codex Thread id; pre-restart replay/pending/idempotency state is not promised.
- Run serialization: v1 exposes one active Codex turn/run per `chat_id`; concurrent turns return typed conflict/precondition.
- History depth: v1 promises Codex-owned turn summary fields/projection where installed Codex supports `thread/read includeTurns` or `thread/turns/list`. Item-level history is unsupported unless later Codex evidence proves support.
- Event replay: v1 live streaming is required for current observed Codex events. Replay is in-memory only under bounded replay limits.

## Codex Evidence Anchors
- `thread/start` creates an idle thread/listener and does not submit user input.
- `turn/start` submits user input to an existing thread and returns an in-progress turn.
- `thread/read includeTurns` and `thread/turns/list` may fail before the first turn is materialized.
- `thread/turns/items/list` currently returns method-not-found.
- `turn/interrupt` succeeds only when the target active turn can be proven; immediate startup interrupt may fail with a typed precondition/unavailable/unknown outcome.

## Explicitly Out Of Scope
- Remote gateway exposure, hosted service, team-shared service, multi-tenant isolation, or internet-facing operation.
- Gateway-owned prompt, message, event, item, or conversation history.
- Gateway-minted durable chat identity or durable identity translation storage.
- Raw app-server JSONL as a public SDK/gRPC contract.
- Importing arbitrary Desktop UI chats or promising SDK-created chats are visible/current Desktop UI threads.
- Production release, release notes, current-state acceptance, SLO/SLA, customer-support commitments, or active release artifacts.
- Final proto/code changes in this SRS step.

## Open Questions
None.

## Readiness Verdict
SRS package status: paper-ready after accepted targeted product/engineering re-reviews and required paper gates for the current SRS scope.

Root approval before implementation planning/implementation has been received. Implementation execution, QA execution, current-behavior docs sync, and release remain pending.
