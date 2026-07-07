package logic

import (
	"testing"

	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

func TestDueScheduled(t *testing.T) {
	day := "2026-07-06"
	tests := []struct {
		name                 string
		morning, evening     int
		localMin             int
		wantMorning, wantEve bool
	}{
		{"до утра ничего", 540, 1290, 300, false, false},
		{"ровно утреннее время", 540, 1290, 540, true, false},
		{"день — окно утра открыто", 540, 1290, 900, true, false},
		{"минута до вечера", 540, 1290, 1289, true, false},
		{"ровно вечернее время", 540, 1290, 1290, false, true},
		{"поздний вечер", 540, 1290, 1400, false, true},
		{"вечер раньше утра — утро до полуночи", 1290, 540, 1350, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			due := DueScheduled(tt.morning, tt.evening, tt.localMin, day)
			var gotMorning, gotEve bool
			for _, d := range due {
				if d.LocalDate != day {
					t.Errorf("LocalDate = %s, ожидалось %s", d.LocalDate, day)
				}
				switch d.Kind {
				case models.KindMorningPlan:
					gotMorning = true
				case models.KindEveningReview:
					gotEve = true
				}
			}
			if gotMorning != tt.wantMorning || gotEve != tt.wantEve {
				t.Errorf("due = (morning %v, evening %v), ожидалось (%v, %v)",
					gotMorning, gotEve, tt.wantMorning, tt.wantEve)
			}
		})
	}
}

func TestMissingScheduledAntiDouble(t *testing.T) {
	day := "2026-07-06"
	due := DueScheduled(540, 1290, 1300, day)
	if len(due) != 1 || due[0].Kind != models.KindEveningReview {
		t.Fatalf("ожидался только evening_review, получено %+v", due)
	}

	sent := map[string]bool{}
	exists := func(kind, localDate string) bool { return sent[kind+"|"+localDate] }

	first := MissingScheduled(due, exists)
	if len(first) != 1 {
		t.Fatalf("первый тик должен отправить, получено %+v", first)
	}
	sent[first[0].Kind+"|"+first[0].LocalDate] = true

	second := MissingScheduled(DueScheduled(540, 1290, 1310, day), exists)
	if len(second) != 0 {
		t.Fatalf("повторный тик того же дня не должен дублировать, получено %+v", second)
	}

	nextDay := MissingScheduled(DueScheduled(540, 1290, 1310, "2026-07-07"), exists)
	if len(nextDay) != 1 {
		t.Fatalf("новый локальный день должен снова отправить, получено %+v", nextDay)
	}
}
