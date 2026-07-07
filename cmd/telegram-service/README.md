# telegram-service

Мост между Telegram Bot API и Kafka. Библиотека — `github.com/go-telegram/bot` (long polling). Своей БД и HTTP-порта нет.

## Потоки

- **Входящие**: текстовые сообщения → `bot.tg-updates` `{update_id, chat_id, message_id, text, username, date}` (ключ партиции — chat_id).
- **Исходящие**: консюмер `bot.tg-outgoing` `{correlation_id, chat_id, text, reply_to_message_id?}` → sendMessage (reply_parameters при наличии reply_to; если reply не удался — повтор без reply).
- **Подтверждения**: результат отправки → `bot.tg-sent` `{correlation_id, chat_id, message_id}` — по нему core-service фиксирует telegram_message_id и эмитит intervention.sent.

## Запуск

```
go run ./cmd/telegram-service
```

Нужны: Kafka и `TELEGRAM_BOT_TOKEN` в `.env` (без токена сервис завершается с ошибкой). Consumer group — `bot-telegram-group`.
