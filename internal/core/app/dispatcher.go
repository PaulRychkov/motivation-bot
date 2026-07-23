package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/PaulRychkov/motivation-bot/internal/cloudevents"
	"github.com/PaulRychkov/motivation-bot/internal/core/logic"
	"github.com/PaulRychkov/motivation-bot/internal/core/models"
	"github.com/PaulRychkov/motivation-bot/internal/kafkax"
	"github.com/PaulRychkov/motivation-bot/internal/texts"
)

const inboxRetryStep = time.Minute

func (a *App) HandleKafka(ctx context.Context, topic string, key, value []byte) error {
	switch topic {
	case kafkax.TopicTasksEvents, kafkax.TopicPomodoroEvents:
		if err := a.ingestExternalEvent(ctx, value); err != nil {
			return err
		}
		a.ProcessInbox(ctx)
		return nil
	case kafkax.TopicPhoneEvents:
		return a.HandlePhoneEvent(ctx, value)
	case kafkax.TopicAgentResponses:
		return a.HandleAgentResponse(ctx, value)
	case kafkax.TopicTgSent:
		return a.HandleTgSent(ctx, value)
	}
	return nil
}

func (a *App) ingestExternalEvent(ctx context.Context, value []byte) error {
	var env cloudevents.Envelope
	if err := json.Unmarshal(value, &env); err != nil {
		a.Log.Warn("невалидный CloudEvent, пропущен", zap.Error(err))
		return nil
	}
	if env.ID == "" || env.Source == "" || env.Type == "" {
		a.Log.Warn("CloudEvent без обязательных полей, пропущен", zap.String("id", env.ID))
		return nil
	}
	occurredAt := env.Time
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	payload := env.Data
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	ev := models.InboxEvent{
		EventID:    env.ID,
		Source:     env.Source,
		Type:       env.Type,
		OccurredAt: occurredAt,
		Payload:    datatypes.JSON(payload),
	}
	if env.Subject != "" {
		ev.Subject = &env.Subject
	}
	if err := a.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source"}, {Name: "event_id"}},
		DoNothing: true,
	}).Create(&ev).Error; err != nil {
		return fmt.Errorf("insert inbox event: %w", err)
	}
	return nil
}

func (a *App) RunDispatcher(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(a.Cfg.DispatchPeriodSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.ProcessInbox(ctx)
		}
	}
}

func (a *App) ProcessInbox(ctx context.Context) {
	a.dispatchMu.Lock()
	defer a.dispatchMu.Unlock()
	for {
		var events []models.InboxEvent
		err := a.DB.WithContext(ctx).
			Where("processed_at IS NULL").
			Where("next_attempt_at IS NULL OR next_attempt_at <= ?", time.Now().UTC()).
			Order("occurred_at").Limit(50).
			Find(&events).Error
		if err != nil {
			a.Log.Error("выборка инбокса", zap.Error(err))
			return
		}
		if len(events) == 0 {
			return
		}
		for _, ev := range events {
			if err := a.processInboxEvent(ctx, ev); err != nil {
				a.Log.Error("обработка события инбокса",
					zap.String("event_id", ev.EventID), zap.String("type", ev.Type), zap.Error(err))
				if derr := a.deferInboxEvent(ctx, ev, err); derr != nil {
					a.Log.Error("отметка ретрая инбокса", zap.Error(derr))
					return
				}
				continue
			}
			if err := a.DB.WithContext(ctx).Model(&models.InboxEvent{}).
				Where("id = ?", ev.ID).Update("processed_at", time.Now().UTC()).Error; err != nil {
				a.Log.Error("отметка processed_at", zap.Error(err))
				return
			}
		}
	}
}

func (a *App) deferInboxEvent(ctx context.Context, ev models.InboxEvent, cause error) error {
	attempts := ev.Attempts + 1
	retryAt := time.Now().UTC().Add(time.Duration(attempts) * inboxRetryStep)
	return a.DB.WithContext(ctx).Model(&models.InboxEvent{}).Where("id = ?", ev.ID).
		Updates(map[string]any{
			"attempts":        attempts,
			"last_error":      cause.Error(),
			"next_attempt_at": retryAt,
		}).Error
}

func (a *App) processInboxEvent(ctx context.Context, ev models.InboxEvent) error {
	var payload map[string]any
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		a.Log.Warn("невалидный payload события", zap.String("event_id", ev.EventID), zap.Error(err))
		return nil
	}
	evt := logic.Event{
		Source:     ev.Source,
		EventID:    ev.EventID,
		Type:       ev.Type,
		OccurredAt: ev.OccurredAt,
		Payload:    payload,
	}
	return a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		var open []models.Intervention
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("outcome IN ?", []string{models.OutcomePending, models.OutcomePartial}).
			Where("window_ends_at > ?", now).
			Find(&open).Error; err != nil {
			return fmt.Errorf("load open interventions: %w", err)
		}
		byID := make(map[uuid.UUID]models.Intervention, len(open))
		pending := make([]logic.Pending, 0, len(open))
		for _, iv := range open {
			byID[iv.ID] = iv
			p := logic.Pending{
				ID:             iv.ID,
				ChatProfileID:  iv.ChatProfileID,
				Kind:           iv.Kind,
				Outcome:        iv.Outcome,
				TaskSource:     iv.TaskSource,
				TaskExternalID: iv.TaskExternalID,
				LocalDate:      iv.LocalDate.String(),
			}
			if iv.TargetDate != nil {
				td := iv.TargetDate.String()
				p.TargetDate = &td
			}
			pending = append(pending, p)
		}

		mctx := logic.MatchContext{
			HasUserReply: func(id uuid.UUID) bool {
				var cnt int64
				tx.Model(&models.DialogMessage{}).
					Where("intervention_id = ? AND role = ?", id, models.RoleUser).
					Count(&cnt)
				return cnt > 0
			},
		}
		if evt.Type == "occurrence.completed" {
			mctx.PlanTaskIDs = a.planTaskIDsForDate(tx, evt.Date())
		}

		res := logic.Match(evt, pending, mctx)
		for _, ch := range res.Changes {
			iv := byID[ch.InterventionID]
			newOutcome, apply := logic.ResolveOutcome(iv.Outcome, ch.Outcome)
			if !apply {
				continue
			}
			entry := map[string]any{
				"source":      evt.Source,
				"event_id":    evt.EventID,
				"type":        evt.Type,
				"occurred_at": evt.OccurredAt.Format(time.RFC3339),
				"payload":     payload,
			}
			evidence, err := appendEvidence(iv.Evidence, entry)
			if err != nil {
				return err
			}
			updates := map[string]any{
				"outcome": newOutcome, "evidence": evidence, "updated_at": now,
			}
			if newOutcome != iv.Outcome {
				updates["outcome_at"] = now
			}
			if err := tx.Model(&models.Intervention{}).Where("id = ?", iv.ID).Updates(updates).Error; err != nil {
				return fmt.Errorf("apply outcome: %w", err)
			}
			if newOutcome != iv.Outcome && (newOutcome == models.OutcomeActivated || newOutcome == models.OutcomePartial) {
				iv.Evidence = evidence
				if err := a.emitActivationDetected(tx, iv, newOutcome, now); err != nil {
					return err
				}
			}
		}
		for _, praise := range res.Praise {
			if err := a.schedulePraise(tx, praise, now); err != nil {
				a.Log.Warn("не удалось создать praise", zap.Error(err))
			}
		}
		return nil
	})
}

func (a *App) planTaskIDsForDate(tx *gorm.DB, date string) map[string]bool {
	if date == "" {
		return nil
	}
	var plan models.InboxEvent
	err := tx.Where("source = ? AND type = ? AND payload->>'date' = ?", models.SourceTasks, "plan.committed", date).
		Order("occurred_at DESC").First(&plan).Error
	if err != nil {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(plan.Payload, &payload); err != nil {
		return nil
	}
	ids := map[string]bool{}
	for _, item := range logic.ParsePlanItems(payload) {
		if item.TaskID != "" {
			ids[item.TaskID] = true
		}
	}
	return ids
}

func (a *App) schedulePraise(tx *gorm.DB, praise logic.PraiseTask, now time.Time) error {
	var profiles []models.ChatProfile
	if err := tx.Find(&profiles).Error; err != nil {
		return fmt.Errorf("load profiles: %w", err)
	}
	for _, p := range profiles {
		loc, err := time.LoadLocation(p.Timezone)
		if err != nil {
			continue
		}
		localDate := logic.LocalDate(now, loc)
		if logic.IsPaused(p.PausedUntil, localDate) {
			continue
		}
		if logic.InQuietHours(p.QuietStartMin, p.QuietEndMin, logic.LocalMinutes(now, loc)) {
			continue
		}
		var cnt int64
		tx.Model(&models.Intervention{}).
			Where("chat_profile_id = ? AND kind = ? AND task_source = ? AND task_external_id = ? AND local_date = ?",
				p.ID, models.KindPraise, praise.Ref.Source, praise.Ref.ExternalID, localDate).
			Count(&cnt)
		if cnt > 0 {
			continue
		}
		ld, err := models.ParseDate(localDate)
		if err != nil {
			continue
		}
		src, ext := praise.Ref.Source, praise.Ref.ExternalID
		iv := models.Intervention{
			ChatProfileID:  p.ID,
			Kind:           models.KindPraise,
			TriggeredBy:    models.TriggerSchedule,
			TaskSource:     &src,
			TaskExternalID: &ext,
			Body:           texts.FallbackBodies[models.KindPraise],
			Tone:           p.DefaultTone,
			SentAt:         now,
			WindowEndsAt:   logic.WindowEnd(models.KindPraise, now, loc, localDate, p.EveningReviewMin, nil),
			Outcome:        models.OutcomePending,
			Evidence:       datatypes.JSON("[]"),
			LocalDate:      ld,
		}
		if err := tx.Create(&iv).Error; err != nil {
			return fmt.Errorf("create praise: %w", err)
		}
		a.requestAgentText(iv, map[string]any{
			"chat_id":    p.ChatID,
			"tone":       p.DefaultTone,
			"local_date": localDate,
			"task_title": praise.Title,
			"reason":     "task.completed: задача закрыта целиком",
		})
	}
	return nil
}
