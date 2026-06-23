# Gateway Config

Gateway config говорит gateway, где лежит Codex, где хранить workflow-состояние,
как слушать локальный gRPC-адрес и каким token проверять backend.

## Минимальный Config

```toml
codex_binary = "C:/path/to/codex.exe"
listen = "127.0.0.1:5575"
workflow_storage_dir = "D:/my-app/.workflow-state"

[client_auth_token_source]
file = "D:/my-app/.local/gateway.token"

[[session_groups]]
session_group_id = "app"
workspace_id = "prod"
cwd = "D:/my-app"
codex_home = "C:/Users/me/.codex"

[session_groups.runtime_policy]
approval_policy = "on-request"
approvals_reviewer = "user"
sandbox_mode = "workspace-write"
```

## Root Поля

- `codex_binary` - обязательное поле. Абсолютный путь к Codex executable или имя
  команды из `PATH`.
- `listen` - loopback address. Разрешены `localhost`, `127.0.0.1`, `[::1]`.
- `workflow_storage_dir` - обязательный persistent-каталог для runtime-состояния.
- `workflow_package_max_bytes` - optional, по умолчанию 10 MiB.
- `workflow_grpc_message_bytes` - optional, должен покрывать package limit.
- `child_env_allowlist` - optional список non-secret env vars, которые можно
  передать child Codex process.

## Почему TOML Строгий

Gateway config - trusted security-файл. Parser намеренно поддерживает узкий
набор TOML: table headers, array-table headers, `key = value`, double-quoted
strings, integers, booleans и массивы строк.

Другие TOML-возможности отклоняются, чтобы конфигурация не зависела от
неоднозначного parsing.

## Gateway Token

Нужно выбрать ровно один источник:

```toml
[client_auth_token_source]
file = "D:/my-app/.local/gateway.token"
```

или:

```toml
[client_auth_token_source]
env = "CODEX_RUNTIME_GATEWAY_TOKEN"
```

Для `file` нужен абсолютный путь. Значение token должно быть непустым,
без пробелов по краям и из bearer-token символов.

## Session Groups

Session group связывает SDK-вызовы с workspace и `CODEX_HOME`.

```toml
[[session_groups]]
session_group_id = "app"
workspace_id = "prod"
cwd = "D:/my-app"
codex_home = "C:/Users/me/.codex"
```

- `session_group_id` - то же значение передается в `codex.WithSessionGroupID`.
- `workspace_id` - то же значение передается в `codex.WithWorkspaceID`.
- `cwd` - workspace, где работает Codex.
- `codex_home` - папка с Codex auth.

Пути `cwd` и `codex_home` должны быть абсолютными и существовать.

## Runtime Policy

```toml
[session_groups.runtime_policy]
approval_policy = "on-request"
approvals_reviewer = "user"
sandbox_mode = "workspace-write"
```

Поддерживаемые `approval_policy`:

- `untrusted`;
- `on-failure`;
- `on-request`.

`approval_policy = "never"` сейчас не поддерживается.

Для sandbox нужно выбрать ровно одно:

- `sandbox_mode`;
- `permissions_profile_id`.

Поддерживаемые `sandbox_mode`:

- `read-only`;
- `workspace-write`;
- `danger-full-access`.

Для `danger-full-access` обязательно укажите `danger_full_access_reason`.

## Optional Limits

Можно явно задать replay, pending и gRPC limits:

```toml
[session_groups.replay_limits]
max_events = 2000
max_bytes = 8388608
ttl_millis = 1800000

[session_groups.pending_limits]
max_active_requests = 32
max_display_payload_bytes = 32768
status_non_pending_budget_bytes = 65536

[session_groups.grpc_limits]
inbound_message_bytes = 4194304
outbound_message_bytes = 4194304
```

Если не указать эти блоки, gateway применит безопасные defaults.

## Частые Ошибки Config

- `client_auth_token_source file must be absolute` - укажите абсолютный путь.
- `listen host must be localhost...` - gateway не должен слушать публичный адрес.
- `at least one session group is required` - добавьте `[[session_groups]]`.
- `approval_policy=never is out of MVP` - используйте `on-request`,
  `on-failure` или `untrusted`.
- `exactly one of sandbox_mode or permissions_profile_id is required` - оставьте
  только один способ sandbox.
