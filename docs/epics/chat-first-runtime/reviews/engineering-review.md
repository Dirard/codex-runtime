# Engineering Review: Chat-First Runtime

mode: fresh_review
status: clean
reviewer: engineering_reviewer
review date: 2026-06-14
review scope: paper QA-basis quality review only; no code, proto, generated files, runtime behavior, release artifacts, or executed tests reviewed
independence: this reviewer did not author or repair the substantive QA/source artifacts under review

## scope_checked

- `docs/epics/chat-first-runtime/BRS/feature.md`
- `docs/epics/chat-first-runtime/BRS/nfr.md`
- `docs/epics/chat-first-runtime/SRS/**`
- `docs/epics/chat-first-runtime/tech-design/tech-design.md`
- `docs/epics/chat-first-runtime/tech-design/adr-001-dedicated-chat-first-service.md`
- `docs/epics/chat-first-runtime/reviews/product-review.md`
- `docs/epics/chat-first-runtime/reviews/security-privacy-data-review.md`
- `docs/epics/chat-first-runtime/reviews/release-ops-review.md`
- `docs/epics/chat-first-runtime/tests/test-strategy.md`
- `docs/epics/chat-first-runtime/tests/test-cases.md`
- `docs/epics/chat-first-runtime/tests/regression.md`
- `docs/epics/chat-first-runtime/tests/test-execution.md`
- `docs/epics/chat-first-runtime/reviews/qa-readiness.md`

## engineering_axes

- bugs_regressions: pass for paper QA basis. The repaired case catalog and regression scope cover the accepted BRS/SRS/security/release/architecture risk surfaces without asserting execution.
- readability: pass. The QA artifacts are readable, grouped by behavior area, and use stable `TC-*`, `REG-*`, and `QA-EVID-*` references.
- architecture: pass for paper traceability. Coverage follows the accepted dedicated chat-first service boundary, `chat_id == Codex Thread.id`, process-local state, no raw JSONL public contract, and local-only/release-inactive constraints.
- naming: pass. Test IDs, evidence IDs, data families, and regression IDs are consistent enough for future execution planning.
- comments: not applicable beyond document notes; risk and forbidden-content notes are useful and not misleading.
- maintainability: pass. The test basis is granular enough to maintain future implementation coverage without a hidden dependency on plan-stage artifacts.
- hidden_executor_decisions: none found. Residual risks are recorded as future implementation proof obligations, not as paper blockers or silent executor choices.

## code_quality_checked

- bugs: not applicable; no code was reviewed.
- readability: not applicable to code; document readability reviewed.
- architecture: not applicable to implementation; paper architecture traceability into QA reviewed.
- naming: not applicable to code; QA identifier naming reviewed.
- comments: not applicable to code.

## findings

None.

## repair_recommendation

none

## rejected_or_not_repeated

None provided by root.

## coverage_gaps

- No implementation engineering review was performed. Future code/proto/config/test implementation must still receive engineering review.
- No test execution was reviewed. `test-execution.md` remains intentionally `not started` / `not executed`.
- Product-doc current-behavior sync and release readiness were not reviewed as completed gates.

## checks_or_sources_reviewed

- Verified the QA basis keeps `test-execution.md` not executed: `tests/test-execution.md` lines 5, 11-16, 36, and 41-42; `tests/test-strategy.md` lines 15-19, 32-37, and 130-135; `reviews/qa-readiness.md` lines 11-12, 25-30, 39-41, and 53-60.
- Verified no PLAN-STAGE or implementation-plan dependency in the test basis: matches in `tests/test-strategy.md` and `tests/test-cases.md` are prohibitions/out-of-scope only.
- Verified high-risk coverage spans identity/start/get/run, status/history/stream/replay, pending/interrupt/idempotency/restart, auth/authz/redaction, config/readiness/coexistence, release non-promises, Desktop UI non-promise, and no SQLite/durable mapping/content store: `tests/test-cases.md` lines 15-73 and `tests/regression.md` lines 14-41.
- Verified security/privacy/data and release/ops paper review risks are represented in QA: `reviews/security-privacy-data-review.md` lines 35-45 and 85-96; `reviews/release-ops-review.md` lines 11-22, 37-51; `reviews/qa-readiness.md` lines 32-51.
- Verified raw secrets/raw JSONL/private content are only discussed as forbidden/prohibited evidence or redaction fixtures, not as raw examples: `tests/test-strategy.md` lines 76-80, 116-120, and 137-140; `tests/test-cases.md` lines 75-77; `tests/test-execution.md` lines 47-49.
- Verified owner statuses do not pretend implementation, release, current-state, or Done readiness: `reviews/product-review.md` lines 80-116; `reviews/security-privacy-data-review.md` lines 14-16 and 98-110; `reviews/release-ops-review.md` lines 11-12 and 46-56; `reviews/qa-readiness.md` lines 53-65.

## summary_for_root

The repaired paper QA basis passes engineering quality review for maintainability, traceability, contradiction control, high-risk coverage, status honesty, and lack of accidental plan-stage dependency.

No source QA docs were repaired by this reviewer. No code was reviewed. This clean result supports the paper QA-basis gate only; implementation engineering review remains a future gate after concrete implementation artifacts exist.

Next gate: docs sync / implementation planning may proceed from the accepted BRS/SRS and repaired QA paper basis, while `test-execution.md` must remain not executed until implementation references exist.

## Targeted Plan Engineering Recheck

mode: targeted_recheck
status / decision: clean for the two original P1 findings; targeted only; not a broad fresh review; not implementation approval
review date: 2026-06-15
review scope: targeted closure check for the two original P1 findings after plan/SRS/QA repairs; no source code, proto, runtime behavior, release artifact, or executed test review

### original P1 findings

- P1 #1 supervisor/backoff route ordering: closed. `stage-03.md` now defines the minimal lazy `codex.exe app-server` supervisor foundation before Codex side effects or Codex-backed reads; `index.md` places Stage 03 before Stage 05 and Stage 07; `stage-05.md` depends on that foundation before `thread/start`; `stage-07.md` uses it before pending response and interrupt Codex calls.
- P1 #2 post-restart idempotency contradiction: closed. SRS and plan semantics now limit duplicate suppression to side-effecting requests tracked by the current gateway process. A pre-restart idempotency key alone provides no replay, recovery, exactly-once delivery, prior-result proof, terminal-state proof, or no-duplicate guarantee. QA covers the same-process retry case separately from post-restart key-only retry.

### findings

None for this targeted recheck.

### residual gates / limits

- This targeted recheck did not perform a broad fresh review and does not approve implementation.
- Remaining gates are plan review, root-approved implementation, post-implementation product and engineering review, QA execution/evidence capture, current-behavior docs sync, and explicit release/current-state activation if later requested.
- Implementation must still prove the current-process idempotency, supervisor/backoff, restart-loss, pending, and interrupt behavior in code and tests.

## Stage 02 Implementation Engineering Review

mode: implementation_engineering_review
status / decision: pass for Stage 02 implementation slice; not runtime acceptance, full QA, Done, or release approval
review date: 2026-06-15
review scope: Stage 02 proto/generated/contract-test/docs evidence only; no unrelated staged/renamed workspace changes reviewed

### scope_checked

- `proto/codex_control/v1/codex_control.proto`
- `gen/codex/control/v1/codex_control.pb.go`
- `gen/codex/control/v1/codex_control_grpc.pb.go`
- `gateway/internal/grpcapi/chat_contract_test.go`
- `docs/epics/chat-first-runtime/tests/test-execution.md`

### engineering_axes

- bugs_regressions: pass. Existing `CodexControl` task service surface remains present; `ChatRuntimeService` is separate and exposes the accepted 8 chat RPCs.
- readability: pass. The proto and descriptor tests are readable and focused for this contract slice; generated files look normal for regenerated protobuf output.
- architecture: pass. The chat contract is separated from the task compatibility surface and represents `chat_id`, `run_id`, idempotency, process-local state, restart loss, and typed outcomes without reintroducing a durable gateway store or gateway-owned history.
- naming: pass.
- comments: pass / not material for generated code.
- maintainability: pass. Descriptor tests are useful guardrails, and exact method checks are appropriate for this Stage 02 contract slice.
- hidden_executor_decisions: none found.

### findings

None.

### repair_recommendation

none

### coverage_gaps

- Runtime behavior beyond Stage 02 contract/generated/test/docs was not approved by this review.
- Full QA remains pending; `test-execution.md` records partial Stage 02 evidence only.
- Later stages must still prove no-empty-chat success prevention, `chat_id == Codex Thread.id` at runtime, process-local correlation, pending, interrupt, restart-loss, and typed unsupported/unavailable/unknown/narrowed behavior.

### checks_or_sources_reviewed

- Proto service split and chat RPC list in `proto/codex_control/v1/codex_control.proto`.
- `ChatEvent` contract does not include `UnknownRawEvent`, `payload_json`, or another raw app-server payload escape hatch; the older raw task compatibility event remains outside the chat-first contract.
- Generated gRPC surfaces contain both `CodexControl` and `ChatRuntimeService`.
- Descriptor tests in `gateway/internal/grpcapi/chat_contract_test.go` cover service split, method lists, chat identity/error fields, and no raw chat event payload escape hatch.
- `GOPROXY=off go test ./...` passed for the repository after Stage 02 implementation.
- Reviewer also ran `GOPROXY=off go test -count=1 ./gateway/internal/grpcapi`, which passed.

### summary_for_root

Stage 02 passes engineering implementation review. No required repairs were found. Root may proceed to Stage 03 after recording this review outcome and preserving the remaining runtime/QA/docs/release gates.

## Stage 03 Implementation Engineering Recheck After Supervisor Repair

mode: targeted_recheck
status / decision: pass for latest Stage 03 repair scope; not Done, full QA, release approval, or current-docs sync
review date: 2026-06-16
review scope: supervisor startup timeout repair, supervisor tests, dynamic chat health/backoff repair, targeted Stage 03 evidence update

### scope_checked

- `../../../../gateway/internal/appserver/supervisor.go`
- `../../../../gateway/internal/appserver/supervisor_test.go`
- `../../../../gateway/internal/grpcapi/health.go`
- `../../../../gateway/internal/grpcapi/server.go`
- `../../../../gateway/internal/grpcapi/server_test.go`
- `../tests/test-execution.md`

### findings

None.

### checks run

- `GOPROXY=off go test -count=1 ./gateway/internal/appserver ./gateway/internal/grpcapi ./gateway/internal/config` passed.

### closure

The previous P1 is closed: supervisor startup timeout now remains an upper bound when the caller deadline is longer, while shorter caller deadlines still take precedence. Chat health now reflects supervisor backoff/closed status without starting Codex, and task RPC compatibility remains preserved.

### remaining gates

Full QA execution, current-behavior docs sync, Done approval, and release/current-state activation remain pending and are not approved by this targeted recheck.

## Stage 05 Implementation Engineering Recheck After Idempotency Repair

mode: targeted_recheck
status / decision: pass for the Stage 05 idempotency repair; not Done, full QA, release approval, current-docs sync, SDK acceptance, or a broad fresh re-review
review date: 2026-06-16
review scope: previous Stage 05 P1 and immediate regressions from the repair

### scope_checked

- `../../../../gateway/internal/chatstate/types.go`
- `../../../../gateway/internal/chatruntime/service.go`
- `../../../../gateway/internal/chatruntime/service_test.go`
- `../tests/test-execution.md`

### findings

None.

### closure

The previous P1 is closed: `IdempotencyScope` now includes `ClientMessageID`, `StartChatRun` fills it from `command.ClientMessageID`, and the store's full-scope comparison returns `idempotency_scope_mismatch` for same-key reuse with a different first-turn correlation id. The new regression test verifies no duplicate `thread/start` or `turn/start` calls occur for that conflict.

### checks run

- Root ran `GOPROXY=off go test -count=1 ./gateway/internal/chatruntime ./gateway/internal/chatstate`, `GOPROXY=off go test ./...`, and `git diff --check`.
- Engineering reviewer independently ran `GOPROXY=off go test -count=1 ./gateway/internal/chatruntime ./gateway/internal/chatstate`.

### remaining gates

Full QA execution, current-behavior docs sync, Done approval, SDK implementation acceptance, and release/current-state activation remain pending and are not approved by this targeted recheck.

## Stage 10 Implementation Engineering Recheck After Replay TTL Repair

mode: targeted_recheck
status / decision: pass for latest Stage 10 repair scope; not Done, full QA, release approval, or current-docs sync
review date: 2026-06-16
review scope: replay TTL-on-read repair, volatile active/pending/idempotency caps and cleanup, targeted Stage 10 evidence update

### scope_checked

- `../../../../gateway/internal/chatstate/types.go`
- `../../../../gateway/internal/chatstate/store.go`
- `../../../../gateway/internal/chatstate/store_test.go`
- `../tests/test-execution.md`

### findings

None.

### checks run

- `GOPROXY=off go test -count=1 ./gateway/internal/chatstate ./gateway/internal/domain ./gateway/internal/grpcapi` passed.

### closure

The previous P1 is closed: `Replay` now enforces replay limits under lock before computing replay results, deletes emptied replay buffers, and returns typed `replay_unavailable` after idle TTL expiry. Added tests cover TTL-on-read without append and volatile-state caps/cleanup for active runs, pending requests, and idempotency entries.

### remaining gates

Full QA execution, current-behavior docs sync, Done approval, and release/current-state activation remain pending and are not approved by this targeted recheck.

## Stage 06 Implementation Engineering Recheck After Existing-Chat Repairs

mode: targeted_recheck
status / decision: pass for the latest Stage 06 repair scope; not Done, full QA, release approval, current-docs sync, SDK acceptance, or a broad fresh review
review date: 2026-06-16
review scope: unknown lifecycle handling, idempotency operation namespace, terminal stream subscription race, HMAC history cursor integrity, canonical Codex thread id validation, and immediate regressions from those repairs

### scope_checked

- `../../../../gateway/internal/chatruntime/service.go`
- `../../../../gateway/internal/chatruntime/errors.go`
- `../../../../gateway/internal/chatruntime/service_test.go`
- `../../../../gateway/internal/chatstate/store.go`
- `../../../../gateway/internal/chatstate/store_test.go`
- `../../../../gateway/internal/domain/errors.go`
- `../../../../gateway/internal/grpcapi/validators.go`
- `../../../../gateway/internal/grpcapi/validators_test.go`
- `../../../../gateway/internal/grpcapi/chat_handlers_test.go`

### findings

None for this targeted recheck.

### closure

The previous Stage 06 engineering blockers are closed. Unknown Codex lifecycle now returns typed `chat_state_unavailable` without clearing local active state; idempotency raw-key lookup no longer lets different operations share a raw key silently; live stream subscription now refuses already-terminal/inactive runs under lock; history cursors are signed and scoped before use; and chat validators require canonical UUID-shaped Codex thread ids.

### checks run

- Root ran `GOPROXY=off go test -count=1 ./gateway/internal/chatstate ./gateway/internal/chatruntime ./gateway/internal/domain ./gateway/internal/grpcapi`, `GOPROXY=off go test -count=1 ./...`, and `git diff --check`.
- Engineering reviewer performed a targeted code recheck and accepted the root-reported test evidence.

### residual notes

- This was a targeted recheck, not a broad fresh review of every Stage 06 file.
- Full QA execution, current-behavior docs sync, Done approval, SDK implementation acceptance, and release/current-state activation remain pending and are not approved by this targeted recheck.

## Stage 07 Implementation Engineering Recheck After Pending/Interrupt Repairs

mode: targeted_recheck
status / decision: pass for the latest Stage 07 repair scope; not Done, full QA, release approval, current-docs sync, SDK acceptance, or a broad fresh review
review date: 2026-06-16
review scope: pending expiry before side effects, respond-pending idempotency scope, content-fingerprint removal from chat runtime, pending completion/creation interrupt races, and stale raw pending cleanup

### scope_checked

- `../../../../gateway/internal/chatstate/types.go`
- `../../../../gateway/internal/chatstate/store.go`
- `../../../../gateway/internal/chatstate/store_test.go`
- `../../../../gateway/internal/chatruntime/pending_interrupt.go`
- `../../../../gateway/internal/chatruntime/service_test.go`
- `../plan/stage-07.md`
- `../tests/test-execution.md`

### findings

None for this targeted recheck.

### closure

The Stage 07 engineering blockers are closed in the targeted scope. Pending expiry is checked before Codex write; respond-pending idempotency scope includes `pending_request_id`; pending response content fingerprints are not retained in the chat runtime path; pending completion uses a store-level transition that preserves `interrupting`; pending creation uses a store-level atomic register-and-transition helper that refuses to claim after `interrupting` or terminal state wins; and stale raw pending records are pruned on lazy pending/status triggers.

### checks run

- Root ran `GOPROXY=off go test -count=1 .\gateway\internal\chatstate .\gateway\internal\chatruntime`, `GOPROXY=off go test -count=1 .\gateway\internal\appserver .\gateway\internal\chatstate .\gateway\internal\chatruntime .\gateway\internal\grpcapi`, `GOPROXY=off go test -count=1 ./...`, and `git diff --check`.
- Engineering reviewer independently ran `GOPROXY=off go test -count=1 .\gateway\internal\chatstate .\gateway\internal\chatruntime`.

### residual notes

- This was a targeted recheck, not a broad fresh review of every Stage 07 file.
- Concurrency confidence is based on store mutex ownership and focused tests, not a race/stress suite.
- Full QA execution, current-behavior docs sync, Done approval, SDK implementation acceptance, and release/current-state activation remain pending and are not approved by this targeted recheck.

## Stage 08 SDK Implementation Engineering Recheck After Repairs

mode: targeted_recheck
status / decision: pass for the latest Stage 08 SDK repair scope; not Done, full QA, release approval, current-docs sync, runtime acceptance as a whole, or a broad fresh review
review date: 2026-06-16
review scope: SDK-side empty prompt validation, accepted-response invariants, required generated IDs, stream close cancellation, status snapshot safety, error mapping, metadata handling, and example hardening

### scope_checked

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

### findings

None for this targeted recheck.

### closure

The Stage 08 engineering blockers are closed in the targeted scope. SDK `Run` and `chat.Run` reject empty prompts before RPC and validate accepted start/turn response invariants; side-effecting helpers generate required client ids and idempotency keys when omitted; `EventStream.Close` cancels the stream context without interrupt; cached chat status/capabilities are mutex-protected snapshots; gRPC status details map into stable SDK errors including gateway fallback details and resource-exhausted normalization; caller outgoing metadata is not copied into SDK calls; token-bearing string forms are redacted; and the example guards insecure dialing to loopback addresses.

### checks run

- Root ran `GOPROXY=off go test -count=1 .`, `GOPROXY=off go test -count=1 .\examples\api-handler`, `GOPROXY=off go test -count=1 ./...`, `GOPROXY=off go list ./...`, `GOPROXY=off go vet ./...`, and `git diff --check`.
- Engineering reviewer performed a targeted code/test recheck and accepted the root-reported final full-suite evidence.

### residual notes

- This was a targeted recheck, not a broad fresh review of every runtime gateway file.
- A full integration/e2e gate, full QA execution, current-behavior docs sync, Done approval, and release/current-state activation remain pending and are not approved by this targeted recheck.

## Stage 09 Integration Engineering Recheck After Repair

mode: targeted_recheck
status / decision: pass for the Stage 09 active-run capacity, proto-sync, and evidence repair scope; not Done, full QA, release approval, current-docs sync, runtime acceptance as a whole, or a broad fresh review
review date: 2026-06-16
review scope: prior Stage 09 P1/P2 findings and immediate regressions from the repair

### scope_checked

- `../../../../gateway/internal/chatruntime/service.go`
- `../../../../gateway/internal/chatstate/store.go`
- `../../../../gateway/internal/chatstate/store_test.go`
- `../../../../gateway/internal/chatruntime/service_test.go`
- `../plan/stage-09.md`
- `../tests/test-execution.md`

### findings

None for this targeted recheck.

### closure

The prior Stage 09 engineering findings are closed in the targeted scope. `StartChatRun` reserves active-run capacity before app-server connection/thread/turn side effects, converts the reservation into a tracked active run only after Codex returns ids, and releases the reservation on failure paths. Stage 09 evidence now scopes legacy-name/import guardrails to Go/source-code implementation paths and records the proto generation sync check.

### checks run

- Root ran `GOPROXY=off go test -count=1 .\gateway\internal\chatstate .\gateway\internal\chatruntime`, `GOPROXY=off go test -count=1 . .\examples\api-handler .\gateway\internal\appserver .\gateway\internal\chatstate .\gateway\internal\chatruntime .\gateway\internal\grpcapi`, `powershell -ExecutionPolicy Bypass -File .\scripts\generate-proto.ps1`, `gofmt -w .\gen`, `GOPROXY=off go test -count=1 ./...`, `GOPROXY=off go list ./...`, `GOPROXY=off go vet ./...`, `git diff --check`, and scoped file-tools guardrail grep checks.
- Engineering reviewer independently ran `GOPROXY=off go test -count=1 .\gateway\internal\chatstate .\gateway\internal\chatruntime`.

### residual notes

- This was a targeted recheck, not a broad fresh review of every Stage 09 or runtime file.
- Full QA execution, current-behavior docs sync, Done approval, and release/current-state activation remain pending and are not approved by this targeted recheck.
