# Security Rule: Local Runtime Boundary

Type: product behavior / security rule
Status: current implemented behavior for the working-copy baseline; full QA and release/current-state pending
Owner: system_analyst
Residual release/current-state gate owners: security_privacy_data_owner
Consumer / intended use: gateway/SDK developers, reviewers, QA, and local operators.
Last repaired: 2026-06-16
Related docs: `docs/product-docs/grpc/codex-runtime-gateway.md`, `docs/product-docs/configuration/gateway-runtime-config.md`, `docs/epics/chat-first-runtime/SRS/feature.md`, `docs/epics/chat-first-runtime/SRS/contracts.md`
Trace IDs: DOC-SEC-001, SRS-FR-009, SRS-FR-010, SRS-FR-011, SRS-FR-012, SRS-FR-013, SRS-FR-019, SRS-NFR-002

## Source Of Truth
This product-doc owns the current implemented security boundary for the local runtime gateway in this working copy. It is not a release/current-state claim, not full-QA evidence, and does not weaken the local-only boundary.

## Authentication
- Gateway config must contain exactly one local client bearer token source: env name or absolute file path.
- External credential-provider command execution for bearer-token sourcing is out of v1 unless a later BRS/SRS/security review approves it.
- Every unary and streaming gRPC request must include exactly one `authorization` metadata value equal to `Bearer <configured token>`.
- Missing, duplicate, malformed, or wrong bearer metadata returns `UNAUTHENTICATED` with a generic safe message.
- Authorization metadata is stripped before handlers, logs, adapters, diagnostics, and downstream contexts.
- If token source config is missing, invalid, unreadable, or syntactically invalid, gateway startup fails before accepting traffic.
- Documentation and tests must use sanitized fixture token sources only and must not include token values or auth header values.

## Authorization / Roles / Permissions
- First implementation targets trusted local clients, not a multi-tenant remote service.
- SDK clients are bound to one `session_group_id` and `workspace_id`; every gRPC request must carry them or the exact client-bound equivalent.
- `StartChatRun` authorizes the requested session/workspace before calling Codex.
- Existing-chat operations validate the requested session/workspace before using `chat_id` as Codex Thread id.
- Invalid or not configured session/workspace context returns `INVALID_ARGUMENT`.
- Unknown or not-found chat in authorized local context returns `NOT_FOUND` or typed unavailable/unknown when absence cannot be proven.
- No Codex app-server call may happen before auth, validation, and workspace/session authorization pass.
- Pending approval decisions must preserve Codex decision semantics and must not auto-grant permissions outside approved scope.

## Tenant Isolation
Not a multi-tenant service in this scope. Any future multi-user or remote access requires new BRS/SRS/security review.

## Data Access Boundaries
- Public `chat_id` is Codex `Thread.id`; no durable gateway-owned identity store exists in v1.
- Gateway may keep process-local active run, replay, pending, idempotency, and diagnostic correlation.
- Gateway must not store prompt/message/event/history payloads, raw JSONL, prompt/response hashes, raw request payloads, or raw response payloads.
- Gateway must not expose raw app-server JSONL as a stable public data contract.
- Authorized SDK/gRPC callers may receive transient normalized projections of Codex-owned history/events/pending display data where Codex supports them and after auth/authz.
- Logs, docs, examples, diagnostics, and errors must not leak raw secrets or private data.

## Secret Handling
- Token values, auth headers, cookies, passwords, private keys, and environment secret values are never documented.
- Configuration docs may name secret-bearing settings and their purpose without values.

## Privacy / PII
No customer PII handling is approved in this scope. Prompt/message/event/history content and pending display content may be private and therefore must stay out of gateway retention, logs, docs, examples, diagnostics, and QA evidence. Transient authorized SDK/gRPC projections are allowed only where Codex supports them and after auth/authz.

## Audit Trail
Minimum audit trail target: chat/run lifecycle, auth failure class, pending request creation/resolution, interrupt request, app-server process lifecycle, gateway warnings, and errors with redacted identifiers.

## Retention And Idempotency
- Replay is process-memory only: 2000 events, 8 MiB, 30m defaults with 5000 events, 32 MiB, 2h caps; replay is not durable across gateway restart.
- Idempotency is process-local in v1 and may be lost after gateway restart.
- Idempotency records must not store prompt/content fingerprints, prompt/response digests, raw request payloads, or raw responses.

## Abuse Cases
- Remote exposure of loopback gateway.
- Token leakage through docs/logs/errors.
- Confusing app `chat_id` with task compatibility identity or run identity.
- Replaying or resolving stale pending requests.
- Retaining prompt/message/event history in gateway storage.
- Duplicating Codex side effects after unknown idempotency retry delivery.

## Security-Sensitive Edge Cases
- Gateway restart with stale active pending request.
- Codex app-server failure during permission request.
- Interrupt while command/file-change approval is pending.
- Cursor replay of unsupported, narrowed, redacted, or unknown raw events.

## Forbidden Content
- Raw secrets, tokens, auth headers, cookies, passwords, private keys, exploit payloads that increase risk, or customer PII dumps.
- Security decisions without owner.
