package logic

import (
	"testing"
	"time"
)

func TestEvaluateDistraction(t *testing.T) {
	now := time.Date(2026, 7, 21, 15, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		snapshot  PhoneSnapshot
		threshold int
		lastNudge time.Time
		cooldown  int
		nudge     bool
		minutes   int
	}{
		{
			name: "долго сидит в отвлекающем приложении",
			snapshot: PhoneSnapshot{
				ForegroundCategory: "video",
				ForegroundSince:    now.Add(-20 * time.Minute),
				WindowEnd:          now,
				ScreenOn:           true,
			},
			threshold: 15,
			nudge:     true,
			minutes:   20,
		},
		{
			name: "сидит меньше порога",
			snapshot: PhoneSnapshot{
				ForegroundCategory: "social",
				ForegroundSince:    now.Add(-5 * time.Minute),
				WindowEnd:          now,
				ScreenOn:           true,
			},
			threshold: 15,
			nudge:     false,
		},
		{
			name: "категория не отвлекающая",
			snapshot: PhoneSnapshot{
				ForegroundCategory: "productivity",
				ForegroundSince:    now.Add(-60 * time.Minute),
				WindowEnd:          now,
				ScreenOn:           true,
			},
			threshold: 15,
			nudge:     false,
		},
		{
			name: "экран выключен",
			snapshot: PhoneSnapshot{
				ForegroundCategory: "games",
				ForegroundSince:    now.Add(-60 * time.Minute),
				WindowEnd:          now,
				ScreenOn:           false,
			},
			threshold: 15,
			nudge:     false,
		},
		{
			name: "не прошёл перерыв после прошлого напоминания",
			snapshot: PhoneSnapshot{
				ForegroundCategory: "video",
				ForegroundSince:    now.Add(-30 * time.Minute),
				WindowEnd:          now,
				ScreenOn:           true,
			},
			threshold: 15,
			lastNudge: now.Add(-10 * time.Minute),
			cooldown:  60,
			nudge:     false,
		},
		{
			name: "перерыв прошёл",
			snapshot: PhoneSnapshot{
				ForegroundCategory: "video",
				ForegroundSince:    now.Add(-30 * time.Minute),
				WindowEnd:          now,
				ScreenOn:           true,
			},
			threshold: 15,
			lastNudge: now.Add(-90 * time.Minute),
			cooldown:  60,
			nudge:     true,
			minutes:   30,
		},
		{
			name: "набралось суммой за окно, хотя приложение открыто недавно",
			snapshot: PhoneSnapshot{
				ForegroundCategory: "social",
				ForegroundSince:    now.Add(-2 * time.Minute),
				WindowEnd:          now,
				DistractingSeconds: 16 * 60,
				ScreenOn:           true,
			},
			threshold: 15,
			nudge:     true,
			minutes:   16,
		},
		{
			name: "порог не задан - берётся 15 минут",
			snapshot: PhoneSnapshot{
				ForegroundCategory: "games",
				ForegroundSince:    now.Add(-16 * time.Minute),
				WindowEnd:          now,
				ScreenOn:           true,
			},
			threshold: 0,
			nudge:     true,
			minutes:   16,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateDistraction(tc.snapshot, tc.threshold, tc.lastNudge, tc.cooldown)
			if got.Nudge != tc.nudge {
				t.Fatalf("напоминание %v, ожидалось %v", got.Nudge, tc.nudge)
			}
			if tc.nudge && got.Minutes != tc.minutes {
				t.Errorf("минут %d, ожидалось %d", got.Minutes, tc.minutes)
			}
		})
	}
}

func TestDistracting(t *testing.T) {
	tests := []struct {
		category string
		want     bool
	}{
		{"social", true},
		{"video", true},
		{"games", true},
		{"work", false},
		{"communication", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := Distracting(tc.category); got != tc.want {
			t.Errorf("%q: %v, ожидалось %v", tc.category, got, tc.want)
		}
	}
}
