# Surface Index: Security

Type: product-docs surface index
Status: synced to implemented working-copy behavior
Owner: docs_writer
Consumer / intended use: security/data owner, gateway/SDK developers, reviewers, and QA.
Last reviewed: 2026-06-16
Related docs: `docs/product-docs/security/local-runtime-boundary.md`
Trace IDs: DOC-SEC-001

## Source Of Truth
Navigation-only map for the security surface. Owning behavior lives in linked product-docs; this index may list purpose, owners, status links, and gaps without duplicating behavior.

## Surface Purpose
Document local gateway authentication, boundary, redaction, and no-secret handling for the runtime.

## Documents
- `local-runtime-boundary.md`: current implemented security behavior for the working-copy baseline.

## Owners
- Security/privacy/data: security_privacy_data_owner.
- Architecture: architect.
- QA: qa_engineer.

## Current / Working-Copy Status Links
- Linked surface doc: current implemented working-copy behavior; not full-QA verified or released/current-state.
- Product baseline for downstream planning: `docs/epics/chat-first-runtime/reviews/product-review.md`
- Downstream package status stays in owning docs: `docs/epics/chat-first-runtime/README.md` and `docs/epics/chat-first-runtime/tech-design/tech-design.md`; required security/privacy/data review remains outside this navigation index.

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
