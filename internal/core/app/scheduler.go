package app

import (
	"context"
	"encoding/json"
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
	switch kind {
	case models.KindMorningPlan:
		reqCtx := map[string]any{"kind_hint": "утренний план дня"}
		occs, err := a.Tasks.Occurrences(ctx, localDate, localDate, "pending")
		if err != nil {
			a.Log.Warn("tasks недоступен для утреннего контекста", zap.Error(err))
		} else {
			reqCtx["today_occurrences"] = occs
		}
		return reqCtx
	case models.KindEveningReview:
		summary := a.eveningSummary(ctx, localDate)
		return map[string]any{
			"kind_hint": "вечерний итог дня",
			"summary":   summary,
		}
	}
	return map[string]any{}
}

func (a *App) eveningSummary(ctx context.Context, localDate string) logic.EveningSummary {
	items := a.planItemsForDate(ctx, localDate)
	completed := map[string]bool{}
	skipped := map[string]bool{}
	var events []models.InboxEvent
	err := a.DB.WithContext(ctx).
		Where("source = ? AND type IN ? AND payload->>'date' = ?",
			models.SourceTasks, []string{"occurrence.completed", "occurrence.skipped"}, localDate).
		Find(&events).Error
	if err != nil {
		a.Log.Warn("выборка дневных событий для итога", zap.Error(err))
	}
	for _, ev := range events {
		var payload map[string]any
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			continue
		}
		taskID, _ := payload["task_id"].(string)
		if taskID == "" {
			continue
		}
		switch ev.Type {
		case "occurrence.completed":
			completed[taskID] = true
		case "occurrence.skipped":
			skipped[taskID] = true
		}
	}
	return logic.BuildEveningSummary(items, completed, skipped)
}

func (a *App) planItemsForDate(ctx context.Context, localDate string) []logic.PlanItem {
	var plan models.InboxEvent
	err := a.DB.WithContext(ctx).
		Where("source = ? AND type = ? AND payload->>'date' = ?", models.SourceTasks, "plan.committed", localDate).
		Order("occurred_at DESC").First(&plan).Error
	if err != nil {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(plan.Payload, &payload); err != nil {
		return nil
	}
	return logic.ParsePlanItems(payload)
}
