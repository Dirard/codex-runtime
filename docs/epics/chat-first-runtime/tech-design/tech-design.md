# Tech Design: Chat-First Runtime

Type: technical design
Status: paper architecture gate ready for downstream owner review
Owner: architect
Requested mode: full
Required mode floor: full
Approved mode: full
Lane: High-risk
Last repaired: 2026-06-14
Related docs: `../BRS/feature.md`, `../BRS/nfr.md`, `../SRS/index.md`, `../SRS/feature.md`, `../SRS/contracts.md`, `../SRS/states-and-outcomes.md`, `../SRS/transient-correlations.md`, `../SRS/events-history.md`, `../SRS/sequences.md`, `../SRS/nfr-mapping.md`, `../SRS/rollout.md`, `../reviews/product-review.md`, `../../../product-docs/sdk/chat-first-go-sdk.md`, `../../../product-docs/grpc/codex-runtime-gateway.md`, `../../../product-docs/domain/chat-runtime.md`, `../../../product-docs/configuration/gateway-runtime-config.md`, `../../../product-docs/security/local-runtime-boundary.md`, `../../../product-docs/observability/event-stream-observability.md`, `../../../product-docs/operations/local-gateway-runbook.md`, `adr-001-dedicated-chat-first-service.md`
Trace IDs: BRS-REQ-001..BRS-REQ-007, BRS-RULE-001..BRS-RULE-014, BRS-NFR-001..BRS-NFR-009, SRS-FR-001..SRS-FR-019, SRS-NFR-001..SRS-NFR-009

## Source Of Truth
This document records the architecture boundary and contracts needed to satisfy the accepted BRS and product-accepted SRS for chat-first runtime v1.

This is a paper architecture gate only. It does not implement proto, generated code, gateway code, SDK code, tests, scripts, release artifacts, or current-behavior documentation.

## Product Baseline Preserved
Target runtime chain:

```text
web app -> API handler with Go SDK -> local gateway -> installed codex.exe app-server
```

The web app and API handler own application UX, user workflows, and durable app-side storage of `chat_id`. The Go SDK owns the application-facing chat API convenience. The local gateway owns stable local gRPC translation, validation/authz, Codex app-server supervision, typed normalization, and process-local operational correlation. Installed Codex owns chat identity, real chat behavior, prompt/message/event/history content, pending semantics, interrupt behavior, and thread/turn truth.

The gateway is a local gRPC compatibility layer over installed Codex. It is not a second Codex runtime, not a history database, not a Desktop UI synchronizer, and not the owner of chat behavior.

## Hard Scope Boundaries
In scope for architecture:
- dedicated chat-first service boundary for SDK/gRPC callers;
- compatibility coexistence with the current task-oriented surface;
- identity, transient state, restart, auth, validation, redaction, config, readiness, supervision, retry, backoff, and observability constraints;
- typed adaptation of Codex-backed status, history, events, pending, and interrupt behavior.

Out of scope:
- implementation, proto regeneration, SDK code, gateway code, tests, scripts, release execution, or current-state claims;
- remote gateway exposure, hosted operation, team-shared service, or multi-tenant isolation;
- gateway-created durable chat identity;
- SQLite, local database, durable mapping table, or durable gateway-owned content/history store;
- gateway-owned prompt/message/event/history retention;
- raw app-server JSONL as public SDK/gRPC contract, logs, examples, diagnostics, or QA evidence;
- empty chat creation as a successful SDK-created chat;
- item-level history promise in v1;
- Desktop UI visible/current-thread synchronization promise.

## Fact Anchors
- Accepted BRS states that `chat_id` is Codex `Thread.id` in v1 and no durable gateway identity store exists.
- Product review passed the SRS with no findings and preserved local-only, Codex-owned behavior/history, and process-local gateway state.
- SRS requires a chat-first SDK/gRPC contract separate from the task compatibility surface.
- SRS requires `StartChatRun` success only after Codex thread identity and first-turn acceptance/correlation are proven.
- SRS states current Codex evidence: `thread/start` creates an idle thread/listener; `turn/start` submits user input and returns an in-progress turn.
- SRS states `thread/turns/items/list` currently returns method-not-found, so item-level history is not promised.
- SRS requires auth, request validation, and session/workspace authorization before any Codex side effect.
- SRS requires replay, pending, idempotency, active-run, and diagnostics correlation to be process-local and lost on gateway restart.

## Architecture Decisions
| ID | Decision | Contract impact | Downstream owner review |
| --- | --- | --- | --- |
| TD-01 | Introduce a dedicated chat-first local gRPC service for the Go SDK. | SDK/gRPC callers use chat names and `chat_id`, not task/run identity as primary identity. | engineering, QA |
| TD-02 | Keep the existing task-oriented service as a compatibility surface. | Chat-first work must not silently redefine task RPC behavior. | engineering, QA, release/ops |
| TD-03 | In v1, `chat_id == Codex Thread.id`. | No gateway-minted durable chat id or durable mapping store. | product, QA |
| TD-04 | `run_id == Codex Turn.id` where Codex provides one. | `run_id` is not a chat id and not durable chat identity. | product, QA |
| TD-05 | Gateway state is process-local only. | Active run, replay cursor/buffer, pending, idempotency, and diagnostics are lost on restart. | security/privacy/data, release/ops, QA |
| TD-06 | Auth, validation, and session/workspace authorization precede every Codex side effect. | Deny invalid/unauthenticated/unauthorized calls before `thread/start`, `turn/start`, pending response, interrupt, stream attach, or history read. | security/privacy/data, QA |
| TD-07 | Codex-backed capabilities are normalized into typed outcomes. | Unsupported, unavailable, unknown, narrowed, stale, duplicate, terminal, out-of-range, and conflict cases are visible, not hidden as success. | product, QA |
| TD-08 | No raw JSONL or private content in public API, logs, docs, diagnostics, tests, or examples. | The adapter may consume internal JSONL, but the stable service never exposes it. | security/privacy/data, QA |
| TD-09 | One lazy installed `codex.exe app-server` supervisor per configured session group. | No per-chat child process; readiness does not require lazy child already running. | release/ops, engineering |
| TD-10 | `chat_runtime.enabled` is the independent disable boundary. | Chat service can be disabled while task RPCs remain healthy when their dependencies are healthy. | release/ops, QA |

## Service Boundary And Dependency Direction
The architecture separates three surfaces:

| Surface | Owner | Purpose | Must not do |
| --- | --- | --- | --- |
| Go SDK chat API | SDK package | Provide `codex.Run`, `codex.GetChat`, chat history, run, stream, status, pending, and interrupt helpers. | Parse raw JSONL, invent gateway-owned chat identity, require task IDs as app identity. |
| Chat-first gRPC service | gateway public contract | Stable local contract for chat-first behavior and typed outcomes. | Expose raw JSONL, raw payload escape hatch, task identity ambiguity, or durable gateway-owned history. |
| Task compatibility service | existing gateway compatibility surface | Preserve existing task-oriented clients and rollback/coexistence path. | Become the primary chat-first SDK target or silently reinterpret task IDs as `chat_id`. |

Dependency direction:
- SDK depends on the chat-first gRPC contract.
- Chat-first service depends on gateway domain/adapters and installed Codex behavior.
- Gateway adapter depends inward on internal Codex app-server JSONL details.
- Codex behavior, identity, and history do not depend on gateway state.
- Task compatibility DTOs must not leak into chat-first DTOs as primary identity.

Exact proto field layout and generated code remain downstream implementation artifacts, but the architecture contract is fixed: the chat-first service is separate, versioned, and named around chat operations, not tasks.

Required chat-first operations:

| Operation | Architecture meaning |
| --- | --- |
| `StartChatRun` | Validate/authz, create/load Codex thread, submit first turn, and return `chat_id = Thread.id` only after first-turn acceptance/correlation. |
| `GetChat` | Treat `chat_id` as Codex Thread id and resolve status/read data where Codex supports it; do not create a chat. |
| `RunChatTurn` | Submit a non-empty turn to the existing Codex thread when no active run exists for that `chat_id`. |
| `GetChatStatus` | Return typed Codex thread lifecycle, current/last run lifecycle, pending state, history depth, replay capability, and gateway-local availability. |
| `GetChatHistory` | Return Codex-owned turn summary/projection where supported; item-level history is unsupported in v1 unless later Codex evidence changes SRS/product acceptance. |
| `StreamChatEvents` | Stream normalized live events and current-process replay where available; never expose raw JSONL. |
| `RespondChatPending` | Forward response only when active pending correlation is proven for `chat_id`, `run_id`, and `pending_request_id`. |
| `InterruptChatRun` | Explicitly interrupt the active Codex turn/run; stream cancellation is not interrupt. |

## Identity Contract
| Identifier | Owner | Visibility | Lifetime | Architecture rule |
| --- | --- | --- | --- | --- |
| `chat_id` | Codex | SDK/gRPC/app, safe logs | Codex thread lifetime | Exactly Codex `Thread.id`; primary app-stored chat identity. |
| `run_id` | Codex | SDK/gRPC/app, safe logs | Codex turn lifetime | Equals Codex turn id where provided; never substituted for `chat_id`. |
| task compatibility ID | existing task surface | task RPC callers only | task surface lifetime | Not accepted as `chat_id` and not promoted into chat-first identity. |
| event cursor/sequence | gateway | stream/replay metadata | current gateway process epoch | Scoped to `chat_id`, `run_id`, and gateway epoch. |
| `pending_request_id` | gateway over Codex pending signal | status/stream/pending response | current process unless Codex proves pending again | Scoped to active `chat_id` and `run_id`. |
| idempotency key | caller or SDK | side-effecting calls | current gateway process | Prevents duplicate side effects only while process-local state can prove prior handling. |

`chat_id` validation must use the Codex thread id shape accepted by the adapter. It must not encode prompts, private content, raw workspace paths, usernames, task IDs, event cursors, pending IDs, or idempotency keys.

## Runtime State And Restart Semantics
Allowed process-local state:
- active run/turn correlation;
- normalized event stream buffer and cursor ownership;
- pending request correlation;
- side-effect idempotency reservations/results that contain only safe references;
- diagnostics correlation using safe request IDs, state classes, and reason codes.

Forbidden gateway retention:
- SQLite or any durable mapping/content store in v1;
- prompt text, assistant text, command output, event payloads, history items, raw JSONL, raw request/response payloads, prompt/content hashes, auth headers, token values, cookies, passwords, private keys, raw environment values, or private data dumps.

Restart semantics:
- gateway restart clears active run, replay, pending, idempotency, and diagnostics correlation;
- app callers may reuse stored `chat_id` only as Codex `Thread.id`;
- post-restart `GetChat`, status, history, continuation, pending, and interrupt are allowed only where Codex can prove the state again;
- pre-restart replay cursors are unavailable/out-of-range/narrowed because the gateway epoch changed;
- previous pending or idempotency state must not be reported as known unless Codex or current process evidence proves it;
- ambiguous prior side effects return typed `unknown` or `unavailable`, and the gateway must not duplicate Codex calls under the same current-process idempotency key.

## Codex Adaptation Contracts
| Area | Codex-backed source | Gateway adaptation | Required typed outcomes |
| --- | --- | --- | --- |
| Start chat | `thread/start` plus `turn/start` evidence | Return success only after thread id and first turn acceptance/correlation. | invalid prompt, unauthenticated, permission denied, unavailable, unknown, first-turn failure; no empty-chat success. |
| Existing chat turn | Codex continuation for `Thread.id` | Enforce one active run per `chat_id` and submit to same thread. | active-run conflict, not found, unavailable, unknown, continuation unsupported. |
| Status | Codex thread status, turn status, gateway-local live state | Normalize thread lifecycle, current/last turn, pending, history depth, replay capability. | invalid, not-found, unavailable, unknown, unsupported, narrowed. |
| History | `thread/read includeTurns` or `thread/turns/list` where supported | Return turn summary/projection only. | unsupported item-level history, unmaterialized, unavailable, unknown, narrowed. |
| Events | Codex observed notifications plus in-memory normalized replay | Stream typed events and safe warnings; replay only within current process limits. | replay unavailable, out-of-range, narrowed-to-live, cancelled, deadline, adapter internal after redaction. |
| Pending | Codex active waiting state or pending notification | Route response only with proven active pending correlation. | stale, duplicate, mismatched, expired, terminal, unavailable after restart, unknown. |
| Interrupt | Codex active turn interrupt support | Send explicit interrupt for active turn/run only. | no active turn, already terminal, already interrupting, not found, unavailable, unknown. |

The gateway must prefer typed unsupported/unavailable/unknown/narrowed outcomes over fabricated status, history, events, pending, or interrupt success.

## Auth, Validation, And Local Boundary
Security ordering for every unary and streaming RPC:

1. Authenticate exactly one local bearer token from the configured token source.
2. Strip authorization metadata before handlers, logs, adapters, diagnostics, and downstream context.
3. Validate request shape, IDs, cursor, prompt, idempotency key, session group, workspace, deadlines, and size limits.
4. Authorize requested local session/workspace.
5. Only then call Codex, attach a stream, forward pending response, interrupt, or read Codex-backed state.

V1 is local-only:
- listen addresses must remain loopback/local unless a new BRS/SRS/security/release decision changes scope;
- trusted local clients only, no remote/team-shared/multi-tenant access;
- request scope must not reveal other workspace/session state through errors;
- auth failures are generic and safe;
- pending responses must not broaden Codex permissions or auto-approve outside Codex semantics.

## Configuration And Readiness Constraints
Configuration is validated before chat service readiness:

| Concern | Architecture constraint |
| --- | --- |
| `codex_binary` | Required installed `codex.exe` path and identity/probe check. |
| `listen` | Loopback/local only; process flag override must pass the same validation. |
| `client_auth_token_source` | Exactly one env name or absolute file source; value is never logged, dumped, or documented. |
| `session_groups` | Canonical `cwd` and `CODEX_HOME` routing for local workspaces; strict validation. |
| `chat_runtime.enabled` | Independent chat service disable path. |
| replay/pending/message/status limits | Validated defaults and hard caps match SRS/product-docs. |
| app-server startup/readiness/supervisor backoff | Local bounded values; caller deadline wins if shorter where applicable. |

V1 precedence remains:

1. Built-in defaults.
2. TOML.
3. Documented process flags; current approved runtime override is `--listen`, while `--config` locates config.
4. Runtime secret resolution from the configured source only.

No non-secret environment override and no runtime config reload are promised in v1. Config changes require gateway restart.

Readiness:
- gateway process readiness requires valid config, token source, loopback listen, session groups, and Codex binary identity;
- `ChatRuntimeService` readiness is serving only when enabled and core local dependencies/config are valid;
- lazy app-server children do not need to be running before readiness, but startup failure must become typed unavailable and observable;
- when `chat_runtime.enabled=false`, chat service readiness is `NOT_SERVING` or equivalent with safe reason `chat_runtime_disabled`, while task RPC readiness may remain serving if healthy.

## Process Supervision, Retry, And Backoff
Supervisor:
- one lazy installed `codex.exe app-server` child process per session group;
- launched only after authorized work needs Codex;
- launched with the session group's canonical `cwd` and `CODEX_HOME`;
- shared by chat-first and task compatibility surfaces for that session group where appropriate;
- no per-chat app-server process in v1.

Retry and idempotency:
- `StartChatRun`, `RunChatTurn`, `RespondChatPending`, and `InterruptChatRun` are current-process idempotency-aware;
- idempotency records contain only safe scope and result references, never prompt/response/event/history content or content hashes;
- same safe key/scope may return a proven prior safe result reference, in-progress state, terminal state, or typed conflict;
- after restart, idempotency is unavailable unless Codex state proves the result;
- unknown delivery must not trigger duplicate Codex side effects.

Backoff:
- repeated non-cancel/non-deadline app-server failures enter a session-group-scoped cooldown according to SRS bounds;
- backoff state is process-local and safe to log by class/reason only;
- backoff must not mutate Codex-owned thread/history state.

## Observability And Operations Constraints
Required safe signals:
- gateway startup/shutdown and chat runtime enable/disable state;
- auth failure class without token values;
- session-group app-server lifecycle and restart backoff;
- active chat/run counts, stream subscriber counts, pending counts, replay misses, app-server failures, and correlation misses;
- pending lifecycle, interrupt lifecycle, terminal run state, and redacted adapter warnings;
- health/readiness for gateway and chat service state.

Redaction:
- logs, errors, diagnostics, docs, examples, test fixtures, and QA evidence must not include raw secrets, auth headers, tokens, cookies, passwords, private keys, raw environment values, raw JSONL, prompt text, assistant text, command output, event payloads, history content, prompt/response hashes, or private data dumps;
- diagnostics may include safe IDs such as `chat_id`, `run_id`, request ID, pending ID, event sequence/cursor ID, session/workspace IDs, state classes, and reason codes.

Ops stance:
- this design defines local readiness and troubleshooting constraints only;
- it does not create release notes, production SLO/SLA, customer support commitments, or current-state acceptance;
- any remote, production, team-shared, multi-user, or multi-tenant operation reopens BRS, SRS, security/privacy/data, release/ops, QA, and current-state review.

## Disable, Rollback, And Compatibility
- `chat_runtime.enabled=false` disables or omits the chat-first service, or makes chat methods return `UNIMPLEMENTED`/typed `chat_runtime_disabled`.
- Disable must not delete, mutate, or repair Codex-owned thread/history state.
- Existing task RPC behavior remains unchanged unless a separate approved task-RPC migration exists.
- Rollback must not expose task identity as `chat_id`.
- Chat service failure must not corrupt task compatibility state.
- Task RPC failure must not corrupt chat process-local state except where the shared app-server dependency is unavailable.

## Architecture Check Implications
Downstream reviews/checks must verify:
- public chat contract is separate from task compatibility and has no raw JSONL escape hatch;
- `chat_id` equality to Codex `Thread.id` and absence of gateway-minted durable chat IDs, SQLite, mapping DB, or retained content/history store;
- validation/auth/authz occurs before every Codex side effect;
- restart loses process-local replay/pending/idempotency/active-run state and returns typed outcomes;
- status/history/events/pending/interrupt behavior is Codex-backed and typed for unsupported, unavailable, unknown, narrowed, stale, duplicate, terminal, and conflict cases;
- item-level history and Desktop UI visibility are not promised;
- config, disable path, readiness, supervisor, retry/backoff, observability, and ops docs stay architectural constraints, not release execution facts;
- redaction rules are enforced in docs, tests, examples, logs, diagnostics, and QA evidence.

## Stop If
- Any design makes `chat_id` differ from Codex `Thread.id` in v1.
- A gateway-created durable chat id, SQLite/local DB, durable mapping store, or gateway-owned history/content retention is introduced.
- Task/run ID, cursor, pending ID, or idempotency key becomes the primary application chat identity.
- Raw app-server JSONL becomes public API, log output, diagnostics, test oracle, docs example, or QA evidence.
- Auth, request validation, or session/workspace authorization cannot be completed before Codex side effects.
- Implementation would fabricate Codex status, history, events, pending, interrupt, or restart continuity.
- SDK-created chats are promised as current or visible in Desktop UI.
- Remote/team-shared/multi-tenant/release/current-state scope enters without reopening the owning gates.

## Reviews Required After This Gate
- `security_privacy_data_owner`: local-only auth/authz, secret handling, data minimization, redaction, no content retention, and pending/idempotency safety.
- `release_ops_owner`: config precedence, disable/readiness, supervisor, retry/backoff, local ops constraints, and inactive release stance.
- `qa_engineer`: contract, identity, authz preflight, restart/replay, pending/interrupt, status/history, redaction, disable, and compatibility coverage.
- `engineering_reviewer`: dependency direction, service boundary, adapter isolation, DTO separation, and maintainability once implementation artifacts exist.
- `product_owner`: only if downstream work attempts to alter product-visible behavior, scope, identity, history depth, Desktop UI visibility, local-only boundary, or release/current-state stance.
