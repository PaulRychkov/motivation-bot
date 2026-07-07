package contracts

import "time"

type TgUpdate struct {
	UpdateID         int64     `json:"update_id"`
	ChatID           int64     `json:"chat_id"`
	MessageID        int64     `json:"message_id"`
	Text             string    `json:"text"`
	Username         string    `json:"username,omitempty"`
	ReplyToMessageID *int64    `json:"reply_to_message_id,omitempty"`
	Date             time.Time `json:"date"`
}

type TgOutgoing struct {
	CorrelationID    string `json:"correlation_id"`
	ChatID           int64  `json:"chat_id"`
	Text             string `json:"text"`
	ReplyToMessageID *int64 `json:"reply_to_message_id,omitempty"`
}

type TgSent struct {
	CorrelationID string `json:"correlation_id"`
	ChatID        int64  `json:"chat_id"`
	MessageID     int64  `json:"message_id"`
}

type AgentRequest struct {
	CorrelationID string         `json:"correlation_id"`
	Kind          string         `json:"kind"`
	Context       map[string]any `json:"context"`
}

type AgentResponse struct {
	CorrelationID string `json:"correlation_id"`
	Text          string `json:"text"`
	Tone          string `json:"tone"`
}
