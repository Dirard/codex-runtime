# Примеры

Эта страница помогает выбрать пример без перебора всех папок.

## Самый Быстрый Lifecycle

Путь: [`../examples/workflow-smoke`](../examples/workflow-smoke)

Проверяет:

- gateway доступен;
- `InitWorkflow` работает;
- первый ответ stream-ится;
- возвращается `chat_id`;
- продолжение того же чата работает.

```powershell
powershell -NoProfile -ExecutionPolicy Bypass `
  -File .\examples\workflow-smoke\run-local.ps1 `
  -TokenSource .\.local\gateway.token `
  -WorkflowDir .\.local\workflows\writer-notes
```

## Backend HTTP Handler

Путь: [`../examples/api-handler`](../examples/api-handler)

Показывает правильную границу:

```text
browser -> app backend /workflow -> Go SDK -> private gateway -> Codex workflow
```

Browser не получает gateway credentials.

## Workflow Scaffold

Путь: [`../examples/workflow-scaffold`](../examples/workflow-scaffold)

Копирует starter workflow в app-owned папку:

```powershell
go run .\examples\workflow-scaffold `
  -source .\examples\workflows\writer-notes `
  -target .\.local\workflows\writer-notes
```

## Workflow Probe

Путь: [`../examples/workflow-probe`](../examples/workflow-probe)

Быстро проверяет, что backend environment может достучаться до gateway.

## Full E2E

Путь: [`../examples/full-e2e`](../examples/full-e2e)

Запускает real Codex, gateway и проверяет полный SDK/gateway lifecycle.

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\examples\full-e2e\run-real-codex.ps1 -WhatIf
powershell -NoProfile -ExecutionPolicy Bypass -File .\examples\full-e2e\run-real-codex.ps1
```

## Starter Workflows

Путь: [`../examples/workflows`](../examples/workflows)

- `plain-chat` - минимальный workflow.
- `writer-notes` - workflow с reference-файлами.
- `visibility-alpha` и `visibility-beta` - fixtures для проверки workflow
  isolation.

## Direct Chat Example

Путь: [`../examples/e2e-chat`](../examples/e2e-chat)

Показывает chat-first SDK path без workflow package. Пример читает stream через
`EventStream.NextEvent`, печатает typed assistant/command/warning/pending/terminal
events, использует `Chat.RunWithEvents` для продолжения и friendly
`GetStatusSnapshot`/`GetHistoryPage` для состояния.

Для нового app-owned workflow обычно полезнее начинать с `workflow-smoke` и
`api-handler`, а детали event/action loop смотреть в
[`go-sdk-friendly.ru.md`](go-sdk-friendly.ru.md).
