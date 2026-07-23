package logic

import (
	"sort"
	"time"
)

type DayApp struct {
	Package           string
	Label             string
	Category          string
	ForegroundSeconds int64
}

type AppMinutes struct {
	Label    string `json:"label"`
	Category string `json:"category"`
	Minutes  int    `json:"minutes"`
}

type ActivityReport struct {
	TotalMinutes       int
	DistractingMinutes int
	ByCategory         map[string]int
	TopDistracting     []AppMinutes
}

func AnalyzeDay(apps []DayApp) ActivityReport {
	rep := ActivityReport{ByCategory: map[string]int{}}
	var distracting []AppMinutes
	for _, app := range apps {
		cat := NormalizeCategory(app.Package, app.Category)
		minutes := int(app.ForegroundSeconds / 60)
		rep.TotalMinutes += minutes
		rep.ByCategory[cat] += minutes
		if !Distracting(cat) || minutes == 0 {
			continue
		}
		rep.DistractingMinutes += minutes
		label := app.Label
		if label == "" {
			label = app.Package
		}
		distracting = append(distracting, AppMinutes{Label: label, Category: cat, Minutes: minutes})
	}
	sort.Slice(distracting, func(i, j int) bool { return distracting[i].Minutes > distracting[j].Minutes })
	if len(distracting) > 5 {
		distracting = distracting[:5]
	}
	rep.TopDistracting = distracting
	return rep
}

type ActivityDecision struct {
	Nudge              bool
	DistractingMinutes int
	Watermark          int
}

func EvaluateActivity(rep ActivityReport, thresholdMin, watermark int, lastNudge, windowEnd time.Time, cooldownMin int) ActivityDecision {
	if thresholdMin <= 0 {
		thresholdMin = 90
	}
	pending := ActivityDecision{DistractingMinutes: rep.DistractingMinutes, Watermark: watermark}
	if rep.DistractingMinutes-watermark < thresholdMin {
		return pending
	}
	if !lastNudge.IsZero() && cooldownMin > 0 && windowEnd.Sub(lastNudge) < time.Duration(cooldownMin)*time.Minute {
		return pending
	}
	return ActivityDecision{Nudge: true, DistractingMinutes: rep.DistractingMinutes, Watermark: rep.DistractingMinutes}
}

type Sample struct {
	T         time.Time        `json:"t"`
	DistrSec  int64            `json:"distr_sec"`
	ScreenSec int64            `json:"screen_sec"`
	Apps      map[string]int64 `json:"apps"`
}

type RecentActivity struct {
	SpanMinutes    int
	DistractingMin int
	ScreenMin      int
	TopApps        []AppMinutes
}

func DeltaMinutes(oldSec, newSec int64) int {
	d := newSec - oldSec
	if d < 0 {
		d = 0
	}
	return int(d / 60)
}

func ComputeRecent(history []Sample, cur Sample, lookback time.Duration, labels, cats map[string]string) RecentActivity {
	var ref *Sample
	for i := len(history) - 1; i >= 0; i-- {
		if cur.T.Sub(history[i].T) >= lookback {
			ref = &history[i]
			break
		}
	}
	if ref == nil && len(history) > 0 {
		ref = &history[0]
	}
	if ref == nil {
		return RecentActivity{}
	}
	ra := RecentActivity{
		SpanMinutes:    int(cur.T.Sub(ref.T).Minutes()),
		DistractingMin: DeltaMinutes(ref.DistrSec, cur.DistrSec),
		ScreenMin:      DeltaMinutes(ref.ScreenSec, cur.ScreenSec),
	}
	var apps []AppMinutes
	for pkg, sec := range cur.Apps {
		minutes := DeltaMinutes(ref.Apps[pkg], sec)
		if minutes == 0 {
			continue
		}
		label := labels[pkg]
		if label == "" {
			label = pkg
		}
		cat := cats[pkg]
		if cat == "" {
			cat = NormalizeCategory(pkg, "")
		}
		apps = append(apps, AppMinutes{Label: label, Category: cat, Minutes: minutes})
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Minutes > apps[j].Minutes })
	if len(apps) > 5 {
		apps = apps[:5]
	}
	ra.TopApps = apps
	return ra
}

func RecentSignificant(ra RecentActivity, thresholdMin int) bool {
	if thresholdMin <= 0 {
		thresholdMin = 20
	}
	return ra.DistractingMin >= thresholdMin
}

// ActivityNow измеряет отвлечение с момента прошлого замера — это «что происходит
// прямо сейчас». Если телефоном не пользовались (счётчики не выросли), вернёт 0,
// и напоминание слать нельзя, даже если за час суммарно набралось много.
func ActivityNow(history []Sample, cur Sample) (distractMin, gapMin int) {
	for i := len(history) - 1; i >= 0; i-- {
		prev := history[i]
		if !prev.T.Before(cur.T) {
			continue
		}
		return DeltaMinutes(prev.DistrSec, cur.DistrSec), int(cur.T.Sub(prev.T).Minutes())
	}
	return 0, 0
}
