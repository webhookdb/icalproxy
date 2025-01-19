package internal

import (
	"context"
	"github.com/getsentry/sentry-go"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"reflect"
	"strings"
	"time"
)

type IRunner interface {
	Run(ctx context.Context) error
}

func StartScheduler(ctx context.Context, r IRunner, interval time.Duration) {
	schedulerName := strings.ToLower(reflect.Indirect(reflect.ValueOf(r)).Type().Name())
	ctx = logctx.AddTo(ctx, "scheduler", schedulerName)
	logctx.Logger(ctx).Info("scheduler_starting")
	go func() {
		for {
			logctx.Logger(ctx).Info("scheduler_starting_run")
			if err := r.Run(ctx); err != nil {
				logctx.Logger(ctx).With("error", err).Error("scheduler_run_error")
				sentry.WithScope(func(scope *sentry.Scope) {
					scope.SetTag("scheduler", schedulerName)
					sentry.CaptureException(err)
				})
			} else {
				logctx.Logger(ctx).Info("scheduler_finished_run")
			}
			select {
			case <-time.After(interval):
				continue
			case <-ctx.Done():
				logctx.Logger(ctx).Info("scheduler_closing")
				return
			}
		}
	}()
}
