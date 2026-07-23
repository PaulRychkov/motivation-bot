package pomoclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	base string
	http *http.Client
}

func New(baseURL string) *Client {
	return &Client{base: baseURL, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) DayPlan(ctx context.Context) ([]map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/plan", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pomodoro request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read pomodoro response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pomodoro %s: %s", resp.Status, string(body))
	}
	var wrapped struct {
		Slots []map[string]any `json:"slots"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, fmt.Errorf("parse pomodoro response: %w", err)
	}
	return wrapped.Slots, nil
}
