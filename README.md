# activization bot

Telegram-бот мотивации: помогает доводить задачи до конца — утренний план, вечерний итог, напоминания о дедлайнах, похвала за сделанное и свободные пинги в рамках дневного бюджета. Часть экосистемы activization (tasks + pomodoro + bot), сервисы общаются через Kafka событиями CloudEvents 1.0.

Концепция «Гибрид»: гарантированные точки касания (расписание, дедлайны) создаёт core-service, а содержание и тон каждого сообщения генерирует ReAct-агент (LLM через OpenRouter; модель настраивается `BOT_LLM_MODEL`, дефолт и живой деплой — z-ai/glm-5.3-flash).

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
- **cmd/chat-service** (8084) — свой чат-канал бота **параллельно Telegram**: WebSocket-сервер принимает сообщения пользователя и публикует в `bot.tg-updates` (тот же вход, что и Telegram), а ответы из `bot.tg-outgoing` (группа `bot-chat-group`) рассылает клиентам. История — из core `/internal/v1/dialog`. Раздаёт свой SPA (`chat-desktop/frontend`). Ответ приходит и в чат, и в Telegram — каналы равноправны.
- **chat-desktop/** — Windows-приложение чата (Wails, отдельный go.mod): встраивает тот же чат-SPA, `GetBackendURL` → chat-service (по умолчанию `localhost:18084` через SSH-туннель к VM). Аналог Telegram-клиента под винду.

Один go.mod (кроме chat-desktop), общие пакеты в `internal/` (config, logger, kafkax, cloudevents, contracts, bootstrap).

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
| BOT_LLM_MODEL | z-ai/glm-5.3-flash | модель агента |
| BOT_HTTP_PORT | 8083 | порт core REST |
| BOT_DB_HOST/PORT/USER/PASSWORD/NAME | localhost/5435/bot/bot/bot | PostgreSQL |
| BOT_KAFKA_BROKERS | localhost:9094 | брокеры через запятую |
| BOT_CORE_URL | http://localhost:8083 | core для агента |
| BOT_TASKS_URL / BOT_POMODORO_URL | :8081 / :8082 | REST соседних проектов |
| BOT_TASKS_MCP_URL / BOT_POMODORO_MCP_URL | …/mcp | MCP-эндпоинты |
| BOT_DEFAULT_TIMEZONE | Europe/Moscow | таймзона новых профилей |
| BOT_LLM_TIMEOUT_SECONDS | 90 | таймаут одного вызова LLM |
| BOT_LLM_MAX_TOKENS | 4096 | потолок ответа за один вызов LLM; без него OpenRouter резервирует максимум модели (65536 у Sonnet 5) под лимит ключа и отвечает 402 |
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
| BOT_CHAT_PORT | 8084 | порт chat-service (WebSocket + SPA) |
| BOT_CHAT_CHAT_ID | 0 | chat_id профиля, с которым работает свой чат-канал |
| BOT_CHAT_TOKEN | — | bearer-токен chat-service (пусто — без проверки) |

## Свой чат-канал (аналог Telegram)

`cmd/chat-service` даёт боту второй канал общения рядом с Telegram — на случай, когда Telegram недоступен или не нужен. Сборка и деплой:

```powershell
deploy\build-chat.ps1   # фронт → webdist chat-service → linux-бинарь для VM → Windows-exe
```

На VM chat-service поднимается в `docker-compose` (порт 8084) из готового бинаря (`Dockerfile.prebuilt`) — VM не качает Go-модули (в РФ прокси Go режется). Windows-клиент `chat-desktop/build/bin/motivator-chat.exe` ходит на chat-service через SSH-туннель `localhost:18084`.

## Бот на телефоне (весь бэкенд одним процессом)

`mobile/` — gomobile-пакет: **весь бот в одном процессе на телефоне**, без Kafka и Postgres. Три сервиса (core + agent + доставка/чат) склеены не Kafka, а внутрипроцессной шиной `internal/inproc` (реализует `kafkax.Sender`: `Send` публикует сообщение подписчикам топика в отдельных горутинах). БД — SQLite (`ncruces/go-sqlite3`, миграции `migrations_sqlite`; UUID генерятся в `BeforeCreate`-хуках — в SQLite нет `gen_random_uuid()`). core-HTTP слушает `127.0.0.1:18085` (для `coreclient` агента), чат UI + WebSocket — `127.0.0.1:18086`. LLM — напрямую в OpenRouter (нужен VPN на телефоне, иначе РФ режет). MCP-инструменты агента (`tasks_*`/`pomodoro_*`) указывают на локальные приложения tasks/pomodoro на телефоне, если они запущены; если нет — агент работает без них.

`android/` — Kotlin-обёртка (WebView + меню настроек: ключ OpenRouter, модель, MCP-адреса). Сборка: `deploy\build-bot-android.ps1`.

Так на телефоне доступны и проактивные напоминания (планировщик/поллеры core), и чат с агентом, который видит локальные задачи и помидоры. Данные телефонного бота отдельны от VM-бота (синхронизацию можно добавить тем же приёмом, что в tasks/pomodoro).

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

## Снимок дня и понимание задач

`GET /internal/v1/day/{chat_id}` отдаёт компактный снимок дня в таймзоне профиля: задачи дня из tasks и сводку плана помидоров из pomodoro (всего слотов, пройдено, засчитано с учётом долей досрочных помидоров, слоты по задачам). У каждой задачи вид:

- `event` — фиксированное время, `requires_pomodoro=false` (сборы, зал, дорога, обед): просто расписание;
- `window` — фиксированное время с помидорами («Работа»): часть помидоров окна отдана ей;
- `effort` — задача на объём по `effort_minutes`, с прогрессом в минутах фокуса.

Рядом флаги `reschedulable` и вид повторения. Снимок строит `logic.BuildDayTasks` и `logic.SummarizePomodoroPlan`, собирает его `app.DaySnapshot`.

Снимок используют три места. Агент добавляет его в системный промпт каждого диалога разделом «Сегодня», поэтому на «что у меня осталось» отвечает по живым данным, а не угадывает. Утренний план получает его в `context.day`. Вечерний итог получает его без событий, чтобы в итог не попадали зал и обед.

Правила, которые держит код, а не только промпт:

- для событий без помидоров уходит одно напоминание «скоро начнётся», без «начинается сейчас» и без «помидор не запущен»; если в этот момент идёт предыдущее такое событие (сборы → зал → дорога), напоминание не шлётся;
- нуджи «давно нет помидоров» и «залипаешь в телефоне» молчат, пока идёт событие без помидоров;
- `BOT_MEETING_MAX_DURATION_MINUTES` на VPS поднят до 600, иначе рабочее окно 10:00–19:00 выпадало бы из напоминаний.

Промпты запрещают предлагать перенос задач с `reschedulable=false`: ежедневная задача завтра придёт сама, перенос бывает только у повторений и разовых задач. Этого же правила держится бэкенд tasks, где `reschedule-missed` для непереносимой задачи ничего не делает.
