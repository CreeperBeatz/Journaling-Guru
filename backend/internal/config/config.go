package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v10"
)

type Env string

const (
	EnvDev  Env = "dev"
	EnvProd Env = "prod"
)

type Config struct {
	AppEnv        Env    `env:"APP_ENV" envDefault:"dev"`
	LogLevel      string `env:"LOG_LEVEL" envDefault:"info"`
	HTTPAddr      string `env:"HTTP_ADDR" envDefault:":8080"`
	PublicBaseURL string `env:"PUBLIC_BASE_URL" envDefault:"http://localhost:5173"`

	CORSAllowedOrigins []string `env:"CORS_ALLOWED_ORIGINS" envDefault:"http://localhost:5173" envSeparator:","`

	DatabaseURL string `env:"DATABASE_URL,required"`

	SessionCookieName string `env:"SESSION_COOKIE_NAME" envDefault:"session"`
	SessionTTLHours   int    `env:"SESSION_TTL_HOURS" envDefault:"720"`
	CookieSecure      bool   `env:"COOKIE_SECURE" envDefault:"false"`

	MagicLinkTTLMinutes  int `env:"MAGIC_LINK_TTL_MINUTES" envDefault:"15"`
	MagicLinkPerEmail15m int `env:"MAGIC_LINK_PER_EMAIL_15M" envDefault:"3"`
	MagicLinkPerEmailDay int `env:"MAGIC_LINK_PER_EMAIL_DAY" envDefault:"10"`
	MagicLinkPerIPHour   int `env:"MAGIC_LINK_PER_IP_HOUR" envDefault:"20"`

	SMTPHost string `env:"SMTP_HOST" envDefault:"localhost"`
	SMTPPort int    `env:"SMTP_PORT" envDefault:"1025"`
	SMTPUser string `env:"SMTP_USER"`
	SMTPPass string `env:"SMTP_PASS"`
	SMTPFrom string `env:"SMTP_FROM" envDefault:"Journaling Guru <hello@journai.local>"`

	OpenRouterKey string `env:"OPENROUTER_API_KEY"`
	// Three model tiers, all routed through OpenRouter. Chat is
	// latency-sensitive; summaries are async long-form; classify covers
	// short JSON tasks (emotion classify, chat coverage, post-session
	// extraction). Summary and classify share a default but stay tunable
	// independently.
	ChatModel     string `env:"CHAT_MODEL"     envDefault:"anthropic/claude-sonnet-4-5"`
	SummaryModel  string `env:"SUMMARY_MODEL"  envDefault:"google/gemma-4-26b-a4b-it"`
	ClassifyModel string `env:"CLASSIFY_MODEL" envDefault:"google/gemma-4-26b-a4b-it"`

	// Worker tick + dormancy. SummaryDispatchInterval controls how often the
	// worker scans summary_jobs for due rows. SummaryInactivityDays is the
	// "haven't journaled in N days → stop scheduling new daily/weekly/monthly
	// jobs" threshold (yearly always continues). When the user resumes
	// journaling, lazy-seed re-engages the cadence.
	SummaryDispatchInterval int `env:"SUMMARY_DISPATCH_INTERVAL_SECONDS" envDefault:"60"`
	SummaryInactivityDays   int `env:"SUMMARY_INACTIVITY_DAYS" envDefault:"30"`

	// Number of parallel narrative shots in the weekly synthesis ensemble.
	// 1 = single-shot (skips the combiner). When ≥2, the pipeline is
	// 1 structured pass + N narrative shots + 1 combiner = N+2 calls per
	// week. The narrative shots use identical prompts and rely on sampling
	// diversity; the combiner merges the strongest insights from each.
	SummaryShotCount int `env:"SUMMARY_SHOT_COUNT" envDefault:"4"`

	// DevForceFlags unlocks dev-only request escape hatches (currently
	// ?force_month on the reflection endpoints, which makes the monthly
	// flow testable on any calendar day). NEVER enable in prod.
	DevForceFlags bool `env:"DEV_FORCE_FLAGS" envDefault:"false"`

	OpenAIKey           string `env:"OPENAI_API_KEY"`
	OpenAIRealtimeModel string `env:"OPENAI_REALTIME_MODEL" envDefault:"gpt-realtime"`

	// Chat (Phase 6a). Model defaults live up top with the other
	// tiers; the knobs below are session-shape, not model selection.
	ChatIdleTimeoutMinutes int    `env:"CHAT_IDLE_TIMEOUT_MIN" envDefault:"20"`
	ChatHardCapMinutes     int    `env:"CHAT_HARD_CAP_MIN" envDefault:"30"`
	ChatMaxTurns           int    `env:"CHAT_MAX_TURNS" envDefault:"50"`
	ChatTranscriptKeepLast int    `env:"CHAT_TRANSCRIPT_KEEP_LAST" envDefault:"30"`
	ChatCrisisResourcesURL string `env:"CHAT_CRISIS_RESOURCES_URL" envDefault:"/resources"`

	VAPIDPublic  string `env:"VAPID_PUBLIC_KEY"`
	VAPIDPrivate string `env:"VAPID_PRIVATE_KEY"`
	VAPIDSubject string `env:"VAPID_SUBJECT"`
}

func Load() (*Config, error) {
	c := &Config{}
	if err := env.Parse(c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return c, nil
}

func (c *Config) IsDev() bool               { return c.AppEnv == EnvDev }
func (c *Config) SessionTTL() time.Duration { return time.Duration(c.SessionTTLHours) * time.Hour }
func (c *Config) MagicLinkTTL() time.Duration {
	return time.Duration(c.MagicLinkTTLMinutes) * time.Minute
}
