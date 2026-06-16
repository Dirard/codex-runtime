# SRS: Chat-First Runtime Feature

Type: software requirements specification
Status: paper-ready after accepted targeted product/engineering re-reviews; root implementation approval received
Owner: system_analyst
Requested mode: full
Required mode floor: full
Approved mode: full
Lane: High-risk
Last repaired: 2026-06-15
Related docs: `index.md`, `../BRS/feature.md`, `../BRS/nfr.md`, `contracts.md`, `states-and-outcomes.md`, `transient-correlations.md`, `events-history.md`, `sequences.md`, `nfr-mapping.md`, `traceability.md`, `rollout.md`, `../../../product-docs/sdk/chat-first-go-sdk.md`, `../../../product-docs/grpc/codex-runtime-gateway.md`, `../../../product-docs/domain/chat-runtime.md`, `../../../product-docs/security/local-runtime-boundary.md`, `../../../product-docs/configuration/gateway-runtime-config.md`, `../../../product-docs/observability/event-stream-observability.md`, `../../../product-docs/operations/local-gateway-runbook.md`
Related BRS: `BRS-GOAL-001`, `BRS-REQ-001`..`BRS-REQ-007`, `BRS-RULE-001`..`BRS-RULE-014`, `BRS-NFR-001`..`BRS-NFR-009`
Related product-docs: `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001`, `DOC-SEC-001`, `DOC-CONFIG-001`, `DOC-OBS-001`, `DOC-OPS-001`
Trace IDs: SRS-FR-001..SRS-FR-019, SRS-NFR-001..SRS-NFR-009
Stop-if: implementation makes `chat_id` differ from Codex `Thread.id`; gateway is asked to own prompt/message/event history; installed Codex cannot support a promised behavior; scope expands to Desktop UI synchronization, remote access, multi-tenant use, or active release/current-state.

## Product Goal
Provide an external runtime around installed Codex:

```text
web app -> API handler with Go SDK -> local gateway -> codex.exe app-server
```

The Go SDK and gateway-facing gRPC/proto contract must be stable and chat-first. The local gateway translates between the stable external contract and unstable/internal Codex app-server JSONL.

## Functional Requirements
| ID | Requirement | Product-doc refs | BRS refs | Test refs |
| --- | --- | --- | --- | --- |
| `SRS-FR-001` | Public SDK/gRPC behavior must be chat-first through a dedicated chat-first gateway contract and must not require callers to parse raw JSONL or use task-oriented identities as primary chat identity. | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001` | `BRS-GOAL-001`, `BRS-REQ-001`, `BRS-REQ-003` | SDK/gRPC contract tests |
| `SRS-FR-002` | Withdrawn: separate durable chat identity is not a v1 requirement; identity is defined by `SRS-FR-018`. | none | withdrawn | identity guardrail tests |
| `SRS-FR-003` | `codex.GetChat(ctx, chatID)` must resolve an existing Codex thread by treating `chat_id` as Codex `Thread.id`; it must not create a chat, start a run, import Desktop UI history, or expose task-only identity as primary identity. | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001` | `BRS-REQ-001`, `BRS-REQ-004` | lookup/not-found/auth tests |
| `SRS-FR-004` | `codex.Run(ctx, prompt)` must validate a non-empty prompt, use installed-Codex thread plus first-turn capability, and return typed status/stream access only after Codex thread identity and first-turn acceptance/correlation are proven. | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001` | `BRS-REQ-002`, `BRS-REQ-007` | new-chat sequence tests |
| `SRS-FR-005` | `chat.Run(ctx, prompt)` must submit a non-empty new Codex turn to the existing Codex thread identified by `chat_id` when Codex can support continuation. It must not silently start an unrelated thread on continuation failure. | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001` | `BRS-REQ-002`, `BRS-RULE-002`, `BRS-RULE-013` | existing-chat turn tests |
| `SRS-FR-006` | Status must distinguish invalid/unknown/not-found/unavailable thread outcomes, normalized Codex thread lifecycle, and current/last turn lifecycle where applicable. | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001`, `DOC-OBS-001` | `BRS-REQ-007`, `BRS-RULE-010` | status tests |
| `SRS-FR-007` | `chat.GetHistory(ctx)` must return Codex-owned turn summary fields/projection where Codex supports it, with typed unsupported/unavailable/unknown/narrowed outcomes for missing or partial support. | `DOC-SDK-001`, `DOC-DOMAIN-001` | `BRS-REQ-007`, `BRS-RULE-012`, `BRS-RULE-013` | history-depth tests |
| `SRS-FR-008` | `chat.GetEventsStream(ctx)` must provide normalized live current events and typed replay/unavailable/narrowed outcomes when replay cannot be proven in the current process. | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-OBS-001` | `BRS-REQ-007`, `BRS-NFR-001`, `BRS-NFR-009` | live stream/replay tests |
| `SRS-FR-009` | Pending approval/user-input states and responses must be Codex-backed, scoped to the active turn/run, and reject stale, duplicate, mismatched, terminal, unknown, and unavailable cases. | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-SEC-001` | `BRS-REQ-007`, `BRS-NFR-005` | pending tests |
| `SRS-FR-010` | Interrupt must be explicit, active-turn scoped, Codex-backed, and typed; SDK/gRPC stream cancellation must not be treated as interrupt. | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001`, `DOC-SEC-001` | `BRS-REQ-007`, `BRS-NFR-005` | interrupt tests |
| `SRS-FR-011` | Unsupported, unavailable, unknown, stale, duplicate, terminal, and narrowed outcomes must be externally typed and must never be hidden as success. | `DOC-GRPC-001`, `DOC-SDK-001`, `DOC-DOMAIN-001` | `BRS-RULE-011`, `BRS-RULE-012`, `BRS-RULE-013` | negative-path tests |
| `SRS-FR-012` | v1 must serialize active turns per `chat_id`; concurrent runs on the same chat return a typed conflict/precondition outcome. | `DOC-DOMAIN-001`, `DOC-GRPC-001` | `BRS-REQ-007`, `BRS-RISK-006` | active-run conflict tests |
| `SRS-FR-013` | All calls must enforce exact local bearer auth, session/workspace validation, safe diagnostics, and redaction before any Codex side effect. | `DOC-SEC-001`, `DOC-CONFIG-001` | `BRS-REQ-006`, `BRS-NFR-002`, `BRS-RULE-014` | security/redaction tests |
| `SRS-FR-014` | Raw app-server JSONL must not be documented, returned, logged, tested, or exposed as the stable SDK/gRPC contract. | `DOC-GRPC-001`, `DOC-SDK-001`, `DOC-OBS-001` | `BRS-RULE-008`, `BRS-NFR-004` | adapter/redaction tests |
| `SRS-FR-015` | Withdrawn: gateway-owned durable runtime state is not a v1 requirement; process-local state is defined by `SRS-FR-019`. | none | withdrawn | transient-state guardrail tests |
| `SRS-FR-016` | SDK-created chats must not be promised as visible/current Codex Desktop UI threads and must not require Desktop UI synchronization. | `DOC-SDK-001`, `DOC-DOMAIN-001` | `BRS-RULE-009`, `BRS-RISK-003` | docs/behavior tests |
| `SRS-FR-017` | Release/current-state artifacts remain inactive until final implementation and explicit release readiness work. | `DOC-OPS-001` | `BRS-RISK-008`, `BRS-NFR-008` | release gate review |
| `SRS-FR-018` | Public `chat_id` must equal Codex `Thread.id` in v1; SDK/gateway must not mint a second durable chat identity. Public `run_id` must equal Codex turn id where Codex provides one. | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-DOMAIN-001` | `BRS-REQ-004`, `BRS-RULE-003`, `BRS-RULE-004` | identity tests |
| `SRS-FR-019` | Gateway may keep only process-local correlation for active run/turn, stream cursor/replay, pending request, current-process idempotency, and diagnostics; restart loses that gateway-local state and removes any gateway-only no-duplicate guarantee. | `DOC-CONFIG-001`, `DOC-OBS-001`, `DOC-OPS-001` | `BRS-REQ-005`, `BRS-RULE-007`, `BRS-NFR-003` | restart/transient-state tests |

## NFR Mapping
| ID | Requirement | BRS refs | Product-doc refs | Test refs |
| --- | --- | --- | --- | --- |
| `SRS-NFR-001` | Interactive local SDK/gRPC behavior must avoid avoidable gateway latency and unbounded scans while Codex execution time remains outside gateway control. | `BRS-NFR-001` | `DOC-GRPC-001`, `DOC-SDK-001`, `DOC-OBS-001` | local threshold tests |
| `SRS-NFR-002` | Auth, secret handling, redaction, and data minimization must be explicit even though v1 is local-only. | `BRS-NFR-002` | `DOC-SEC-001`, `DOC-CONFIG-001` | security/redaction tests |
| `SRS-NFR-003` | Gateway restart preserves no process-local replay/pending/idempotency state; app-stored `chat_id` can be reused only as Codex Thread id where Codex can prove state, and a pre-restart idempotency key alone cannot provide replay or no-duplicate recovery. | `BRS-NFR-003` | `DOC-CONFIG-001`, `DOC-OPS-001` | restart/recovery tests |
| `SRS-NFR-004` | Diagnostics must be safe and typed for local QA/support without release-grade SLO/SLA promises. | `BRS-NFR-004`, `BRS-NFR-007` | `DOC-OBS-001`, `DOC-OPS-001` | diagnostics tests |
| `SRS-NFR-005` | Pending/interrupt flows must reject stale, duplicate, mismatched, terminal, unavailable, and unknown cases. | `BRS-NFR-005` | `DOC-SDK-001`, `DOC-GRPC-001`, `DOC-SEC-001` | pending/interrupt tests |
| `SRS-NFR-006` | Status/error categories must be stable enough for API-handler/UI translation without parsing raw JSONL. | `BRS-NFR-006` | `DOC-DOMAIN-001`, `DOC-SDK-001` | status/error tests |
| `SRS-NFR-007` | Local audit trail must use safe IDs and lifecycle/failure categories only. | `BRS-NFR-007` | `DOC-OBS-001`, `DOC-SEC-001` | redaction/audit tests |
| `SRS-NFR-008` | Coexistence, disable, rollback, and compatibility expectations must preserve existing task RPCs unless a separate task-RPC migration is approved. | `BRS-NFR-008` | `DOC-OPS-001`, `DOC-GRPC-001` | compatibility tests |
| `SRS-NFR-009` | SDK/gRPC semantics must remain stable across internal JSONL changes where safe; unavailable Codex depth must be typed, not fabricated. | `BRS-NFR-009` | `DOC-GRPC-001`, `DOC-SDK-001`, `DOC-OBS-001` | adapter compatibility tests |

## Constraints
- The runtime wraps an installed Codex; it must not claim capabilities that app-server does not provide.
- Stable public behavior must be expressed through Go SDK and gRPC/proto, not raw JSONL.
- `chat_id` is Codex Thread id; `run_id`, task compatibility identity, event cursor, pending request identity, and idempotency key are distinct.
- Empty chat creation is not required and must not be added as placeholder behavior.
- Turn/summary-level history is Codex-owned; item-level history is not promised because current Codex evidence shows item-level turn history unsupported.
- Release/current-state, Desktop UI synchronization, remote service exposure, and multi-tenant access are out of scope.

## Affected Contracts
- SDK: `../../../product-docs/sdk/chat-first-go-sdk.md`
- gRPC/proto: `../../../product-docs/grpc/codex-runtime-gateway.md`
- Domain: `../../../product-docs/domain/chat-runtime.md`
- Security/privacy: `../../../product-docs/security/local-runtime-boundary.md`
- Configuration: `../../../product-docs/configuration/gateway-runtime-config.md`
- Observability: `../../../product-docs/observability/event-stream-observability.md`
- Operations: `../../../product-docs/operations/local-gateway-runbook.md`

Not applicable in this epic unless scope changes: REST, WebSocket, Kafka, cache, UI.

## Edge Cases
- Prompt is empty, whitespace-only, or exceeds configured size.
- `GetChat` receives malformed, unknown, not found, or unauthorized `chat_id`.
- A second run is requested while a run is active or waiting for pending response.
- Codex thread creation/loading succeeds but first-turn submission fails or is not accepted.
- Gateway restarts during starting, running, pending, interrupting, or event streaming.
- A caller retries after gateway restart with only a pre-restart idempotency key and no usable Codex Thread id.
- Event cursor is unsupported, older than current replay window, unprovable after restart, or points to a different chat/run.
- Stream client disconnects while run continues.
- Context cancellation closes SDK/gRPC stream but does not interrupt Codex.
- Pending request is answered twice, after expiration, with wrong ID, or after terminal state.
- Interrupt arrives before Codex active turn is observed, while already interrupting, or after terminal state.
- Internal JSONL includes an unknown event, malformed payload, raw secret-like value, or private prompt/message/event content.

## Acceptance Basis
SRS review acceptance for this slice requires:
- accepted BRS intent is preserved without task-first drift;
- all FR/NFR items have BRS, product-doc, owner-review, and test-basis trace links;
- `chat_id == Codex Thread.id` and no durable gateway id translation is required;
- states, errors, permissions, stream/history, transient correlation, and stop-if conditions are explicit;
- Desktop UI visibility remains out of scope;
- release/ops constraints remain explicit instead of silently inferred.

## Open Questions
None.

## Stop If
- Final proto direction allows task identity to ambiguously mean `chat_id`.
- Codex app-server lacks a reliable way to continue a thread for `chat.Run`.
- Implementation would add a durable gateway identity store for v1.
- Implementation would persist prompt/message/event/history payloads, raw JSONL, prompt/response hashes, or raw auth material.
- Local authentication or workspace/session access is weaker than the approved bearer/authz policy.
- Product scope expands to Desktop UI visibility, remote access, multi-tenant access, or release/current-state.
