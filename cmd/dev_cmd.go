package cmd

import (
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/lithictech/go-aperitif/v2/convext"
	"github.com/urfave/cli/v2"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/feedstorage"
)

var devCmd = &cli.Command{
	Name: "dev",
	Subcommands: []*cli.Command{
		{
			Name: "create-s3-bucket",
			Action: func(c *cli.Context) error {
				ctx, appGlobals := loadAppCtx(loadCtx(c, loadConfig(c)))
				fs, err := feedstorage.New(ctx, appGlobals.Config)
				if err != nil {
					return err
				}
				if _, err := fs.S3Client().CreateBucket(ctx, &s3.CreateBucketInput{
					Bucket: aws.String(appGlobals.Config.S3Bucket),
				}); err != nil {
					return err
				}
				return nil
			},
		},
		{
			Name:  "config",
			Usage: "Print out config and exit. Used to help debug config loading behavior",
			Action: func(c *cli.Context) error {
				cfg, err := config.LoadConfig()
				if err != nil {
					return err
				}
				obj, err := convext.ToObject(cfg)
				if err != nil {
					fmt.Println("BuildTime:", config.BuildTime, "BuildSha:", config.BuildSha)
					fmt.Printf("%+v\n", cfg)
				} else {
					fmt.Println("BuildTime:", config.BuildTime)
					fmt.Println("BuildSha:", config.BuildSha)
					for _, k := range convext.SortedObjectKeys(obj) {
						fmt.Printf("%s: %v\n", k, obj[k])
					}
				}
				return nil
			},
		},
	},
}
