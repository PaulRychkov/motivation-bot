package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/PaulRychkov/motivation-bot/internal/contracts"
	"github.com/PaulRychkov/motivation-bot/internal/core/logic"
	"github.com/PaulRychkov/motivation-bot/internal/core/models"
	"github.com/PaulRychkov/motivation-bot/internal/kafkax"
	"github.com/PaulRychkov/motivation-bot/internal/texts"
)

type createParams struct {
	Kind           string
	TriggeredBy    string
	TaskSource     *string
	TaskExternalID *string
	TargetDate     *string
	Context        map[string]any
}

func (a *App) createIntervention(ctx context.Context, p models.ChatProfile, loc *time.Location, now time.Time, params createParams) (*models.Intervention, error) {
	localDate := logic.LocalDate(now, loc)
	localMin := logic.LocalMinutes(now, loc)
	if logic.IsPaused(p.PausedUntil, localDate) {
		return nil, nil
	}
	if logic.InQuietHours(p.QuietStartMin, p.QuietEndMin, localMin) {
		return nil, nil
	}
	if params.Kind == models.KindFreePing {
		used, err := a.freePingsUsedToday(a.DB.WithContext(ctx), p, loc, now)
		if err != nil {
			return nil, err
		}
		if !logic.FreePingAllowed(used, p.FreePingDailyBudget) {
			a.Log.Info("бюджет free_ping исчерпан", zap.Int64("chat_id", p.ChatID))
			return nil, nil
		}
	}
	if params.TaskSource != nil && *params.TaskSource == models.SourceTasks &&
		params.TaskExternalID != nil && params.TargetDate != nil &&
		a.isSuppressed(ctx, *params.TaskExternalID, *params.TargetDate) {
		a.Log.Info("пинг подавлен: задача осознанно пропущена на эту дату",
			zap.String("task_id", *params.TaskExternalID), zap.String("date", *params.TargetDate))
		return nil, nil
	}

	ld, err := models.ParseDate(localDate)
	if err != nil {
		return nil, err
	}
	iv := models.Intervention{
		ChatProfileID:  p.ID,
		Kind:           params.Kind,
		TriggeredBy:    params.TriggeredBy,
		TaskSource:     params.TaskSource,
		TaskExternalID: params.TaskExternalID,
		Body:           texts.FallbackBodies[params.Kind],
		Tone:           p.DefaultTone,
		SentAt:         now,
		WindowEndsAt:   logic.WindowEnd(params.Kind, now, loc, localDate, p.EveningReviewMin, params.TargetDate),
		Outcome:        models.OutcomePending,
		Evidence:       datatypes.JSON("[]"),
		LocalDate:      ld,
	}
	if params.TargetDate != nil {
		td, err := models.ParseDate(*params.TargetDate)
		if err != nil {
			return nil, err
		}
		iv.TargetDate = &td
	}
	if err := a.DB.WithContext(ctx).Create(&iv).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, nil
		}
		return nil, fmt.Errorf("create intervention: %w", err)
	}

	reqCtx := params.Context
	if reqCtx == nil {
		reqCtx = map[string]any{}
	}
	reqCtx["chat_id"] = p.ChatID
	reqCtx["tone"] = p.DefaultTone
	reqCtx["local_date"] = localDate
	if p.Persona != nil {
		reqCtx["persona"] = *p.Persona
	}
	a.requestAgentText(iv, reqCtx)
	return &iv, nil
}

func (a *App) CreateAgentIntervention(ctx context.Context, in models.AgentInterventionRequest) (*models.Intervention, error) {
	if in.Kind != models.KindFreePing && in.Kind != models.KindQuestion {
		return nil, fmt.Errorf("kind должен быть free_ping или question, получено %q", in.Kind)
	}
	if (in.TaskSource == nil) != (in.TaskExternalID == nil) {
		return nil, fmt.Errorf("task_source и task_external_id задаются только парой")
	}
	if in.TargetDate != nil {
		if _, err := models.ParseDate(*in.TargetDate); err != nil {
			return nil, err
		}
	}
	p, err := a.ProfileByChatID(ctx, in.ChatID)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return nil, fmt.Errorf("таймзона профиля %q: %w", p.Timezone, err)
	}
	return a.createIntervention(ctx, p, loc, time.Now().UTC(), createParams{
		Kind:           in.Kind,
		TriggeredBy:    models.TriggerAgent,
		TaskSource:     in.TaskSource,
		TaskExternalID: in.TaskExternalID,
		TargetDate:     in.TargetDate,
		Context:        in.Context,
	})
}

func (a *App) requestAgentText(iv models.Intervention, reqCtx map[string]any) {
	req := contracts.AgentRequest{CorrelationID: iv.ID.String(), Kind: iv.Kind, Context: reqCtx}
	b, err := json.Marshal(req)
	if err != nil {
		a.Log.Error("marshal agent request", zap.Error(err))
		return
	}
	if err := a.Prod.Send(kafkax.TopicAgentRequests, iv.ID.String(), b); err != nil {
		a.Log.Warn("не удалось запросить текст у агента, останется фолбэк", zap.Error(err))
	}
}

func (a *App) HandleAgentResponse(ctx context.Context, value []byte) error {
	var resp contracts.AgentResponse
	if err := json.Unmarshal(value, &resp); err != nil {
		return fmt.Errorf("parse agent response: %w", err)
	}
	id, err := uuid.Parse(resp.CorrelationID)
	if err != nil {
		return nil
	}
	var iv models.Intervention
	if err := a.DB.WithContext(ctx).Where("id = ?", id).First(&iv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("load intervention: %w", err)
	}
	if iv.TelegramMessageID != nil {
		return nil
	}
	updates := map[string]any{"updated_at": time.Now().UTC()}
	if text := strings.TrimSpace(resp.Text); text != "" {
		updates["body"] = text
		iv.Body = text
	}
	if resp.Tone != "" && logic.ValidTone(resp.Tone) {
		updates["tone"] = resp.Tone
		iv.Tone = resp.Tone
	}
	if err := a.DB.WithContext(ctx).Model(&models.Intervention{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("update intervention body: %w", err)
	}
	return a.sendIntervention(ctx, iv)
}

func (a *App) sendIntervention(ctx context.Context, iv models.Intervention) error {
	var p models.ChatProfile
	if err := a.DB.WithContext(ctx).Where("id = ?", iv.ChatProfileID).First(&p).Error; err != nil {
		return fmt.Errorf("load profile: %w", err)
	}
	msg := contracts.TgOutgoing{
		CorrelationID: iv.ID.String(),
		ChatID:        p.ChatID,
		Text:          iv.Body,
	}
	if replyTo := a.replyTarget(ctx, iv); replyTo != nil {
		msg.ReplyToMessageID = replyTo
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal tg outgoing: %w", err)
	}
	if err := a.Prod.Send(kafkax.TopicTgOutgoing, fmt.Sprint(p.ChatID), b); err != nil {
		return fmt.Errorf("send tg outgoing: %w", err)
	}
	body := iv.Body
	ivID := iv.ID
	if _, err := a.AppendDialogMessage(ctx, p.ChatID, models.DialogAppend{
		Role:           models.RoleAssistant,
		Content:        &body,
		InterventionID: &ivID,
	}); err != nil {
		a.Log.Warn("intervention dialog append", zap.Error(err))
	}
	return nil
}

func (a *App) replyTarget(ctx context.Context, iv models.Intervention) *int64 {
	switch iv.Kind {
	case models.KindEveningReview:
		var morning models.Intervention
		err := a.DB.WithContext(ctx).
			Where("chat_profile_id = ? AND kind = ? AND local_date = ?", iv.ChatProfileID, models.KindMorningPlan, iv.LocalDate).
			First(&morning).Error
		if err == nil && morning.TelegramMessageID != nil {
			return morning.TelegramMessageID
		}
	case models.KindPraise:
		if iv.TaskSource == nil {
			return nil
		}
		var reminder models.Intervention
		err := a.DB.WithContext(ctx).
			Where("chat_profile_id = ? AND kind = ? AND task_source = ? AND task_external_id = ?",
				iv.ChatProfileID, models.KindDeadlineReminder, *iv.TaskSource, *iv.TaskExternalID).
			Order("sent_at DESC").First(&reminder).Error
		if err == nil && reminder.TelegramMessageID != nil {
			return reminder.TelegramMessageID
		}
	}
	return nil
}

func (a *App) HandleTgSent(ctx context.Context, value []byte) error {
	var sent contracts.TgSent
	if err := json.Unmarshal(value, &sent); err != nil {
		return fmt.Errorf("parse tg sent: %w", err)
	}
	id, err := uuid.Parse(sent.CorrelationID)
	if err != nil {
		return nil
	}
	return a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var iv models.Intervention
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", id).First(&iv).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return fmt.Errorf("load intervention: %w", err)
		}
		if iv.TelegramMessageID != nil {
			return nil
		}
		if err := tx.Model(&models.Intervention{}).Where("id = ?", id).
			Updates(map[string]any{"telegram_message_id": sent.MessageID, "updated_at": time.Now().UTC()}).Error; err != nil {
			return fmt.Errorf("set telegram message id: %w", err)
		}
		var p models.ChatProfile
		if err := tx.Where("id = ?", iv.ChatProfileID).First(&p).Error; err != nil {
			return fmt.Errorf("load profile: %w", err)
		}
		payload := map[string]any{
			"intervention_id": iv.ID.String(),
			"chat_id":         p.ChatID,
			"kind":            iv.Kind,
			"triggered_by":    iv.TriggeredBy,
			"tone":            iv.Tone,
			"task":            taskPayload(iv),
			"target_date":     datePayload(iv.TargetDate),
			"sent_at":         iv.SentAt.Format(time.RFC3339),
			"window_ends_at":  iv.WindowEndsAt.Format(time.RFC3339),
		}
		return a.emitOutbox(tx, "intervention.sent", iv.ID, payload)
	})
}

func (a *App) emitActivationDetected(tx *gorm.DB, iv models.Intervention, outcome string, now time.Time) error {
	var evidence []any
	if len(iv.Evidence) > 0 {
		_ = json.Unmarshal(iv.Evidence, &evidence)
	}
	payload := map[string]any{
		"intervention_id": iv.ID.String(),
		"kind":            iv.Kind,
		"outcome":         outcome,
		"task":            taskPayload(iv),
		"evidence":        evidence,
		"detected_at":     now.Format(time.RFC3339),
	}
	return a.emitOutbox(tx, "activation.detected", iv.ID, payload)
}

func (a *App) emitOutbox(tx *gorm.DB, eventType string, aggregateID uuid.UUID, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}
	ev := models.OutboxEvent{
		EventType:     eventType,
		AggregateType: "intervention",
		AggregateID:   aggregateID,
		Payload:       datatypes.JSON(b),
	}
	if err := tx.Create(&ev).Error; err != nil {
		return fmt.Errorf("create outbox event: %w", err)
	}
	return nil
}

func taskPayload(iv models.Intervention) any {
	if iv.TaskSource == nil || iv.TaskExternalID == nil {
		return nil
	}
	return map[string]any{"source": *iv.TaskSource, "external_id": *iv.TaskExternalID}
}

func datePayload(d *models.Date) any {
	if d == nil {
		return nil
	}
	return d.String()
}
