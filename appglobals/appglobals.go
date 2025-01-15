package appglobals

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/pgxt"
)

type AppGlobals struct {
	Config config.Config
	DB     *pgxpool.Pool
}

func New(ctx context.Context, cfg config.Config) (ac *AppGlobals, err error) {
	logger := logctx.LoggerOrNil(ctx)
	if logger == nil {
		return nil, errors.New("context must have an existing logger")
	}
	ac = &AppGlobals{}
	ac.Config = cfg
	if ac.DB, err = pgxt.ConnectToUrl(cfg.DatabaseUrl, func(p *pgxpool.Config) {
		p.ConnConfig.Tracer = pgxt.NewLoggingTracer()
	}); err != nil {
		return
	}
	return
}
