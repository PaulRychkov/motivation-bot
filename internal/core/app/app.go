package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/PaulRychkov/motivation-bot/internal/config"
	"github.com/PaulRychkov/motivation-bot/internal/core/logic"
	"github.com/PaulRychkov/motivation-bot/internal/core/models"
	"github.com/PaulRychkov/motivation-bot/internal/core/pomoclient"
	"github.com/PaulRychkov/motivation-bot/internal/core/tasksclient"
	"github.com/PaulRychkov/motivation-bot/internal/kafkax"
)

type App struct {
	DB    *gorm.DB
	Prod  kafkax.Sender
	Cfg   config.Config
	Log   *zap.Logger
	Tasks *tasksclient.Client
	Pomo  *pomoclient.Client

	dispatchMu sync.Mutex
	pushMu     sync.Mutex
	lastPush   map[uuid.UUID]time.Time
}

func New(db *gorm.DB, prod kafkax.Sender, cfg config.Config, log *zap.Logger) *App {
	return &App{
		DB:       db,
		Prod:     prod,
		Cfg:      cfg,
		Log:      log,
		Tasks:    tasksclient.New(cfg.TasksURL),
		Pomo:     pomoclient.New(cfg.PomodoroURL),
		lastPush: map[uuid.UUID]time.Time{},
	}
}

var ErrNotFound = models.ErrNotFound

func (a *App) EnsureProfile(ctx context.Context, chatID int64) (models.ChatProfile, bool, error) {
	var p models.ChatProfile
	err := a.DB.WithContext(ctx).Where("chat_id = ?", chatID).First(&p).Error
	if err == nil {
		return p, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return p, false, fmt.Errorf("load profile: %w", err)
	}
	p = models.ChatProfile{
		ChatID:              chatID,
		Timezone:            a.Cfg.DefaultTimezone,
		MorningPlanMin:      models.DefaultMorningPlanMin,
		EveningReviewMin:    models.DefaultEveningReviewMin,
		FreePingDailyBudget: models.DefaultFreePingDailyBudget,
		DefaultTone:         models.ToneNeutral,
	}
	if err := a.DB.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "chat_id"}}, DoNothing: true}).
		Create(&p).Error; err != nil {
		return p, false, fmt.Errorf("create profile: %w", err)
	}
	created := p.ID != uuid.Nil
	if err := a.DB.WithContext(ctx).Where("chat_id = ?", chatID).First(&p).Error; err != nil {
		return p, false, fmt.Errorf("reload profile: %w", err)
	}
	if created {
		a.Log.Info("создан новый профиль чата", zap.Int64("chat_id", chatID))
	}
	return p, created, nil
}

func (a *App) ProfileByChatID(ctx context.Context, chatID int64) (models.ChatProfile, error) {
	var p models.ChatProfile
	err := a.DB.WithContext(ctx).Where("chat_id = ?", chatID).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, fmt.Errorf("load profile: %w", err)
	}
	return p, nil
}

func (a *App) UpdateProfile(ctx context.Context, chatID int64, patch map[string]any) (models.ChatProfile, error) {
	updates, err := logic.ValidateProfilePatch(patch)
	if err != nil {
		return models.ChatProfile{}, err
	}
	p, err := a.ProfileByChatID(ctx, chatID)
	if err != nil {
		return p, err
	}
	updates["updated_at"] = time.Now().UTC()
	if err := a.DB.WithContext(ctx).Model(&models.ChatProfile{}).
		Where("id = ?", p.ID).Updates(updates).Error; err != nil {
		return p, fmt.Errorf("update profile: %w", err)
	}
	return a.ProfileByChatID(ctx, chatID)
}

func (a *App) Interventions(ctx context.Context, chatID int64, limit int) ([]models.Intervention, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := a.DB.WithContext(ctx).Model(&models.Intervention{}).Order("sent_at DESC").Limit(limit)
	if chatID != 0 {
		p, err := a.ProfileByChatID(ctx, chatID)
		if err != nil {
			return nil, err
		}
		q = q.Where("chat_profile_id = ?", p.ID)
	}
	var out []models.Intervention
	if err := q.Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list interventions: %w", err)
	}
	return out, nil
}

func (a *App) PendingInterventions(ctx context.Context, chatID int64) ([]models.Intervention, error) {
	q := a.DB.WithContext(ctx).Model(&models.Intervention{}).
		Where("outcome IN ?", []string{models.OutcomePending, models.OutcomePartial}).
		Where("window_ends_at > ?", time.Now().UTC()).
		Order("sent_at DESC")
	if chatID != 0 {
		p, err := a.ProfileByChatID(ctx, chatID)
		if err != nil {
			return nil, err
		}
		q = q.Where("chat_profile_id = ?", p.ID)
	}
	var out []models.Intervention
	if err := q.Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list pending interventions: %w", err)
	}
	return out, nil
}

func (a *App) SetInterventionOutcome(ctx context.Context, id uuid.UUID, outcome string) error {
	return a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var iv models.Intervention
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", id).First(&iv).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("load intervention: %w", err)
		}
		now := time.Now().UTC()
		updates := map[string]any{"outcome": outcome, "outcome_at": now, "updated_at": now}
		if err := tx.Model(&models.Intervention{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return fmt.Errorf("set outcome: %w", err)
		}
		if outcome == models.OutcomeActivated || outcome == models.OutcomePartial {
			return a.emitActivationDetected(tx, iv, outcome, now)
		}
		return nil
	})
}

func (a *App) DialogHistory(ctx context.Context, chatID int64, limit int) ([]models.DialogMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 40
	}
	p, err := a.ProfileByChatID(ctx, chatID)
	if err != nil {
		return nil, err
	}
	var msgs []models.DialogMessage
	if err := a.DB.WithContext(ctx).
		Where("chat_profile_id = ?", p.ID).
		Order("created_at DESC").Limit(limit).
		Find(&msgs).Error; err != nil {
		return nil, fmt.Errorf("load dialog: %w", err)
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

func (a *App) AppendDialogMessage(ctx context.Context, chatID int64, in models.DialogAppend) (models.DialogMessage, error) {
	p, _, err := a.EnsureProfile(ctx, chatID)
	if err != nil {
		return models.DialogMessage{}, err
	}
	msg := models.DialogMessage{
		ChatProfileID:  p.ID,
		Role:           in.Role,
		Content:        in.Content,
		ToolCallID:     in.ToolCallID,
		InterventionID: in.InterventionID,
	}
	if len(in.ToolCalls) > 0 {
		msg.ToolCalls = datatypes.JSON(in.ToolCalls)
	}
	err = a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if msg.InterventionID == nil && in.Role == models.RoleUser && in.ReplyToTelegramMessageID != nil {
			msg.InterventionID = a.interventionIDByMessage(tx, p.ID, *in.ReplyToTelegramMessageID)
		}
		if err := tx.Create(&msg).Error; err != nil {
			return fmt.Errorf("create dialog message: %w", err)
		}
		if in.Role == models.RoleUser {
			return a.activateOnReply(tx, p.ID, in.Content)
		}
		return nil
	})
	if err != nil {
		return msg, err
	}
	return msg, nil
}

func (a *App) interventionIDByMessage(tx *gorm.DB, profileID uuid.UUID, telegramMessageID int64) *uuid.UUID {
	var iv models.Intervention
	err := tx.
		Where("chat_profile_id = ? AND telegram_message_id = ?", profileID, telegramMessageID).
		First(&iv).Error
	if err != nil {
		return nil
	}
	return &iv.ID
}

func (a *App) activateOnReply(tx *gorm.DB, profileID uuid.UUID, content *string) error {
	now := time.Now().UTC()
	var pending []models.Intervention
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("chat_profile_id = ?", profileID).
		Where("kind IN ?", []string{models.KindEveningReview, models.KindQuestion}).
		Where("outcome IN ?", []string{models.OutcomePending, models.OutcomePartial}).
		Where("window_ends_at > ?", now).
		Find(&pending).Error; err != nil {
		return fmt.Errorf("load reply targets: %w", err)
	}
	for _, iv := range pending {
		entry := map[string]any{"type": "user_reply", "at": now.Format(time.RFC3339)}
		if content != nil {
			entry["text"] = *content
		}
		evidence, err := appendEvidence(iv.Evidence, entry)
		if err != nil {
			return err
		}
		updates := map[string]any{
			"outcome": models.OutcomeActivated, "outcome_at": now,
			"evidence": evidence, "updated_at": now,
		}
		if err := tx.Model(&models.Intervention{}).Where("id = ?", iv.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("activate on reply: %w", err)
		}
		if err := a.emitActivationDetected(tx, iv, models.OutcomeActivated, now); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) Notes(ctx context.Context) ([]models.AgentNote, error) {
	var notes []models.AgentNote
	if err := a.DB.WithContext(ctx).
		Where("expires_at IS NULL OR expires_at > ?", time.Now().UTC()).
		Order("key").Find(&notes).Error; err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	return notes, nil
}

func (a *App) UpsertNote(ctx context.Context, key string, value json.RawMessage, expiresAt *time.Time) error {
	if key == "" || len(value) == 0 {
		return fmt.Errorf("note: нужны key и value")
	}
	if !json.Valid(value) {
		return fmt.Errorf("note value: невалидный JSON")
	}
	note := models.AgentNote{Key: key, Value: datatypes.JSON(value), ExpiresAt: expiresAt}
	err := a.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.Assignments(map[string]any{"value": datatypes.JSON(value), "expires_at": expiresAt, "updated_at": time.Now().UTC()}),
	}).Create(&note).Error
	if err != nil {
		return fmt.Errorf("upsert note: %w", err)
	}
	return nil
}

func (a *App) EffectivenessStats(ctx context.Context, chatID int64) ([]models.KindStat, error) {
	q := a.DB.WithContext(ctx).Table("interventions").
		Select("kind::text AS kind, outcome::text AS outcome, count(*) AS count").
		Group("1, 2").Order("1, 2")
	if chatID != 0 {
		p, err := a.ProfileByChatID(ctx, chatID)
		if err != nil {
			return nil, err
		}
		q = q.Where("chat_profile_id = ?", p.ID)
	}
	var out []models.KindStat
	if err := q.Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("effectiveness stats: %w", err)
	}
	return out, nil
}

func (a *App) freePingsUsedToday(tx *gorm.DB, p models.ChatProfile, loc *time.Location, now time.Time) (int, error) {
	localDate := logic.LocalDate(now, loc)
	start, end, err := logic.LocalDayBoundsUTC(loc, localDate)
	if err != nil {
		return 0, err
	}
	var cnt int64
	if err := tx.Model(&models.Intervention{}).
		Where("chat_profile_id = ?", p.ID).
		Where("kind = ?", models.KindFreePing).
		Where("sent_at >= ? AND sent_at < ?", start, end).
		Count(&cnt).Error; err != nil {
		return 0, fmt.Errorf("count free pings: %w", err)
	}
	return int(cnt), nil
}

func appendEvidence(cur datatypes.JSON, entry any) (datatypes.JSON, error) {
	var arr []any
	if len(cur) > 0 {
		if err := json.Unmarshal(cur, &arr); err != nil {
			arr = nil
		}
	}
	arr = append(arr, entry)
	b, err := json.Marshal(arr)
	if err != nil {
		return nil, fmt.Errorf("marshal evidence: %w", err)
	}
	return datatypes.JSON(b), nil
}
