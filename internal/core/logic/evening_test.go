package logic

import "testing"

func TestParsePlanItems(t *testing.T) {
	payload := map[string]any{
		"plan_id":      "p1",
		"date":         "2026-07-06",
		"committed_by": "app",
		"items": []any{
			map[string]any{"task_id": "t1", "occurrence_id": "o1", "title": "Английский"},
			map[string]any{"task_id": "t2", "occurrence_id": "o2"},
			map[string]any{"note": "мусор без идентификаторов"},
			"не объект",
		},
	}
	items := ParsePlanItems(payload)
	if len(items) != 2 {
		t.Fatalf("items = %d, ожидалось 2: %+v", len(items), items)
	}
	if items[0].Title != "Английский" || items[0].TaskID != "t1" {
		t.Errorf("первый item распарсен неверно: %+v", items[0])
	}
}

func TestBuildEveningSummary(t *testing.T) {
	items := []PlanItem{
		{TaskID: "t1", Title: "Сделано"},
		{TaskID: "t2", Title: "Пропущено осознанно"},
		{TaskID: "t3", Title: "Не сделано"},
		{TaskID: "t4", Title: "Тоже не сделано"},
	}
	s := BuildEveningSummary(items,
		map[string]bool{"t1": true},
		map[string]bool{"t2": true},
	)
	if len(s.Done) != 1 || s.Done[0].TaskID != "t1" {
		t.Errorf("done: %+v", s.Done)
	}
	if len(s.Skipped) != 1 || s.Skipped[0].TaskID != "t2" {
		t.Errorf("skipped: %+v", s.Skipped)
	}
	if len(s.Remaining) != 2 {
		t.Errorf("remaining должен содержать невыполненные без missed-статуса: %+v", s.Remaining)
	}
}

func TestBuildEveningSummaryEmptyPlan(t *testing.T) {
	s := BuildEveningSummary(nil, map[string]bool{"t1": true}, nil)
	if len(s.Done)+len(s.Skipped)+len(s.Remaining) != 0 {
		t.Errorf("пустой план должен давать пустой итог: %+v", s)
	}
}
