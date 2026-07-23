package inproc

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/kafkax"
)

// Bus — внутрипроцессная замена Kafka для однопроцессной сборки бота
// (мобильное приложение). Реализует kafkax.Sender: Send публикует
// сообщение всем подписчикам топика, каждый обрабатывается в своей
// горутине, чтобы обработчик мог сам публиковать без взаимоблокировки.
type Bus struct {
	mu   sync.RWMutex
	subs map[string][]kafkax.MessageHandler
	ctx  context.Context
	log  *zap.Logger
}

func New(ctx context.Context, log *zap.Logger) *Bus {
	return &Bus{subs: map[string][]kafkax.MessageHandler{}, ctx: ctx, log: log}
}

func (b *Bus) Subscribe(topic string, h kafkax.MessageHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[topic] = append(b.subs[topic], h)
}

func (b *Bus) Send(topic, key string, value []byte) error {
	b.mu.RLock()
	handlers := append([]kafkax.MessageHandler(nil), b.subs[topic]...)
	b.mu.RUnlock()
	payload := append([]byte(nil), value...)
	for _, h := range handlers {
		h := h
		go func() {
			if err := h(b.ctx, topic, []byte(key), payload); err != nil {
				b.log.Warn("inproc handler", zap.String("topic", topic), zap.Error(err))
			}
		}()
	}
	return nil
}
