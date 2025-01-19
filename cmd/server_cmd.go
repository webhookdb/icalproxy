package cmd

import (
	"errors"
	"fmt"
	sentryecho "github.com/getsentry/sentry-go/echo"
	"github.com/labstack/echo/v4"
	"github.com/lithictech/go-aperitif/v2/api"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/urfave/cli/v2"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/db"
	"github.com/webhookdb/icalproxy/internal"
	"github.com/webhookdb/icalproxy/refresher"
	"github.com/webhookdb/icalproxy/server"
	"net/http"
	"time"
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
			HealthHandler: func(c echo.Context) error {
				dbstart := time.Now()
				var dblatency float64
				if _, err := appGlobals.DB.Exec(ctx, "SELECT 1"); err != nil {
					dblatency = -1
				} else {
					dblatency = time.Since(dbstart).Seconds()
				}
				resp := map[string]any{
					"g": 1,
					"d": dblatency,
				}
				return c.JSON(http.StatusOK, resp)
			},
			StatusResponse: map[string]interface{}{
				"build_sha":       config.BuildSha,
				"build_time":      config.BuildTime,
				"release_version": config.ReleaseVersion,
				"message":         "icalproxy",
			},
		})
		e.Use(sentryecho.New(sentryecho.Options{
			Repanic: true,
		}))

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
