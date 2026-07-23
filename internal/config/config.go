package config

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	HTTPPort          int
	DBHost            string
	DBPort            int
	DBUser            string
	DBPassword        string
	DBName            string
	DBSSLMode         string
	KafkaBrokers      []string
	TelegramToken     string
	OpenRouterAPIKey  string
	OpenRouterBaseURL string
	LLMModel          string
	LLMTimeoutSec     int
	ReactMaxIter      int
	CoreURL           string
	TasksURL          string
	PomodoroURL       string
	TasksMCPURL       string
	PomodoroMCPURL    string
	PromptDir         string
	MigrationsDir     string
	DefaultTimezone   string
	DispatchPeriodSec int
	WindowWorkerSec   int
	DeadlinePollSec   int
	SchedulerSec      int
	OutboxRelaySec    int

	PhoneIngestToken       string
	DistractionMinutes     int
	DistractionCooldownMin int
	PhoneDailyDistractMin  int
	PhoneRecentWindowMin   int
	PhoneRecentDistractMin int
	PhoneRecentCooldownMin int
	PhoneNowDistractMin    int
	PhoneEffectCheckMin    int
	PhoneEffectWorkedMin   int
	PhoneRestStartMin      int
	PhoneRestEndMin        int

	ProactivePollSec        int
	MeetingLeadMin          int
	WorkNudgeCooldownMin    int
	WorkNudgePomodoroGapMin int
	WorkNudgePhoneMin       int
	StallGapMin             int
	StallCooldownMin        int
}

func Load() Config {
	loadDotEnv(".env")

	v := viper.New()
	v.SetEnvPrefix("BOT")
	v.AutomaticEnv()

	v.SetDefault("HTTP_PORT", 8083)
	v.SetDefault("DB_HOST", "localhost")
	v.SetDefault("DB_PORT", 5435)
	v.SetDefault("DB_USER", "bot")
	v.SetDefault("DB_PASSWORD", "bot")
	v.SetDefault("DB_NAME", "bot")
	v.SetDefault("DB_SSLMODE", "disable")
	v.SetDefault("KAFKA_BROKERS", "localhost:9094")
	v.SetDefault("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1")
	v.SetDefault("LLM_MODEL", "deepseek/deepseek-chat")
	v.SetDefault("LLM_TIMEOUT_SECONDS", 90)
	v.SetDefault("REACT_MAX_ITERATIONS", 8)
	v.SetDefault("CORE_URL", "http://localhost:8083")
	v.SetDefault("TASKS_URL", "http://localhost:8081")
	v.SetDefault("POMODORO_URL", "http://localhost:8082")
	v.SetDefault("TASKS_MCP_URL", "http://localhost:8081/mcp")
	v.SetDefault("POMODORO_MCP_URL", "http://localhost:8082/mcp")
	v.SetDefault("PROMPT_DIR", "prompts")
	v.SetDefault("MIGRATIONS_DIR", "migrations")
	v.SetDefault("DEFAULT_TIMEZONE", "Europe/Moscow")
	v.SetDefault("DISPATCH_PERIOD_SECONDS", 5)
	v.SetDefault("WINDOW_WORKER_SECONDS", 300)
	v.SetDefault("DEADLINE_POLL_SECONDS", 900)
	v.SetDefault("SCHEDULER_SECONDS", 60)
	v.SetDefault("OUTBOX_RELAY_SECONDS", 5)
	v.SetDefault("DISTRACTION_MINUTES", 15)
	v.SetDefault("DISTRACTION_COOLDOWN_MINUTES", 60)
	v.SetDefault("PHONE_DAILY_DISTRACT_MINUTES", 90)
	v.SetDefault("PHONE_RECENT_WINDOW_MINUTES", 60)
	v.SetDefault("PHONE_RECENT_DISTRACT_MINUTES", 20)
	v.SetDefault("PHONE_RECENT_COOLDOWN_MINUTES", 45)
	v.SetDefault("PHONE_NOW_DISTRACT_MINUTES", 2)
	v.SetDefault("PHONE_EFFECT_CHECK_MINUTES", 30)
	v.SetDefault("PHONE_EFFECT_WORKED_MINUTES", 8)
	v.SetDefault("PHONE_REST_START_MINUTES", 1200)
	v.SetDefault("PHONE_REST_END_MINUTES", 300)
	v.SetDefault("PROACTIVE_POLL_SECONDS", 300)
	v.SetDefault("MEETING_LEAD_MINUTES", 15)
	v.SetDefault("WORK_NUDGE_COOLDOWN_MINUTES", 90)
	v.SetDefault("WORK_NUDGE_POMODORO_GAP_MINUTES", 90)
	v.SetDefault("WORK_NUDGE_PHONE_MINUTES", 5)
	v.SetDefault("STALL_GAP_MINUTES", 120)
	v.SetDefault("STALL_COOLDOWN_MINUTES", 150)

	return Config{
		HTTPPort:          v.GetInt("HTTP_PORT"),
		DBHost:            v.GetString("DB_HOST"),
		DBPort:            v.GetInt("DB_PORT"),
		DBUser:            v.GetString("DB_USER"),
		DBPassword:        v.GetString("DB_PASSWORD"),
		DBName:            v.GetString("DB_NAME"),
		DBSSLMode:         v.GetString("DB_SSLMODE"),
		KafkaBrokers:      splitCSV(v.GetString("KAFKA_BROKERS")),
		TelegramToken:     strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		OpenRouterAPIKey:  strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")),
		OpenRouterBaseURL: strings.TrimRight(v.GetString("OPENROUTER_BASE_URL"), "/"),
		LLMModel:          v.GetString("LLM_MODEL"),
		LLMTimeoutSec:     v.GetInt("LLM_TIMEOUT_SECONDS"),
		ReactMaxIter:      v.GetInt("REACT_MAX_ITERATIONS"),
		CoreURL:           strings.TrimRight(v.GetString("CORE_URL"), "/"),
		TasksURL:          strings.TrimRight(v.GetString("TASKS_URL"), "/"),
		PomodoroURL:       strings.TrimRight(v.GetString("POMODORO_URL"), "/"),
		TasksMCPURL:       v.GetString("TASKS_MCP_URL"),
		PomodoroMCPURL:    v.GetString("POMODORO_MCP_URL"),
		PromptDir:         v.GetString("PROMPT_DIR"),
		MigrationsDir:     v.GetString("MIGRATIONS_DIR"),
		DefaultTimezone:   v.GetString("DEFAULT_TIMEZONE"),
		DispatchPeriodSec: v.GetInt("DISPATCH_PERIOD_SECONDS"),
		WindowWorkerSec:   v.GetInt("WINDOW_WORKER_SECONDS"),
		DeadlinePollSec:   v.GetInt("DEADLINE_POLL_SECONDS"),
		SchedulerSec:      v.GetInt("SCHEDULER_SECONDS"),
		OutboxRelaySec:    v.GetInt("OUTBOX_RELAY_SECONDS"),

		PhoneIngestToken:       strings.TrimSpace(v.GetString("PHONE_INGEST_TOKEN")),
		DistractionMinutes:     v.GetInt("DISTRACTION_MINUTES"),
		DistractionCooldownMin: v.GetInt("DISTRACTION_COOLDOWN_MINUTES"),
		PhoneDailyDistractMin:  v.GetInt("PHONE_DAILY_DISTRACT_MINUTES"),
		PhoneRecentWindowMin:   v.GetInt("PHONE_RECENT_WINDOW_MINUTES"),
		PhoneRecentDistractMin: v.GetInt("PHONE_RECENT_DISTRACT_MINUTES"),
		PhoneRecentCooldownMin: v.GetInt("PHONE_RECENT_COOLDOWN_MINUTES"),
		PhoneNowDistractMin:    v.GetInt("PHONE_NOW_DISTRACT_MINUTES"),
		PhoneEffectCheckMin:    v.GetInt("PHONE_EFFECT_CHECK_MINUTES"),
		PhoneEffectWorkedMin:   v.GetInt("PHONE_EFFECT_WORKED_MINUTES"),
		PhoneRestStartMin:      v.GetInt("PHONE_REST_START_MINUTES"),
		PhoneRestEndMin:        v.GetInt("PHONE_REST_END_MINUTES"),

		ProactivePollSec:        v.GetInt("PROACTIVE_POLL_SECONDS"),
		MeetingLeadMin:          v.GetInt("MEETING_LEAD_MINUTES"),
		WorkNudgeCooldownMin:    v.GetInt("WORK_NUDGE_COOLDOWN_MINUTES"),
		WorkNudgePomodoroGapMin: v.GetInt("WORK_NUDGE_POMODORO_GAP_MINUTES"),
		WorkNudgePhoneMin:       v.GetInt("WORK_NUDGE_PHONE_MINUTES"),
		StallGapMin:             v.GetInt("STALL_GAP_MINUTES"),
		StallCooldownMin:        v.GetInt("STALL_COOLDOWN_MINUTES"),
	}
}

func (c Config) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode)
}

func (c Config) MigrateURL() string {
	return fmt.Sprintf("pgx5://%s:%s@%s:%d/%s?sslmode=%s",
		url.QueryEscape(c.DBUser), url.QueryEscape(c.DBPassword), c.DBHost, c.DBPort, c.DBName, c.DBSSLMode)
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.Trim(strings.TrimSpace(line[eq+1:]), `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
}
