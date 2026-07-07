package kafkax

import (
	"context"
	"fmt"
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"
)

const (
	TopicTasksEvents    = "tasks.events"
	TopicPomodoroEvents = "pomodoro.events"
	TopicBotEvents      = "bot.events"
	TopicTgUpdates      = "bot.tg-updates"
	TopicTgOutgoing     = "bot.tg-outgoing"
	TopicTgSent         = "bot.tg-sent"
	TopicAgentRequests  = "bot.agent-requests"
	TopicAgentResponses = "bot.agent-responses"
)

type Producer struct {
	inner sarama.SyncProducer
	log   *zap.Logger
}

func NewProducer(brokers []string, log *zap.Logger) (*Producer, error) {
	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Retry.Max = 5
	p, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, fmt.Errorf("create kafka producer: %w", err)
	}
	return &Producer{inner: p, log: log}, nil
}

func (p *Producer) Send(topic, key string, value []byte) error {
	msg := &sarama.ProducerMessage{Topic: topic, Value: sarama.ByteEncoder(value)}
	if key != "" {
		msg.Key = sarama.StringEncoder(key)
	}
	partition, offset, err := p.inner.SendMessage(msg)
	if err != nil {
		return fmt.Errorf("send to %s: %w", topic, err)
	}
	p.log.Debug("kafka message sent", zap.String("topic", topic), zap.Int32("partition", partition), zap.Int64("offset", offset))
	return nil
}

func (p *Producer) Close() error {
	return p.inner.Close()
}

type MessageHandler func(ctx context.Context, topic string, key, value []byte) error

type Consumer struct {
	group   sarama.ConsumerGroup
	topics  []string
	handler MessageHandler
	strict  map[string]bool
	log     *zap.Logger
}

func NewConsumer(brokers []string, groupID string, topics []string, handler MessageHandler, log *zap.Logger) (*Consumer, error) {
	cfg := sarama.NewConfig()
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	cfg.Consumer.Return.Errors = true
	group, err := sarama.NewConsumerGroup(brokers, groupID, cfg)
	if err != nil {
		return nil, fmt.Errorf("create consumer group %s: %w", groupID, err)
	}
	return &Consumer{group: group, topics: topics, handler: handler, strict: map[string]bool{}, log: log}, nil
}

func (c *Consumer) RequireSuccess(topics ...string) {
	for _, t := range topics {
		c.strict[t] = true
	}
}

func (c *Consumer) Run(ctx context.Context) error {
	h := &groupHandler{handler: c.handler, strict: c.strict, log: c.log}
	for {
		if err := c.group.Consume(ctx, c.topics, h); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.log.Error("consumer group error", zap.Error(err))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

func (c *Consumer) Close() error {
	return c.group.Close()
}

type groupHandler struct {
	handler MessageHandler
	strict  map[string]bool
	log     *zap.Logger
}

func (h *groupHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *groupHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *groupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		if err := h.handler(session.Context(), msg.Topic, msg.Key, msg.Value); err != nil {
			h.log.Error("message handling failed",
				zap.String("topic", msg.Topic),
				zap.Int64("offset", msg.Offset),
				zap.Error(err))
			if h.strict[msg.Topic] {
				return fmt.Errorf("handle %s: %w", msg.Topic, err)
			}
		}
		session.MarkMessage(msg, "")
	}
	return nil
}
