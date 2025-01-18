package cmd

import (
	"context"
	"github.com/lithictech/go-aperitif/v2/convext"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/urfave/cli/v2"
	"github.com/webhookdb/icalproxy/appglobals"
	"github.com/webhookdb/icalproxy/config"
	"log"
	"os"
)

func Execute() {
	app := &cli.App{
		Name: "icalproxy",
		Commands: []*cli.Command{
			dbCmd,
			serverCmd,
		},
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "debug", EnvVars: s1("DEBUG")},
			&cli.StringFlag{Name: "log-file", EnvVars: s1("LOG_FILE"), Usage: "Filename to log to instead of stdout/stderr"},
			&cli.StringFlag{Name: "log-format", EnvVars: s1("LOG_FORMAT"), Usage: "Log format (json, text)"},
			&cli.StringFlag{Name: "log-level", EnvVars: s1("LOG_LEVEL"), Usage: "Log level"},
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func s1(s string) []string {
	return []string{s}
}

func loadAppCtx(ctx context.Context, cfg config.Config) (context.Context, *appglobals.AppGlobals) {
	appCtx, err := appglobals.New(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	return ctx, appCtx
}

func loadConfig(c *cli.Context) config.Config {
	if c.Bool("debug") {
		convext.Must(os.Setenv("LOG_LEVEL", "debug"))
		convext.Must(os.Setenv("DEBUG", "true"))
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	return cfg
}

// Return a context and logger with a logger and process trace id in it.
func loadCtx(c *cli.Context, cfg config.Config) (context.Context, config.Config) {
	logger, err := cfg.NewLogger()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	ctx = logctx.WithLogger(ctx, logger)
	ctx, logger = logctx.AddToR(ctx, string(logctx.ProcessTraceIdKey), logctx.IdProvider())
	logger.InfoContext(ctx, "cli_started",
		"command", c.Command.FullName(),
		"process_pid", os.Getpid(),
		"build_sha", config.BuildSha,
		"build_time", config.BuildTime,
	)
	return ctx, cfg
}
