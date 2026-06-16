# gRPC Service: codex-runtime Gateway

Type: product behavior / gRPC contract
Status: current implemented behavior for the working-copy baseline; full QA and release/current-state pending
Owner: system_analyst
Residual release/current-state gate owners: security_privacy_data_owner, qa_engineer
Consumer / intended use: SDK implementers, gateway implementers, reviewers, and QA.
Last repaired: 2026-06-16
Related docs: `proto/codex_control/v1/codex_control.proto`, `docs/product-docs/sdk/chat-first-go-sdk.md`, `docs/product-docs/domain/chat-runtime.md`, `docs/epics/chat-first-runtime/SRS/contracts.md`, `docs/epics/chat-first-runtime/SRS/sequences.md`
Trace IDs: DOC-GRPC-001, SRS-FR-001..SRS-FR-019, SRS-NFR-001, SRS-NFR-004, SRS-NFR-009
Auth/Authz: local authenticated client only; bearer token source remains configured outside docs and examples.

## Source Of Truth
This product-doc owns the current implemented external gateway contract semantics in this working copy. It is not a release/current-state claim, not full-QA evidence, and does not replace the generated proto for exact field-by-field wire details.

## Purpose
Expose a stable local gRPC runtime contract to SDK clients while the gateway adapts to unstable internal `codex.exe app-server` JSONL.

## Current Proto Services
The implemented proto file is `proto/codex_control/v1/codex_control.proto` in package `codex.control.v1`.

Task-compatibility service preserved in the same proto:
- `StartTask`
- `StreamTask`
- `RespondPendingRequest`
- `InterruptTask`
- `GetTaskStatus`

`CodexControl` remains a task-compatibility surface. It is not the owning SDK chat-first service contract, and its task IDs must not be treated as `chat_id`.

Implemented chat-first service in the same proto:
- `ChatRuntimeService`

## ChatRuntimeService / Methods
- `StartChatRun`: start chat run with first prompt using installed-Codex thread plus first-turn capability; return `chat_id = Thread.id` only after first-turn acceptance/correlation;
- `GetChat`: get existing chat by `chat_id` as Codex `Thread.id`;
- `GetChatHistory`: get Codex-owned turn summary fields/projection where supported;
- `RunChatTurn`: run a new turn in an existing chat when no active run exists;
- `StreamChatEvents`: stream current live chat events and return typed replay-unavailable/out-of-range/narrowed behavior when current-process replay cannot be proven;
- `GetChatStatus`: get Codex thread lifecycle, current/last run, pending, history-depth, and replay-capability status;
- `RespondChatPending`: respond to an active pending request;
- `InterruptChatRun`: interrupt the active Codex turn/run.

The chat-first service must not include raw app-server JSONL or any equivalent raw payload escape hatch.

## Request Messages
- Every request requires exactly one bearer auth metadata value supplied by the local configured token source.
- New chat run requires session/workspace selection, current-process idempotency key, and non-empty prompt.
- Existing chat operations require `chat_id`, which is Codex `Thread.id`.
- Stream requests require `chat_id` and cursor choice.
- Pending and interrupt requests require `chat_id`, active `run_id` correlation where applicable, stable request IDs, and idempotency IDs.

## Response Messages
- New chat run returns `chat_id`, `run_id`, current typed status, and stream cursor metadata only after validation/auth/authz, Codex thread identity, and first-turn acceptance/correlation are proven.
- Failed or ambiguous `StartChatRun` responses return typed failure without promising an empty chat.
- History returns ordered Codex-owned turn summary fields/projection where supported.
- Status returns Codex thread lifecycle, current/last run lifecycle, active pending requests, terminal info, and stream/history capability depth.
- Errors use typed details and safe display messages.

Public response metadata may expose only `chat_id`, `run_id`, typed status, event cursor/sequence, `pending_request_id`, and idempotency outcomes. Raw app-server JSONL payloads and task-only identities are not SDK/gRPC contract fields.

## Errors / Status Codes
- missing, duplicate, malformed, or wrong bearer auth returns `UNAUTHENTICATED`;
- invalid/not configured session or workspace returns `INVALID_ARGUMENT`;
- unauthorized local scope returns `PERMISSION_DENIED` without revealing external state;
- unknown or not-found chat/thread returns `NOT_FOUND`;
- session group unavailable or config mismatch returns `UNAVAILABLE` with safe reason;
- invalid prompt or malformed IDs return `INVALID_ARGUMENT`;
- unsupported/unavailable/out-of-range/narrowed history or replay is typed;
- gateway process dependency or Codex app-server unavailable returns `UNAVAILABLE`;
- active-run conflict, pending request not active/stale/duplicate/terminal/mismatched, no active run, already interrupting, or already terminal returns typed precondition/conflict;
- current-process idempotency conflict returns `ABORTED`; unknown previous side effect returns `UNKNOWN` or `UNAVAILABLE` without duplicate Codex call.

## Streaming / Deadlines / Retries
- Server streaming must preserve event order per chat/run as far as Codex/gateway observation supports it.
- Replay notices or typed narrowed-to-live outcomes are required when a requested cursor cannot be served.
- Client retries must be safe through current-process idempotency IDs for start/run/pending/interrupt where side effects exist.
- Deadlines should report clear timeout/unavailable errors without leaking internal JSONL payloads.

## Compatibility / Versioning
- Stable contract changes require SRS update, proto regeneration, SDK update, tests, and compatibility notes.
- Internal JSONL changes should be absorbed by gateway adapters when possible.
- Existing task RPC behavior must remain unchanged unless a separate approved task-RPC migration is created.

## Examples
Use the generated proto and SDK tests as field-shape references. Do not copy raw bearer tokens or raw app-server JSONL into examples.

## Forbidden Content
- Raw secrets, tokens, auth headers, cookies, passwords, private keys, or customer PII dumps.
- Release delta or implementation-only notes.
