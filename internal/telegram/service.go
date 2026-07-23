package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	tgbot "github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"
	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/contracts"
	"github.com/PaulRychkov/motivation-bot/internal/kafkax"
)

type Service struct {
	Bot  *tgbot.Bot
	Prod kafkax.Sender
	Log  *zap.Logger
}

func New(token string, prod kafkax.Sender, log *zap.Logger) (*Service, error) {
	s := &Service{Prod: prod, Log: log}
	b, err := tgbot.New(token, tgbot.WithDefaultHandler(s.onUpdate))
	if err != nil {
		return nil, fmt.Errorf("telegram bot: %w", err)
	}
	s.Bot = b
	return s, nil
}

func (s *Service) onUpdate(_ context.Context, _ *tgbot.Bot, update *tgmodels.Update) {
	if update == nil || update.Message == nil || update.Message.Text == "" {
		return
	}
	m := update.Message
	msg := contracts.TgUpdate{
		UpdateID:  update.ID,
		ChatID:    m.Chat.ID,
		MessageID: int64(m.ID),
		Text:      m.Text,
		Date:      time.Unix(int64(m.Date), 0).UTC(),
	}
	if m.From != nil {
		msg.Username = m.From.Username
	}
	if m.ReplyToMessage != nil {
		replyTo := int64(m.ReplyToMessage.ID)
		msg.ReplyToMessageID = &replyTo
	}
	b, err := json.Marshal(msg)
	if err != nil {
		s.Log.Error("marshal tg update", zap.Error(err))
		return
	}
	if err := s.Prod.Send(kafkax.TopicTgUpdates, fmt.Sprint(m.Chat.ID), b); err != nil {
		s.Log.Error("публикация tg update", zap.Error(err))
		return
	}
	s.Log.Info("update опубликован", zap.Int64("chat_id", m.Chat.ID))
}

func (s *Service) HandleOutgoing(ctx context.Context, topic string, _, value []byte) error {
	if topic != kafkax.TopicTgOutgoing {
		return nil
	}
	var msg contracts.TgOutgoing
	if err := json.Unmarshal(value, &msg); err != nil {
		s.Log.Warn("невалидное исходящее сообщение", zap.Error(err))
		return nil
	}
	if msg.ChatID == 0 || msg.Text == "" {
		return nil
	}
	params := &tgbot.SendMessageParams{ChatID: msg.ChatID, Text: msg.Text}
	if msg.ReplyToMessageID != nil {
		params.ReplyParameters = &tgmodels.ReplyParameters{MessageID: int(*msg.ReplyToMessageID)}
	}
	sent, err := s.Bot.SendMessage(ctx, params)
	if err != nil {
		if msg.ReplyToMessageID != nil {
			params.ReplyParameters = nil
			sent, err = s.Bot.SendMessage(ctx, params)
		}
		if err != nil {
			return fmt.Errorf("send telegram message: %w", err)
		}
	}
	confirmation := contracts.TgSent{
		CorrelationID: msg.CorrelationID,
		ChatID:        msg.ChatID,
		MessageID:     int64(sent.ID),
	}
	b, err := json.Marshal(confirmation)
	if err != nil {
		return fmt.Errorf("marshal tg sent: %w", err)
	}
	if err := s.Prod.Send(kafkax.TopicTgSent, fmt.Sprint(msg.ChatID), b); err != nil {
		return fmt.Errorf("publish tg sent: %w", err)
	}
	return nil
}
