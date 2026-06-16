# Product-Docs Index: codex-runtime

Type: product-docs index
Status: current-behavior surfaces synced to working-copy baseline; release/current-state pending
Owner: docs_writer
Consumer / intended use: contributors and reviewers finding permanent behavior and contract docs for codex-runtime.
Last repaired: 2026-06-16
Related docs: `docs/index.md`, `docs/product-docs/baseline-inventory.md`, `docs/product-docs/doc-debt-register.md`, `docs/epics/chat-first-runtime/README.md`
Trace IDs: DOC-SDK-001, DOC-GRPC-001, DOC-DOMAIN-001, DOC-SEC-001, DOC-CONFIG-001, DOC-OBS-001, DOC-OPS-001

## Source Of Truth
Navigation-only product-docs surface map. Owning behavior lives in linked surface docs; this index lists touched surfaces, owners, and scope boundaries without duplicating behavior.

## Boundary With Epics
- Product-docs own the current implemented behavior of the synced chat-first runtime surfaces in this working copy.
- BRS, SRS, plan, tests, and reviews remain in `docs/epics/**` and are linked as related change-package context only.
- There is no DB/data-store product-doc surface for v1 because gateway-owned durable chat identity storage is not required.
- This index records navigation and touched-surface traceability for the synced working-copy baseline; it is not full-QA evidence or release/current-state documentation.

## Related Change-Package Context
- Epic package root: `docs/epics/chat-first-runtime/README.md`
- Tech design set: `docs/epics/chat-first-runtime/tech-design/tech-design.md`, `docs/epics/chat-first-runtime/tech-design/adr-001-dedicated-chat-first-service.md`
- QA paper package: `docs/epics/chat-first-runtime/tests/test-strategy.md`, `docs/epics/chat-first-runtime/tests/test-cases.md`, `docs/epics/chat-first-runtime/tests/regression.md`, `docs/epics/chat-first-runtime/tests/test-execution.md`, `docs/epics/chat-first-runtime/reviews/qa-readiness.md`

## Touched Surfaces
- SDK: `docs/product-docs/sdk/index.md`
- gRPC: `docs/product-docs/grpc/index.md`
- Domain: `docs/product-docs/domain/index.md`
- Security: `docs/product-docs/security/index.md`
- Configuration: `docs/product-docs/configuration/index.md`
- Observability: `docs/product-docs/observability/index.md`
- Operations: `docs/product-docs/operations/index.md`

## Explicitly Not Touched
- DB/data store: not applicable for v1. Public `chat_id` is Codex `Thread.id`; gateway keeps only process-local correlation.
- REST: external web/API handler remains outside this repo-local runtime docs scope.
- WS: not applicable unless a future web streaming transport is added.
- UI: not touched; SDK-created chats are not promised to appear as visible Desktop UI threads.
- Kafka: not applicable.
- Cache: not applicable.

## Minimal / Deferred Areas
- `README.md` and `proto/codex_control/v1/README.md` remain orientation docs and are tracked as doc debt.
- Release docs are intentionally absent until a release/current-state trigger exists.

## Completion Criteria
- Every touched surface has an index and an owning doc.
- Removed DB/data-store surface is not advertised as current target behavior.

## Forbidden Content
- Orphan surface docs.
- Raw secrets or private data.
- Behavior details, release delta, acceptance evidence, QA evidence, or current-state facts except links to owning docs.
