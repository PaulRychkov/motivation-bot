package texts

import "github.com/PaulRychkov/motivation-bot/internal/core/models"

const (
	FallbackDialog   = "Немного завис — попробуй переформулировать или напиши чуть позже."
	FallbackCoreDown = "Сервис временно недоступен, попробуй чуть позже."
)

var FallbackBodies = map[string]string{
	models.KindMorningPlan:      "Доброе утро! План на сегодня в планировщике, помидоры расставлены — поехали. Если что-то поменялось, напиши.",
	models.KindEveningReview:    "Вечер! Давай подведём итоги: что удалось сделать сегодня?",
	models.KindDeadlineReminder: "Напоминаю: у задачи приближается дедлайн. Успеешь?",
	models.KindFreePing:         "Как продвигаются дела? Могу чем-то помочь?",
	models.KindQuestion:         "Как идут дела? Если что-то застопорилось — начни с одного помидора.",
	models.KindPraise:           "Задача закрыта — отличная работа! Так держать!",
}
