# Baseline Inventory: codex-runtime

Type: docs adoption inventory
Status: adoption snapshot updated after current-behavior product-doc sync; full QA and release/current-state pending
Owner: docs_writer
Consumer / intended use: docs_writer, product_owner, system_analyst, qa_engineer, and reviewers assessing docs coverage/readiness for the current working-copy baseline.
Last repaired: 2026-06-16
Related docs: `docs/index.md`, `docs/product-docs/index.md`, `docs/product-docs/doc-debt-register.md`, `docs/epics/chat-first-runtime/README.md`
Trace IDs: DOC-DEBT-001

## Source Of Truth
This inventory owns the adoption snapshot of existing docs roots, detected product surfaces, coverage/status, missing indexes, touched high-risk surfaces, and links to doc debt. It does not own product behavior, release delta, acceptance evidence, QA evidence, or release facts.

## Project / Service Name
codex-runtime

## Existing Docs Roots
- `README.md`: existing root orientation for the gateway executable and local runtime startup.
- `proto/codex_control/v1/README.md`: existing proto code generation note.
- `docs/`: docs package for the high-risk/full chat-first runtime work, including synced current-behavior product-doc surfaces and epic/change artifacts.

## Detected Product Surfaces
| Surface | Coverage/status | Index exists? | Owner | Consumer / intended use | Notes |
| --- | --- | --- | --- | --- | --- |
| SDK | current implemented working-copy behavior | yes | product_owner + system_analyst | SDK implementers and app integrators | Synced to the current chat-first Go SDK baseline; not full-QA or release/current-state evidence. |
| gRPC | current implemented working-copy behavior | yes | system_analyst + architect | gateway and SDK implementers | Synced to the current external gateway/proto baseline; not full-QA or release/current-state evidence. |
| Domain | current implemented working-copy behavior | yes | product_owner + system_analyst | product, dev, QA | Synced chat identity, lifecycle, and scope boundaries for the working-copy baseline. |
| Security | current implemented working-copy behavior | yes | security_privacy_data_owner | developers and reviewers | Synced local gateway auth, loopback boundary, and redaction expectations for the working-copy baseline. |
| Configuration | current implemented working-copy behavior | yes | release_ops_owner + security_privacy_data_owner | operators and developers | Synced current config keys/defaults/limits for the local runtime baseline. |
| Observability | current implemented working-copy behavior | yes | release_ops_owner + qa_engineer | operators and QA | Synced event-stream diagnostics, readiness, replay-failure, and redaction expectations for the working-copy baseline. |
| Operations | current implemented working-copy behavior | yes | release_ops_owner | local operators and support | Synced local gateway runbook for the working-copy baseline. |
| DB/data store | not applicable | no | not applicable | not applicable | `chat_id` is Codex `Thread.id`; no gateway durable identity store in v1. |
| UI | missing / not touched | no | owner unknown / needs assignment | future Desktop/UI sync owner | SDK-created chats are not promised to appear as visible Desktop UI threads. |
| REST / WS / Kafka / cache | not applicable | no | owner unknown / needs assignment | future scope only | Web app and API handler sit outside this repo-local runtime doc scope. |

## Missing Indexes
- Release index is deferred until release/current-state artifacts exist.
- UI surface index is intentionally not created because UI is not touched.

## Touched High-Risk Surfaces
- Public SDK contract.
- Public stable gRPC/proto contract.
- Local auth/security boundary.
- Process-local gateway runtime state for current run, stream, pending, interrupt, and idempotency behavior.
- Operational behavior for the local gateway and `codex.exe app-server`.

## Current Doc Debt Links
- `docs/product-docs/doc-debt-register.md`

## Completion Criteria
- Source-of-truth statement confirms this is an adoption snapshot, not behavior or release documentation.
- Touched surfaces are known.
- Touched surfaces and touched high-risk surfaces have owner and consumer/intended use.
- DB/data-store surface is explicitly not applicable for v1.

## Forbidden Content
- Raw secrets, private data dumps, or unrelated legacy rewrite scope.
- Product behavior details, release delta, acceptance evidence, QA evidence, or release facts.
