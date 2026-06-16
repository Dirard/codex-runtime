# Regression Scope: Chat-First Runtime

Type: regression scope
Artifact purpose / consumer: pre-implementation regression boundary for future implementation, automation, and QA execution.
Status: repaired for accepted paper package; execution not started
Owner: qa_engineer
Last repaired: 2026-06-15
Related docs: `test-strategy.md`, `test-cases.md`, `../BRS/feature.md`, `../BRS/nfr.md`, `../SRS/traceability.md`, `../SRS/rollout.md`, `../tech-design/tech-design.md`, `../tech-design/adr-001-dedicated-chat-first-service.md`, `../reviews/product-review.md`, `../reviews/security-privacy-data-review.md`, `../reviews/release-ops-review.md`, `../../../product-docs/grpc/codex-runtime-gateway.md`, `../../../product-docs/security/local-runtime-boundary.md`, `../../../product-docs/configuration/gateway-runtime-config.md`, `../../../product-docs/observability/event-stream-observability.md`, `../../../product-docs/operations/local-gateway-runbook.md`
Trace IDs: REG-001..REG-012, TC-001..TC-036

## Source Of Truth
This artifact names the behavior that must not regress when implementation starts. Detailed checks live in `test-cases.md`; execution evidence remains deferred to `test-execution.md`.

## Must Preserve
- `chat_id` remains the only primary application identity for chat-first callers and equals Codex `Thread.id`.
- Public chat-first behavior stays separate from `codex.control.v1.CodexControl`.
- No empty chat success and no first-turn shortcut before acceptance/correlation.
- `GetChat` never creates a chat and `RunChatTurn` never silently starts an unrelated thread.
- Status, history, stream, pending, interrupt, and restart behavior remain Codex-backed and typed.
- Item-level history remains unsupported in v1 unless the accepted paper package changes.
- Replay, pending, active-run, idempotency, and diagnostics state remain process-local and are lost after restart; a pre-restart idempotency key alone cannot prove prior result, recovery, or no-duplicate delivery, and `chat_id` recovery stays valid only where Codex re-proves thread state.
- Auth/authz/validation happens before any Codex side effect or disclosure for every public chat RPC.
- No raw JSONL, no secrets, no private content retention, and no content hashes/digests appear in public or diagnostic surfaces.
- Local-only config/listen/token-source rules, disable/readiness semantics, and supervisor backoff remain intact.
- No Desktop UI visibility promise, no release/current-state promise, and no SQLite/durable mapping/content store enter v1.

## Regression Cases
| ID | Surface | Requirement / doc anchors | Risk | Check refs | Evidence expectation |
| --- | --- | --- | --- | --- | --- |
| `REG-001` | Dedicated chat-first service split and task compatibility coexistence | `BRS-REQ-003`, `SRS-FR-001`, `SRS-NFR-008`, `DOC-GRPC-001`, `tech-design.md`, `adr-001-dedicated-chat-first-service.md` | P0 | `TC-034` | `QA-EVID-034` |
| `REG-002` | Chat identity equals Codex thread identity | `BRS-REQ-004`, `SRS-FR-018`, `DOC-SDK-001`, `DOC-DOMAIN-001` | P0 | `TC-001`, `TC-004`, `TC-006`, `TC-027` | `QA-EVID-001`, `QA-EVID-004`, `QA-EVID-006`, `QA-EVID-027` |
| `REG-003` | No empty chat and first-turn acceptance boundary | `BRS-REQ-002`, `BRS-RULE-012`, `SRS-FR-004`, `SRS-FR-011` | P0 | `TC-001`..`TC-003` | `QA-EVID-001`..`QA-EVID-003` |
| `REG-004` | Existing chat lookup and continuation semantics | `SRS-FR-003`, `SRS-FR-005`, `SRS-FR-012`, `DOC-SDK-001`, `DOC-DOMAIN-001` | P0 | `TC-004`..`TC-008` | `QA-EVID-004`..`QA-EVID-008` |
| `REG-005` | Typed status and non-fabricated recovery | `SRS-FR-006`, `SRS-FR-011`, `SRS-NFR-006`, `DOC-DOMAIN-001`, `DOC-OBS-001` | P0 | `TC-009`, `TC-010`, `TC-027` | `QA-EVID-009`, `QA-EVID-010`, `QA-EVID-027` |
| `REG-006` | History depth, no item-level promise, and no invented history | `SRS-FR-007`, `SRS-FR-011`, `DOC-SDK-001`, `DOC-DOMAIN-001` | P0 | `TC-011`..`TC-013` | `QA-EVID-011`..`QA-EVID-013` |
| `REG-007` | Live stream, replay boundaries, and stream-cancel separation | `SRS-FR-008`, `SRS-FR-010`, `SRS-FR-019`, `DOC-GRPC-001`, `DOC-OBS-001` | P0 | `TC-014`..`TC-018` | `QA-EVID-014`..`QA-EVID-018` |
| `REG-008` | Pending and interrupt safety | `SRS-FR-009`, `SRS-FR-010`, `SRS-NFR-005`, `DOC-SEC-001` | P0 | `TC-019`..`TC-023` | `QA-EVID-019`..`QA-EVID-023` |
| `REG-009` | Current-process idempotency and post-restart key-only honesty | `SRS-FR-011`, `SRS-FR-019`, `SRS-NFR-003`, `SRS-NFR-009` | P0 | `TC-024`..`TC-027` | `QA-EVID-024`..`QA-EVID-027` |
| `REG-010` | All-RPC auth/authz/validation before side effects | `BRS-REQ-006`, `SRS-FR-013`, `DOC-SEC-001`, `tech-design.md`, `security-privacy-data-review.md` | P0 | `TC-028`..`TC-031` | `QA-EVID-028`..`QA-EVID-031` |
| `REG-011` | Redaction, no raw JSONL, and no private content retention | `BRS-RULE-008`, `BRS-RULE-014`, `SRS-FR-014`, `SRS-NFR-007`, `DOC-OBS-001`, `DOC-SEC-001` | P0 | `TC-032` | `QA-EVID-032` |
| `REG-012` | Local-only config, readiness, disable path, release inactive stance, and non-promises | `BRS-NFR-008`, `SRS-FR-016`, `SRS-FR-017`, `SRS-FR-019`, `DOC-CONFIG-001`, `DOC-OPS-001`, `release-ops-review.md` | P0/P1 | `TC-033`..`TC-036` | `QA-EVID-033`..`QA-EVID-036` |

## Skipped Regression With Waiver
- None.

## Forbidden Content
- Regression scope that assumes implementation already passed.
- Raw secrets, raw JSONL, or private data.
