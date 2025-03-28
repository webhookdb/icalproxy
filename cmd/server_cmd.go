package cmd

import (
	"context"
	"errors"
	"fmt"
	sentryecho "github.com/getsentry/sentry-go/echo"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/lithictech/go-aperitif/v2/api"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/urfave/cli/v2"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/db"
	"github.com/webhookdb/icalproxy/internal"
	"github.com/webhookdb/icalproxy/notifier"
	"github.com/webhookdb/icalproxy/refresher"
	"github.com/webhookdb/icalproxy/server"
	"log/slog"
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
		if err := db.New(appGlobals.DB).Migrate(ctx); err != nil {
			return internal.ErrWrap(err, "migrating schema")
		}
		logger := logctx.Logger(ctx)
		e := api.New(api.Config{
			Logger: logger,
			LoggingMiddlwareConfig: api.LoggingMiddlwareConfig{
				DoLog: func(c echo.Context, logger *slog.Logger) {
					// Let the load balancer (ELB, Heroku router) do most of the logging work unless we have a server error
					logMethod := logger.Debug
					if c.Response().Status >= 500 {
						logMethod = logger.Error
					}
					logMethod("request_finished")
				},
			},
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
		e.Use(middleware.TimeoutWithConfig(middleware.TimeoutConfig{
			Timeout: time.Duration(appGlobals.Config.HttpRequestTimeout) * time.Second,
		}))

		if err := server.Register(ctx, e, appGlobals); err != nil {
			return internal.ErrWrap(err, "failed to register v1 endpoints")
		}

		cancelCtx, cancel := context.WithCancel(ctx)
		refresher.StartScheduler(cancelCtx, refresher.New(appGlobals))
		notifier.StartScheduler(cancelCtx, notifier.New(appGlobals))

		logger.With("port", appGlobals.Config.Port).InfoContext(ctx, "server_listening")
		if err := e.Start(fmt.Sprintf(":%d", appGlobals.Config.Port)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.With("error", err).ErrorContext(ctx, "server_start")
			panic(err)
		}
		cancel()
		return nil
	},
}
