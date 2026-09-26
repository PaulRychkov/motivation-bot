package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/PaulRychkov/motivation-bot/internal/core/logic"
	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

func (a *App) deferOnReply(tx *gorm.DB, p models.ChatProfile, content *string) error {
	if content == nil {
		return nil
	}
	deferral, ok := logic.ParseDeferral(*content)
	if !ok {
		return nil
	}
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().UTC()
	var iv models.Intervention
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("chat_profile_id = ? AND kind = ?", p.ID, models.KindDeadlineReminder).
		Where("task_external_id IS NOT NULL AND target_date IS NOT NULL").
		Where("sent_at > ?", now.Add(-3*time.Hour)).
		Order("sent_at DESC").First(&iv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("load reminder to defer: %w", err)
	}
	resume := resumeAt(now, loc, deferral)
	updates := map[string]any{
		"snooze_until": resume,
		"outcome":      models.OutcomeRescheduled,
		"outcome_at":   now,
		"updated_at":   now,
	}
	if err := tx.Model(&models.Intervention{}).Where("id = ?", iv.ID).Updates(updates).Error; err != nil {
		return fmt.Errorf("defer reminder: %w", err)
	}
	a.Log.Info("напоминание отложено по ответу пользователя", zap.Time("resume_at", resume))
	return nil
}

func resumeAt(now time.Time, loc *time.Location, d logic.Deferral) time.Time {
	local := now.In(loc)
	target := d.ResumeAt(local.Hour()*60 + local.Minute())
	day := local
	if target >= 24*60 {
		target -= 24 * 60
		day = day.AddDate(0, 0, 1)
	}
	resume := time.Date(day.Year(), day.Month(), day.Day(), target/60, target%60, 0, 0, loc)
	if !resume.After(local) {
		resume = resume.AddDate(0, 0, 1)
	}
	return resume.UTC()
}

func (a *App) taskSnoozed(ctx context.Context, p models.ChatProfile, taskID, localDate string, now time.Time) (snoozed bool, released bool) {
	var iv models.Intervention
	err := a.DB.WithContext(ctx).
		Where("chat_profile_id = ? AND task_external_id = ? AND target_date = ? AND snooze_until IS NOT NULL",
			p.ID, taskID, localDate).
		Order("snooze_until DESC").First(&iv).Error
	if err != nil || iv.SnoozeUntil == nil {
		return false, false
	}
	if now.Before(*iv.SnoozeUntil) {
		return true, false
	}
	a.releaseSnooze(ctx, p, taskID, localDate, now)
	return false, true
}

func (a *App) releaseSnooze(ctx context.Context, p models.ChatProfile, taskID, localDate string, now time.Time) {
	if err := a.DB.WithContext(ctx).Model(&models.Intervention{}).
		Where("chat_profile_id = ? AND task_external_id = ? AND target_date = ? AND snooze_until IS NOT NULL",
			p.ID, taskID, localDate).
		Updates(map[string]any{"snooze_until": nil, "updated_at": now}).Error; err != nil {
		a.Log.Warn("снятие отсрочки", zap.Error(err))
	}
}
