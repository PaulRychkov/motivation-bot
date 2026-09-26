package telegram

import (
	"context"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	healthTTL     = 30 * time.Second
	healthTimeout = 20 * time.Second
)

type health struct {
	svc *Service

	mu        sync.Mutex
	checkedAt time.Time
	alive     bool
}

func (s *Service) HealthHandler() http.Handler {
	h := &health{svc: s}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.serve)
	return mux
}

func (h *health) serve(w http.ResponseWriter, r *http.Request) {
	if h.check(r.Context()) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte("telegram unreachable"))
}

func (h *health) check(ctx context.Context) bool {
	h.mu.Lock()
	if time.Since(h.checkedAt) < healthTTL {
		alive := h.alive
		h.mu.Unlock()
		return alive
	}
	h.mu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	_, err := h.svc.Bot.GetMe(callCtx)

	h.mu.Lock()
	h.checkedAt = time.Now()
	h.alive = err == nil
	h.mu.Unlock()

	if err != nil {
		h.svc.Log.Warn("проверка живости telegram не прошла", zap.Error(err))
	}
	return err == nil
}
