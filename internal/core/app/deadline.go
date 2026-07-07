package app

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/core/logic"
	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

func (a *App) RunDeadlinePoller(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(a.Cfg.DeadlinePollSec) * time.Second)
	defer ticker.Stop()
	a.deadlineTick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.deadlineTick(ctx)
		}
	}
}

func (a *App) deadlineTick(ctx context.Context) {
	var profiles []models.ChatProfile
	if err := a.DB.WithContext(ctx).Find(&profiles).Error; err != nil {
		a.Log.Error("выборка профилей", zap.Error(err))
		return
	}
	if len(profiles) == 0 {
		return
	}
	tasks, err := a.Tasks.Tasks(ctx)
	if err != nil {
		a.Log.Warn("tasks REST недоступен, пропуск опроса дедлайнов", zap.Error(err))
		return
	}
	now := time.Now().UTC()
	for _, p := range profiles {
		loc, err := time.LoadLocation(p.Timezone)
		if err != nil {
			continue
		}
		localDate := logic.LocalDate(now, loc)
		localMin := logic.LocalMinutes(now, loc)
		if logic.IsPaused(p.PausedUntil, localDate) {
			continue
		}
		if logic.InQuietHours(p.QuietStartMin, p.QuietEndMin, localMin) {
			continue
		}
		tomorrow := nextDate(localDate)
		for _, t := range tasks {
			a.maybeRemindDue(ctx, p, loc, now, t, tomorrow)
		}
		a.maybeRemindTodayOccurrences(ctx, p, loc, now, tasks, localDate, localMin)
	}
}

func (a *App) maybeRemindDue(ctx context.Context, p models.ChatProfile, loc *time.Location, now time.Time, task map[string]any, tomorrow string) {
	due, _ := task["due"].(string)
	if len(due) < 10 {
		return
	}
	due = due[:10]
	if due > tomorrow {
		return
	}
	if !taskOpen(task) {
		return
	}
	taskID, _ := task["id"].(string)
	if taskID == "" {
		return
	}
	title, _ := task["title"].(string)
	a.createDeadlineReminder(ctx, p, loc, now, taskID, title, due, "дедлайн задачи")
}

func (a *App) maybeRemindTodayOccurrences(ctx context.Context, p models.ChatProfile, loc *time.Location, now time.Time, tasks []map[string]any, localDate string, localMin int) {
	occs, err := a.Tasks.Occurrences(ctx, localDate, localDate, "pending")
	if err != nil {
		a.Log.Warn("tasks occurrences недоступны", zap.Error(err))
		return
	}
	byID := map[string]map[string]any{}
	for _, t := range tasks {
		if id, _ := t["id"].(string); id != "" {
			byID[id] = t
		}
	}
	for _, occ := range occs {
		taskID, _ := occ["task_id"].(string)
		task, ok := byID[taskID]
		if !ok || !taskOpen(task) {
			continue
		}
		startRaw, ok := task["start_time_minutes"].(float64)
		if !ok {
			continue
		}
		if localMin <= int(startRaw)+60 {
			continue
		}
		title, _ := task["title"].(string)
		a.createDeadlineReminder(ctx, p, loc, now, taskID, title, localDate, "запланированное на сегодня вхождение просрочено")
	}
}

func (a *App) createDeadlineReminder(ctx context.Context, p models.ChatProfile, loc *time.Location, now time.Time, taskID, title, targetDate, reason string) {
	var cnt int64
	a.DB.WithContext(ctx).Model(&models.Intervention{}).
		Where("chat_profile_id = ? AND kind = ? AND task_source = ? AND task_external_id = ? AND target_date = ?",
			p.ID, models.KindDeadlineReminder, models.SourceTasks, taskID, targetDate).
		Count(&cnt)
	if cnt > 0 {
		return
	}
	src := models.SourceTasks
	tid := taskID
	td := targetDate
	if _, err := a.createIntervention(ctx, p, loc, now, createParams{
		Kind:           models.KindDeadlineReminder,
		TriggeredBy:    models.TriggerSchedule,
		TaskSource:     &src,
		TaskExternalID: &tid,
		TargetDate:     &td,
		Context: map[string]any{
			"task_title":  title,
			"target_date": targetDate,
			"reason":      reason,
		},
	}); err != nil {
		a.Log.Error("создание deadline_reminder", zap.Error(err))
	}
}

func (a *App) isSuppressed(ctx context.Context, taskID, date string) bool {
	var cnt int64
	a.DB.WithContext(ctx).Model(&models.InboxEvent{}).
		Where("source = ? AND type = ? AND payload->>'task_id' = ? AND payload->>'date' = ?",
			models.SourceTasks, "occurrence.skipped", taskID, date).
		Count(&cnt)
	return cnt > 0
}

func taskOpen(task map[string]any) bool {
	if active, ok := task["is_active"].(bool); ok && !active {
		return false
	}
	progress, _ := task["progress"].(string)
	return progress == "" || progress == "needs_action" || progress == "in_process"
}

func nextDate(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, 1).Format("2006-01-02")
}
