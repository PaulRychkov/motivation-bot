package logic

import "strings"

var categoryByPrefix = []struct {
	prefix   string
	category string
}{
	{"com.supercell.", "games"},
	{"com.king.", "games"},
	{"com.mojang.", "games"},
	{"com.roblox.", "games"},
	{"com.miHoYo.", "games"},
	{"com.riotgames.", "games"},
	{"com.activision.", "games"},
	{"com.nianticlabs.", "games"},
	{"com.playrix.", "games"},
	{"com.instagram", "social"},
	{"com.zhiliaoapp.musically", "social"},
	{"com.ss.android.ugc", "social"},
	{"com.facebook", "social"},
	{"com.twitter", "social"},
	{"com.snapchat", "social"},
	{"com.reddit", "social"},
	{"com.pinterest", "social"},
	{"com.vkontakte", "social"},
	{"ru.ok.", "social"},
	{"com.google.android.youtube", "video"},
	{"com.netflix", "video"},
	{"tv.twitch", "video"},
	{"com.ivi.", "video"},
	{"ru.kinopoisk", "video"},
	{"ru.rutube", "video"},
}

var gameKeywords = []string{"game", "clash", "royale", "pubg", "minecraft"}

func NormalizeCategory(pkg, phoneCategory string) string {
	if Distracting(phoneCategory) || phoneCategory == "communication" {
		return phoneCategory
	}
	p := strings.ToLower(pkg)
	for _, m := range categoryByPrefix {
		if strings.HasPrefix(p, strings.ToLower(m.prefix)) {
			return m.category
		}
	}
	for _, kw := range gameKeywords {
		if strings.Contains(p, kw) {
			return "games"
		}
	}
	if phoneCategory == "" {
		return "other"
	}
	return phoneCategory
}
