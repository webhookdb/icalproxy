package cmd

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/urfave/cli/v2"
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
	},
}
