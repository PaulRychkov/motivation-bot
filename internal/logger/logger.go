package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(service string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncoderConfig.TimeKey = "ts"
	log, err := cfg.Build()
	if err != nil {
		return nil, err
	}
	return log.With(zap.String("service", service)), nil
}
