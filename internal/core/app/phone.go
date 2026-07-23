package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/cloudevents"
	"github.com/PaulRychkov/motivation-bot/internal/core/logic"
	"github.com/PaulRychkov/motivation-bot/internal/core/models"
	"github.com/PaulRychkov/motivation-bot/internal/kafkax"
)

type phoneApp struct {
	Package           string `json:"package"`
	Label             string `json:"label"`
	Category          string `json:"category"`
	ForegroundSeconds int64  `json:"foreground_seconds"`
}

type phoneToday struct {
	Date                   string     `json:"date"`
	TotalForegroundSeconds int64      `json:"total_foreground_seconds"`
	Unlocks                int        `json:"unlocks"`
	Apps                   []phoneApp `json:"apps"`
}

type phonePayload struct {
	DeviceID           string     `json:"device_id"`
	ChatID             int64      `json:"chat_id"`
	WindowStart        string     `json:"window_start"`
	WindowEnd          string     `json:"window_end"`
	ForegroundApp      string     `json:"foreground_app"`
	ForegroundPackage  string     `json:"foreground_package"`
	ForegroundCategory string     `json:"foreground_category"`
	ForegroundSince    string     `json:"foreground_since"`
	ScreenOn           bool       `json:"screen_on"`
	Apps               []phoneApp `json:"apps"`
	DistractingSeconds int64      `json:"distracting_seconds"`
	Today              phoneToday `json:"today"`
}

func (a *App) IngestPhoneUsage(ctx context.Context, raw []byte) error {
	var env cloudevents.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("невалидный CloudEvent: %w", err)
	}
	if env.ID == "" || env.Source == "" || env.Type == "" {
		return fmt.Errorf("CloudEvent без обязательных полей id/source/type")
	}
	if env.Source != models.SourcePhone {
		return fmt.Errorf("ожидался source=%s, получен %q", models.SourcePhone, env.Source)
	}
	if err := a.Prod.Send(kafkax.TopicPhoneEvents, env.Subject, raw); err != nil {
		return fmt.Errorf("публикация события телефона: %w", err)
	}
	return nil
}

func (a *App) HandlePhoneEvent(ctx context.Context, value []byte) error {
	if err := a.ingestExternalEvent(ctx, value); err != nil {
		return err
	}
	a.ProcessInbox(ctx)
	a.evaluateDistraction(ctx, value)
	return nil
}

type phoneNudge struct {
	At        time.Time `json:"at"`
	DistrSec  int64     `json:"distr_sec"`
	IV        string    `json:"iv"`
	Kind      string    `json:"kind"`
	Evaluated bool      `json:"evaluated"`
}

type phoneState struct {
	Date         string         `json:"date"`
	DayWatermark int            `json:"day_watermark"`
	History      []logic.Sample `json:"history"`
	LastNudge    phoneNudge     `json:"last_nudge"`
}

func (a *App) evaluateDistraction(ctx context.Context, value []byte) {
	var env cloudevents.Envelope
	if err := json.Unmarshal(value, &env); err != nil {
		return
	}
	if env.Type != models.TypePhoneUsageSnapshot {
		return
	}
	var payload phonePayload
	if err := json.Unmarshal(env.Data, &payload); err != nil {
		a.Log.Warn("невалидные данные телефона", zap.Error(err))
		return
	}
	if payload.ChatID == 0 {
		return
	}

	profile, err := a.ProfileByChatID(ctx, payload.ChatID)
	if err != nil {
		a.Log.Warn("профиль для телефона не найден", zap.Int64("chat_id", payload.ChatID))
		return
	}
	loc, err := time.LoadLocation(profile.Timezone)
	if err != nil {
		return
	}

	windowEnd := parseMoment(payload.WindowEnd, env.Time)
	localDate := logic.LocalDate(windowEnd, loc)
	state := a.loadPhoneState(ctx, payload.ChatID, localDate)

	cur, labels, cats := phoneSample(payload, windowEnd)

	if logic.InDailyWindow(logic.LocalMinutes(windowEnd, loc), a.Cfg.PhoneRestStartMin, a.Cfg.PhoneRestEndMin) {
		a.savePhoneState(ctx, payload.ChatID, appendPhoneSample(state, cur, a.Cfg.PhoneRecentWindowMin))
		return
	}

	if a.evaluatePhoneEffect(ctx, profile, loc, &state, cur, windowEnd, labels, cats) {
		a.savePhoneState(ctx, payload.ChatID, appendPhoneSample(state, cur, a.Cfg.PhoneRecentWindowMin))
		return
	}

	a.recommendRecent(ctx, profile, loc, &state, payload, cur, windowEnd, labels, cats)

	a.savePhoneState(ctx, payload.ChatID, appendPhoneSample(state, cur, a.Cfg.PhoneRecentWindowMin))
}

func (a *App) recommendRecent(ctx context.Context, profile models.ChatProfile, loc *time.Location, state *phoneState, payload phonePayload, cur logic.Sample, windowEnd time.Time, labels, cats map[string]string) bool {
	nowDistract, _ := logic.ActivityNow(state.History, cur)
	if nowDistract < a.Cfg.PhoneNowDistractMin {
		return false
	}
	recent := logic.ComputeRecent(state.History, cur, time.Duration(a.Cfg.PhoneRecentWindowMin)*time.Minute, labels, cats)
	if !logic.RecentSignificant(recent, a.Cfg.PhoneRecentDistractMin) {
		return false
	}
	if !coolPassed(state.LastNudge.At, windowEnd, a.Cfg.PhoneRecentCooldownMin) {
		return false
	}

	day := logic.AnalyzeDay(dayApps(payload.Today.Apps))
	reqCtx := map[string]any{
		"reason":                    "activity_recent",
		"recent_minutes":            recent.DistractingMin,
		"window_minutes":            recent.SpanMinutes,
		"now_minutes":               nowDistract,
		"screen_minutes":            recent.ScreenMin,
		"top_apps":                  recent.TopApps,
		"today_minutes":             day.TotalMinutes,
		"today_distracting_minutes": day.DistractingMinutes,
		"unlocks":                   payload.Today.Unlocks,
	}
	iv := a.emitPhoneNudge(ctx, profile, loc, reqCtx)
	if iv == nil {
		return true
	}
	state.LastNudge = phoneNudge{At: windowEnd, DistrSec: cur.DistrSec, IV: iv.ID.String(), Kind: "recent"}
	a.Log.Info("рекомендация по текущей активности",
		zap.Int("now_min", nowDistract), zap.Int("recent_min", recent.DistractingMin), zap.Int("span_min", recent.SpanMinutes))
	return true
}

func (a *App) evaluatePhoneEffect(ctx context.Context, profile models.ChatProfile, loc *time.Location, state *phoneState, cur logic.Sample, windowEnd time.Time, labels, cats map[string]string) bool {
	ln := state.LastNudge
	if ln.At.IsZero() || ln.Evaluated {
		return false
	}
	if windowEnd.Sub(ln.At) < time.Duration(a.Cfg.PhoneEffectCheckMin)*time.Minute {
		return false
	}

	postDistract := logic.DeltaMinutes(ln.DistrSec, cur.DistrSec)
	worked := postDistract < a.Cfg.PhoneEffectWorkedMin
	state.LastNudge.Evaluated = true

	if id, err := uuid.Parse(ln.IV); err == nil {
		outcome := models.OutcomeIgnored
		if worked {
			outcome = models.OutcomeActivated
		}
		a.setInterventionOutcome(ctx, id, outcome)
	}

	if worked {
		a.Log.Info("сообщение подействовало", zap.Int("отвлечение_после_мин", postDistract))
		return false
	}

	nowDistract, _ := logic.ActivityNow(state.History, cur)
	if nowDistract < a.Cfg.PhoneNowDistractMin {
		a.Log.Info("сообщение не сработало, но телефон сейчас не активен — не догоняем",
			zap.Int("отвлечение_после_мин", postDistract))
		return false
	}

	a.Log.Info("сообщение не подействовало, отвлечение продолжается",
		zap.Int("отвлечение_после_мин", postDistract), zap.Int("сейчас_мин", nowDistract))
	recent := logic.ComputeRecent(state.History, cur, time.Duration(a.Cfg.PhoneRecentWindowMin)*time.Minute, labels, cats)
	reqCtx := map[string]any{
		"reason":         "activity_persist",
		"recent_minutes": recent.DistractingMin,
		"since_minutes":  int(windowEnd.Sub(ln.At).Minutes()),
		"top_apps":       recent.TopApps,
	}
	iv := a.emitPhoneNudge(ctx, profile, loc, reqCtx)
	if iv == nil {
		return true
	}
	state.LastNudge = phoneNudge{At: windowEnd, DistrSec: cur.DistrSec, IV: iv.ID.String(), Kind: "persist", Evaluated: true}
	return true
}

func (a *App) emitPhoneNudge(ctx context.Context, profile models.ChatProfile, loc *time.Location, reqCtx map[string]any) *models.Intervention {
	iv, err := a.createIntervention(ctx, profile, loc, time.Now().UTC(), createParams{
		Kind:        models.KindQuestion,
		TriggeredBy: models.TriggerSchedule,
		Context:     reqCtx,
	})
	if err != nil {
		a.Log.Error("создание рекомендации по активности", zap.Error(err))
		return nil
	}
	return iv
}

func (a *App) setInterventionOutcome(ctx context.Context, id uuid.UUID, outcome string) {
	now := time.Now().UTC()
	if err := a.DB.WithContext(ctx).Model(&models.Intervention{}).
		Where("id = ? AND outcome = ?", id, models.OutcomePending).
		Updates(map[string]any{"outcome": outcome, "outcome_at": now, "updated_at": now}).Error; err != nil {
		a.Log.Warn("не удалось отметить исход рекомендации", zap.Error(err))
	}
}

func phoneSample(payload phonePayload, windowEnd time.Time) (logic.Sample, map[string]string, map[string]string) {
	labels := map[string]string{}
	cats := map[string]string{}
	cur := logic.Sample{T: windowEnd, ScreenSec: payload.Today.TotalForegroundSeconds, Apps: map[string]int64{}}
	for _, app := range payload.Today.Apps {
		cat := logic.NormalizeCategory(app.Package, app.Category)
		label := app.Label
		if label == "" {
			label = app.Package
		}
		labels[app.Package] = label
		cats[app.Package] = cat
		if logic.Distracting(cat) {
			cur.DistrSec += app.ForegroundSeconds
			cur.Apps[app.Package] = app.ForegroundSeconds
		}
	}
	return cur, labels, cats
}

func appendPhoneSample(state phoneState, cur logic.Sample, windowMin int) phoneState {
	history := state.History
	if n := len(history); n > 0 && cur.T.Sub(history[n-1].T) < time.Minute {
		history = history[:n-1]
	}
	history = append(history, cur)
	keepFrom := cur.T.Add(-2 * time.Duration(maxInt(windowMin, 30)) * time.Minute)
	pruned := history[:0]
	for _, s := range history {
		if !s.T.Before(keepFrom) {
			pruned = append(pruned, s)
		}
	}
	if len(pruned) > 40 {
		pruned = pruned[len(pruned)-40:]
	}
	state.History = pruned
	return state
}

func coolPassed(last, now time.Time, cooldownMin int) bool {
	if last.IsZero() || cooldownMin <= 0 {
		return true
	}
	return now.Sub(last) >= time.Duration(cooldownMin)*time.Minute
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func dayApps(apps []phoneApp) []logic.DayApp {
	out := make([]logic.DayApp, 0, len(apps))
	for _, app := range apps {
		out = append(out, logic.DayApp{
			Package:           app.Package,
			Label:             app.Label,
			Category:          app.Category,
			ForegroundSeconds: app.ForegroundSeconds,
		})
	}
	return out
}

func (a *App) loadPhoneState(ctx context.Context, chatID int64, localDate string) phoneState {
	fresh := phoneState{Date: localDate}
	var note models.AgentNote
	if err := a.DB.WithContext(ctx).Where("key = ?", phoneStateKey(chatID)).First(&note).Error; err != nil {
		return fresh
	}
	var loaded phoneState
	if json.Unmarshal(note.Value, &loaded) != nil || loaded.Date != localDate {
		return fresh
	}
	return loaded
}

func (a *App) savePhoneState(ctx context.Context, chatID int64, state phoneState) {
	b, err := json.Marshal(state)
	if err != nil {
		return
	}
	if err := a.UpsertNote(ctx, phoneStateKey(chatID), b, nil); err != nil {
		a.Log.Warn("не удалось сохранить состояние телефона", zap.Error(err))
	}
}

func phoneStateKey(chatID int64) string {
	return fmt.Sprintf("phone_state:%d", chatID)
}

func (a *App) RecentPhoneUsage(ctx context.Context, limit int) ([]models.InboxEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	var events []models.InboxEvent
	if err := a.DB.WithContext(ctx).
		Where("source = ?", models.SourcePhone).
		Order("occurred_at DESC").Limit(limit).
		Find(&events).Error; err != nil {
		return nil, fmt.Errorf("выборка событий телефона: %w", err)
	}
	return events, nil
}

func (a *App) PhoneActivity(ctx context.Context, chatID int64) (models.PhoneActivity, error) {
	out := models.PhoneActivity{ByCategory: map[string]int{}}

	var evs []models.InboxEvent
	if err := a.DB.WithContext(ctx).
		Where("source = ?", models.SourcePhone).
		Order("occurred_at DESC").Limit(1).Find(&evs).Error; err != nil {
		return out, fmt.Errorf("последнее событие телефона: %w", err)
	}
	if len(evs) == 0 {
		return out, nil
	}
	ev := evs[0]

	var env cloudevents.Envelope
	payloadRaw := json.RawMessage(ev.Payload)
	if json.Unmarshal(ev.Payload, &env) == nil && len(env.Data) > 0 {
		payloadRaw = env.Data
	}
	var payload phonePayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return out, nil
	}

	day := logic.AnalyzeDay(dayApps(payload.Today.Apps))
	occurred := ev.OccurredAt
	out.UpdatedAt = &occurred
	out.TodayMinutes = day.TotalMinutes
	out.TodayDistractingMinutes = day.DistractingMinutes
	out.ByCategory = day.ByCategory
	out.ForegroundApp = payload.ForegroundApp
	for _, ap := range day.TopDistracting {
		out.TopApps = append(out.TopApps, models.PhoneAppMinutes{Label: ap.Label, Category: ap.Category, Minutes: ap.Minutes})
	}

	loc := time.UTC
	if p, err := a.ProfileByChatID(ctx, chatID); err == nil {
		if l, lerr := time.LoadLocation(p.Timezone); lerr == nil {
			loc = l
		}
	}
	state := a.loadPhoneState(ctx, chatID, logic.LocalDate(occurred, loc))
	cur, labels, cats := phoneSample(payload, occurred)
	recent := logic.ComputeRecent(state.History, cur, time.Duration(a.Cfg.PhoneRecentWindowMin)*time.Minute, labels, cats)
	now, _ := logic.ActivityNow(state.History, cur)
	out.LastHourDistractingMinutes = recent.DistractingMin
	out.NowDistractingMinutes = now
	return out, nil
}

func parseMoment(value string, fallback time.Time) time.Time {
	if value == "" {
		return fallback
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fallback
	}
	return parsed
}
