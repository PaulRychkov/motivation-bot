package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/PaulRychkov/motivation-bot/internal/bootstrap"
	"github.com/PaulRychkov/motivation-bot/internal/config"
	"github.com/PaulRychkov/motivation-bot/internal/core/app"
	"github.com/PaulRychkov/motivation-bot/internal/core/handler"
	"github.com/PaulRychkov/motivation-bot/internal/kafkax"
	"github.com/PaulRychkov/motivation-bot/internal/logger"
)

func main() {
	cfg := config.Load()
	log, err := logger.New("core-service")
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		os.Exit(1)
	}
	defer log.Sync()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := runMigrations(ctx, cfg, log); err != nil {
		log.Fatal("миграции", zap.Error(err))
	}

	db, err := bootstrap.Retry(ctx, log, "postgres", 30, 2*time.Second, func() (*gorm.DB, error) {
		return gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
			Logger:         gormlogger.Default.LogMode(gormlogger.Silent),
			TranslateError: true,
		})
	})
	if err != nil {
		log.Fatal("подключение к БД", zap.Error(err))
	}

	prod, err := bootstrap.Producer(ctx, cfg.KafkaBrokers, log)
	if err != nil {
		log.Fatal("kafka producer", zap.Error(err))
	}
	defer prod.Close()

	a := app.New(db, prod, cfg, log)

	topics := []string{
		kafkax.TopicTasksEvents,
		kafkax.TopicPomodoroEvents,
		kafkax.TopicAgentResponses,
		kafkax.TopicTgSent,
	}
	consumer, err := bootstrap.Consumer(ctx, cfg.KafkaBrokers, "bot-core-group", topics, a.HandleKafka, log)
	if err != nil {
		log.Fatal("kafka consumer", zap.Error(err))
	}
	defer consumer.Close()
	consumer.RequireSuccess(kafkax.TopicTasksEvents, kafkax.TopicPomodoroEvents)

	go consumer.Run(ctx)
	go a.RunScheduler(ctx)
	go a.RunDeadlinePoller(ctx)
	go a.RunDispatcher(ctx)
	go a.RunWindowWorker(ctx)
	go a.RunOutboxRelay(ctx)
	go a.RunRetention(ctx)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: handler.New(a, log),
	}
	go func() {
		log.Info("core-service запущен", zap.Int("port", cfg.HTTPPort))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", zap.Error(err))
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("остановка core-service")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("http shutdown", zap.Error(err))
	}
}

func runMigrations(ctx context.Context, cfg config.Config, log *zap.Logger) error {
	_, err := bootstrap.Retry(ctx, log, "migrations", 30, 2*time.Second, func() (struct{}, error) {
		m, err := migrate.New("file://"+cfg.MigrationsDir, cfg.MigrateURL())
		if err != nil {
			return struct{}{}, err
		}
		defer m.Close()
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return struct{}{}, err
		}
		return struct{}{}, nil
	})
	if err == nil {
		log.Info("миграции применены")
	}
	return err
}
