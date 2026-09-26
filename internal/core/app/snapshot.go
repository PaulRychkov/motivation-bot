package app

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/core/logic"
	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

func (a *App) DaySnapshot(ctx context.Context, chatID int64) (models.DaySnapshot, error) {
	p, err := a.ProfileByChatID(ctx, chatID)
	if err != nil {
		return models.DaySnapshot{}, err
	}
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return models.DaySnapshot{}, fmt.Errorf("таймзона профиля %q: %w", p.Timezone, err)
	}
	return a.daySnapshot(ctx, loc)
}

func (a *App) daySnapshot(ctx context.Context, loc *time.Location) (models.DaySnapshot, error) {
	now := time.Now().In(loc)
	localDate := now.Format("2006-01-02")
	snap := models.DaySnapshot{Date: localDate, Now: now.Format("15:04"), Tasks: []models.DayTask{}}
	occs, err := a.Tasks.Occurrences(ctx, localDate, localDate, "")
	if err != nil {
		return snap, fmt.Errorf("задачи дня: %w", err)
	}
	snap.Tasks = logic.BuildDayTasks(occs)
	if a.Pomo == nil {
		return snap, nil
	}
	slots, err := a.Pomo.DayPlan(ctx)
	if err != nil {
		a.Log.Warn("план помидоров недоступен для снимка дня", zap.Error(err))
		return snap, nil
	}
	credit, phase := 0.0, ""
	if st, err := a.Pomo.State(ctx); err == nil && st != nil {
		credit, phase = st.CreditToday, st.Phase
	}
	snap.Pomodoro = logic.SummarizePomodoroPlan(slots, credit, phase)
	return snap, nil
}
