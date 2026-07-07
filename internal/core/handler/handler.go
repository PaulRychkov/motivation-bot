package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

type Service interface {
	EnsureProfile(ctx context.Context, chatID int64) (models.ChatProfile, bool, error)
	ProfileByChatID(ctx context.Context, chatID int64) (models.ChatProfile, error)
	UpdateProfile(ctx context.Context, chatID int64, patch map[string]any) (models.ChatProfile, error)
	Interventions(ctx context.Context, chatID int64, limit int) ([]models.Intervention, error)
	PendingInterventions(ctx context.Context, chatID int64) ([]models.Intervention, error)
	SetInterventionOutcome(ctx context.Context, id uuid.UUID, outcome string) error
	CreateAgentIntervention(ctx context.Context, in models.AgentInterventionRequest) (*models.Intervention, error)
	DialogHistory(ctx context.Context, chatID int64, limit int) ([]models.DialogMessage, error)
	AppendDialogMessage(ctx context.Context, chatID int64, in models.DialogAppend) (models.DialogMessage, error)
	Notes(ctx context.Context) ([]models.AgentNote, error)
	UpsertNote(ctx context.Context, key string, value json.RawMessage, expiresAt *time.Time) error
	EffectivenessStats(ctx context.Context, chatID int64) ([]models.KindStat, error)
}

var allowedOutcomes = map[string]bool{
	models.OutcomeActivated:   true,
	models.OutcomePartial:     true,
	models.OutcomeIgnored:     true,
	models.OutcomeRefused:     true,
	models.OutcomeRescheduled: true,
}

var allowedRoles = map[string]bool{
	models.RoleUser:      true,
	models.RoleAssistant: true,
	models.RoleTool:      true,
}

type Handler struct {
	svc Service
	log *zap.Logger
}

func New(svc Service, log *zap.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	h := &Handler{svc: svc, log: log}
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := r.Group("/api/v1")
	api.GET("/profiles/:chat_id", h.getProfile)
	api.PATCH("/profiles/:chat_id", h.patchProfile)
	api.GET("/interventions", h.listInterventions)

	in := r.Group("/internal/v1")
	in.POST("/profiles/ensure", h.ensureProfile)
	in.GET("/dialog/:chat_id", h.getDialog)
	in.POST("/dialog/:chat_id", h.postDialog)
	in.GET("/interventions/pending", h.pendingInterventions)
	in.POST("/interventions", h.createIntervention)
	in.POST("/interventions/:id/outcome", h.setOutcome)
	in.GET("/notes", h.listNotes)
	in.PUT("/notes/:key", h.putNote)
	in.GET("/stats/effectiveness", h.stats)

	return r
}

func errResp(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func (h *Handler) fail(c *gin.Context, err error) {
	if errors.Is(err, models.ErrNotFound) {
		errResp(c, http.StatusNotFound, "not_found", "объект не найден")
		return
	}
	h.log.Error("internal error", zap.Error(err))
	errResp(c, http.StatusInternalServerError, "internal", err.Error())
}

func chatIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("chat_id"), 10, 64)
	if err != nil {
		errResp(c, http.StatusBadRequest, "bad_chat_id", "chat_id должен быть целым числом")
		return 0, false
	}
	return id, true
}

func chatIDQuery(c *gin.Context) (int64, bool) {
	raw := c.Query("chat_id")
	if raw == "" {
		return 0, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		errResp(c, http.StatusBadRequest, "bad_chat_id", "chat_id должен быть целым числом")
		return 0, false
	}
	return id, true
}

func (h *Handler) getProfile(c *gin.Context) {
	chatID, ok := chatIDParam(c)
	if !ok {
		return
	}
	p, err := h.svc.ProfileByChatID(c.Request.Context(), chatID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) patchProfile(c *gin.Context) {
	chatID, ok := chatIDParam(c)
	if !ok {
		return
	}
	var patch map[string]any
	if err := c.ShouldBindJSON(&patch); err != nil {
		errResp(c, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	p, err := h.svc.UpdateProfile(c.Request.Context(), chatID, patch)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			errResp(c, http.StatusNotFound, "not_found", "профиль не найден")
			return
		}
		errResp(c, http.StatusBadRequest, "bad_patch", err.Error())
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) ensureProfile(c *gin.Context) {
	var body struct {
		ChatID int64 `json:"chat_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.ChatID == 0 {
		errResp(c, http.StatusBadRequest, "bad_json", "нужен chat_id")
		return
	}
	p, created, err := h.svc.EnsureProfile(c.Request.Context(), body.ChatID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, models.EnsureProfileResult{Profile: p, Created: created})
}

func (h *Handler) listInterventions(c *gin.Context) {
	chatID, ok := chatIDQuery(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	out, err := h.svc.Interventions(c.Request.Context(), chatID, limit)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"interventions": out})
}

func (h *Handler) pendingInterventions(c *gin.Context) {
	chatID, ok := chatIDQuery(c)
	if !ok {
		return
	}
	out, err := h.svc.PendingInterventions(c.Request.Context(), chatID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"interventions": out})
}

func (h *Handler) createIntervention(c *gin.Context) {
	var in models.AgentInterventionRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		errResp(c, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	if in.ChatID == 0 {
		errResp(c, http.StatusBadRequest, "bad_chat_id", "нужен chat_id")
		return
	}
	iv, err := h.svc.CreateAgentIntervention(c.Request.Context(), in)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			errResp(c, http.StatusNotFound, "not_found", "профиль не найден")
			return
		}
		errResp(c, http.StatusBadRequest, "bad_intervention", err.Error())
		return
	}
	if iv == nil {
		c.JSON(http.StatusOK, models.AgentInterventionResult{Status: "suppressed"})
		return
	}
	c.JSON(http.StatusCreated, models.AgentInterventionResult{Status: "created", Intervention: iv})
}

func (h *Handler) setOutcome(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errResp(c, http.StatusBadRequest, "bad_id", "id должен быть UUID")
		return
	}
	var body struct {
		Outcome string `json:"outcome"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errResp(c, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	if !allowedOutcomes[body.Outcome] {
		errResp(c, http.StatusBadRequest, "bad_outcome", "недопустимый outcome: "+body.Outcome)
		return
	}
	if err := h.svc.SetInterventionOutcome(c.Request.Context(), id, body.Outcome); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) getDialog(c *gin.Context) {
	chatID, ok := chatIDParam(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	msgs, err := h.svc.DialogHistory(c.Request.Context(), chatID, limit)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": msgs})
}

func (h *Handler) postDialog(c *gin.Context) {
	chatID, ok := chatIDParam(c)
	if !ok {
		return
	}
	var in models.DialogAppend
	if err := c.ShouldBindJSON(&in); err != nil {
		errResp(c, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	if !allowedRoles[in.Role] {
		errResp(c, http.StatusBadRequest, "bad_role", "недопустимая роль: "+in.Role)
		return
	}
	if in.Content == nil && len(in.ToolCalls) == 0 {
		errResp(c, http.StatusBadRequest, "empty_message", "нужен content или tool_calls")
		return
	}
	if (in.Role == models.RoleTool) != (in.ToolCallID != nil) {
		errResp(c, http.StatusBadRequest, "bad_tool_call_id", "tool_call_id обязателен для role=tool и запрещён иначе")
		return
	}
	if len(in.ToolCalls) > 0 && in.Role != models.RoleAssistant {
		errResp(c, http.StatusBadRequest, "bad_tool_calls", "tool_calls допустимы только у assistant")
		return
	}
	msg, err := h.svc.AppendDialogMessage(c.Request.Context(), chatID, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, msg)
}

func (h *Handler) listNotes(c *gin.Context) {
	notes, err := h.svc.Notes(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"notes": notes})
}

func (h *Handler) putNote(c *gin.Context) {
	key := c.Param("key")
	var body models.NoteUpsert
	if err := c.ShouldBindJSON(&body); err != nil {
		errResp(c, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	if len(body.Value) == 0 {
		errResp(c, http.StatusBadRequest, "empty_value", "нужно поле value")
		return
	}
	if err := h.svc.UpsertNote(c.Request.Context(), key, body.Value, body.ExpiresAt); err != nil {
		errResp(c, http.StatusBadRequest, "bad_note", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) stats(c *gin.Context) {
	chatID, ok := chatIDQuery(c)
	if !ok {
		return
	}
	stats, err := h.svc.EffectivenessStats(c.Request.Context(), chatID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"stats": stats})
}
