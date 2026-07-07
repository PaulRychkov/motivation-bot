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

type EveningSummary struct {
	Done      []PlanItem `json:"done"`
	Skipped   []PlanItem `json:"skipped"`
	Remaining []PlanItem `json:"remaining"`
}

func BuildEveningSummary(items []PlanItem, completedTaskIDs, skippedTaskIDs map[string]bool) EveningSummary {
	var s EveningSummary
	for _, it := range items {
		switch {
		case completedTaskIDs[it.TaskID]:
			s.Done = append(s.Done, it)
		case skippedTaskIDs[it.TaskID]:
			s.Skipped = append(s.Skipped, it)
		default:
			s.Remaining = append(s.Remaining, it)
		}
	}
	return s
}
