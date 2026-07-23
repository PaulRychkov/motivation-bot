package logic

import "time"

var distractingCategories = map[string]bool{
	"social": true,
	"video":  true,
	"games":  true,
}

type PhoneSnapshot struct {
	ForegroundPackage  string
	ForegroundCategory string
	ForegroundSince    time.Time
	WindowEnd          time.Time
	DistractingSeconds int64
	ScreenOn           bool
}

type DistractionDecision struct {
	Nudge   bool
	Minutes int
}

func Distracting(category string) bool {
	return distractingCategories[category]
}

func EvaluateDistraction(s PhoneSnapshot, thresholdMinutes int, lastNudge time.Time, cooldownMinutes int) DistractionDecision {
	if thresholdMinutes <= 0 {
		thresholdMinutes = 15
	}
	if !s.ScreenOn || !Distracting(s.ForegroundCategory) {
		return DistractionDecision{}
	}
	if !lastNudge.IsZero() && cooldownMinutes > 0 {
		if s.WindowEnd.Sub(lastNudge) < time.Duration(cooldownMinutes)*time.Minute {
			return DistractionDecision{}
		}
	}

	minutes := 0
	if !s.ForegroundSince.IsZero() && s.WindowEnd.After(s.ForegroundSince) {
		minutes = int(s.WindowEnd.Sub(s.ForegroundSince).Minutes())
	}
	if window := int(s.DistractingSeconds / 60); window > minutes {
		minutes = window
	}
	if minutes < thresholdMinutes {
		return DistractionDecision{}
	}
	return DistractionDecision{Nudge: true, Minutes: minutes}
}
