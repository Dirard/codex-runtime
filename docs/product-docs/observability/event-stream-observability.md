# Observability: Event Stream

Type: product behavior / observability contract
Status: current implemented behavior for the working-copy baseline; full QA and release/current-state pending
Owner: system_analyst
Residual release/current-state gate owners: release_ops_owner, qa_engineer, security_privacy_data_owner
Consumer / intended use: gateway developers, QA, local operators, and reviewers.
Last repaired: 2026-06-16
Related docs: `docs/product-docs/domain/chat-runtime.md`, `docs/product-docs/security/local-runtime-boundary.md`, `docs/epics/chat-first-runtime/SRS/contracts.md`, `docs/epics/chat-first-runtime/SRS/sequences.md`
Trace IDs: DOC-OBS-001, SRS-FR-006, SRS-FR-007, SRS-FR-008, SRS-FR-009, SRS-FR-010, SRS-FR-011, SRS-FR-014, SRS-FR-019, SRS-NFR-004, SRS-NFR-007

## Source Of Truth
This product-doc owns the current implemented observability behavior for event-stream and runtime diagnostics in this working copy. It is not a release/current-state claim, not full-QA evidence, and does not establish production SLOs.

## Logs
- Logs should identify gateway lifecycle, chat runtime enable/disable state, Codex child process lifecycle, chat/run state transitions, auth failure class, pending request lifecycle, interrupt requests, stream replay misses, app-server restart backoff, and adapter warnings.
- Logs must redact secrets and private data.

## Metrics
Formal production metrics are not required for first local implementation. Safe local counters through logs or diagnostics must cover active chats/runs, active stream subscribers, pending requests, replay misses, app-server failures, and current-process correlation misses.

## Traces
Distributed tracing is not in scope for first implementation.

## Correlation IDs
Use `chat_id`, `run_id`, client request IDs, pending IDs, and event/cursor correlation IDs in safe diagnostic contexts. Do not expose raw auth, raw JSONL, prompt text, assistant text, command output, event payloads, history content, or private data.

## Expected Failure Signals
- Codex app-server unavailable.
- Codex thread not found or cannot be read/continued.
- Session/workspace authz mismatch.
- Stream cursor unsupported, evicted, unavailable, or narrowed.
- Current-process pending correlation unavailable after restart.
- Interrupt already terminal/already interrupting.
- App-server restart backoff by session group.

## Health / Readiness
The gateway must provide gRPC health or equivalent local readiness for gateway process and `ChatRuntimeService` readiness. Readiness requires valid config, valid auth token source, loopback listen, valid session groups, and valid Codex binary identity. Lazy app-server child processes do not need to be running for readiness.

When `chat_runtime.enabled=false`, `ChatRuntimeService` readiness is `NOT_SERVING`; overall gateway/process readiness may still be serving for task RPCs when their own dependencies are healthy.

When an app-server supervisor is in restart backoff or closed, `ChatRuntimeService` readiness is also `NOT_SERVING` until the affected session-group runtime recovers.

## Redaction Rules
Errors, logs, docs, examples, local diagnostics, and QA evidence must not include raw tokens, auth headers, private keys, cookies, passwords, raw environment values, raw JSONL, prompt text, assistant text, command output, event payloads, history content, prompt/response hashes, or private data dumps.

## Post-Release Watch Metrics
Deferred until release/current-state path exists.

## Forbidden Content
- Raw secret values, tokens, auth headers, cookies, private keys, passwords, private traces, or customer PII dumps.
- Unredacted logs.
- Release delta or temporary change notes.
