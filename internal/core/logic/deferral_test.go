package logic

import "testing"

func TestParseDeferralRelative(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{"сейчас занят, начну через 20 минут", 20},
		{"через 5 мин", 5},
		{"Через 45 минуты приступлю", 45},
		{"через 2 часа", 120},
		{"через 1 ч", 60},
		{"через полчаса", 30},
		{"через полтора часа", 90},
		{"через час начну", 60},
		{"через пятнадцать минут", 15},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got, ok := ParseDeferral(tt.text)
			if !ok || got.DelayMin != tt.want {
				t.Fatalf("получено %+v (ok=%v), ожидалась задержка %d", got, ok, tt.want)
			}
			if got.ResumeAt(600) != 600+tt.want {
				t.Fatalf("возобновление посчитано неверно: %d", got.ResumeAt(600))
			}
		})
	}
}

func TestParseDeferralAbsolute(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{"начну в 14:30", 14*60 + 30},
		{"буду к 9.15", 9*60 + 15},
		{"в 7 часов сяду", 7 * 60},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got, ok := ParseDeferral(tt.text)
			if !ok || got.AtMin != tt.want {
				t.Fatalf("получено %+v (ok=%v), ожидалось время %d", got, ok, tt.want)
			}
			if got.ResumeAt(600) != tt.want {
				t.Fatalf("абсолютное время не должно зависеть от текущего: %d", got.ResumeAt(600))
			}
		})
	}
}

func TestParseDeferralIgnoresOtherText(t *testing.T) {
	for _, text := range []string{"", "уже начал", "сделаю сегодня", "не сегодня", "через недельку", "через 900 минут"} {
		if got, ok := ParseDeferral(text); ok {
			t.Fatalf("текст %q не должен считаться отсрочкой, получено %+v", text, got)
		}
	}
}

func TestParseDeferralPrefersExplicitMinutes(t *testing.T) {
	got, ok := ParseDeferral("занят, через 20 минут начну, потом ещё через час перерыв")
	if !ok || got.DelayMin != 20 {
		t.Fatalf("должна выигрывать первая явная задержка, получено %+v", got)
	}
}
