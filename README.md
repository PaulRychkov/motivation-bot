# activization bot

Telegram-бот мотивации: помогает доводить задачи до конца — утренний план, вечерний итог, напоминания о дедлайнах, похвала за сделанное и свободные пинги в рамках дневного бюджета. Часть экосистемы activization (tasks + pomodoro + bot), сервисы общаются через Kafka событиями CloudEvents 1.0.

Концепция «Гибрид»: гарантированные точки касания (расписание, дедлайны) создаёт core-service, а содержание и тон каждого сообщения генерирует ReAct-агент (LLM через OpenRouter; модель настраивается `BOT_LLM_MODEL`, дефолт deepseek/deepseek-chat, в живом деплое — anthropic/claude-sonnet-5).

## Архитектура

```
Telegram <—long polling—> telegram-service
                              |  bot.tg-updates / bot.tg-outgoing / bot.tg-sent
                            Kafka
                              |  bot.agent-requests / bot.agent-responses
core-service (8083, PG 5435) <—> agent-service (ReAct, OpenRouter, MCP)
        ^  tasks.events / pomodoro.events            |
        |                                            v
   tasks REST (8081)                    tasks MCP (8081/mcp), pomodoro MCP (8082/mcp)
```

- **cmd/telegram-service** — мост Telegram ⇄ Kafka (long polling через github.com/go-telegram/bot).
- **cmd/core-service** — единственный владелец БД bot (PostgreSQL 5435): профили, интервенции, инбокс событий, диалог, память агента, outbox; планировщик, матчер активаций, воркеры.
- **cmd/agent-service** — ReAct-агент: диалог с пользователем и генерация текстов интервенций; тулы — внутренние (core REST) и MCP-клиенты tasks/pomodoro.

Один go.mod, общие пакеты в `internal/` (config, logger, kafkax, cloudevents, contracts, bootstrap).

## Структура

```
bot/
├── cmd/
│   ├── core-service/      main + Dockerfile + README
│   ├── agent-service/     main + Dockerfile + README
│   └── telegram-service/  main + Dockerfile + README
├── internal/
│   ├── agent/             openrouter, prompts, react, tools (core+MCP), coreclient, run
│   ├── core/              models, logic (чистая бизнес-логика), app, handler, tasksclient
│   ├── bootstrap/         retry-подключения к Kafka/PG
│   ├── cloudevents/       конверт CloudEvents 1.0
│   ├── config/            viper + .env, префикс BOT_
│   ├── contracts/         сообщения внутренних топиков
│   ├── kafkax/            producer/consumer (IBM/sarama) + имена топиков
│   ├── logger/            zap
│   └── texts/             общие фолбэк-тексты core и агента
├── migrations/            SQL-миграции БД bot (golang-migrate)
├── prompts/               промпты агента (*.md, hot-reload без пересборки)
├── embed.go               go:embed промптов — фолбэк, если каталога нет рядом с бинарём
├── docker-compose.yml     postgres 5435 + три сервиса
└── .env                   секреты (не в git), образец — .env.example
```

Бот однопользовательский по дизайну: одна БД, один владелец; профили чатов существуют для стройности модели, praise рассылается всем профилям.

## Запуск

Секреты: скопируйте `.env.example` в `.env` и заполните `TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`.

Один раз создайте общую docker-сеть экосистемы (compose-файлы объявляют её как external):

```
wsl -d Ubuntu-22.04 -- docker network create ecosystem
```

Сначала общая Kafka экосистемы (из корня экосистемы):

```
wsl -d Ubuntu-22.04 -- bash -c "cd /mnt/c/Users/Pavel/Documents/Projects/activization_bot/infra && docker compose up -d"
```

Затем бот (Docker только в WSL):

```
wsl -d Ubuntu-22.04 -- bash -c "cd /mnt/c/Users/Pavel/Documents/Projects/activization_bot/bot && docker compose up -d --build"
```

Локально без Docker (нужны запущенные Kafka 9092 и PostgreSQL 5435):

```
go run ./cmd/core-service
go run ./cmd/agent-service
go run ./cmd/telegram-service
```

Тесты и проверки:

```
go test ./...
go vet ./...
```

## Конфигурация (env, префикс BOT_, поверх .env)

| Переменная | Default | Смысл |
|---|---|---|
| TELEGRAM_BOT_TOKEN | — | токен BotFather (без префикса BOT_) |
| OPENROUTER_API_KEY | — | ключ OpenRouter (без префикса BOT_) |
| BOT_LLM_MODEL | deepseek/deepseek-chat | модель агента |
| BOT_HTTP_PORT | 8083 | порт core REST |
| BOT_DB_HOST/PORT/USER/PASSWORD/NAME | localhost/5435/bot/bot/bot | PostgreSQL |
| BOT_KAFKA_BROKERS | localhost:9094 | брокеры через запятую |
| BOT_CORE_URL | http://localhost:8083 | core для агента |
| BOT_TASKS_URL / BOT_POMODORO_URL | :8081 / :8082 | REST соседних проектов |
| BOT_TASKS_MCP_URL / BOT_POMODORO_MCP_URL | …/mcp | MCP-эндпоинты |
| BOT_DEFAULT_TIMEZONE | Europe/Moscow | таймзона новых профилей |
| BOT_LLM_TIMEOUT_SECONDS | 90 | таймаут одного вызова LLM |
| BOT_REACT_MAX_ITERATIONS | 8 | лимит итераций ReAct-цикла |
| BOT_SCHEDULER_SECONDS | 60 | тик планировщика |
| BOT_DEADLINE_POLL_SECONDS | 900 | опрос дедлайнов tasks |
| BOT_WINDOW_WORKER_SECONDS | 300 | закрытие просроченных окон |
| BOT_OUTBOX_RELAY_SECONDS | 5 | релей outbox → bot.events |
| BOT_PROMPT_DIR | prompts | каталог промптов агента |
| BOT_MIGRATIONS_DIR | migrations | каталог миграций |
| BOT_PHONE_INGEST_TOKEN | — | токен для приёма данных с телефона; пустой — приём без проверки |
| BOT_DISTRACTION_MINUTES | 15 | сколько минут в отвлекающем приложении до напоминания |
| BOT_DISTRACTION_COOLDOWN_MINUTES | 60 | перерыв между напоминаниями об отвлечении |

## Топики Kafka

`tasks.events`, `pomodoro.events`, `phone.events` (вход, CloudEvents) · `bot.events` (выход: intervention.sent, activation.detected) · внутренние: `bot.tg-updates`, `bot.tg-outgoing`, `bot.tg-sent`, `bot.agent-requests`, `bot.agent-responses`. Consumer group core — `bot-core-group`.

## Приём данных с телефона

`POST /ingest/v1/phone-usage` — принимает CloudEvent `source=phone`, `type=phone.usage.snapshot` от приложения [phone-focus](https://github.com/PaulRychkov/phone-focus). Авторизация — заголовок `Authorization: Bearer <BOT_PHONE_INGEST_TOKEN>`. Событие публикуется в топик `phone.events`, оттуда попадает в инбокс тем же путём, что задачи и помидор.

Дальше `internal/core/app/phone.go` смотрит снимок: если экран включён, текущее приложение из отвлекающей категории (social, video, games) и человек сидит в нём дольше `BOT_DISTRACTION_MINUTES` подряд (или суммарно за окно), создаётся интервенция `question` с триггером `schedule` и контекстом `reason=distraction`. Текст пишет агент по `prompts/intervention.md`. Действуют общие ограничения `createIntervention`: тихие часы, пауза профиля и перерыв между напоминаниями. Решение вынесено в чистую функцию `logic.EvaluateDistraction` и покрыто table-driven-тестами.

## Ключевые правила (docs/data-model.md §4)

- Матчинг активаций — строго по payload (task_id/date у tasks, task{source,external_id} у pomodoro), все 9 правил покрыты table-driven-тестами в `internal/core/logic`.
- `plan.committed` активирует morning_plan при `committed_by=app` или при коммите через явную реплику пользователя: ответ реплаем в Telegram на сообщение интервенции связывает реплику с ней (`dialog_messages.intervention_id`).
- Инбокс — at-least-once: при ошибке обработки событие не закрывается, а уходит в отложенный ретрай (attempts / last_error / next_attempt_at); consumer внешних событий не коммитит offset при ошибке вставки.
- Антидубль планировщика — partial UNIQUE (chat_profile_id, kind, local_date) + предварительная проверка.
- Бюджет free_ping — count() по interventions за локальные сутки профиля; praise и question вне бюджета; free_ping/question инициирует агент тулом send_ping (POST /internal/v1/interventions).
- Тихие часы поддерживают интервал через полночь; paused_until останавливает все касания.
- Окна: pending → ignored при пустом evidence (воркер ~5 мин); refused/rescheduled ставит только агент.
- Retention: диалог 30 дней (последние 200 всегда живут), инбокс 90 дней.
