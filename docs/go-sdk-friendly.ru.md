# Friendly Go SDK

Эта страница показывает основной путь для backend-кода: typed events, typed
pending actions и cursor-based resume. Generated protobuf остается доступен
только как advanced compatibility layer.

## Ментальная Модель

- `Chat` и `ChatID` - conversation/thread, который ваше приложение хранит в
  своей базе рядом с `user_id` или `tenant_id`.
- `RunID` - текущая попытка ответа внутри этого chat. Отдельного public turn id
  в текущем runtime/proto нет; это зафиксированный gap.
- `EventMeta.CanResumeAfter` означает, что после этого события можно безопасно
  сохранить `EventMeta.Cursor` и позже передать `codex.AfterEventCursor(cursor)`.
- `ReplayNotice`, narrowed/synthetic diagnostics и unresolved pending action не
  являются resume points.

## Общий Event/Action Loop

Один и тот же loop подходит для `Client.Run`, `Workflow.Run`,
`Chat.RunWithEvents` и `Chat.EventsForRun`.

```go
chat, events, err := workflow.Run(ctx, "Summarize the harbor note.")
if err != nil {
    return err
}
defer events.Close()

saveChatIDForUser(userID, chat.ID)

var safeCursor string
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

    if event.Meta().CanResumeAfter {
        safeCursor = event.Meta().Cursor
        saveResumeCursor(chat.ID, safeCursor)
    }
}
```

`NextEvent(ctx)` проверяет `ctx` перед каждым receive. Если receive уже ждет
gRPC stream, отмена управляется context-ом, с которым stream был открыт, или
`EventStream.Close()`.

`Client.Run(ctx, prompt)` возвращает `Chat + EventStream` для plain chat.
`Workflow.Run(ctx, prompt)` возвращает тот же friendly stream, но запускает Codex
в workflow package. Старый `Chat.Run(ctx, prompt)` сохранен совместимым и
возвращает только `*RunResult`; для typed stream продолжения используйте
`Chat.RunWithEvents`.

## Продолжение И Resume

```go
chat, err := workflow.GetChat(ctx, storedChatID)
if err != nil {
    return err
}

result, events, err := chat.RunWithEvents(ctx, "Continue.")
if err != nil {
    return err
}
defer events.Close()

_ = result.RunID
```

Если run уже был принят gateway и у вас есть `RunResult`, получите stream так:

```go
events, err := chat.EventsForRun(ctx, result)
if errors.Is(err, codex.ErrEventCursorUnavailable) {
    // Accepted raw-compatible run had no opaque cursor. Re-run is not started.
    return err
}
```

Для replay после сохраненного события:

```go
events, err := chat.GetEventsStream(ctx, codex.AfterEventCursor(savedCursor))
```

`AfterEventID` и `LastEventID` оставлены как advanced gateway-scope
compatibility. Beginner path использует opaque cursor. `RunWithEvents` и
`RunWithHandler` отклоняют initial stream options, которые двигают cursor, с
`ErrStreamCursorConflict` до старта нового run.

## Terminal, Status И History

Terminal-событие завершает текущий stream:

```go
case codex.TerminalEvent:
    result := e.Result()
    if result.Err != nil {
        return result.Err
    }
```

После terminal можно запросить friendly snapshot и history page:

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
```

`CachedStatusSnapshot` полезен, когда stream уже обновил local status. Raw
`CachedStatus`, `GetStatus`, `GetHistory` и protobuf-shaped `HistoryOption`
остаются advanced compatibility APIs. Friendly значения используют SDK-level
строковые enums: `ThreadLifecycle`, `RunLifecycle`, `CapabilityState`,
`HistoryDepth` и `HistorySortDirection`, поэтому common path не импортирует
generated protobuf.

## Pending Actions

UI должен рендерить `ActionDisplay`: `Title` и `Summary` всегда непустые за счет
SDK fallback, `Details` могут быть пустыми или redacted.

```go
case codex.PendingAction:
    display := e.Display()
    showAction(display.Title, display.Summary, display.Details)
```

Pending action не является safe resume point: SDK возвращает
`EventMeta.CanResumeAfter == false`, пока action не решен через `Chat.Respond`
или отдельный product flow.

Approval нельзя принимать без policy check:

```go
case *codex.ApprovalRequested:
    if e.Subject() == codex.ApprovalSubjectCommand &&
        allowedCommand(e.Command().CommandDisplay, e.Command().Security, e.Decisions()) {
        response, err := e.Approve()
        if err != nil {
            return err
        }
        return chat.Respond(ctx, response)
    }
    response, err := e.Deny()
    if err != nil {
        return err
    }
    return chat.Respond(ctx, response)
```

Для file-change approval проверяйте `e.File().FileLabel`,
`e.File().ChangeKind`, `e.File().DiffSummary`, `e.File().GrantRoot` и
source-backed `Decisions()`. Для command approval проверяйте
`e.Command().Security`: privilege expansion, filesystem entries, network
summary, policy amendments и blocking reason, если они source-backed.

Permissions тоже требуют явного решения. Default для grant - strict
auto-review:

```go
case *codex.PermissionsRequested:
    if narrowTurnGrantAllowed(e.RequestedPermissions()) {
        response, err := e.GrantTurn("workspace-read")
        if err != nil {
            return err
        }
        return chat.Respond(ctx, response)
    }
    return chat.Respond(ctx, e.Deny())
```

Не используйте unconditional `GrantSession` и не отключайте strict auto-review в
beginner path.

## Structured И User Input

Structured input показывает source-backed message/form/url. `Fields()` может
вернуть `UnsupportedSchemaError`; `SchemaError()` позволяет показать форму как
unsupported без raw JSON.

```go
case *codex.StructuredInputRequested:
    fields, err := e.Fields()
    if err != nil {
        var unsupported *codex.UnsupportedSchemaError
        if errors.As(err, &unsupported) {
            response := e.Cancel()
            return chat.Respond(ctx, response)
        }
        return err
    }
    values := collectFormValues(fields)
    response, err := e.Submit(values)
    if err != nil {
        var validation *codex.ActionValidationError
        if errors.As(err, &validation) {
            showValidation(validation)
            return nil
        }
        return err
    }
    return chat.Respond(ctx, response)
```

User input использует source-backed questions/options/secret flags/other option
markers:

```go
case *codex.UserInputRequested:
    answers := []codex.UserInputAnswer{
        {QuestionID: "city", Values: []string{"Berlin"}},
    }
    response, err := e.Answer(answers...)
    if err != nil {
        return err
    }
    return chat.Respond(ctx, response)
```

P0 docs не обещают required/default/multiselect/selected state, потому что это
не source-backed текущим wire contract.

## Runtime Command Progress

Normal command output уже несет friendly command context; caller не ведет
`item_id` map.

```go
case *codex.CommandStarted:
    command := e.Command()
    workspace, _ := e.WorkspaceLabel()
    renderCommandStart(command.Display, workspace)

case *codex.CommandOutput:
    command := e.Command()
    truncated, truncatedKnown := e.Truncated()
    if command.Known {
        renderCommandOutput(command.Display, e.Stream(), e.Delta(), truncated, truncatedKnown)
    } else if e.OrphanReplay() {
        renderLimitedProgress(e.Stream(), e.Delta())
    }
```

Текущий runtime использует `CommandOutputStreamCombined`. Если replay начинается
уже после `CommandStarted`, текущий wire contract не несет command snapshot
внутри output delta, поэтому SDK честно показывает `OrphanReplay()` и limited
progress без синтетического command display. `TerminalInteraction` является
terminal stdin и никогда не рендерится как command output.

Warning-события экспортируются только когда они chat/run-owned:

```go
case *codex.Warning:
    code, codeKnown := e.Code()
    renderWarning(e.Message(), code, codeKnown)
```

## RunWithHandler

`RunWithHandler` - это sugar поверх typed stream для backend automation. Он есть
на `Client`, `Workflow` и `Chat`; принимает `opts ...RequestOption`, forward-ит
обычные request options и заранее отклоняет cursor-moving initial stream options.

```go
result, err := workflow.RunWithHandler(ctx, prompt, codex.RunHandler{
    Text: func(ctx context.Context, chat *codex.Chat, e *codex.AssistantTextDelta) error {
        sendToBrowser(e.TextDelta())
        return nil
    },
    Event: func(ctx context.Context, chat *codex.Chat, e codex.StreamEvent) error {
        // CommandStarted, CommandOutput, Warning and UnknownEvent land here
        // when no specialized callback consumed them.
        return nil
    },
    Action: func(ctx context.Context, chat *codex.Chat, action codex.PendingAction) (codex.ActionResponse, error) {
        showAction(action.Display())
        return nil, nil
    },
    Terminal: func(ctx context.Context, chat *codex.Chat, terminal codex.TerminalEvent) error {
        return terminal.Result().Err
    },
})
if errors.Is(err, codex.ErrUnhandledAction) {
    if result.LastSafeResumeMeta.Cursor != "" {
        saveRecovery(result.Chat.ID, result.LastSafeResumeMeta.Cursor)
    }
    showAction(result.UnhandledAction.Display())
}
```

Helper не принимает product decisions за вас: без `Action` callback или при
`nil` response он закрывает stream и возвращает
`HandlerResult{Chat, LastEvent, LastMeta, LastSafeResumeMeta, UnhandledAction}`.
На stream/decode/transport/cancellation ошибках до terminal тот же result
содержит последний безопасный resume point, если он уже был.

## Interrupt

`chat.Interrupt(ctx)` просит gateway остановить active run. Это не то же самое,
что context cancellation или `EventStream.Close()`: cancellation закрывает ваш
запрос, `Close()` закрывает локальное чтение stream, а interrupt меняет runtime
run state и должен позже проявиться как `RunInterrupted`.

```go
if _, err := chat.Interrupt(ctx); err != nil {
    return err
}
```

Workflow-bound chat использует тот же метод после `workflow.GetChat(ctx, id)`.

## Raw/Proto Advanced Boundary

Friendly events всегда имеют `Raw()`. `RawEvent.String`, `GoString` и `slog`
печатают только metadata. `RawEvent.Proto()` возвращает clone
`*pb.StreamChatEventsResponse`; изменение clone не меняет событие.

| Friendly source | `RawEvent.Proto()` envelope |
| --- | --- |
| Chat event | `*pb.StreamChatEventsResponse` with `event` payload |
| Replay notice | `*pb.StreamChatEventsResponse` with `replay_notice` payload |
| Stream narrowed | `*pb.StreamChatEventsResponse` with `narrowed` payload |
| Decode/unknown payload | cloned envelope when source-backed, otherwise metadata-only raw |

Raw `Recv`, generated protobuf clients, raw status/history and legacy gateway
fields are for advanced compatibility, diagnostics or bridge work. Beginner
examples should import only `github.com/Dirard/codex-runtime/sdk/go`.

## Event/Control Catalog

Полный source-backed catalog лежит в
[`sdk-event-catalog.ru.md`](sdk-event-catalog.ru.md). Он разделяет:

- Phase 2 current-P0 typed events.
- Phase 4 runtime-backed `CommandStarted`, `CommandOutput`, run-owned
  `Warning`.
- Bounded `UnknownEvent` plus catalog-only local gaps such as `tool_progress`.
- Catalog-only event/request gaps.
- Non-event or out-of-current-SDK control surfaces including realtime, remote
  control, hooks, collab/subagents and app-server-only areas.
