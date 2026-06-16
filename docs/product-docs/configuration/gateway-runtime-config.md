# Configuration: Gateway Runtime

Type: product behavior / configuration contract
Status: current implemented behavior for the working-copy baseline; full QA and release/current-state pending
Owner: system_analyst
Residual release/current-state gate owners: release_ops_owner, security_privacy_data_owner
Consumer / intended use: gateway developers, local operators, reviewers, and QA.
Last repaired: 2026-06-16
Related docs: `docs/product-docs/security/local-runtime-boundary.md`, `docs/epics/chat-first-runtime/SRS/contracts.md`, `docs/epics/chat-first-runtime/SRS/rollout.md`
Trace IDs: DOC-CONFIG-001, SRS-FR-013, SRS-FR-019, SRS-NFR-002, SRS-NFR-003, SRS-NFR-008

## Source Of Truth
This product-doc owns the current implemented configuration behavior for the local runtime gateway in this working copy. It is not a release/current-state claim, not full-QA evidence, and should track only real config keys, defaults, validation, and runtime effects.

## Keys / Variables
| Name | Purpose | Secret? | Source of truth | Default | Hard cap / validation | Rotation owner |
| --- | --- | --- | --- | --- | --- | --- |
| `codex_binary` | path to installed `codex.exe` | no | gateway config | none | required executable path; `--version` probe timeout 5s | not applicable |
| `listen` | loopback gRPC address | no | gateway config | `127.0.0.1:0` | host must be `localhost`, `127.0.0.1`, or `::1`; port must be numeric and in range | release_ops_owner |
| `strict_schema_verification` | fail fast on runtime schema mismatches instead of tolerating them with diagnostics | no | gateway config | `false` when omitted | boolean only | release_ops_owner |
| `client_auth_token_source` | local client auth token source, exactly one env name or absolute file source | yes, by referenced value | gateway config/env/file reference | none | source is required, readable, syntactically valid, and value must not be logged or documented; external credential-provider command execution is out of v1 | security_privacy_data_owner |
| `child_env_allowlist` | explicit environment names allowed to reach child processes | may reveal safe env names only | gateway config | empty when omitted | names must be syntactically valid and must not be secret-like, the client bearer-token env, or `CODEX_HOME` | security_privacy_data_owner |
| `credential_providers` | optional external credential-provider commands for session groups | may reference secret-bearing env sources | gateway config | none | unique `provider_id`; absolute executable; optional existing workdir; env source names validated; defaults timeout `10s`, stdout `16 KiB`, stderr `8 KiB`; caps `30s`, `64 KiB`, `32 KiB` | security_privacy_data_owner |
| `session_groups` | workspace/session routing with canonical `cwd` and `codex_home` identity | may contain private paths | gateway config | none | at least one is required; session-group IDs must be unique; `cwd` and `codex_home` must exist; canonical `codex_home` identities must not collide | release_ops_owner |
| `session_groups.runtime_policy` | per-session-group approval and sandbox policy for child-runtime execution | no | gateway config | none | `approval_policy` is required and `never` is rejected; `approvals_reviewer` defaults to `user`; exactly one of `sandbox_mode` or `permissions_profile_id` is required | release_ops_owner |
| `session_groups.thread_binding_limits` | task-compatibility thread binding cache cap and TTL preserved alongside chat-first runtime | no | validated config | `1000` bindings, `24h` TTL | caps `10000` bindings and `7d` TTL | release_ops_owner |
| `chat_runtime.enabled` | independent chat-first service enable/disable path | no | gateway config | `true` when omitted | `false` disables/omits `ChatRuntimeService`, reports chat readiness as `NOT_SERVING`, and allows task RPCs to remain healthy when their own dependencies are healthy | release_ops_owner |
| `replay_limits` | in-memory normalized event replay buffer | no | validated config | 2000 events, 8 MiB, 30m | 5000 events, 32 MiB, 2h; process memory only | release_ops_owner |
| `pending_limits` | active pending requests and memory-only display payload | no | validated config | 32 active requests, 32 KiB display payload | 64 active requests, 64 KiB display payload | release_ops_owner |
| gRPC message limits | inbound/outbound message size | no | validated config | 4 MiB | 8 MiB | release_ops_owner |
| status detail budget | non-pending status detail budget | no | implementation constant/config | 64 KiB | 256 KiB | release_ops_owner |
| app-server startup timeout | per-session app-server connect/start wait | no | implementation constant | 15s | caller deadline wins if shorter; not currently configurable through TOML | release_ops_owner |
| supervisor restart backoff | app-server process failure cooldown | no | implementation constant | 3 non-cancel/non-deadline failures then 30s | scoped to session group | release_ops_owner |

## Injection Method
Configuration file plus environment/file references for secret-bearing values. Do not document actual secret values, raw auth headers, or environment dumps. No external process command is an approved bearer-token source in v1.

## Precedence
V1 config precedence is fixed:

1. Built-in defaults.
2. TOML.
3. Documented process flags. The current approved runtime override is `--listen` only; `--config` is the required config locator, not a runtime config override.
4. Runtime secret resolution from the configured source only.

There is no non-secret environment variable override in v1. Unknown or duplicate TOML keys fail validation. Process flag overrides must pass the same validation as TOML-derived values. Secret-bearing values are referenced by source only, not copied into config examples.

## Reload Behavior
Runtime config reload is not promised in v1. Config changes require gateway restart unless a later approved design adds reload semantics.

## Per-Environment Differences
Local Windows development/test fixtures may use temporary paths and sanitized fixture token sources. Production, remote, team-shared, or multi-tenant deployment is out of scope.

## Rollout / Rollback Impact
`chat_runtime.enabled=false` disables or omits `ChatRuntimeService`, reports its health/readiness `NOT_SERVING`, and allows existing task RPC behavior to remain healthy when its own dependencies are healthy. Gateway-local replay/pending/idempotency state is process-local and is lost after restart.

## Drift Detection
Checks should validate strict config parsing, unknown/duplicate TOML rejection, absence of non-secret environment overrides, exact-one token source limited to env name or absolute file path, rejection of external credential-provider command execution for the client bearer token, loopback `--listen` override validation, runtime-policy validation, replay/thread-binding/pending/message limit caps, disabled chat readiness as `NOT_SERVING`, and redacted diagnostics without dumping env values or token contents.

## Forbidden Content
- Raw secret values, credentials, tokens, private keys, passwords, cookies, or auth headers.
- Environment dumps containing private data.
- Release delta or temporary change notes.
