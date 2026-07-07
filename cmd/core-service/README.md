# core-service

Ядро бота и единственный владелец БД bot (PostgreSQL, порт 5435). Порт HTTP — 8083.

## Что делает

- **Миграции** — golang-migrate из `migrations/` при старте (chat_profiles, interventions, inbox_events, dialog_messages, agent_notes, events_outbox).
- **Планировщик** — morning_plan / evening_review по временам профиля в его таймзоне; антидубль через partial UNIQUE (chat_profile_id, kind, local_date); тихие часы (в т.ч. через полночь) и paused_until откладывают отправку.
- **Дедлайны (pull)** — опрос tasks REST: задачи с due сегодня/завтра + сегодняшние pending-вхождения с прошедшим start_time; подавление по occurrence.skipped из инбокса.
- **Инбокс + диспетчер** — консюмер tasks.events / pomodoro.events → inbox_events (идемпотентно по UNIQUE(source, event_id)) → единый диспетчер прогоняет событие через матчер активаций (правила data-model §4) в одной транзакции.
- **Воркеры** — закрытие просроченных окон (~5 мин, pending → ignored при пустом evidence), повторная отправка неотправленных интервенций, retention (диалог 30 дней / 200 последних, инбокс 90 дней), outbox-релей → `bot.events`.
- **Тексты интервенций** — запрос агенту через `bot.agent-requests` {correlation_id, kind, context}; ответ `bot.agent-responses` {text, tone} → отправка в `bot.tg-outgoing`; подтверждение `bot.tg-sent` фиксирует telegram_message_id и эмитит intervention.sent.

## REST API

Публичные (`/api/v1`):
- `GET /healthz`
- `GET /api/v1/profiles/{chat_id}` — профиль
- `PATCH /api/v1/profiles/{chat_id}` — частичное обновление (timezone, morning_plan_min, evening_review_min, quiet_start_min+quiet_end_min парой, free_ping_daily_budget, default_tone, persona, paused_until)
- `GET /api/v1/interventions?chat_id=&limit=` — список интервенций

Внутренние для agent-service (`/internal/v1`):
- `POST /profiles/ensure` {chat_id} → {profile, created} — онбординг
- `GET /dialog/{chat_id}?limit=` / `POST /dialog/{chat_id}` — история и добавление сообщений диалога (реплика пользователя активирует открытые evening_review/question)
- `GET /interventions/pending?chat_id=`
- `POST /interventions` {chat_id, kind: free_ping|question, task_source?, task_external_id?, target_date?, context?} — агент инициирует касание; ответ {status: created|suppressed, intervention?}; free_ping тратит дневной бюджет, оба вида уважают тихие часы, paused_until и подавление по occurrence.skipped
- `POST /interventions/{id}/outcome` {outcome: activated|partial|ignored|refused|rescheduled}
- `GET /notes` / `PUT /notes/{key}` {value, expires_at}
- `GET /stats/effectiveness?chat_id=` — счётчики kind × outcome

Ошибки: `{"error":{"code":"...","message":"..."}}`.

## Запуск

```
go run ./cmd/core-service
```

Конфиг — env с префиксом BOT_ (см. корневой README). Тесты матчера и правил — `go test ./internal/core/...`.
