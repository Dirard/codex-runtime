# SRS NFR Mapping: Chat-First Runtime

Type: software requirements specification
Status: paper-ready after accepted targeted product/engineering re-reviews; root implementation approval received
Owner: system_analyst
Last repaired: 2026-06-15
Related docs: `index.md`, `feature.md`, `contracts.md`, `states-and-outcomes.md`, `transient-correlations.md`, `events-history.md`, `rollout.md`, `../BRS/nfr.md`
Trace IDs: SRS-NFR-001..SRS-NFR-009

## NFR Requirements
| SRS ID | Requirement | BRS refs | Owner review / QA implication |
| --- | --- | --- | --- |
| `SRS-NFR-001` | Interactive local SDK/gRPC calls must avoid avoidable gateway latency and unbounded scans; Codex model/tool execution time is outside gateway control. | `BRS-NFR-001` | QA tests bounded start/status/history/stream attach paths. |
| `SRS-NFR-002` | Local auth, secret handling, redaction, and minimized data retention are mandatory despite local-only scope. | `BRS-NFR-002` | Security/privacy/data owner approves auth/redaction; QA checks logs/errors/docs/examples. |
| `SRS-NFR-003` | Gateway restart loses process-local replay/pending/idempotency/current-run correlation; app-stored `chat_id` can be reused only as Codex Thread id where Codex proves state, and pre-restart idempotency keys alone cannot prove prior requests or no-duplicate recovery. | `BRS-NFR-003` | QA tests restart/no durable replay/no fabricated state/no fabricated idempotent replay. |
| `SRS-NFR-004` | Diagnostics must be safe and typed enough for local QA/support without raw JSONL or private data dumps. | `BRS-NFR-004`, `BRS-NFR-007` | Observability and security review; QA redaction evidence. |
| `SRS-NFR-005` | Pending and interrupt flows must reject stale, duplicate, mismatched, already-terminal, unavailable, and unknown cases. | `BRS-NFR-005` | QA tests each negative path against current active run/pending correlation. |
| `SRS-NFR-006` | Status/error categories must be stable and UI/API-handler consumable without parsing raw JSONL. | `BRS-NFR-006` | Product/UX/QA can map categories to accessible copy later. |
| `SRS-NFR-007` | Local audit trail must include safe IDs and lifecycle/failure categories, not raw secrets, raw JSONL, prompt/message/event content, or prompt/response hashes. | `BRS-NFR-007` | Security/privacy/data and release/ops review. |
| `SRS-NFR-008` | Coexistence, independent chat runtime disable, rollback, and compatibility expectations must preserve existing task RPCs unless a separate task-RPC migration is approved. | `BRS-NFR-008` | Release/ops reviews disable/rollback; QA tests compatibility. |
| `SRS-NFR-009` | SDK/gRPC semantics must stay stable across internal JSONL changes where safe; unsupported Codex depth must be typed, not fabricated. | `BRS-NFR-009` | Adapter boundary constrains implementation; QA tests unsupported/narrowed outcomes. |

## Approved Local Bounds And Test Oracles
The BRS has no release-grade SLO/SLA. The following implementation bounds are accepted for local test oracles and config drift checks:
- gRPC inbound/outbound message size defaults to 4 MiB and is capped at 8 MiB.
- In-memory replay defaults to 2000 events, 8 MiB, and 30 minutes, with hard caps of 5000 events, 32 MiB, and 2 hours.
- Pending active requests default to 32 and cap at 64; pending display payload is memory-only, 32 KiB default and 64 KiB cap.
- Status non-pending detail budget defaults to 64 KiB and caps at 256 KiB.
- Bearer token source resolution is limited to exactly one configured env name or absolute file path. External credential-provider command execution is out of v1.
- Codex `--version` probe is 5 seconds; stderr drain cap is 64 KiB.
- App-server start/initialize timeout defaults to 20 seconds and caps at 60 seconds per session group, with caller deadline winning if shorter.
- Chat service health/readiness local dependency check timeout defaults to 2 seconds.
- Supervisor restart backoff is 3 non-cancel/non-deadline failures followed by 30 seconds cooldown scoped to the session group.

QA must assert these values against implementation constants/config and treat drift as a test-basis update requirement.
