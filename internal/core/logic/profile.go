package logic

import (
	"fmt"
	"time"
)

var validTones = map[string]bool{
	"gentle": true, "neutral": true, "energetic": true, "strict": true, "humorous": true,
}

func ValidTone(t string) bool {
	return validTones[t]
}

func ValidateProfilePatch(patch map[string]any) (map[string]any, error) {
	updates := map[string]any{}
	for key, val := range patch {
		switch key {
		case "timezone":
			s, ok := val.(string)
			if !ok {
				return nil, fmt.Errorf("timezone: ожидалась строка")
			}
			if _, err := time.LoadLocation(s); err != nil {
				return nil, fmt.Errorf("timezone %q: %w", s, err)
			}
			updates["timezone"] = s
		case "quiet_start_min", "quiet_end_min":
			if val == nil {
				updates[key] = nil
				continue
			}
			m, err := toMinutes(key, val)
			if err != nil {
				return nil, err
			}
			updates[key] = m
		case "morning_plan_min", "evening_review_min":
			m, err := toMinutes(key, val)
			if err != nil {
				return nil, err
			}
			updates[key] = m
		case "free_ping_daily_budget":
			n, ok := toInt(val)
			if !ok || n < 0 {
				return nil, fmt.Errorf("free_ping_daily_budget: ожидалось целое >= 0")
			}
			updates[key] = n
		case "default_tone":
			s, ok := val.(string)
			if !ok || !validTones[s] {
				return nil, fmt.Errorf("default_tone: недопустимое значение %v", val)
			}
			updates[key] = s
		case "persona":
			if val == nil {
				updates[key] = nil
				continue
			}
			s, ok := val.(string)
			if !ok {
				return nil, fmt.Errorf("persona: ожидалась строка")
			}
			updates[key] = s
		case "paused_until":
			if val == nil {
				updates[key] = nil
				continue
			}
			s, ok := val.(string)
			if !ok {
				return nil, fmt.Errorf("paused_until: ожидалась дата YYYY-MM-DD")
			}
			if _, err := time.Parse(dateLayout, s); err != nil {
				return nil, fmt.Errorf("paused_until %q: %w", s, err)
			}
			updates[key] = s
		default:
			return nil, fmt.Errorf("неизвестное поле профиля: %s", key)
		}
	}
	if len(updates) == 0 {
		return nil, fmt.Errorf("пустой patch")
	}
	qs, hasQS := updates["quiet_start_min"]
	qe, hasQE := updates["quiet_end_min"]
	if hasQS != hasQE || (hasQS && ((qs == nil) != (qe == nil))) {
		return nil, fmt.Errorf("quiet_start_min и quiet_end_min меняются только парой")
	}
	return updates, nil
}

func toMinutes(key string, val any) (int, error) {
	n, ok := toInt(val)
	if !ok || n < 0 || n > 1439 {
		return 0, fmt.Errorf("%s: ожидались минуты 0..1439", key)
	}
	return n, nil
}

func toInt(val any) (int, bool) {
	switch v := val.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if v != float64(int(v)) {
			return 0, false
		}
		return int(v), true
	}
	return 0, false
}
