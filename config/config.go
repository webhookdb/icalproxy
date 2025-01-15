package config

import (
	"context"
	"github.com/joho/godotenv"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/sethvargo/go-envconfig"
	"log/slog"
	"os"
)

var BuildTime string
var BuildSha string
var ReleaseVersion string

type Config struct {
	DatabaseUrl string `env:"DATABASE_URL, default=postgres://ical:ical@localhost:18042/ical?sslmode=disable"`
	Debug       bool   `env:"DEBUG"`
	LogFile     string `env:"LOG_FILE"`
	LogFormat   string `env:"LOG_FORMAT"`
	LogLevel    string `env:"LOG_LEVEL, default=warn"`
	Port        int    `env:"PORT, default=18041"`
}

func (c Config) NewLogger(fields ...any) (*slog.Logger, error) {
	return NewLoggerAt(c, c.LogLevel, fields...)
}

func LoadConfig() (Config, error) {
	cfg := Config{}
	if err := godotenv.Load(); err != nil {
		return cfg, err
	}

	if err := envconfig.Process(context.Background(), &cfg); err != nil {
		return cfg, err
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
