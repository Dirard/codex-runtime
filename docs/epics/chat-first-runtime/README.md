# Epic: Chat-First Runtime

Type: epic package index
Status: current-behavior product-docs synced to the working-copy baseline; full QA and current-state pending
Owner: docs_writer
Artifact purpose / consumer: navigation and trace root for the high-risk/full chat-first runtime package used by product, analysis, QA, and implementation roles.
Last repaired: 2026-06-16
Related docs: `docs/product-docs/index.md`, `docs/product-docs/sdk/chat-first-go-sdk.md`, `docs/product-docs/grpc/codex-runtime-gateway.md`
Trace IDs: BRS-GOAL-001, BRS-REQ-001, BRS-REQ-002, BRS-REQ-003, SRS-FR-001, SRS-FR-018, SRS-FR-019, PLAN-STAGE-001..PLAN-STAGE-003, PLAN-STAGE-005..PLAN-STAGE-010

## Source Of Truth
This epic README owns the change-package map and traceability entry points for the chat-first runtime initiative. Permanent behavior stays in product-docs; detailed requirements stay in BRS/SRS; QA evidence stays in tests/reviews.

## Product Goal
Create an external runtime around installed Codex so application code can use a stable chat-first Go SDK and a stable local gRPC/proto surface while the gateway absorbs unstable internal `codex.exe app-server` JSONL details.

## System Boundary
`web app -> API handler with Go SDK -> local gateway -> codex.exe app-server`

## In Scope
- Chat-first SDK behavior for `Run`, `GetChat`, `GetHistory`, `GetEventsStream`, status, pending, and interrupt.
- Stable gateway-facing gRPC/proto semantics over unstable internal JSONL.
- `chat_id` as Codex `Thread.id` under chat-first naming.
- Process-local gateway correlation for active runs, replay, pending, interrupt, idempotency, and diagnostics.
- Local security/auth/configuration/observability/operations skeleton needed for a high-risk/full baseline.

## Out Of Scope
- Empty chat creation without a first prompt.
- Gateway-owned durable chat identity or local identity database.
- Promise that SDK-created chats appear as visible Desktop UI threads.
- Remote or multi-tenant runtime exposure.
- Release/current-state artifacts before a release trigger exists.
- Direct JSONL exposure as a supported public contract.

## Artifact Map
- BRS: `BRS/feature.md`, `BRS/nfr.md`
- SRS: `SRS/feature.md`, `SRS/contracts.md`, `SRS/transient-correlations.md`, `SRS/sequences.md`
- Tech design: `tech-design/tech-design.md`, `tech-design/adr-001-dedicated-chat-first-service.md`
- Plan: `plan/index.md`, stage files
- Tests: `tests/test-strategy.md`, `tests/test-cases.md`, `tests/regression.md`, `tests/test-execution.md`
- Reviews: `reviews/product-review.md`, `reviews/engineering-review.md`, `reviews/security-privacy-data-review.md`, `reviews/release-ops-review.md`, `reviews/documentation-sync-check.md`, `reviews/qa-readiness.md`

## Traceability Convention
- Business goal / requirements: `BRS-GOAL-*`, `BRS-REQ-*`, `BRS-NFR-*`
- Product-docs: `DOC-SDK-*`, `DOC-GRPC-*`, `DOC-DOMAIN-*`, `DOC-SEC-*`, `DOC-CONFIG-*`, `DOC-OBS-*`, `DOC-OPS-*`
- System requirements: `SRS-FR-*`, `SRS-NFR-*`
- Plan slices: `PLAN-STAGE-*`
- Test cases and evidence: `TC-*`, `QA-EVID-*`

## Current Package State
- BRS is accepted, SRS metadata/header statuses are synchronized to the accepted targeted re-review state, and the targeted SRS/plan/QA repairs passed product re-review.
- Tech design is paper architecture gate ready and ADR-001 is accepted for the paper architecture gate.
- Security/privacy/data, release/ops, QA readiness, and the targeted engineering recheck passed for the paper package.
- Documentation sync now covers current-behavior product-doc surfaces for the implemented working-copy baseline; it does not claim full QA or release/current-state activation.
- Root approval before implementation planning/implementation has been received; implementation is partial through Stage 03, Stage 05, Stage 06, Stage 07, Stage 08, Stage 09, and Stage 10. Full QA execution, final baseline repair/review if new findings appear, and release/current-state artifacts remain pending.

## Pending Follow-Up Gates
- Stage 02 contract implementation has product and engineering review pass recorded.
- Stage 03 local readiness/security-boundary implementation has local test pass recorded after supervisor foundation repair, with product, engineering, and security/data re-review pass recorded.
- Stage 05 `StartChatRun` boundary implementation has local test pass recorded, with product, engineering, and security/data re-review pass recorded after the idempotency scope repair.
- Stage 06 existing-chat runtime behavior has local test pass recorded, with product, engineering, and security/data re-review pass recorded after unknown-state, cursor-integrity, idempotency-scope, stream-subscribe, and `chat_id` validation repairs.
- Stage 07 pending/interrupt runtime behavior has local test pass recorded after owner-finding repairs, with product, engineering, and security/data targeted re-review pass recorded.
- Stage 08 Go SDK/adoption surface has local test/list/vet/diff-check pass recorded after owner-finding repairs, with product, engineering, security/data, and QA targeted re-review pass recorded.
- Stage 09 integration hardening has local active-run capacity repair, proto generation sync, full-suite/list/vet/diff-check/guardrail-grep evidence recorded, with targeted owner re-review pass recorded.
- Stage 10 volatile gateway correlations implementation has local test pass recorded, with product, engineering, and security/data re-review pass recorded.
- Current-behavior product-doc sync is recorded for the current working-copy baseline; rerun it if implementation behavior or owning decisions change before full QA.
- Full QA execution remains pending until the final implementation baseline is ready.
- Release/current-state navigation stays deferred until an explicit release trigger exists.
