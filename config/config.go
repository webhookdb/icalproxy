package config

import (
	"context"
	"github.com/joho/godotenv"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/sethvargo/go-envconfig"
	"github.com/webhookdb/icalproxy/internal"
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
	ApiKey        string        `env:"API_KEY"`
	DatabaseUrl   string        `env:"DATABASE_URL, default=postgres://ical:ical@localhost:18042/ical?sslmode=disable"`
	Debug         bool          `env:"DEBUG"`
	LogFile       string        `env:"LOG_FILE"`
	LogFormat     string        `env:"LOG_FORMAT"`
	LogLevel      string        `env:"LOG_LEVEL, default=info"`
	Port          int           `env:"PORT, default=18041"`
	IcalBaseTTL   time.Duration `env:"ICAL_BASE_TTL, default=2h"`
	IcalICloudTTL time.Duration `env:"ICAL_ICLOUD_TTL, default=5m"`
	// Parsed from ICAL_TTL_ vars.
	// See README for details.
	IcalTTLs map[string]time.Duration
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
	cfg.IcalTTLs = make(map[string]time.Duration)
	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		k, v := parts[0], parts[1]
		// ICAL_TTL_EXAMPLEORG=1h
		if strings.HasPrefix(k, "ICAL_TTL_") {
			d, err := time.ParseDuration(v)
			if err != nil {
				return cfg, internal.EWrap(err, "%s is not a valid duration", k)
			}
			hostname := strings.ToUpper(k[len("ICAL_TTL_"):])
			cfg.IcalTTLs[hostname] = d
		}
	}
	return cfg, nil
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
