# Domain Process: Chat Runtime

Type: product behavior / domain process
Status: current implemented behavior for the working-copy baseline; full QA and release/current-state pending
Owner: system_analyst
Residual release/current-state gate owners: product_owner, security_privacy_data_owner
Consumer / intended use: product, SDK, gateway, and QA owners.
Last repaired: 2026-06-16
Related docs: `docs/product-docs/sdk/chat-first-go-sdk.md`, `docs/product-docs/grpc/codex-runtime-gateway.md`, `docs/epics/chat-first-runtime/SRS/feature.md`, `docs/epics/chat-first-runtime/SRS/sequences.md`
Trace IDs: DOC-DOMAIN-001, BRS-REQ-001..BRS-REQ-007, SRS-FR-001..SRS-FR-012, SRS-FR-016, SRS-FR-018, SRS-FR-019

## Source Of Truth
This product-doc owns the current implemented domain behavior of the chat-first runtime in this working copy. It is not a release/current-state claim, not full-QA evidence, and leaves exact wire details to the SDK and gRPC owning docs.

## Purpose
Provide a stable application chat model around installed Codex without pretending that the gateway is a replacement Codex runtime or Desktop UI.

## Actors / Stakeholders
- Application code using the Go SDK.
- Local gateway process.
- Installed `codex.exe app-server`.
- Product owner and QA validating behavior.

## Trigger
An application sends a non-empty prompt through the SDK for a new or existing chat.

## Main Flow
1. Application calls `codex.Run(ctx, prompt)` for a new chat.
2. Gateway validates/authenticates/authorizes the request and records current-process idempotency/correlation.
3. Gateway uses installed-Codex thread plus first-turn capability for the first prompt.
4. Gateway observes first-turn acceptance/correlation.
5. SDK caller receives and stores `chat_id = Codex Thread.id`.
6. Later, application calls `GetChat(chatID)`, `GetHistory`, `Run`, `GetEventsStream`, status, pending response, or interrupt.

## Alternative Flows
- Existing chat continues with `chat.Run(ctx, prompt)` when Codex can support continuation.
- Stream resumes from a supported/proven current-process cursor or returns replay-unavailable/narrowed behavior.
- Pending request waits for caller response.
- Interrupt is requested for an active Codex turn/run.
- `chat_runtime.enabled=false` leaves the task-compatibility surface intact while the chat-first service reports disabled/not-serving behavior.

## States / Transitions
- unknown: `chat_id` is malformed, unknown, or not found by Codex.
- starting: Codex thread/run start is in progress.
- running: Codex turn/run emits events.
- waiting_on_approval / waiting_on_user_input: Codex requires user/application decision.
- interrupting: interrupt was accepted and terminal state is pending.
- completed: Codex turn/run ended successfully.
- failed: Codex turn/run ended with error.
- interrupted: Codex turn/run stopped by interrupt.
- unknown/unavailable/narrowed: gateway cannot prove current Codex state or requested depth; caller receives typed status/outcome.

## Business Rules
- No empty chat without a first prompt.
- Failed or ambiguous new-chat attempts return no successful chat result; `thread/start` alone must not be advertised as a completed chat.
- Application stores `chat_id`; in v1 that value is Codex `Thread.id`.
- `run_id`, task compatibility ID, Codex turn ID, cursor, pending request ID, and idempotency key must remain distinct from `chat_id`.
- SDK-created chats are not promised to be visible or selected in Desktop UI.
- Gateway must not expose raw app-server JSONL as the stable public API.
- Gateway must not own prompt/message/event history; history is Codex-owned and v1 item-level history is not promised.
- Gateway state for active runs, replay, pending, and idempotency is process-local in v1.

## Permissions
Local authenticated clients only. Existing-chat actions validate local session/workspace scope before using `chat_id` as Codex Thread id; detailed auth rules live in `docs/product-docs/security/local-runtime-boundary.md`.

## Edge Cases
- Gateway restart with app-stored `chat_id` but missing/unavailable Codex state.
- Codex process restart or unavailable app-server.
- `thread/start` succeeds but first `turn/start` is not accepted.
- Replay is unsupported, narrowed, or unavailable after restart.
- Pending request resolved twice or after run terminal state.
- Interrupt arrives before turn starts, while interrupting, or after terminal state.

## Related API / UI / Data Docs
- SDK: `docs/product-docs/sdk/chat-first-go-sdk.md`
- gRPC: `docs/product-docs/grpc/codex-runtime-gateway.md`
- UI: not applicable; Desktop UI visibility is not guaranteed.

## Forbidden Content
- Release delta or temporary change notes.
- Duplicate source of truth for API/UI behavior.
- Raw secrets or private data examples.
