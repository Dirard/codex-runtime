# Workflow-Пакет

Workflow - это папка, которую приложение передает в gateway. Gateway проверяет
ее, материализует в отдельное runtime-состояние и запускает Codex с этой
конфигурацией.

## Минимальная Структура

```text
my-workflow/
  config.toml
  AGENTS.md
  agents/
    default.toml
  skills/
    my-skill/
      SKILL.md
  references/
    note.md
```

Обязателен только `config.toml`. Остальное добавляется по задаче.

## `config.toml`

```toml
name = "support"
description = "Support assistant workflow."
default_agent = "default"
```

Если workflow объявляет MCP servers, gateway может потребовать отдельную
политику reload. В текущем локальном сценарии проще начинать без MCP, а потом
добавлять его осознанно.

## `AGENTS.md`

`AGENTS.md` в корне workflow - это project instructions только для этого
workflow.

Примеры содержимого:

- тон и стиль ответов;
- что агент обязан проверять;
- какие файлы являются источниками правды;
- что нельзя делать.

Gateway кладет этот файл в runtime workspace root, поэтому Codex читает его как
обычный project-level `AGENTS.md`.

## `agents/`

В `agents/` лежат agent profiles. Минимальный пример:

```toml
name = "default"
description = "Answers support questions."
developer_instructions = """
You help support operators answer user questions clearly and safely.
"""
```

Агенты из одного workflow не видны в другом workflow.

## `skills/`

Каждый skill - отдельная папка с `SKILL.md`.

```text
skills/
  writer-notes/
    SKILL.md
```

Skills тоже изолированы по workflow.

## `references/`

`references/` подходит для небольших не-secret материалов: справок, шаблонов,
тестовых заметок, доменных правил.

Не кладите туда:

- `auth.json`;
- gateway-token;
- cookies;
- private keys;
- API keys;
- `.env` с секретами.

SDK и gateway отклоняют workflow-пакеты с подозрительными secret-like строками и
небезопасными путями.

## Как Gateway Обновляет Workflow

При `InitWorkflow` gateway сравнивает fingerprint пакета:

- тот же пакет - no-op;
- изменились только reference-файлы - обновление применяется без рестарта;
- изменились agents/skills/instructions - обычно нужен `workflow.Restart(ctx)`;
- MCP config может требовать отдельный MCP reload или рестарт.

Если SDK вернул `RestartRequired`, вызовите:

```go
status, err := workflow.Restart(ctx)
```

## Готовые Starters

- [`../examples/workflows/plain-chat`](../examples/workflows/plain-chat) -
  самый маленький workflow.
- [`../examples/workflows/writer-notes`](../examples/workflows/writer-notes) -
  workflow с reference-файлами и локальным tool fixture.
- [`../examples/workflows/visibility-alpha`](../examples/workflows/visibility-alpha)
  и [`../examples/workflows/visibility-beta`](../examples/workflows/visibility-beta) -
  fixtures для проверки изоляции.
