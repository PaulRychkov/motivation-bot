package openrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatSendsToolsAndParsesToolCalls(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("auth = %s", auth)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{"message":{
				"role":"assistant",
				"content":"",
				"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_profile","arguments":"{\"chat_id\":42}"}}]
			}}]
		}`))
	}))
	defer srv.Close()

	c := New("test-key", srv.URL)
	msg, err := c.Chat(context.Background(), ChatRequest{
		Model:    "deepseek/deepseek-chat",
		Messages: []Message{{Role: "user", Content: "привет"}},
		Tools: []Tool{{
			Type:     "function",
			Function: ToolFunction{Name: "get_profile", Parameters: map[string]any{"type": "object"}},
		}},
		ToolChoice: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}

	if tools, ok := gotBody["tools"].([]any); !ok || len(tools) != 1 {
		t.Errorf("tools не переданы в запрос: %v", gotBody["tools"])
	}
	if gotBody["tool_choice"] != "auto" {
		t.Errorf("tool_choice = %v", gotBody["tool_choice"])
	}

	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, ожидался 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "get_profile" {
		t.Errorf("tool call распарсен неверно: %+v", tc)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("arguments не парсятся: %v", err)
	}
	if args["chat_id"] != float64(42) {
		t.Errorf("args = %v", args)
	}
}

func TestChatPlainText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"готово"}}]}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL)
	msg, err := c.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "готово" || len(msg.ToolCalls) != 0 {
		t.Errorf("msg = %+v", msg)
	}
}

func TestChatAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL)
	if _, err := c.Chat(context.Background(), ChatRequest{Model: "m"}); err == nil {
		t.Fatal("ожидалась ошибка при 429")
	}
}

func TestChatInBodyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":{"message":"провайдер упал"}}`))
	}))
	defer srv.Close()

	c := New("k", srv.URL)
	if _, err := c.Chat(context.Background(), ChatRequest{Model: "m"}); err == nil {
		t.Fatal("ожидалась ошибка из тела ответа")
	}
}
