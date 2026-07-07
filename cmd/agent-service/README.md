# agent-service

ReAct-агент на DeepSeek (через OpenRouter, OpenAI-совместимый Chat Completions с tools/tool_calls). БД не имеет — всё состояние через core REST.

## Потоки

1. **Диалог** — консюмер `bot.tg-updates`: ensure-профиль в core (первое сообщение незнакомого chat_id создаёт профиль → онбординг-приветствие), реплика сохраняется в dialog_messages, история + persona + agent_notes собираются в системный промпт, запускается ReAct-цикл (лимит итераций BOT_REACT_MAX_ITERATIONS, таймаут шага BOT_LLM_TIMEOUT_SECONDS), каждый шаг (assistant/tool) сохраняется через core, финальный текст → `bot.tg-outgoing`. При сбое LLM — фолбэк-текст.
2. **Генерация текстов** — консюмер `bot.agent-requests` {correlation_id, kind, context}: короткая генерация по промпту intervention.md с учётом тона → `bot.agent-responses` {correlation_id, text, tone}; при сбое LLM — фолбэк по kind.

## Тулы агента

- Внутренние (core REST): `get_profile`, `update_profile`, `list_pending_interventions`, `set_intervention_outcome`, `send_ping` (free_ping/question с учётом бюджета и тихих часов), `save_note`, `get_effectiveness_stats`.
- MCP: клиенты tasks (`:8081/mcp`) и pomodoro (`:8082/mcp`) через `modelcontextprotocol/go-sdk` (streamable HTTP); тулы проксируются с префиксами `tasks_` / `pomodoro_`; недоступность MCP не роняет диалог — агент получает текст ошибки.

## Промпты

`prompts/*.md` (dialog, onboarding, intervention) читаются с диска с hot-reload по mtime — правки применяются без пересборки; пустой/удалённый файл откатывается на вшитый фолбэк (`internal/agent/run/fallbacks.go`).

## Запуск

```
go run ./cmd/agent-service
```

Нужны: Kafka, core-service, `OPENROUTER_API_KEY` в `.env`. Модель — `BOT_LLM_MODEL` (default `deepseek/deepseek-chat`). Тесты ReAct-цикла с mock-LLM — `go test ./internal/agent/...`.
