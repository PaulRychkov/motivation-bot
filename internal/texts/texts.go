package texts

import "github.com/PaulRychkov/motivation-bot/internal/core/models"

const (
	FallbackDialog   = "Немного завис — попробуй переформулировать или напиши чуть позже."
	FallbackCoreDown = "Сервис временно недоступен, попробуй чуть позже."
)

var FallbackBodies = map[string]string{
	models.KindMorningPlan:      "Доброе утро! Что планируешь сделать сегодня? Составим план дня?",
	models.KindEveningReview:    "Вечер! Давай подведём итоги: что удалось сделать сегодня?",
	models.KindDeadlineReminder: "Напоминаю: у задачи приближается дедлайн. Успеешь?",
	models.KindFreePing:         "Как продвигаются дела? Могу чем-то помочь?",
	models.KindQuestion:         "Есть минутка? Хочу кое-что уточнить про твои планы.",
	models.KindPraise:           "Задача закрыта — отличная работа! Так держать!",
}
