package logic

import (
	"fmt"
	"sort"

	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

func BuildDayTasks(occs []map[string]any) []models.DayTask {
	out := make([]models.DayTask, 0, len(occs))
	for _, o := range occs {
		task, _ := o["task"].(map[string]any)
		if task == nil {
			continue
		}
		dt := models.DayTask{
			Title:            stringOf(task["title"]),
			Status:           stringOf(o["status"]),
			ProgressMinutes:  intOf(o["progress_minutes"]),
			RequiresPomodoro: boolOr(task["requires_pomodoro"], true),
			Reschedulable:    boolOr(task["reschedulable"], false),
			Recurrence:       stringOf(task["recurrence_kind"]),
		}
		if start, ok := task["start_time_minutes"].(float64); ok {
			s := int(start)
			dt.Start = clock(s)
			if dur := intOf(task["estimated_duration_minutes"]); dur > 0 {
				dt.End = clock(s + dur)
			}
			dt.Kind = models.DayTaskEvent
			if dt.RequiresPomodoro {
				dt.Kind = models.DayTaskWindow
			}
		} else {
			dt.Kind = models.DayTaskEffort
			dt.EffortMinutes = intOf(task["effort_minutes"])
		}
		out = append(out, dt)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Start == "") != (b.Start == "") {
			return a.Start != ""
		}
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		return a.Title < b.Title
	})
	return out
}

func WithoutEvents(tasks []models.DayTask) []models.DayTask {
	out := make([]models.DayTask, 0, len(tasks))
	for _, t := range tasks {
		if t.Kind != models.DayTaskEvent {
			out = append(out, t)
		}
	}
	return out
}

func SummarizePomodoroPlan(slots []map[string]any, credit float64, phase string) *models.PomodoroSummary {
	s := &models.PomodoroSummary{Credit: credit, Phase: phase, ByTask: []models.TaskSlots{}}
	index := map[string]int{}
	for _, sl := range slots {
		if b, _ := sl["overflow"].(bool); b {
			s.Overflow++
			continue
		}
		s.Total++
		done, _ := sl["done"].(bool)
		if done {
			s.Done++
		}
		title := slotTitle(sl)
		i, ok := index[title]
		if !ok {
			i = len(s.ByTask)
			index[title] = i
			s.ByTask = append(s.ByTask, models.TaskSlots{Title: title})
		}
		s.ByTask[i].Slots++
		if done {
			s.ByTask[i].Done++
		}
	}
	return s
}

func slotTitle(sl map[string]any) string {
	if task, ok := sl["task"].(map[string]any); ok {
		if t := stringOf(task["title_snapshot"]); t != "" {
			return t
		}
	}
	if l := stringOf(sl["label"]); l != "" {
		return l
	}
	return "свободный"
}

func clock(minutes int) string {
	minutes = ((minutes % 1440) + 1440) % 1440
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func intOf(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func boolOr(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}
