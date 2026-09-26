package logic

import (
	"testing"

	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

func occurrence(status string, progress float64, task map[string]any) map[string]any {
	return map[string]any{"status": status, "progress_minutes": progress, "task": task}
}

func TestBuildDayTasksClassifiesAndSorts(t *testing.T) {
	occs := []map[string]any{
		occurrence("pending", 25, map[string]any{"title": "Поиск работы", "effort_minutes": 120.0, "requires_pomodoro": true, "reschedulable": false, "recurrence_kind": "daily"}),
		occurrence("pending", 0, map[string]any{"title": "Работа", "start_time_minutes": 600.0, "estimated_duration_minutes": 540.0, "requires_pomodoro": true, "recurrence_kind": "daily"}),
		occurrence("pending", 0, map[string]any{"title": "Сборы в зал", "start_time_minutes": 400.0, "estimated_duration_minutes": 20.0, "requires_pomodoro": false, "recurrence_kind": "daily"}),
		occurrence("completed", 0, map[string]any{"title": "GOROOT", "requires_pomodoro": true, "reschedulable": true, "recurrence_kind": "spaced_repetition"}),
		{"status": "pending"},
	}
	got := BuildDayTasks(occs)
	if len(got) != 4 {
		t.Fatalf("ожидалось 4 задачи, получено %d: %+v", len(got), got)
	}
	want := []struct {
		title, kind, start, end string
	}{
		{"Сборы в зал", models.DayTaskEvent, "06:40", "07:00"},
		{"Работа", models.DayTaskWindow, "10:00", "19:00"},
		{"GOROOT", models.DayTaskEffort, "", ""},
		{"Поиск работы", models.DayTaskEffort, "", ""},
	}
	for i, w := range want {
		g := got[i]
		if g.Title != w.title || g.Kind != w.kind || g.Start != w.start || g.End != w.end {
			t.Fatalf("позиция %d: %+v, ожидалось %+v", i, g, w)
		}
	}
	job := got[3]
	if job.EffortMinutes != 120 || job.ProgressMinutes != 25 || job.Reschedulable || !job.RequiresPomodoro || job.Recurrence != "daily" {
		t.Fatalf("поиск работы разобран неверно: %+v", job)
	}
	if !got[2].Reschedulable || got[2].Status != "completed" {
		t.Fatalf("повторение из learning должно быть переносимым и выполненным: %+v", got[2])
	}
}

func TestWithoutEventsKeepsWindowsAndEffort(t *testing.T) {
	tasks := []models.DayTask{
		{Title: "Зал", Kind: models.DayTaskEvent},
		{Title: "Работа", Kind: models.DayTaskWindow},
		{Title: "Английский", Kind: models.DayTaskEffort},
	}
	got := WithoutEvents(tasks)
	if len(got) != 2 || got[0].Title != "Работа" || got[1].Title != "Английский" {
		t.Fatalf("события без помидоров должны уйти, остальное остаться: %+v", got)
	}
}

func TestSummarizePomodoroPlan(t *testing.T) {
	slots := []map[string]any{
		{"idx": 0.0, "done": true, "task": map[string]any{"title_snapshot": "Поиск работы"}},
		{"idx": 1.0, "done": true, "task": map[string]any{"title_snapshot": "Поиск работы"}},
		{"idx": 2.0, "done": false, "task": map[string]any{"title_snapshot": "Работа"}},
		{"idx": 3.0, "done": false, "label": "чтение"},
		{"idx": 4.0, "done": false},
		{"idx": 5.0, "done": false, "overflow": true, "task": map[string]any{"title_snapshot": "Английский"}},
	}
	s := SummarizePomodoroPlan(slots, 1.75, "idle")
	if s.Total != 5 || s.Done != 2 || s.Overflow != 1 || s.Credit != 1.75 || s.Phase != "idle" {
		t.Fatalf("сводка: %+v", s)
	}
	want := []models.TaskSlots{{Title: "Поиск работы", Slots: 2, Done: 2}, {Title: "Работа", Slots: 1}, {Title: "чтение", Slots: 1}, {Title: "свободный", Slots: 1}}
	if len(s.ByTask) != len(want) {
		t.Fatalf("по задачам: %+v", s.ByTask)
	}
	for i := range want {
		if s.ByTask[i] != want[i] {
			t.Fatalf("по задачам[%d] = %+v, ожидалось %+v", i, s.ByTask[i], want[i])
		}
	}
}
