# Plan: Stage 09 Integration Hardening And Handoff

Type: implementation plan slice
Status: local integration checks passed after engineering/QA repair; targeted owner re-review passed; not Done or release approval
Owner: system_analyst
Last repaired: 2026-06-16
Related docs: `index.md`, `../BRS/feature.md`, `../BRS/nfr.md`, `../SRS/index.md`, `../SRS/traceability.md`, `../SRS/rollout.md`, `../SRS/nfr-mapping.md`, `../tech-design/tech-design.md`, `../tech-design/adr-001-dedicated-chat-first-service.md`, `../tests/test-strategy.md`, `../tests/test-cases.md`, `../tests/regression.md`, `../tests/test-execution.md`, `../reviews/product-review.md`, `../reviews/security-privacy-data-review.md`, `../reviews/release-ops-review.md`, `../reviews/qa-readiness.md`, `../reviews/engineering-review.md`, `../reviews/documentation-sync-check.md`
Trace IDs: PLAN-STAGE-009, SRS-FR-001..SRS-FR-019, SRS-NFR-001..SRS-NFR-009, TC-001..TC-036, REG-001..REG-012

## Goal
Provide the final implementation hardening and handoff slice so the current implementation can be reviewed, tested, documented against current behavior, and held out of release/current-state activation until the later gates pass.

## Scope
- Cross-stage integration checks for service split, auth, start boundary, existing-chat behavior, volatile correlation, pending/interrupt, SDK, config, readiness, and disabled runtime.
- Trace consistency from accepted SRS to active plan stages to QA basis.
- Developer handoff checklist and implementation closeout expectations.
- QA execution handoff using `TC-001`..`TC-036`, `REG-001`..`REG-012`, and `QA-EVID-001`..`QA-EVID-036`.
- Current-behavior docs sync handoff after implementation exists.
- Release/current-state non-activation and explicit future activation gate.

## Implemented Evidence
- Full local Go package test pass across root SDK, API-handler example, gateway command, gateway internals, task compatibility packages, and generated package.
- Stage 09 engineering P1 repair: `StartChatRun` now reserves active-run capacity before app-server `Connection`, `thread/start`, or `turn/start`, and converts that reservation into a tracked active run only after Codex returns thread and turn ids.
- Go package discovery and vet pass across all packages.
- Proto generation sync check pass via `scripts/generate-proto.ps1`, followed by `gofmt -w .\gen`.
- Diff whitespace check pass.
- Guardrail search pass for old gateway naming/imports and forbidden implementation concepts.
- Stage 08/09 evidence synchronized in `../tests/test-execution.md` and `../README.md`; Stage 09 targeted owner re-review is recorded in the review registers.

## Executed Checks
- `GOPROXY=off go test -count=1 ./...`
- `GOPROXY=off go list ./...`
- `GOPROXY=off go vet ./...`
- `powershell -ExecutionPolicy Bypass -File .\scripts\generate-proto.ps1`
- `gofmt -w .\gen`
- `git diff --check`
- File-tools grep over Go/source-code paths: no legacy `codex-control-gateway` implementation references remain; documentation may mention the legacy name only as migration/guardrail evidence.
- File-tools grep over Go files: no `github.com/Dirard/codex-runtime/internal/` imports.
- File-tools grep over Go files: no SQLite/local DB implementation marker.
- File-tools grep over Go files: no Desktop UI/current-thread promise marker.
- File-tools grep over Go files: raw/unknown payload markers remain confined to task compatibility mapping/tests and chat contract guardrail tests, not the chat-first `ChatEvent` contract.

## Repair Evidence
- Engineering P1 closed locally: `gateway/internal/chatstate.Store` now has mutex-owned active-run capacity reservations, and ordinary `StartRun` counts outstanding reservations against `ActiveRunsCap`.
- `gateway/internal/chatruntime.Service.StartChatRun` reserves capacity before app-server calls and releases the reservation on failure paths that do not become a tracked active run.
- Focused tests added for store reservation capacity and for `StartChatRun` capacity exhaustion before Codex side effects.
- Engineering/QA evidence wording repaired from repo-wide "no references" to scoped Go/source-code guardrails.
- Engineering proto-sync gap closed locally by running the Docker-based generator and re-running the full verification set.

## Passed Paper Gates To Preserve
- Product review passed for BRS, SRS, and QA-basis sanity.
- Architecture and ADR are accepted for the paper architecture gate.
- Security/privacy/data paper review passed.
- Release/ops paper review passed.
- QA readiness passed for pre-implementation paper basis.
- Documentation sync check passed for paper docs/navigation.
- Engineering review passed for paper QA-basis quality only.

## Implementation Steps
1. After implementing stages 02, 03, 10, 05, 06, 07, and 08, run an integration pass across the full runtime chain.
2. Verify the dedicated chat service and task compatibility service remain separated and coexist safely.
3. Verify all public chat operations enforce auth, validation, and session/workspace authorization before any Codex side effect or state disclosure.
4. Verify `chat_id == Codex Thread.id` throughout SDK, gRPC, gateway domain, adapter, logs, diagnostics, and tests.
5. Verify no code path introduces gateway-owned retained conversation content, thread identity translation, thread-only chat success, Desktop visible/current-thread promise, external/shared gateway scope, release/current-state activation, or raw internal payload escape hatch.
6. Verify `chat_runtime.enabled=false`, readiness/health, Stage 03 pre-side-effect lazy supervisor foundation, repeated failure cooldown, Stage 07 pending/interrupt usage, and task RPC coexistence together.
7. Verify all SRS FR/NFR items have implementation coverage and QA basis coverage; record any uncovered item as a blocker, not a silent executor decision.
8. Prepare implementation handoff notes for product implementation review, engineering implementation review, QA execution, current-behavior docs sync, and any root-approved owner re-review triggered by changed scope or changed controls.
9. Keep full `TC-001`..`TC-036` QA execution pending until the final implementation baseline is ready; record only partial/local implementation evidence before that gate.
10. Keep release/current-state activation inactive until QA execution, docs sync, product/engineering implementation reviews, and explicit activation approval pass.

## Acceptance / Checks
- Every `SRS-FR-*` and `SRS-NFR-*` maps to at least one active plan stage and at least one QA test basis item.
- QA evidence does not rely on `PLAN-STAGE-001` as an executable anchor.
- Forbidden scopes/concepts are absent from implementation artifacts except as explicit guardrails.
- Post-restart idempotency does not claim replay, recovery, exactly-once delivery, or no-duplicate suppression from a pre-restart key alone.
- Current-behavior docs sync is not claimed before implementation exists.
- Release/current-state activation is not claimed by plan, docs, implementation closeout, or paper reviews.

## Traceability
- SRS: `SRS-FR-001`..`SRS-FR-019`, `SRS-NFR-001`..`SRS-NFR-009`.
- QA: `TC-001`..`TC-036`, `QA-EVID-001`..`QA-EVID-036`.
- Regression: `REG-001`..`REG-012`.
- Reviews: product, security/privacy/data, release/ops, QA readiness, engineering paper QA-basis, docs sync.

## Remaining Gates
- Full QA execution and evidence capture.
- Current-behavior docs sync against the actual implemented behavior.
- Final implementation baseline review/repair for any owner findings outside this Stage 09 targeted closeout.
- Release/current-state activation gate, only after final implementation readiness.

## Stop And Ask If
- A release/current-state milestone is requested before the remaining gates pass.
- QA finds current granular refs cannot cover cases without changing requirement meaning.
- Any owner review finds product, architecture, security/privacy/data, release/ops, QA, or docs decisions missing from accepted inputs.
