package logic

import (
	"testing"
	"time"
)

func sampleDay() []DayApp {
	return []DayApp{
		{Package: "com.google.android.youtube", Label: "Youtube", Category: "video", ForegroundSeconds: 4927},
		{Package: "com.supercell.clashroyale", Label: "Clashroyale", Category: "other", ForegroundSeconds: 1633},
		{Package: "com.android.chrome", Label: "Chrome", Category: "other", ForegroundSeconds: 1254},
		{Package: "org.telegram.messenger", Label: "Messenger", Category: "communication", ForegroundSeconds: 1116},
		{Package: "com.android.launcher", Label: "System Launcher", Category: "other", ForegroundSeconds: 624},
	}
}

func TestAnalyzeDay(t *testing.T) {
	rep := AnalyzeDay(sampleDay())

	if rep.ByCategory["games"] != 27 {
		t.Errorf("games %d, ожидалось 27 (Clash Royale переклассифицирован)", rep.ByCategory["games"])
	}
	if rep.ByCategory["video"] != 82 {
		t.Errorf("video %d, ожидалось 82", rep.ByCategory["video"])
	}
	if rep.DistractingMinutes != 109 {
		t.Errorf("отвлекающих минут %d, ожидалось 109 (82 видео + 27 игры)", rep.DistractingMinutes)
	}
	if len(rep.TopDistracting) != 2 {
		t.Fatalf("топ отвлекающих %d, ожидалось 2", len(rep.TopDistracting))
	}
	if rep.TopDistracting[0].Label != "Youtube" || rep.TopDistracting[0].Minutes != 82 {
		t.Errorf("первый в топе %+v, ожидался Youtube 82", rep.TopDistracting[0])
	}
	if rep.TopDistracting[1].Category != "games" {
		t.Errorf("второй в топе категория %q, ожидалась games", rep.TopDistracting[1].Category)
	}
}

func TestEvaluateActivity(t *testing.T) {
	now := time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC)
	rep := AnalyzeDay(sampleDay())

	t.Run("первый раз за день превышен порог", func(t *testing.T) {
		d := EvaluateActivity(rep, 90, 0, time.Time{}, now, 60)
		if !d.Nudge {
			t.Fatalf("ожидалась рекомендация: отвлечение %d >= 90", rep.DistractingMinutes)
		}
		if d.Watermark != 109 {
			t.Errorf("watermark %d, ожидался 109", d.Watermark)
		}
	})

	t.Run("после напоминания прирост меньше порога", func(t *testing.T) {
		d := EvaluateActivity(rep, 90, 109, time.Time{}, now, 60)
		if d.Nudge {
			t.Fatal("не ожидалась рекомендация: прирост 0 < 90")
		}
	})

	t.Run("не прошёл перерыв после прошлой рекомендации", func(t *testing.T) {
		d := EvaluateActivity(rep, 90, 0, now.Add(-10*time.Minute), now, 60)
		if d.Nudge {
			t.Fatal("не ожидалась рекомендация внутри cooldown")
		}
	})

	t.Run("перерыв прошёл", func(t *testing.T) {
		d := EvaluateActivity(rep, 90, 0, now.Add(-90*time.Minute), now, 60)
		if !d.Nudge {
			t.Fatal("ожидалась рекомендация после cooldown")
		}
	})

	t.Run("порог не задан - берётся 90", func(t *testing.T) {
		d := EvaluateActivity(rep, 0, 0, time.Time{}, now, 60)
		if !d.Nudge {
			t.Fatal("ожидалась рекомендация при пороге по умолчанию")
		}
	})
}

func TestComputeRecent(t *testing.T) {
	base := time.Date(2026, 7, 22, 14, 0, 0, 0, time.UTC)
	labels := map[string]string{"com.google.android.youtube": "Youtube", "com.supercell.clashroyale": "Clashroyale"}
	cats := map[string]string{"com.google.android.youtube": "video", "com.supercell.clashroyale": "games"}

	t.Run("час назад есть опорная точка", func(t *testing.T) {
		history := []Sample{
			{T: base, DistrSec: 3600, ScreenSec: 7200, Apps: map[string]int64{"com.google.android.youtube": 3600}},
			{T: base.Add(20 * time.Minute), DistrSec: 4200, ScreenSec: 8000, Apps: map[string]int64{"com.google.android.youtube": 4200}},
		}
		cur := Sample{T: base.Add(60 * time.Minute), DistrSec: 5100, ScreenSec: 9000, Apps: map[string]int64{"com.google.android.youtube": 5100}}
		ra := ComputeRecent(history, cur, 60*time.Minute, labels, cats)
		if ra.DistractingMin != 25 {
			t.Errorf("отвлечение за час %d, ожидалось 25 ((5100-3600)/60)", ra.DistractingMin)
		}
		if ra.SpanMinutes != 60 {
			t.Errorf("охват %d, ожидалось 60", ra.SpanMinutes)
		}
		if len(ra.TopApps) != 1 || ra.TopApps[0].Label != "Youtube" || ra.TopApps[0].Minutes != 25 {
			t.Errorf("топ %+v, ожидался Youtube 25", ra.TopApps)
		}
	})

	t.Run("нет истории — пусто", func(t *testing.T) {
		cur := Sample{T: base, DistrSec: 5100, Apps: map[string]int64{"com.google.android.youtube": 5100}}
		ra := ComputeRecent(nil, cur, 60*time.Minute, labels, cats)
		if ra.DistractingMin != 0 || len(ra.TopApps) != 0 {
			t.Errorf("ожидалась пустая активность, получено %+v", ra)
		}
	})

	t.Run("окно короче часа — берётся самая старая точка", func(t *testing.T) {
		history := []Sample{
			{T: base.Add(35 * time.Minute), DistrSec: 4000, ScreenSec: 8000, Apps: map[string]int64{"com.supercell.clashroyale": 4000}},
		}
		cur := Sample{T: base.Add(60 * time.Minute), DistrSec: 5500, ScreenSec: 9500, Apps: map[string]int64{"com.supercell.clashroyale": 5500}}
		ra := ComputeRecent(history, cur, 60*time.Minute, labels, cats)
		if ra.SpanMinutes != 25 {
			t.Errorf("охват %d, ожидалось 25", ra.SpanMinutes)
		}
		if ra.DistractingMin != 25 {
			t.Errorf("отвлечение %d, ожидалось 25", ra.DistractingMin)
		}
	})

	t.Run("счётчик сброшен — дельта не уходит в минус", func(t *testing.T) {
		history := []Sample{{T: base, DistrSec: 5000, ScreenSec: 9000, Apps: map[string]int64{"com.google.android.youtube": 5000}}}
		cur := Sample{T: base.Add(60 * time.Minute), DistrSec: 120, ScreenSec: 200, Apps: map[string]int64{"com.google.android.youtube": 120}}
		ra := ComputeRecent(history, cur, 60*time.Minute, labels, cats)
		if ra.DistractingMin != 0 {
			t.Errorf("при сбросе счётчика ожидалось 0, получено %d", ra.DistractingMin)
		}
	})
}

func TestActivityNow(t *testing.T) {
	base := time.Date(2026, 7, 22, 14, 0, 0, 0, time.UTC)

	t.Run("телефон использовался с прошлого замера", func(t *testing.T) {
		history := []Sample{{T: base, DistrSec: 5000}}
		cur := Sample{T: base.Add(10 * time.Minute), DistrSec: 5300}
		now, gap := ActivityNow(history, cur)
		if now != 5 {
			t.Errorf("сейчас %d, ожидалось 5 ((5300-5000)/60)", now)
		}
		if gap != 10 {
			t.Errorf("зазор %d, ожидалось 10", gap)
		}
	})

	t.Run("телефон простаивал — активности сейчас нет", func(t *testing.T) {
		history := []Sample{{T: base, DistrSec: 5000}}
		cur := Sample{T: base.Add(35 * time.Minute), DistrSec: 5000}
		now, _ := ActivityNow(history, cur)
		if now != 0 {
			t.Errorf("при простое ожидалось 0, получено %d", now)
		}
	})

	t.Run("нет прошлого замера", func(t *testing.T) {
		now, gap := ActivityNow(nil, Sample{T: base, DistrSec: 5000})
		if now != 0 || gap != 0 {
			t.Errorf("без истории ожидалось 0,0, получено %d,%d", now, gap)
		}
	})
}

func TestRecentSignificant(t *testing.T) {
	if !RecentSignificant(RecentActivity{DistractingMin: 25}, 20) {
		t.Error("25 мин отвлечения за час должны быть значимы при пороге 20")
	}
	if RecentSignificant(RecentActivity{DistractingMin: 15}, 20) {
		t.Error("15 мин не должны срабатывать при пороге 20")
	}
	if !RecentSignificant(RecentActivity{DistractingMin: 20}, 0) {
		t.Error("при пороге 0 берётся дефолт 20, 20 должно срабатывать")
	}
}

func TestNormalizeCategory(t *testing.T) {
	tests := []struct {
		pkg   string
		phone string
		want  string
	}{
		{"com.supercell.clashroyale", "other", "games"},
		{"com.instagram.android", "other", "social"},
		{"com.google.android.youtube", "video", "video"},
		{"org.telegram.messenger", "communication", "communication"},
		{"com.android.chrome", "other", "other"},
		{"com.some.unknowngame", "other", "games"},
		{"ru.ozon.app.android", "", "other"},
	}
	for _, tc := range tests {
		if got := NormalizeCategory(tc.pkg, tc.phone); got != tc.want {
			t.Errorf("NormalizeCategory(%q,%q) = %q, ожидалось %q", tc.pkg, tc.phone, got, tc.want)
		}
	}
}
