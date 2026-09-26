package logic

type PlanItem struct {
	TaskID       string `json:"task_id"`
	OccurrenceID string `json:"occurrence_id"`
	Title        string `json:"title"`
}

func ParsePlanItems(payload map[string]any) []PlanItem {
	raw, ok := payload["items"].([]any)
	if !ok {
		return nil
	}
	items := make([]PlanItem, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		item := PlanItem{}
		item.TaskID, _ = m["task_id"].(string)
		item.OccurrenceID, _ = m["occurrence_id"].(string)
		item.Title, _ = m["title"].(string)
		if item.TaskID == "" && item.OccurrenceID == "" {
			continue
		}
		items = append(items, item)
	}
	return items
}
