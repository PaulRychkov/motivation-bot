package react

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PaulRychkov/motivation-bot/internal/agent/openrouter"
	"github.com/PaulRychkov/motivation-bot/internal/agent/tools"
)

var ErrIterationLimit = errors.New("превышен лимит итераций ReAct-цикла")

type LLM interface {
	Chat(ctx context.Context, in openrouter.ChatRequest) (openrouter.Message, error)
}

type Engine struct {
	LLM           LLM
	Model         string
	MaxIterations int
	StepTimeout   time.Duration
	Temperature   *float64
}

func (e *Engine) Run(ctx context.Context, src tools.Source, messages []openrouter.Message, onMessage func(openrouter.Message)) (string, error) {
	msgs := make([]openrouter.Message, len(messages))
	copy(msgs, messages)

	maxIter := e.MaxIterations
	if maxIter <= 0 {
		maxIter = 8
	}
	stepTimeout := e.StepTimeout
	if stepTimeout <= 0 {
		stepTimeout = 90 * time.Second
	}

	for i := 0; i < maxIter; i++ {
		req := openrouter.ChatRequest{
			Model:       e.Model,
			Messages:    msgs,
			Temperature: e.Temperature,
		}
		if defs := src.Definitions(ctx); len(defs) > 0 {
			req.Tools = defs
			req.ToolChoice = "auto"
		}

		stepCtx, cancel := context.WithTimeout(ctx, stepTimeout)
		resp, err := e.LLM.Chat(stepCtx, req)
		cancel()
		if err != nil {
			return "", fmt.Errorf("llm chat: %w", err)
		}
		if onMessage != nil {
			onMessage(resp)
		}
		msgs = append(msgs, resp)

		if len(resp.ToolCalls) == 0 {
			return resp.Content, nil
		}
		for _, tc := range resp.ToolCalls {
			out, handled, err := src.Execute(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			switch {
			case err != nil:
				out = "ошибка инструмента: " + err.Error()
			case !handled:
				out = "неизвестный инструмент: " + tc.Function.Name
			case out == "":
				out = "(пустой результат)"
			}
			toolMsg := openrouter.Message{Role: "tool", ToolCallID: tc.ID, Content: out}
			if onMessage != nil {
				onMessage(toolMsg)
			}
			msgs = append(msgs, toolMsg)
		}
	}
	return "", ErrIterationLimit
}
