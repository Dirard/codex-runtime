# SRS Rollout: Chat-First Runtime

Type: software requirements specification
Status: paper-ready after accepted targeted product/engineering re-reviews; root implementation approval received
Owner: system_analyst
Requested mode: full
Required mode floor: full
Approved mode: full
Lane: High-risk
Last repaired: 2026-06-15
Related docs: `index.md`, `feature.md`, `contracts.md`, `states-and-outcomes.md`, `transient-correlations.md`, `events-history.md`, `sequences.md`, `nfr-mapping.md`, `traceability.md`, `../../../product-docs/configuration/gateway-runtime-config.md`, `../../../product-docs/operations/local-gateway-runbook.md`, `../../../product-docs/observability/event-stream-observability.md`, `../../../product-docs/security/local-runtime-boundary.md`
Related product-docs: `DOC-CONFIG-001`, `DOC-OPS-001`, `DOC-OBS-001`, `DOC-SEC-001`, `DOC-GRPC-001`
Trace IDs: SRS-FR-001..SRS-FR-019, SRS-NFR-001..SRS-NFR-009

## Source Of Truth
This SRS owns rollout, transition, compatibility, and readiness requirements for the chat-first runtime change. It does not record release execution facts, current-state acceptance, production acceptance, or implementation details.

No release-impacting deployment or current-state milestone is approved by this document.

## Release / Rollout Scope
Current scope:
- local development/test target only;
- future chat-first SDK/gRPC/gateway behavior;
- draft target docs and SRS only until required owner reviews and future delivery-readiness gates are complete.

Not in scope:
- production/external release;
- remote or multi-tenant gateway exposure;
- Desktop UI synchronization;
- release note, release manifest, production acceptance, or current-state acceptance.

## Deployment Order Requirements
Future delivery readiness must preserve this order:

1. Register or expose a dedicated chat-first gRPC/proto surface separately from existing `codex.control.v1.CodexControl`.
2. Apply v1 config precedence, then validate loopback listen, bearer token source, session groups, Codex binary identity, and local limits before chat service readiness.
3. Enforce authentication, request validation, session/workspace authorization, and current-process idempotency before any side-effecting Codex call.
4. Use the minimal lazy `codex.exe app-server` supervisor foundation per session group before first side-effecting Codex access; startup timeout, caller-deadline precedence, repeated-failure cooldown, and typed dependency failure apply here.
5. For new chat, prove Codex thread identity and accepted first turn, and return `chat_id = Thread.id` only after first-turn acceptance/correlation.
6. Expose health/readiness and redacted diagnostics before QA acceptance.
7. Verify existing task RPC compatibility and independent chat disable behavior before implementation handoff can be considered ready.

## Feature Flags / Kill Switch
`chat_runtime.enabled` is the independent v1 disable path.

Required behavior:
- `chat_runtime.enabled=false` disables `ChatRuntimeService` registration or makes chat methods return `UNIMPLEMENTED` / typed `chat_runtime_disabled`.
- `ChatRuntimeService` health/readiness must be `NOT_SERVING` or an equivalent disabled/not-serving state with safe reason `chat_runtime_disabled` while disabled.
- Overall gateway/process readiness may still be serving for task RPCs when their own dependencies are healthy.
- Existing task RPC behavior must continue unchanged when its own dependencies are healthy.

## Configuration Precedence
V1 config precedence is fixed:

1. Built-in defaults.
2. TOML.
3. Documented process flags; the current approved runtime override is `--listen` only. `--config` is the required config locator, not a runtime config override.
4. Runtime secret resolution from the configured source only.

There is no non-secret environment variable override in v1. Unknown or duplicate TOML keys fail validation. Process flag overrides must pass the same validation as TOML-derived values, including loopback/local listen constraints.

## Backfill / Replay Dependency
- No gateway identity database or Desktop UI chat import/backfill is required for v1.
- Arbitrary Codex Desktop history import is not required.
- History availability is bounded by Codex support and approved turn summary projection.
- Event replay beyond current process in-memory buffers must fail with typed unavailable/out-of-range/narrowed behavior.
- Idempotent replay beyond current-process memory is not available; a pre-restart idempotency key alone must not be treated as proof of prior side effect or no-duplicate recovery.

## Rollback / Rollforward Rules
Rollback requirements:
- Rollback must not expose task identity as a substitute for `chat_id`.
- Existing task-oriented RPC behavior must not be silently broken unless explicitly approved.
- Disabling chat runtime must not delete or mutate Codex-owned thread/history state.

Rollforward requirements:
- Adapter/proto changes must preserve or intentionally migrate public error/event/status semantics.
- Unknown process-local state after restart/upgrade must become typed unknown/unavailable state or normal operation-specific precondition behavior, not fabricated completion or fabricated idempotent replay.

## Environment Expectations
Supported target environment:
- local-only v1 for Windows development/test;
- loopback/local gateway only;
- installed `codex.exe`;
- local client authentication;
- configured workspace/session groups;
- process-local gateway state only.

Any remote, team-shared, production, multi-user, or multi-tenant environment reopens BRS, SRS, security/privacy/data, operations, QA, and release readiness.

## Monitoring / Readiness Checks
Because no release/current-state milestone is active, these are implementation-readiness check requirements, not post-release facts:

- gateway starts and reports readiness without dumping secrets;
- gRPC health or equivalent readiness reports gateway and `ChatRuntimeService` serving state;
- when `chat_runtime.enabled=false`, `ChatRuntimeService` readiness is `NOT_SERVING` or equivalent with safe reason `chat_runtime_disabled` while task RPC readiness may remain serving;
- configured `codex.exe` path is validated;
- loopback listen address and local auth are enforced;
- new chat returns `chat_id = Thread.id` only after first-turn acceptance/correlation;
- event stream emits normalized current live events;
- replay cursor unsupported/unavailable/narrowed failure is typed and redacted;
- pending request lifecycle is current-process only and stale/duplicate responses are rejected;
- post-restart idempotency keys are not recognized as prior requests; any recovery must come from Codex-proven state through a usable `chat_id`;
- interrupt reports accepted/already-terminal/already-interrupting/not-found/unavailable cases;
- Codex app-server unavailable state is explicit;
- logs/diagnostics redact tokens, auth headers, private keys, raw env values, raw JSONL, and private data.

## Stop If
- The proto path leaves SDK implementers guessing whether a field is `chat_id` or run/task identity.
- Codex app-server cannot provide or resume the thread required by `chat.Run`.
- Implementation adds durable gateway identity storage for v1.
- Implementation stores prompt/message/event/history payloads, raw JSONL, prompt/response hashes, or raw auth material.
- Security owner rejects local auth, workspace/session access, pending response, idempotency, or redaction rules.
- Implementation promises cross-restart idempotent replay, exactly-once delivery, or no-duplicate suppression based only on a gateway idempotency key.
- Release/current-state, remote access, multi-tenant access, Desktop UI synchronization, or production operations enter scope.
- Tests cannot cover restart/replay/pending/interrupt/redaction at the required high-risk depth.

## Acceptance / Test Basis
QA must recreate or revalidate rollout and recovery test cases from `traceability.md`. Additional rollout-readiness checks must be added before implementation if tech design introduces service split, feature flags, or release/current-state scope.

## Trace Links
| Rollout concern | Product-doc refs | SRS refs | Tests |
| --- | --- | --- | --- |
| Start/restart behavior | `DOC-OPS-001`, `DOC-OBS-001` | `SRS-FR-004`, `SRS-FR-018`, `SRS-FR-019`, `SRS-NFR-003` | start/restart tests |
| Config/auth readiness | `DOC-CONFIG-001`, `DOC-SEC-001` | `SRS-FR-013`, `SRS-NFR-002` | auth/config/redaction tests |
| Stream/replay diagnostics | `DOC-OBS-001`, `DOC-OPS-001` | `SRS-FR-008`, `SRS-FR-011`, `SRS-NFR-004` | stream/replay diagnostics tests |
| Compatibility/rollback | `DOC-GRPC-001`, `DOC-OPS-001` | `SRS-FR-012`, `SRS-FR-017`, `SRS-FR-019`, `SRS-NFR-008` | compatibility/rollback tests |
