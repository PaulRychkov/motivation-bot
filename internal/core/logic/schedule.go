package logic

import "github.com/PaulRychkov/motivation-bot/internal/core/models"

type ScheduledDue struct {
	Kind      string
	LocalDate string
}

func DueScheduled(morningMin, eveningMin, localMin int, localDate string) []ScheduledDue {
	var out []ScheduledDue
	morningOpen := localMin >= morningMin && (eveningMin <= morningMin || localMin < eveningMin)
	if morningOpen {
		out = append(out, ScheduledDue{Kind: models.KindMorningPlan, LocalDate: localDate})
	}
	if localMin >= eveningMin {
		out = append(out, ScheduledDue{Kind: models.KindEveningReview, LocalDate: localDate})
	}
	return out
}

func MissingScheduled(due []ScheduledDue, exists func(kind, localDate string) bool) []ScheduledDue {
	var out []ScheduledDue
	for _, d := range due {
		if !exists(d.Kind, d.LocalDate) {
			out = append(out, d)
		}
	}
	return out
}
