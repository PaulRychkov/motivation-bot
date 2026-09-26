package run

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

var statusText = map[string]string{
	"completed":   "выполнено",
	"skipped":     "пропущено",
	"missed":      "пропущено по сроку",
	"rescheduled": "перенесено",
}

var recurrenceText = map[string]string{
	"daily":             "ежедневная",
	"weekdays":          "по будням",
	"weekly":            "еженедельная",
	"once":              "разовая",
	"spaced_repetition": "интервальное повторение",
}

func renderDay(day models.DaySnapshot, ok bool) string {
	var b strings.Builder
	b.WriteString("\n## Сегодня\n")
	if !ok {
		b.WriteString("Снимок дня сейчас недоступен — если нужны задачи или помидоры, возьми их инструментами tasks_* и pomodoro_*.\n")
		return b.String()
	}
	b.WriteString(fmt.Sprintf("Дата %s, сейчас %s. Это актуальные данные из tasks и pomodoro — опирайся на них, а не на догадки.\n", day.Date, day.Now))

	var timed, effort []models.DayTask
	for _, t := range day.Tasks {
		if t.Kind == models.DayTaskEffort {
			effort = append(effort, t)
		} else {
			timed = append(timed, t)
		}
	}
	if len(timed) > 0 {
		b.WriteString("\nПо времени:\n")
		for _, t := range timed {
			span := t.Start
			if t.End != "" {
				span += "–" + t.End
			}
			kind := "событие без помидоров: помидоры не предлагать, не переносится"
			if t.Kind == models.DayTaskWindow {
				kind = "рабочее окно: часть помидоров внутри отдана этой задаче"
			}
			b.WriteString(fmt.Sprintf("- %s %s — %s%s\n", span, t.Title, kind, statusSuffix(t.Status)))
		}
	}
	if len(effort) > 0 {
		b.WriteString("\nЗадачи на объём:\n")
		for _, t := range effort {
			b.WriteString(fmt.Sprintf("- %s — %s; %s; %s%s\n", t.Title, effortText(t), recurrenceLabel(t.Recurrence), moveText(t), statusSuffix(t.Status)))
		}
	}
	if p := day.Pomodoro; p != nil {
		b.WriteString(fmt.Sprintf("\nПомидоры: пройдено %d из %d слотов плана, засчитано %s", p.Done, p.Total, strconv.FormatFloat(p.Credit, 'f', -1, 64)))
		if p.Overflow > 0 {
			b.WriteString(fmt.Sprintf(", сверх плана ещё %d", p.Overflow))
		}
		if p.Phase != "" && p.Phase != "idle" {
			b.WriteString(", таймер сейчас идёт")
		}
		b.WriteString(".\n")
		if len(p.ByTask) > 0 {
			parts := make([]string, 0, len(p.ByTask))
			for _, s := range p.ByTask {
				parts = append(parts, fmt.Sprintf("%s %d (сделано %d)", s.Title, s.Slots, s.Done))
			}
			b.WriteString("Слоты по задачам: " + strings.Join(parts, ", ") + ".\n")
		}
	} else {
		b.WriteString("\nПлан помидоров сейчас недоступен (приложение помидора на ноутбуке не на связи).\n")
	}
	return b.String()
}

func effortText(t models.DayTask) string {
	if t.EffortMinutes <= 0 {
		return fmt.Sprintf("сделано %d мин", t.ProgressMinutes)
	}
	return fmt.Sprintf("сделано %d из %d мин", t.ProgressMinutes, t.EffortMinutes)
}

func recurrenceLabel(kind string) string {
	if text, ok := recurrenceText[kind]; ok {
		return text
	}
	if kind == "" {
		return "без повторения"
	}
	return kind
}

func moveText(t models.DayTask) string {
	if t.Reschedulable {
		return "можно перенести"
	}
	return "не переносится (завтра будет новое вхождение само)"
}

func statusSuffix(status string) string {
	if text, ok := statusText[status]; ok {
		return " — " + text
	}
	return ""
}
