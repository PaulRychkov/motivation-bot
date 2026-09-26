package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	KindMorningPlan      = "morning_plan"
	KindEveningReview    = "evening_review"
	KindDeadlineReminder = "deadline_reminder"
	KindFreePing         = "free_ping"
	KindQuestion         = "question"
	KindPraise           = "praise"

	TriggerSchedule = "schedule"
	TriggerAgent    = "agent"

	OutcomePending     = "pending"
	OutcomeActivated   = "activated"
	OutcomePartial     = "partial"
	OutcomeIgnored     = "ignored"
	OutcomeRefused     = "refused"
	OutcomeRescheduled = "rescheduled"

	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"

	ToneNeutral = "neutral"

	SourceTasks    = "tasks"
	SourcePomodoro = "pomodoro"
	SourceBot      = "bot"
	SourcePhone    = "phone"

	TypePhoneUsageSnapshot = "phone.usage.snapshot"

	DefaultMorningPlanMin      = 540
	DefaultEveningReviewMin    = 1290
	DefaultFreePingDailyBudget = 3
)

type ChatProfile struct {
	ID                  uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ChatID              int64     `gorm:"uniqueIndex;not null" json:"chat_id"`
	Timezone            string    `gorm:"not null" json:"timezone"`
	QuietStartMin       *int      `json:"quiet_start_min"`
	QuietEndMin         *int      `json:"quiet_end_min"`
	MorningPlanMin      int       `gorm:"not null;default:540" json:"morning_plan_min"`
	EveningReviewMin    int       `gorm:"not null;default:1290" json:"evening_review_min"`
	FreePingDailyBudget int       `gorm:"not null;default:3" json:"free_ping_daily_budget"`
	DefaultTone         string    `gorm:"type:tone;not null;default:'neutral'" json:"default_tone"`
	Persona             *string   `json:"persona"`
	PausedUntil         *Date     `gorm:"type:date" json:"paused_until"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type Intervention struct {
	ID                uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ChatProfileID     uuid.UUID      `gorm:"type:uuid;not null" json:"chat_profile_id"`
	Kind              string         `gorm:"type:intervention_kind;not null" json:"kind"`
	TriggeredBy       string         `gorm:"type:intervention_trigger;not null" json:"triggered_by"`
	TaskSource        *string        `json:"task_source"`
	TaskExternalID    *string        `json:"task_external_id"`
	TargetDate        *Date          `gorm:"type:date" json:"target_date"`
	Stage             *string        `json:"stage"`
	SnoozeUntil       *time.Time     `json:"snooze_until"`
	Body              string         `gorm:"not null" json:"body"`
	Tone              string         `gorm:"type:tone;not null" json:"tone"`
	TelegramMessageID *int64         `json:"telegram_message_id"`
	SentAt            time.Time      `gorm:"not null" json:"sent_at"`
	WindowEndsAt      time.Time      `gorm:"not null" json:"window_ends_at"`
	Outcome           string         `gorm:"type:intervention_outcome;not null;default:'pending'" json:"outcome"`
	Evidence          datatypes.JSON `gorm:"not null;default:'[]'" json:"evidence"`
	OutcomeAt         *time.Time     `json:"outcome_at"`
	LocalDate         Date           `gorm:"type:date;not null" json:"local_date"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type InboxEvent struct {
	ID            uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	EventID       string         `gorm:"not null" json:"event_id"`
	Source        string         `gorm:"not null" json:"source"`
	Type          string         `gorm:"not null" json:"type"`
	Subject       *string        `json:"subject"`
	OccurredAt    time.Time      `gorm:"not null" json:"occurred_at"`
	Payload       datatypes.JSON `gorm:"not null" json:"payload"`
	ProcessedAt   *time.Time     `json:"processed_at"`
	Attempts      int            `gorm:"not null;default:0" json:"attempts"`
	LastError     *string        `json:"last_error"`
	NextAttemptAt *time.Time     `json:"next_attempt_at"`
	CreatedAt     time.Time      `json:"created_at"`
}

type DialogMessage struct {
	ID             uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ChatProfileID  uuid.UUID      `gorm:"type:uuid;not null" json:"chat_profile_id"`
	Role           string         `gorm:"type:message_role;not null" json:"role"`
	Content        *string        `json:"content"`
	ToolCalls      datatypes.JSON `json:"tool_calls"`
	ToolCallID     *string        `json:"tool_call_id"`
	InterventionID *uuid.UUID     `gorm:"type:uuid" json:"intervention_id"`
	CreatedAt      time.Time      `json:"created_at"`
}

type AgentNote struct {
	ID        uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Key       string         `gorm:"uniqueIndex;not null" json:"key"`
	Value     datatypes.JSON `gorm:"not null" json:"value"`
	ExpiresAt *time.Time     `json:"expires_at"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type OutboxEvent struct {
	ID            uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	EventType     string         `gorm:"not null"`
	AggregateType string         `gorm:"not null"`
	AggregateID   uuid.UUID      `gorm:"type:uuid;not null"`
	Payload       datatypes.JSON `gorm:"not null"`
	CreatedAt     time.Time
	PublishedAt   *time.Time
	Attempts      int `gorm:"not null;default:0"`
	LastError     *string
}

type PhoneActivity struct {
	UpdatedAt                  *time.Time        `json:"updated_at"`
	TodayMinutes               int               `json:"today_minutes"`
	TodayDistractingMinutes    int               `json:"today_distracting_minutes"`
	LastHourDistractingMinutes int               `json:"last_hour_distracting_minutes"`
	NowDistractingMinutes      int               `json:"now_distracting_minutes"`
	ForegroundApp              string            `json:"foreground_app"`
	ByCategory                 map[string]int    `json:"by_category"`
	TopApps                    []PhoneAppMinutes `json:"top_apps"`
}

type PhoneAppMinutes struct {
	Label    string `json:"label"`
	Category string `json:"category"`
	Minutes  int    `json:"minutes"`
}

func (ChatProfile) TableName() string   { return "chat_profiles" }
func (Intervention) TableName() string  { return "interventions" }
func (InboxEvent) TableName() string    { return "inbox_events" }
func (DialogMessage) TableName() string { return "dialog_messages" }
func (AgentNote) TableName() string     { return "agent_notes" }
func (OutboxEvent) TableName() string   { return "events_outbox" }

func ensureUUID(id *uuid.UUID) {
	if *id == uuid.Nil {
		*id = uuid.New()
	}
}

func (m *ChatProfile) BeforeCreate(*gorm.DB) error   { ensureUUID(&m.ID); return nil }
func (m *Intervention) BeforeCreate(*gorm.DB) error  { ensureUUID(&m.ID); return nil }
func (m *InboxEvent) BeforeCreate(*gorm.DB) error    { ensureUUID(&m.ID); return nil }
func (m *DialogMessage) BeforeCreate(*gorm.DB) error { ensureUUID(&m.ID); return nil }
func (m *AgentNote) BeforeCreate(*gorm.DB) error     { ensureUUID(&m.ID); return nil }
func (m *OutboxEvent) BeforeCreate(*gorm.DB) error   { ensureUUID(&m.ID); return nil }
