# Release/Ops Review: Chat-First Runtime

Type: release/ops review
Artifact purpose / consumer: независимое pre-implementation release/ops решение по paper solution для chat-first runtime; не является release activation, current-state acceptance или live rollout plan.
Status: pass
Reviewer: release_ops_owner
Last reviewed: 2026-06-14
Related docs: `../BRS/feature.md`, `../BRS/nfr.md`, `../SRS/index.md`, `../SRS/feature.md`, `../SRS/contracts.md`, `../SRS/events-history.md`, `../SRS/transient-correlations.md`, `../SRS/sequences.md`, `../SRS/traceability.md`, `../SRS/rollout.md`, `../tech-design/tech-design.md`, `../tech-design/adr-001-dedicated-chat-first-service.md`, `../../../product-docs/configuration/gateway-runtime-config.md`, `../../../product-docs/operations/local-gateway-runbook.md`, `../../../product-docs/observability/event-stream-observability.md`, `../../../product-docs/grpc/codex-runtime-gateway.md`
Trace IDs: `BRS-NFR-003`, `BRS-NFR-004`, `BRS-NFR-008`, `SRS-FR-017`, `SRS-FR-019`, `SRS-NFR-002`, `SRS-NFR-003`, `SRS-NFR-004`, `SRS-NFR-008`, `DOC-CONFIG-001`, `DOC-OBS-001`, `DOC-OPS-001`, `DOC-GRPC-001`

## Source Of Truth
Этот review фиксирует только release/ops verdict по бумажному решению. Он не утверждает кодовую готовность, build readiness, release readiness, deployment readiness, migration readiness или текущий production/local current state.

## Scope Reviewed
- inactive release/current-state stance до финальной реализации и отдельного release readiness;
- local-only operation, loopback boundary, config precedence, token-source handling;
- `chat_runtime.enabled` disable path и coexistence с existing task RPCs;
- readiness/health semantics, lazy app-server supervisor, typed `UNAVAILABLE` при startup/dependency failure;
- restart semantics для process-local replay/pending/idempotency/correlation;
- rollback/rollforward guardrails без порчи Codex-owned state и task compatibility;
- достаточность target/future docs как draft paper docs, а не release facts;
- отсутствие promises про migration, SQLite, durable store, release deployment.

## Decision
pass

## Evidence
- Inactive release stance выдержана последовательно: BRS прямо оставляет release artifacts inactive до final implementation и explicit release readiness work, SRS rollout отдельно запрещает трактовать пакет как release/current-state milestone, а tech design и target ops/obs docs не объявляют release facts (`../BRS/feature.md` lines 59-68, 102, 153; `../BRS/nfr.md` lines 19, 69, 80, 88; `../SRS/rollout.md` lines 15-30, 89-103; `../../../product-docs/operations/local-gateway-runbook.md` lines 13, 59; `../../../product-docs/observability/event-stream-observability.md` lines 13, 45-46).
- Local-only boundary и loopback/token-source contract описаны достаточно жестко для ops-level draft: exact-one token source ограничен env name или absolute file path, external credential-provider command execution исключен, non-secret env overrides не разрешены, `listen` ограничен loopback (`../SRS/contracts.md` lines 96-115; `../SRS/rollout.md` lines 52-60, 78-87; `../tech-design/tech-design.md` lines 148-190; `../../../product-docs/configuration/gateway-runtime-config.md` lines 15-54).
- Disable/coexistence path согласован: `chat_runtime.enabled=false` выключает или не регистрирует `ChatRuntimeService`, переводит его readiness в `NOT_SERVING`/equivalent с reason `chat_runtime_disabled`, при этом task RPC surface может оставаться healthy и не должен молча менять свою семантику (`../SRS/rollout.md` lines 43-50, 93-94; `../tech-design/tech-design.md` lines 186-190, 230-236; `../../../product-docs/configuration/gateway-runtime-config.md` lines 22, 50-54; `../../../product-docs/operations/local-gateway-runbook.md` lines 49-53; `../../../product-docs/observability/event-stream-observability.md` lines 37-40; `../../../product-docs/grpc/codex-runtime-gateway.md` lines 19-45, 81-84).
- Readiness/health semantics и lazy supervisor описаны без преждевременных release promises: readiness зависит от valid config, token source, loopback listen, session groups и Codex binary identity; lazy app-server child не обязан работать до readiness; startup/dependency failure должен становиться typed `UNAVAILABLE` и observable (`../SRS/rollout.md` lines 35-40, 89-103; `../tech-design/tech-design.md` lines 164-210; `../../../product-docs/observability/event-stream-observability.md` lines 37-40; `../../../product-docs/operations/local-gateway-runbook.md` lines 26-40; `../../../product-docs/grpc/codex-runtime-gateway.md` lines 63-79).
- Restart semantics сформулированы честно и без faux recovery: после restart теряются replay, pending correlation, idempotency и active-run correlation; сохраняется только возможность повторно использовать app-stored `chat_id` как Codex `Thread.id`, если Codex реально может доказать state; pre-restart replay должен стать `unavailable`/`out_of_range`/`narrowed`/`unknown`, а не подделываться (`../BRS/feature.md` lines 38-39, 121-122, 131-132; `../BRS/nfr.md` lines 29-31; `../SRS/feature.md` lines 45-47, 54, 59; `../SRS/events-history.md` lines 42-56; `../SRS/transient-correlations.md` lines 19-53; `../SRS/sequences.md` lines 70-113; `../tech-design/tech-design.md` lines 115-146, 200-205; `../../../product-docs/operations/local-gateway-runbook.md` lines 44-53).
- Rollback/rollforward constraints достаточны для paper stage: rollback не должен подменять `chat_id` task identity, не должен мутировать Codex-owned thread/history state и не должен ломать task compatibility surface; rollforward обязан сохранять либо явно мигрировать public error/event/status semantics, а неизвестное local state после restart/upgrade переводить в typed unknown/unavailable (`../BRS/nfr.md` lines 52-60; `../SRS/contracts.md` lines 117-121; `../SRS/rollout.md` lines 68-77; `../tech-design/tech-design.md` lines 230-236; `../../../product-docs/operations/local-gateway-runbook.md` lines 49-53; `../../../product-docs/grpc/codex-runtime-gateway.md` lines 81-84).
- Draft target docs достаточно четко помечены как future/draft paper docs и не выдают себя за release facts: SRS traceability прямо описывает `DOC-CONFIG-001`, `DOC-OBS-001`, `DOC-OPS-001`, `DOC-GRPC-001` как draft target/future surfaces и не release/current-state evidence (`../SRS/index.md` lines 11, 16, 18, 77; `../SRS/traceability.md` lines 69-104; `../../../product-docs/configuration/gateway-runtime-config.md` line 13; `../../../product-docs/operations/local-gateway-runbook.md` line 13; `../../../product-docs/observability/event-stream-observability.md` line 13; `../../../product-docs/grpc/codex-runtime-gateway.md` line 14).
- Migration/SQLite/durable store/release deployment promises не введены: BRS, SRS, tech design и ADR последовательно запрещают durable gateway identity store, SQLite, local database, retained content store и release/deployment promises в текущем пакете (`../BRS/feature.md` lines 53, 74, 90, 148-149; `../SRS/index.md` lines 18, 21, 65; `../SRS/transient-correlations.md` lines 11-18, 55-57; `../tech-design/tech-design.md` lines 123-133, 225-228; `../tech-design/adr-001-dedicated-chat-first-service.md` lines 41-46, 69-75, 100-103).

## Findings / Questions
- Блокирующих release/ops findings не найдено в рамках paper review scope.
- Неблокирующий watch item для будущей реализации: если `ChatRuntimeService` будет именно omitted, а не merely disabled, health surface должен все равно давать однозначный equivalent disabled state, чтобы оператор не видел двусмысленное `unknown service` вместо осознанного `chat_runtime_disabled`. Текущий paper package это допускает формулировкой `NOT_SERVING or equivalent`, но implementation review должен зафиксировать конкретную форму.

## Residual Risks
- До реализации и local validation остаётся риск drift между planned health semantics и фактическим gRPC health surface.
- Thresholds и backoff значения пока paper-only; их практическая пригодность не подтверждена build/run evidence.
- Shared app-server dependency между chat-first и task compatibility surfaces потребует особенно аккуратной implementation review, чтобы failure isolation соответствовала заявленным guardrails.

## Next Gates
- security/privacy/data review для local auth, secret handling, redaction и pending safety.
- QA readiness re-review для restart/replay/pending/interrupt/compatibility coverage.
- architecture/proto follow-up для exact chat-first service shape и explicit coexistence with `codex.control.v1.CodexControl`.
- implementation + local validation review для concrete health surface, disable semantics, supervisor/backoff behavior и typed startup failure handling.
- отдельный release readiness gate только после реализации; этот review его не заменяет.

## Forbidden Content
- Любое чтение этого review как approval на release activation, deployment, migration или current-state acceptance.
- Любое добавление из этого review promises про production SLO/SLA, remote exposure, SQLite/durable gateway store или release facts.
- Raw secrets, auth headers, token values, cookies, private keys, raw environment values, raw JSONL или private data.

## Stage 09 Targeted Release/Ops Closeout After Repair

Decision: pass for Stage 09 targeted release/ops closeout after the active-run capacity and evidence repair; not release readiness, current-state acceptance, deployment approval, production approval, or Done.

Review date: 2026-06-16.

Release/ops verdict:
- The capacity repair is operationally safer: local active-run capacity is reserved before app-server `Connection`, `thread/start`, or `turn/start`, preventing capacity exhaustion from creating untracked Codex side effects.
- Proto generation sync and local verification are recorded as local evidence only, without changing the inactive release/current-state stance.
- Stage 09 docs still keep full QA, current-behavior docs sync, and explicit release/current-state activation gates pending.

Release/ops blockers:
- None.

Residual gates:
- Full QA execution and evidence capture.
- Current-behavior docs sync against the final implementation baseline.
- Release/current-state activation only after explicit final readiness approval.
