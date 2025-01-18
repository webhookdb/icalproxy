package cmd

import (
	"github.com/urfave/cli/v2"
	"github.com/webhookdb/icalproxy/db"
)

var dbCmd = &cli.Command{
	Name:  "db",
	Usage: "Run commands on the DB",
	Subcommands: []*cli.Command{
		{
			Name: "migrate",
			Action: func(c *cli.Context) error {
				ctx, appGlobals := loadAppCtx(loadCtx(c, loadConfig(c)))
				return db.Migrate(ctx, appGlobals.DB)
			},
		},
		{
			Name: "reset",
			Action: func(c *cli.Context) error {
				ctx, appGlobals := loadAppCtx(loadCtx(c, loadConfig(c)))
				return db.Reset(ctx, appGlobals.DB)
			},
		},
	},
}
