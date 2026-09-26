package app

import "testing"

func eventTask(id, title string, start, dur int, requires bool) map[string]any {
	return map[string]any{
		"id":                         id,
		"title":                      title,
		"start_time_minutes":         float64(start),
		"estimated_duration_minutes": float64(dur),
		"requires_pomodoro":          requires,
		"is_active":                  true,
		"progress":                   "needs_action",
	}
}

func TestNonPomodoroEventAt(t *testing.T) {
	byID := map[string]map[string]any{
		"prep":  eventTask("prep", "Сборы в зал", 400, 20, false),
		"gym":   eventTask("gym", "Зал", 420, 40, false),
		"work":  eventTask("work", "Работа", 600, 540, true),
		"lunch": eventTask("lunch", "Обед", 840, 60, false),
	}
	occs := []map[string]any{{"task_id": "prep"}, {"task_id": "gym"}, {"task_id": "work"}, {"task_id": "lunch"}}
	cases := []struct {
		name   string
		minute int
		except string
		want   string
		busy   bool
	}{
		{"до сборов свободно", 386, "prep", "", false},
		{"напоминание про зал во время сборов подавляется", 406, "gym", "Сборы в зал", true},
		{"работа не считается событием без помидоров", 700, "", "", false},
		{"обед занят", 850, "", "Обед", true},
		{"само событие не мешает себе", 845, "lunch", "", false},
		{"после обеда снова работа", 900, "", "", false},
	}
	for _, tc := range cases {
		title, busy := nonPomodoroEventAt(byID, occs, tc.minute, tc.except)
		if busy != tc.busy || title != tc.want {
			t.Errorf("%s: получено (%q, %v), ожидалось (%q, %v)", tc.name, title, busy, tc.want, tc.busy)
		}
	}
}

func TestRequiresPomodoroDefaultsToTrue(t *testing.T) {
	if !requiresPomodoro(map[string]any{"title": "без флага"}) {
		t.Fatal("задача без флага требует помидоров по умолчанию")
	}
	if requiresPomodoro(map[string]any{"requires_pomodoro": false}) {
		t.Fatal("флаг false должен читаться")
	}
}
