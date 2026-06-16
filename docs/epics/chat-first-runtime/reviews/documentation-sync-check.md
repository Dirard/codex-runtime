# Documentation Sync Check: Chat-First Runtime

Type: documentation sync check
Artifact purpose / consumer: current-behavior documentation sync and traceability verdict for the implemented chat-first runtime working-copy baseline.
Status: current-behavior docs sync pass for the working-copy baseline; full QA and release/current-state pending
Reviewer: docs_writer
Last repaired: 2026-06-16
Related docs: `../BRS/feature.md`, `../BRS/nfr.md`, `product-review.md`, `engineering-review.md`, `qa-readiness.md`, `release-ops-review.md`, `security-privacy-data-review.md`, `../SRS/index.md`, `../SRS/feature.md`, `../SRS/contracts.md`, `../SRS/states-and-outcomes.md`, `../SRS/transient-correlations.md`, `../SRS/events-history.md`, `../SRS/sequences.md`, `../SRS/nfr-mapping.md`, `../SRS/traceability.md`, `../SRS/rollout.md`, `../tech-design/tech-design.md`, `../tech-design/adr-001-dedicated-chat-first-service.md`, `../plan/index.md`, `../plan/stage-01.md`, `../plan/stage-02.md`, `../plan/stage-03.md`, `../plan/stage-05.md`..`../plan/stage-10.md`, `../tests/test-strategy.md`, `../tests/test-cases.md`, `../tests/regression.md`, `../tests/test-execution.md`, `../../../product-docs/index.md`, `../../../product-docs/doc-debt-register.md`, `../../../product-docs/sdk/index.md`, `../../../product-docs/sdk/chat-first-go-sdk.md`, `../../../product-docs/grpc/index.md`, `../../../product-docs/grpc/codex-runtime-gateway.md`, `../../../product-docs/domain/index.md`, `../../../product-docs/domain/chat-runtime.md`, `../../../product-docs/security/index.md`, `../../../product-docs/security/local-runtime-boundary.md`, `../../../product-docs/configuration/index.md`, `../../../product-docs/configuration/gateway-runtime-config.md`, `../../../product-docs/observability/index.md`, `../../../product-docs/observability/event-stream-observability.md`, `../../../product-docs/operations/index.md`, `../../../product-docs/operations/local-gateway-runbook.md`
Trace IDs: DOC-SDK-001, DOC-GRPC-001, DOC-DOMAIN-001, DOC-SEC-001, DOC-CONFIG-001, DOC-OBS-001, DOC-OPS-001, PLAN-STAGE-001..PLAN-STAGE-003, PLAN-STAGE-005..PLAN-STAGE-010, TC-001..TC-036

## Source Of Truth
This check records documentation sync, navigation, and traceability status for the implemented working-copy baseline. It describes current implemented behavior only. It does not claim full QA completion, release readiness, production readiness, remote/shared support, or release/current-state activation.

## Inputs Reviewed
- Accepted BRS baseline: `../BRS/feature.md`, `../BRS/nfr.md`
- Product review pass, QA-basis product sanity pass, and targeted product re-review for SRS/plan/QA repairs: `product-review.md`
- Accepted SRS bundle and traceability set
- Paper architecture gate inputs: `../tech-design/tech-design.md`, `../tech-design/adr-001-dedicated-chat-first-service.md`
- Accepted paper reviews plus targeted engineering recheck: `engineering-review.md`, `qa-readiness.md`, `release-ops-review.md`, `security-privacy-data-review.md`
- QA basis: `../tests/test-strategy.md`, `../tests/test-cases.md`, `../tests/regression.md`, `../tests/test-execution.md`
- Touched product-doc surfaces and navigation indexes under `../../../product-docs/**`
- Implemented working-copy behavior in the gateway config, SDK, chat runtime, gRPC handlers, health service, and proto surface

## Documentation Sync Result
- Touched product-doc surfaces now describe the implemented working-copy behavior instead of staying in draft-target wording.
- Navigation now points to the synced current-behavior product-doc surfaces, the accepted BRS/SRS/plan package, the QA basis, and the remaining downstream gates.
- The removed DB/data-store surface is not present under `docs/product-docs/`; no `docs/product-docs/db` directory or orphan navigation link was found in the reviewed docs scope.
- Reviewed docs do not reintroduce stale SQLite, durable mapping, gateway-created chat-id, persistence, or `stage-04` references in the synced navigation surfaces.
- Navigation/status wording stays aligned with the implemented baseline: Stage 09 targeted owner closeout is passed, full QA remains pending, current-behavior docs sync is now recorded, and release/current-state remains deferred.
- Synced docs preserve the implemented guardrails: `chat_id == Codex Thread.id`, no empty-chat success promise, no Desktop UI visibility promise, no raw JSONL public escape hatch, process-local replay/pending/idempotency state only, and no durable gateway-owned content or identity store.
- The config docs now match the implemented defaults and constants, including `chat_runtime.enabled=true` when omitted, loopback-only `listen` validation, and the current supervisor startup/backoff behavior.

## Residual Risks
- `../tests/test-execution.md` records partial/local implementation evidence only; this pass does not imply full `TC-001`..`TC-036` execution or final QA evidence.
- Release/current-state docs remain deferred and inactive; this pass is not release readiness or current-state acceptance.
- If code behavior changes again before full QA, product-doc current-behavior sync must be rerun so the QA baseline does not drift.
- Existing doc debt outside this write scope remains open for root `README.md` and `proto/codex_control/v1/README.md`.

## Decision
pass for current-behavior docs sync of the implemented working-copy baseline; not full QA, Done, release readiness, or release/current-state activation

## Next Recommended Gate
Full QA execution against this synced baseline, then release/current-state activation only if the later explicit gates are opened and passed.

## Rerun Trigger
Rerun this check after any touched product-doc or epic-doc behavior changes, after any accepted product/system/security/release decision changes, or before full QA execution if the implementation baseline moved.

## Forbidden Content
- Claiming full QA execution, release readiness, or current-state acceptance from this docs sync pass.
- Inventing future behavior beyond the implemented working-copy baseline.
- Raw secrets, auth headers, token values, raw JSONL, or private data.
