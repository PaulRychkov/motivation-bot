package bootstrap

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/kafkax"
)

func Retry[T any](ctx context.Context, log *zap.Logger, label string, attempts int, delay time.Duration, fn func() (T, error)) (T, error) {
	var zero T
	var lastErr error
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return zero, ctx.Err()
		}
		v, err := fn()
		if err == nil {
			return v, nil
		}
		lastErr = err
		log.Warn(label+" недоступен, повтор", zap.Duration("retry_in", delay), zap.Error(err))
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
		}
	}
	return zero, fmt.Errorf("%s: %w", label, lastErr)
}

func Producer(ctx context.Context, brokers []string, log *zap.Logger) (*kafkax.Producer, error) {
	return Retry(ctx, log, "kafka producer", 60, 3*time.Second, func() (*kafkax.Producer, error) {
		return kafkax.NewProducer(brokers, log)
	})
}

func Consumer(ctx context.Context, brokers []string, group string, topics []string, h kafkax.MessageHandler, log *zap.Logger) (*kafkax.Consumer, error) {
	return Retry(ctx, log, "kafka consumer", 60, 3*time.Second, func() (*kafkax.Consumer, error) {
		return kafkax.NewConsumer(brokers, group, topics, h, log)
	})
}
