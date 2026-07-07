package run

import (
	"github.com/PaulRychkov/motivation-bot/internal/core/models"
	"github.com/PaulRychkov/motivation-bot/internal/texts"
)

var Fallbacks = map[string]string{
	"fallback_dialog":            texts.FallbackDialog,
	"fallback_core_down":         texts.FallbackCoreDown,
	"fallback_morning_plan":      texts.FallbackBodies[models.KindMorningPlan],
	"fallback_evening_review":    texts.FallbackBodies[models.KindEveningReview],
	"fallback_deadline_reminder": texts.FallbackBodies[models.KindDeadlineReminder],
	"fallback_free_ping":         texts.FallbackBodies[models.KindFreePing],
	"fallback_question":          texts.FallbackBodies[models.KindQuestion],
	"fallback_praise":            texts.FallbackBodies[models.KindPraise],
}
