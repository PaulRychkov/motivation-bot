package models

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("not found")

type DialogAppend struct {
	Role                     string          `json:"role"`
	Content                  *string         `json:"content"`
	ToolCalls                json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID               *string         `json:"tool_call_id,omitempty"`
	InterventionID           *uuid.UUID      `json:"intervention_id,omitempty"`
	ReplyToTelegramMessageID *int64          `json:"reply_to_telegram_message_id,omitempty"`
}

type KindStat struct {
	Kind    string `json:"kind"`
	Outcome string `json:"outcome"`
	Count   int64  `json:"count"`
}

type NoteUpsert struct {
	Value     json.RawMessage `json:"value"`
	ExpiresAt *time.Time      `json:"expires_at,omitempty"`
}

type EnsureProfileResult struct {
	Profile ChatProfile `json:"profile"`
	Created bool        `json:"created"`
}

type AgentInterventionRequest struct {
	ChatID         int64          `json:"chat_id"`
	Kind           string         `json:"kind"`
	TaskSource     *string        `json:"task_source,omitempty"`
	TaskExternalID *string        `json:"task_external_id,omitempty"`
	TargetDate     *string        `json:"target_date,omitempty"`
	Context        map[string]any `json:"context,omitempty"`
}

type AgentInterventionResult struct {
	Status       string        `json:"status"`
	Intervention *Intervention `json:"intervention,omitempty"`
}
