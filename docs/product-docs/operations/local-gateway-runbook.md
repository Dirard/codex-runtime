# Runbook: Local Gateway

Type: product behavior / operations runbook
Status: current implemented behavior for the working-copy baseline; full QA and release/current-state pending
Owner: system_analyst
Residual release/current-state gate owners: release_ops_owner, security_privacy_data_owner
Consumer / intended use: local operators, developers, QA, and reviewers.
Last repaired: 2026-06-16
Related docs: `docs/product-docs/configuration/gateway-runtime-config.md`, `docs/product-docs/observability/event-stream-observability.md`, `docs/epics/chat-first-runtime/SRS/rollout.md`, `docs/epics/chat-first-runtime/SRS/contracts.md`
Trace IDs: DOC-OPS-001, SRS-FR-006, SRS-FR-008, SRS-FR-010, SRS-FR-011, SRS-FR-017, SRS-FR-019, SRS-NFR-003, SRS-NFR-004, SRS-NFR-008

## Source Of Truth
This product-doc owns the current implemented local operations behavior for the gateway in this working copy. It is not a release/current-state claim, not full-QA evidence, and is not a release plan.

## Symptoms
- Gateway cannot start.
- Codex child process cannot start or exits.
- SDK cannot authenticate to gateway.
- App-stored `chat_id` cannot be read or continued by Codex.
- Stream cursor is unsupported, narrowed, or unavailable.
- Pending request or interrupt appears stuck.

## Dashboards / Alerts
Not applicable for first local implementation.

## First Checks
- Confirm configured `codex_binary` exists.
- Confirm listen address is loopback.
- Confirm client token source is configured without printing the value.
- Confirm `chat_runtime.enabled` expected value; when omitted, the current implementation defaults it to enabled.
- Confirm `strict_schema_verification`, `child_env_allowlist`, and any `credential_providers` match the intended local setup.
- Confirm session group workspace paths are valid.
- Confirm gRPC health/equivalent readiness reports gateway and `ChatRuntimeService` serving state.

## Operational Checks
- Gateway startup and graceful shutdown.
- Codex app-server supervisor lifecycle.
- gRPC health or equivalent local readiness check for gateway and `ChatRuntimeService`.
- Current-process replay, pending, interrupt, and idempotency behavior.
- Supervisor backoff check: after 3 non-cancel/non-deadline failures, a 30s cooldown is scoped to the affected session group.

## Support Diagnostics
Use sanitized logs, chat IDs, turn/run IDs, pending IDs, event/cursor IDs, gateway warnings, and safe reason codes. Do not paste raw token values, private prompt/message/event content, or unredacted app-server JSONL.

## Mitigation
- Restart gateway for local transient failures.
- If Codex app-server or Codex thread state is unavailable, report typed unavailable/unknown/narrowed status rather than fabricating chat state.
- After gateway restart, tell callers that current-process replay, pending correlation, and idempotency memory may be unavailable; callers can retry `GetChat(chat_id)` only as a Codex Thread id lookup.

## Feature Disable Path
`chat_runtime.enabled=false` disables or omits `ChatRuntimeService`, reports its health/readiness as `NOT_SERVING`, and leaves Codex-owned thread/history state untouched. Existing task RPCs may continue when their own dependencies are healthy and overall gateway/process readiness may remain serving for that task-RPC surface.

## Rollback / Rollforward
Rollback must not expose task identity as chat identity and must not promise recovery of process-local replay/pending/idempotency state. Existing task RPC behavior remains the compatibility surface unless a separate approved migration exists.

## Escalation / Contact
release_ops_owner for operations questions; security_privacy_data_owner for secret or data exposure risk.

## Customer Impact Notes
No external/customer release is in scope yet.

## Supported Environments
Supported target is local-only v1 for Windows development/test with loopback gateway, installed `codex.exe`, local bearer auth, configured session groups, and process-local gateway state. Remote, team-shared, production, multi-user, or multi-tenant environments require new product, security/privacy/data, release/ops, QA, and current-state decisions.

## Forbidden Content
- Raw secrets, credentials, tokens, private keys, passwords, auth headers, unredacted logs, or customer PII dumps.
- Unsafe production actions without owner/approval.
