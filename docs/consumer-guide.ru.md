# Простое Руководство Для Потребителя

Этот документ для backend-разработчика, который хочет подключить Codex Runtime к
своему приложению без погружения во внутренности gateway.

Коротко: вы запускаете локальный private gateway, а из backend-кода вызываете Go
SDK. Браузер или мобильное приложение ходит только в ваш backend. Токены Codex и
gateway-токен пользователю не отдаются.

## Что Нужно Понять

В системе всего четыре понятия:

- Gateway - локальное приложение, которое запускает Codex и принимает gRPC
  запросы только от вашего backend.
- Go SDK - библиотека, через которую backend создает workflow, запускает чат и
  читает stream ответа.
- Workflow - обычная папка с `config.toml`, `agents/` и опционально `skills/`.
  Gateway превращает ее в изолированную `.codex`-папку для конкретного workflow.
- `chat_id` - идентификатор чата. Его хранит ваше приложение, чтобы продолжить
  тот же диалог позже.

Обычная схема такая:

```text
browser/mobile -> your backend -> Go SDK -> private gateway -> Codex
```

## Минимальный Сценарий

### 1. Подготовьте Codex Auth

На машине, где работает gateway, должен быть обычный `CODEX_HOME` с
аутентификацией Codex.

По умолчанию это:

```text
C:\Users\<user>\.codex
```

Внутри этого каталога Codex хранит `auth.json`. Его не нужно класть в workflow и
не нужно передавать в SDK. В gateway config вы указываете путь к `CODEX_HOME`,
а gateway сам запускает Codex так, чтобы Codex увидел свою аутентификацию.

### 2. Создайте Gateway Token

Gateway защищен отдельным bearer-token. Это не Codex auth, а простой локальный
секрет между вашим backend и gateway.

Создайте файл:

```powershell
New-Item -ItemType Directory -Force .\.local | Out-Null
Set-Content -Encoding UTF8 .\.local\gateway.token "<put-random-local-token-here>"
```

Не печатайте настоящий token в логи, README, ответы API и browser.

### 3. Создайте Workflow

Самый простой путь - скопировать готовый starter workflow:

```powershell
go run .\examples\workflow-scaffold `
  -source .\examples\workflows\writer-notes `
  -target .\.local\workflows\writer-notes
```

Минимальная структура workflow выглядит так:

```text
my-workflow/
  config.toml
  AGENTS.md
  agents/
    default.toml
  skills/
    my-skill/
      SKILL.md
```

Пример `config.toml`:

```toml
name = "support"
description = "Support assistant workflow."
default_agent = "default"
```

Пример `agents/default.toml`:

```toml
name = "default"
description = "Answers support questions."
developer_instructions = """
You help support operators answer user questions clearly and safely.
"""
```

Все `agents/` и `skills/` из этой папки видны только этому workflow. Другой
workflow получает свою отдельную папку и свой набор агентов/skills.

`AGENTS.md` в корне workflow тоже относится только к этому workflow. Положите в
него общие project instructions: стиль ответов, ограничения, что проверять,
какие команды запускать. Gateway положит этот файл в runtime workspace root, и
Codex прочитает его как обычный `AGENTS.md`.

### 4. Создайте Gateway Config

Пример локального config:

```powershell
$codexBinary = "<absolute-path-to-codex.exe>"
$codexHome = "C:/Users/<user>/.codex"
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
session_group_id = "app"
workspace_id = "prod"
cwd = "$workspace"
codex_home = "$codexHome"

[session_groups.runtime_policy]
approval_policy = "on-request"
approvals_reviewer = "user"
sandbox_mode = "workspace-write"
"@ | Set-Content -Encoding UTF8 .\.local\gateway.workflow.toml
```

Главное:

- `codex_home` указывает на папку, где лежит `auth.json`.
- `workflow_storage_dir` - место, где gateway хранит runtime-состояние workflow и
  чатов.
- `client_auth_token_source.file` - файл с gateway-token.
- `session_group_id` и `workspace_id` должны совпадать с тем, что использует SDK.

### 5. Запустите Gateway

В одном терминале:

```powershell
go run .\gateway\cmd\codex-runtime-gateway --config .\.local\gateway.workflow.toml
```

В другом терминале можно проверить готовым smoke-примером:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass `
  -File .\examples\workflow-smoke\run-local.ps1 `
  -TokenSource .\.local\gateway.token `
  -WorkflowDir .\.local\workflows\writer-notes
```

Если все хорошо, smoke выведет готовность gateway, `chat_id`, первый ответ и
продолжение того же чата.

## Как Выглядит Backend-Код

Установите import path:

```go
import codex "github.com/Dirard/codex-runtime/sdk/go"
```

Минимальная инициализация:

```go
conn, err := grpc.NewClient(
    "127.0.0.1:5575",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
if err != nil {
    return err
}
defer conn.Close()

tokenBytes, err := os.ReadFile(".\\.local\\gateway.token")
if err != nil {
    return err
}

client, err := codex.New(
    conn,
    codex.WithBearerToken(strings.TrimSpace(string(tokenBytes))),
    codex.WithSessionGroupID("app"),
    codex.WithWorkspaceID("prod"),
)
if err != nil {
    return err
}
```

Инициализация workflow:

```go
workflow, err := client.InitWorkflow(ctx, codex.WorkflowDir{
    Namespace: "my-app",
    ID:        "support",
    Path:      ".\\.local\\workflows\\writer-notes",
}, codex.WithWorkflowMCPReload(true))
if err != nil {
    return err
}
```

Первый запрос пользователя:

```go
chat, events, err := workflow.Run(ctx, "Summarize the harbor note.")
if err != nil {
    return err
}
defer events.Close()

saveChatIDForUser(userID, chat.ID)
```

Чтение stream:

```go
for {
    message, err := events.Recv()
    if errors.Is(err, io.EOF) {
        break
    }
    if err != nil {
        return err
    }

    event := message.GetEvent()
    if event == nil {
        continue
    }
    if delta := event.GetAssistantDelta(); delta != nil {
        sendToBrowser(delta.GetTextDelta())
    }
    if event.GetAssistantMessageCompleted() != nil || event.GetTerminal() != nil {
        break
    }
}
```

Продолжение того же чата:

```go
chat, err := workflow.GetChat(ctx, storedChatID)
if err != nil {
    return err
}

result, err := chat.Run(ctx, "Continue from the previous answer.")
if err != nil {
    return err
}

events, err := chat.GetEventsStream(ctx, codex.AfterEventCursor(result.EventCursor))
if err != nil {
    return err
}
defer events.Close()
```

Полный runnable backend-пример лежит в
`examples/api-handler`.

## Что Хранить В Своем Приложении

Gateway не заменяет вашу базу чатов. В приложении обычно хранят:

- `chat_id` - чтобы продолжить диалог.
- `workflow_namespace` и `workflow_id` - чтобы понимать, каким workflow создан
  чат.
- ваш `user_id`, `tenant_id`, project id или другой бизнес-контекст.
- название, дату, статус, последние сообщения, если вам нужен список чатов в UI.

Gateway хранит runtime-состояние под `workflow_storage_dir`, но список чатов для
пользовательского интерфейса лучше вести в вашей базе.

## Как Работает Auth

Есть два разных секрета:

- Codex auth: `auth.json` внутри `CODEX_HOME`. Его использует только Codex.
- Gateway token: локальный bearer-token из `client_auth_token_source.file`. Его
  использует ваш backend, когда вызывает gateway через SDK.

Workflow не должен содержать `auth.json`. Workflow - это изолированный пакет с
`config.toml`, `agents/`, `skills/`, `AGENTS.md` и другими не-secret файлами для
конкретной задачи.

## Как Работает Изоляция Workflow

Каждый workflow передается в gateway как отдельный пакет. Gateway материализует
`config.toml`, `agents/` и `skills/` как собственную `.codex`-папку внутри
runtime workspace, а корневой `AGENTS.md` кладет рядом с этой `.codex`-папкой,
чтобы Codex прочитал project instructions стандартным способом.

Практически это значит:

- агент из `workflow A` не становится доступным в `workflow B`;
- skill из `workflow A` не появляется в `workflow B`;
- `AGENTS.md` из `workflow A` не применяется к `workflow B`;
- один и тот же global `CODEX_HOME` может давать Codex auth, но workflow-состав
  берется из конкретной workflow-папки.

Для воспроизводимой проверки есть:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\examples\full-e2e\run-real-codex.ps1
```

Этот e2e поднимает gateway, запускает real Codex, проверяет SDK/gateway lifecycle
и отдельно проверяет разные workflow с разными `AGENTS.md`, agents и skills.

## Какие Примеры Смотреть

- `examples/workflow-smoke` - самый быстрый lifecycle: init, первый ответ,
  continuation.
- `examples/api-handler` - backend HTTP handler поверх SDK.
- `examples/full-e2e` - полный real-Codex e2e для проверки SDK/gateway.
- `examples/workflows/plain-chat` - самый маленький workflow.
- `examples/workflows/writer-notes` - workflow с локальными reference-файлами и
  MCP fixture.

## Production Checklist

Перед использованием в приложении проверьте:

- Gateway слушает только loopback или приватную сеть.
- Browser никогда не получает gateway-token.
- `auth.json` не копируется в workflow и не попадает в repository.
- Backend хранит `chat_id` и свой список чатов в своей базе.
- `workflow_storage_dir` находится в понятном persistent-каталоге.
- Для каждого tenant/project выбран понятный `workflow_namespace`,
  `workflow_id`, `session_group_id` и `workspace_id`.
- Ошибки показываются пользователю через безопасное сообщение, а не через raw
  dump.
