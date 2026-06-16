# SRS Events And History: Chat-First Runtime

Type: software requirements specification
Status: paper-ready after accepted targeted product/engineering re-reviews; root implementation approval received
Owner: system_analyst
Last repaired: 2026-06-14
Related docs: `index.md`, `feature.md`, `contracts.md`, `states-and-outcomes.md`, `transient-correlations.md`, `sequences.md`, `../../../product-docs/observability/event-stream-observability.md`
Trace IDs: SRS-FR-005, SRS-FR-006, SRS-FR-007, SRS-FR-008, SRS-FR-012, SRS-FR-014, SRS-FR-016, SRS-FR-018, SRS-FR-019, SRS-NFR-001, SRS-NFR-003, SRS-NFR-004, SRS-NFR-009

## History Source Of Truth
Codex owns prompt/message/event/history content. The gateway may request Codex-owned history/events/pending display data and normalize transient projections for authorized SDK/gRPC callers where Codex supports them and after auth/authz. Gateway logs, docs/examples, diagnostics, raw JSONL captures, prompt hashes, and private dumps must not retain that content.

## `chat.GetHistory(ctx)`
- Must treat `chat_id` as Codex `Thread.id`.
- Must use Codex-supported history APIs such as `thread/read includeTurns` or `thread/turns/list` where available.
- Must return turn summary fields/projection only unless installed Codex later proves item-level support.
- The turn summary projection may include turn order, public `run_id` correlation, role/source, first user summary, final assistant summary, status, pending/terminal metadata, timestamps when available, and depth/narrowing metadata.
- Must return typed unsupported/unavailable/unknown/narrowed outcomes when Codex history is unsupported, thread state is unavailable, thread is not materialized, thread is ephemeral, or depth is narrower than requested.
- Must not return raw app-server JSONL, arbitrary Desktop UI history, or gateway-invented messages.

Codex evidence: `Thread.turns` is populated only for specific read/resume/rollback/fork responses with `includeTurns`; item-level turn history currently returns method-not-found.

## `chat.GetEventsStream(ctx)`
The external stream is a normalized gateway stream over Codex-observed events for a chat/turn. It must not expose raw JSONL.

| Mode | Required behavior |
| --- | --- |
| Current live stream | Subscribe to current observed Codex notifications for the chat/turn when the gateway has an attached listener and active state. |
| Start-and-stream | `codex.Run` and `chat.Run` must provide access to stream events for the accepted run/turn. |
| Replay from cursor | Supported only from current-process normalized event buffers within approved replay limits and current gateway process epoch. |
| Narrowed live-only stream | If replay is not supported or no longer available, return typed `narrowed_to_live` or `replay_unavailable` behavior instead of silently starting at a misleading point. |

## Stream Event Semantics
- Events must be typed gateway events with stable categories, safe display text where applicable, status updates, pending request notices, terminal turn result, and redacted adapter warnings.
- Event and pending display content may be delivered only as transient normalized stream data to authorized SDK/gRPC callers; it must not be retained, logged, documented in examples, exposed through diagnostics, hashed, or dumped as raw/private content.
- Event ordering must be stable per `chat_id` and active turn/run as far as Codex/gateway observation supports it.
- Event IDs or sequence numbers must support client dedupe when reconnect redelivers events.
- A cursor must be scoped to exactly one `chat_id`, `run_id`, and process epoch.
- Stream cancellation by the caller closes only that stream. It must not interrupt Codex.
- Unknown or malformed internal Codex events must become redacted typed warning/error/unsupported outcomes; raw JSONL must not leak.

## Replay And Recovery Limits
- Durable replay of event payloads across gateway restart is not provided in v1.
- Replay buffers are process memory only.
- Replay limits default to 2000 events, 8 MiB, and 30 minutes, with hard caps of 5000 events, 32 MiB, and 2 hours.
- After gateway restart, epoch mismatch, eviction, session/workspace mismatch, or missing buffer, stream recovery must be narrowed to status/history lookup plus new live events or fail with typed replay outcome.
- If a cursor points to evicted, unsupported, mismatched, out-of-range, or unprovable data, the result must be typed `replay_unavailable`, `out_of_range`, `unknown`, or `narrowed_to_live`.

## Pending In Streams
- Waiting on approval and waiting on user input must reflect Codex active flags or notifications.
- Pending display payloads are memory/stream data only.
- Pending response events must be correlated to `chat_id`, active turn/run, and pending request ID.
- After gateway restart, previous pending display/correlation may be unavailable unless Codex can prove it again.

## Acceptance Basis
QA must cover turn summary projection, unsupported item-level history, history-before-materialization typed errors, live stream order, stream cancellation without interrupt, no durable replay after restart, replay unavailable/out-of-range/narrowed behavior, pending notices, terminal events, and redaction.
