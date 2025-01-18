package cmd

import (
	"errors"
	"fmt"
	"github.com/lithictech/go-aperitif/v2/api"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/urfave/cli/v2"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/db"
	"github.com/webhookdb/icalproxy/internal"
	"github.com/webhookdb/icalproxy/refresher"
	"github.com/webhookdb/icalproxy/server"
	"net/http"
)

var serverCmd = &cli.Command{
	Name:  "server",
	Usage: "Run the web server",
	Flags: []cli.Flag{
		&cli.IntFlag{Name: "port", Aliases: s1("p"), Value: 0, Usage: "port to bind to", EnvVars: s1("PORT")},
	},
	Action: func(c *cli.Context) error {
		ctx, appGlobals := loadAppCtx(loadCtx(c, loadConfig(c)))
		if err := db.Migrate(ctx, appGlobals.DB); err != nil {
			return internal.ErrWrap(err, "migrating schema")
		}
		logger := logctx.Logger(ctx)
		e := api.New(api.Config{
			Logger:                 logger,
			LoggingMiddlwareConfig: api.LoggingMiddlwareConfig{},
			HealthResponse:         map[string]interface{}{"o": "k"},
			StatusResponse: map[string]interface{}{
				"build_sha":       config.BuildSha,
				"build_time":      config.BuildTime,
				"release_version": config.ReleaseVersion,
				"message":         "icalproxy",
			},
		})

		if err := server.Register(ctx, e, appGlobals); err != nil {
			return internal.ErrWrap(err, "failed to register v1 endpoints")
		}

		refresher.StartScheduler(ctx, refresher.New(appGlobals))

		logger.With("port", appGlobals.Config.Port).InfoContext(ctx, "server_listening")
		if err := e.Start(fmt.Sprintf(":%d", appGlobals.Config.Port)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.With("error", err).ErrorContext(ctx, "server_start")
			panic(err)
		}
		return nil
	},
}
