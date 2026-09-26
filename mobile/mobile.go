package mobile

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	_ "time/tzdata"

	"github.com/gorilla/websocket"
	"github.com/ncruces/go-sqlite3/gormlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	bot "github.com/PaulRychkov/motivation-bot"
	"github.com/PaulRychkov/motivation-bot/internal/agent/coreclient"
	"github.com/PaulRychkov/motivation-bot/internal/agent/openrouter"
	"github.com/PaulRychkov/motivation-bot/internal/agent/prompts"
	"github.com/PaulRychkov/motivation-bot/internal/agent/react"
	"github.com/PaulRychkov/motivation-bot/internal/agent/run"
	"github.com/PaulRychkov/motivation-bot/internal/agent/tools"
	"github.com/PaulRychkov/motivation-bot/internal/config"
	"github.com/PaulRychkov/motivation-bot/internal/contracts"
	"github.com/PaulRychkov/motivation-bot/internal/core/app"
	"github.com/PaulRychkov/motivation-bot/internal/core/handler"
	"github.com/PaulRychkov/motivation-bot/internal/inproc"
	"github.com/PaulRychkov/motivation-bot/internal/kafkax"
	migrationssqlite "github.com/PaulRychkov/motivation-bot/migrations_sqlite"
	"github.com/PaulRychkov/motivation-bot/internal/sqlitemigrate"
)

//go:embed all:webdist
var webFS embed.FS

const (
	uiAddr   = "127.0.0.1:18086"
	coreAddr = "127.0.0.1:18085"
)

var (
	mu      sync.Mutex
	uiSrv   *http.Server
	coreSrv *http.Server
	cancel  context.CancelFunc
	msgSeq  int64
)

type chatEvent struct {
	Type      string    `json:"type"`
	Role      string    `json:"role"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

type hub struct {
	mu    sync.Mutex
	conns map[*websocket.Conn]bool
}

func (h *hub) add(c *websocket.Conn)    { h.mu.Lock(); h.conns[c] = true; h.mu.Unlock() }
func (h *hub) remove(c *websocket.Conn) { h.mu.Lock(); delete(h.conns, c); h.mu.Unlock(); _ = c.Close() }
func (h *hub) broadcast(v any) {
	b, _ := json.Marshal(v)
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := c.WriteMessage(websocket.TextMessage, b); err != nil {
			delete(h.conns, c)
			_ = c.Close()
		}
	}
}

// Start поднимает весь бот одним процессом: SQLite + внутрипроцессная
// шина вместо Kafka + core (планировщик, поллеры, интервенции) + agent
// (ReAct, LLM, MCP к локальным tasks/pomodoro) + чат UI. Возвращает
// пустую строку при успехе или текст ошибки.
func Start(dataDir, openrouterKey, model, tasksMCP, pomodoroMCP string) string {
	mu.Lock()
	defer mu.Unlock()
	if uiSrv != nil {
		return ""
	}
	log, err := zap.NewProduction()
	if err != nil {
		return "logger: " + err.Error()
	}

	cfg := config.Load()
	cfg.OpenRouterAPIKey = openrouterKey
	if model != "" {
		cfg.LLMModel = model
	}
	cfg.ChatChatID = 1
	if tasksMCP != "" {
		cfg.TasksMCPURL = tasksMCP
	}
	if pomodoroMCP != "" {
		cfg.PomodoroMCPURL = pomodoroMCP
	}
	cfg.CoreURL = "http://" + coreAddr

	dbPath := "file:" + strings.ReplaceAll(filepath.Join(dataDir, "bot.db"), "\\", "/") +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := gorm.Open(gormlite.Open(dbPath), &gorm.Config{
		TranslateError: true,
		Logger:         gormlogger.Default.LogMode(gormlogger.Silent),
		NowFunc:        func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return "open db: " + err.Error()
	}
	sqlDB, err := db.DB()
	if err != nil {
		return "unwrap db: " + err.Error()
	}
	if err := sqlitemigrate.Up(sqlDB, migrationssqlite.FS); err != nil {
		return "migrate: " + err.Error()
	}

	ctx, stop := context.WithCancel(context.Background())
	cancel = stop
	bus := inproc.New(ctx, log)

	a := app.New(db, bus, cfg, log)
	bus.Subscribe(kafkax.TopicTasksEvents, a.HandleKafka)
	bus.Subscribe(kafkax.TopicPomodoroEvents, a.HandleKafka)
	bus.Subscribe(kafkax.TopicPhoneEvents, a.HandleKafka)
	bus.Subscribe(kafkax.TopicAgentResponses, a.HandleKafka)
	bus.Subscribe(kafkax.TopicTgSent, a.HandleKafka)

	go a.RunScheduler(ctx)
	go a.RunDeadlinePoller(ctx)
	go a.RunProactivePoller(ctx)
	go a.RunDispatcher(ctx)
	go a.RunWindowWorker(ctx)
	go a.RunOutboxRelay(ctx)
	go a.RunRetention(ctx)

	coreSrv = &http.Server{Addr: coreAddr, Handler: handler.New(a, log, cfg.PhoneIngestToken)}
	go func() {
		if err := coreSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("core http", zap.Error(err))
		}
	}()

	llm := openrouter.New(cfg.OpenRouterAPIKey, cfg.OpenRouterBaseURL)
	store := prompts.New(cfg.PromptDir, bot.PromptsFS, run.Fallbacks)
	coreCli := coreclient.New(cfg.CoreURL)
	tasksSrc := tools.NewMCP("tasks", cfg.TasksMCPURL, log)
	pomoSrc := tools.NewMCP("pomodoro", cfg.PomodoroMCPURL, log)
	engine := &react.Engine{
		LLM:           llm,
		Model:         cfg.LLMModel,
		MaxIterations: cfg.ReactMaxIter,
		StepTimeout:   time.Duration(cfg.LLMTimeoutSec) * time.Second,
		Temperature:   openrouter.Float(0.7),
		MaxTokens:     cfg.LLMMaxTokens,
	}
	svc := &run.Service{
		Cfg:     cfg,
		Log:     log,
		Core:    coreCli,
		Prod:    bus,
		Engine:  engine,
		LLM:     llm,
		Prompts: store,
		Shared:  []tools.Source{tasksSrc, pomoSrc},
	}
	bus.Subscribe(kafkax.TopicTgUpdates, svc.HandleMessage)
	bus.Subscribe(kafkax.TopicAgentRequests, svc.HandleMessage)

	h := &hub{conns: map[*websocket.Conn]bool{}}
	// доставка: bot.tg-outgoing → показать в чате + подтвердить tg-sent (как telegram-service)
	bus.Subscribe(kafkax.TopicTgOutgoing, func(_ context.Context, _ string, _, value []byte) error {
		var out contracts.TgOutgoing
		if err := json.Unmarshal(value, &out); err != nil {
			return nil
		}
		h.broadcast(chatEvent{Type: "message", Role: "assistant", Text: out.Text, CreatedAt: time.Now().UTC()})
		sent := contracts.TgSent{CorrelationID: out.CorrelationID, ChatID: out.ChatID, MessageID: atomic.AddInt64(&msgSeq, 1)}
		b, _ := json.Marshal(sent)
		return bus.Send(kafkax.TopicTgSent, fmt.Sprint(out.ChatID), b)
	})

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		h.add(conn)
		go func() {
			defer h.remove(conn)
			for {
				_, raw, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var in struct {
					Text string `json:"text"`
				}
				if json.Unmarshal(raw, &in) != nil || strings.TrimSpace(in.Text) == "" {
					continue
				}
				now := time.Now().UTC()
				upd := contracts.TgUpdate{UpdateID: now.UnixNano(), ChatID: cfg.ChatChatID, MessageID: now.UnixNano(), Text: in.Text, Date: now}
				b, _ := json.Marshal(upd)
				if err := bus.Send(kafkax.TopicTgUpdates, fmt.Sprint(cfg.ChatChatID), b); err != nil {
					continue
				}
				h.broadcast(chatEvent{Type: "message", Role: "user", Text: in.Text, CreatedAt: now})
			}
		}()
	})
	mux.HandleFunc("/api/v1/history", func(w http.ResponseWriter, r *http.Request) {
		limit := r.URL.Query().Get("limit")
		if limit == "" {
			limit = "50"
		}
		url := fmt.Sprintf("%s/internal/v1/dialog/%d?limit=%s", cfg.CoreURL, cfg.ChatChatID, limit)
		resp, err := http.Get(url)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
	})
	static, _ := fs.Sub(webFS, "webdist")
	fileServer := http.FileServer(http.FS(static))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(static, p); err != nil {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})

	uiSrv = &http.Server{Addr: uiAddr, Handler: mux}
	go func() {
		if err := uiSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("ui http", zap.Error(err))
		}
	}()
	log.Info("bot-mobile запущен", zap.String("ui", uiAddr), zap.String("model", cfg.LLMModel))
	return ""
}

func Stop() {
	mu.Lock()
	defer mu.Unlock()
	if uiSrv == nil {
		return
	}
	if cancel != nil {
		cancel()
	}
	shutdownCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()
	_ = uiSrv.Shutdown(shutdownCtx)
	_ = coreSrv.Shutdown(shutdownCtx)
	uiSrv = nil
}

func BaseURL() string { return "http://" + uiAddr + "/" }
