# BRS: Chat-First Runtime Feature

Type: business requirements specification
Status: product baseline; ready for product owner review
Owner: business_analyst
Artifact purpose / consumer: product baseline for product owner review, requirements analysis, QA, docs, SDK, and gateway implementation.
Last repaired: 2026-06-14
Related docs: `nfr.md`, `../README.md`, `../reviews/product-review.md`
Trace IDs: BRS-GOAL-001, BRS-REQ-001..BRS-REQ-007, BRS-RULE-001..BRS-RULE-014, BRS-NFR-001..BRS-NFR-009, BRS-RISK-001..BRS-RISK-009

## Source Of Truth
This BRS owns the product requirement after the product decision that Codex Thread is Chat for v1. Downstream artifacts must follow this document and must not reintroduce a gateway-owned chat identity, durable mapping store, or gateway-owned history.

## Problem
Application teams need a stable, local, chat-first way to use an installed Codex runtime from a web application without coupling application code to unstable/internal `codex.exe app-server` JSONL.

The target product scenario is a web application whose API handler uses the Go SDK to reach a local gateway backed by installed Codex.

The product risk is drift: implementation can accidentally make the external model task-first, make the gateway own Codex conversation state, or invent continuity/history/status behavior that installed Codex does not provide. The gateway must remain a local gRPC compatibility layer over working Codex.

## Target Users / Stakeholders
- Web application end users who interact with Codex-backed chat experiences through a product UI.
- Application developers who call the Go SDK from an API handler and need a small chat-first API.
- API handler owners who store and pass `chat_id` across requests.
- SDK maintainers who keep the Go surface stable and simple.
- Gateway developers who translate between stable gRPC and internal Codex app-server JSONL.
- QA engineers who verify identity, lifecycle/status, events, history, pending, interrupt, restart, and safe failures.
- Security/privacy/data owners who approve the local-only auth, redaction, and data-minimization boundary.
- Product owner and system analyst who decide what is promised externally and what remains out of scope.

## Business Goal
- `BRS-GOAL-001`: provide a stable, local, chat-first external runtime around installed Codex so applications can start a Codex chat, send messages to it, inspect status, and use supported Codex chat capabilities through a Go SDK and local gRPC gateway without depending on internal app-server JSONL or making the gateway the owner of Codex behavior.

## Value / Expected Outcome
- Application code uses an explicit `Chat` concept instead of unrelated tasks.
- The application stores `chat_id` as its durable app-side identifier. In v1, that value is the Codex `Thread.id` exposed under chat-first naming.
- The gateway shields SDK/application callers from internal JSONL churn while remaining a wrapper over Codex, not a second runtime owner.
- Codex remains the source of truth for chat identity, prompt/message/event history, status, pending behavior, interrupt behavior, and real runtime functionality.
- Gateway restart behavior is honest: callers may reuse a stored `chat_id` as a Codex thread id where Codex can read or continue that thread; process-local gateway state such as active stream buffers, pending correlation, and idempotency memory is lost on gateway restart unless Codex itself can prove the state.

## Scope
- `BRS-REQ-001`: define `Chat` as the external product entity. In v1, a chat is a Codex Thread.
- `BRS-REQ-002`: support starting a chat with a non-empty first prompt and sending a new message to an existing chat.
- `BRS-REQ-003`: define a stable local SDK/gRPC product boundary for the SDK-to-gateway interaction while allowing the gateway to adapt unstable/internal app-server JSONL.
- `BRS-REQ-004`: define `chat_id` as the application-facing name for Codex `Thread.id`; SDK/gateway must not mint a separate durable chat identity in v1.
- `BRS-REQ-005`: allow only process-local gateway correlation metadata for active runs, stream cursors, pending requests, idempotency, and diagnostics. No durable gateway identity store is required in v1.
- `BRS-REQ-006`: define local-only security, secret-handling, redaction, and data-minimization expectations for SDK/gateway/docs/tests/examples.
- `BRS-REQ-007`: define safe domain semantics for chat status, event stream, history access, pending request, interrupt, and typed error behavior as Codex-backed capabilities, without exposing raw JSONL or fabricating unsupported data.

## Out Of Scope
- Remote service exposure, internet-facing gateway access, hosted operation, multi-tenant use, or user/account isolation beyond trusted local clients.
- Gateway ownership of prompt history, message history, event history, or Codex functionality.
- Gateway-minted durable chat identity, separate identity translation storage, gateway database, repair/export workflow, or retention policy for gateway-owned chat state in v1.
- Importing arbitrary Codex Desktop chat history into the SDK/gateway.
- Promise that SDK-created chats appear as the current visible Codex Desktop UI thread or are selected/synchronized in Desktop UI.
- Treating internal app-server JSONL as a supported public API.
- Empty chat creation without a first message.
- Product UI design for the web app beyond the fact that it consumes an API handler backed by the SDK.
- Production release, release-grade SLO/SLA, or customer-support messaging before final implementation and explicit release readiness work.

## Assumptions
- v1 is local only.
- The external runtime wraps an installed Codex; it does not replace Codex and must not invent unavailable Codex capabilities.
- `codex.exe app-server` can be started or reached locally by the gateway in the target environment, but its JSONL format is unstable/internal.
- The API handler, not the gateway, owns application business data and stores `chat_id` for later calls.
- `codex.Run(ctx, prompt)` is SDK convenience for starting a new Codex-backed chat with the first prompt. It must not make the gateway the owner of chat lifecycle, history, or business logic.
- Pending/status/interrupt/history/event semantics are promised only where installed Codex exposes enough signal to support them. Otherwise the SDK/gateway returns typed unsupported, unavailable, unknown, or narrowed outcomes.
- Release artifacts remain inactive now; release only happens after final implementation and explicit release readiness work.

## Business Rules
- `BRS-RULE-001`: `Chat` is an explicit external entity with lifecycle/status; in v1 it is a Codex Thread.
- `BRS-RULE-002`: callers can start a chat and send messages to an existing chat.
- `BRS-RULE-003`: the application stores and reuses `chat_id`; in v1 `chat_id` is exactly the Codex Thread id surfaced under chat-first naming.
- `BRS-RULE-004`: SDK/gateway must not mint a second durable chat identifier or keep a durable identity translation table in v1.
- `BRS-RULE-005`: `run_id`, task compatibility identity, Codex turn id, cursor, pending request id, and idempotency key must not be substituted for `chat_id`.
- `BRS-RULE-006`: the gateway is a local gRPC wrapper/compatibility layer over working Codex. It must not own product logic that belongs to Codex.
- `BRS-RULE-007`: gateway state may include process-local correlation metadata only. It must not become a product source of truth for identity, prompt, message, event, or history data.
- `BRS-RULE-008`: raw app-server JSONL must not be documented, tested, logged, or exposed as the stable external contract.
- `BRS-RULE-009`: SDK-created chats must not be promised as visible/current Desktop UI threads.
- `BRS-RULE-010`: external chat status must be Codex-backed and typed. At minimum it must distinguish invalid/unknown/not-found/unavailable thread outcomes, normalized Codex thread lifecycle, and current/last turn lifecycle where applicable.
- `BRS-RULE-011`: chat status, history, event streams, pending responses, and interrupts must reflect Codex-supported facts. Unsupported, stale, duplicate, mismatched, terminal, unknown, unavailable, or narrowed cases must be explicit typed outcomes.
- `BRS-RULE-012`: if a `chat_id` points to a missing or unavailable Codex thread/state, the SDK/gateway must return typed not-found/unavailable/unknown behavior and must not invent prompt, message, event, history, or status data.
- `BRS-RULE-013`: chat-first operations stay in the contract only as Codex-backed capabilities. If installed Codex lacks required support or supports only narrower depth, callers receive typed unsupported, unavailable, unknown, or narrowed results.
- `BRS-RULE-014`: docs, tests, examples, logs, and diagnostics must never contain raw secrets, auth headers, token values, private keys, passwords, cookies, private data dumps, or raw JSONL.

## Constraints
- The solution must run locally around installed Codex.
- The SDK and external model are chat-first. Existing task-oriented gateway behavior may be evolved, wrapped, or versioned only if callers do not face `chat_id`/task identity ambiguity.
- Gateway behavior may provide local gRPC access, compatibility adaptation, process-local correlation, diagnostics, and typed errors, but it cannot become a second implementation of Codex logic.
- Any future need for durable gateway-owned identity storage or retained conversation content reopens product, requirements, security/privacy/data, QA, and release readiness review.

## Must Preserve
- Chat-first external model centered on explicit `Chat`, `chat_id`, lifecycle/status, and message submission.
- `chat_id == Codex Thread.id` in v1.
- Stable external contract separation from internal JSONL.
- Local-only v1 trust boundary unless a new scope decision explicitly changes it.
- Codex ownership of real functionality and prompt/message/event/history data.
- Gateway process-local correlation only; no durable gateway identity store in v1.
- Codex-backed typed status meanings and safe unavailable/unsupported behavior.
- No Desktop UI visibility/current-thread promise for SDK-created chats.
- No raw secret/private data exposure in docs, tests, examples, logs, errors, summaries, or QA evidence.
- Release artifacts remain inactive until final implementation and explicit release readiness work.

## NFR Expectations Summary / Link
Detailed business-level NFR expectations are owned by `nfr.md`.

Summary:
- Stable SDK/gRPC behavior should shield callers from internal JSONL changes.
- Local security, secret handling, and redaction are mandatory despite local-only scope.
- Restart continuity must be bounded to Codex Thread id reuse and must not imply recovery of process-local gateway state.
- Diagnostics must be useful for local QA/support without leaking secrets, private data, raw JSONL, or private prompt/event content.

## Acceptance
Product-owner review of this BRS is required before implementation planning. This BRS is ready for product-owner review because the previously open identity/storage question is closed by the product decision.

Observable business acceptance for the eventual feature requires:
- A caller can start a chat with a non-empty first prompt and receive/store `chat_id = Codex Thread.id`.
- A caller can send a message to an existing chat using the same `chat_id`.
- A caller can inspect explicit chat status without knowing task-oriented RPC details or parsing JSONL.
- SDK/gateway responses never require callers to parse raw internal JSONL.
- Gateway restart behavior follows documented limits: app-stored `chat_id` can be reused only where Codex can read/continue the thread; process-local stream/pending/idempotency state is not promised after restart.
- Codex-owned history/events/pending/interrupt behavior is exposed only where Codex supports it; otherwise callers receive typed unsupported, unavailable, unknown, or narrowed outcomes.
- The BRS does not promise that SDK-created chats appear as the current visible Codex Desktop UI thread.

## Unacceptable Result
- The external model is task-first while only being named chat-first.
- Application callers must store `task_id`, run id, Codex turn id, or another non-chat identifier as their primary identity.
- SDK/gateway mints a second durable chat id and then has to translate it to Codex Thread id.
- Gateway becomes the source of truth for prompt/message/event history or real chat functionality.
- Raw app-server JSONL becomes the public SDK/gRPC contract.
- The product promises history, replay, pending, interrupt, or status behavior that installed Codex cannot actually support.
- Implementation assumes gateway restart preserves active stream/pending/idempotency state without Codex evidence.
- Security docs/tests/examples/logs leak raw secrets, auth headers, private keys, token values, cookies, passwords, private prompts, or private data dumps.

## Risks
- `BRS-RISK-001`: internal app-server JSONL changes may break gateway assumptions unless the compatibility boundary is owned and tested.
- `BRS-RISK-002`: gateway restart semantics can overpromise continuity beyond Codex Thread id reuse.
- `BRS-RISK-003`: users may assume SDK-created chats synchronize with Desktop UI unless docs and errors consistently reject that promise.
- `BRS-RISK-004`: the current task-oriented API model may pull SDK design away from the chat-first product model.
- `BRS-RISK-005`: gateway implementation may drift into storing prompt/message/event history because it is convenient.
- `BRS-RISK-006`: pending/interrupt/status semantics may become unsafe if stale, duplicated, terminal, or unsupported cases are not explicit.
- `BRS-RISK-007`: local-only auth can be accidentally exposed remotely if listen/config rules are weak.
- `BRS-RISK-008`: release expectations may be inferred too early from draft docs.
- `BRS-RISK-009`: installed Codex behavior may be narrower than the desired chat-first experience; product acceptance must stay limited to Codex-backed guarantees and typed unsupported/unavailable/unknown/narrowed outcomes.

## Resolved Product Decisions
- `chat_id` is Codex `Thread.id` in v1, surfaced under chat-first naming.
- No SQLite, separate durable identity translation table, local database, or other durable mapping store is required for v1.
- Gateway may keep only process-local correlation for active run/turn, stream cursor/replay, pending request, current-process idempotency, and diagnostics.
- `codex.Run(prompt)` remains SDK convenience for a new Codex-backed chat with the first prompt. It is not gateway-owned chat logic.
- History is Codex-owned and exposed at turn/summary level where Codex supports it. Item-level history is not promised in v1 because installed Codex currently returns method-not-found for item-level turn history.
- v1 scope is local only.
- Release artifacts are inactive now; release comes only after final implementation and explicit release readiness work.

## Evidence From Installed Codex
- `thread/start` creates a Codex thread/listener and returns a thread with `status=idle`; it does not submit user input.
- `turn/start` submits user input to an existing thread and returns an in-progress turn.
- `thread/read includeTurns` and `thread/turns/list` may fail before the first turn is materialized; v1 must return typed unavailable/unmaterialized outcomes rather than inventing empty history.
- `thread/turns/items/list` currently returns method-not-found; item-level history is not a v1 promise.
- `turn/interrupt` is reliable only after an active turn is observed; immediate startup interrupt may return a typed precondition/unavailable/unknown outcome.

## Open Questions
None.

## Trace Links
| BRS ID | Business meaning | Downstream validation target |
| --- | --- | --- |
| `BRS-GOAL-001` | Stable local chat-first external runtime around installed Codex | SDK, gateway, domain, security, QA, and docs surfaces |
| `BRS-REQ-001` | `Chat` is Codex Thread | Downstream requirements and product-docs |
| `BRS-REQ-002` | Start chat and send message to existing chat | Downstream requirements and QA |
| `BRS-REQ-003` | Stable local gRPC compatibility layer | Downstream requirements and technical review |
| `BRS-REQ-004` | `chat_id == Codex Thread.id` | Downstream requirements and tests |
| `BRS-REQ-005` | Process-local gateway correlation only | Downstream requirements and restart tests |
| `BRS-REQ-006` | Local security/redaction/data minimization | Security/privacy/data requirements and docs |
| `BRS-REQ-007` | Codex-backed status/history/event/pending/interrupt/error semantics | Downstream requirements and QA |

## Forbidden Content
- Detailed API/wire format, final code design, release deliverables, or production acceptance evidence.
- Durable gateway-owned identity storage requirements for v1.
- Raw secrets, tokens, credentials, auth headers, cookies, private keys, passwords, private prompt content, customer PII dumps, or unredacted logs.
