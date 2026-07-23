package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/core/logic"
	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

func (a *App) RunProactivePoller(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(a.Cfg.ProactivePollSec) * time.Second)
	defer ticker.Stop()
	a.proactiveTick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.proactiveTick(ctx)
		}
	}
}

func (a *App) proactiveTick(ctx context.Context) {
	var profiles []models.ChatProfile
	if err := a.DB.WithContext(ctx).Find(&profiles).Error; err != nil {
		return
	}
	if len(profiles) == 0 {
		return
	}
	tasks, err := a.Tasks.Tasks(ctx)
	if err != nil {
		a.Log.Warn("проактивный опрос: tasks недоступен", zap.Error(err))
		return
	}
	byID := make(map[string]map[string]any, len(tasks))
	for _, t := range tasks {
		if id, _ := t["id"].(string); id != "" {
			byID[id] = t
		}
	}

	now := time.Now().UTC()
	for _, p := range profiles {
		loc, err := time.LoadLocation(p.Timezone)
		if err != nil {
			continue
		}
		localDate := logic.LocalDate(now, loc)
		localMin := logic.LocalMinutes(now, loc)
		if logic.IsPaused(p.PausedUntil, localDate) || logic.InQuietHours(p.QuietStartMin, p.QuietEndMin, localMin) {
			continue
		}
		occs, err := a.Tasks.Occurrences(ctx, localDate, localDate, "pending")
		if err != nil {
			a.Log.Warn("проактивный опрос: occurrences недоступны", zap.Error(err))
			continue
		}
		a.remindMeetingsSoon(ctx, p, loc, now, byID, occs, localMin, localDate)
		a.maybeWorkNudge(ctx, p, loc, now, byID, occs, localDate, localMin)
		a.maybeStallNudge(ctx, p, loc, now, byID, occs, localDate, localMin)
	}
}

func (a *App) remindMeetingsSoon(ctx context.Context, p models.ChatProfile, loc *time.Location, now time.Time, byID map[string]map[string]any, occs []map[string]any, localMin int, localDate string) {
	lead := a.Cfg.MeetingLeadMin
	if lead <= 0 {
		lead = 15
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
		maxDur := a.Cfg.MeetingMaxDurMin
		if maxDur <= 0 {
			maxDur = 240
		}
		if durRaw, ok := task["estimated_duration_minutes"].(float64); ok && int(durRaw) > maxDur {
			continue
		}
		start := int(startRaw)
		if localMin < start-lead || localMin > start {
			continue
		}
		title, _ := task["title"].(string)
		a.createMeetingReminder(ctx, p, loc, now, taskID, title, localDate, start, start-localMin)
	}
}

func (a *App) createMeetingReminder(ctx context.Context, p models.ChatProfile, loc *time.Location, now time.Time, taskID, title, localDate string, startMin, minutesUntil int) {
	var cnt int64
	a.DB.WithContext(ctx).Model(&models.Intervention{}).
		Where("chat_profile_id = ? AND kind = ? AND task_source = ? AND task_external_id = ? AND target_date = ?",
			p.ID, models.KindDeadlineReminder, models.SourceTasks, taskID, localDate).Count(&cnt)
	if cnt > 0 {
		return
	}
	src := models.SourceTasks
	tid := taskID
	td := localDate
	if _, err := a.createIntervention(ctx, p, loc, now, createParams{
		Kind:           models.KindDeadlineReminder,
		TriggeredBy:    models.TriggerSchedule,
		TaskSource:     &src,
		TaskExternalID: &tid,
		TargetDate:     &td,
		Context: map[string]any{
			"reason":        "meeting_soon",
			"task_title":    title,
			"start_time":    fmt.Sprintf("%02d:%02d", startMin/60, startMin%60),
			"minutes_until": minutesUntil,
		},
	}); err != nil {
		a.Log.Error("напоминание о митинге", zap.Error(err))
		return
	}
	a.Log.Info("напоминание о митинге", zap.String("task", title), zap.Int("через_мин", minutesUntil))
}

func (a *App) maybeWorkNudge(ctx context.Context, p models.ChatProfile, loc *time.Location, now time.Time, byID map[string]map[string]any, occs []map[string]any, localDate string, localMin int) {
	if logic.InDailyWindow(localMin, a.Cfg.PhoneRestStartMin, a.Cfg.PhoneRestEndMin) {
		return
	}

	pending, titles := pendingWork(byID, occs)
	if pending == 0 {
		return
	}

	gap := a.Cfg.WorkNudgePomodoroGapMin
	if gap <= 0 {
		gap = 90
	}
	last := a.lastPomodoroAt(ctx)
	if !last.IsZero() && now.Sub(last) < time.Duration(gap)*time.Minute {
		return
	}

	activity, err := a.PhoneActivity(ctx, p.ChatID)
	if err != nil {
		return
	}
	phoneActive := activity.UpdatedAt != nil && now.Sub(*activity.UpdatedAt) < 25*time.Minute &&
		(activity.NowDistractingMinutes > 0 || activity.LastHourDistractingMinutes >= a.Cfg.WorkNudgePhoneMin)
	if !phoneActive {
		return
	}

	if !a.nudgeCooldownPassed(ctx, p.ChatID, localDate, now, a.Cfg.WorkNudgeCooldownMin, 90) {
		return
	}

	reqCtx := map[string]any{
		"reason":              "work_nudge",
		"pending_count":       pending,
		"pending_titles":      titles,
		"distracting_minutes": activity.LastHourDistractingMinutes,
		"top_apps":            activity.TopApps,
	}
	iv, err := a.createIntervention(ctx, p, loc, now, createParams{
		Kind:        models.KindQuestion,
		TriggeredBy: models.TriggerSchedule,
		Context:     reqCtx,
	})
	if err != nil {
		a.Log.Error("нудж по работе", zap.Error(err))
		return
	}
	if iv == nil {
		return
	}
	a.setNudgeAt(ctx, workNudgeKey(p.ChatID), localDate, now)
	a.Log.Info("нудж по работе",
		zap.Int("дел", pending), zap.Int("отвлечение_за_час", activity.LastHourDistractingMinutes))
}

func (a *App) maybeStallNudge(ctx context.Context, p models.ChatProfile, loc *time.Location, now time.Time, byID map[string]map[string]any, occs []map[string]any, localDate string, localMin int) {
	if logic.InDailyWindow(localMin, a.Cfg.PhoneRestStartMin, a.Cfg.PhoneRestEndMin) {
		return
	}
	gap := a.Cfg.StallGapMin
	if gap <= 0 {
		gap = 120
	}
	if localMin < p.MorningPlanMin+gap {
		return
	}
	pending, titles := pendingWork(byID, occs)
	if pending == 0 {
		return
	}
	last := a.lastPomodoroAt(ctx)
	if !last.IsZero() && now.Sub(last) < time.Duration(gap)*time.Minute {
		return
	}
	if !a.nudgeCooldownPassed(ctx, p.ChatID, localDate, now, a.Cfg.StallCooldownMin, 150) {
		return
	}
	stallMin := localMin - p.MorningPlanMin
	if !last.IsZero() && int(now.Sub(last).Minutes()) < stallMin {
		stallMin = int(now.Sub(last).Minutes())
	}
	reqCtx := map[string]any{
		"reason":         "stall_nudge",
		"pending_count":  pending,
		"pending_titles": titles,
		"stall_minutes":  stallMin,
	}
	iv, err := a.createIntervention(ctx, p, loc, now, createParams{
		Kind:        models.KindQuestion,
		TriggeredBy: models.TriggerSchedule,
		Context:     reqCtx,
	})
	if err != nil {
		a.Log.Error("нудж по простою", zap.Error(err))
		return
	}
	if iv == nil {
		return
	}
	a.setNudgeAt(ctx, stallNudgeKey(p.ChatID), localDate, now)
	a.Log.Info("нудж по простою", zap.Int("дел", pending), zap.Int("минут_без_помидоров", stallMin))
}

func pendingWork(byID map[string]map[string]any, occs []map[string]any) (int, []string) {
	count := 0
	titles := make([]string, 0, 3)
	for _, occ := range occs {
		taskID, _ := occ["task_id"].(string)
		task, ok := byID[taskID]
		if !ok || !taskOpen(task) {
			continue
		}
		if _, isEvent := task["start_time_minutes"].(float64); isEvent {
			continue
		}
		count++
		if title, _ := task["title"].(string); title != "" && len(titles) < 3 {
			titles = append(titles, title)
		}
	}
	return count, titles
}

func (a *App) lastPomodoroAt(ctx context.Context) time.Time {
	var ev models.InboxEvent
	err := a.DB.WithContext(ctx).
		Where("source = ?", models.SourcePomodoro).
		Order("occurred_at DESC").First(&ev).Error
	if err != nil {
		return time.Time{}
	}
	return ev.OccurredAt
}

func (a *App) nudgeCooldownPassed(ctx context.Context, chatID int64, localDate string, now time.Time, cooldownMin, defMin int) bool {
	cooldown := cooldownMin
	if cooldown <= 0 {
		cooldown = defMin
	}
	last := a.lastNudgeAt(ctx, chatID, localDate)
	return last.IsZero() || now.Sub(last) >= time.Duration(cooldown)*time.Minute
}

func (a *App) lastNudgeAt(ctx context.Context, chatID int64, localDate string) time.Time {
	var latest time.Time
	for _, key := range []string{workNudgeKey(chatID), stallNudgeKey(chatID)} {
		var note models.AgentNote
		if err := a.DB.WithContext(ctx).Where("key = ?", key).First(&note).Error; err != nil {
			continue
		}
		var v struct {
			Date string    `json:"date"`
			At   time.Time `json:"at"`
		}
		if json.Unmarshal(note.Value, &v) != nil || v.Date != localDate {
			continue
		}
		if v.At.After(latest) {
			latest = v.At
		}
	}
	return latest
}

func (a *App) setNudgeAt(ctx context.Context, key, localDate string, now time.Time) {
	b, err := json.Marshal(map[string]any{"date": localDate, "at": now})
	if err != nil {
		return
	}
	if err := a.UpsertNote(ctx, key, b, nil); err != nil {
		a.Log.Warn("сохранение времени нуджа", zap.Error(err))
	}
}

func workNudgeKey(chatID int64) string {
	return fmt.Sprintf("work_nudge:%d", chatID)
}

func stallNudgeKey(chatID int64) string {
	return fmt.Sprintf("stall_nudge:%d", chatID)
}
