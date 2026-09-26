package run

import (
	"strings"
	"testing"

	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

func TestRenderDayMarksMovabilityAndEvents(t *testing.T) {
	day := models.DaySnapshot{
		Date: "2026-09-23",
		Now:  "12:05",
		Tasks: []models.DayTask{
			{Title: "Сборы в зал", Kind: models.DayTaskEvent, Start: "06:40", End: "07:00", Status: "pending"},
			{Title: "Работа", Kind: models.DayTaskWindow, Start: "10:00", End: "19:00", RequiresPomodoro: true, Status: "pending"},
			{Title: "Поиск работы", Kind: models.DayTaskEffort, EffortMinutes: 120, ProgressMinutes: 25, RequiresPomodoro: true, Recurrence: "daily", Status: "pending"},
			{Title: "GOROOT", Kind: models.DayTaskEffort, Reschedulable: true, Recurrence: "spaced_repetition", Status: "completed"},
		},
		Pomodoro: &models.PomodoroSummary{Total: 23, Done: 3, Credit: 2.75, ByTask: []models.TaskSlots{{Title: "Поиск работы", Slots: 5, Done: 3}}},
	}
	text := renderDay(day, true)
	for _, want := range []string{
		"06:40–07:00 Сборы в зал — событие без помидоров: помидоры не предлагать, не переносится",
		"10:00–19:00 Работа — рабочее окно",
		"Поиск работы — сделано 25 из 120 мин; ежедневная; не переносится",
		"GOROOT — сделано 0 мин; интервальное повторение; можно перенести — выполнено",
		"пройдено 3 из 23 слотов плана, засчитано 2.75",
		"Поиск работы 5 (сделано 3)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("в снимке нет %q:\n%s", want, text)
		}
	}
}

func TestRenderDayUnavailable(t *testing.T) {
	text := renderDay(models.DaySnapshot{}, false)
	if !strings.Contains(text, "недоступен") || strings.Contains(text, "Помидоры:") {
		t.Fatalf("при недоступном снимке нужна подсказка про инструменты:\n%s", text)
	}
}
