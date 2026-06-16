# Surface Index: Domain

Type: product-docs surface index
Status: synced to implemented working-copy behavior
Owner: docs_writer
Consumer / intended use: product, system analysis, SDK/gateway developers, and QA finding chat runtime behavior.
Last reviewed: 2026-06-16
Related docs: `docs/product-docs/domain/chat-runtime.md`
Trace IDs: DOC-DOMAIN-001

## Source Of Truth
Navigation-only map for the domain surface. Owning behavior lives in linked product-docs; this index may list purpose, owners, status links, and gaps without duplicating behavior.

## Surface Purpose
Document the domain concepts that keep application chat identity separate from internal Codex thread/task identity.

## Documents
- `chat-runtime.md`: current implemented domain behavior for the working-copy baseline.

## Owners
- Product meaning: product_owner.
- Requirements: system_analyst.
- QA: qa_engineer.

## Current / Working-Copy Status Links
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
