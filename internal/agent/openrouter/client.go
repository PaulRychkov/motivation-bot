package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

func New(apiKey, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
	Tools       []Tool    `json:"tools,omitempty"`
	ToolChoice  any       `json:"tool_choice,omitempty"`
}

func Float(v float64) *float64 { return &v }

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) Chat(ctx context.Context, in ChatRequest) (Message, error) {
	in.Stream = false
	body, err := json.Marshal(in)
	if err != nil {
		return Message{}, fmt.Errorf("marshal chat request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, fmt.Errorf("build chat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "activization-bot")

	resp, err := c.http.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("openrouter request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, fmt.Errorf("read openrouter response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Message{}, fmt.Errorf("openrouter %s: %s", resp.Status, truncate(string(raw), 500))
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return Message{}, fmt.Errorf("parse openrouter response: %w (%s)", err, truncate(string(raw), 300))
	}
	if cr.Error != nil {
		return Message{}, fmt.Errorf("openrouter: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return Message{}, fmt.Errorf("openrouter: пустой ответ")
	}
	msg := cr.Choices[0].Message
	if msg.Role == "" {
		msg.Role = "assistant"
	}
	return msg, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
