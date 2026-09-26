package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/bootstrap"
	"github.com/PaulRychkov/motivation-bot/internal/config"
	"github.com/PaulRychkov/motivation-bot/internal/kafkax"
	"github.com/PaulRychkov/motivation-bot/internal/logger"
	"github.com/PaulRychkov/motivation-bot/internal/telegram"
)

func main() {
	cfg := config.Load()
	log, err := logger.New("telegram-service")
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		os.Exit(1)
	}
	defer log.Sync()

	if cfg.TelegramToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN не задан")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	prod, err := bootstrap.Producer(ctx, cfg.KafkaBrokers, log)
	if err != nil {
		log.Fatal("kafka producer", zap.Error(err))
	}
	defer prod.Close()

	svc, err := telegram.New(cfg.TelegramToken, prod, log)
	if err != nil {
		log.Fatal("telegram bot", zap.Error(err))
	}

	consumer, err := bootstrap.Consumer(ctx, cfg.KafkaBrokers, "bot-telegram-group",
		[]string{kafkax.TopicTgOutgoing}, svc.HandleOutgoing, log)
	if err != nil {
		log.Fatal("kafka consumer", zap.Error(err))
	}
	defer consumer.Close()

	go consumer.Run(ctx)

	srv := &http.Server{Addr: ":8084", Handler: svc.HealthHandler()}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("health-сервер", zap.Error(err))
		}
	}()
	defer func() {
		shutdownCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		srv.Shutdown(shutdownCtx)
	}()

	log.Info("telegram-service запущен, long polling начат")
	svc.Bot.Start(ctx)
	log.Info("остановка telegram-service")
}
