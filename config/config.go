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
	ApiKey      string `env:"API_KEY"`
	DatabaseUrl string `env:"DATABASE_URL, default=postgres://ical:ical@localhost:18042/ical?sslmode=disable"`
	Debug       bool   `env:"DEBUG"`
	LogFile     string `env:"LOG_FILE"`
	LogFormat   string `env:"LOG_FORMAT"`
	LogLevel    string `env:"LOG_LEVEL, default=info"`
	Port        int    `env:"PORT, default=18041"`
	// Parsed from ICAL_TTL_ vars.
	// See README for details.
	IcalTTLMap map[types.NormalizedHostname]types.TTL
	// Number of feeds that are refreshed at a time before changes are committed to the database.
	// Smaller pages will see more responsive updates, while larger pages may see better performance.
	RefreshPageSize int `env:"REFRESH_PAGE_SIZE, default=100"`
	// Seconds to wait for an origin server before timing out an ICalendar feed request.
	// Only used for the refresh routine.
	RefreshTimeout  int    `env:"REFRESH_TIMEOUT, default=30"`
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
	if m, err := BuildTTLMap(os.Environ()); err != nil {
		return cfg, err
	} else {
		cfg.IcalTTLMap = m
	}
	return cfg, nil
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
