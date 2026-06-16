# Surface Index: gRPC

Type: product-docs surface index
Status: synced to implemented working-copy behavior
Owner: docs_writer
Consumer / intended use: gateway, SDK, proto, and QA owners finding the stable external contract docs.
Last reviewed: 2026-06-16
Related docs: `docs/product-docs/grpc/codex-runtime-gateway.md`, `proto/codex_control/v1/codex_control.proto`, `proto/codex_control/v1/README.md`
Trace IDs: DOC-GRPC-001

## Source Of Truth
Navigation-only map for the gRPC surface. Owning behavior lives in linked product-docs; this index may list purpose, owners, status links, and gaps without duplicating behavior.

## Surface Purpose
Document the stable gateway contract exposed to the Go SDK and other local clients.

## Documents
- `codex-runtime-gateway.md`: current implemented gateway contract behavior for the working-copy baseline.

## Owners
- Contract requirements: system_analyst.
- Architecture/proto design: architect.
- Security boundary: security_privacy_data_owner.
- QA: qa_engineer.

## Current / Working-Copy Status Links
- Existing proto: `proto/codex_control/v1/codex_control.proto` contains both the preserved task-compatibility service and the implemented chat-first service; product-docs still own the behavior summary and scope boundaries.
- Linked surface doc: current implemented working-copy behavior; not full-QA verified or released/current-state.
- Product baseline for downstream planning: `docs/epics/chat-first-runtime/reviews/product-review.md`
- Epic package status stays in owning docs: `docs/epics/chat-first-runtime/README.md`

## Related Epics
- `docs/epics/chat-first-runtime/README.md`

## Completion Criteria
- Touched surface docs are linked.
- Orphan docs are not allowed.
- Missing docs link to doc debt register.

## Forbidden Content
- Orphan docs.
- Raw secrets or private data.
- Release delta as permanent documentation.
- Behavior details, acceptance evidence, QA evidence, or current-state facts except links to owning docs.

## Review Checklist
- Surface purpose, documents, owners, status links, related epics, missing docs, consumer/intended use, and last reviewed are complete.
