# Product Review: Chat-First Runtime

Type: product review
Status: BRS accepted; SRS product review passed; QA-basis product sanity passed; implementation/release acceptance still pending
Reviewer: product_owner
Review date: 2026-06-14
Related docs: `../BRS/feature.md`, `../BRS/nfr.md`, `../SRS/index.md`
Trace IDs: BRS-GOAL-001, BRS-REQ-001..BRS-REQ-007, BRS-RULE-001..BRS-RULE-014, BRS-NFR-001..BRS-NFR-009

## Review Scope
The first review covered the BRS product baseline:
- `../BRS/feature.md`
- `../BRS/nfr.md`

This SRS product review covers whether the SRS bundle faithfully translates the accepted BRS into system behavior without adding product scope or unsupported promises:
- `../SRS/**`
- `../../../product-docs/sdk/chat-first-go-sdk.md`
- `../../../product-docs/grpc/codex-runtime-gateway.md`
- `../../../product-docs/domain/chat-runtime.md`
- `../../../product-docs/security/local-runtime-boundary.md`

This QA-basis product sanity review covers product fit and traceability of the repaired paper QA artifacts:
- `../tests/test-strategy.md`
- `../tests/test-cases.md`
- `../tests/regression.md`
- `../tests/test-execution.md`
- `qa-readiness.md`

This is not acceptance of implementation plans, code, proto, scripts, release readiness, current-state docs, runtime behavior, executed test evidence, or implementation.

## Product Verdict
Decision: pass.

The BRS is accepted as the product baseline for downstream SRS work. It reflects the user's required direction: a local chat-first compatibility layer over working Codex, with Codex owning real runtime behavior and history.

This original BRS verdict is retained for traceability. The separate SRS product review outcome is recorded below.

## Product Fit Checks
- Gateway is scoped as a local gRPC wrapper/compatibility layer over installed Codex, not a second owner of Codex functionality.
- `Chat` is explicit and has lifecycle/status; in v1 `Chat == Codex Thread`.
- Application-facing `chat_id` is exactly the origin Codex `Thread.id` in v1.
- No gateway-created durable chat id, SQLite store, durable mapping store, or durable gateway-owned history is in v1 scope.
- Gateway-owned state is limited to process-local correlation; restart continuity is not promised beyond what Codex can prove for the stored thread id.
- Empty chat creation without a first message is out of scope.
- `codex.Run(ctx, prompt)` is framed as SDK convenience for a new Codex-backed chat with the first prompt, not gateway-owned business logic.
- History, events, pending, interrupt, and status behavior are Codex-backed only; unsupported, unavailable, unknown, or narrowed outcomes are required instead of invented behavior.
- SDK-created chats are not promised as visible/current Codex Desktop UI threads.
- v1 is local only, and release remains inactive until final implementation and explicit release readiness work.

## Product Findings
None.

## Open Questions
None.

## SRS Product Review
Decision: pass.

The SRS bundle preserves the accepted BRS product intent. It defines the future local chat-first SDK/gRPC behavior as a compatibility layer over installed Codex, keeps Codex as the owner of real behavior and history, and does not add a gateway-owned chat identity, durable mapping store, gateway-owned history, Desktop UI synchronization, remote exposure, or release/current-state promise.

## SRS Product Fit Checks
- `chat_id` remains exactly Codex `Thread.id` in v1; `run_id`, task compatibility IDs, cursors, pending IDs, and idempotency keys are kept distinct from `chat_id`.
- No SQLite, gateway database, durable mapping store, durable gateway-owned chat id, or gateway-owned history/content retention is introduced.
- The gateway remains a local gRPC wrapper/compatibility layer over installed Codex and internal app-server JSONL; raw JSONL is not the public SDK/gRPC contract.
- Gateway state is process-local only for active run, replay, pending, idempotency, and diagnostics correlation; restart loss is explicit and typed.
- Empty chat creation is not exposed as successful SDK-created chat behavior; success requires non-empty first prompt and first-turn acceptance/correlation.
- `codex.Run(ctx, prompt)` remains SDK convenience for a new Codex-backed chat with the first prompt, not gateway-owned product logic.
- History, events, status, pending, and interrupt behavior are Codex-backed; unsupported, unavailable, unknown, narrowed, stale, duplicate, terminal, and replay/cursor outcomes are typed.
- v1 history stays at Codex-owned turn summary/projection depth where supported; item-level history is explicitly unsupported unless later Codex evidence proves support.
- SDK-created chats are not promised as visible or current Codex Desktop UI threads.
- v1 remains local-only; release/current-state artifacts remain inactive until final implementation and explicit release readiness.
- The touched product-doc surfaces are marked draft target/future/not-current and do not pretend docs_writer current-behavior sync has happened.

## SRS Product Findings
None.

## SRS Open Questions
None.

## QA-Basis Product Sanity Review
Decision: pass.

The repaired QA basis covers the accepted BRS/SRS product requirements and does not introduce product drift. The QA pass remains a paper readiness pass only: `test-execution.md` is still not started/not executed, and no implementation, release, current-state, or Done acceptance is created by this review.

## QA-Basis Product Fit Checks
- Test strategy, case catalog, regression scope, execution register, and QA readiness stay aligned to `chat_id == Codex Thread.id`.
- Cases cover start with a non-empty first prompt, first-turn acceptance/correlation, no empty-chat success, existing-chat lookup, and existing-chat turn submission.
- QA basis does not introduce a gateway-minted durable chat id, SQLite/local database, durable mapping store, durable replay, durable pending/idempotency memory, or gateway-owned prompt/message/event/history store.
- Gateway state remains process-local only; restart tests require loss of replay, pending, active-run, idempotency, and diagnostics correlation unless Codex can prove state again.
- History coverage stays limited to Codex-owned turn summary/projection where supported; item-level history is explicitly unsupported in v1 unless accepted Codex evidence changes.
- Status, history, event stream, pending, interrupt, idempotency, replay, and restart cases require typed unsupported, unavailable, unknown, narrowed, stale, duplicate, terminal, conflict/precondition, or out-of-range outcomes instead of fabricated success.
- Raw app-server JSONL is prohibited from public contract, tests, logs, diagnostics, docs/examples, and QA evidence.
- Security/privacy/data coverage includes all-RPC auth/authz/validation before Codex side effects, redaction, no secret/private data leakage, and no retained content hashes/digests.
- v1 remains local-only; remote, shared, multi-tenant, hosted, production, and internet-facing operation remain out of QA paper scope.
- Desktop UI visibility/current-thread synchronization is not promised.
- Release/current-state remains inactive; QA readiness explicitly avoids release, implementation, code/proto, execution, or Done claims.

## QA-Basis Product Findings
None.

## QA-Basis Open Questions
None.

## Downstream Gates Preserved
The SRS and QA-basis product review passes do not approve implementation readiness by themselves. Implementation planning and future delivery still need the required downstream owner gates where touched: architecture/proto, docs sync/traceability, implementation, QA execution, security/privacy/data follow-up, release/ops follow-up, and engineering/product implementation review if code later changes.

## Must Preserve For SRS
- Do not reintroduce task-first identity, task/run ids as primary app identity, or `chat_id` ambiguity.
- Do not introduce a gateway-created durable chat id or durable mapping store.
- Do not make the gateway the source of truth for prompt, message, event, history, or real runtime functionality.
- Do not expose raw app-server JSONL as the stable SDK/gRPC contract.
- Do not promise Desktop UI visibility/current-thread synchronization for SDK-created chats.
- Do not promise release/current-state readiness before final implementation and explicit release gate.

## Next Gate
Exact next gate: docs sync and implementation planning may proceed from the accepted BRS/SRS and repaired QA paper basis, while test execution remains blocked until implementation references exist. After implementation, QA execution plus product/engineering implementation review and release readiness must pass before Done or release.

## Plan Product Review
Decision: pass for plan review; not implementation approval.

Review date: 2026-06-15.

Reviewed plan bundle:
- `../plan/index.md`
- `../plan/stage-01.md`
- `../plan/stage-02.md`
- `../plan/stage-03.md`
- `../plan/stage-05.md`
- `../plan/stage-06.md`
- `../plan/stage-07.md`
- `../plan/stage-08.md`
- `../plan/stage-09.md`
- `../plan/stage-10.md`

## Plan Product Fit Summary
The repaired plan faithfully carries the accepted BRS/SRS intent into implementation planning. It keeps the gateway as a local gRPC compatibility wrapper over installed Codex, preserves `Chat == Codex Thread`, keeps public `chat_id == Codex Thread.id`, and does not add gateway-created durable chat identity, SQLite/local mapping storage, gateway-owned conversation history/content, raw app-server payload escape hatches, Desktop visible/current-thread promises, remote/shared scope, or release/current-state activation.

The plan answers what will be built, how the work is sequenced, and what limitations remain. It explicitly covers the dedicated chat-first contract, local auth/readiness, process-local correlations, new-chat first-turn acceptance, existing-chat behavior, status/history/events, pending/interrupt, SDK adoption, integration hardening, QA handoff, current-behavior docs sync handoff, and release non-activation.

## Plan Product Findings
None.

## Plan Open Questions
None.

## Plan Remaining Gates
- Engineering plan review.
- Root approval before moving from plan review to implementation.
- Developer implementation.
- Post-implementation product and engineering review.
- QA execution and evidence capture.
- Current-behavior docs sync against implemented behavior.
- Explicit release/current-state activation gate after final implementation readiness.

## Targeted Product Re-Review After Engineering P1 Repairs
Decision: pass for targeted product re-review; not implementation approval.

Review date: 2026-06-15.

Reviewed repair scope:
- SRS repairs in `../SRS/contracts.md`, `../SRS/sequences.md`, `../SRS/rollout.md`, `../SRS/feature.md`, `../SRS/nfr-mapping.md`, and `../SRS/traceability.md`.
- Plan repairs in `../plan/index.md`, `../plan/stage-01.md`, `../plan/stage-03.md`, `../plan/stage-05.md`, `../plan/stage-07.md`, `../plan/stage-09.md`, and `../plan/stage-10.md`.
- QA repairs in `../tests/test-strategy.md`, `../tests/test-cases.md`, `../tests/regression.md`, and `qa-readiness.md`.

Targeted product verdict:
- Moving app-server supervisor startup/backoff into Stage 03 before side-effecting Codex access stays within product scope. It strengthens the existing local readiness/auth/validation-before-side-effect boundary and does not add a new user-facing runtime promise beyond typed dependency/startup/backoff failure.
- Narrowing idempotency to current-process side-effect tracking matches the accepted BRS. After gateway restart, a pre-restart idempotency key alone is explicitly not proof of prior request, replay, recovery, exactly-once delivery, terminal state, or no-duplicate suppression. App-stored `chat_id` remains usable only as Codex `Thread.id` where Codex can prove thread/status/history state.
- The repaired SRS, plan, and QA basis preserve `chat_id == Codex Thread.id`, process-local gateway correlation only, Codex ownership of real behavior/history, local-only scope, no Desktop UI visibility promise, no durable gateway identity/content store, no raw JSONL public contract, and inactive release/current-state stance.

Product findings: None.

Remaining gates:
- Engineering re-review of the P1 repairs.
- Root approval before implementation planning/implementation proceeds.
- Developer implementation after approval.
- Post-implementation product and engineering review.
- QA execution and evidence capture.
- Current-behavior docs sync against implemented behavior.
- Explicit release/current-state activation gate after final implementation readiness.

Open product questions: None.

## Stage 02 Implementation Product Review
Decision: pass for Stage 02 implementation product review; not Done, runtime acceptance, QA completion, docs-current-state sync, or release approval.

Review date: 2026-06-15.

Reviewed implementation slice:
- `../../../../proto/codex_control/v1/codex_control.proto`
- `../../../../gen/codex/control/v1/codex_control.pb.go`
- `../../../../gen/codex/control/v1/codex_control_grpc.pb.go`
- `../../../../gateway/internal/grpcapi/chat_contract_test.go`
- `../plan/stage-02.md`
- `../tests/test-execution.md`

Product fit verdict:
- The implementation introduces a dedicated `ChatRuntimeService` with the accepted chat-first operations: `StartChatRun`, `GetChat`, `RunChatTurn`, `GetChatStatus`, `GetChatHistory`, `StreamChatEvents`, `RespondChatPending`, and `InterruptChatRun`.
- Existing `CodexControl` task RPCs remain present as a compatibility surface; Stage 02 does not make task IDs the primary chat SDK contract.
- Public chat identity is represented as `chat_id`, with `run_id`, cursors, pending IDs, idempotency keys, and task compatibility identity kept separate.
- `StartChatRun` requires a prompt-shaped request and its response includes `run_id`, `status`, and `first_turn_accepted`, preserving the product intent that successful new-chat behavior depends on first-turn acceptance/correlation rather than empty thread creation.
- Status, history, event, pending, interrupt, replay, local gateway state, capability depth, and error outcomes are represented as typed chat-first contract concepts, including unsupported, unavailable, unknown, narrowed, out-of-range, cancelled, deadline, and internal-after-redaction categories.
- The chat-first event contract does not include `UnknownRawEvent`, `payload_json`, or another raw app-server JSONL/payload escape hatch. The existing raw task compatibility event remains outside this Stage 02 chat-first review target.
- The slice does not introduce SQLite, a gateway-owned durable chat identity, a local DB mapping store, gateway-owned history/content storage, Desktop UI visibility/current-thread promises, remote/shared operation, or release/current-state activation.
- `test-execution.md` correctly records Stage 02 as partial contract evidence only and does not claim full QA execution or Done.

Findings:
- None product-blocking.
- Severity: watch item, non-blocking. The proto can represent the first-turn acceptance marker, but proto shape alone cannot prove the runtime invariant that `StartChatRun` never returns OK for a thread-only/empty-chat result. Stage 05 runtime implementation and QA must prove that no successful `StartChatRun`/`codex.Run` response is emitted unless first prompt submission and first-turn acceptance/correlation are proven.

Remaining gates:
- Engineering implementation review for Stage 02 generated/proto/test quality and build hygiene.
- Stage 03 implementation for local config/auth/session/workspace/readiness foundations before side-effecting Codex access.
- Stage 05 runtime implementation and tests for first prompt, first-turn acceptance/correlation, no empty-chat success, and `chat_id == Codex Thread.id` evidence.
- Later runtime stages for process-local correlation, restart loss, status/history/events/pending/interrupt behavior, and typed unsupported/unavailable/unknown/narrowed outcomes.
- QA execution and evidence capture beyond the partial Stage 02 contract check.
- Current-behavior documentation sync after implemented behavior exists.
- Explicit release/current-state activation gate after final implementation readiness.

Product proceed verdict: Stage 03 may proceed from product perspective.

## Stage 03 Implementation Product Review
Decision: fail for Stage 03 implementation product acceptance; not Done, QA completion, current-behavior docs sync, or release approval.

Review date: 2026-06-16.

Reviewed implementation slice:
- `../../../../gateway/internal/config/types.go`
- `../../../../gateway/internal/config/toml.go`
- `../../../../gateway/internal/config/validate.go`
- `../../../../gateway/internal/config/limits.go`
- `../../../../gateway/internal/domain/errors.go`
- `../../../../gateway/internal/grpcapi/server.go`
- `../../../../gateway/internal/grpcapi/chat_handlers.go`
- `../../../../gateway/internal/grpcapi/server_test.go`
- `../../../../gateway/internal/config/config_test.go`
- `../plan/stage-03.md`
- `../tests/test-execution.md`
- `../README.md`

Product fit verdict:
- The implemented slice preserves the accepted product boundaries that are visible in this review: the gateway remains local-only, loopback-bound, bearer-authenticated, and a thin gRPC wrapper; it does not introduce SQLite, gateway-owned durable chat identity/history, raw JSONL as the public chat contract, empty-chat success, Desktop UI promises, remote exposure, release activation, or Stage 05/06/07 runtime completion claims.
- The config/auth/readiness-disabled portion is product-aligned: `[chat_runtime].enabled` is parsed and validated, defaults to enabled, can disable chat runtime independently, keeps task RPCs serving, registers health, strips authorization metadata before handlers, uses typed disabled/not-implemented chat runtime errors, enforces loopback listen, rejects unsafe runtime policy/config choices, applies message and pending limits, and records partial Stage 03 evidence.
- The implementation intentionally leaves chat runtime RPCs as typed `Unimplemented`, which is acceptable for Stage 03 only if the Stage 03 readiness/security/supervisor foundation is otherwise complete and clearly evidenced.

Findings:
- Severity: P1, blocking. Stage 03 is not complete against its own accepted plan because the reviewed implementation/evidence does not demonstrate the minimal lazy `codex.exe app-server` supervisor foundation, startup timeout, caller-deadline precedence, repeated-failure cooldown/backoff, typed dependency failure, or supervisor readiness/backoff health states required by `../plan/stage-03.md`. The reviewed chat runtime handlers validate auth/size/disabled state and then return `chat_runtime_not_implemented`; no reviewed Stage 03 path proves that later `thread/start`, `turn/start`, pending response, interrupt, or Codex-backed reads can rely on a pre-side-effect supervisor boundary. This is a product acceptance gap because Stage 05/06/07 would otherwise have to infer or add part of the Stage 03 safety foundation.
- Severity: P2, evidence/docs gap. `../tests/test-execution.md` records Stage 03 local readiness/security-boundary checks as pass, but the recorded implementation references and notes cover config, auth-gated chat service registration, disabled behavior, health status, and task compatibility only. It should keep supervisor/backoff/readiness-dependency checks as pending or explicitly unevidenced until implementation references and tests prove them.

Remaining gates:
- Implement and evidence the Stage 03 lazy app-server supervisor foundation, including startup timeout, caller-deadline precedence, repeated-failure cooldown/backoff, safe redacted reason categories, typed dependency failure, and readiness/health behavior.
- Re-run Stage 03 product review after the supervisor evidence is added.
- Engineering implementation review for Stage 03.
- QA execution and evidence capture beyond partial local checks.
- Stage 05 runtime implementation and tests for first prompt, first-turn acceptance/correlation, no empty-chat success, and `chat_id == Codex Thread.id` evidence.
- Later runtime stages for process-local correlation, restart loss, status/history/events/pending/interrupt behavior, and typed unsupported/unavailable/unknown/narrowed outcomes.
- Current-behavior documentation sync after implemented behavior exists.
- Explicit release/current-state activation gate after final implementation readiness.

Product proceed verdict: Stage 03 should not proceed to accepted implementation status until the P1 supervisor foundation gap is repaired and re-reviewed.

## Targeted Stage 03 Implementation Product Re-Review After Supervisor Repair
Decision: pass for targeted Stage 03 supervisor repair product acceptance; not Done, full QA completion, current-behavior docs sync, runtime acceptance, or release approval.

Review date: 2026-06-16.

Reviewed repair refs:
- `../../../../gateway/internal/appserver/supervisor.go`
- `../../../../gateway/internal/appserver/supervisor_test.go`
- `../../../../gateway/internal/appserver/process.go` and `../../../../gateway/internal/appserver/connection.go` as narrow supporting checks for app-server supervisor wiring and typed dependency behavior.
- `../plan/stage-03.md`
- `../tests/test-execution.md`
- `../README.md`

Product fit verdict:
- The prior P1 supervisor foundation gap is closed for Stage 03 product purposes. The repaired app-server supervisor provides a lazy per-session-group foundation, shared cold start, startup timeout, caller-deadline precedence, repeated non-cancel/non-deadline failure cooldown/backoff, retryable typed backoff reason, connecting/backoff/closed status, close cancellation, and process-supervisor wiring to `StartProcessConnection`.
- The repair remains within Stage 03 scope. It adds readiness/security/supervisor foundation only and does not claim Stage 05/06/07 chat runtime behavior, first-turn acceptance, status/history/events, pending, interrupt, restart recovery, or current-process idempotency completion.
- The accepted product boundaries remain intact: local-only gateway, Codex-owned chat identity/history, `chat_id == Codex Thread.id`, no SQLite or gateway-owned durable history, no raw JSONL public chat contract, no empty-chat success, no Desktop UI promise, and no release/current-state activation.
- The repaired evidence no longer overpromises the supervisor area: `test-execution.md` records Stage 03 supervisor checks as partial implementation evidence and still states that full `TC-001`..`TC-036` QA execution is pending.

Findings:
- None product-blocking.

Remaining gates:
- Engineering implementation re-review for Stage 03, including concurrency/error-handling/code-quality judgment.
- QA execution and evidence capture beyond the partial Stage 02/03 local checks.
- Stage 05 runtime implementation and tests must wire actual chat side-effecting paths through the Stage 03 auth/validation/authz/supervisor boundary and prove first prompt, first-turn acceptance/correlation, no empty-chat success, and `chat_id == Codex Thread.id`.
- Later runtime stages must still prove process-local correlation, restart loss, status/history/events/pending/interrupt behavior, and typed unsupported/unavailable/unknown/narrowed outcomes.
- Current-behavior documentation sync after implemented behavior exists.
- Explicit release/current-state activation gate after final implementation readiness.

Product proceed verdict: Stage 03 passes product owner re-review after the supervisor repair, with downstream gates preserved.

## Stage 03 Product Re-Review Addendum After Timeout And Dynamic Health Repair
Decision: prior Stage 03 product pass remains valid after the additional timeout and health repair; not Done, full QA completion, current-behavior docs sync, runtime acceptance, or release approval.

Review date: 2026-06-16.

Reviewed additional delta:
- `../../../../gateway/internal/appserver/supervisor.go`
- `../../../../gateway/internal/appserver/supervisor_test.go`
- `../../../../gateway/internal/grpcapi/health.go`
- `../../../../gateway/internal/grpcapi/server.go`
- `../../../../gateway/internal/grpcapi/server_test.go`
- `../tests/test-execution.md`

Product addendum verdict:
- The timeout repair strengthens the Stage 03 safety boundary: app-server startup now has an upper bound even when the caller provides a longer deadline, while shorter caller deadlines still win.
- Dynamic chat runtime health remains product-aligned: supervisor backoff/closed status reports `NOT_SERVING` without starting Codex, disabled chat runtime still reports `NOT_SERVING`, and task RPC compatibility remains serving.
- The docs/evidence update stays appropriately narrow. It records partial Stage 03 local readiness/security-boundary evidence and does not claim Stage 05/06/07 chat runtime behavior, full `TC-001`..`TC-036` QA execution, current docs sync, Done, or release readiness.

Findings:
- None product-blocking.

Remaining gates:
- Engineering re-review closure for the timeout/health repair.
- QA execution beyond partial Stage 02/03 checks.
- Stage 05+ runtime implementation and evidence for actual chat behavior, first-turn acceptance, no empty-chat success, `chat_id == Codex Thread.id`, status/history/events/pending/interrupt, restart-loss behavior, current-docs sync, and explicit release/current-state activation when applicable.

## Stage 10 Implementation Product Review
Decision: pass for Stage 10 implementation product acceptance; not Done, full QA completion, current-behavior docs sync, runtime acceptance, or release approval.

Review date: 2026-06-16.

Reviewed implementation slice:
- `../../../../gateway/internal/chatstate/doc.go`
- `../../../../gateway/internal/chatstate/types.go`
- `../../../../gateway/internal/chatstate/store.go`
- `../../../../gateway/internal/chatstate/store_test.go`
- `../../../../gateway/internal/domain/errors.go`
- `../../../../gateway/internal/grpcapi/service_errors.go`
- `../plan/stage-10.md`
- `../tests/test-execution.md`
- `../README.md`

Product fit verdict:
- The Stage 10 implementation stays process-local and volatile. `chatstate.Store` is in-memory only, uses a process epoch, and a fresh store loses active run, replay, pending, idempotency, and diagnostics correlations.
- The implementation does not introduce SQLite, a gateway-owned durable chat identity, identity translation storage, durable history/content storage, raw JSONL retention, auth retention, prompt/response retention, or content hash/digest retention.
- Stored records are limited to safe identifiers, operation/status/state categories, reason codes, timestamps, replay size metadata, and capped diagnostics. Idempotency stores a safe result reference only (`chat_id`, `run_id`, status), not the request or response content.
- Restart semantics are product-aligned: replay, pending, idempotency, and diagnostics are unavailable after process loss unless later Codex-backed runtime stages can prove state from `chat_id == Codex Thread.id`.
- The docs/evidence stay narrow. `test-execution.md` records Stage 10 as partial implementation evidence and still keeps full `TC-001`..`TC-036` QA execution, current docs sync, Done, and release readiness pending.

Findings:
- None product-blocking.
- Severity: watch item, non-blocking. Stage 05 must prove the actual new-chat side-effect path uses current-process idempotency before Codex side effects and then binds only to the Codex-proven `Thread.id`; Stage 10 provides the volatile foundation but does not by itself prove runtime no-duplicate or no-empty-chat behavior.

Remaining gates:
- Engineering implementation review for Stage 10.
- QA execution and evidence capture beyond partial local checks.
- Stage 05/06/07 runtime implementation must wire these volatile correlations into actual chat side-effecting paths and prove first-turn acceptance, no empty-chat success, `chat_id == Codex Thread.id`, status/history/events/pending/interrupt behavior, and restart-loss outcomes.
- Current-behavior documentation sync after implemented behavior exists.
- Explicit release/current-state activation gate after final implementation readiness.

Product proceed verdict: Stage 10 passes product owner implementation review with downstream gates preserved.

## Stage 10 Product Re-Review Addendum After Replay TTL And Bounds Repair
Decision: prior Stage 10 product pass remains valid after the replay TTL-on-read and volatile-state bounds repair; not Done, full QA completion, current-behavior docs sync, runtime acceptance, or release approval.

Review date: 2026-06-16.

Reviewed additional delta:
- `../../../../gateway/internal/chatstate/types.go`
- `../../../../gateway/internal/chatstate/store.go`
- `../../../../gateway/internal/chatstate/store_test.go`
- `../tests/test-execution.md`

Product addendum verdict:
- The replay repair closes the product-relevant TTL gap: replay expiration is enforced on read, so stale buffered events cannot remain available merely because no later append occurred.
- The bounds/cleanup repair strengthens the accepted volatile-state boundary: active runs, pending records, and idempotency entries now have explicit caps/cleanup paths, terminal active runs are removed, expired/resolved pending entries are cleaned, completed idempotency records age out, and capacity failures use typed `resource_exhausted` outcomes.
- The repair preserves the core Stage 10 constraints: in-memory process-local correlations only, restart loss remains explicit, no durable gateway chat identity/store/content retention, no raw JSONL/auth/prompt/response/hash/digest retention, and no Stage 05+ runtime behavior is claimed.
- `test-execution.md` stays correctly scoped as partial implementation evidence; it does not claim full QA, Done, release readiness, or current-docs sync.

Findings:
- None product-blocking.

Remaining gates:
- Engineering closure for the replay TTL/bounds repair.
- QA execution beyond partial local checks.
- Stage 05/06/07 runtime wiring must still prove actual side-effect ordering, first-turn acceptance, no empty-chat success, `chat_id == Codex Thread.id`, status/history/events/pending/interrupt behavior, and restart-loss outcomes.
- Current-behavior documentation sync and explicit release/current-state activation remain pending.

## Stage 05 Implementation Product Re-Review After Idempotency Repair
Decision: pass for Stage 05 implementation product acceptance after repair; not Done, full QA completion, current-behavior docs sync, SDK implementation acceptance, or release approval.

Review date: 2026-06-16.

Reviewed implementation slice:
- `../../../../gateway/internal/chatruntime/service.go`
- `../../../../gateway/internal/chatruntime/service_test.go`
- `../../../../gateway/internal/chatstate/types.go`
- `../../../../gateway/internal/chatstate/store.go`
- `../../../../gateway/internal/grpcapi/chat_handlers.go`
- `../../../../gateway/internal/grpcapi/validators.go`
- `../../../../gateway/cmd/codex-runtime-gateway/main.go`
- `../plan/stage-05.md`
- `../tests/test-execution.md`

Product fit verdict:
- The Stage 05 gateway `StartChatRun` boundary preserves the accepted product intent: new chat success requires a non-empty first prompt, Codex `thread/start`, Codex `turn/start`, and observed Thread/Turn ids.
- Successful response keeps `chat_id == Codex Thread.id` and uses `run_id == Codex Turn.id`; no gateway-created durable chat id, SQLite/local mapping store, or gateway-owned history/content is introduced.
- The implementation does not promise Desktop UI visibility, raw JSONL access, SDK completion, remote exposure, current-state readiness, or release activation.
- The P1 found during review is closed: same idempotency key with a different `client_message_id` now conflicts before duplicate Codex side effects instead of returning the prior first-turn result.

Findings:
- None product-blocking after repair.

Remaining gates:
- Stage 06/07 runtime behavior for existing chat lookup, status, history, stream/replay, pending, interrupt, and cancellation.
- Stage 08 Go SDK implementation and examples.
- Full QA execution and evidence capture beyond partial local checks.
- Current-behavior documentation sync after implementation exists.
- Explicit release/current-state activation gate after final implementation readiness.

Product proceed verdict: Stage 05 passes product owner re-review with downstream gates preserved.

## Stage 06 Implementation Product Re-Review
Decision: pass for Stage 06 implementation product acceptance after targeted repairs; not Done, full QA completion, SDK implementation acceptance, current-behavior docs sync, runtime acceptance as a whole, or release approval.

Review date: 2026-06-16.

Reviewed implementation slice:
- `../../../../gateway/internal/chatruntime/service.go`
- `../../../../gateway/internal/chatruntime/errors.go`
- `../../../../gateway/internal/chatruntime/service_test.go`
- `../../../../gateway/internal/chatruntime/stream.go`
- `../../../../gateway/internal/chatstate/store.go`
- `../../../../gateway/internal/chatstate/store_test.go`
- `../../../../gateway/internal/domain/errors.go`
- `../../../../gateway/internal/grpcapi/validators.go`
- `../../../../gateway/internal/grpcapi/validators_test.go`
- `../../../../gateway/internal/grpcapi/chat_handlers.go`
- `../../../../gateway/internal/grpcapi/chat_handlers_test.go`
- `../plan/stage-06.md`
- `../tests/test-execution.md`

Product fit verdict:
- Stage 06 preserves the accepted existing-chat direction: public `chat_id` is the Codex `Thread.id`, and existing-chat operations wrap Codex-backed lookup, continuation, status, history, event stream, replay, and cancellation behavior without creating a gateway-owned durable chat identity.
- The repair keeps gateway state process-local only and does not introduce SQLite, local database mapping, gateway-owned history/content retention, raw JSONL public exposure, Desktop UI visibility promises, remote/shared operation, or release/current-state activation.
- Unknown Codex thread lifecycle now remains honest: the gateway returns a typed retryable unavailable result and preserves local active-run state instead of silently clearing it or fabricating idle success.
- History cursor behavior is product-aligned: cursors are scoped and signed for the local process, tampered or cross-chat reuse is rejected, and old cursors may become invalid after restart without creating a durable replay promise.
- `chat_id` validation now matches current Codex evidence that `Thread.id` is UUID-shaped, which prevents task/run/cursor/idempotency identifiers from being accepted as chat identity.

Findings:
- None product-blocking after repair.

Residual product risk:
- UUID-shaped `chat_id` validation assumes installed Codex keeps `Thread.id` canonical UUID-shaped. If Codex changes that identity format, BRS/SRS/contract validation must be reopened before compatibility is promised.
- Process-local signed history cursors become invalid after gateway restart because the signing key is process-local. This is acceptable for v1 and consistent with the no-durable-replay boundary.

Remaining gates:
- Stage 07 pending/interrupt runtime behavior.
- Stage 08 Go SDK implementation and examples.
- Integration hardening and full QA execution beyond partial local checks.
- Current-behavior documentation sync after implementation exists.
- Explicit release/current-state activation gate after final implementation readiness.

Product proceed verdict: Stage 06 passes product owner re-review with downstream gates preserved.

## Stage 07 Implementation Product Re-Review After Pending/Interrupt Repairs
Decision: pass for Stage 07 implementation product acceptance after targeted repairs; not Done, full QA completion, SDK implementation acceptance, current-behavior docs sync, runtime acceptance as a whole, or release approval.

Review date: 2026-06-16.

Reviewed implementation slice:
- `../../../../gateway/internal/chatstate/store.go`
- `../../../../gateway/internal/chatstate/store_test.go`
- `../../../../gateway/internal/chatruntime/pending_interrupt.go`
- `../../../../gateway/internal/chatruntime/service_test.go`
- `../plan/stage-07.md`
- `../tests/test-execution.md`
- `../README.md`

Product fit verdict:
- Stage 07 preserves the accepted product boundary: pending and interrupt stay Codex-backed and process-local, with no gateway-owned durable chat identity, SQLite/local mapping store, gateway-owned history/content store, raw JSONL public contract, Desktop UI promise, remote/shared scope, or release/current-state activation.
- The repaired pending flow rejects expired/stale pending before Codex write, binds respond-pending idempotency to the concrete `pending_request_id`, avoids content-fingerprint retention in the chat runtime, cleans process-local raw pending records on accepted/stale paths and lazy cleanup triggers, and does not expose stale wrong-shape type mismatch after expiry.
- The repaired interrupt/pending lifecycle preserves `interrupting` during pending response completion and pending creation races by moving the relevant state transitions under `chatstate.Store` mutex-owned helpers.
- Documentation and QA evidence stay appropriately limited: Stage 07 has local test evidence and targeted owner pass recorded, while full QA, SDK acceptance, current-behavior docs sync, Done, and release readiness remain pending.

Findings:
- None product-blocking after repairs.

Remaining gates:
- Stage 08 Go SDK implementation and examples.
- Integration hardening and full QA execution beyond partial local checks.
- Current-behavior documentation sync after implementation exists.
- Explicit release/current-state activation gate after final implementation readiness.

## Stage 08 SDK Implementation Product Re-Review After Repairs
Decision: pass for Stage 08 Go SDK/adoption implementation acceptance after targeted repairs; not Done, full QA completion, current-behavior docs sync, runtime acceptance as a whole, or release approval.

Review date: 2026-06-16.

Reviewed implementation slice:
- `../../../../client.go`
- `../../../../chat.go`
- `../../../../history.go`
- `../../../../stream.go`
- `../../../../pending.go`
- `../../../../types.go`
- `../../../../client_test.go`
- `../../../../examples/api-handler/main.go`
- `../../../../examples/api-handler/main_test.go`
- `../plan/stage-08.md`
- `../tests/test-execution.md`
- `../README.md`

Product fit verdict:
- The SDK exposes the accepted chat-first surface: `Run`, `GetChat`, `chat.Run`, status, history, events stream, pending response helpers, and interrupt helpers over the local gateway.
- SDK public identity remains `chat_id == Codex Thread.id`; generated client message ids, idempotency keys, run ids, cursor ids, and pending ids are not treated as chat identity.
- `Run` and `chat.Run` reject empty prompts before RPC, and successful start/turn calls validate accepted response invariants before returning handles/results.
- Stream close/cancellation remains distinct from interrupt; interrupt is an explicit SDK call.
- The API-handler example stays local and sanitized: loopback-only insecure gRPC dialing, no raw token/private prompt/history/event disclosure, and generic external HTTP error output.
- The slice does not introduce SQLite, durable gateway chat identity, gateway-owned history/content storage, raw JSONL public access, Desktop UI visibility/current-thread promises, remote/shared operation, release activation, or current-state docs claims.

Findings:
- None product-blocking after repairs.

Remaining gates:
- Integration hardening and final end-to-end runtime checks.
- Full QA execution and evidence capture beyond partial local checks.
- Current-behavior documentation sync after final implementation baseline.
- Explicit release/current-state activation gate after final implementation readiness.

## Stage 09 Targeted Product Closeout Recheck After Repair
Decision: pass for Stage 09 targeted product closeout after the active-run capacity and evidence repair; not Done, full QA completion, current-behavior docs sync, runtime acceptance as a whole, or release approval.

Review date: 2026-06-16.

Reviewed repair delta:
- `../../../../gateway/internal/chatruntime/service.go`
- `../../../../gateway/internal/chatstate/store.go`
- `../../../../gateway/internal/chatstate/store_test.go`
- `../../../../gateway/internal/chatruntime/service_test.go`
- `../plan/stage-09.md`
- `../tests/test-execution.md`
- `../README.md`

Product fit verdict:
- `StartChatRun` now reserves active-run capacity before app-server `Connection`, `thread/start`, or `turn/start`, then converts the reservation only after Codex returns `chat_id` and `run_id`.
- The repair strengthens the accepted product boundary by preventing local-capacity exhaustion from creating untracked Codex thread/turn side effects.
- The docs delta remains scoped to local checks, proto sync, and Go/source-code guardrails, without claiming Done, full QA, current-behavior docs, or release/current-state activation.

Findings:
- None product-blocking.

Residual gates:
- Full `TC-001`..`TC-036` QA execution remains pending.
- Current-behavior docs sync remains pending, including reconciliation of draft config docs against actual config defaults before release/current-state docs.
- Release/current-state activation remains deferred until explicit final readiness approval.
