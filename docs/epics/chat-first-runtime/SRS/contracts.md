# SRS Contracts: Chat-First Runtime

Type: software requirements specification
Status: paper-ready after accepted targeted product/engineering re-reviews; root implementation approval received
Owner: system_analyst
Requested mode: full
Required mode floor: full
Approved mode: full
Lane: High-risk
Last repaired: 2026-06-15
Related docs: `index.md`, `feature.md`, `states-and-outcomes.md`, `transient-correlations.md`, `events-history.md`, `sequences.md`, `nfr-mapping.md`, `traceability.md`, `rollout.md`, `../../../product-docs/sdk/chat-first-go-sdk.md`, `../../../product-docs/grpc/codex-runtime-gateway.md`, `../../../product-docs/domain/chat-runtime.md`, `../../../product-docs/security/local-runtime-boundary.md`, `../../../product-docs/configuration/gateway-runtime-config.md`, `../../../product-docs/observability/event-stream-observability.md`, `../../../product-docs/operations/local-gateway-runbook.md`
Related product-docs: `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001`, `DOC-SEC-001`, `DOC-CONFIG-001`, `DOC-OBS-001`, `DOC-OPS-001`
Trace IDs: SRS-FR-001..SRS-FR-019, SRS-NFR-001..SRS-NFR-009

## Contract Boundary
Target external boundary:

```text
Go SDK -> local gRPC/proto -> gateway adapter -> internal codex.exe app-server JSONL
```

The public contract must be chat-first through a dedicated chat-first gRPC/proto surface. The current `codex.control.v1.CodexControl` service remains a task compatibility surface and must not be reinterpreted as the SDK chat-first API.

## Identity Contract
| Identifier | Required meaning | Visibility | Requirement |
| --- | --- | --- | --- |
| `chat_id` | Codex `Thread.id`, exposed under chat-first naming. | SDK/gRPC/app/safe logs. | Primary identity for app storage, `GetChat`, history, stream, status, pending, and interrupt. |
| `run_id` | Codex turn id where Codex provides one. | SDK/gRPC metadata and diagnostics. | Must not be used as `chat_id`; exactly one active run is allowed per `chat_id` in v1. |
| event cursor / sequence | Current-process stream position and dedupe marker. | SDK/gRPC stream metadata. | Scoped to one `chat_id`; replay may be unavailable after restart. |
| `pending_request_id` | Active Codex pending approval/user-input reference as surfaced by the gateway. | SDK/gRPC pending methods. | Scoped to `chat_id` and active run/turn. |
| idempotency key | Caller-provided or SDK-generated duplicate prevention key for current-process side effects. | SDK/gRPC side-effecting requests. | Not durable across gateway restart in v1; not a `chat_id`. |

The public SDK/gRPC contract must not expose a second public thread identity or require callers to store task-oriented ids as chat identity.

## SDK Contract Requirements
Product-doc source: `../../../product-docs/sdk/chat-first-go-sdk.md`.

- `codex.Run(ctx, prompt)` starts a new chat by proving Codex thread identity and accepted first turn from a non-empty prompt. It returns a chat handle with `chat_id = Thread.id`, typed status, and stream access only after first-turn acceptance/correlation.
- `codex.GetChat(ctx, chatID)` returns a handle for an existing Codex thread id; it must not create a chat, start a run, import Desktop UI history, or expose task-only identifiers as primary identity.
- `chat.GetHistory(ctx)` returns Codex-owned turn summary fields/projection where Codex supports it, or typed unsupported/unavailable/unknown/narrowed outcomes.
- `chat.Run(ctx, prompt)` starts a new Codex-backed turn in the chat only when v1 has no active turn for that `chat_id`.
- `chat.GetEventsStream(ctx)` returns live current events by `chat_id`; replay is returned only from current-process buffers where supported/proven.
- Status, pending-response, and interrupt helpers must be first-class typed chat operations. Raw JSONL and task-first-only semantics are not allowed.

## gRPC / Proto Contract Requirements
Product-doc source: `../../../product-docs/grpc/codex-runtime-gateway.md`.

Current implementation context: the existing gateway has task-oriented `codex.control.v1.CodexControl` methods. That service is compatibility/current-surface context only for this epic.

Target contract, to be finalized during proto/architecture review, must be a versioned chat-first gRPC/proto surface separate from the current task compatibility service. Exact file paths, package names, and generated package layout are not decided by this SRS.

The chat-first service must cover:
- `StartChatRun`: start a new chat with a non-empty first prompt using installed-Codex thread plus first-turn capability;
- `GetChat`: resolve an existing `chat_id` as a Codex Thread id without creating a chat or importing Desktop UI history;
- `RunChatTurn`: run a new non-empty turn in an existing chat when no active run exists;
- `GetChatStatus`: return typed Codex thread lifecycle, run, pending, history-depth, and replay-capability status;
- `GetChatHistory`: return Codex-owned turn summary fields/projection where supported;
- `StreamChatEvents`: stream normalized current live events and in-memory replay when available in the current process;
- `RespondChatPending`: respond to an active pending request after authz and correlation checks;
- `InterruptChatRun`: interrupt the active Codex turn/run.

The chat-first proto must not make SDK callers infer chat behavior from task-only naming and must not include raw app-server JSONL payload escape hatches.

## Error Contract
| Condition | Required external error class |
| --- | --- |
| Empty/invalid prompt, malformed IDs, malformed cursor, invalid idempotency key, invalid request shape | `INVALID_ARGUMENT` |
| Missing, duplicate, malformed, or wrong bearer metadata | `UNAUTHENTICATED` |
| Session/workspace selector is syntactically invalid or not configured | `INVALID_ARGUMENT` |
| Request scope is not authorized for the selected local session/workspace | `PERMISSION_DENIED` without revealing external state |
| Codex thread id is unknown or not found within the authorized local context | `NOT_FOUND` |
| Codex app-server, thread state, or history source unavailable | `UNAVAILABLE` |
| Active run already exists; no active run exists for interrupt; pending mismatch; terminal run cannot be modified | `FAILED_PRECONDITION` |
| Replay cursor is valid shape but belongs to another chat/run or is older than current-process range | `OUT_OF_RANGE` |
| Replay is unsupported, evicted, unavailable after restart, or narrowed to live-only | `UNAVAILABLE` with typed replay detail or success with explicit `narrowed_to_live` metadata |
| Idempotency key reuse conflicts with operation or safe scope in the current process | `ABORTED` or typed idempotency conflict |
| Prior side effect cannot be proven for a tracked same-process retry | `UNKNOWN` or `UNAVAILABLE`; gateway must not duplicate the Codex call under that current-process idempotency record |
| Retry after gateway restart supplies only a pre-restart idempotency key | No idempotent replay result; the key is unrecognized as a prior request, and normal operation-specific validation/preconditions apply |
| Request/stream deadline exceeded | `DEADLINE_EXCEEDED` |
| Client cancelled stream/request | `CANCELLED`; cancellation is not interrupt |
| Installed Codex lacks requested capability, such as item-level history | `UNIMPLEMENTED` or typed unsupported detail |
| Internal JSONL adapter cannot safely translate a required event | `INTERNAL` with redacted adapter detail |

Error messages and details must not include raw tokens, auth headers, private keys, passwords, cookies, raw env values, raw JSONL, or unredacted private data.

## Idempotency / Restart Contract
- Duplicate suppression is guaranteed only for side-effecting requests tracked by the current gateway process.
- Gateway restart loses idempotency memory; pre-restart idempotency keys must not be treated as proof of a prior request, prior result, in-progress operation, terminal state, or no-duplicate guarantee.
- After restart, callers may reuse an app-stored `chat_id` only as a Codex `Thread.id`; the gateway may report recovered state only when Codex proves that thread/status/history state.
- If the caller has no usable `chat_id` and Codex cannot prove the prior side effect, the gateway must not pretend idempotent replay, exactly-once delivery, or cross-restart duplicate suppression.

## Event / Stream Contract
- Events must be normalized gateway events, not raw JSONL.
- Event order must be stable per `chat_id` and run/turn as far as Codex/gateway observation supports it.
- A cursor must be scoped to a chat, run, and gateway process epoch; after restart/epoch mismatch it fails safely or narrows to live.
- Stream cancellation must close the client stream only; it must not interrupt the active run.

## History Contract
- History is a normalized projection of Codex-owned turn summary fields suitable for app chat display where installed Codex supports it.
- History must not expose raw internal JSONL as the public shape.
- If Codex history is unsupported, unmaterialized, ephemeral, unavailable, item-level-only requested, or narrower than requested, response must return typed unsupported/unavailable/unknown/narrowed behavior.

## Security / Privacy Implications
- All calls require exactly one configured local bearer token and fail with `UNAUTHENTICATED` for missing, duplicate, malformed, or wrong metadata.
- The first target is trusted local clients only; remote, team-shared, production, or multi-tenant exposure is out of scope.
- Every request must authenticate first, validate request/session/workspace shape next, and authorize the session/workspace before any Codex call, pending response, interrupt, stream attach, or other side effect.
- Authorized SDK/gRPC callers may receive transient normalized projections of Codex-owned history/events/pending display data where Codex supports them and after auth/authz; the gateway must not retain, export, log, document in examples, hash, or dump that content.

## Configuration / Secret Implications
Configuration must include or explicitly defer:
- installed `codex.exe` path;
- loopback listen address;
- local client auth token source without values, limited in v1 to exactly one configured env name or absolute file path;
- session/workspace routing;
- `chat_runtime.enabled` independent disable path;
- in-memory replay limits;
- pending request limits/expiration;
- message size and stream buffer/backpressure limits;
- app-server startup, gRPC health/readiness, and supervisor backoff limits;
- runtime config reload is not promised in v1.

No non-secret environment variable override exists in v1. Secret-bearing values must be referenced by source/purpose only, never documented as raw values.

## Compatibility / Versioning Constraints
- SDK/gRPC breaking changes require BRS, SRS, product-doc, test, and migration guidance updates.
- The gateway should absorb internal JSONL changes when safe.
- If internal JSONL changes remove required capability, SRS/product acceptance must be reopened; do not fake capability in SDK.
- Existing task RPC behavior must not be silently broken by introducing chat-first behavior unless explicitly approved.

## Trace Links
| Contract area | Product-doc refs | SRS refs | Test refs |
| --- | --- | --- | --- |
| SDK | `DOC-SDK-001` | `SRS-FR-001`..`SRS-FR-011`, `SRS-FR-016`, `SRS-FR-018`, `SRS-FR-019` | SDK contract, status, history, stream, pending, interrupt tests |
| gRPC/proto | `DOC-GRPC-001` | `SRS-FR-001`..`SRS-FR-014`, `SRS-FR-018`, `SRS-FR-019` | proto contract and adapter tests |
| Domain | `DOC-DOMAIN-001` | `SRS-FR-001`..`SRS-FR-012`, `SRS-FR-016`, `SRS-FR-018` | identity/status/turn lifecycle tests |
| Security/config | `DOC-SEC-001`, `DOC-CONFIG-001` | `SRS-FR-009`, `SRS-FR-010`, `SRS-FR-013`, `SRS-NFR-002` | auth/redaction/config tests |
| Observability/ops | `DOC-OBS-001`, `DOC-OPS-001` | `SRS-FR-006`, `SRS-FR-008`, `SRS-FR-011`, `SRS-FR-017`, `SRS-FR-019`, `SRS-NFR-004`, `SRS-NFR-007` | diagnostics/restart/release-gate tests |

## Forbidden Content
- Architecture decisions owned by tech design.
- Release notes.
- Raw secrets, auth headers, tokens, credentials, cookies, passwords, private keys, raw JSONL, or private data.
