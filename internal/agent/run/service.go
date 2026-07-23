package run

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/agent/coreclient"
	"github.com/PaulRychkov/motivation-bot/internal/agent/openrouter"
	"github.com/PaulRychkov/motivation-bot/internal/agent/prompts"
	"github.com/PaulRychkov/motivation-bot/internal/agent/react"
	"github.com/PaulRychkov/motivation-bot/internal/agent/tools"
	"github.com/PaulRychkov/motivation-bot/internal/config"
	"github.com/PaulRychkov/motivation-bot/internal/contracts"
	"github.com/PaulRychkov/motivation-bot/internal/core/models"
	"github.com/PaulRychkov/motivation-bot/internal/kafkax"
)

const reactRunTimeout = 5 * time.Minute

type Service struct {
	Cfg     config.Config
	Log     *zap.Logger
	Core    *coreclient.Client
	Prod    kafkax.Sender
	Engine  *react.Engine
	LLM     react.LLM
	Prompts *prompts.Store
	Shared  []tools.Source
}

func (s *Service) HandleMessage(ctx context.Context, topic string, _, value []byte) error {
	switch topic {
	case kafkax.TopicTgUpdates:
		return s.handleUpdate(ctx, value)
	case kafkax.TopicAgentRequests:
		return s.handleAgentRequest(ctx, value)
	}
	return nil
}

func (s *Service) handleUpdate(ctx context.Context, value []byte) error {
	var upd contracts.TgUpdate
	if err := json.Unmarshal(value, &upd); err != nil {
		s.Log.Warn("невалидный tg update", zap.Error(err))
		return nil
	}
	if upd.ChatID == 0 || strings.TrimSpace(upd.Text) == "" {
		return nil
	}
	s.Log.Info("сообщение пользователя", zap.Int64("chat_id", upd.ChatID))

	profile, created, err := s.Core.EnsureProfile(ctx, upd.ChatID)
	if err != nil {
		s.Log.Error("core недоступен", zap.Error(err))
		s.sendText(upd.ChatID, s.Prompts.Get("fallback_core_down"))
		return err
	}

	text := upd.Text
	if err := s.Core.AppendDialog(ctx, upd.ChatID, models.DialogAppend{
		Role:                     models.RoleUser,
		Content:                  &text,
		ReplyToTelegramMessageID: upd.ReplyToMessageID,
	}); err != nil {
		s.Log.Error("сохранение реплики пользователя", zap.Error(err))
	}

	history, err := s.Core.Dialog(ctx, upd.ChatID, 40)
	if err != nil {
		s.Log.Warn("история диалога недоступна", zap.Error(err))
	}
	notes, err := s.Core.Notes(ctx)
	if err != nil {
		s.Log.Warn("заметки недоступны", zap.Error(err))
	}

	system := s.buildSystemPrompt(profile, notes, created)
	msgs := append([]openrouter.Message{{Role: "system", Content: system}}, historyToMessages(history)...)

	src := tools.NewCombined(append([]tools.Source{tools.NewCoreTools(s.Core, upd.ChatID)}, s.Shared...)...)

	runCtx, cancel := context.WithTimeout(ctx, reactRunTimeout)
	defer cancel()
	final, err := s.Engine.Run(runCtx, src, msgs, func(m openrouter.Message) {
		s.persistStep(ctx, upd.ChatID, m)
	})
	if err != nil || strings.TrimSpace(final) == "" {
		if err != nil {
			s.Log.Warn("ReAct-цикл завершился с ошибкой, фолбэк", zap.Error(err))
		}
		final = s.Prompts.Get("fallback_dialog")
		fb := final
		if aerr := s.Core.AppendDialog(ctx, upd.ChatID, models.DialogAppend{Role: models.RoleAssistant, Content: &fb}); aerr != nil {
			s.Log.Warn("сохранение фолбэка", zap.Error(aerr))
		}
	}
	s.sendText(upd.ChatID, final)
	return nil
}

func (s *Service) persistStep(ctx context.Context, chatID int64, m openrouter.Message) {
	in := models.DialogAppend{Role: m.Role}
	if m.Content != "" {
		content := m.Content
		in.Content = &content
	}
	switch m.Role {
	case "assistant":
		if len(m.ToolCalls) > 0 {
			raw, err := json.Marshal(m.ToolCalls)
			if err == nil {
				in.ToolCalls = raw
			}
		}
	case "tool":
		id := m.ToolCallID
		in.ToolCallID = &id
	}
	if in.Content == nil && len(in.ToolCalls) == 0 {
		return
	}
	if err := s.Core.AppendDialog(ctx, chatID, in); err != nil {
		s.Log.Warn("сохранение шага диалога", zap.Error(err))
	}
}

func (s *Service) sendText(chatID int64, text string) {
	msg := contracts.TgOutgoing{
		CorrelationID: "dlg-" + uuid.NewString(),
		ChatID:        chatID,
		Text:          text,
	}
	b, err := json.Marshal(msg)
	if err != nil {
		s.Log.Error("marshal tg outgoing", zap.Error(err))
		return
	}
	if err := s.Prod.Send(kafkax.TopicTgOutgoing, fmt.Sprint(chatID), b); err != nil {
		s.Log.Error("отправка в tg-outgoing", zap.Error(err))
	}
}

func (s *Service) buildSystemPrompt(p models.ChatProfile, notes []models.AgentNote, created bool) string {
	var b strings.Builder
	b.WriteString(s.Prompts.Get("dialog"))
	b.WriteString("\n\n## Профиль пользователя\n")
	b.WriteString(fmt.Sprintf("- Таймзона: %s\n", p.Timezone))
	b.WriteString(fmt.Sprintf("- Утренний план: %s, вечерний итог: %s\n", minutesToHHMM(p.MorningPlanMin), minutesToHHMM(p.EveningReviewMin)))
	b.WriteString(fmt.Sprintf("- Бюджет свободных пингов в день: %d\n", p.FreePingDailyBudget))
	b.WriteString(fmt.Sprintf("- Тон по умолчанию: %s\n", p.DefaultTone))
	if p.QuietStartMin != nil && p.QuietEndMin != nil {
		b.WriteString(fmt.Sprintf("- Тихие часы: %s–%s\n", minutesToHHMM(*p.QuietStartMin), minutesToHHMM(*p.QuietEndMin)))
	}
	if p.Persona != nil && *p.Persona != "" {
		b.WriteString("\n## Персона\n")
		b.WriteString(*p.Persona)
		b.WriteString("\n")
	}
	if len(notes) > 0 {
		b.WriteString("\n## Долгосрочная память (agent_notes)\n")
		for _, n := range notes {
			b.WriteString(fmt.Sprintf("- %s: %s\n", n.Key, string(n.Value)))
		}
	}
	if created {
		b.WriteString("\n")
		b.WriteString(s.Prompts.Get("onboarding"))
	}
	if loc, err := time.LoadLocation(p.Timezone); err == nil {
		b.WriteString(fmt.Sprintf("\nСейчас: %s\n", time.Now().In(loc).Format("2006-01-02 15:04 (Monday)")))
	}
	return b.String()
}

func (s *Service) handleAgentRequest(ctx context.Context, value []byte) error {
	var req contracts.AgentRequest
	if err := json.Unmarshal(value, &req); err != nil {
		s.Log.Warn("невалидный agent request", zap.Error(err))
		return nil
	}
	if req.CorrelationID == "" {
		return nil
	}

	tone := models.ToneNeutral
	if t, ok := req.Context["tone"].(string); ok && t != "" {
		tone = t
	}

	text := s.generateInterventionText(ctx, req, tone)
	resp := contracts.AgentResponse{CorrelationID: req.CorrelationID, Text: text, Tone: tone}
	b, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal agent response: %w", err)
	}
	if err := s.Prod.Send(kafkax.TopicAgentResponses, req.CorrelationID, b); err != nil {
		return fmt.Errorf("send agent response: %w", err)
	}
	return nil
}

func (s *Service) generateInterventionText(ctx context.Context, req contracts.AgentRequest, tone string) string {
	fallback := s.Prompts.Get("fallback_" + req.Kind)
	if fallback == "" {
		fallback = s.Prompts.Get("fallback_dialog")
	}

	payload, err := json.Marshal(map[string]any{"kind": req.Kind, "tone": tone, "context": req.Context})
	if err != nil {
		return fallback
	}

	llmCtx, cancel := context.WithTimeout(ctx, time.Duration(s.Cfg.LLMTimeoutSec)*time.Second)
	defer cancel()
	msg, err := s.LLM.Chat(llmCtx, openrouter.ChatRequest{
		Model:       s.Cfg.LLMModel,
		Temperature: openrouter.Float(0.8),
		Messages: []openrouter.Message{
			{Role: "system", Content: s.Prompts.Get("intervention")},
			{Role: "user", Content: string(payload)},
		},
	})
	if err != nil {
		s.Log.Warn("LLM недоступен, фолбэк-текст", zap.String("kind", req.Kind), zap.Error(err))
		return fallback
	}
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		return fallback
	}
	return text
}

func historyToMessages(history []models.DialogMessage) []openrouter.Message {
	answered := map[string]bool{}
	for _, m := range history {
		if m.Role == models.RoleTool && m.ToolCallID != nil {
			answered[*m.ToolCallID] = true
		}
	}
	var out []openrouter.Message
	pendingToolIDs := map[string]bool{}
	for _, m := range history {
		msg := openrouter.Message{Role: m.Role}
		if m.Content != nil {
			msg.Content = *m.Content
		}
		switch m.Role {
		case models.RoleAssistant:
			if len(m.ToolCalls) > 0 {
				calls, ok := parseAnsweredCalls(m.ToolCalls, answered)
				if !ok && msg.Content == "" {
					continue
				}
				msg.ToolCalls = calls
				for _, tc := range calls {
					pendingToolIDs[tc.ID] = true
				}
			}
		case models.RoleTool:
			if m.ToolCallID == nil || !pendingToolIDs[*m.ToolCallID] {
				continue
			}
			msg.ToolCallID = *m.ToolCallID
		}
		out = append(out, msg)
	}
	return out
}

func parseAnsweredCalls(raw []byte, answered map[string]bool) ([]openrouter.ToolCall, bool) {
	var calls []openrouter.ToolCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil, false
	}
	for _, tc := range calls {
		if !answered[tc.ID] {
			return nil, false
		}
	}
	return calls, true
}

func minutesToHHMM(m int) string {
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}
