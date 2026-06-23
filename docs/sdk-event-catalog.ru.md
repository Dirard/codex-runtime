# Каталог событий Go SDK

Этот каталог фиксирует границу friendly Go SDK после фаз 2-4. Он source-backed от текущих локальных `proto/codex_control/v1/codex_control.proto`, `gateway/internal/domain/events.go` и upstream `D:\ai-apps\codex\codex-rs\app-server-protocol\src\protocol\common.rs`.

## Preflight Matrix

| SDK event | Minimal source | Chat state/domain | Existing proto/gRPC | SDK status |
| --- | --- | --- | --- | --- |
| `CommandStarted` | `item/started` only when `item.type == commandExecution`, sourced by upstream `ExecCommandBegin -> ThreadItem::CommandExecution` | `domain.ChatEvent.CommandStarted`, `chatstate.EventRecord.CommandStarted` | `ChatEvent.command_started` with `item_id`, `command_display`, `workspace_label` | Exported as `CommandStarted`, seeds `CommandRef{ID, Display, WorkspaceLabel, Known:true}` |
| `CommandOutput` | `item/commandExecution/outputDelta` with `threadId`, `turnId`, `itemId`, `delta` | `domain.ChatEvent.CommandOutputDelta`, `chatstate.EventRecord.CommandOutputDelta` | `ChatEvent.command_output_delta` with `item_id`, `stream`, `delta`, `truncated` | Exported as `CommandOutput`; normal flow resolves command context from prior start, orphan replay is explicit |
| `Warning` | run-owned warnings only: currently `model/rerouted`, `model/verification`, and warning/config/guardian variants only if `threadId` and `turnId` match an active run | `domain.ChatEvent.GatewayWarning`, `chatstate.EventRecord.GatewayWarning` | `ChatEvent.gateway_warning` with `code`, `message`, `request_type`, `auto_resolution`, `limit_reason` | Exported as `Warning`; generic non-run-owned warnings stay catalog-only |

`TerminalInteraction` is excluded: upstream payload is terminal stdin, not command output.

## Local ChatEvent Payloads

| Local payload | Bucket | Reason | Next decision |
| --- | --- | --- | --- |
| `lifecycle` | Catalog-only local gap | Local chat stream does not expose a stable friendly lifecycle payload in this plan | Future event contract if needed |
| `assistant_delta` | Phase 2 current P0 | Friendly `AssistantTextDelta` exists and is tested | Keep stable |
| `assistant_message_completed` | Phase 2 current P0 | Friendly `AssistantMessageCompleted` exists and is tested | Keep stable |
| `plan_updated` | Catalog-only target gap | Needs public plan shape, docs and replay semantics | Future plan event contract |
| `tool_progress` | Bounded `UnknownEvent` plus catalog row | Local proto has `ToolProgressEvent{item_id, tool_name, state, summary}`, but P0 has no stable progress helper/redaction semantics | Future progress event contract |
| `command_started` | Phase 4 runtime-backed | Source-backed bridge/storage/replay proof added for command-execution item start | Keep stable |
| `command_output_delta` | Phase 4 runtime-backed | Source-backed bridge/storage/replay proof added for command output delta | Keep stable |
| `file_diff_updated` | Catalog-only target gap | Needs stable file/diff payload and truncation rules | Future diff event contract |
| `turn_diff_updated` | Catalog-only target gap | Needs stable turn diff payload and replay semantics | Future diff event contract |
| `pending_request_created` | Phase 2 current P0 | Friendly pending action objects exist for approval, permissions, structured input and user input | Keep stable |
| `pending_request_resolved` | Phase 2 current P0 | Friendly `ActionResolved` exists and is tested | Keep stable |
| `terminal` | Phase 2 current P0 | Friendly terminal result/errors exist and are tested | Keep stable |
| `gateway_warning` | Phase 4 runtime-backed only when run-owned | Generic warnings without run ownership are not synthesized | Keep run-owned only |
| `status_updated` | Phase 2 current P0 | Friendly status snapshots exist and are tested | Keep stable |

## Upstream EventMsg Sweep

| Source members | Bucket | Reason | Next decision |
| --- | --- | --- | --- |
| `ExecCommandBegin` | Phase 4 runtime-backed through command-execution-shaped `ItemStarted` | Carries command id/display/cwd through app-server item start | Keep as only `CommandStarted` source |
| `ExecCommandOutputDelta` | Phase 4 runtime-backed through `CommandExecutionOutputDelta` | Carries `item_id` and `delta`; known command context comes from prior command start | Keep with orphan replay semantics |
| `Warning`, `GuardianWarning`, config/model warning forms | Phase 4 only when run-owned | Message is useful only when chat/run ownership is source-backed | Keep generic warnings catalog-only |
| `ExecCommandEnd` | Catalog-only command completion gap | Needs public completion/status payload before export | Future command completion contract |
| `Error`, `TurnStarted`, `ThreadSettingsApplied`, `TurnComplete`, `TurnAborted`, `ShutdownComplete`, `SessionConfigured`, `ContextCompacted`, `ThreadRolledBack`, `ThreadGoalUpdated`, `TokenCount`, `AgentMessage`, `UserMessage`, `AgentReasoning`, `AgentReasoningRawContent`, `AgentReasoningSectionBreak`, `AgentMessageContentDelta`, `ReasoningContentDelta`, `ReasoningRawContentDelta` | Catalog-only lifecycle/text/reasoning gaps | Current Go runtime already has P0 status/text/terminal; upstream variants need separate bridge decisions | Future source-backed event contracts |
| `ExecApprovalRequest`, `RequestPermissions`, `RequestUserInput`, `DynamicToolCallRequest`, `DynamicToolCallResponse`, `ElicitationRequest`, `ApplyPatchApprovalRequest`, `GuardianAssessment` | Catalog-only pending/control source gaps | P0 pending actions use current Go proto pending surface | Future request bridge/correlation contract |
| `PlanUpdate`, `PlanDelta`, `TurnDiff`, `ViewImageToolCall`, `EnteredReviewMode`, `ExitedReviewMode` | Catalog-only plan/diff/review/media gaps | Useful, but no stable Go payload/replay/docs contract yet | Future plan/diff/review contract |
| `McpStartupUpdate`, `McpStartupComplete`, `McpToolCallBegin`, `McpToolCallEnd`, `WebSearchBegin`, `WebSearchEnd`, `ImageGenerationBegin`, `ImageGenerationEnd`, `PatchApplyBegin`, `PatchApplyUpdated`, `PatchApplyEnd`, generic `ItemStarted`, generic `ItemCompleted` | Catalog-only item/tool/patch/MCP/web/image gaps | Need correlation, payload and redaction rules | Future progress/lifecycle contract |
| `ModelReroute`, `ModelVerification`, `TurnModerationMetadata`, `SafetyBuffering`, `StreamError`, `DeprecationNotice`, `RawResponseItem` | Catalog-only model/safety/raw stream gaps | May expose provider-shaped or sensitive presentation payloads | Separate redaction/product decision |
| `RealtimeConversationStarted`, `RealtimeConversationRealtime`, `RealtimeConversationClosed`, `RealtimeConversationSdp`, `RealtimeConversationListVoicesResponse` | Non-event realtime control gap | Needs separate realtime transport/session SDK | Future realtime SDK, not chat-event widening |

The upstream checklist is the current 77/77 named `EventMsg` seed from `SDK_DESIGN.md`; each named member above is intentionally bucketed once.

## Server Request Definitions

| Member | Wire name | Bucket | Reason / next decision |
| --- | --- | --- | --- |
| `CommandExecutionRequestApproval` | `item/commandExecution/requestApproval` | P0 equivalent through pending actions | Current Go proto pending approval covers friendly response flow |
| `FileChangeRequestApproval` | `item/fileChange/requestApproval` | P0 equivalent through pending actions | Current Go proto pending approval covers friendly response flow |
| `ToolRequestUserInput` | `item/tool/requestUserInput` | P0 equivalent through pending actions | Current Go proto user input covers friendly response flow |
| `McpServerElicitationRequest` | `mcpServer/elicitation/request` | P0 equivalent through pending actions | Current Go proto structured input covers friendly response flow |
| `PermissionsRequestApproval` | `item/permissions/requestApproval` | P0 equivalent through pending actions | Current Go proto permissions approval covers friendly response flow |
| `DynamicToolCall` | `item/tool/call` | Catalog-only dynamic-tool gap | Needs response routing/security design |
| `ThreadBackgroundTerminalsClean` | `thread/backgroundTerminals/clean` | Out-of-current-SDK app-server control | Background terminals are app-server process controls, not chat stream events |
| `ThreadBackgroundTerminalsList` | `thread/backgroundTerminals/list` | Out-of-current-SDK app-server control | Requires separate terminal/process SDK contract |
| `ThreadBackgroundTerminalsTerminate` | `thread/backgroundTerminals/terminate` | Out-of-current-SDK app-server control | Destructive process control needs explicit future API and policy |
| `ThreadRollback` | `thread/rollback` | Out-of-current-SDK app-server control | Thread history mutation/rollback is not a friendly run event |
| `RemoteControlEnable` | `remoteControl/enable` | Out-of-current-SDK app-server control | Remote-control enablement is high-risk global control, not beginner chat SDK |
| `RemoteControlDisable` | `remoteControl/disable` | Out-of-current-SDK app-server control | Needs dedicated remote-control auth/lifecycle contract |
| `RemoteControlStatusRead` | `remoteControl/status/read` | Out-of-current-SDK app-server control | Read API belongs to remote-control SDK surface |
| `RemoteControlPairingStart` | `remoteControl/pairing/start` | Out-of-current-SDK app-server control | Pairing flow needs explicit UX/security contract |
| `RemoteControlPairingStatus` | `remoteControl/pairing/status` | Out-of-current-SDK app-server control | Pairing state is not a chat run event |
| `RemoteControlClientsList` | `remoteControl/client/list` | Out-of-current-SDK app-server control | Client inventory belongs to remote-control management |
| `RemoteControlClientsRevoke` | `remoteControl/client/revoke` | Out-of-current-SDK app-server control | Revocation is a future management API, not P0 friendly chat |
| `CollaborationModeList` | `collaborationMode/list` | Out-of-current-SDK app-server control | Collaboration presets are cataloged explicitly; no P0 chat API promise |
| `ChatgptAuthTokensRefresh` | `account/chatgptAuthTokens/refresh` | Out-of-current-SDK client-service request | Auth token handling stays outside friendly chat SDK |
| `AttestationGenerate` | `attestation/generate` | Out-of-current-SDK client-service request | Needs separate service contract |
| `CurrentTimeRead` | `currentTime/read` | Out-of-current-SDK client-service request | Needs separate client-service contract |
| `ApplyPatchApproval` | legacy request | Raw/legacy compatibility | Do not promote as beginner API |
| `ExecCommandApproval` | legacy request | Raw/legacy compatibility | Do not promote as beginner API |

## Server Notifications

| Member | Wire name | Bucket/status | Reason / next decision |
| --- | --- | --- | --- |
| `Error` | `error` | Catalog-only app-server error gap | Needs app-server error contract before SDK exposure |
| `ThreadStarted` | `thread/started` | Out-of-current-SDK thread lifecycle surface | Future app-server SDK |
| `ThreadStatusChanged` | `thread/status/changed` | Out-of-current-SDK thread/status surface | Do not conflate with P0 chat status without bridge proof |
| `ThreadArchived` | `thread/archived` | Out-of-current-SDK thread lifecycle surface | Future app-server SDK |
| `ThreadDeleted` | `thread/deleted` | Out-of-current-SDK thread lifecycle surface | Future app-server SDK |
| `ThreadUnarchived` | `thread/unarchived` | Out-of-current-SDK thread lifecycle surface | Future app-server SDK |
| `ThreadClosed` | `thread/closed` | Out-of-current-SDK thread lifecycle surface | Future app-server SDK |
| `SkillsChanged` | `skills/changed` | Out-of-current-SDK skills/config surface | Future config SDK |
| `ThreadNameUpdated` | `thread/name/updated` | Out-of-current-SDK thread metadata surface | Future app-server SDK |
| `ThreadGoalUpdated` | `thread/goal/updated` | Out-of-current-SDK thread goal surface | Future app-server SDK |
| `ThreadGoalCleared` | `thread/goal/cleared` | Out-of-current-SDK thread goal surface | Future app-server SDK |
| `ThreadSettingsUpdated` | `thread/settings/updated` | Out-of-current-SDK thread settings surface | Future app-server SDK |
| `ThreadTokenUsageUpdated` | `thread/tokenUsage/updated` | Out-of-current-SDK usage/accounting surface | Future app-server SDK |
| `TurnStarted` | `turn/started` | Catalog-only turn lifecycle gap | P0 uses current status/terminal contracts |
| `HookStarted` | `hook/started` | Non-event hook control/audit surface | Future hook SDK if needed |
| `TurnCompleted` | `turn/completed` | Catalog-only turn lifecycle gap | P0 maps run-owned terminal through current chat terminal |
| `HookCompleted` | `hook/completed` | Non-event hook control/audit surface | Future hook SDK if needed |
| `TurnDiffUpdated` | `turn/diff/updated` | Catalog-only diff lifecycle gap | Future diff contract |
| `TurnPlanUpdated` | `turn/plan/updated` | Catalog-only plan lifecycle gap | Future plan contract |
| `ItemStarted` | `item/started` | Catalog-only item lifecycle gap, with one Phase 4 exception | Only command-execution-shaped item starts become `CommandStarted` |
| `ItemGuardianApprovalReviewStarted` | `item/autoApprovalReview/started` | Catalog-only guardian review lifecycle gap | Future review contract |
| `ItemGuardianApprovalReviewCompleted` | `item/autoApprovalReview/completed` | Catalog-only guardian review lifecycle gap | Future review contract |
| `ItemCompleted` | `item/completed` | Catalog-only item lifecycle gap | Future item contract |
| `RawResponseItemCompleted` | `rawResponseItem/completed` | Internal/raw response gap | Keep out of friendly API |
| `AgentMessageDelta` | `item/agentMessage/delta` | P0 equivalent through current assistant delta | Current bridge maps to friendly assistant text |
| `PlanDelta` | `item/plan/delta` | Catalog-only plan delta gap | Future plan contract |
| `CommandExecOutputDelta` | `command/exec/outputDelta` | Out-of-current-SDK standalone command surface | Not chat/run-owned command output |
| `ProcessOutputDelta` | `process/outputDelta` | Out-of-current-SDK standalone process surface | Not chat/run-owned command output |
| `ProcessExited` | `process/exited` | Out-of-current-SDK standalone process surface | Future process SDK |
| `CommandExecutionOutputDelta` | `item/commandExecution/outputDelta` | Phase 4 runtime-backed | Maps to `CommandOutput` only with chat/run active run correlation |
| `TerminalInteraction` | `item/commandExecution/terminalInteraction` | Catalog-only redacted terminal-input gap | Never `CommandOutput` |
| `FileChangeOutputDelta` | `item/fileChange/outputDelta` | Catalog-only file-change output gap | Future file-change contract |
| `FileChangePatchUpdated` | `item/fileChange/patchUpdated` | Catalog-only patch lifecycle gap | Future patch contract |
| `ServerRequestResolved` | `serverRequest/resolved` | Catalog-only request-resolution gap | Needs correlation/replay semantics |
| `McpToolCallProgress` | `item/mcpToolCall/progress` | Catalog-only MCP progress gap | Future progress contract |
| `McpServerOauthLoginCompleted` | `mcpServer/oauthLogin/completed` | Out-of-current-SDK MCP/account surface | Future MCP/account SDK |
| `McpServerStatusUpdated` | `mcpServer/startupStatus/updated` | Out-of-current-SDK MCP status surface | Future MCP SDK |
| `AccountUpdated` | `account/updated` | Out-of-current-SDK account surface | Future account SDK |
| `AccountRateLimitsUpdated` | `account/rateLimits/updated` | Out-of-current-SDK rate-limit surface | Future account SDK |
| `AppListUpdated` | `app/list/updated` | Out-of-current-SDK app marketplace surface | Future app SDK |
| `RemoteControlStatusChanged` | `remoteControl/status/changed` | Non-event/high-risk remote-control surface | Future remote-control SDK |
| `ExternalAgentConfigImportProgress` | `externalAgentConfig/import/progress` | Out-of-current-SDK external-agent config surface | Future config SDK |
| `ExternalAgentConfigImportCompleted` | `externalAgentConfig/import/completed` | Out-of-current-SDK external-agent config surface | Future config SDK |
| `FsChanged` | `fs/changed` | Out-of-current-SDK filesystem/watch surface | Future filesystem SDK |
| `ReasoningSummaryTextDelta` | `item/reasoning/summaryTextDelta` | Catalog-only reasoning presentation gap | Future reasoning contract |
| `ReasoningSummaryPartAdded` | `item/reasoning/summaryPartAdded` | Catalog-only reasoning presentation gap | Future reasoning contract |
| `ReasoningTextDelta` | `item/reasoning/textDelta` | Catalog-only reasoning presentation gap | Future reasoning contract |
| `ContextCompacted` | `thread/compacted` | Out-of-current-SDK thread compact/control surface | Future app-server SDK |
| `ModelRerouted` | `model/rerouted` | Phase 4 only when run-owned, otherwise catalog-only | Current chat bridge exports active-run instance as `Warning` |
| `ModelVerification` | `model/verification` | Phase 4 only when run-owned, otherwise catalog-only | Current chat bridge exports active-run instance as `Warning` |
| `TurnModerationMetadata` | `turn/moderationMetadata` | Catalog-only safety/moderation gap | Future safety contract |
| `ModelSafetyBufferingUpdated` | `model/safetyBuffering/updated` | Catalog-only safety buffering gap | Future safety contract |
| `Warning` | `warning` | Phase 4 only when run-owned, otherwise catalog-only | No synthetic run metadata |
| `GuardianWarning` | `guardianWarning` | Phase 4 only when run-owned, otherwise catalog-only | No synthetic run metadata |
| `DeprecationNotice` | `deprecationNotice` | Catalog-only deprecation notice gap | Future app-server SDK |
| `ConfigWarning` | `configWarning` | Out-of-current-SDK config warning surface unless run-owned | Future config SDK |
| `FuzzyFileSearchSessionUpdated` | `fuzzyFileSearch/sessionUpdated` | Out-of-current-SDK fuzzy-search surface | Future search SDK |
| `FuzzyFileSearchSessionCompleted` | `fuzzyFileSearch/sessionCompleted` | Out-of-current-SDK fuzzy-search surface | Future search SDK |
| `ThreadRealtimeStarted` | `thread/realtime/started` | Out-of-current-SDK realtime transport/session surface | Future realtime SDK |
| `ThreadRealtimeItemAdded` | `thread/realtime/itemAdded` | Out-of-current-SDK realtime transport/session surface | Future realtime SDK |
| `ThreadRealtimeTranscriptDelta` | `thread/realtime/transcript/delta` | Out-of-current-SDK realtime transport/session surface | Future realtime SDK |
| `ThreadRealtimeTranscriptDone` | `thread/realtime/transcript/done` | Out-of-current-SDK realtime transport/session surface | Future realtime SDK |
| `ThreadRealtimeOutputAudioDelta` | `thread/realtime/outputAudio/delta` | Out-of-current-SDK realtime transport/session surface | Future realtime SDK |
| `ThreadRealtimeSdp` | `thread/realtime/sdp` | Out-of-current-SDK realtime transport/session surface | Future realtime SDK |
| `ThreadRealtimeError` | `thread/realtime/error` | Out-of-current-SDK realtime transport/session surface | Future realtime SDK |
| `ThreadRealtimeClosed` | `thread/realtime/closed` | Out-of-current-SDK realtime transport/session surface | Future realtime SDK |
| `WindowsWorldWritableWarning` | `windows/worldWritableWarning` | Out-of-current-SDK Windows environment warning surface | Future environment SDK |
| `WindowsSandboxSetupCompleted` | `windowsSandbox/setupCompleted` | Out-of-current-SDK Windows sandbox setup surface | Future environment SDK |
| `AccountLoginCompleted` | `account/login/completed` | Out-of-current-SDK account auth surface | Future account SDK |

## Baseline Verdict

`CommandStarted`, `CommandOutput` and run-owned `Warning` are implemented as friendly SDK events. Generic `ItemStarted`, standalone command/process streams, terminal stdin, plan/diff/tool/patch/realtime/control/account/config surfaces remain catalog-only or out-of-current-SDK. Background terminals, rollback, remote control and collaboration-mode methods are named as app-server controls, not chat events. Beginner examples must use command context helpers, not caller-owned raw `item_id` maps.
