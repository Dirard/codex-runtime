# Быстрый Старт

Цель: за несколько минут поднять gateway, скопировать workflow и проверить
первый запрос через готовый smoke-пример.

## 1. Проверьте Требования

Нужно:

- Go.
- Локальный Codex binary.
- Рабочий `CODEX_HOME`, где Codex уже авторизован.
- Отдельный gateway-token для связи backend -> gateway.

На Windows обычный `CODEX_HOME`:

```text
C:\Users\<user>\.codex
```

Внутри лежит Codex auth. Его не нужно копировать в workflow.

Быстрая проверка Codex binary:

```powershell
codex --version
```

Если Codex не в `PATH`, запомните абсолютный путь к `codex.exe` и используйте
его в `codex_binary`.

Быстрая проверка `CODEX_HOME`:

```powershell
Test-Path "$env:USERPROFILE\.codex\auth.json"
```

Если команда вернула `False`, сначала авторизуйте Codex обычным способом на той
машине, где будет работать gateway.

## 2. Создайте Gateway Token

Gateway-token - это локальный секрет между вашим backend и gateway.

```powershell
New-Item -ItemType Directory -Force .\.local | Out-Null

$bytes = New-Object byte[] 32
[System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
[Convert]::ToBase64String($bytes).TrimEnd("=") |
  Set-Content -Encoding UTF8 .\.local\gateway.token
```

Не печатайте настоящий token в README, logs, browser response или issue.

## 3. Скопируйте Starter Workflow

```powershell
go run .\examples\workflow-scaffold `
  -source .\examples\workflows\writer-notes `
  -target .\.local\workflows\writer-notes
```

Команда копирует только workflow-файлы. Она не читает и не меняет глобальный
`CODEX_HOME`.

## 4. Создайте Gateway Config

Создайте `.local\gateway.workflow.toml`.

```powershell
$codexBinary = "<absolute-path-to-codex.exe>"
$codexHome = "<absolute-path-to-codex-home>"
$workspace = (Get-Location).Path.Replace("\", "/")
$workflowState = (Join-Path (Get-Location) ".workflow-state").Replace("\", "/")
$tokenSource = (Join-Path (Get-Location) ".local\gateway.token").Replace("\", "/")

@"
codex_binary = "$codexBinary"
listen = "127.0.0.1:5575"
workflow_storage_dir = "$workflowState"

[client_auth_token_source]
file = "$tokenSource"

[[session_groups]]
session_group_id = "workflow-smoke-session"
workspace_id = "workflow-smoke-workspace"
cwd = "$workspace"
codex_home = "$codexHome"

[session_groups.runtime_policy]
approval_policy = "on-request"
approvals_reviewer = "user"
sandbox_mode = "workspace-write"
"@ | Set-Content -Encoding UTF8 .\.local\gateway.workflow.toml
```

Важные поля:

- `codex_binary` - путь к Codex executable или имя команды из `PATH`.
- `workflow_storage_dir` - persistent-каталог runtime-состояния.
- `client_auth_token_source.file` - абсолютный путь к gateway-token.
- `session_group_id` и `workspace_id` - значения, с которыми потом подключается SDK.
- `codex_home` - папка с Codex auth.

## 5. Запустите Gateway

В первом терминале:

```powershell
go run .\gateway\cmd\codex-runtime-gateway --config .\.local\gateway.workflow.toml
```

Ожидаемый вывод:

```text
gateway listening on 127.0.0.1:5575
```

## 6. Запустите Smoke

Во втором терминале:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass `
  -File .\examples\workflow-smoke\run-local.ps1 `
  -TokenSource .\.local\gateway.token `
  -WorkflowDir .\.local\workflows\writer-notes
```

Smoke проверяет:

- gateway доступен;
- workflow инициализируется;
- первый ответ stream-ится;
- возвращается `chat_id`;
- тот же chat можно продолжить.

## Что Дальше

- Для backend-кода: [`backend-integration.ru.md`](backend-integration.ru.md).
- Для typed stream/action loop: [`go-sdk-friendly.ru.md`](go-sdk-friendly.ru.md).
- Для workflow-файлов: [`workflow-package.ru.md`](workflow-package.ru.md).
- Если что-то не запустилось: [`troubleshooting.ru.md`](troubleshooting.ru.md).
