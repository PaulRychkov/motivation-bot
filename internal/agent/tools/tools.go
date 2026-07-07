package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/PaulRychkov/motivation-bot/internal/agent/openrouter"
)

type Source interface {
	Definitions(ctx context.Context) []openrouter.Tool
	Execute(ctx context.Context, name string, args json.RawMessage) (string, bool, error)
}

type Handler func(ctx context.Context, args json.RawMessage) (string, error)

type Static struct {
	defs     []openrouter.Tool
	handlers map[string]Handler
}

func NewStatic() *Static {
	return &Static{handlers: map[string]Handler{}}
}

func (s *Static) Add(name, description string, parameters map[string]any, h Handler) {
	if parameters == nil {
		parameters = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	s.defs = append(s.defs, openrouter.Tool{
		Type:     "function",
		Function: openrouter.ToolFunction{Name: name, Description: description, Parameters: parameters},
	})
	s.handlers[name] = h
}

func (s *Static) Definitions(context.Context) []openrouter.Tool {
	out := make([]openrouter.Tool, len(s.defs))
	copy(out, s.defs)
	return out
}

func (s *Static) Execute(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	h, ok := s.handlers[name]
	if !ok {
		return "", false, nil
	}
	out, err := h(ctx, args)
	return out, true, err
}

type Combined struct {
	sources []Source
}

func NewCombined(sources ...Source) *Combined {
	return &Combined{sources: sources}
}

func (c *Combined) Definitions(ctx context.Context) []openrouter.Tool {
	var out []openrouter.Tool
	for _, s := range c.sources {
		out = append(out, s.Definitions(ctx)...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Function.Name < out[j].Function.Name })
	return out
}

func (c *Combined) Execute(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	for _, s := range c.sources {
		out, handled, err := s.Execute(ctx, name, args)
		if handled {
			return out, true, err
		}
	}
	return "", false, nil
}

func MustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(b)
}
