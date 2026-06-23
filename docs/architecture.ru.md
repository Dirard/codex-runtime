# Архитектура

Codex Runtime - это локальная прослойка между вашим backend и Codex.

```text
browser/mobile
    |
    v
your backend
    |
    v
Go SDK
    |
    v
private gateway (gRPC, loopback)
    |
    v
Codex app-server / Codex process
```

## Компоненты

### Go SDK

Путь: [`../sdk/go`](../sdk/go)

SDK строит workflow package, вызывает gRPC gateway и нормализует ответы в Go
типы:

- `Client`;
- `Workflow`;
- `Chat`;
- `EventStream`;
- typed errors через `codex.AsError`.

### Gateway

Путь: [`../gateway/cmd/codex-runtime-gateway`](../gateway/cmd/codex-runtime-gateway)

Gateway:

- читает trusted TOML config;
- проверяет token source;
- поднимает gRPC server;
- запускает Codex process;
- создает runtime для workflow;
- stream-ит события обратно в SDK.

### Proto Contract

Путь: [`../proto/codex_control/v1/codex_control.proto`](../proto/codex_control/v1/codex_control.proto)

Главные сервисы:

- `ChatRuntimeService` - обычные chat operations.
- `WorkflowRuntimeService` - workflow init/status/run/restart/chat operations.
- `CodexControl` - legacy task API.

### Workflow Storage

Gateway хранит workflow runtime state под `workflow_storage_dir`.

Для каждого workflow есть storage key, current/pending/previous package revision
и runtime workspace. Это внутренняя механика gateway; приложение обычно хранит
только `chat_id`, workflow identity и свои user/tenant данные.

## Lifecycle Запроса

1. Backend создает SDK client с gateway-token.
2. SDK собирает workflow folder или zip в canonical package.
3. Gateway валидирует package: paths, fingerprint, size limits, secret-like content.
4. Gateway материализует workflow в storage.
5. Gateway запускает или переиспользует Codex runtime.
6. Backend вызывает `workflow.Run`.
7. Gateway начинает Codex turn и stream-ит normalized events.
8. Backend отправляет свой stream во frontend.
9. Backend сохраняет `chat_id`.

## Изоляция

Workflow A не получает автоматически:

- agents из workflow B;
- skills из workflow B;
- `AGENTS.md` из workflow B;
- reference-файлы workflow B.

Один global `CODEX_HOME` может давать Codex auth, но runtime-конфигурация берется
из конкретного workflow-пакета.

## Где Что Искать В Коде

- Gateway entrypoint: [`../gateway/cmd/codex-runtime-gateway/main.go`](../gateway/cmd/codex-runtime-gateway/main.go)
- Config validation: [`../gateway/internal/config`](../gateway/internal/config)
- gRPC handlers: [`../gateway/internal/grpcapi`](../gateway/internal/grpcapi)
- Chat runtime: [`../gateway/internal/chatruntime`](../gateway/internal/chatruntime)
- Workflow runtime: [`../gateway/internal/workflowruntime`](../gateway/internal/workflowruntime)
- Workflow storage: [`../gateway/internal/workflowstorage`](../gateway/internal/workflowstorage)
- SDK: [`../sdk/go`](../sdk/go)
