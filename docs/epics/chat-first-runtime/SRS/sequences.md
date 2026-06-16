# SRS Sequences: Chat-First Runtime

Type: software requirements specification
Status: paper-ready after accepted targeted product/engineering re-reviews; root implementation approval received
Owner: system_analyst
Last repaired: 2026-06-15
Related docs: `index.md`, `feature.md`, `contracts.md`, `states-and-outcomes.md`, `transient-correlations.md`, `events-history.md`, `rollout.md`, `../../../product-docs/domain/chat-runtime.md`, `../../../product-docs/sdk/chat-first-go-sdk.md`, `../../../product-docs/grpc/codex-runtime-gateway.md`, `../../../product-docs/observability/event-stream-observability.md`
Trace IDs: SRS-FR-001..SRS-FR-019

## Changed Scenarios
| Scenario | SRS refs | Product-doc refs | Test refs |
| --- | --- | --- | --- |
| New chat with first prompt | `SRS-FR-001`, `SRS-FR-004`, `SRS-FR-018`, `SRS-FR-019` | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001` | new-chat and identity tests |
| Get existing chat | `SRS-FR-003`, `SRS-FR-006`, `SRS-FR-018` | `DOC-SDK-001`, `DOC-DOMAIN-001` | lookup/not-found/auth tests |
| Existing chat new turn | `SRS-FR-005`, `SRS-FR-006`, `SRS-FR-012`, `SRS-FR-019` | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001` | existing-chat turn tests |
| History retrieval | `SRS-FR-007`, `SRS-FR-011` | `DOC-SDK-001`, `DOC-DOMAIN-001`, `DOC-OBS-001` | history-depth tests |
| Event stream live/replay | `SRS-FR-008`, `SRS-FR-011`, `SRS-FR-014`, `SRS-FR-019` | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-OBS-001` | live stream/replay tests |
| Pending response | `SRS-FR-006`, `SRS-FR-009`, `SRS-FR-011`, `SRS-FR-013`, `SRS-FR-019` | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-SEC-001` | pending tests |
| Interrupt | `SRS-FR-006`, `SRS-FR-010`, `SRS-FR-013` | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001` | interrupt tests |
| Gateway restart | `SRS-FR-006`, `SRS-FR-011`, `SRS-FR-019` | `DOC-OPS-001`, `DOC-OBS-001` | restart/recovery tests |

## New Chat Run
1. Caller invokes SDK `codex.Run(ctx, prompt)` or gRPC `StartChatRun`.
2. SDK/gateway validates request shape, idempotency key, non-empty prompt, exact local bearer metadata, and session/workspace context before any Codex call.
3. Gateway authorizes the requested session/workspace.
4. Gateway records process-local idempotency/correlation for the current request only.
5. Gateway uses the pre-side-effect app-server supervisor foundation for the authorized session group; startup timeout, caller-deadline precedence, repeated-failure cooldown, and typed dependency failure apply before `thread/start`.
6. Gateway uses installed-Codex thread creation/loading capability to obtain the Codex thread/listener. Current Codex evidence maps this to `thread/start`, which does not submit user input.
7. Gateway uses installed-Codex first-turn submission capability with the first prompt. Current Codex evidence maps this to `turn/start`, which submits user input and returns an in-progress turn.
8. Gateway observes first-turn acceptance/correlation.
9. Caller receives `chat_id = Thread.id`, `run_id = Turn.id`, current typed status, and stream/cursor metadata.

Required branches:
- If validation, auth, authz, or current-process idempotency checks fail before Codex side effects, no `chat_id` is returned and no Codex call is made.
- If Codex thread creation/loading succeeds but first-turn submission fails or is not accepted, gateway returns a typed failure without promising an empty chat.
- If Codex delivery/acceptance cannot be proven while the current gateway process still tracks the idempotency record, gateway returns `UNKNOWN` or `UNAVAILABLE` and must not duplicate a side-effecting Codex call under that current-process key.
- After gateway restart, a retry carrying only a pre-restart idempotency key is not recognized as a prior request; the gateway may use an app-stored `chat_id` only as Codex `Thread.id` where Codex proves state, and otherwise must not claim idempotent replay, recovery, or cross-restart duplicate suppression.
- If auth/authz fails, no Codex call is made.

## Get Existing Chat
1. Caller invokes `codex.GetChat(ctx, chatID)` or equivalent gRPC chat lookup.
2. Gateway authenticates caller and validates requested session/workspace syntax and configuration.
3. Gateway treats `chat_id` as Codex `Thread.id` and asks Codex for status/read data where supported.
4. Gateway returns a handle plus Codex-backed status metadata or a typed error.

Required branches:
- Invalid/not configured session or workspace returns `INVALID_ARGUMENT`.
- Unknown/not-found thread returns `NOT_FOUND` where absence is proven.
- Unavailable Codex state returns `UNAVAILABLE` or `UNKNOWN`.
- `GetChat` must not create a chat, start a run, import Desktop UI history, promise Desktop UI visibility, or expose task-only identifiers as primary identity.

## Existing Chat New Turn
1. Caller invokes `chat.Run(ctx, prompt)`.
2. Gateway validates auth, session/workspace access, prompt, idempotency key, and active chat state.
3. Gateway rejects the request when an active run/turn already exists for the chat in v1.
4. Gateway calls Codex `turn/start` against the thread identified by `chat_id`.
5. Gateway streams normalized live events and terminal state as observed from Codex.

Required branches:
- If Codex cannot continue the thread, gateway must return typed unavailable/recovery behavior instead of silently starting an unrelated thread.
- If an active run already exists for the chat, gateway returns typed active-run conflict and does not call Codex.

## History Retrieval
1. Caller invokes `chat.GetHistory(ctx)`.
2. Gateway authenticates, validates requested session/workspace syntax and configuration, and treats `chat_id` as Codex `Thread.id`.
3. Gateway calls Codex-supported history APIs and returns normalized ordered turn summary fields/projection where supported.

Required branches:
- If Codex history is unsupported, unmaterialized, ephemeral, unavailable, or narrower than requested, response includes typed unsupported/unavailable/unknown/narrowed behavior.
- History must not return raw JSONL, gateway-invented messages, item-level history, or arbitrary Desktop UI history.

## Event Stream
1. Caller invokes `chat.GetEventsStream(ctx)` or gRPC stream with `chat_id` and optional cursor.
2. Gateway authenticates, validates requested session/workspace syntax and configuration, and validates cursor ownership.
3. Gateway provides live current events for active observed work and replays only from in-memory normalized event buffers within approved replay limits and current process epoch.
4. Gateway emits terminal event/status when the active run ends.

Required branches:
- Cursor from another chat/run, unsupported replay, evicted replay, process restart, epoch mismatch, or unprovable state fails safely or returns typed narrowed-to-live behavior.
- Reconnect may redeliver events when replay is supported; event IDs/sequences support dedupe.
- Client stream cancellation closes only that stream and must not interrupt the active Codex run.

## Pending Response
1. Internal Codex flow emits a pending request.
2. Gateway records process-local `pending_request_id`, `chat_id`, `run_id`, pending status, timestamps, and expiry metadata.
3. Gateway exposes pending state/status and safe display data to SDK/gRPC caller when available.
4. Caller responds with `chat_id`, `pending_request_id`, and response payload/choice.
5. Gateway authenticates, validates requested session/workspace syntax and configuration, validates the pending request is active and belongs to the current run, and forwards the response to Codex only when correlation is valid.

Required branches:
- Duplicate, stale, expired, mismatched, terminal, or unavailable-after-restart pending response returns typed failure.
- Pending response must not auto-approve or broaden Codex permissions.

## Interrupt
1. Caller explicitly invokes interrupt for `chat_id` or active run.
2. Gateway validates auth, session/workspace, idempotency key, and active state.
3. Gateway sends interrupt to Codex for the active turn/run.
4. Gateway emits terminal interrupted/failed/unknown/unavailable state according to observed Codex result.

Required branches:
- Interrupt before Codex active turn is known returns typed precondition/unavailable behavior unless Codex can safely accept it.
- Interrupt after terminal state returns already-terminal behavior.
- Repeated interrupt while interrupting returns already-interrupting behavior.
- Stream cancellation remains separate from interrupt and never sends Codex interrupt.

## Restart Summary
```text
new_chat -> thread_started -> first_turn_accepted -> visible chat_id == Thread.id
idle -> active_running -> completed | failed | interrupted
active_running -> waiting_on_approval | waiting_on_user_input -> active_running
active_running/waiting -> interrupting -> interrupted | failed | unknown
gateway_restart -> lose replay/pending/idempotency/current-run memory
post_restart_get_chat -> Codex read/status if available | not_found | unavailable | unknown | narrowed
post_restart_idempotency_key_only -> unrecognized_as_prior_request | normal_preconditions | no_replay_claim
```

## Edge Cases
- Whitespace-only prompt.
- Long prompt or event exceeding configured message size.
- New chat request retry after `thread/start` but before `turn/start` acceptance.
- Existing chat run retry after Codex accepted internal `turn/start`.
- Gateway crash after first turn acceptance but before caller receives response.
- Post-restart retry with only a pre-restart idempotency key and no usable Codex Thread id.
- Pending request arrives while stream subscriber is disconnected.
- Interrupt while pending request is active.
- Codex child process exits without terminal JSONL event.
- Internal JSONL event arrives out of expected order.
- Replay cursor is valid syntactically but replay is unsupported, narrowed, or unprovable after restart.
- Chat runtime is disabled while task RPCs remain enabled.
- Idempotency retry conflicts with safe scope or previous delivery is unknown.
- User asks for Desktop UI thread visibility for SDK-created chat.

## Acceptance / Test Basis
QA must recreate or revalidate test cases from `traceability.md`. Existing `../tests/**` files are downstream draft inputs only until QA refreshes them against this repaired SRS.
