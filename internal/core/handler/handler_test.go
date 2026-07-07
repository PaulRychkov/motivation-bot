package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/core/logic"
	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

type fakeService struct {
	profiles  map[int64]models.ChatProfile
	outcomes  map[uuid.UUID]string
	dialog    []models.DialogMessage
	pingUsed  int
	suppress  bool
	created   []models.AgentInterventionRequest
	pingLimit int
}

func newFakeService() *fakeService {
	return &fakeService{
		profiles:  map[int64]models.ChatProfile{},
		outcomes:  map[uuid.UUID]string{},
		pingLimit: 3,
	}
}

func (f *fakeService) EnsureProfile(_ context.Context, chatID int64) (models.ChatProfile, bool, error) {
	if p, ok := f.profiles[chatID]; ok {
		return p, false, nil
	}
	p := models.ChatProfile{ID: uuid.New(), ChatID: chatID, Timezone: "Europe/Moscow", DefaultTone: models.ToneNeutral}
	f.profiles[chatID] = p
	return p, true, nil
}

func (f *fakeService) ProfileByChatID(_ context.Context, chatID int64) (models.ChatProfile, error) {
	p, ok := f.profiles[chatID]
	if !ok {
		return p, models.ErrNotFound
	}
	return p, nil
}

func (f *fakeService) UpdateProfile(_ context.Context, chatID int64, patch map[string]any) (models.ChatProfile, error) {
	p, ok := f.profiles[chatID]
	if !ok {
		return p, models.ErrNotFound
	}
	updates, err := logic.ValidateProfilePatch(patch)
	if err != nil {
		return p, err
	}
	if tz, ok := updates["timezone"].(string); ok {
		p.Timezone = tz
	}
	f.profiles[chatID] = p
	return p, nil
}

func (f *fakeService) Interventions(context.Context, int64, int) ([]models.Intervention, error) {
	return nil, nil
}

func (f *fakeService) PendingInterventions(context.Context, int64) ([]models.Intervention, error) {
	return nil, nil
}

func (f *fakeService) SetInterventionOutcome(_ context.Context, id uuid.UUID, outcome string) error {
	f.outcomes[id] = outcome
	return nil
}

func (f *fakeService) CreateAgentIntervention(_ context.Context, in models.AgentInterventionRequest) (*models.Intervention, error) {
	if in.Kind != models.KindFreePing && in.Kind != models.KindQuestion {
		return nil, errors.New("kind должен быть free_ping или question")
	}
	if _, ok := f.profiles[in.ChatID]; !ok {
		return nil, models.ErrNotFound
	}
	if f.suppress || (in.Kind == models.KindFreePing && f.pingUsed >= f.pingLimit) {
		return nil, nil
	}
	if in.Kind == models.KindFreePing {
		f.pingUsed++
	}
	f.created = append(f.created, in)
	iv := models.Intervention{ID: uuid.New(), Kind: in.Kind, TriggeredBy: models.TriggerAgent}
	return &iv, nil
}

func (f *fakeService) DialogHistory(context.Context, int64, int) ([]models.DialogMessage, error) {
	return f.dialog, nil
}

func (f *fakeService) AppendDialogMessage(_ context.Context, chatID int64, in models.DialogAppend) (models.DialogMessage, error) {
	msg := models.DialogMessage{ID: uuid.New(), Role: in.Role, Content: in.Content}
	f.dialog = append(f.dialog, msg)
	return msg, nil
}

func (f *fakeService) Notes(context.Context) ([]models.AgentNote, error) { return nil, nil }

func (f *fakeService) UpsertNote(context.Context, string, json.RawMessage, *time.Time) error {
	return nil
}

func (f *fakeService) EffectivenessStats(context.Context, int64) ([]models.KindStat, error) {
	return []models.KindStat{{Kind: "free_ping", Outcome: "activated", Count: 2}}, nil
}

func setup(t *testing.T) (*fakeService, *gin.Engine) {
	t.Helper()
	svc := newFakeService()
	return svc, New(svc, zap.NewNop())
}

func doRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHealthz(t *testing.T) {
	_, r := setup(t)
	w := doRequest(r, http.MethodGet, "/healthz", "")
	if w.Code != http.StatusOK {
		t.Fatalf("healthz = %d", w.Code)
	}
}

func TestGetProfile(t *testing.T) {
	svc, r := setup(t)
	svc.profiles[42] = models.ChatProfile{ID: uuid.New(), ChatID: 42, Timezone: "Europe/Moscow"}

	tests := []struct {
		name string
		path string
		want int
	}{
		{"существующий профиль", "/api/v1/profiles/42", http.StatusOK},
		{"несуществующий профиль", "/api/v1/profiles/99", http.StatusNotFound},
		{"невалидный chat_id", "/api/v1/profiles/abc", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doRequest(r, http.MethodGet, tt.path, "")
			if w.Code != tt.want {
				t.Errorf("GET %s = %d, ожидалось %d: %s", tt.path, w.Code, tt.want, w.Body.String())
			}
		})
	}
}

func TestPatchProfile(t *testing.T) {
	svc, r := setup(t)
	svc.profiles[42] = models.ChatProfile{ID: uuid.New(), ChatID: 42, Timezone: "Europe/Moscow"}

	tests := []struct {
		name string
		body string
		want int
	}{
		{"валидный patch", `{"timezone":"Asia/Yekaterinburg"}`, http.StatusOK},
		{"невалидная таймзона", `{"timezone":"Mars/Olympus"}`, http.StatusBadRequest},
		{"неизвестное поле", `{"hacker_field":1}`, http.StatusBadRequest},
		{"тихие часы только парой", `{"quiet_start_min":1320}`, http.StatusBadRequest},
		{"тихие часы парой ок", `{"quiet_start_min":1320,"quiet_end_min":420}`, http.StatusOK},
		{"недопустимый тон", `{"default_tone":"sarcastic"}`, http.StatusBadRequest},
		{"минуты вне диапазона", `{"morning_plan_min":1500}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doRequest(r, http.MethodPatch, "/api/v1/profiles/42", tt.body)
			if w.Code != tt.want {
				t.Errorf("PATCH = %d, ожидалось %d: %s", w.Code, tt.want, w.Body.String())
			}
		})
	}

	w := doRequest(r, http.MethodPatch, "/api/v1/profiles/99", `{"timezone":"Europe/Moscow"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("PATCH несуществующего = %d, ожидалось 404", w.Code)
	}
}

func TestEnsureProfile(t *testing.T) {
	_, r := setup(t)
	w := doRequest(r, http.MethodPost, "/internal/v1/profiles/ensure", `{"chat_id":42}`)
	if w.Code != http.StatusOK {
		t.Fatalf("ensure = %d: %s", w.Code, w.Body.String())
	}
	var res models.EnsureProfileResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Profile.ChatID != 42 {
		t.Errorf("первый ensure должен создать профиль: %+v", res)
	}

	w = doRequest(r, http.MethodPost, "/internal/v1/profiles/ensure", `{"chat_id":42}`)
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.Created {
		t.Error("повторный ensure не должен создавать профиль")
	}

	w = doRequest(r, http.MethodPost, "/internal/v1/profiles/ensure", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("ensure без chat_id = %d, ожидалось 400", w.Code)
	}
}

func TestSetOutcome(t *testing.T) {
	svc, r := setup(t)
	id := uuid.New()

	tests := []struct {
		name    string
		path    string
		body    string
		want    int
		outcome string
	}{
		{"refused ок", "/internal/v1/interventions/" + id.String() + "/outcome", `{"outcome":"refused"}`, http.StatusOK, "refused"},
		{"rescheduled ок", "/internal/v1/interventions/" + id.String() + "/outcome", `{"outcome":"rescheduled"}`, http.StatusOK, "rescheduled"},
		{"pending запрещён", "/internal/v1/interventions/" + id.String() + "/outcome", `{"outcome":"pending"}`, http.StatusBadRequest, ""},
		{"мусорный outcome", "/internal/v1/interventions/" + id.String() + "/outcome", `{"outcome":"whatever"}`, http.StatusBadRequest, ""},
		{"невалидный id", "/internal/v1/interventions/not-a-uuid/outcome", `{"outcome":"refused"}`, http.StatusBadRequest, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doRequest(r, http.MethodPost, tt.path, tt.body)
			if w.Code != tt.want {
				t.Errorf("POST = %d, ожидалось %d: %s", w.Code, tt.want, w.Body.String())
			}
			if tt.outcome != "" && svc.outcomes[id] != tt.outcome {
				t.Errorf("outcome = %s, ожидалось %s", svc.outcomes[id], tt.outcome)
			}
		})
	}
}

func TestPostDialog(t *testing.T) {
	_, r := setup(t)
	tests := []struct {
		name string
		body string
		want int
	}{
		{"user с текстом", `{"role":"user","content":"привет"}`, http.StatusCreated},
		{"assistant с tool_calls", `{"role":"assistant","tool_calls":[{"id":"tc1"}]}`, http.StatusCreated},
		{"tool с tool_call_id", `{"role":"tool","content":"результат","tool_call_id":"tc1"}`, http.StatusCreated},
		{"tool без tool_call_id", `{"role":"tool","content":"результат"}`, http.StatusBadRequest},
		{"user с tool_call_id", `{"role":"user","content":"x","tool_call_id":"tc1"}`, http.StatusBadRequest},
		{"tool_calls у user", `{"role":"user","content":"x","tool_calls":[{}]}`, http.StatusBadRequest},
		{"пустое сообщение", `{"role":"user"}`, http.StatusBadRequest},
		{"неизвестная роль", `{"role":"system","content":"x"}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doRequest(r, http.MethodPost, "/internal/v1/dialog/42", tt.body)
			if w.Code != tt.want {
				t.Errorf("POST dialog = %d, ожидалось %d: %s", w.Code, tt.want, w.Body.String())
			}
		})
	}
}

func TestCreateAgentIntervention(t *testing.T) {
	svc, r := setup(t)
	svc.profiles[42] = models.ChatProfile{ID: uuid.New(), ChatID: 42, Timezone: "Europe/Moscow", DefaultTone: models.ToneNeutral}
	svc.pingLimit = 1

	tests := []struct {
		name       string
		body       string
		suppress   bool
		want       int
		wantStatus string
	}{
		{"free_ping создаётся", `{"chat_id":42,"kind":"free_ping"}`, false, http.StatusCreated, "created"},
		{"бюджет исчерпан — suppressed", `{"chat_id":42,"kind":"free_ping"}`, false, http.StatusOK, "suppressed"},
		{"question вне бюджета", `{"chat_id":42,"kind":"question"}`, false, http.StatusCreated, "created"},
		{"тихие часы — suppressed", `{"chat_id":42,"kind":"question"}`, true, http.StatusOK, "suppressed"},
		{"недопустимый kind", `{"chat_id":42,"kind":"morning_plan"}`, false, http.StatusBadRequest, ""},
		{"незнакомый chat_id", `{"chat_id":99,"kind":"free_ping"}`, false, http.StatusNotFound, ""},
		{"без chat_id", `{"kind":"free_ping"}`, false, http.StatusBadRequest, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc.suppress = tt.suppress
			w := doRequest(r, http.MethodPost, "/internal/v1/interventions", tt.body)
			if w.Code != tt.want {
				t.Fatalf("POST = %d, ожидалось %d: %s", w.Code, tt.want, w.Body.String())
			}
			if tt.wantStatus == "" {
				return
			}
			var res models.AgentInterventionResult
			if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
				t.Fatalf("парсинг ответа: %v", err)
			}
			if res.Status != tt.wantStatus {
				t.Errorf("status = %s, ожидалось %s", res.Status, tt.wantStatus)
			}
			if tt.wantStatus == "created" && res.Intervention == nil {
				t.Error("нет intervention в ответе")
			}
		})
	}
}

func TestStats(t *testing.T) {
	_, r := setup(t)
	w := doRequest(r, http.MethodGet, "/internal/v1/stats/effectiveness?chat_id=42", "")
	if w.Code != http.StatusOK {
		t.Fatalf("stats = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "free_ping") {
		t.Errorf("нет статистики в ответе: %s", w.Body.String())
	}
}
