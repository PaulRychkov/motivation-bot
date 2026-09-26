package logic

import (
	"regexp"
	"strconv"
	"strings"
)

type Deferral struct {
	DelayMin int
	AtMin    int
}

var (
	relativeRe = regexp.MustCompile(`(?i)через\s+(\d{1,3})\s*(минуток|минуты|минут|мин|часов|часа|час|ч|м)(?:[^\p{L}]|$)`)
	absoluteRe = regexp.MustCompile(`(?i)(?:в|к)\s+(\d{1,2})[:.](\d{2})`)
	wholeHour  = regexp.MustCompile(`(?i)(?:в|к)\s+(\d{1,2})\s*(?:часов|часа|час|ч)(?:[^\p{L}]|$)`)
)

var wordDelays = []struct {
	fragment string
	minutes  int
}{
	{"через полчаса", 30},
	{"через пол часа", 30},
	{"через полтора часа", 90},
	{"через час", 60},
	{"через минуту", 1},
	{"через пару минут", 2},
	{"через пять минут", 5},
	{"через десять минут", 10},
	{"через пятнадцать минут", 15},
	{"через двадцать минут", 20},
	{"через тридцать минут", 30},
	{"через сорок минут", 40},
}

func ParseDeferral(text string) (Deferral, bool) {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return Deferral{}, false
	}
	if m := relativeRe.FindStringSubmatch(lower); m != nil {
		n, err := strconv.Atoi(m[1])
		if err == nil && n > 0 {
			unit := m[2]
			if strings.HasPrefix(unit, "ч") {
				n *= 60
			}
			if n <= 12*60 {
				return Deferral{DelayMin: n, AtMin: -1}, true
			}
		}
	}
	for _, w := range wordDelays {
		if strings.Contains(lower, w.fragment) {
			return Deferral{DelayMin: w.minutes, AtMin: -1}, true
		}
	}
	if m := absoluteRe.FindStringSubmatch(lower); m != nil {
		h, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		if h < 24 && min < 60 {
			return Deferral{DelayMin: -1, AtMin: h*60 + min}, true
		}
	}
	if m := wholeHour.FindStringSubmatch(lower); m != nil {
		h, _ := strconv.Atoi(m[1])
		if h < 24 {
			return Deferral{DelayMin: -1, AtMin: h * 60}, true
		}
	}
	return Deferral{}, false
}

func (d Deferral) ResumeAt(localMin int) int {
	if d.AtMin >= 0 {
		return d.AtMin
	}
	return localMin + d.DelayMin
}
