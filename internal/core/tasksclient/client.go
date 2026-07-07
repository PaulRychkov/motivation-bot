package tasksclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	base string
	http *http.Client
}

func New(baseURL string) *Client {
	return &Client{base: baseURL, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) Tasks(ctx context.Context) ([]map[string]any, error) {
	return c.getList(ctx, c.base+"/api/v1/tasks", "tasks")
}

func (c *Client) Occurrences(ctx context.Context, from, to, status string) ([]map[string]any, error) {
	q := url.Values{}
	q.Set("from", from)
	q.Set("to", to)
	if status != "" {
		q.Set("status", status)
	}
	return c.getList(ctx, c.base+"/api/v1/occurrences?"+q.Encode(), "occurrences")
}

func (c *Client) getList(ctx context.Context, rawURL, itemsKey string) ([]map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tasks request %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read tasks response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tasks %s: %s", resp.Status, string(body))
	}
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err == nil {
		return arr, nil
	}
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, fmt.Errorf("parse tasks response: %w", err)
	}
	for _, key := range []string{itemsKey, "items", "data"} {
		raw, ok := wrapped[key]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, fmt.Errorf("parse tasks response %s: %w", key, err)
		}
		return arr, nil
	}
	return nil, fmt.Errorf("unexpected tasks response shape")
}
