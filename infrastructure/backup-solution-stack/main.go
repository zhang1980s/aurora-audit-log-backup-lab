package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"backup-solution-stack/config"
	"backup-solution-stack/resources"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Load configuration
		cfg, err := config.LoadConfig(ctx)
		if err != nil {
			return err
		}

		// Get network stack reference
		networkStack, err := pulumi.NewStackReference(ctx, "zhang1980s/aurora-network/dev", nil)
		if err != nil {
			return err
		}

		// Get ECR stack reference
		ecrStack, err := pulumi.NewStackReference(ctx, "zhang1980s/aurora-ecr/dev", nil)
		if err != nil {
			return err
		}

		// Create backup solution resources
		backupResources, err := resources.CreateBackupResources(ctx, cfg, networkStack, ecrStack)
		if err != nil {
			return err
		}

		// Export backup solution outputs
		ctx.Export("logBucketName", backupResources.LogBucket.ID())
		ctx.Export("dynamoDBTableName", backupResources.DynamoDBTable.Name)
		ctx.Export("sqsQueueUrl", backupResources.SQSQueue.Url)
		ctx.Export("dbScannerLambdaArn", backupResources.DBScannerLambda.Arn)
		ctx.Export("logDetectorLambdaArn", backupResources.LogDetectorLambda.Arn)
		ctx.Export("logDownloaderLambdaArn", backupResources.LogDownloaderLambda.Arn)
		ctx.Export("dbScannerLambdaAliasArn", backupResources.DBScannerLambdaAlias.Arn)
		ctx.Export("logDetectorLambdaAliasArn", backupResources.LogDetectorLambdaAlias.Arn)
		ctx.Export("logDownloaderLambdaAliasArn", backupResources.LogDownloaderLambdaAlias.Arn)
		ctx.Export("eventBridgeRuleName", backupResources.EventBridgeRule.Name)

		return nil
	})
}