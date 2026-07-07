package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/PaulRychkov/motivation-bot/internal/agent/coreclient"
	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

func NewCoreTools(core *coreclient.Client, chatID int64) *Static {
	s := NewStatic()

	s.Add("get_profile",
		"Получить профиль пользователя: таймзона, времена утреннего плана и вечернего итога (минуты от полуночи), тихие часы, бюджет пингов, тон, персона, пауза.",
		nil,
		func(ctx context.Context, _ json.RawMessage) (string, error) {
			p, err := core.Profile(ctx, chatID)
			if err != nil {
				return "", err
			}
			return MustJSON(p), nil
		})

	s.Add("update_profile",
		"Изменить настройки профиля. Передавай только изменяемые поля.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"timezone":               map[string]any{"type": "string", "description": "IANA-таймзона, например Asia/Yekaterinburg"},
				"morning_plan_min":       map[string]any{"type": "integer", "description": "минуты от полуночи, 540 = 09:00"},
				"evening_review_min":     map[string]any{"type": "integer"},
				"quiet_start_min":        map[string]any{"type": []string{"integer", "null"}},
				"quiet_end_min":          map[string]any{"type": []string{"integer", "null"}},
				"free_ping_daily_budget": map[string]any{"type": "integer"},
				"default_tone":           map[string]any{"type": "string", "enum": []string{"gentle", "neutral", "energetic", "strict", "humorous"}},
				"persona":                map[string]any{"type": []string{"string", "null"}},
				"paused_until":           map[string]any{"type": []string{"string", "null"}, "description": "YYYY-MM-DD или null"},
			},
		},
		func(ctx context.Context, args json.RawMessage) (string, error) {
			var patch map[string]any
			if err := json.Unmarshal(args, &patch); err != nil {
				return "", fmt.Errorf("невалидные аргументы: %w", err)
			}
			p, err := core.UpdateProfile(ctx, chatID, patch)
			if err != nil {
				return "", err
			}
			return MustJSON(p), nil
		})

	s.Add("list_pending_interventions",
		"Список интервенций бота с открытым окном ожидания (outcome pending/partial).",
		nil,
		func(ctx context.Context, _ json.RawMessage) (string, error) {
			list, err := core.PendingInterventions(ctx, chatID)
			if err != nil {
				return "", err
			}
			return MustJSON(list), nil
		})

	s.Add("set_intervention_outcome",
		"Закрыть интервенцию исходом из диалога: refused (пользователь отказался) или rescheduled (перенёс). Также допустимы activated/partial/ignored.",
		map[string]any{
			"type":     "object",
			"required": []string{"intervention_id", "outcome"},
			"properties": map[string]any{
				"intervention_id": map[string]any{"type": "string", "description": "UUID интервенции"},
				"outcome":         map[string]any{"type": "string", "enum": []string{"activated", "partial", "ignored", "refused", "rescheduled"}},
			},
		},
		func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				InterventionID string `json:"intervention_id"`
				Outcome        string `json:"outcome"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("невалидные аргументы: %w", err)
			}
			id, err := uuid.Parse(in.InterventionID)
			if err != nil {
				return "", fmt.Errorf("intervention_id должен быть UUID: %w", err)
			}
			if err := core.SetInterventionOutcome(ctx, id, in.Outcome); err != nil {
				return "", err
			}
			return `{"status":"ok"}`, nil
		})

	s.Add("send_ping",
		"Инициировать проактивное касание вне расписания: free_ping (лёгкий пинг, тратит дневной бюджет) или question (уточняющий вопрос, вне бюджета). Может быть подавлено тихими часами, паузой, бюджетом или skipped-задачей — тогда вернётся status=suppressed.",
		map[string]any{
			"type":     "object",
			"required": []string{"kind", "reason"},
			"properties": map[string]any{
				"kind":             map[string]any{"type": "string", "enum": []string{"free_ping", "question"}},
				"reason":           map[string]any{"type": "string", "description": "зачем пингуешь — попадёт в контекст генерации текста"},
				"task_external_id": map[string]any{"type": "string", "description": "UUID задачи из tasks, если пинг про конкретную задачу"},
				"target_date":      map[string]any{"type": "string", "description": "YYYY-MM-DD, день вхождения задачи"},
			},
		},
		func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Kind           string  `json:"kind"`
				Reason         string  `json:"reason"`
				TaskExternalID *string `json:"task_external_id"`
				TargetDate     *string `json:"target_date"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("невалидные аргументы: %w", err)
			}
			req := models.AgentInterventionRequest{
				ChatID:         chatID,
				Kind:           in.Kind,
				TaskExternalID: in.TaskExternalID,
				TargetDate:     in.TargetDate,
				Context:        map[string]any{"reason": in.Reason},
			}
			if in.TaskExternalID != nil {
				src := models.SourceTasks
				req.TaskSource = &src
			}
			res, err := core.CreateIntervention(ctx, req)
			if err != nil {
				return "", err
			}
			return MustJSON(res), nil
		})

	s.Add("save_note",
		"Сохранить заметку в долгосрочную память (upsert по key). Ключи с неймспейсом через точку, например timing.best_ping_hours.",
		map[string]any{
			"type":     "object",
			"required": []string{"key", "note"},
			"properties": map[string]any{
				"key":        map[string]any{"type": "string"},
				"note":       map[string]any{"type": "string"},
				"confidence": map[string]any{"type": "number", "description": "0..1"},
				"expires_at": map[string]any{"type": []string{"string", "null"}, "description": "RFC3339, момент автозабывания"},
			},
		},
		func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Key        string   `json:"key"`
				Note       string   `json:"note"`
				Confidence *float64 `json:"confidence"`
				ExpiresAt  *string  `json:"expires_at"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("невалидные аргументы: %w", err)
			}
			if in.Key == "" || in.Note == "" {
				return "", fmt.Errorf("нужны key и note")
			}
			value := map[string]any{"note": in.Note}
			if in.Confidence != nil {
				value["confidence"] = *in.Confidence
			}
			var expiresAt *time.Time
			if in.ExpiresAt != nil {
				t, err := time.Parse(time.RFC3339, *in.ExpiresAt)
				if err != nil {
					return "", fmt.Errorf("expires_at: %w", err)
				}
				expiresAt = &t
			}
			raw, err := json.Marshal(value)
			if err != nil {
				return "", err
			}
			if err := core.SaveNote(ctx, in.Key, raw, expiresAt); err != nil {
				return "", err
			}
			return `{"status":"ok"}`, nil
		})

	s.Add("get_effectiveness_stats",
		"Агрегированная статистика эффективности интервенций: счётчики по kind и outcome.",
		nil,
		func(ctx context.Context, _ json.RawMessage) (string, error) {
			stats, err := core.Stats(ctx, chatID)
			if err != nil {
				return "", err
			}
			return MustJSON(stats), nil
		})

	return s
}
