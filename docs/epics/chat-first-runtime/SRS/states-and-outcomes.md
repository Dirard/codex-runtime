# SRS States And Outcomes: Chat-First Runtime

Type: software requirements specification
Status: paper-ready after accepted targeted product/engineering re-reviews; root implementation approval received
Owner: system_analyst
Last repaired: 2026-06-14
Related docs: `index.md`, `feature.md`, `contracts.md`, `transient-correlations.md`, `events-history.md`, `sequences.md`, `../BRS/feature.md`, `../BRS/nfr.md`
Trace IDs: SRS-FR-006, SRS-FR-007, SRS-FR-008, SRS-FR-009, SRS-FR-010, SRS-FR-011, SRS-FR-012, SRS-FR-013, SRS-FR-014, SRS-FR-018, SRS-FR-019

## Status Model
The external status model must be typed and Codex-backed. Exact enum names and protobuf fields are architecture-owned, but the meanings below are mandatory.

| Layer | Required meanings |
| --- | --- |
| Chat/thread lookup | `valid`, `invalid`, `not_found`, `codex_unavailable`, `unauthorized`, `unknown`. |
| Codex thread lifecycle | `not_loaded`, `idle`, `active_running`, `waiting_on_approval`, `waiting_on_user_input`, `system_error`, `unknown`. |
| Current or last turn lifecycle | `in_progress`, `completed`, `interrupted`, `failed`, `turn_unknown`, `turn_unavailable`. |
| Capability depth | `supported`, `unsupported`, `unavailable`, `unknown`, `narrowed`. |
| Gateway-local state | `live`, `replay_available`, `replay_unavailable`, `pending_known`, `pending_unavailable_after_restart`, `idempotency_unavailable_after_restart`. |

Codex evidence: `ThreadStatus` has `NotLoaded`, `Idle`, `SystemError`, and `Active` with `WaitingOnApproval` and `WaitingOnUserInput`; `TurnStatus` has `Completed`, `Interrupted`, `Failed`, and `InProgress`.

## Permission Requirements
- Every gRPC gateway call must require exactly one `authorization` metadata value equal to `Bearer <configured token>`.
- Missing, duplicate, malformed, or wrong bearer metadata must return `UNAUTHENTICATED` with a generic safe message.
- Authorization metadata must be stripped before handlers, logs, adapters, diagnostics, and downstream contexts.
- Every request must carry or resolve the SDK-bound `session_group_id` and `workspace_id` before any Codex side effect.
- Invalid or not configured session/workspace context must return `INVALID_ARGUMENT`.
- Existing-chat operations use `chat_id` as Codex `Thread.id`; they must validate the request scope before calling Codex.
- Unknown or not-found Codex thread in the authorized local context must return `NOT_FOUND` or typed unavailable/unknown when absence cannot be proven.
- Pending response and interrupt operations must verify the active turn/run before forwarding to Codex.
- No SDK/gateway operation may call Codex before authentication, request validation, and workspace/session authorization pass.
- No SDK/gateway operation may grant access to arbitrary Desktop UI history, other workspaces, remote clients, or multi-tenant data.

## All-RPC Auth/Authz Preflight Matrix
For every row, authentication must happen before any Codex call, stream attach, pending response, interrupt, or side effect.

| RPC | Required pre-side-effect gate |
| --- | --- |
| `StartChatRun` | Authenticate, validate request/session/workspace and prompt/idempotency, authorize session/workspace, then call `thread/start` and `turn/start`. |
| `GetChat` | Authenticate, validate request/session/workspace and `chat_id`, authorize session/workspace, then call Codex read/status behavior. |
| `GetChatHistory` | Authenticate, validate request/session/workspace and `chat_id`, authorize session/workspace, then call Codex history behavior. |
| `RunChatTurn` | Authenticate, validate request/session/workspace, `chat_id`, prompt, and idempotency, authorize session/workspace, then check active-run state and call Codex turn behavior. |
| `StreamChatEvents` | Authenticate, validate request/session/workspace, `chat_id`, and cursor, authorize session/workspace, then attach to current live stream or current-process replay. |
| `GetChatStatus` | Authenticate, validate request/session/workspace and `chat_id`, authorize session/workspace, then read Codex/gateway-local status. |
| `RespondChatPending` | Authenticate, validate request/session/workspace, `chat_id`, `pending_request_id`, response shape, and idempotency, authorize session/workspace, then forward only if the active pending correlation is proven. |
| `InterruptChatRun` | Authenticate, validate request/session/workspace, `chat_id`, optional `run_id`, and idempotency, authorize session/workspace, then send interrupt only if the active turn is proven. |

## Validation Requirements
- Prompt input for `codex.Run` and `chat.Run` must be non-empty after validation and within configured size limits.
- `chat_id`, pending request IDs, event cursors, idempotency keys, and workspace/session selectors must be syntactically valid before side effects.
- Cursor replay requests must be scoped to the same `chat_id` and must fail if reused across chats.
- Side-effecting operations must be idempotency-aware within the current process where retries could duplicate Codex work or duplicate a pending/interrupt action.

## Error / Outcome Classes
| Outcome | Required use |
| --- | --- |
| `invalid_argument` | Malformed IDs, empty prompt, invalid cursor shape, invalid/not configured session or workspace, unsupported request shape. |
| `unauthenticated` | Missing, duplicate, malformed, or wrong bearer metadata. |
| `permission_denied` | Request scope is unauthorized; response must not reveal external state. |
| `not_found` | Codex thread is unknown or known absent within authorized local context. |
| `failed_precondition` | Active run conflict, no active run to interrupt, pending mismatch, stale/terminal pending response, terminal run mutation, interrupt before active turn is known. |
| `aborted` | Idempotency key is reused with conflicting operation or safe scope in the current process. |
| `unsupported` | Installed Codex or gateway contract does not support the requested capability, such as item-level history. |
| `unavailable` | Gateway process dependency, Codex app-server, Codex thread state, replay buffer, pending correlation, or history source cannot be reached. |
| `unknown` | Prior side effect, current Codex state, or delivery cannot be proven safely. |
| `out_of_range` | Cursor is well-formed but outside current-process range or scoped to a different chat/run. |
| `narrowed` | Caller requested a deeper capability than v1 can prove; response is limited to supported depth. |
| `deadline_exceeded` | Request or stream deadline elapsed without a completed result. |
| `cancelled` | Caller cancelled the SDK/gRPC request or stream. Cancellation is not interrupt. |
| `internal` | Adapter translation or invariant failure after redaction. |

Error details must never include raw JSONL, raw auth headers, token values, cookies, passwords, private keys, raw environment values, private prompts, private message/event content, or customer/private data dumps.

## Active Run And Pending Rules
- v1 must serialize active work per `chat_id`; a second `chat.Run` on the same chat while the current turn is active or pending must return a typed precondition/conflict outcome.
- Pending approvals or user-input requests must be represented as Codex-backed waiting states, not invented gateway states.
- Pending response must require `chat_id`, active turn/run correlation, and the pending request identifier.
- Duplicate, stale, mismatched, expired, terminal, unavailable-after-restart, or already-resolved pending responses must not be forwarded as successful approvals.

## Interrupt Rules
- Interrupt requires an explicit interrupt call; closing a stream or cancelling an SDK context only cancels that client request/stream.
- Interrupt targets the active Codex turn/run for `chat_id`.
- If Codex reports no active turn, mismatched active turn, terminal state, or unavailable thread, the gateway returns the corresponding typed outcome.
- Normal interrupt acknowledgment may be delayed until Codex abort handling is observed; the SDK/gateway must not report completed interrupt before the observed or typed terminal outcome supports it.

## Idempotency Rules
- `StartChatRun`, `RunChatTurn`, `RespondChatPending`, and `InterruptChatRun` must accept or generate current-process idempotency keys for retry-safe side-effect handling.
- Idempotency scope is operation, `session_group_id`, `workspace_id`, and `chat_id` where applicable.
- Idempotency memory must not contain prompt text, response text, event content, raw request payload, raw response payload, prompt/content fingerprint, or prompt/content digest.
- Reusing an accepted key with the same safe scope returns the prior safe result reference or typed in-progress/terminal outcome while the gateway process can prove it.
- After gateway restart, idempotency memory is unavailable and the gateway must not claim a previous side effect result unless Codex itself proves it.

## Unsupported And Partial Capability Rules
- Unsupported installed-Codex capabilities must remain visible as typed unsupported/unavailable/unknown/narrowed outcomes.
- The gateway must not fabricate status, history, events, pending requests, or interrupt success from stale process-local data.
- If Codex exposes a narrower history depth than requested, the SDK/gateway must state the depth and provide the supported projection only when safe.
