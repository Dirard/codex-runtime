# Plan: Stage 03 Local Readiness, Security Boundary, And Supervisor Foundation

Type: implementation plan slice
Status: implemented locally with product, engineering, and security/data re-review pass; not full QA, Done, current-docs sync, or release approval
Owner: system_analyst
Last repaired: 2026-06-16
Related docs: `index.md`, `../SRS/contracts.md`, `../SRS/states-and-outcomes.md`, `../SRS/rollout.md`, `../SRS/nfr-mapping.md`, `../tech-design/tech-design.md`, `../reviews/security-privacy-data-review.md`, `../reviews/release-ops-review.md`, `../tests/test-strategy.md`, `../tests/test-cases.md`
Trace IDs: PLAN-STAGE-003, SRS-FR-013, SRS-FR-014, SRS-FR-017, SRS-FR-019, SRS-NFR-002, SRS-NFR-004, SRS-NFR-007, SRS-NFR-008

## Goal
Ensure every chat-first entry point is local, authenticated, authorized, redacted, readiness-gated, and backed by the minimal app-server supervisor foundation before any Codex side effect can occur.

## Scope
- Config precedence and validation for chat runtime.
- `chat_runtime.enabled` independent disable path.
- Exact local bearer auth behavior and metadata stripping.
- Session/workspace selector validation and authorization.
- Loopback/local listen constraints.
- Secret-source references without raw values.
- Redacted diagnostics, errors, examples, logs, and QA evidence.
- Gateway and `ChatRuntimeService` readiness/health behavior.
- Minimal lazy `codex.exe app-server` supervisor foundation for authorized session groups before `thread/start`, `turn/start`, pending response, interrupt, or Codex-backed reads.
- Supervisor startup timeout, caller-deadline precedence, repeated-failure cooldown, safe dependency failure, and redacted backoff reason categories.

## Implementation Steps
1. Implement config loading with fixed precedence: built-in defaults, TOML, documented process flags, then runtime secret resolution from the configured source.
2. Fail validation on unknown or duplicate TOML keys, invalid listen address, invalid Codex binary identity, invalid token source, invalid limits, or invalid session/workspace definitions.
3. Enforce loopback/local listen; process flag `--listen` must pass the same validation as TOML-derived config.
4. Resolve exactly one local bearer token source from either one configured env name or one absolute file path; do not support external credential-provider commands or non-secret env overrides.
5. For every unary and streaming chat RPC, require exactly one bearer metadata value matching `Bearer <configured token>`.
6. Strip authorization metadata before handlers, logs, adapters, diagnostics, downstream contexts, and test evidence.
7. Validate request shape, IDs, cursor, prompt, idempotency key, deadline/size limits, session group, and workspace before any Codex call or stream attach.
8. Authorize requested local session/workspace before `thread/start`, `turn/start`, history/status reads, stream attach, pending response, interrupt, or diagnostics disclosure.
9. Implement safe generic auth/authz/validation errors that do not reveal other workspace/session/thread state.
10. Implement `chat_runtime.enabled=false` so the chat service is disabled or returns typed disabled/unimplemented behavior while healthy task RPCs may remain serving.
11. Implement one lazy app-server supervisor foundation per configured session group, invoked only after Stage 03 auth/validation/authz passes and before any Codex side effect or Codex-backed state read.
12. Apply app-server startup/initialize timeout, caller-deadline precedence, repeated non-cancel/non-deadline failure cooldown, safe backoff reason logging, and typed dependency failure.
13. Expose readiness/health for gateway and chat service with safe reason codes only, including app-server dependency unavailable/backoff states where relevant.
14. Add redaction utilities/tests for logs, errors, diagnostics, examples, and future QA evidence.

## Acceptance / Checks
- Auth, validation, and authorization happen before every possible Codex side effect or state disclosure.
- Auth material never reaches logs, diagnostics, adapters, examples, tests, or QA evidence.
- Invalid config fails before traffic; disabled chat runtime does not redefine task RPC behavior.
- Health/readiness states are explicit and redacted.
- Stage 05/06/07 can rely on a pre-side-effect supervisor foundation before `thread/start`, `turn/start`, pending response, interrupt, or Codex-backed reads.
- Supervisor startup timeout/backoff is typed and redacted, and supervisor start occurs only after authorized work needs Codex.
- Bounds from `SRS/nfr-mapping.md` are represented as implementation constants/config and are testable for drift.

## Traceability
- SRS: `SRS-FR-013`, `SRS-FR-014`, `SRS-FR-017`, `SRS-FR-019`, `SRS-NFR-002`, `SRS-NFR-004`, `SRS-NFR-007`, `SRS-NFR-008`.
- QA: `TC-028`..`TC-033`, `TC-034`, `TC-035`, `TC-036`.
- Regression: `REG-010`, `REG-011`, `REG-012`.

## Stop And Ask If
- A non-local, shared, hosted, production, or multi-user auth model is requested.
- Implementation needs to read, log, display, or store raw secret values.
- Config precedence, disabled-service behavior, or readiness semantics would differ from accepted SRS/tech design/reviews.
