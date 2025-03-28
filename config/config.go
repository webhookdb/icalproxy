package config

import (
	"context"
	"github.com/joho/godotenv"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/sethvargo/go-envconfig"
	"github.com/webhookdb/icalproxy/internal"
	"github.com/webhookdb/icalproxy/types"
	"log/slog"
	"os"
	"strings"
	"time"
)

var BuildTime string
var BuildSha string
var ReleaseVersion string
var UserAgent = "github.com/webhookdb/icalproxy"

type Config struct {
	// Protect endpoints behind an "Authorization: Apikey <value>" header.
	ApiKey                    string `env:"API_KEY"`
	DatabaseUrl               string `env:"DATABASE_URL, default=postgres://ical:ical@localhost:18042/ical?sslmode=disable"`
	DatabaseConnectionPoolUrl string `env:"DATABASE_CONNECTION_POOL_URL"`
	Debug                     bool   `env:"DEBUG"`
	// The HTTP request timeout, to avoid hung goroutines.
	// 0 is no timeout. If Heroku is detected and 0 is set, use 27s.
	HttpRequestTimeout int    `env:"HTTP_REQUEST_TIMEOUT, default=0"`
	LogFile            string `env:"LOG_FILE"`
	LogFormat          string `env:"LOG_FORMAT"`
	LogLevel           string `env:"LOG_LEVEL, default=info"`
	Port               int    `env:"PORT, default=18041"`
	// Parsed from ICAL_TTL_ vars.
	// See README for details.
	IcalTTLMap map[types.NormalizedHostname]types.TTL
	// Number of feeds that are refreshed at a time before changes are committed to the database.
	// Smaller pages will see more responsive updates, while larger pages may see better performance.
	RefreshPageSize int `env:"REFRESH_PAGE_SIZE, default=100"`
	// Seconds to wait for an origin server before timing out an ICalendar feed request.
	// Only used for the refresh routine.
	RefreshTimeout int `env:"REFRESH_TIMEOUT, default=30"`
	// When requesting an ICS url, and it is not in the database or has an expired TTL,
	// a request is made synchronously. Because this is a slow, blocking request,
	// it should have a fast timeout. If that request times out, the URL is still added
	// to the database so it can be synced by the refresher, in case it's just a slow URL.
	RequestTimeout int `env:"REQUEST_TIMEOUT, default=7"`
	// When requesting an ICS url, and hitting the 'fallback' mode when the database is not available,
	// use this timeout. Generally this should be a touch less than the load balancer timeout.
	// We want to avoid load balancer timeouts since they indicate operations issues,
	// whereas a timeout here is an origin issue.
	RequestMaxTimeout int `env:"REQUEST_MAX_TIMEOUT, default=25"`
	// AWS or similar access key (Cloudflare R2, etc).
	// If empty, use the default AWS config loading behavior.
	S3AccessKeyId string `env:"S3_ACCESS_KEY_ID, default=testkey"`
	// AWS or similar secret (R2, etc).
	S3AccessKeySecret string `env:"S3_ACCESS_KEY_SECRET, default=testsecret"`
	// Bucket to store feeds.
	S3Bucket string `env:"S3_BUCKET, default=icalproxy-feeds"`
	// Endpoint to reach S3, R2, etc.
	// Only set if not empty (so it can be empty for S3, for example).
	// If using Cloudflare R2, set to https://<account id>.r2.cloudflarestorage.com
	S3Endpoint string `env:"S3_ENDPOINT, default=http://localhost:18043"`
	// Key prefix to store feed files under.
	S3Prefix        string `env:"S3_PREFIX, default=icalproxy/feeds"`
	SentryDSN       string `env:"SENTRY_DSN"`
	WebhookPageSize int    `env:"WEBHOOK_PAGE_SIZE, default=100"`
	WebhookUrl      string `env:"WEBHOOK_URL"`
}

func (c Config) NewLogger(fields ...any) (*slog.Logger, error) {
	return NewLoggerAt(c, c.LogLevel, fields...)
}

func LoadConfig() (Config, error) {
	cfg := Config{}
	if err := godotenv.Load(); err != nil && !strings.Contains(err.Error(), "no such file or directory") {
		return cfg, err
	}
	if err := envconfig.Process(context.Background(), &cfg); err != nil {
		return cfg, err
	}
	// If we're running tests with our default database setup,
	// it means we're using pgbouncer, and should enable connection pooling.
	if strings.HasPrefix(cfg.DatabaseUrl, "postgres://ical:ical@localhost:18042/ical") && cfg.DatabaseConnectionPoolUrl == "" {
		cfg.DatabaseConnectionPoolUrl = cfg.DatabaseUrl
	}
	cfg.HttpRequestTimeout = calculateHttpRequestTimeout(cfg)
	if m, err := BuildTTLMap(os.Environ()); err != nil {
		return cfg, err
	} else {
		cfg.IcalTTLMap = m
	}
	return cfg, nil
}

func calculateHttpRequestTimeout(cfg Config) int {
	if cfg.HttpRequestTimeout != 0 {
		// Non-default, use what's configured.
		return cfg.HttpRequestTimeout
	}
	if os.Getenv("DYNO") != "" {
		// Heroku, use a default a few seconds under the 30s platform limit
		return 27
	}
	// Return 0, which is no timeout.
	return 0
}

func BuildTTLMap(environ []string) (map[types.NormalizedHostname]types.TTL, error) {
	m := map[types.NormalizedHostname]types.TTL{}
	for _, e := range environ {
		parts := strings.SplitN(e, "=", 2)
		k, v := parts[0], parts[1]
		// ICAL_TTL_EXAMPLEORG=1h
		if strings.HasPrefix(k, "ICAL_TTL_") {
			d, err := time.ParseDuration(v)
			if err != nil {
				return m, internal.ErrWrap(err, "%s is not a valid duration", k)
			}
			hostname := types.NormalizeHostname(k[len("ICAL_TTL_"):])
			m[hostname] = types.TTL(d)
		}
	}
	return m, nil
}

// NewLoggerAt returns a configured slog.Logger at the given level.
func NewLoggerAt(cfg Config, level string, fields ...any) (*slog.Logger, error) {
	return logctx.NewLogger(logctx.NewLoggerInput{
		Level:  level,
		Format: cfg.LogFormat,
		File:   cfg.LogFile,
		Fields: fields,
		MakeHandler: func(_ *slog.HandlerOptions, h slog.Handler) slog.Handler {
			return logctx.NewTracingHandler(h)
		},
	})
}

func init() {
	if BuildSha == "" {
		BuildSha = os.Getenv("HEROKU_SLUG_COMMIT")
	}
	if ReleaseVersion == "" {
		ReleaseVersion = os.Getenv("HEROKU_RELEASE_VERSION")
	}
	if BuildTime == "" {
		BuildTime = os.Getenv("HEROKU_RELEASE_CREATED_AT")
	}
}
