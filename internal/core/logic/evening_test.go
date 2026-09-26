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
