package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/bootstrap"
	"github.com/PaulRychkov/motivation-bot/internal/config"
	"github.com/PaulRychkov/motivation-bot/internal/contracts"
	"github.com/PaulRychkov/motivation-bot/internal/kafkax"
	"github.com/PaulRychkov/motivation-bot/internal/logger"
)

//go:embed all:webdist
var webFS embed.FS

type wsHub struct {
	mu    sync.Mutex
	conns map[*websocket.Conn]bool
}

func (h *wsHub) add(c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[c] = true
}

func (h *wsHub) remove(c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns, c)
	_ = c.Close()
}

func (h *wsHub) broadcast(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
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

type chatEvent struct {
	Type      string    `json:"type"`
	Role      string    `json:"role"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

func main() {
	cfg := config.Load()
	log, err := logger.New("chat-service")
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		os.Exit(1)
	}
	defer log.Sync()

	if cfg.ChatChatID == 0 {
		log.Fatal("BOT_CHAT_CHAT_ID обязателен: chat_id профиля, с которым работает чат")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	prod, err := bootstrap.Producer(ctx, cfg.KafkaBrokers, log)
	if err != nil {
		log.Fatal("kafka producer", zap.Error(err))
	}
	defer prod.Close()

	hub := &wsHub{conns: map[*websocket.Conn]bool{}}

	consumer, err := bootstrap.Consumer(ctx, cfg.KafkaBrokers, "bot-chat-group", []string{kafkax.TopicTgOutgoing},
		func(ctx context.Context, topic string, key, value []byte) error {
			var out contracts.TgOutgoing
			if err := json.Unmarshal(value, &out); err != nil {
				return nil
			}
			if out.ChatID != cfg.ChatChatID {
				return nil
			}
			hub.broadcast(chatEvent{Type: "message", Role: "assistant", Text: out.Text, CreatedAt: time.Now().UTC()})
			return nil
		}, log)
	if err != nil {
		log.Fatal("kafka consumer", zap.Error(err))
	}
	defer consumer.Close()
	go consumer.Run(ctx)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	authorized := func(c *gin.Context) bool {
		if cfg.ChatToken == "" {
			return true
		}
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.TrimPrefix(header, "Bearer ") == cfg.ChatToken {
			return true
		}
		return c.Query("token") == cfg.ChatToken
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	r.GET("/ws", func(c *gin.Context) {
		if !authorized(c) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		hub.add(conn)
		log.Info("чат-клиент подключился")
		go func() {
			defer hub.remove(conn)
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
				upd := contracts.TgUpdate{
					UpdateID:  now.UnixNano(),
					ChatID:    cfg.ChatChatID,
					MessageID: now.UnixNano(),
					Text:      in.Text,
					Date:      now,
				}
				b, err := json.Marshal(upd)
				if err != nil {
					continue
				}
				if err := prod.Send(kafkax.TopicTgUpdates, fmt.Sprint(cfg.ChatChatID), b); err != nil {
					log.Warn("публикация сообщения из чата", zap.Error(err))
					continue
				}
				hub.broadcast(chatEvent{Type: "message", Role: "user", Text: in.Text, CreatedAt: now})
			}
		}()
	})

	r.GET("/api/v1/history", func(c *gin.Context) {
		if !authorized(c) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		limit := c.DefaultQuery("limit", "50")
		url := fmt.Sprintf("%s/internal/v1/dialog/%d?limit=%s", cfg.CoreURL, cfg.ChatChatID, limit)
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, url, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		c.Data(resp.StatusCode, "application/json", body)
	})

	static, err := fs.Sub(webFS, "webdist")
	if err == nil {
		fileServer := http.FileServer(http.FS(static))
		r.NoRoute(func(c *gin.Context) {
			p := strings.TrimPrefix(c.Request.URL.Path, "/")
			if p == "" {
				p = "index.html"
			}
			if _, err := fs.Stat(static, p); err != nil {
				c.Request.URL.Path = "/"
			}
			fileServer.ServeHTTP(c.Writer, c.Request)
		})
	}

	srv := &http.Server{Addr: fmt.Sprintf(":%d", cfg.ChatPort), Handler: r}
	go func() {
		log.Info("chat-service запущен", zap.Int("port", cfg.ChatPort))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", zap.Error(err))
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("остановка chat-service")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
