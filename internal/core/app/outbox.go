package app

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/cloudevents"
	"github.com/PaulRychkov/motivation-bot/internal/core/models"
	"github.com/PaulRychkov/motivation-bot/internal/kafkax"
)

func (a *App) RunOutboxRelay(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(a.Cfg.OutboxRelaySec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.relayTick(ctx)
		}
	}
}

func (a *App) relayTick(ctx context.Context) {
	var events []models.OutboxEvent
	err := a.DB.WithContext(ctx).
		Where("published_at IS NULL").
		Order("created_at").Limit(100).
		Find(&events).Error
	if err != nil {
		a.Log.Error("выборка outbox", zap.Error(err))
		return
	}
	for _, ev := range events {
		env := cloudevents.New(ev.ID.String(), models.SourceBot, ev.EventType, ev.AggregateID.String(), ev.CreatedAt, json.RawMessage(ev.Payload))
		b, err := json.Marshal(env)
		if err != nil {
			a.Log.Error("marshal outbox envelope", zap.Error(err))
			continue
		}
		if err := a.Prod.Send(kafkax.TopicBotEvents, ev.AggregateID.String(), b); err != nil {
			if uerr := a.DB.WithContext(ctx).Model(&models.OutboxEvent{}).Where("id = ?", ev.ID).
				Updates(map[string]any{"attempts": ev.Attempts + 1, "last_error": err.Error()}).Error; uerr != nil {
				a.Log.Error("отметка ошибки outbox", zap.Error(uerr))
			}
			a.Log.Warn("публикация outbox не удалась", zap.Error(err))
			return
		}
		now := time.Now().UTC()
		if err := a.DB.WithContext(ctx).Model(&models.OutboxEvent{}).Where("id = ?", ev.ID).
			Updates(map[string]any{"published_at": now, "attempts": ev.Attempts + 1}).Error; err != nil {
			a.Log.Error("отметка published_at", zap.Error(err))
			return
		}
	}
}
