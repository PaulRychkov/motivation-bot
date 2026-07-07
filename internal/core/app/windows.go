package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

const resendAfter = 3 * time.Minute

func (a *App) RunWindowWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(a.Cfg.WindowWorkerSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.closeExpiredWindows(ctx)
			a.resendStale(ctx)
		}
	}
}

func (a *App) closeExpiredWindows(ctx context.Context) {
	now := time.Now().UTC()
	res := a.DB.WithContext(ctx).Model(&models.Intervention{}).
		Where("outcome = ? AND window_ends_at < ? AND evidence = '[]'::jsonb", models.OutcomePending, now).
		Updates(map[string]any{"outcome": models.OutcomeIgnored, "outcome_at": now, "updated_at": now})
	if res.Error != nil {
		a.Log.Error("закрытие просроченных окон", zap.Error(res.Error))
		return
	}
	if res.RowsAffected > 0 {
		a.Log.Info("окна закрыты как ignored", zap.Int64("count", res.RowsAffected))
	}
	res = a.DB.WithContext(ctx).Model(&models.Intervention{}).
		Where("outcome = ? AND window_ends_at < ? AND evidence <> '[]'::jsonb", models.OutcomePending, now).
		Updates(map[string]any{"outcome": models.OutcomePartial, "outcome_at": now, "updated_at": now})
	if res.Error != nil {
		a.Log.Error("закрытие окон с evidence", zap.Error(res.Error))
	}
}

func (a *App) resendStale(ctx context.Context) {
	now := time.Now().UTC()
	var stale []models.Intervention
	err := a.DB.WithContext(ctx).
		Where("telegram_message_id IS NULL AND outcome = ?", models.OutcomePending).
		Where("sent_at < ? AND window_ends_at > ?", now.Add(-resendAfter), now).
		Find(&stale).Error
	if err != nil {
		a.Log.Error("выборка неотправленных интервенций", zap.Error(err))
		return
	}
	staleIDs := make(map[uuid.UUID]bool, len(stale))
	for _, iv := range stale {
		staleIDs[iv.ID] = true
	}
	a.pushMu.Lock()
	for id := range a.lastPush {
		if !staleIDs[id] {
			delete(a.lastPush, id)
		}
	}
	a.pushMu.Unlock()
	for _, iv := range stale {
		a.pushMu.Lock()
		last, ok := a.lastPush[iv.ID]
		if ok && now.Sub(last) < resendAfter {
			a.pushMu.Unlock()
			continue
		}
		a.lastPush[iv.ID] = now
		a.pushMu.Unlock()
		if err := a.sendIntervention(ctx, iv); err != nil {
			a.Log.Warn("повторная отправка интервенции", zap.Error(err))
		}
	}
}

func (a *App) RunRetention(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.retentionTick(ctx)
		}
	}
}

func (a *App) retentionTick(ctx context.Context) {
	res := a.DB.WithContext(ctx).Exec(`
		DELETE FROM dialog_messages WHERE id IN (
			SELECT id FROM (
				SELECT id, created_at,
					row_number() OVER (PARTITION BY chat_profile_id ORDER BY created_at DESC) AS rn
				FROM dialog_messages
			) ranked
			WHERE ranked.rn > 200 AND ranked.created_at < now() - interval '30 days'
		)`)
	if res.Error != nil {
		a.Log.Error("retention диалога", zap.Error(res.Error))
	} else if res.RowsAffected > 0 {
		a.Log.Info("диалог обрезан", zap.Int64("rows", res.RowsAffected))
	}
	res = a.DB.WithContext(ctx).Exec(
		`DELETE FROM inbox_events WHERE processed_at IS NOT NULL AND processed_at < now() - interval '90 days'`)
	if res.Error != nil {
		a.Log.Error("retention инбокса", zap.Error(res.Error))
	} else if res.RowsAffected > 0 {
		a.Log.Info("инбокс очищен", zap.Int64("rows", res.RowsAffected))
	}
}
