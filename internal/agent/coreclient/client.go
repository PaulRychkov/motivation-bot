package coreclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

type Client struct {
	base string
	http *http.Client
}

func New(baseURL string) *Client {
	return &Client{base: baseURL, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) EnsureProfile(ctx context.Context, chatID int64) (models.ChatProfile, bool, error) {
	var res models.EnsureProfileResult
	err := c.do(ctx, http.MethodPost, "/internal/v1/profiles/ensure", map[string]any{"chat_id": chatID}, &res)
	return res.Profile, res.Created, err
}

func (c *Client) Profile(ctx context.Context, chatID int64) (models.ChatProfile, error) {
	var p models.ChatProfile
	err := c.do(ctx, http.MethodGet, "/api/v1/profiles/"+strconv.FormatInt(chatID, 10), nil, &p)
	return p, err
}

func (c *Client) UpdateProfile(ctx context.Context, chatID int64, patch map[string]any) (models.ChatProfile, error) {
	var p models.ChatProfile
	err := c.do(ctx, http.MethodPatch, "/api/v1/profiles/"+strconv.FormatInt(chatID, 10), patch, &p)
	return p, err
}

func (c *Client) Dialog(ctx context.Context, chatID int64, limit int) ([]models.DialogMessage, error) {
	var res struct {
		Messages []models.DialogMessage `json:"messages"`
	}
	path := fmt.Sprintf("/internal/v1/dialog/%d?limit=%d", chatID, limit)
	err := c.do(ctx, http.MethodGet, path, nil, &res)
	return res.Messages, err
}

func (c *Client) AppendDialog(ctx context.Context, chatID int64, in models.DialogAppend) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/internal/v1/dialog/%d", chatID), in, nil)
}

func (c *Client) PendingInterventions(ctx context.Context, chatID int64) ([]models.Intervention, error) {
	var res struct {
		Interventions []models.Intervention `json:"interventions"`
	}
	path := "/internal/v1/interventions/pending"
	if chatID != 0 {
		path += "?chat_id=" + strconv.FormatInt(chatID, 10)
	}
	err := c.do(ctx, http.MethodGet, path, nil, &res)
	return res.Interventions, err
}

func (c *Client) CreateIntervention(ctx context.Context, in models.AgentInterventionRequest) (models.AgentInterventionResult, error) {
	var res models.AgentInterventionResult
	err := c.do(ctx, http.MethodPost, "/internal/v1/interventions", in, &res)
	return res, err
}

func (c *Client) SetInterventionOutcome(ctx context.Context, id uuid.UUID, outcome string) error {
	path := "/internal/v1/interventions/" + id.String() + "/outcome"
	return c.do(ctx, http.MethodPost, path, map[string]any{"outcome": outcome}, nil)
}

func (c *Client) Notes(ctx context.Context) ([]models.AgentNote, error) {
	var res struct {
		Notes []models.AgentNote `json:"notes"`
	}
	err := c.do(ctx, http.MethodGet, "/internal/v1/notes", nil, &res)
	return res.Notes, err
}

func (c *Client) SaveNote(ctx context.Context, key string, value json.RawMessage, expiresAt *time.Time) error {
	body := models.NoteUpsert{Value: value, ExpiresAt: expiresAt}
	return c.do(ctx, http.MethodPut, "/internal/v1/notes/"+url.PathEscape(key), body, nil)
}

func (c *Client) Stats(ctx context.Context, chatID int64) ([]models.KindStat, error) {
	var res struct {
		Stats []models.KindStat `json:"stats"`
	}
	path := "/internal/v1/stats/effectiveness"
	if chatID != 0 {
		path += "?chat_id=" + strconv.FormatInt(chatID, 10)
	}
	err := c.do(ctx, http.MethodGet, path, nil, &res)
	return res.Stats, err
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("core request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read core response: %w", err)
	}
	if resp.StatusCode >= 300 {
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &envelope) == nil && envelope.Error.Message != "" {
			return fmt.Errorf("core %s: %s (%s)", resp.Status, envelope.Error.Message, envelope.Error.Code)
		}
		return fmt.Errorf("core %s: %s", resp.Status, string(raw))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("parse core response: %w", err)
	}
	return nil
}
