# BRS NFR: Chat-First Runtime

Type: business requirements specification
Status: product baseline; ready for product owner review
Owner: business_analyst
Last repaired: 2026-06-14
Related docs: `feature.md`, `../reviews/product-review.md`
Trace IDs: BRS-NFR-001..BRS-NFR-009

## Source Of Truth
This BRS owns business-level non-functional expectations for the local chat-first runtime. It does not choose final API/wire shape, implementation mechanics, observability stack, rollout strategy, or release readiness.

The v1 NFR baseline is local only: gateway is a convenient gRPC compatibility layer over working Codex, Codex owns all real functionality and history, `chat_id` is Codex `Thread.id`, and gateway retains only process-local operational correlation.

## Performance Expectations
- `BRS-NFR-001`: SDK/gRPC calls must feel suitable for interactive local chat usage and avoid avoidable gateway latency, while Codex model/tool execution time remains outside gateway control.
- Start, status, history, stream attach, pending response, and interrupt paths should avoid unbounded local scans.
- Event streaming should deliver Codex-originated events in order for a chat/run where Codex supports that behavior and surface backpressure, deadline, timeout, or unavailable states as typed outcomes.
- No release-grade SLO/SLA is created before final implementation and explicit release readiness.

## Security / Privacy / Compliance Expectations
- `BRS-NFR-002`: local auth and secret handling must avoid raw token, auth header, cookie, password, private key, environment secret, or credential exposure in docs, tests, logs, examples, errors, prompts, summaries, or QA evidence.
- gRPC access is local authenticated client only in v1. Remote or multi-tenant access requires new product/security/privacy/data decisions.
- The gateway must not expose raw app-server JSONL as a public data contract or diagnostic dump.
- Gateway state must not retain prompt/message/event/history content, raw JSONL, raw request/response payloads, prompt/response hashes, or private data dumps.
- Prompt, message, event, and history data belongs to Codex. Any future retained content requires product, privacy, data, security, technical, QA, and release readiness review.

## Availability / Reliability Expectations
- `BRS-NFR-003`: after a gateway restart, the application may reuse `chat_id` as Codex `Thread.id` only where installed Codex can read or continue that thread.
- Gateway restart does not promise recovery of process-local stream replay buffers, pending correlation, current idempotency memory, active run correlation, or stream subscribers.
- The gateway must prefer typed unavailable/unknown results over fabricated success when Codex, live stream state, pending state, or current turn state cannot be proven.
- Reliability does not mean recovering arbitrary Desktop UI chats, owning chat history in the gateway, or reconstructing Codex state after process failure.

## Operability / Supportability Expectations
- `BRS-NFR-004`: errors and diagnostics must be safe, typed, and useful for local QA/support.
- Operators/developers should be able to diagnose gateway start failure, Codex app-server unavailability, auth failure, unknown thread, unsupported/narrowed capability, replay cursor unavailable, stale pending request, and interrupt already-terminal/already-interrupting cases.
- Configuration must make Codex binary path, loopback listen address, token source, session/workspace routing, replay/pending limits, message-size/deadline limits, and app-server supervision understandable without printing secret values.

## Pending / Interrupt Safety
- `BRS-NFR-005`: security-sensitive pending and interrupt flows must reject stale, duplicate, mismatched, already-terminal, unavailable, and unknown cases with typed safe outcomes.
- Pending display data may be transiently delivered to authorized callers where Codex supports it, but must not be retained in gateway docs, examples, logs, diagnostics, or QA evidence.

## UI / API Consumability
- `BRS-NFR-006`: status, pending, interrupt, and error categories should be understandable enough for an API handler or UI to translate without parsing raw JSONL.
- Minimum status meanings are Codex-thread not loaded/idle/active or running/waiting on approval/waiting on user input/system error, current/last turn in progress/completed/interrupted/failed, and typed invalid/not-found/unavailable/unknown/unsupported/narrowed outcomes.

## Auditability Expectations
- `BRS-NFR-007`: the runtime should produce a safe local troubleshooting trail for lifecycle, auth failure class, pending request routing, interrupt request, stream replay miss, gateway warning, and terminal failure category.
- Audit/log records may use safe identifiers such as `chat_id`, `run_id`, request IDs, pending IDs, event IDs, and safe reason codes.
- Audit/log records must not include raw secrets, raw JSONL, prompt/message/event/history content, or private data dumps.

## Compatibility / Disable / Rollback Expectations
- `BRS-NFR-008`: disable and rollback expectations must be defined before implementation readiness if chat-first runtime coexists with current task RPCs.
- Disabling the chat-first service must not imply deletion or mutation of Codex-owned chat state.
- Existing task RPC behavior must not be silently redefined by chat-first work unless a separate product decision approves that change.

## Contract Stability Expectations
- `BRS-NFR-009`: SDK/gRPC semantics should stay stable for application callers while gateway internals adapt to internal app-server JSONL changes where feasible.
- Breaking SDK/gRPC behavior changes require requirements, product-doc, test, and product-owner acceptance updates.
- Unsupported or partially supported installed-Codex capabilities must be represented as typed unsupported, unavailable, unknown, or narrowed outcomes rather than hidden by fabricated success.
- Localization is not in first scope, but error/status categories should be stable enough for a future UI/API handler to localize messages without relying on internal JSONL text.

## Acceptance At Business Level
NFR acceptance for implementation readiness requires:
- Each NFR maps to downstream requirements, product-doc sections, test strategy/test cases, and owner review evidence.
- Architect/system analyst define measurable or explicitly bounded local thresholds for latency, replay, diagnostics, unsupported outcomes, and compatibility.
- Security/privacy/data owner approves local-only auth, redaction, process-local gateway data, logging, and privacy assumptions.
- QA confirms coverage for compatibility, restart limits, typed safe errors, redaction, pending/interrupt safety, and unsupported underlying Codex behavior.
- Release/ops owner confirms release/SLO artifacts remain inactive until final implementation and explicit release readiness work.

## Constraints / Must Preserve
- Local-only v1 scope.
- Separation between stable external SDK/gRPC semantics and unstable internal app-server JSONL.
- Explicit `Chat` lifecycle/status and `chat_id == Codex Thread.id`.
- No `chat_id`/task identity ambiguity.
- Codex ownership of real functionality and prompt/message/event/history data.
- Gateway process-local correlation only; no durable gateway identity store in v1.
- No Desktop UI visible/current-thread promise.
- Redacted-first docs/tests/logs/errors/diagnostics.
- No release-grade SLO/SLA without final implementation and explicit release readiness.

## Risks
- Internal app-server JSONL may not expose enough stable signal for a promised NFR; product must narrow or mark the behavior unsupported/unavailable rather than fabricate.
- Restart expectations may be misunderstood as gateway continuity rather than Codex Thread id reuse.
- Gateway implementation may be tempted to persist prompt/message/event history for convenience.
- Local-only auth may be insufficient if any future packaging exposes the gateway beyond loopback.
- No accepted performance thresholds exist yet.
- Release readiness milestones are intentionally inactive now.

## Open Questions
None.

## Trace Links
| NFR ID | Business expectation | Downstream validation target |
| --- | --- | --- |
| `BRS-NFR-001` | Interactive local performance and bounded gateway latency | Downstream requirements, local thresholds, QA strategy, observability docs |
| `BRS-NFR-002` | Local auth, redaction, and secret handling | Security/privacy/data requirements, configuration docs, diagnostics rules |
| `BRS-NFR-003` | Restart behavior bounded to Codex Thread id reuse | Downstream requirements, restart tests, support docs |
| `BRS-NFR-004` | Safe typed diagnostics for QA/support | Error/status requirements, observability/support docs |
| `BRS-NFR-005` | Pending/interrupt safety | Downstream requirements and QA tests |
| `BRS-NFR-006` | API/UI consumable status/error categories | Domain/SDK requirements and docs |
| `BRS-NFR-007` | Safe local audit trail | Security/privacy/data, observability, support docs |
| `BRS-NFR-008` | Operability, disable, rollback expectations | Config/ops requirements and release readiness review |
| `BRS-NFR-009` | External compatibility and typed unsupported depth | SDK/gateway compatibility requirements and QA tests |

## Forbidden Content
- Implementation design as the source of product expectations.
- Detailed API/wire format, deployment plan, release acceptance, or release-grade SLO/SLA promises.
- Durable gateway-owned identity storage requirements for v1.
- Raw secrets, credentials, auth headers, cookies, private keys, passwords, private prompt content, customer PII dumps, or unredacted logs.
