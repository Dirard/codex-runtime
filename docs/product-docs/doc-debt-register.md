# Doc Debt Register: codex-runtime

Type: doc debt register
Status: draft
Owner: docs_writer
Consumer / intended use: delivery roles tracking missing-doc and navigation debt that should not be hidden inside BRS/SRS.
Last reviewed: 2026-06-12
Related docs: `docs/product-docs/baseline-inventory.md`, `docs/epics/chat-first-runtime/README.md`
Related baseline inventory: `docs/product-docs/baseline-inventory.md`
Trace IDs: DOC-DEBT-001

## Source Of Truth
This register owns missing-doc and documentation-navigation debt only. It tracks owner, consumer/intended use, risk, blocking decision, related epic, and expected resolution/follow-up; it does not own product behavior, release delta, acceptance evidence, QA evidence, or release facts.

| Missing surface/doc | Owner | Consumer / intended use | Risk | Related epic | Expected resolution | Blocks current work? |
| --- | --- | --- | --- | --- | --- | --- |
| Root `README.md` still uses older gateway-centric framing | docs_writer | contributors and integrators | medium: onboarding drift | `docs/epics/chat-first-runtime/README.md` | Update after BRS/SRS review so the root README does not pre-approve unstable contract decisions. | No for draft skeleton; yes before final current-state docs. |
| `proto/codex_control/v1/README.md` documents codegen but not the chat-first target contract | docs_writer + system_analyst | proto/SDK implementers | medium: contract drift | `docs/epics/chat-first-runtime/README.md` | Update when the stable proto direction is reviewed and approved. | No for draft skeleton; yes before implementation closeout if proto changes. |
| Release/current-state docs | release_ops_owner | release owner and QA | low now, high only when release/current-state trigger appears | `docs/epics/chat-first-runtime/README.md` | Create only when a release-impacting deployment or explicit current-state milestone is selected. | No. |

## Recently Resolved / Superseded
- The previous blocker for missing detailed tech design / ADR decisions is closed for the paper package: `docs/epics/chat-first-runtime/tech-design/tech-design.md` and `docs/epics/chat-first-runtime/tech-design/adr-001-dedicated-chat-first-service.md` now exist as repaired, ready-for-review inputs. Fresh owner reviews, developer planning, implementation, and release/current-state work remain separate pending gates, not active doc-debt items in this register.

## Completion Criteria
- Source-of-truth statement confirms this is missing-doc and documentation-navigation debt only, not behavior or release documentation.
- Touched-surface, high-risk, or blocking debt has a real owner, consumer/intended use, risk, related epic if any, expected resolution, and blocking decision.
- High-risk touched surfaces are not waived due to legacy debt.

## Forbidden Content
- Raw secrets or private data.
- Product behavior details, release delta, acceptance evidence, QA evidence, or release facts.
- Unowned touched-surface, high-risk, or blocking debt items.
- Hidden exemption for touched high-risk surfaces.

## Review Checklist
- Source-of-truth statement is present, and every debt item has owner or allowed `owner unknown / needs assignment`, consumer/intended use, risk, related epic, resolution expectation, blocking decision, and last reviewed context.
