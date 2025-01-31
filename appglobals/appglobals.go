package appglobals

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lithictech/go-aperitif/v2/logctx"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/feedstorage"
	"github.com/webhookdb/icalproxy/pgxt"
)

type AppGlobals struct {
	Config      config.Config
	DB          *pgxpool.Pool
	FeedStorage feedstorage.Interface
}

func New(ctx context.Context, cfg config.Config) (ac *AppGlobals, err error) {
	logger := logctx.LoggerOrNil(ctx)
	if logger == nil {
		return nil, errors.New("context must have an existing logger")
	}
	ac = &AppGlobals{}
	ac.Config = cfg
	dbUrl := ac.Config.DatabaseUrl
	if ac.Config.DatabaseConnectionPoolUrl != "" {
		dbUrl = ac.Config.DatabaseConnectionPoolUrl
	}
	if ac.DB, err = pgxt.ConnectToUrl(dbUrl, func(p *pgxpool.Config) {
		p.ConnConfig.Tracer = pgxt.NewLoggingTracer()
		if ac.Config.DatabaseConnectionPoolUrl != "" {
			// pgx uses prepared statements by default, but pgbouncer doesn't work right with them
			p.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
		}
	}); err != nil {
		return
	}
	if ac.FeedStorage, err = feedstorage.New(ctx, cfg); err != nil {
		return
	}
	return
}
