package logic

import (
	"time"

	"github.com/google/uuid"

	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

type TaskRef struct {
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
}

type Event struct {
	Source     string
	EventID    string
	Type       string
	OccurredAt time.Time
	Payload    map[string]any
}

func (e Event) TaskRef() (TaskRef, bool) {
	switch e.Source {
	case models.SourceTasks:
		id, _ := e.Payload["task_id"].(string)
		if id == "" {
			return TaskRef{}, false
		}
		return TaskRef{Source: models.SourceTasks, ExternalID: id}, true
	case models.SourcePomodoro:
		raw, ok := e.Payload["task"].(map[string]any)
		if !ok {
			return TaskRef{}, false
		}
		src, _ := raw["source"].(string)
		ext, _ := raw["external_id"].(string)
		if src == "" || ext == "" {
			return TaskRef{}, false
		}
		return TaskRef{Source: src, ExternalID: ext}, true
	}
	return TaskRef{}, false
}

func (e Event) Date() string {
	s, _ := e.Payload["date"].(string)
	return s
}

type Pending struct {
	ID             uuid.UUID
	ChatProfileID  uuid.UUID
	Kind           string
	Outcome        string
	TaskSource     *string
	TaskExternalID *string
	TargetDate     *string
	LocalDate      string
}

type Change struct {
	InterventionID uuid.UUID
	Outcome        string
}

type PraiseTask struct {
	Ref   TaskRef
	Title string
}

type MatchResult struct {
	Changes []Change
	Praise  []PraiseTask
}

type MatchContext struct {
	PlanTaskIDs  map[string]bool
	HasUserReply func(uuid.UUID) bool
}

func Match(evt Event, pending []Pending, mctx MatchContext) MatchResult {
	var res MatchResult
	switch evt.Source + "/" + evt.Type {
	case "tasks/occurrence.completed":
		ref, ok := evt.TaskRef()
		if !ok {
			return res
		}
		date := evt.Date()
		for _, p := range pending {
			if taskMatches(p, ref) && dateMatches(p, date) {
				res.Changes = append(res.Changes, Change{p.ID, models.OutcomeActivated})
				continue
			}
			if p.Kind == models.KindMorningPlan && p.LocalDate == date && mctx.PlanTaskIDs[ref.ExternalID] {
				res.Changes = append(res.Changes, Change{p.ID, models.OutcomeActivated})
			}
		}
	case "tasks/task.completed":
		ref, ok := evt.TaskRef()
		if !ok {
			return res
		}
		title, _ := evt.Payload["title"].(string)
		res.Praise = append(res.Praise, PraiseTask{Ref: ref, Title: title})
		for _, p := range pending {
			if taskMatches(p, ref) {
				res.Changes = append(res.Changes, Change{p.ID, models.OutcomeActivated})
			}
		}
	case "tasks/occurrence.skipped":
		ref, ok := evt.TaskRef()
		if !ok {
			return res
		}
		date := evt.Date()
		for _, p := range pending {
			if taskMatches(p, ref) && dateMatches(p, date) {
				res.Changes = append(res.Changes, Change{p.ID, models.OutcomeRefused})
			}
		}
	case "tasks/plan.committed":
		committedBy, _ := evt.Payload["committed_by"].(string)
		date := evt.Date()
		for _, p := range pending {
			if p.Kind != models.KindMorningPlan || p.LocalDate != date {
				continue
			}
			if committedBy == "app" || (mctx.HasUserReply != nil && mctx.HasUserReply(p.ID)) {
				res.Changes = append(res.Changes, Change{p.ID, models.OutcomeActivated})
			}
		}
	case "pomodoro/pomodoro.completed":
		ref, ok := evt.TaskRef()
		if !ok {
			return res
		}
		for _, p := range pending {
			if taskMatches(p, ref) {
				res.Changes = append(res.Changes, Change{p.ID, models.OutcomePartial})
			}
		}
	}
	res.Changes = dedupeChanges(res.Changes)
	return res
}

func ResolveOutcome(current, proposed string) (string, bool) {
	switch current {
	case models.OutcomePending:
		switch proposed {
		case models.OutcomeActivated, models.OutcomePartial, models.OutcomeRefused:
			return proposed, true
		}
		return current, false
	case models.OutcomePartial:
		switch proposed {
		case models.OutcomeActivated, models.OutcomeRefused:
			return proposed, true
		case models.OutcomePartial:
			return models.OutcomePartial, true
		}
		return current, false
	default:
		return current, false
	}
}

func taskMatches(p Pending, ref TaskRef) bool {
	return p.TaskSource != nil && p.TaskExternalID != nil &&
		*p.TaskSource == ref.Source && *p.TaskExternalID == ref.ExternalID
}

func dateMatches(p Pending, date string) bool {
	return p.TargetDate == nil || date == "" || *p.TargetDate == date
}

func changeRank(outcome string) int {
	switch outcome {
	case models.OutcomeActivated:
		return 3
	case models.OutcomeRefused:
		return 2
	case models.OutcomePartial:
		return 1
	}
	return 0
}

func dedupeChanges(changes []Change) []Change {
	if len(changes) < 2 {
		return changes
	}
	best := make(map[uuid.UUID]Change, len(changes))
	order := make([]uuid.UUID, 0, len(changes))
	for _, ch := range changes {
		cur, ok := best[ch.InterventionID]
		if !ok {
			best[ch.InterventionID] = ch
			order = append(order, ch.InterventionID)
			continue
		}
		if changeRank(ch.Outcome) > changeRank(cur.Outcome) {
			best[ch.InterventionID] = ch
		}
	}
	out := make([]Change, 0, len(order))
	for _, id := range order {
		out = append(out, best[id])
	}
	return out
}
