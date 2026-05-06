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
	SMTPFrom string `env:"SMTP_FROM" envDefault:"JournAI <hello@journai.local>"`

	OpenRouterKey   string `env:"OPENROUTER_API_KEY"`
	OpenRouterModel string `env:"OPENROUTER_MODEL" envDefault:"anthropic/claude-sonnet-4-5"`

	OpenAIKey           string `env:"OPENAI_API_KEY"`
	OpenAIRealtimeModel string `env:"OPENAI_REALTIME_MODEL" envDefault:"gpt-realtime"`

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

func (c *Config) IsDev() bool                 { return c.AppEnv == EnvDev }
func (c *Config) SessionTTL() time.Duration   { return time.Duration(c.SessionTTLHours) * time.Hour }
func (c *Config) MagicLinkTTL() time.Duration { return time.Duration(c.MagicLinkTTLMinutes) * time.Minute }
