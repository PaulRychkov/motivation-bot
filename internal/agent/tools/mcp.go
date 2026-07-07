package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/PaulRychkov/motivation-bot/internal/agent/openrouter"
)

type MCP struct {
	prefix   string
	endpoint string
	log      *zap.Logger

	mu      sync.Mutex
	session *mcp.ClientSession
	defs    []openrouter.Tool
	lastTry time.Time
}

func NewMCP(prefix, endpoint string, log *zap.Logger) *MCP {
	return &MCP{prefix: prefix, endpoint: endpoint, log: log}
}

func (m *MCP) Definitions(ctx context.Context) []openrouter.Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session == nil {
		m.tryConnectLocked(ctx)
	}
	out := make([]openrouter.Tool, len(m.defs))
	copy(out, m.defs)
	return out
}

func (m *MCP) Execute(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	if !strings.HasPrefix(name, m.prefix+"_") {
		return "", false, nil
	}
	toolName := strings.TrimPrefix(name, m.prefix+"_")

	m.mu.Lock()
	if m.session == nil {
		m.lastTry = time.Time{}
		m.tryConnectLocked(ctx)
	}
	session := m.session
	m.mu.Unlock()
	if session == nil {
		return "", true, fmt.Errorf("сервис %s недоступен (MCP %s)", m.prefix, m.endpoint)
	}

	params := &mcp.CallToolParams{Name: toolName}
	if len(args) > 0 {
		params.Arguments = args
	}
	res, err := session.CallTool(ctx, params)
	if err != nil {
		m.mu.Lock()
		if m.session == session {
			m.session = nil
		}
		m.mu.Unlock()
		return "", true, fmt.Errorf("вызов %s: %w", name, err)
	}
	text := joinContent(res)
	if res.IsError {
		return "", true, fmt.Errorf("инструмент %s вернул ошибку: %s", name, text)
	}
	return text, true, nil
}

func (m *MCP) tryConnectLocked(ctx context.Context) {
	if time.Since(m.lastTry) < 30*time.Second {
		return
	}
	m.lastTry = time.Now()

	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "activization-bot-agent", Version: "1.0.0"}, nil)
	session, err := client.Connect(connectCtx, &mcp.StreamableClientTransport{Endpoint: m.endpoint}, nil)
	if err != nil {
		m.log.Debug("MCP недоступен", zap.String("endpoint", m.endpoint), zap.Error(err))
		return
	}
	listed, err := session.ListTools(connectCtx, nil)
	if err != nil {
		m.log.Warn("MCP ListTools", zap.String("endpoint", m.endpoint), zap.Error(err))
		_ = session.Close()
		return
	}
	defs := make([]openrouter.Tool, 0, len(listed.Tools))
	for _, t := range listed.Tools {
		defs = append(defs, openrouter.Tool{
			Type: "function",
			Function: openrouter.ToolFunction{
				Name:        m.prefix + "_" + t.Name,
				Description: fmt.Sprintf("[%s] %s", m.prefix, t.Description),
				Parameters:  schemaToMap(t.InputSchema),
			},
		})
	}
	m.session = session
	m.defs = defs
	m.log.Info("MCP подключён", zap.String("endpoint", m.endpoint), zap.Int("tools", len(defs)))
}

func (m *MCP) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session != nil {
		_ = m.session.Close()
		m.session = nil
	}
}

func schemaToMap(schema any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if m, ok := schema.(map[string]any); ok {
		return m
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return m
}

func joinContent(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}
