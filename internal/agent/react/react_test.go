package react

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/PaulRychkov/motivation-bot/internal/agent/openrouter"
)

type mockLLM struct {
	responses []openrouter.Message
	err       error
	requests  []openrouter.ChatRequest
	calls     int
}

func (m *mockLLM) Chat(_ context.Context, in openrouter.ChatRequest) (openrouter.Message, error) {
	m.requests = append(m.requests, in)
	if m.err != nil {
		return openrouter.Message{}, m.err
	}
	if m.calls >= len(m.responses) {
		return m.responses[len(m.responses)-1], nil
	}
	resp := m.responses[m.calls]
	m.calls++
	return resp, nil
}

type mockTools struct {
	defs     []openrouter.Tool
	executed []string
	args     []string
	result   string
	err      error
}

func (m *mockTools) Definitions(context.Context) []openrouter.Tool {
	return m.defs
}

func (m *mockTools) Execute(_ context.Context, name string, args json.RawMessage) (string, bool, error) {
	m.executed = append(m.executed, name)
	m.args = append(m.args, string(args))
	for _, d := range m.defs {
		if d.Function.Name == name {
			return m.result, true, m.err
		}
	}
	return "", false, nil
}

func profileTool() openrouter.Tool {
	return openrouter.Tool{Type: "function", Function: openrouter.ToolFunction{Name: "get_profile"}}
}

func toolCallMsg(id, name, args string) openrouter.Message {
	return openrouter.Message{
		Role: "assistant",
		ToolCalls: []openrouter.ToolCall{{
			ID: id, Type: "function",
			Function: openrouter.ToolCallFunction{Name: name, Arguments: args},
		}},
	}
}

func TestRunDirectAnswer(t *testing.T) {
	llm := &mockLLM{responses: []openrouter.Message{{Role: "assistant", Content: "просто ответ"}}}
	e := &Engine{LLM: llm, Model: "m", MaxIterations: 5, StepTimeout: time.Second}
	got, err := e.Run(context.Background(), &mockTools{}, []openrouter.Message{{Role: "user", Content: "привет"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "просто ответ" {
		t.Errorf("ответ = %q", got)
	}
	if len(llm.requests) != 1 {
		t.Errorf("llm вызван %d раз", len(llm.requests))
	}
}

func TestRunToolCallCycle(t *testing.T) {
	llm := &mockLLM{responses: []openrouter.Message{
		toolCallMsg("call_1", "get_profile", `{"detail":"full"}`),
		{Role: "assistant", Content: "профиль получен"},
	}}
	tls := &mockTools{defs: []openrouter.Tool{profileTool()}, result: `{"timezone":"Europe/Moscow"}`}
	e := &Engine{LLM: llm, Model: "m", MaxIterations: 5, StepTimeout: time.Second}

	var steps []openrouter.Message
	got, err := e.Run(context.Background(), tls,
		[]openrouter.Message{{Role: "user", Content: "какая у меня таймзона?"}},
		func(m openrouter.Message) { steps = append(steps, m) })
	if err != nil {
		t.Fatal(err)
	}
	if got != "профиль получен" {
		t.Errorf("ответ = %q", got)
	}
	if len(tls.executed) != 1 || tls.executed[0] != "get_profile" {
		t.Fatalf("executed = %v", tls.executed)
	}
	if tls.args[0] != `{"detail":"full"}` {
		t.Errorf("аргументы tool_call переданы неверно: %s", tls.args[0])
	}

	second := llm.requests[1]
	last := second.Messages[len(second.Messages)-1]
	if last.Role != "tool" || last.ToolCallID != "call_1" || last.Content != `{"timezone":"Europe/Moscow"}` {
		t.Errorf("результат тула не попал в следующий запрос: %+v", last)
	}
	if len(steps) != 3 {
		t.Errorf("onMessage вызван %d раз, ожидалось 3", len(steps))
	}
}

func TestRunIterationLimit(t *testing.T) {
	llm := &mockLLM{responses: []openrouter.Message{
		toolCallMsg("call_x", "get_profile", `{}`),
	}}
	tls := &mockTools{defs: []openrouter.Tool{profileTool()}, result: "{}"}
	e := &Engine{LLM: llm, Model: "m", MaxIterations: 3, StepTimeout: time.Second}

	_, err := e.Run(context.Background(), tls, []openrouter.Message{{Role: "user", Content: "зациклись"}}, nil)
	if !errors.Is(err, ErrIterationLimit) {
		t.Fatalf("ожидался ErrIterationLimit, получено %v", err)
	}
	if len(llm.requests) != 3 {
		t.Errorf("llm вызван %d раз, ожидалось ровно 3", len(llm.requests))
	}
}

func TestRunLLMError(t *testing.T) {
	llm := &mockLLM{err: fmt.Errorf("openrouter 500")}
	e := &Engine{LLM: llm, Model: "m", MaxIterations: 3, StepTimeout: time.Second}
	_, err := e.Run(context.Background(), &mockTools{}, []openrouter.Message{{Role: "user", Content: "x"}}, nil)
	if err == nil {
		t.Fatal("ошибка LLM должна пробрасываться для фолбэка")
	}
}

func TestRunUnknownToolFedBack(t *testing.T) {
	llm := &mockLLM{responses: []openrouter.Message{
		toolCallMsg("call_1", "nonexistent_tool", `{}`),
		{Role: "assistant", Content: "ладно, без тула"},
	}}
	tls := &mockTools{defs: []openrouter.Tool{profileTool()}}
	e := &Engine{LLM: llm, Model: "m", MaxIterations: 5, StepTimeout: time.Second}

	got, err := e.Run(context.Background(), tls, []openrouter.Message{{Role: "user", Content: "x"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ладно, без тула" {
		t.Errorf("ответ = %q", got)
	}
	second := llm.requests[1]
	last := second.Messages[len(second.Messages)-1]
	if last.Role != "tool" || last.Content != "неизвестный инструмент: nonexistent_tool" {
		t.Errorf("LLM не получил сообщение о неизвестном туле: %+v", last)
	}
}

func TestRunToolErrorFedBack(t *testing.T) {
	llm := &mockLLM{responses: []openrouter.Message{
		toolCallMsg("call_1", "get_profile", `{}`),
		{Role: "assistant", Content: "не вышло, но живём"},
	}}
	tls := &mockTools{defs: []openrouter.Tool{profileTool()}, err: fmt.Errorf("core недоступен")}
	e := &Engine{LLM: llm, Model: "m", MaxIterations: 5, StepTimeout: time.Second}

	got, err := e.Run(context.Background(), tls, []openrouter.Message{{Role: "user", Content: "x"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "не вышло, но живём" {
		t.Errorf("ответ = %q", got)
	}
	second := llm.requests[1]
	last := second.Messages[len(second.Messages)-1]
	if last.Role != "tool" || last.Content != "ошибка инструмента: core недоступен" {
		t.Errorf("ошибка тула не дошла до LLM: %+v", last)
	}
}

func TestRunPassesToolDefinitions(t *testing.T) {
	llm := &mockLLM{responses: []openrouter.Message{{Role: "assistant", Content: "ок"}}}
	tls := &mockTools{defs: []openrouter.Tool{profileTool()}}
	e := &Engine{LLM: llm, Model: "m", MaxIterations: 2, StepTimeout: time.Second}
	if _, err := e.Run(context.Background(), tls, []openrouter.Message{{Role: "user", Content: "x"}}, nil); err != nil {
		t.Fatal(err)
	}
	if len(llm.requests[0].Tools) != 1 || llm.requests[0].Tools[0].Function.Name != "get_profile" {
		t.Errorf("дефиниции тулов не переданы: %+v", llm.requests[0].Tools)
	}
	if llm.requests[0].ToolChoice != "auto" {
		t.Errorf("tool_choice = %v", llm.requests[0].ToolChoice)
	}
}
