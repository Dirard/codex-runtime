# Surface Index: Configuration

Type: product-docs surface index
Status: synced to implemented working-copy behavior
Owner: docs_writer
Consumer / intended use: gateway developers, release/ops owner, security owner, and QA.
Last reviewed: 2026-06-16
Related docs: `docs/product-docs/configuration/gateway-runtime-config.md`
Trace IDs: DOC-CONFIG-001

## Source Of Truth
Navigation-only map for the configuration surface. Owning behavior lives in linked product-docs; this index may list purpose, owners, status links, and gaps without duplicating behavior.

## Surface Purpose
Document configuration needed to run the local gateway, Codex child process, auth token source, and process-local runtime limits safely.

## Documents
- `gateway-runtime-config.md`: current implemented configuration behavior for the working-copy baseline.

## Owners
- Release/ops: release_ops_owner.
- Security classification: security_privacy_data_owner.
- QA: qa_engineer.

## Current / Working-Copy Status Links
- Linked surface doc: current implemented working-copy behavior; not full-QA verified or released/current-state.
- Product baseline for downstream planning: `docs/epics/chat-first-runtime/reviews/product-review.md`
- Downstream package status stays in owning docs: `docs/epics/chat-first-runtime/README.md`, `docs/epics/chat-first-runtime/tech-design/tech-design.md`, `docs/epics/chat-first-runtime/tech-design/adr-001-dedicated-chat-first-service.md`

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
