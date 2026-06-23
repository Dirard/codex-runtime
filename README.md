# Codex Runtime

Codex Runtime помогает встроить Codex в свое backend-приложение.

Идея простая:

```text
browser/mobile -> ваш backend -> Go SDK -> private gateway -> Codex
```

Browser или mobile-клиент не ходит в gateway напрямую и не получает секреты.
Gateway живет рядом с backend, слушает локальный gRPC-адрес, запускает Codex и
работает с workflow-пакетами.

## Что Внутри

- `gateway/cmd/codex-runtime-gateway` - локальный gateway-процесс.
- `sdk/go` - Go SDK для backend-кода.
- `proto/codex_control/v1` - gRPC contract между SDK и gateway.
- `examples/workflows` - готовые workflow-пакеты.
- `examples/api-handler` - пример HTTP handler поверх SDK.
- `examples/workflow-smoke` - быстрая проверка lifecycle.
- `docs` - подробная документация на русском.

## Быстрый Старт

Это короткий обзор команд. Полный copy-paste запуск с gateway config лежит в
[`docs/quickstart.ru.md`](docs/quickstart.ru.md).

Нужны Go, локальный Codex binary, рабочий `CODEX_HOME` с Codex auth и отдельный
gateway-token для backend -> gateway.

```powershell
New-Item -ItemType Directory -Force .\.local | Out-Null

$bytes = New-Object byte[] 32
[System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
[Convert]::ToBase64String($bytes).TrimEnd("=") |
  Set-Content -Encoding UTF8 .\.local\gateway.token

go run .\examples\workflow-scaffold `
  -source .\examples\workflows\writer-notes `
  -target .\.local\workflows\writer-notes
```

Создайте `.local\gateway.workflow.toml` по полному quickstart, затем запустите
gateway:

```powershell
go run .\gateway\cmd\codex-runtime-gateway --config .\.local\gateway.workflow.toml
```

Во втором терминале проверьте smoke-сценарий:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass `
  -File .\examples\workflow-smoke\run-local.ps1 `
  -TokenSource .\.local\gateway.token `
  -WorkflowDir .\.local\workflows\writer-notes
```

Smoke должен показать готовность gateway, `chat_id`, первый ответ и продолжение
того же чата.

## Куда Читать Дальше

- [`docs/README.ru.md`](docs/README.ru.md) - карта всей документации.
- [`docs/quickstart.ru.md`](docs/quickstart.ru.md) - запуск с нуля.
- [`docs/backend-integration.ru.md`](docs/backend-integration.ru.md) - как
  встроить Go SDK в backend.
- [`docs/go-sdk-friendly.ru.md`](docs/go-sdk-friendly.ru.md) - friendly typed
  events, pending actions, resume и raw/proto advanced boundary.
- [`docs/workflow-package.ru.md`](docs/workflow-package.ru.md) - как устроен
  workflow-пакет.
- [`docs/gateway-config.ru.md`](docs/gateway-config.ru.md) - поля gateway config.
- [`docs/architecture.ru.md`](docs/architecture.ru.md) - устройство проекта.
- [`docs/troubleshooting.ru.md`](docs/troubleshooting.ru.md) - частые ошибки.
- [`docs/examples.ru.md`](docs/examples.ru.md) - какой пример запускать.
- [`docs/releases.ru.md`](docs/releases.ru.md) - сборка и публикация релизов.

## Минимальный SDK Поток

```go
workflow, err := client.InitWorkflow(ctx, codex.WorkflowDir{
    Namespace: "examples",
    ID:        "writer-notes",
    Path:      ".\\.local\\workflows\\writer-notes",
})
if err != nil {
    return err
}

chat, events, err := workflow.Run(ctx, "Summarize the harbor note.")
if err != nil {
    return err
}
defer events.Close()

saveChatIDForUser(userID, chat.ID)

for {
    event, err := events.NextEvent(ctx)
    if errors.Is(err, io.EOF) {
        break
    }
    if err != nil {
        return err
    }
    switch e := event.(type) {
    case *codex.AssistantTextDelta:
        sendToBrowser(e.TextDelta())
    case codex.PendingAction:
        renderAction(e.Display())
        return nil
    case codex.TerminalEvent:
        return e.Result().Err
    }
}
```

Для продолжения диалога backend хранит `chat.ID`, потом вызывает
`workflow.GetChat(ctx, chatID)` и `chat.RunWithEvents(ctx, prompt)`. Старый
`chat.Run(ctx, prompt)` сохранен совместимым и возвращает `*RunResult`.

## Главные Правила Безопасности

- Не отдавайте gateway-token в browser/mobile.
- Не кладите `auth.json`, gateway-token, cookies или API keys в workflow.
- Держите gateway на loopback или в приватной сети.
- Храните список чатов, пользователей и tenant-данные в своей базе, а не в UI.
- Показывайте пользователю безопасные ошибки из SDK, а не raw logs.
