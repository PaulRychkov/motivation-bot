package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot"
	"github.com/PaulRychkov/motivation-bot/internal/agent/coreclient"
	"github.com/PaulRychkov/motivation-bot/internal/agent/openrouter"
	"github.com/PaulRychkov/motivation-bot/internal/agent/prompts"
	"github.com/PaulRychkov/motivation-bot/internal/agent/react"
	"github.com/PaulRychkov/motivation-bot/internal/agent/run"
	"github.com/PaulRychkov/motivation-bot/internal/agent/tools"
	"github.com/PaulRychkov/motivation-bot/internal/bootstrap"
	"github.com/PaulRychkov/motivation-bot/internal/config"
	"github.com/PaulRychkov/motivation-bot/internal/kafkax"
	"github.com/PaulRychkov/motivation-bot/internal/logger"
)

func main() {
	cfg := config.Load()
	log, err := logger.New("agent-service")
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		os.Exit(1)
	}
	defer log.Sync()

	if cfg.OpenRouterAPIKey == "" {
		log.Warn("OPENROUTER_API_KEY не задан — LLM-вызовы будут падать в фолбэк")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	prod, err := bootstrap.Producer(ctx, cfg.KafkaBrokers, log)
	if err != nil {
		log.Fatal("kafka producer", zap.Error(err))
	}
	defer prod.Close()

	llm := openrouter.New(cfg.OpenRouterAPIKey, cfg.OpenRouterBaseURL)
	store := prompts.New(cfg.PromptDir, bot.PromptsFS, run.Fallbacks)
	core := coreclient.New(cfg.CoreURL)

	tasksMCP := tools.NewMCP("tasks", cfg.TasksMCPURL, log)
	pomodoroMCP := tools.NewMCP("pomodoro", cfg.PomodoroMCPURL, log)
	defer tasksMCP.Close()
	defer pomodoroMCP.Close()

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
		Core:    core,
		Prod:    prod,
		Engine:  engine,
		LLM:     llm,
		Prompts: store,
		Shared:  []tools.Source{tasksMCP, pomodoroMCP},
	}

	topics := []string{kafkax.TopicTgUpdates, kafkax.TopicAgentRequests}
	consumer, err := bootstrap.Consumer(ctx, cfg.KafkaBrokers, "bot-agent-group", topics, svc.HandleMessage, log)
	if err != nil {
		log.Fatal("kafka consumer", zap.Error(err))
	}
	defer consumer.Close()

	log.Info("agent-service запущен",
		zap.String("model", cfg.LLMModel),
		zap.String("core", cfg.CoreURL))

	if err := consumer.Run(ctx); err != nil {
		log.Error("consumer", zap.Error(err))
	}
	log.Info("остановка agent-service")
}
