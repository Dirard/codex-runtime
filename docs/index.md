# Docs Index: codex-runtime

Type: docs root index
Status: navigation synced for current paper package
Owner: docs_writer
Consumer / intended use: contributors and delivery roles finding the product-doc, epic, and deferred release navigation for codex-runtime.
Last reviewed: 2026-06-15
Related docs: `docs/product-docs/index.md`, `docs/epics/index.md`, `docs/product-docs/doc-debt-register.md`
Trace IDs: not applicable for navigation index

## Source Of Truth
Navigation-only root map for the current docs package. This index links owning indexes and deferred release navigation; it does not own behavior details, release delta, acceptance evidence, QA evidence, or current-state facts.

## Product-Docs
- Index link: `docs/product-docs/index.md`
- Current readiness: draft target adoption package exists for the touched chat-first runtime surfaces, and paper docs sync is current for navigation/traceability only.

## Epics
- Index link: `docs/epics/index.md`
- Active epic: `docs/epics/chat-first-runtime/README.md`
- Paper docs sync verdict: `docs/epics/chat-first-runtime/reviews/documentation-sync-check.md`

## Releases
- Release navigation: deferred.
- Reason: no release note, release-impacting deployment, production acceptance, or explicit current-state milestone is in approved scope yet.
- Owner / follow-up: release_ops_owner before any release-impacting deployment, current-state acceptance, or user/operator-visible release note.

## Package Rules
- Product behavior and permanent contracts belong in `docs/product-docs/**`.
- Change-package intent, requirements, plan, tests, and review placeholders belong in `docs/epics/**`.
- Release artifacts stay absent until a release/current-state trigger appears.
- Product-docs link to epic docs as related context, but do not replace or duplicate BRS/SRS ownership.

## Current Navigation Gaps
- Root `README.md` still reflects the older gateway-centric framing and is tracked as doc debt outside this write scope.
- `proto/codex_control/v1/README.md` remains a codegen note, not a chat-first runtime source of truth.
- Release navigation stays deferred until release/current-state artifacts are activated.

## Completion Criteria
- Product-docs index exists and is linked.
- Epics index exists and links the chat-first runtime epic.
- Releases navigation is explicitly deferred until a release/current-state artifact exists.
- Navigation gaps are tracked in the doc debt register.

## Forbidden Content
- Orphan navigation.
- Raw secrets or private data.
- Behavior details, release delta, acceptance evidence, QA evidence, or current-state facts except links to owning docs.

## Review Checklist
- Product-docs index link, epics index link, releases deferral, navigation gaps, owner, consumer/intended use, and last reviewed are complete.
