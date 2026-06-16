# SRS: Transient Gateway Correlations

Type: software requirements specification
Status: paper-ready after accepted targeted product/engineering re-reviews; root implementation approval received
Owner: system_analyst
Last repaired: 2026-06-14
Related docs: `index.md`, `feature.md`, `contracts.md`, `states-and-outcomes.md`, `events-history.md`, `sequences.md`, `rollout.md`
Trace IDs: SRS-FR-018, SRS-FR-019, SRS-NFR-003, SRS-NFR-007, SRS-NFR-009

## Source Of Truth
This file replaces the removed v1 durable gateway identity-store requirement. The gateway may keep only volatile process-local metadata needed to operate current requests around Codex.

## Identity Rule
- Public `chat_id` is Codex `Thread.id`.
- Public `run_id` is the Codex turn id exposed under run naming where Codex provides it.
- There is no separate durable gateway chat id, no durable identity translation table, and no local database requirement in v1.
- `GetChat` receives `chat_id` and treats it as a Codex thread id after auth, request validation, and session/workspace validation.

## Allowed Process-Local State
The gateway may keep these records in memory for the life of the gateway process:

| State | Required use | Lost on restart |
| --- | --- | --- |
| active run correlation | connect a current `StartChatRun`/`RunChatTurn` request to the accepted Codex turn | yes |
| stream cursor/replay buffer | serve current-process reconnects from normalized events | yes |
| pending request correlation | route a current pending response to the active Codex turn | yes |
| idempotency reservation | avoid duplicate side effects in the current process | yes |
| diagnostics correlation | connect safe request IDs, event IDs, and redacted reason codes | yes |

No process-local state may contain raw secrets, auth headers, token values, raw JSONL dumps, prompt/message/event/history payloads intended for retention, prompt/response hashes, or private data dumps.

## StartChatRun Boundary
- `StartChatRun` validates auth, request shape, session/workspace, and non-empty prompt before any Codex call.
- The gateway uses installed-Codex thread creation/loading plus first-turn submission capability. Current Codex evidence maps this to `thread/start` followed by `turn/start`.
- A successful response returns `chat_id = thread.id` and `run_id = turn.id` only after the first Codex turn is accepted/correlated.
- If thread creation/loading succeeds but first-turn submission is not accepted, the gateway returns a typed failure/unknown/unavailable outcome and must not promise an empty chat.
- If delivery is ambiguous, the gateway must not duplicate a side-effecting Codex call under the same current-process idempotency key.

## Restart Semantics
- Gateway restart clears process-local active run, replay, pending, idempotency, and diagnostics correlation.
- After restart, the application may call `GetChat(chat_id)` with the stored Codex thread id.
- The gateway asks Codex for the thread/status/history where supported. If Codex cannot prove the state, return typed not-found/unavailable/unknown/narrowed behavior.
- Replay from a pre-restart cursor is unavailable or narrowed to live/current status/history behavior.
- Pending responses from before restart are accepted only if Codex and the new gateway process can prove an active pending request correlation; otherwise return typed unavailable/unknown/precondition.

## Acceptance / Test Basis
QA must cover:
- `chat_id` equals Codex `Thread.id` and no separate durable gateway id exists.
- successful start returns ids only after first turn acceptance/correlation;
- no empty chat promise after `thread/start` without first turn acceptance;
- history before materialization returns typed unavailable/unmaterialized behavior;
- replay and current-process idempotency are lost after gateway restart;
- pending/interrupt behavior after restart is typed rather than fabricated.

## Forbidden Content
- Durable gateway-owned identity translation or local database requirements for v1.
- Raw JSONL, raw secrets, raw auth material, prompt/message/event/history retention, prompt/response hashes, or private data dumps.
