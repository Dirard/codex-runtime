# Интеграция В Backend

Этот документ для backend-разработчика. Frontend вызывает ваш backend, а backend
уже вызывает gateway через Go SDK.

## Поток

```text
1. backend создает SDK client
2. backend вызывает InitWorkflow
3. backend отправляет первый prompt через workflow.Run
4. backend читает typed events и stream-ит безопасные payload во frontend
5. backend сохраняет chat.ID
6. backend продолжает чат через workflow.GetChat + chat.RunWithEvents
```

## Создайте SDK Client

```go
import (
    "os"
    "strings"

    codex "github.com/Dirard/codex-runtime/sdk/go"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

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
    codex.WithSessionGroupID("workflow-smoke-session"),
    codex.WithWorkspaceID("workflow-smoke-workspace"),
)
if err != nil {
    return err
}
```

`session_group_id` и `workspace_id` должны совпадать с gateway config.

## Инициализируйте Workflow

```go
workflow, err := client.InitWorkflow(ctx, codex.WorkflowDir{
    Namespace: "examples",
    ID:        "writer-notes",
    Path:      ".\\.local\\workflows\\writer-notes",
})
if err != nil {
    return err
}
```

`Namespace` и `ID` - стабильная identity workflow. Их удобно хранить рядом с
`chat_id` в базе приложения.

## Запустите Первый Turn

```go
chat, events, err := workflow.Run(ctx, "Summarize the harbor note.")
if err != nil {
    return err
}
defer events.Close()

saveChatIDForUser(userID, chat.ID)
```

## Читайте Stream

```go
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
    case *codex.CommandStarted:
        renderCommandStart(e.Command().Display)
    case *codex.CommandOutput:
        command := e.Command()
        if command.Known {
            renderCommandOutput(command, e.Stream(), e.Delta())
        } else if e.OrphanReplay() {
            renderLimitedCommandOutput(e.Stream(), e.Delta())
        }
    case *codex.Warning:
        renderWarning(e.Message())
    case codex.TerminalEvent:
        return e.Result().Err
    }

    if event.Meta().CanResumeAfter {
        saveResumeCursor(chat.ID, event.Meta().Cursor)
    }
}
```

Frontend должен получать только ваш stream. Gateway-token, Codex auth и gRPC
адрес gateway во frontend не передаются.
Pending action не сохраняется как safe resume cursor: сначала покажите action и
примите явное product decision, затем продолжайте через `Chat.Respond`.

## Продолжите Тот Же Chat

```go
chat, err := workflow.GetChat(ctx, storedChatID)
if err != nil {
    return err
}

result, events, err := chat.RunWithEvents(ctx, "Continue from the previous answer.")
if err != nil {
    return err
}
defer events.Close()

_ = result.RunID
```

`Chat.Run(ctx, prompt)` по-прежнему возвращает `*RunResult` для совместимости.
Если run уже принят gateway, stream можно открыть так:

```go
events, err := chat.EventsForRun(ctx, result)
if err != nil {
    return err
}
defer events.Close()
```

Для replay после сохраненного safe cursor используйте
`chat.GetEventsStream(ctx, codex.AfterEventCursor(cursor))`. `AfterEventID` и
`LastEventID` оставьте для advanced gateway compatibility.

## Status И History

```go
status, err := chat.GetStatusSnapshot(ctx)
if err != nil {
    return err
}

page, err := chat.GetHistoryPage(
    ctx,
    codex.WithHistoryPageDepth(codex.HistoryDepthTurnSummary),
    codex.WithHistoryPageLimit(20),
    codex.WithHistoryPageSort(codex.HistorySortAscending),
)
if err != nil {
    return err
}

_ = status.RunID
_ = page.Turns
```

Raw `GetStatus`, `GetHistory`, generated protobuf and `events.Recv()` остаются
advanced compatibility path. Для обычного backend-кода начинайте с
[`go-sdk-friendly.ru.md`](go-sdk-friendly.ru.md).

## Что Хранить В Своей Базе

Минимум:

- ваш `user_id` или `tenant_id`;
- `workflow_namespace`;
- `workflow_id`;
- `chat_id`;
- title/status/last message, если нужен список чатов в UI.

Gateway хранит runtime-состояние, но не заменяет вашу продуктовую базу.

## Ошибки

SDK возвращает typed error. Не парсите строки ошибок.

```go
if sdkErr, ok := codex.AsError(err); ok {
    logSafe(sdkErr.WorkflowCode, sdkErr.DisplayMessage, sdkErr.NextAction)
    return safeHTTPError(sdkErr)
}
```

Полный backend HTTP пример: [`../examples/api-handler`](../examples/api-handler).
