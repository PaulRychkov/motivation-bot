package logic

import (
	"testing"
	"time"

	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

func intp(v int) *int { return &v }

func TestInQuietHours(t *testing.T) {
	tests := []struct {
		name       string
		start, end *int
		localMin   int
		want       bool
	}{
		{"нет тихих часов", nil, nil, 120, false},
		{"обычный интервал внутри", intp(600), intp(720), 660, true},
		{"обычный интервал до начала", intp(600), intp(720), 599, false},
		{"обычный интервал граница начала", intp(600), intp(720), 600, true},
		{"обычный интервал граница конца", intp(600), intp(720), 720, false},
		{"через полночь поздний вечер", intp(1320), intp(420), 1380, true},
		{"через полночь раннее утро", intp(1320), intp(420), 180, true},
		{"через полночь граница конца", intp(1320), intp(420), 420, false},
		{"через полночь день", intp(1320), intp(420), 720, false},
		{"через полночь граница начала", intp(1320), intp(420), 1320, true},
		{"вырожденный интервал", intp(300), intp(300), 300, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InQuietHours(tt.start, tt.end, tt.localMin); got != tt.want {
				t.Errorf("InQuietHours(%v, %v, %d) = %v, ожидалось %v", tt.start, tt.end, tt.localMin, got, tt.want)
			}
		})
	}
}

func TestLocalDateBoundaries(t *testing.T) {
	yekat, err := time.LoadLocation("Asia/Yekaterinburg")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		utc  time.Time
		want string
	}{
		{"вечер UTC уже завтра в +05", time.Date(2026, 7, 6, 21, 30, 0, 0, time.UTC), "2026-07-07"},
		{"утро UTC тот же день", time.Date(2026, 7, 6, 9, 0, 0, 0, time.UTC), "2026-07-06"},
		{"18:59 UTC ещё сегодня в +05", time.Date(2026, 7, 6, 18, 59, 0, 0, time.UTC), "2026-07-06"},
		{"19:00 UTC уже завтра в +05", time.Date(2026, 7, 6, 19, 0, 0, 0, time.UTC), "2026-07-07"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LocalDate(tt.utc, yekat); got != tt.want {
				t.Errorf("LocalDate = %s, ожидалось %s", got, tt.want)
			}
		})
	}
}

func TestLocalDayBoundsUTC(t *testing.T) {
	yekat, err := time.LoadLocation("Asia/Yekaterinburg")
	if err != nil {
		t.Fatal(err)
	}
	start, end, err := LocalDayBoundsUTC(yekat, "2026-07-06")
	if err != nil {
		t.Fatal(err)
	}
	wantStart := time.Date(2026, 7, 5, 19, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 7, 6, 19, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Errorf("границы (%v, %v), ожидалось (%v, %v)", start, end, wantStart, wantEnd)
	}

	inside := time.Date(2026, 7, 6, 18, 59, 0, 0, time.UTC)
	outside := time.Date(2026, 7, 6, 19, 0, 0, 0, time.UTC)
	if !(inside.Compare(start) >= 0 && inside.Before(end)) {
		t.Error("момент внутри локальных суток не попал в границы")
	}
	if outside.Before(end) {
		t.Error("момент следующих суток попал в границы")
	}
}

func TestFreePingBudget(t *testing.T) {
	tests := []struct {
		used, budget int
		want         bool
	}{
		{0, 3, true},
		{2, 3, true},
		{3, 3, false},
		{4, 3, false},
		{0, 0, false},
	}
	for _, tt := range tests {
		if got := FreePingAllowed(tt.used, tt.budget); got != tt.want {
			t.Errorf("FreePingAllowed(%d, %d) = %v, ожидалось %v", tt.used, tt.budget, got, tt.want)
		}
	}
}

func TestIsPaused(t *testing.T) {
	d := models.NewDate(2026, 7, 6)
	tests := []struct {
		name        string
		pausedUntil *models.Date
		localDate   string
		want        bool
	}{
		{"нет паузы", nil, "2026-07-06", false},
		{"пауза до сегодня включительно", &d, "2026-07-06", true},
		{"пауза ещё действует", &d, "2026-07-01", true},
		{"пауза закончилась", &d, "2026-07-07", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPaused(tt.pausedUntil, tt.localDate); got != tt.want {
				t.Errorf("IsPaused = %v, ожидалось %v", got, tt.want)
			}
		})
	}
}

func TestWindowEnd(t *testing.T) {
	yekat, _ := time.LoadLocation("Asia/Yekaterinburg")
	now := time.Date(2026, 7, 6, 4, 0, 0, 0, time.UTC)
	day := "2026-07-06"
	target := "2026-07-07"

	morning := WindowEnd(models.KindMorningPlan, now, yekat, day, 1290, nil)
	wantMorning := time.Date(2026, 7, 6, 16, 30, 0, 0, time.UTC)
	if !morning.Equal(wantMorning) {
		t.Errorf("morning window = %v, ожидалось %v", morning, wantMorning)
	}

	evening := WindowEnd(models.KindEveningReview, now, yekat, day, 1290, nil)
	wantEvening := time.Date(2026, 7, 6, 19, 0, 0, 0, time.UTC)
	if !evening.Equal(wantEvening) {
		t.Errorf("evening window = %v, ожидалось %v", evening, wantEvening)
	}

	deadline := WindowEnd(models.KindDeadlineReminder, now, yekat, day, 1290, &target)
	wantDeadline := time.Date(2026, 7, 7, 19, 0, 0, 0, time.UTC)
	if !deadline.Equal(wantDeadline) {
		t.Errorf("deadline window = %v, ожидалось %v", deadline, wantDeadline)
	}

	lateNow := time.Date(2026, 7, 6, 18, 0, 0, 0, time.UTC)
	lateMorning := WindowEnd(models.KindMorningPlan, lateNow, yekat, day, 1290, nil)
	if !lateMorning.After(lateNow) {
		t.Error("окно должно быть строго позже sent_at")
	}

	ping := WindowEnd(models.KindFreePing, now, yekat, day, 1290, nil)
	if got := ping.Sub(now); got != 3*time.Hour {
		t.Errorf("free_ping окно %v, ожидалось 3h", got)
	}

	praise := WindowEnd(models.KindPraise, now, yekat, day, 1290, nil)
	if got := praise.Sub(now); got != time.Hour {
		t.Errorf("praise окно %v, ожидалось 1h", got)
	}
}
