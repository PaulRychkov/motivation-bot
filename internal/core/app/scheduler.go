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
	switch kind {
	case models.KindMorningPlan:
		reqCtx := map[string]any{"kind_hint": "утренний план дня"}
		occs, err := a.Tasks.Occurrences(ctx, localDate, localDate, "pending")
		if err != nil {
			a.Log.Warn("tasks недоступен для утреннего контекста", zap.Error(err))
		} else {
			reqCtx["today_occurrences"] = occs
		}
		slots, err := a.Pomo.DayPlan(ctx)
		if err != nil {
			a.Log.Warn("pomodoro недоступен для утреннего контекста", zap.Error(err))
		} else {
			reqCtx["pomodoro_slots"] = slots
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
	occs, err := a.Tasks.Occurrences(ctx, localDate, localDate, "")
	if err != nil {
		a.Log.Warn("tasks недоступен для вечернего итога", zap.Error(err))
		return logic.EveningSummary{}
	}
	items := make([]logic.PlanItem, 0, len(occs))
	completed := map[string]bool{}
	skipped := map[string]bool{}
	for _, o := range occs {
		taskID, _ := o["task_id"].(string)
		if taskID == "" {
			continue
		}
		title := ""
		if task, ok := o["task"].(map[string]any); ok {
			title, _ = task["title"].(string)
		}
		occID, _ := o["id"].(string)
		items = append(items, logic.PlanItem{TaskID: taskID, OccurrenceID: occID, Title: title})
		switch o["status"] {
		case "completed":
			completed[taskID] = true
		case "skipped":
			skipped[taskID] = true
		}
	}
	return logic.BuildEveningSummary(items, completed, skipped)
}

