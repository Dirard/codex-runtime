# Документация Codex Runtime

Эта папка - маршрут для человека, который впервые открыл проект.

Codex Runtime состоит из private gateway и Go SDK. Backend вызывает SDK, SDK
ходит в gateway, gateway запускает Codex и изолирует workflow-пакеты.

```text
browser/mobile -> backend -> Go SDK -> private gateway -> Codex
```

## Что Читать

| Если нужно | Откройте |
| --- | --- |
| Быстро запустить локальный пример | [`quickstart.ru.md`](quickstart.ru.md) |
| Встроить SDK в backend | [`backend-integration.ru.md`](backend-integration.ru.md) |
| Писать typed event/action loop без protobuf | [`go-sdk-friendly.ru.md`](go-sdk-friendly.ru.md) |
| Понять, что лежит в workflow | [`workflow-package.ru.md`](workflow-package.ru.md) |
| Настроить gateway TOML | [`gateway-config.ru.md`](gateway-config.ru.md) |
| Разобраться в архитектуре | [`architecture.ru.md`](architecture.ru.md) |
| Найти подходящий пример | [`examples.ru.md`](examples.ru.md) |
| Починить частую ошибку | [`troubleshooting.ru.md`](troubleshooting.ru.md) |
| Собрать release | [`releases.ru.md`](releases.ru.md) |

## Минимальная Модель

Есть четыре важных слова:

- Gateway - локальный процесс с gRPC API. Он запускает Codex.
- SDK - Go-библиотека, которую вызывает backend.
- Workflow - папка с `config.toml`, `agents/`, `skills/`, `AGENTS.md` и
  reference-файлами для конкретной задачи.
- `chat_id` - id чата. Его хранит ваше приложение, чтобы продолжить диалог.
- `run_id` - текущая попытка ответа внутри chat.

## Что Не Нужно Делать

- Не подключайте browser напрямую к gateway.
- Не храните gateway-token в frontend.
- Не кладите Codex `auth.json` в workflow.
- Не считайте gateway базой пользовательских чатов: список чатов хранит ваше
  приложение.
