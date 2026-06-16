# ADR-001: Dedicated Chat-First Service And Identity Model

Status: accepted for paper architecture gate; downstream owner review pending
Date: 2026-06-14
Owner: architect
Lane: High-risk
Related design: `tech-design.md`
Related requirements: `../BRS/feature.md`, `../BRS/nfr.md`, `../SRS/index.md`, `../SRS/contracts.md`, `../SRS/transient-correlations.md`, `../SRS/rollout.md`

## Context
The accepted BRS and product-accepted SRS define a local chat-first runtime around installed Codex:

```text
web app -> API handler with Go SDK -> local gateway -> installed codex.exe app-server
```

The current gateway context includes an existing task-oriented compatibility surface. The target product model for this epic is not task-first: application code stores `chat_id`, and in v1 that value is exactly Codex `Thread.id` exposed under chat-first naming.

The gateway must remain a local gRPC compatibility layer over internal `codex.exe app-server` JSONL. It must not own Codex behavior, prompt/message/event/history content, Desktop UI state, or durable chat identity translation.

## Decision
Create a dedicated chat-first local gRPC service for SDK callers and keep the existing task-oriented service as a compatibility surface.

The chat-first service is the only intended SDK target for v1 chat behavior. It owns chat-shaped operation names and typed DTOs for:
- starting a chat run with a non-empty first prompt;
- getting a chat by `chat_id`;
- running a new turn in an existing chat;
- getting status and Codex-owned turn-summary history where supported;
- streaming normalized live/current-process replay events;
- responding to active pending requests;
- interrupting an active Codex turn/run.

The identity model is part of the same decision:
- `chat_id == Codex Thread.id` in v1;
- `run_id == Codex Turn.id` where Codex provides one;
- task compatibility ID, Codex turn ID, cursor, pending request ID, and idempotency key must not be accepted as primary chat identity;
- the gateway must not mint a durable chat id or keep a durable identity mapping store.

The chat-first service must not expose raw app-server JSONL or an equivalent raw payload escape hatch. Unknown or malformed internal Codex events are normalized into redacted typed warning/error/unsupported outcomes, or fail safely when they cannot be normalized.

## Hard-To-Reverse Implications
- A public chat-first service boundary becomes an SDK/gRPC contract once proto, generated code, tests, and docs depend on it.
- The `chat_id == Codex Thread.id` identity model becomes an application storage contract for v1.
- Moving chat methods back into the task service later would be a breaking contract and documentation migration.
- Introducing a gateway-minted durable chat id later would require a new product, SRS, security/privacy/data, QA, release/ops, and migration decision because application identity semantics would change.
- Adding durable gateway-owned history/content storage later would require a new data retention and privacy design; it is not a compatible internal refactor.

## Consequences
Easier:
- SDK and application code use a chat-shaped contract without task identity ambiguity.
- QA can test chat-first behavior separately from task compatibility.
- Security review can forbid raw JSONL and content retention on the chat surface without retrofitting existing task semantics.
- Restart behavior stays honest because only Codex Thread id is durable from the gateway point of view.

Harder:
- The gateway must maintain a clean adapter/domain split so task DTOs and chat DTOs do not leak into each other.
- Compatibility tests must prove existing task RPC behavior is preserved.
- Adapter changes must map internal JSONL into typed chat outcomes without exposing raw payloads.
- Idempotency and replay are limited to current-process evidence, so callers must handle typed unknown/unavailable/narrowed outcomes after restart.

## Rejected Alternatives
### Gateway-minted durable `chat_id`
Rejected because the accepted BRS/SRS require public `chat_id` to be Codex `Thread.id` in v1.

Rejected because a second durable id would create identity translation, migration, support, and QA burden that product explicitly removed from v1.

Rejected because it would make the gateway appear to own chat lifecycle even though Codex owns real chat behavior and history.

### SQLite or durable mapping store in the gateway
Rejected because v1 allows only process-local gateway state for active run, stream cursor/replay, pending, idempotency, and diagnostics.

Rejected because durable gateway storage would reopen privacy, retention, backup, migration, corruption-repair, and release/ops questions that are outside the accepted SRS.

Rejected because restart semantics must be bounded to app-stored Codex Thread id reuse, not gateway recovery of local state.

### SDK-only chat wrapper over task RPCs
Rejected because the stable wire contract would remain task-shaped and would keep task/run identity ambiguity hidden under the SDK.

Rejected because app/API handler owners and QA need a chat-first contract at the local gRPC boundary, not only at SDK convenience level.

### Evolve the task service into chat semantics
Rejected because existing task clients need a compatibility surface.

Rejected because task method names and task identity invite accidental use of task/run ids as `chat_id`.

Rejected because chat-first raw JSONL restrictions should be enforced on a clean service boundary.

### Raw app-server JSONL as public API
Rejected because internal JSONL is unstable and owned by installed Codex internals, not by the SDK/gateway public contract.

Rejected because raw JSONL can leak internal structure, private content, and secret-like data into docs, tests, logs, diagnostics, and caller code.

Rejected because SRS requires typed unsupported/unavailable/unknown/narrowed/stale/duplicate/terminal outcomes instead of caller-side JSONL parsing.

### Desktop UI synchronization
Rejected because accepted BRS/SRS explicitly do not promise SDK-created chats are visible, selected, or current in Codex Desktop UI.

Rejected because Desktop UI state is not the gateway's source of truth and would create product-visible behavior outside v1 scope.

## Required Follow-Up Gates
- Security/privacy/data review for local auth, validation-before-side-effect, redaction, no raw JSONL, no content retention, pending safety, and idempotency without content hashes.
- Release/ops review for config, disable/readiness, supervisor, retry/backoff, local-only operations, and inactive release stance.
- QA readiness review for identity, task compatibility, restart/replay, pending/interrupt, status/history, raw JSONL absence, and redaction coverage.
- Engineering review after implementation artifacts exist for dependency direction, DTO separation, adapter isolation, and generated contract maintainability.

## Stop If
- A downstream design makes task/run identity the primary chat identity.
- A gateway-created durable chat id, SQLite/local DB, durable mapping store, or gateway-owned content/history retention is introduced for v1.
- Raw app-server JSONL becomes a public response, stream field, diagnostic dump, docs example, test oracle, or log output.
- Desktop UI visibility/current-thread synchronization is promised without reopening product and SRS decisions.
