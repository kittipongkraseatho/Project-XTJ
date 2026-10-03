// Package config loads runtime configuration from environment variables.
//
// All fields are populated via envconfig tags. Required fields cause Load()
// to return an error if missing. Defaults are safe for local dev (see infra/.env.example).
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	// Runtime
	Env      string `envconfig:"ENV" default:"development"`
	LogLevel string `envconfig:"LOG_LEVEL" default:"info"`
	APIPort  int    `envconfig:"API_PORT" default:"8080"`

	// Database
	DatabaseURL string `envconfig:"DATABASE_URL" required:"true"`
	DBMaxConns  int32  `envconfig:"DB_MAX_CONNS" default:"10"`

	// Redis (rate limit, future queue)
	RedisURL string `envconfig:"REDIS_URL" required:"true"`

	// CORS
	CORSAllowedOrigins []string `envconfig:"CORS_ALLOWED_ORIGINS" default:"http://localhost:3000"`

	// JWT (EdDSA)
	JWTKeyPath        string        `envconfig:"JWT_ED25519_PRIVATE_KEY_PATH" default:"./keys/ed25519.pem"`
	JWTIssuer         string        `envconfig:"JWT_ISSUER" default:"trading-journal"`
	JWTAudience       string        `envconfig:"JWT_AUDIENCE" default:"trading-journal-web"`
	AccessTokenTTL    time.Duration `envconfig:"ACCESS_TOKEN_TTL" default:"15m"`
	RefreshTokenTTL   time.Duration `envconfig:"REFRESH_TOKEN_TTL" default:"168h"` // 7d

	// OAuth: Google
	OAuthGoogleID       string `envconfig:"OAUTH_GOOGLE_CLIENT_ID"`
	OAuthGoogleSecret   string `envconfig:"OAUTH_GOOGLE_CLIENT_SECRET"`
	OAuthGoogleRedirect string `envconfig:"OAUTH_GOOGLE_REDIRECT_URI"`

	// OAuth: Line
	OAuthLineID       string `envconfig:"OAUTH_LINE_CLIENT_ID"`
	OAuthLineSecret   string `envconfig:"OAUTH_LINE_CLIENT_SECRET"`
	OAuthLineRedirect string `envconfig:"OAUTH_LINE_REDIRECT_URI"`

	// Public web origin (for OAuth callback completion redirect)
	WebBaseURL string `envconfig:"WEB_BASE_URL" default:"http://localhost:3000"`

	// Cloudinary (Phase 1 — trade screenshots)
	CloudinaryCloudName string `envconfig:"CLOUDINARY_CLOUD_NAME"`
	CloudinaryAPIKey    string `envconfig:"CLOUDINARY_API_KEY"`
	CloudinaryAPISecret string `envconfig:"CLOUDINARY_API_SECRET"`
	CloudinaryFolder    string `envconfig:"CLOUDINARY_FOLDER" default:"trading-journal"`

	// Phase 2 — broker sync worker
	SyncPollIntervalSeconds int `envconfig:"SYNC_POLL_INTERVAL_SECONDS" default:"30"`

	// Phase 4 — billing (Stripe)
	StripeSecretKey           string `envconfig:"STRIPE_SECRET_KEY"`
	StripeWebhookSecret       string `envconfig:"STRIPE_WEBHOOK_SECRET"`
	StripePriceUploadJournal   string `envconfig:"STRIPE_PRICE_UPLOAD_JOURNAL"`
	StripePriceStarter         string `envconfig:"STRIPE_PRICE_STARTER"`
	StripePricePro             string `envconfig:"STRIPE_PRICE_PRO"`

	// Phase 4 — email (SMTP)
	SMTPHost     string `envconfig:"SMTP_HOST"`
	SMTPPort     int    `envconfig:"SMTP_PORT" default:"587"`
	SMTPUsername string `envconfig:"SMTP_USERNAME"`
	SMTPPassword string `envconfig:"SMTP_PASSWORD"`
	SMTPFrom     string `envconfig:"SMTP_FROM" default:"Trading Journal <noreply@trading-journal.local>"`

	// Phase 4 — MetaApi
	MetaAPIToken     string `envconfig:"METAAPI_TOKEN"`
	MetaAPIRegion    string `envconfig:"METAAPI_REGION" default:"ap-southeast"`
	MetaAPIAccountID string `envconfig:"METAAPI_ACCOUNT_ID"`

	// Phase 5 — Public API rate limit (per-key token bucket)
	APIPublicRatePerMinute int `envconfig:"API_PUBLIC_RATE_PER_MINUTE" default:"60"`
	APIPublicRateBurst     int `envconfig:"API_PUBLIC_RATE_BURST" default:"10"`

	// Phase 5.1 — Rate limit backend selection (memory | redis)
	RateLimitBackend    string `envconfig:"RATE_LIMIT_BACKEND" default:"memory"`
	RateLimitKeyPrefix  string `envconfig:"RATE_LIMIT_KEY_PREFIX" default:"trj:rl:"`
	RateLimitTTLSeconds int    `envconfig:"RATE_LIMIT_TTL_SECONDS" default:"3600"`

	// Phase 5.2 — Webhook delivery knobs
	WebhookPollIntervalSeconds int `envconfig:"WEBHOOK_POLL_INTERVAL_SECONDS" default:"30"`
	WebhookBatchSize           int `envconfig:"WEBHOOK_BATCH_SIZE" default:"50"`
	WebhookHTTPTimeoutSeconds  int `envconfig:"WEBHOOK_HTTP_TIMEOUT_SECONDS" default:"10"`
	WebhookMaxAttempts         int `envconfig:"WEBHOOK_MAX_ATTEMPTS" default:"5"`
}

// Load reads env vars and returns a Config.
func Load() (*Config, error) {
	var c Config
	if err := envconfig.Process("", &c); err != nil {
		return nil, fmt.Errorf("envconfig: %w", err)
	}
	return &c, nil
}

// GoogleOAuthEnabled returns true iff both client_id and client_secret are set.
func (c *Config) GoogleOAuthEnabled() bool {
	return c.OAuthGoogleID != "" && c.OAuthGoogleSecret != ""
}

// LineOAuthEnabled returns true iff both client_id and client_secret are set.
func (c *Config) LineOAuthEnabled() bool {
	return c.OAuthLineID != "" && c.OAuthLineSecret != ""
}

// CloudinaryEnabled returns true iff all 3 Cloudinary env vars are set.
func (c *Config) CloudinaryEnabled() bool {
	return c.CloudinaryCloudName != "" && c.CloudinaryAPIKey != "" && c.CloudinaryAPISecret != ""
}

// CORSAllowed returns the allowed origins as a set of strings (Echo expects []string).
func (c *Config) CORSAllowed() []string {
	out := make([]string, 0, len(c.CORSAllowedOrigins))
	for _, o := range c.CORSAllowedOrigins {
		if s := strings.TrimSpace(o); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// SyncPollInterval returns the SyncWorker's poll cadence as a Duration. Falls
// back to 30s if the env var is unset or zero.
func (c *Config) SyncPollInterval() time.Duration {
	if c.SyncPollIntervalSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.SyncPollIntervalSeconds) * time.Second
}

// StripeBillingEnabled returns true iff STRIPE_SECRET_KEY is set. Drives the
// provider choice in router.go (Stripe vs Stub).
func (c *Config) StripeBillingEnabled() bool {
	return c.StripeSecretKey != ""
}

// SMTPEnabled returns true iff SMTP_HOST is set.
func (c *Config) SMTPEnabled() bool {
	return c.SMTPHost != ""
}

// MetaApiEnabled returns true iff METAAPI_TOKEN is set. Drives the broker
// registry choice in router.go (MetaApi vs Mock-only).
func (c *Config) MetaApiEnabled() bool {
	return c.MetaAPIToken != ""
}

// RateLimitRedisEnabled returns true iff RATE_LIMIT_BACKEND=redis.
// Phase 5.1: drives the bucket choice in router.go.
func (c *Config) RateLimitRedisEnabled() bool {
	return c.RateLimitBackend == "redis"
}