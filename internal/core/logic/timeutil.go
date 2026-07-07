package logic

import (
	"fmt"
	"time"

	"github.com/PaulRychkov/motivation-bot/internal/core/models"
)

const dateLayout = "2006-01-02"

func LocalMinutes(t time.Time, loc *time.Location) int {
	lt := t.In(loc)
	return lt.Hour()*60 + lt.Minute()
}

func LocalDate(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(dateLayout)
}

func InQuietHours(quietStart, quietEnd *int, localMin int) bool {
	if quietStart == nil || quietEnd == nil {
		return false
	}
	s, e := *quietStart, *quietEnd
	if s == e {
		return false
	}
	if s < e {
		return localMin >= s && localMin < e
	}
	return localMin >= s || localMin < e
}

func LocalDayBoundsUTC(loc *time.Location, localDate string) (time.Time, time.Time, error) {
	start, err := time.ParseInLocation(dateLayout, localDate, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parse local date %q: %w", localDate, err)
	}
	return start.UTC(), start.AddDate(0, 0, 1).UTC(), nil
}

func MinuteOfLocalDay(loc *time.Location, localDate string, minutes int) (time.Time, error) {
	start, err := time.ParseInLocation(dateLayout, localDate, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse local date %q: %w", localDate, err)
	}
	return start.Add(time.Duration(minutes) * time.Minute).UTC(), nil
}

func IsPaused(pausedUntil *models.Date, localDate string) bool {
	return pausedUntil != nil && localDate <= pausedUntil.String()
}

func FreePingAllowed(usedToday, budget int) bool {
	return usedToday < budget
}

func WindowEnd(kind string, now time.Time, loc *time.Location, localDate string, eveningMin int, targetDate *string) time.Time {
	switch kind {
	case models.KindMorningPlan:
		end, err := MinuteOfLocalDay(loc, localDate, eveningMin)
		if err == nil && end.After(now) {
			return end
		}
		return now.Add(time.Hour)
	case models.KindEveningReview:
		_, end, err := LocalDayBoundsUTC(loc, localDate)
		if err == nil && end.After(now.Add(30*time.Minute)) {
			return end
		}
		return now.Add(30 * time.Minute)
	case models.KindDeadlineReminder:
		date := localDate
		if targetDate != nil {
			date = *targetDate
		}
		_, end, err := LocalDayBoundsUTC(loc, date)
		if err == nil && end.After(now) {
			return end
		}
		return now.Add(time.Hour)
	case models.KindPraise:
		return now.Add(time.Hour)
	default:
		return now.Add(3 * time.Hour)
	}
}
