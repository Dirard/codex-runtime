# Security / Privacy / Data Review: Chat-First Runtime

status: complete
role: security_privacy_data_owner
mode: full
lane: High-risk
date: 2026-06-14
decision: pass

## security_privacy_data_verdict

Decision: pass.

The paper solution is acceptable for the architecture paper gate from a security, privacy, and data-governance perspective. The accepted BRS, product-reviewed SRS, technical design, ADR, and draft target product-docs consistently preserve the local-only v1 boundary, exact local bearer authentication, session/workspace authorization before Codex side effects, no raw JSONL public exposure, no gateway-owned content retention, and process-local-only correlation state.

This pass is limited to paper readiness. It is not implementation readiness, release readiness, production approval, remote/team-shared/multi-tenant approval, or approval for durable gateway storage/content retention.

## touched_surfaces

- BRS: `docs/epics/chat-first-runtime/BRS/feature.md`, `docs/epics/chat-first-runtime/BRS/nfr.md`.
- SRS bundle: `docs/epics/chat-first-runtime/SRS/**`.
- Architecture: `docs/epics/chat-first-runtime/tech-design/tech-design.md`, `docs/epics/chat-first-runtime/tech-design/adr-001-dedicated-chat-first-service.md`.
- Draft target product-docs: security boundary, gateway runtime config, gRPC gateway contract, event-stream observability, and local gateway runbook.

## data_classes_and_boundaries

- `chat_id`: public application-facing identity; in v1 it is exactly Codex `Thread.id`.
- `run_id`: Codex turn identity where provided; it is not a chat identity.
- Session/workspace selectors: authorization scope and potential private local-path metadata; must be validated before Codex access.
- Auth material: local bearer token value, auth metadata/header, token source value, cookies, passwords, private keys, and raw environment values are secret/sensitive and must never be logged, documented, dumped, used in examples, or retained in diagnostics.
- Prompt/message/event/history/pending display content: Codex-owned private content; gateway may transiently normalize and deliver it only to authorized SDK/gRPC callers where Codex supports it, but must not retain, hash, log, export, document, or dump it.
- Gateway state: active run, replay buffer/cursor, pending correlation, idempotency, and diagnostics are process-local only and lost on restart.
- Out of scope: remote exposure, hosted operation, team-shared use, multi-tenant isolation, production release, Desktop UI synchronization, SQLite/local DB, durable mapping store, and durable gateway-owned content/history storage.

## required_controls

- Listen/config must enforce loopback/local-only v1. Any remote, team-shared, production, multi-user, or multi-tenant use reopens BRS/SRS/security/privacy/data/release/QA decisions.
- Every unary and streaming chat RPC must authenticate exactly one configured local bearer token, strip authorization metadata, validate request/session/workspace shape, and authorize the session/workspace before any Codex call, stream attach, pending response, interrupt, or other side effect.
- Auth failures must be generic and safe. Unauthorized scope must return `PERMISSION_DENIED` without revealing external workspace/session/thread state.
- Token source resolution must be limited to exactly one configured env name or absolute file path. External credential-provider command execution and non-secret environment overrides are out of v1.
- Public SDK/gRPC/docs/tests/logs/diagnostics must expose normalized typed outcomes only, never raw app-server JSONL or raw payload escape hatches.
- Gateway state and idempotency records must contain only safe scope/result references; no prompt text, response text, event content, history content, raw request/response payloads, prompt/content fingerprints, hashes, digests, or auth material.
- Pending and interrupt flows must reject stale, duplicate, mismatched, expired, terminal, already-interrupting, unknown, and unavailable-after-restart cases with typed outcomes before forwarding to Codex.
- Restart behavior must explicitly lose process-local replay, pending, active-run, idempotency, and diagnostics correlation; post-restart behavior may rely only on Codex-proven thread/status/history facts.
- Draft target product-docs must remain clearly marked as future target behavior, not current behavior or release evidence.

## forbidden_shortcuts

- Do not bind the gateway to non-loopback interfaces or document remote/team-shared/multi-tenant access as v1 behavior.
- Do not weaken bearer auth, request validation, or session/workspace authorization because the service is local.
- Do not call Codex before auth/authz/validation preflight on any chat operation.
- Do not log, test, document, summarize, or expose raw token values, auth headers, cookies, passwords, private keys, raw environment values, raw JSONL, raw request/response payloads, private prompts, message/event/history content, command output, private data dumps, prompt/content hashes, or prompt/content digests.
- Do not add SQLite, a local database, durable identity mapping, durable replay, durable pending/idempotency memory, or gateway-owned content/history retention in v1.
- Do not use task IDs, run IDs, cursors, pending IDs, or idempotency keys as `chat_id`.
- Do not fabricate success, status, history, replay, pending, interrupt, or restart continuity when Codex/current-process evidence cannot prove it.
- Do not turn target/future product-docs into current-behavior, release, production, or customer-support claims.

## evidence

- Local-only and out-of-scope remote/multi-tenant boundary: BRS feature lines 50-52 and 96; BRS NFR lines 22-26; SRS index lines 62-68; SRS rollout lines 78-87; tech design lines 157-162 and 225-228.
- Auth/authz ordering before side effects: SRS contracts lines 96-100; SRS states/outcomes lines 23-33 and 35-47; SRS sequences lines 22-36, 38-48, 50-59, 61-68, 70-79, 81-90, and 92-102; tech design lines 148-155.
- Token/config handling: SRS contracts lines 102-115; SRS rollout lines 52-60; product config lines 15-33 and 53-58; product security lines 15-23 and 45-48; tech design lines 164-184.
- Session/workspace authorization and no cross-workspace leakage: SRS contracts lines 68-71 and 97-100; SRS states/outcomes lines 27-33, 40-47, 58-61, and 87-92; tech design lines 148-162 and 169-175.
- No raw JSONL public exposure: BRS feature lines 78 and 119-130; SRS contracts lines 61-63, 83-94, and 117-120; SRS events/history lines 23-40; product gRPC lines 45, 61, 86-90; ADR lines 88-93.
- No prompt/message/event/history/content retention and no content hashes: BRS NFR lines 24-26 and 47-50; SRS events/history lines 10-12 and 33-47; SRS states/outcomes lines 87-92; SRS transient correlations lines 19-31 and 55-58; tech design lines 115-133 and 200-205.
- Pending/interrupt safety: BRS feature lines 80-83 and 155-160; SRS feature lines 37-39; SRS states/outcomes lines 75-85; SRS sequences lines 81-102; tech design lines 135-146.
- Process-local state and restart loss: BRS feature lines 73-78 and 146-153; SRS transient correlations lines 13-31 and 39-45; SRS rollout lines 62-67; tech design lines 115-133.
- No SQLite/durable mapping/content store: BRS feature lines 50-53 and 146-149; SRS index lines 44-54 and 62-66; SRS transient correlations lines 13-31 and 55-58; tech design lines 123-125 and 249-253; ADR lines 61-74.
- Target/future docs not current behavior: SRS index lines 10-18 and 74-78; SRS traceability lines 69-78; product security lines 12-14; product config lines 12-14; product gRPC lines 13-15; product observability lines 12-14; product operations lines 12-14.

Search evidence: targeted redaction-aware searches across `docs/epics/chat-first-runtime/**/*.md` and the requested `docs/product-docs/**` surfaces found security-sensitive terms used as controls, prohibitions, or future-target labels, not as raw secret values, raw auth header examples, raw JSONL examples, or current-behavior/release claims.

## blockers

None for the paper architecture gate.

## residual_risks

- Local-only bearer auth is acceptable only while loopback/trusted-client constraints are enforced exactly; packaging or operator config could accidentally expose the gateway remotely if validation is weak.
- Session/workspace authorization is specified at paper level; implementation must prove there is no cross-workspace/thread leakage through lookup, errors, streams, pending, or diagnostics.
- Idempotency, pending, interrupt, and replay safety depend on precise current-process state handling and restart-loss tests.
- Raw app-server JSONL can contain private or secret-like content; adapter and observability implementation must prove normalization/redaction before any public/log/test/diagnostic surface.
- Future pressure to add durable replay, SQLite, identity mapping, or retained history would materially change data ownership, privacy, retention, backup, and migration obligations.

## tests_or_evidence_needed

- Auth matrix for every chat RPC: missing, duplicate, malformed, wrong bearer metadata, metadata stripping, generic safe errors, and no Codex side effect before auth/authz/validation.
- Config tests: loopback-only listen including `--listen`, exact-one token source, no raw token value output, unknown/duplicate TOML rejection, no external credential-provider command, no non-secret environment override, and startup failure on invalid token source.
- Session/workspace authorization tests: invalid/not configured selectors, unauthorized scope, not-found inside authorized context, cross-workspace `chat_id`, stream cursor, pending response, interrupt, and diagnostic non-disclosure.
- Redaction tests for logs, errors, diagnostics, examples, docs, QA evidence, adapter warnings, app-server stderr, and failure details.
- Contract tests proving no raw JSONL response/stream/diagnostic/test oracle/public proto field and no raw payload escape hatch.
- Data-minimization tests proving no prompt/message/event/history/pending display content, raw request/response payloads, prompt/content hashes, digests, auth headers, token values, cookies, private keys, raw env values, or private dumps are retained.
- Restart tests proving replay, pending, active-run, idempotency, and diagnostics correlation are lost and only Codex-proven `chat_id` lookup/status/history can be used after restart.
- Pending/interrupt tests for stale, duplicate, mismatched, expired, terminal, already-interrupting, unavailable, unknown, and after-restart cases.
- Storage guardrail tests proving no SQLite/local DB/durable mapping/content store is introduced in v1.
- Product-doc checks proving target/future docs do not claim current behavior, release readiness, production support, or remote/multi-tenant support.

## approval_or_owner_gaps

- Security/privacy/data owner approves the paper security/data design for the architecture gate only.
- QA must still approve high-risk coverage before implementation readiness or Done.
- Release/ops must still approve config, readiness, disable/rollback, supervisor/backoff, and inactive release stance.
- Engineering review is still required after implementation artifacts exist.
- Re-review is required if any scope change introduces remote/team-shared/multi-tenant operation, production/release/current-state claims, durable storage, retained content/history, content hashes, external credential providers, config reload, non-loopback listen, weaker auth/authz, or Desktop UI synchronization.

## readiness_or_no_delta_decision

Paper architecture security/privacy/data decision: pass.

Implementation/release decision: not approved by this review. The next gates must preserve the required controls above and produce implementation evidence before security/privacy/data can support Done or release readiness.

## Stage 03 Implementation Security/Privacy/Data Review

Decision: conditional pass for the reviewed Stage 03 implementation slice; no Critical or High security/privacy/data findings were found in the requested files. This is not release approval, production approval, or Done approval.

Findings by severity:

- Critical: none.
- High: none.
- Medium: token-source exact-one and absolute-file proof remains a required gate. The reviewed config path calls token-source validation and the listed tests cover invalid token values, missing env, one trailing newline from file, and NUL rejection, but the exact-one env/file and absolute-file helper implementation is outside the requested review file list and is not directly evidenced here.
- Medium: credential-provider command references are parsed and validated as config. They are not used as the local bearer token source or executed by the reviewed gRPC slice, but they must remain out of the local bearer auth path and need explicit owner approval before any supervisor/runtime execution path depends on them.
- Low: QA evidence remains partial. `tests/test-execution.md` correctly records Stage 03 partial evidence and does not claim full QA, release readiness, or Done.

Security/privacy/data verdict for the requested slice:

- Local-only boundary is preserved by config/server listen validation evidence and non-loopback rejection tests.
- Unary and streaming gRPC auth is enforced before reviewed task/chat handlers, and missing, malformed, wrong, duplicate, and non-bearer metadata are rejected with no handler call.
- Authorization metadata stripping is evidenced before task unary, task stream, and chat runtime handlers.
- `chat_runtime.enabled=false` returns typed disabled behavior for chat runtime, marks chat health `NOT_SERVING`, and keeps task RPC behavior serving.
- Reviewed chat runtime behavior is Stage 03 safe-unimplemented/disabled only; no Codex side effect, app-server call, raw JSONL exposure, private prompt/history retention, or durable store is introduced in the requested files.
- Health exposure is limited to gRPC serving status codes and does not disclose token values, workspace/session internals, raw JSONL, or private content.
- Redaction evidence covers auth failures, oversized request/response failures, panic recovery, unsafe service-supplied status details, and task compatibility checks.

Evidence executed:

- `GOPROXY=off go test ./gateway/internal/config ./gateway/internal/grpcapi` passed locally on 2026-06-16.

Remaining required gates:

- Full high-risk QA matrix and final `GOPROXY=off go test ./...` before Done or release readiness.
- Security/data review must include token-source helper implementation, or explicit evidence for exact-one env/file, absolute token file path, missing/unreadable token file, and no raw token value in errors/logs.
- Supervisor/backoff/readiness security review is still required when the actual app-server execution path is in scope; this slice only proves safe pre-side-effect registration/disabled/unimplemented behavior.
- Release/ops and QA owner approvals remain required.
- Re-review is required for any non-loopback listen, remote/team-shared/multi-tenant mode, external credential-provider execution in the bearer auth path, durable storage, raw JSONL exposure, private content logging/retention, weaker auth/authz, or current-behavior/release claims beyond the tested slice.

### Stage 03 Supervisor/Health Repair Security Addendum

Decision: targeted pass for the supervisor/health repair. No new Critical, High, or Medium security/privacy/data findings were found. This addendum does not approve Done, full QA, release readiness, production use, or current-docs sync.

Reviewed surfaces: `gateway/internal/appserver/supervisor.go`, `gateway/internal/appserver/supervisor_test.go`, `gateway/internal/grpcapi/health.go`, `gateway/internal/grpcapi/server.go`, `gateway/internal/grpcapi/server_test.go`, and `tests/test-execution.md`.

Security/privacy/data verdict:

- Dynamic health is registered behind the same gRPC auth interceptors as the other services.
- Health `Check` and `Watch` return only root/CodexControl `SERVING`, ChatRuntime `SERVING`/`NOT_SERVING`, or unknown-service `NotFound`; they do not expose session IDs, workspace paths, thread IDs, raw reasons, cooldown timestamps, failure counts, raw JSONL, tokens, or private content.
- ChatRuntime health reads supervisor `Status()` snapshots only and does not call `Connection()`, start Codex, spawn app-server, attach streams, or perform Codex-backed reads.
- Backoff/closed supervisor states are reduced to `NOT_SERVING`; task RPC compatibility remains preserved while ChatRuntime is disabled or in supervisor backoff.
- Supervisor retry/backoff errors remain typed and redacted; the only gateway error detail observed in backoff uses a safe reason category and the configured local session group id.
- No durable store, content retention, raw secret output, or non-local exposure was introduced by the repair.

Evidence:

- `GOPROXY=off go test -count=1 ./gateway/internal/appserver ./gateway/internal/grpcapi` passed locally on 2026-06-16.
- Root-reported evidence remains partial and correctly does not claim full QA or Done.

Remaining gates:

- Keep the prior token-source and credential-provider gates open.
- Dedicated regression coverage for unauthenticated health and unknown-service health behavior has been added after this addendum and must be retained before Done.
- Full high-risk QA, final `GOPROXY=off go test ./...`, release/ops approval, and product-doc current-state sync approval remain required.

## Stage 10 Implementation Security/Privacy/Data Review

Decision: conditional pass for the reviewed Stage 10 implementation slice. No Critical or High security/privacy/data blockers were found. This does not approve Done, full QA, release readiness, production use, or current-docs sync.

Reviewed surfaces: `gateway/internal/chatstate/doc.go`, `gateway/internal/chatstate/types.go`, `gateway/internal/chatstate/store.go`, `gateway/internal/chatstate/store_test.go`, `gateway/internal/domain/errors.go`, `gateway/internal/grpcapi/service_errors.go`, `plan/stage-10.md`, `tests/test-execution.md`, and `README.md`.

Findings by severity:

- Critical: none.
- High: none.
- Medium: retention bounds are proven for replay events and diagnostics, but not yet for `pending`, `idempotency`, or terminal `activeRuns` maps. The retained data is limited to safe IDs/status/reason references, so this is not a private-content leakage blocker for this slice; however, bounded cleanup/caps or explicit upstream bounds must be added or evidenced before side-effecting integration, Done, or release readiness.
- Low: idempotency keys are retained as process-local safe identifiers. Callers must keep them opaque and must not derive them from prompt/response/history content, content hashes, or digests.

Security/privacy/data verdict:

- The implementation is process-local and volatile: no file/database/SQLite/durable store, raw JSONL retention, auth material retention, prompt/response/history payload retention, or content hash/digest field was introduced in the reviewed chatstate package.
- Correlation records store safe identifiers and state categories only: session group, workspace, chat id, run id, pending request id, idempotency key/result reference, event kind/state/reason, size, timestamps, and bounded diagnostics.
- Replay buffers store event metadata only and enforce event-count, byte, and TTL bounds; no event payload/content is stored.
- Restart loss is honest in code and evidence: a new store epoch loses active run, replay, pending, idempotency, and diagnostics state and returns typed unavailable outcomes for old replay/pending/idempotency correlations.
- Idempotency is current-process only and does not claim cross-process no-duplicate or exactly-once behavior. Pre-restart keys are unrecognized by a new store.
- Diagnostics are capped and safe-label bounded; current records do not include prompt, response, payload, JSONL, token, or raw content fields.
- Stage 10 domain reasons and gRPC mapping are typed/redacted; store-generated display messages are static and do not include private content.
- Local-only assumptions remain unchanged; this slice does not add remote, multi-tenant, or public exposure.

Evidence:

- `GOPROXY=off go test -count=1 ./gateway/internal/chatstate ./gateway/internal/domain ./gateway/internal/grpcapi` passed locally on 2026-06-16.
- `tests/test-execution.md` records Stage 10 evidence as partial and does not claim full QA or Done.
- `README.md` keeps Stage 10 as local test pass with owner reviews pending and keeps current-docs sync, QA execution, and release/current-state artifacts pending.

Remaining gates:

- Add or evidence cleanup/caps for pending, idempotency, and terminal active-run records before side-effecting chat integration, Done, or release readiness.
- Preserve the rule that idempotency keys and diagnostic labels are opaque safe IDs, not content-derived hashes/digests.
- Full high-risk QA, final full-suite execution, release/ops approval, and current-behavior docs sync approval remain required.

### Stage 10 Bounded Retention Repair Security Addendum

Decision: targeted pass for the bounded-retention repair. The prior Stage 10 Medium gate for unbounded `pending`, `idempotency`, and terminal `activeRuns` retention is closed for the reviewed delta. No new Critical, High, or Medium security/privacy/data findings were found. This does not approve Done, full QA, release readiness, production use, or current-docs sync.

Reviewed delta: `gateway/internal/chatstate/types.go`, `gateway/internal/chatstate/store.go`, `gateway/internal/chatstate/store_test.go`, and `tests/test-execution.md`.

Findings by severity:

- Critical: none.
- High: none.
- Medium: none.
- Low: in-flight idempotency entries are cap-bounded and return typed `resource_exhausted` under pressure; completed entries are TTL-cleaned. Preserve the documented opaque safe-ID rule for idempotency keys so these identifiers cannot become prompt/response/history hashes or content digests.

Security/privacy/data verdict:

- `ActiveRunsCap`, `PendingCap`, and `IdempotencyCap` now bound process-local correlation maps.
- Terminal `CompleteRun` removes active-run records, closing terminal active-run retention.
- Pending records are checked after cleanup, active pending entries are capped, and resolved/expired pending records are removed by TTL.
- Idempotency entries are checked after cleanup, capped under pressure, and completed entries are removed by TTL; cap pressure returns typed `resource_exhausted` with static safe messages.
- Replay TTL is enforced on read as well as append, so idle expired replay buffers do not remain observable until the next append.
- The reviewed repair adds no durable store and no prompt, response, history, raw JSONL, auth material, token, content hash, digest, or private-content retention.

Evidence:

- `GOPROXY=off go test -count=1 ./gateway/internal/chatstate ./gateway/internal/domain ./gateway/internal/grpcapi` passed locally on 2026-06-16.
- `tests/test-execution.md` records the Stage 10 bounded-cap/cleanup evidence as partial implementation evidence and does not claim full QA or Done.

Remaining gates:

- Keep idempotency keys and diagnostic labels opaque safe IDs, not content-derived hashes or digests.
- Keep cap values aligned with local NFR/config ownership before side-effecting chat integration.
- Full high-risk QA, final full-suite execution, release/ops approval, and current-behavior docs sync approval remain required.

## Stage 05 Implementation Security/Privacy/Data Review

Decision: pass for the Stage 05 `StartChatRun` gateway boundary after idempotency scope repair. No Critical, High, or Medium security/privacy/data findings were found. This does not approve Done, full QA, release readiness, production use, current-docs sync, or SDK implementation acceptance.

Reviewed surfaces: `gateway/internal/chatruntime`, `gateway/internal/chatstate`, `gateway/internal/grpcapi` chat handler/validator/error mapping, gateway composition, `plan/stage-05.md`, and `tests/test-execution.md`.

Security/privacy/data verdict:

- Auth interceptor, validation, session/workspace checks, and idempotency reservation remain before Codex side effects.
- Prompt/context are sent only to Codex `turn/start`; `chatstate` idempotency stores safe metadata/result references only and no prompt, response, raw JSONL, raw request/response payload, auth material, content hash, or digest.
- `ClientMessageID` is now part of the safe idempotency scope for `StartChatRun`, which prevents same-key/different-first-turn-correlation reuse without retaining private content.
- Successful retry returns a safe prior result reference; uncertain post-side-effect retry returns typed `Unknown/idempotency_result_unavailable`; raw JSONL escape hatch, durable gateway chat id/store, and external persistence were not introduced.

Evidence:

- Root ran `GOPROXY=off go test -count=1 ./gateway/internal/chatruntime ./gateway/internal/chatstate`, `GOPROXY=off go test ./...`, and `git diff --check`.
- Security/privacy/data reviewer inspected the repaired surface and reported no blockers.

Remaining gates:

- Keep `client_message_id` and `idempotency_key` as caller-provided opaque safe IDs, not secrets or content-derived hashes/digests.
- Full high-risk QA, final full-suite execution, release/ops approval, and current-behavior docs sync approval remain required.

## Stage 06 Implementation Security/Privacy/Data Recheck

Decision: pass for the Stage 06 existing-chat gateway behavior after targeted repairs. No Critical, High, or Medium security/privacy/data findings remain in the reviewed delta. This does not approve Done, full QA, release readiness, production use, current-docs sync, or SDK implementation acceptance.

Reviewed surfaces: `gateway/internal/chatruntime`, `gateway/internal/chatstate`, `gateway/internal/grpcapi` chat handlers/validators/tests, `gateway/internal/domain/errors.go`, `plan/stage-06.md`, and `tests/test-execution.md`.

Security/privacy/data verdict:
- Existing-chat operations keep auth/validation/session/workspace preflight before Codex calls, stream attach, state disclosure, or side effects.
- `chat_id` is validated as canonical UUID-shaped Codex `Thread.id`; task IDs, run IDs, cursors, pending IDs, and idempotency keys are not accepted as chat identity.
- History cursors are process-local signed envelopes scoped to session group, workspace, chat id, requested depth, sort direction, and cursor payload; tampered or cross-chat cursors are rejected before Codex history calls.
- Unknown Codex thread lifecycle returns a sanitized typed unavailable outcome and preserves local active state; it does not fabricate safe status or leak raw Codex/app-server state.
- Stream subscription after run completion now closes/refuses the live stream under lock instead of leaving an orphan live stream observable.
- The reviewed repair adds no durable store, no raw JSONL escape hatch, no prompt/response/history/event content retention, no auth material retention, and no prompt/content hash or digest.

Evidence:
- Root ran `GOPROXY=off go test -count=1 ./gateway/internal/chatstate ./gateway/internal/chatruntime ./gateway/internal/domain ./gateway/internal/grpcapi`, `GOPROXY=off go test -count=1 ./...`, and `git diff --check`.
- Security/privacy/data reviewer inspected the repaired surface and reported no P0/P1/P2 findings.

Residual risks:
- Process-local HMAC signing invalidates old history cursors after gateway restart. This is an availability-only tradeoff and matches the process-local replay/cursor boundary.
- If Codex changes `Thread.id` away from canonical UUID shape, validation and SRS/contract docs must be reopened before compatibility is claimed.

Remaining gates:
- Keep idempotency keys, client message ids, cursor strings, and diagnostic labels opaque safe IDs, not secrets or content-derived hashes/digests.
- Stage 07 pending/interrupt targeted security/privacy/data recheck is recorded below; downstream full QA, release/ops, current-docs sync, SDK acceptance, and release/current-state gates remain open.
- Full high-risk QA, final full-suite execution, release/ops approval, and current-behavior docs sync approval remain required.

## Stage 07 Implementation Security/Privacy/Data Recheck

Decision: pass for the targeted Stage 07 pending/interrupt repair scope after owner-finding repairs. No Critical, High, or Medium security/privacy/data findings remain in the reviewed delta. This does not approve Done, full QA, release readiness, production use, current-docs sync, or SDK implementation acceptance.

Review date: 2026-06-16.

Reviewed surfaces: `gateway/internal/chatstate/types.go`, `gateway/internal/chatstate/store.go`, `gateway/internal/chatstate/store_test.go`, `gateway/internal/chatruntime/pending_interrupt.go`, `gateway/internal/chatruntime/service_test.go`, `plan/stage-07.md`, and `tests/test-execution.md`.

Security/privacy/data verdict:
- Expired pending is claimed/checked in `chatstate.Store` before a pending response can be forwarded to Codex.
- Expired wrong-shape pending responses return typed stale/unavailable behavior before response-type disclosure and prune the stale process-local raw pending record.
- Respond-pending idempotency is bound to the concrete `pending_request_id`; same key reuse against another pending target conflicts before duplicate Codex side effects.
- Pending response content fingerprints/digests are not retained in the chat runtime pending response path.
- Accepted, stale, and lazily observed expired pending records are removed from `Service.pendingRecords`; raw cloned app-server request params remain process-local and only for active pending work.
- Pending completion and pending creation preserve `interrupting` through store mutex-owned state transitions and do not fabricate terminal completion.
- The repair adds no durable store, raw JSONL public escape hatch, prompt/history/content retention, auth material retention, content hash/digest, remote exposure, or release/current-state claim.

Evidence:
- Root ran `GOPROXY=off go test -count=1 .\gateway\internal\chatstate .\gateway\internal\chatruntime`, `GOPROXY=off go test -count=1 .\gateway\internal\appserver .\gateway\internal\chatstate .\gateway\internal\chatruntime .\gateway\internal\grpcapi`, `GOPROXY=off go test -count=1 ./...`, and `git diff --check`.
- Security/privacy/data reviewer ran scoped checks including chatstate/chatruntime tests and accepted the repair.

Residual risks:
- Cleanup of expired raw pending records is lazy, triggered by later pending/status paths or process restart; it is not a hard real-time erasure guarantee. This is accepted only within the current local, process-only, volatile gateway boundary.

Remaining gates:
- Preserve the no-content-fingerprint/no-content-digest rule for pending responses and idempotency.
- Full high-risk QA, final full-suite execution, release/ops approval, current-behavior docs sync approval, SDK implementation acceptance, and explicit release/current-state activation remain required.

## Stage 08 SDK Implementation Security/Privacy/Data Recheck

Decision: pass for the targeted Stage 08 SDK/adoption repair scope after owner-finding repairs. No Critical, High, or Medium security/privacy/data findings remain in the reviewed delta. This does not approve Done, full QA, release readiness, production use, current-docs sync, runtime acceptance as a whole, or release/current-state activation.

Review date: 2026-06-16.

Reviewed surfaces: `client.go`, `chat.go`, `history.go`, `stream.go`, `pending.go`, `types.go`, `client_test.go`, `examples/api-handler/main.go`, `examples/api-handler/main_test.go`, `plan/stage-08.md`, and `tests/test-execution.md`.

Security/privacy/data verdict:
- SDK auth builds fresh outgoing metadata for runtime calls and sends exactly one configured bearer header; caller-provided outgoing metadata is not copied into gateway calls.
- Bearer tokens are redacted from SDK string representations, and tests cover the redaction behavior.
- The API-handler example refuses insecure gRPC dialing to non-loopback addresses, rejects empty prompts before SDK side effects, and returns a generic external error string instead of leaking gateway/Codex details to HTTP callers.
- SDK-generated client message ids, response ids, request ids, subscriber ids, and idempotency keys use random opaque public-safe identifiers; they are not content hashes/digests and do not encode prompt/history/event material.
- Stream close/cancellation does not send interrupt, preventing accidental destructive user action from ordinary request cleanup.
- The repair adds no durable store, raw JSONL public escape hatch, prompt/history/content retention, auth material retention, content fingerprint/digest, remote exposure, or current-state/release claim.

Evidence:
- Root ran `GOPROXY=off go test -count=1 .`, `GOPROXY=off go test -count=1 .\examples\api-handler`, `GOPROXY=off go test -count=1 ./...`, `GOPROXY=off go list ./...`, `GOPROXY=off go vet ./...`, and `git diff --check`.
- Security/privacy/data reviewer inspected the repaired SDK/adoption surface and reported no Critical, High, or Medium findings.

Residual risks:
- The example remains an example for local API-handler use, not a production hardening guide. External auth, tenant isolation, and deployment policy remain application responsibilities outside v1 local runtime scope.

Remaining gates:
- Preserve the fresh-metadata/no-caller-metadata-copy rule for all future SDK RPC wrappers.
- Full high-risk QA, final integration/e2e checks, release/ops approval, current-behavior docs sync approval, and explicit release/current-state activation remain required.

## Stage 09 Targeted Security/Privacy/Data Recheck After Repair

Decision: pass for the Stage 09 targeted security/privacy/data closeout after the active-run capacity repair. No Critical, High, or Medium findings were found in the scoped delta. This does not approve Done, full QA, release readiness, production use, current-docs sync, runtime acceptance as a whole, or release/current-state activation.

Review date: 2026-06-16.

Reviewed surfaces: `gateway/internal/chatstate/store.go`, `gateway/internal/chatstate/store_test.go`, `gateway/internal/chatruntime/service.go`, `gateway/internal/chatruntime/service_test.go`, `plan/stage-09.md`, and `tests/test-execution.md`.

Security/privacy/data verdict:
- Active-run capacity reservations are process-local and store safe scope fields only; they do not store prompt, response, history, raw payload, auth header, token, or private content.
- `StartChatRun` reserves capacity before app-server connection/thread/turn side effects and releases the reservation on failure paths that do not become a tracked active run.
- The repair adds no durable store, raw JSONL public escape hatch, content retention, auth material retention, remote exposure, or release/current-state claim.
- Stage 09 evidence stays scoped to local verification and does not claim Done, full QA, current docs, or release.

Evidence:
- Root ran targeted and full local verification, proto generation sync, vet/list, diff-check, and scoped guardrail greps.
- Security/privacy/data reviewer inspected the delta and reported no Critical, High, or Medium findings.

Remaining gates:
- Keep reservation and idempotency scope fields opaque safe IDs, not secrets or content-derived hashes/digests.
- Full high-risk QA, current-behavior docs sync approval, release/ops approval, and explicit release/current-state activation remain required.
