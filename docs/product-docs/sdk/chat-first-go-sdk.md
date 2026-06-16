# SDK Contract: Chat-First Go SDK

Type: product behavior / SDK contract
Status: current implemented behavior for the working-copy baseline; full QA and release/current-state pending
Owner: system_analyst
Residual release/current-state gate owners: product_owner, security_privacy_data_owner
Consumer / intended use: Go SDK implementers, gateway implementers, reviewers, QA, and app integrators.
Last repaired: 2026-06-16
Related docs: `docs/product-docs/grpc/codex-runtime-gateway.md`, `docs/product-docs/domain/chat-runtime.md`, `docs/epics/chat-first-runtime/SRS/feature.md`, `docs/epics/chat-first-runtime/SRS/contracts.md`
Trace IDs: DOC-SDK-001, SRS-FR-001..SRS-FR-014, SRS-FR-016, SRS-FR-018, SRS-FR-019
Docs sync note: this file describes the implemented working-copy SDK surface and must stay aligned with code until a later release/current-state gate exists.

## Source Of Truth
This product-doc owns the current implemented behavior of the public Go SDK in this working copy. It is not a release/current-state claim, not full-QA evidence, and does not invent Codex capabilities beyond what the gateway currently exposes.

## Purpose
Let application code use a small chat-oriented API while the gateway absorbs the unstable `codex.exe app-server` JSONL protocol through the implemented chat-first gRPC contract.

## Public SDK Surface
- `New(conn, opts...)`: creates a client over an existing gRPC connection.
- `NewWithClient(rpc, opts...)`: creates a client around an existing `pb.ChatRuntimeServiceClient`.
- `WithBearerToken`, `WithSessionGroupID`, and `WithWorkspaceID`: required client options.
- `SetDefaultClient`: sets the process-global default client used by the package-level helpers.
- `codex.Run(ctx, prompt)`: starts a new chat by sending the first prompt and returns a chat handle plus event stream access.
- `codex.GetChat(ctx, chatID)`: returns a handle for a Codex thread identified by `chat_id`.
- `chat.GetHistory(ctx)`: returns Codex-owned turn summary fields/projection where installed Codex supports it.
- `chat.Run(ctx, prompt)`: sends a new prompt into an existing chat.
- `chat.GetEventsStream(ctx)`: streams normalized current live events for active chat activity; replay returns typed unsupported/unavailable/narrowed behavior unless it can be proven in the current process.
- `chat.GetStatus(ctx)`: returns the current typed chat status.
- `chat.RespondApproval`, `chat.RespondPermissions`, `chat.RespondMcpElicitation`, and `chat.RespondToolUserInput`: send typed pending responses without exposing internal JSONL.
- `chat.Interrupt(ctx)` and `chat.InterruptRun(ctx, runID)`: request interrupt for the active or explicitly identified run.
- `FromStart`, `AfterEventID`, `AfterEventCursor`, and `WithClientSubscriberID`: configure event-stream attachment.
- `WithHistoryDepth`, `WithHistoryCursor`, `WithHistoryLimit`, and `WithHistorySortDirection`: configure history reads.
- `WithClientMessageID`, `WithClientResponseID`, `WithClientRequestID`, `WithIdempotencyKey`, `WithContextBlocks`, `WithUICorrelationMetadata`, and `WithInitialStreamOptions`: configure side-effecting requests and initial stream behavior.

## Chat Identity Rules
- `chat_id` is the stable application-level identifier stored by the application.
- In v1, `chat_id` equals Codex `Thread.id`.
- `run_id`, task compatibility ID, Codex turn ID, cursor, pending request ID, and idempotency key are distinct identities and must not be substituted for `chat_id`.
- A chat is created only by sending a prompt. Empty chat creation is out of scope.
- SDK-created chats are not promised to appear as the current visible Desktop UI thread.
- Package-level `Run` and `GetChat` require `SetDefaultClient` to be configured first; otherwise they return `ErrDefaultClientNotConfigured`.

## History And Stream Rules
- History is Codex-owned and bounded by installed Codex support; it must not imply access to arbitrary Codex Desktop history or item-level history.
- Streaming events should preserve order within a chat/run stream as far as Codex/gateway observation supports it and expose replay notices or errors when the requested cursor is unsupported, out of range, unavailable after restart, or narrowed to live-only.
- Replay is in-memory only in v1 and is not durable across gateway restart.
- Unknown internal Codex events may be surfaced only through safe, bounded, redacted gateway warning/unknown-event shapes.
- `EventStream.Close()` cancels the stream subscription only; it is not an interrupt request.

## Pending / Status / Interrupt
- Pending requests must preserve safe user-visible display information and decision options where Codex supports them.
- Pending responses must be scoped to `chat_id`, active `run_id`, and `pending_request_id`.
- Status must distinguish invalid/unknown/not-found/unavailable thread outcomes, Codex thread lifecycle, and current/last run lifecycle where applicable.
- Interrupt must target the active Codex turn/run and must report no-active, mismatched, already-terminal, already-interrupting, unavailable, and not-found cases.
- SDK/gRPC stream cancellation is not interrupt.

## Errors
- Errors must be typed enough for SDK callers to distinguish invalid prompt, missing/duplicate/malformed/wrong bearer auth, invalid session/workspace, permission denied, not found chat/thread, unsupported capability, unavailable gateway/Codex/session group, unknown state, interrupted, failed, cursor out-of-range/unavailable/narrowed, active-run conflict, pending request mismatch, and current-process idempotency conflict.
- Error messages must not include raw secrets, auth headers, tokens, private keys, or unredacted private data.
- `AsError(err)` converts returned errors into the SDK `Error` type when typed gateway details are available.

## Retry / Idempotency
- Start, run, pending response, and interrupt operations must be idempotency-aware in the current process.
- Idempotency data is process-local in v1 and may be lost after gateway restart.
- Prompt text, prompt/content fingerprints, prompt/response digests, raw request payloads, and raw responses are not retained for idempotency.
- If a retry conflicts with prior safe scope, callers receive typed conflict. If prior side-effect delivery is unknown, callers receive typed unknown/unavailable and the gateway must not duplicate the Codex call.
- When callers omit side-effect IDs, the SDK generates safe public client IDs and idempotency keys for start, run, pending-response, and interrupt helpers.

## Example
```go
chat, events, err := codex.Run(ctx, "Summarize this workspace")
_ = chat.ID // chat.ID is Codex Thread.id under chat-first naming.
_ = events
_ = err
```

## Completion Criteria
- Methods, identity rules, history/stream rules, pending/status/interrupt, errors, and compatibility are documented with sanitized examples.

## Forbidden Content
- Raw secrets, tokens, auth headers, cookies, passwords, private keys, or customer PII dumps.
- Release delta or implementation-only notes.
