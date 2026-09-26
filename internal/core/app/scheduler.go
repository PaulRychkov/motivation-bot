package app

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/core/logic"
	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

func (a *App) RunScheduler(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(a.Cfg.SchedulerSec) * time.Second)
	defer ticker.Stop()
	a.schedulerTick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.schedulerTick(ctx)
		}
	}
}

func (a *App) schedulerTick(ctx context.Context) {
	var profiles []models.ChatProfile
	if err := a.DB.WithContext(ctx).Find(&profiles).Error; err != nil {
		a.Log.Error("выборка профилей", zap.Error(err))
		return
	}
	now := time.Now().UTC()
	for _, p := range profiles {
		loc, err := time.LoadLocation(p.Timezone)
		if err != nil {
			a.Log.Warn("невалидная таймзона профиля", zap.String("tz", p.Timezone), zap.Error(err))
			continue
		}
		localMin := logic.LocalMinutes(now, loc)
		localDate := logic.LocalDate(now, loc)
		if logic.IsPaused(p.PausedUntil, localDate) {
			continue
		}
		if logic.InQuietHours(p.QuietStartMin, p.QuietEndMin, localMin) {
			continue
		}
		due := logic.DueScheduled(p.MorningPlanMin, p.EveningReviewMin, localMin, localDate)
		missing := logic.MissingScheduled(due, func(kind, ld string) bool {
			var cnt int64
			a.DB.WithContext(ctx).Model(&models.Intervention{}).
				Where("chat_profile_id = ? AND kind = ? AND local_date = ?", p.ID, kind, ld).
				Count(&cnt)
			return cnt > 0
		})
		for _, d := range missing {
			reqCtx := a.scheduledContext(ctx, p, d.Kind, localDate, loc)
			if _, err := a.createIntervention(ctx, p, loc, now, createParams{
				Kind:        d.Kind,
				TriggeredBy: models.TriggerSchedule,
				Context:     reqCtx,
			}); err != nil {
				a.Log.Error("создание плановой интервенции", zap.String("kind", d.Kind), zap.Error(err))
			}
		}
	}
}

func (a *App) scheduledContext(ctx context.Context, p models.ChatProfile, kind, localDate string, loc *time.Location) map[string]any {
	hint := map[string]string{
		models.KindMorningPlan:   "утренний план дня",
		models.KindEveningReview: "вечерний итог дня",
	}[kind]
	if hint == "" {
		return map[string]any{}
	}
	reqCtx := map[string]any{"kind_hint": hint}
	snap, err := a.daySnapshot(ctx, loc)
	if err != nil {
		a.Log.Warn("снимок дня для плановой интервенции", zap.String("kind", kind), zap.Error(err))
	}
	if kind == models.KindEveningReview {
		snap.Tasks = logic.WithoutEvents(snap.Tasks)
	}
	reqCtx["day"] = snap
	return reqCtx
}
