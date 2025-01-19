package internal

import (
	"context"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"time"
)

type IRunner interface {
	Run(ctx context.Context) error
}

func StartScheduler(ctx context.Context, r IRunner, interval time.Duration) {
	logctx.Logger(ctx).Info("starting_scheduler")
	go func() {
		for {
			logctx.Logger(ctx).Info("executing_scheduled")
			if err := r.Run(ctx); err != nil {
				logctx.Logger(ctx).With("error", err).Error("scheduled_job_error")
			} else {
				logctx.Logger(ctx).Info("executed_scheduled")
			}
			select {
			case <-time.After(interval):
				continue
			case <-ctx.Done():
				logctx.Logger(ctx).Info("canceling_scheduler")
				return
			}
		}
	}()
}
