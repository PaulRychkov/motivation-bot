package run

import (
	"testing"

	"gorm.io/datatypes"

	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

func strPtr(s string) *string { return &s }

func userMsg(text string) models.DialogMessage {
	return models.DialogMessage{Role: models.RoleUser, Content: strPtr(text)}
}

func assistantMsg(text string) models.DialogMessage {
	return models.DialogMessage{Role: models.RoleAssistant, Content: strPtr(text)}
}

func assistantToolCall(id string) models.DialogMessage {
	return models.DialogMessage{
		Role:      models.RoleAssistant,
		ToolCalls: datatypes.JSON(`[{"id":"` + id + `","type":"function","function":{"name":"get_profile","arguments":"{}"}}]`),
	}
}

func toolMsg(callID, text string) models.DialogMessage {
	return models.DialogMessage{Role: models.RoleTool, Content: strPtr(text), ToolCallID: &callID}
}

func TestHistoryToMessages(t *testing.T) {
	tests := []struct {
		name      string
		history   []models.DialogMessage
		wantRoles []string
	}{
		{
			name:      "обычный диалог без тулов",
			history:   []models.DialogMessage{userMsg("привет"), assistantMsg("привет!")},
			wantRoles: []string{"user", "assistant"},
		},
		{
			name: "полная пара tool_call и tool-ответ сохраняется",
			history: []models.DialogMessage{
				userMsg("покажи профиль"),
				assistantToolCall("call-1"),
				toolMsg("call-1", "{}"),
				assistantMsg("вот профиль"),
			},
			wantRoles: []string{"user", "assistant", "tool", "assistant"},
		},
		{
			name: "assistant с tool_calls без tool-ответов выбрасывается",
			history: []models.DialogMessage{
				userMsg("покажи профиль"),
				assistantToolCall("call-1"),
				userMsg("ау?"),
			},
			wantRoles: []string{"user", "user"},
		},
		{
			name: "осиротевший tool-ответ без assistant-шага выбрасывается",
			history: []models.DialogMessage{
				toolMsg("call-x", "{}"),
				userMsg("привет"),
			},
			wantRoles: []string{"user"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := historyToMessages(tt.history)
			if len(out) != len(tt.wantRoles) {
				t.Fatalf("len = %d, want %d (%+v)", len(out), len(tt.wantRoles), out)
			}
			for i, role := range tt.wantRoles {
				if out[i].Role != role {
					t.Errorf("out[%d].Role = %q, want %q", i, out[i].Role, role)
				}
			}
			for _, m := range out {
				for _, tc := range m.ToolCalls {
					found := false
					for _, other := range out {
						if other.Role == models.RoleTool && other.ToolCallID == tc.ID {
							found = true
						}
					}
					if !found {
						t.Errorf("tool_call %s без парного tool-сообщения", tc.ID)
					}
				}
			}
		})
	}
}
