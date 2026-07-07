package logic

import (
	"testing"

	"github.com/google/uuid"

	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

func str(s string) *string { return &s }

func pendingIv(kind, outcome string, task *string, targetDate *string, localDate string) Pending {
	p := Pending{
		ID:        uuid.New(),
		Kind:      kind,
		Outcome:   outcome,
		LocalDate: localDate,
	}
	if task != nil {
		p.TaskSource = str(models.SourceTasks)
		p.TaskExternalID = task
	}
	p.TargetDate = targetDate
	return p
}

func tasksEvent(eventType string, payload map[string]any) Event {
	return Event{Source: models.SourceTasks, EventID: uuid.NewString(), Type: eventType, Payload: payload}
}

func pomodoroEvent(eventType string, payload map[string]any) Event {
	return Event{Source: models.SourcePomodoro, EventID: uuid.NewString(), Type: eventType, Payload: payload}
}

func TestMatch(t *testing.T) {
	taskA := "11111111-1111-1111-1111-111111111111"
	taskB := "22222222-2222-2222-2222-222222222222"
	day := "2026-07-06"
	otherDay := "2026-07-07"

	tests := []struct {
		name         string
		event        Event
		pending      []Pending
		mctx         MatchContext
		wantOutcomes []string
		wantPraise   int
	}{
		{
			name:         "occurrence.completed по задаче и дате активирует",
			event:        tasksEvent("occurrence.completed", map[string]any{"task_id": taskA, "date": day}),
			pending:      []Pending{pendingIv(models.KindDeadlineReminder, models.OutcomePending, &taskA, &day, day)},
			wantOutcomes: []string{models.OutcomeActivated},
		},
		{
			name:         "occurrence.completed с другой датой не матчится",
			event:        tasksEvent("occurrence.completed", map[string]any{"task_id": taskA, "date": otherDay}),
			pending:      []Pending{pendingIv(models.KindDeadlineReminder, models.OutcomePending, &taskA, &day, day)},
			wantOutcomes: nil,
		},
		{
			name:         "occurrence.completed без target_date матчится по любой дате",
			event:        tasksEvent("occurrence.completed", map[string]any{"task_id": taskA, "date": otherDay}),
			pending:      []Pending{pendingIv(models.KindFreePing, models.OutcomePending, &taskA, nil, day)},
			wantOutcomes: []string{models.OutcomeActivated},
		},
		{
			name:         "occurrence.completed чужой задачи не матчится",
			event:        tasksEvent("occurrence.completed", map[string]any{"task_id": taskB, "date": day}),
			pending:      []Pending{pendingIv(models.KindDeadlineReminder, models.OutcomePending, &taskA, &day, day)},
			wantOutcomes: nil,
		},
		{
			name:         "occurrence.completed задачи из плана активирует morning_plan",
			event:        tasksEvent("occurrence.completed", map[string]any{"task_id": taskA, "date": day}),
			pending:      []Pending{pendingIv(models.KindMorningPlan, models.OutcomePending, nil, nil, day)},
			mctx:         MatchContext{PlanTaskIDs: map[string]bool{taskA: true}},
			wantOutcomes: []string{models.OutcomeActivated},
		},
		{
			name:         "occurrence.completed задачи из плана другого дня не трогает morning_plan",
			event:        tasksEvent("occurrence.completed", map[string]any{"task_id": taskA, "date": otherDay}),
			pending:      []Pending{pendingIv(models.KindMorningPlan, models.OutcomePending, nil, nil, day)},
			mctx:         MatchContext{PlanTaskIDs: map[string]bool{taskA: true}},
			wantOutcomes: nil,
		},
		{
			name:         "occurrence.completed апгрейдит partial до activated",
			event:        tasksEvent("occurrence.completed", map[string]any{"task_id": taskA, "date": day}),
			pending:      []Pending{pendingIv(models.KindDeadlineReminder, models.OutcomePartial, &taskA, &day, day)},
			wantOutcomes: []string{models.OutcomeActivated},
		},
		{
			name:         "pomodoro.completed даёт partial",
			event:        pomodoroEvent("pomodoro.completed", map[string]any{"task": map[string]any{"source": "tasks", "external_id": taskA}}),
			pending:      []Pending{pendingIv(models.KindDeadlineReminder, models.OutcomePending, &taskA, &day, day)},
			wantOutcomes: []string{models.OutcomePartial},
		},
		{
			name:         "pomodoro.completed без привязки к задаче не матчится",
			event:        pomodoroEvent("pomodoro.completed", map[string]any{"task": nil, "label": "чтение"}),
			pending:      []Pending{pendingIv(models.KindDeadlineReminder, models.OutcomePending, &taskA, &day, day)},
			wantOutcomes: nil,
		},
		{
			name:         "pomodoro.interrupted трактуется как abandoned и не активирует",
			event:        pomodoroEvent("pomodoro.interrupted", map[string]any{"task": map[string]any{"source": "tasks", "external_id": taskA}}),
			pending:      []Pending{pendingIv(models.KindDeadlineReminder, models.OutcomePending, &taskA, &day, day)},
			wantOutcomes: nil,
		},
		{
			name:         "pomodoro.abandoned не активирует",
			event:        pomodoroEvent("pomodoro.abandoned", map[string]any{"task": map[string]any{"source": "tasks", "external_id": taskA}}),
			pending:      []Pending{pendingIv(models.KindDeadlineReminder, models.OutcomePending, &taskA, &day, day)},
			wantOutcomes: nil,
		},
		{
			name:         "task.completed активирует и даёт повод для praise",
			event:        tasksEvent("task.completed", map[string]any{"task_id": taskA, "title": "Диплом"}),
			pending:      []Pending{pendingIv(models.KindDeadlineReminder, models.OutcomePending, &taskA, &day, day)},
			wantOutcomes: []string{models.OutcomeActivated},
			wantPraise:   1,
		},
		{
			name:       "task.completed без pending всё равно даёт praise",
			event:      tasksEvent("task.completed", map[string]any{"task_id": taskA, "title": "Диплом"}),
			pending:    nil,
			wantPraise: 1,
		},
		{
			name:         "occurrence.skipped закрывает pending как refused",
			event:        tasksEvent("occurrence.skipped", map[string]any{"task_id": taskA, "date": day}),
			pending:      []Pending{pendingIv(models.KindDeadlineReminder, models.OutcomePending, &taskA, &day, day)},
			wantOutcomes: []string{models.OutcomeRefused},
		},
		{
			name:         "plan.committed от app активирует morning_plan",
			event:        tasksEvent("plan.committed", map[string]any{"date": day, "committed_by": "app"}),
			pending:      []Pending{pendingIv(models.KindMorningPlan, models.OutcomePending, nil, nil, day)},
			wantOutcomes: []string{models.OutcomeActivated},
		},
		{
			name:         "plan.committed от agent без реплики пользователя не активирует",
			event:        tasksEvent("plan.committed", map[string]any{"date": day, "committed_by": "agent"}),
			pending:      []Pending{pendingIv(models.KindMorningPlan, models.OutcomePending, nil, nil, day)},
			mctx:         MatchContext{HasUserReply: func(uuid.UUID) bool { return false }},
			wantOutcomes: nil,
		},
		{
			name:         "plan.committed от agent с явной репликой пользователя активирует",
			event:        tasksEvent("plan.committed", map[string]any{"date": day, "committed_by": "agent"}),
			pending:      []Pending{pendingIv(models.KindMorningPlan, models.OutcomePending, nil, nil, day)},
			mctx:         MatchContext{HasUserReply: func(uuid.UUID) bool { return true }},
			wantOutcomes: []string{models.OutcomeActivated},
		},
		{
			name:         "plan.committed другого дня не активирует morning_plan",
			event:        tasksEvent("plan.committed", map[string]any{"date": otherDay, "committed_by": "app"}),
			pending:      []Pending{pendingIv(models.KindMorningPlan, models.OutcomePending, nil, nil, day)},
			wantOutcomes: nil,
		},
		{
			name:  "одно событие закрывает несколько pending-интервенций",
			event: tasksEvent("occurrence.completed", map[string]any{"task_id": taskA, "date": day}),
			pending: []Pending{
				pendingIv(models.KindDeadlineReminder, models.OutcomePending, &taskA, &day, day),
				pendingIv(models.KindFreePing, models.OutcomePending, &taskA, nil, day),
			},
			wantOutcomes: []string{models.OutcomeActivated, models.OutcomeActivated},
		},
		{
			name:         "occurrence.missed не влияет на матчинг",
			event:        tasksEvent("occurrence.missed", map[string]any{"task_id": taskA, "date": day}),
			pending:      []Pending{pendingIv(models.KindDeadlineReminder, models.OutcomePending, &taskA, &day, day)},
			wantOutcomes: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Match(tt.event, tt.pending, tt.mctx)
			if len(res.Changes) != len(tt.wantOutcomes) {
				t.Fatalf("изменений %d, ожидалось %d: %+v", len(res.Changes), len(tt.wantOutcomes), res.Changes)
			}
			for i, ch := range res.Changes {
				if ch.Outcome != tt.wantOutcomes[i] {
					t.Errorf("change[%d].Outcome = %s, ожидалось %s", i, ch.Outcome, tt.wantOutcomes[i])
				}
			}
			if len(res.Praise) != tt.wantPraise {
				t.Errorf("praise %d, ожидалось %d", len(res.Praise), tt.wantPraise)
			}
		})
	}
}

func TestResolveOutcome(t *testing.T) {
	tests := []struct {
		current, proposed string
		want              string
		apply             bool
	}{
		{models.OutcomePending, models.OutcomeActivated, models.OutcomeActivated, true},
		{models.OutcomePending, models.OutcomePartial, models.OutcomePartial, true},
		{models.OutcomePending, models.OutcomeRefused, models.OutcomeRefused, true},
		{models.OutcomePartial, models.OutcomeActivated, models.OutcomeActivated, true},
		{models.OutcomePartial, models.OutcomePartial, models.OutcomePartial, true},
		{models.OutcomePartial, models.OutcomeRefused, models.OutcomeRefused, true},
		{models.OutcomeActivated, models.OutcomePartial, models.OutcomeActivated, false},
		{models.OutcomeActivated, models.OutcomeActivated, models.OutcomeActivated, false},
		{models.OutcomeIgnored, models.OutcomeActivated, models.OutcomeIgnored, false},
		{models.OutcomeRefused, models.OutcomeActivated, models.OutcomeRefused, false},
		{models.OutcomeRescheduled, models.OutcomePartial, models.OutcomeRescheduled, false},
	}
	for _, tt := range tests {
		got, apply := ResolveOutcome(tt.current, tt.proposed)
		if got != tt.want || apply != tt.apply {
			t.Errorf("ResolveOutcome(%s, %s) = (%s, %v), ожидалось (%s, %v)",
				tt.current, tt.proposed, got, apply, tt.want, tt.apply)
		}
	}
}

func TestDedupeChanges(t *testing.T) {
	id := uuid.New()
	changes := []Change{
		{InterventionID: id, Outcome: models.OutcomePartial},
		{InterventionID: id, Outcome: models.OutcomeActivated},
	}
	out := dedupeChanges(changes)
	if len(out) != 1 || out[0].Outcome != models.OutcomeActivated {
		t.Fatalf("ожидался один change activated, получено %+v", out)
	}
}
