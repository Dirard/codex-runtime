# Troubleshooting

Сначала проверьте три вещи:

1. Gateway запущен и пишет `gateway listening on ...`.
2. SDK использует тот же gateway-token, что указан в gateway config.
3. `session_group_id` и `workspace_id` совпадают в config и SDK.

## `--config is required`

Gateway запускается только с trusted config:

```powershell
go run .\gateway\cmd\codex-runtime-gateway --config .\.local\gateway.workflow.toml
```

## `client_auth_token_source file must be absolute`

В config для token file нужен абсолютный путь.

В PowerShell удобно собрать его так:

```powershell
$tokenSource = (Join-Path (Get-Location) ".local\gateway.token").Replace("\", "/")
```

## Invalid Token Или Unauthenticated

Проверьте:

- gateway читает тот token file, который вы ожидаете;
- SDK читает тот же file;
- token не содержит пробелов в начале/конце;
- token не выводится в logs.

## Gateway Не Стартует Из-За `listen`

Gateway должен слушать только loopback:

- `127.0.0.1:5575`;
- `localhost:5575`;
- `[::1]:5575`.

Публичные адреса не принимаются.

## `chat_runtime.enabled=false`

Chat/workflow SDK calls недоступны, если chat runtime выключен.

Уберите этот override или включите:

```toml
[chat_runtime]
enabled = true
```

## Invalid Workflow Package

Частые причины:

- нет `config.toml` в корне workflow;
- путь внутри package небезопасен;
- есть duplicate/case-conflicting paths;
- файл слишком большой;
- весь package больше 10 MiB;
- найден secret-like literal;
- fingerprint не совпал с содержимым.

Исправьте workflow и повторите `InitWorkflow`.

## Empty Prompt

`workflow.Run` и `chat.Run` требуют непустой prompt.

## `RestartRequired`

Gateway принял новую версию workflow, но для активного runtime нужен restart.

```go
status, err := workflow.Restart(ctx)
```

После успешного restart можно запускать новые turns.

## `ReplayUnavailable`

Event replay process-local и ограничен TTL/размером. После restart gateway или
при старом cursor replay может быть недоступен.

Практичный вариант: запросите актуальный status/history и продолжайте чат
новым turn.

## MCP Unavailable Или MCP Not Reachable

Проверьте:

- workflow действительно должен объявлять MCP;
- gateway policy разрешает MCP reload;
- команда/endpoint MCP доступен с машины gateway;
- workflow не содержит raw secrets, а использует env/file references.

## Browser Получает Gateway Token

Это архитектурная ошибка. Browser должен общаться только с вашим backend.

Правильная схема:

```text
browser -> backend -> SDK -> gateway
```
