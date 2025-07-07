package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"aurora-audit-log-backup-lab/config"
	"aurora-audit-log-backup-lab/resources"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Load configuration
		cfg, err := config.LoadConfig(ctx)
		if err != nil {
			return err
		}

		// Get ECR repository URLs from ECR stack
		ecrStack, err := pulumi.NewStackReference(ctx, "zhang1980s/aurora-ecr/dev", nil)
		if err != nil {
			return err
		}

		// 1. Create fundamental network environment
		networkResources, err := resources.CreateNetworkResources(ctx, cfg)
		if err != nil {
			return err
		}

		// 2. Create log backup resources
		logBackupResources, err := resources.CreateLogBackupResources(ctx, cfg, networkResources, ecrStack)
		if err != nil {
			return err
		}

		// 3. Create Aurora test environment
		testEnvResources, err := resources.CreateTestEnvironmentResources(ctx, cfg, networkResources)
		if err != nil {
			return err
		}

		// Export network outputs
		ctx.Export("vpcId", networkResources.Vpc.ID())
		ctx.Export("publicSubnetId", networkResources.PublicSubnet.ID())
		ctx.Export("privateSubnet1Id", networkResources.PrivateSubnet1.ID())
		ctx.Export("privateSubnet2Id", networkResources.PrivateSubnet2.ID())

		// Export Log Backup resources
		ctx.Export("logBackupBucketName", logBackupResources.LogBucket.ID())
		ctx.Export("logBackupDynamoTableName", logBackupResources.DynamoDBTable.Name)
		ctx.Export("logBackupSQSQueueUrl", logBackupResources.SQSQueue.Url)

		// Export Test Environment resources
		ctx.Export("ec2PublicIp", testEnvResources.Ec2Instance.PublicIp)
		ctx.Export("auroraEndpoint", testEnvResources.AuroraCluster.Endpoint)
		ctx.Export("auroraReadEndpoint", testEnvResources.AuroraCluster.ReaderEndpoint)
		ctx.Export("auroraPrimaryId", testEnvResources.AuroraPrimary.ID())
		ctx.Export("auroraReplicaId", testEnvResources.AuroraReplica.ID())
		ctx.Export("auditLogBucketName", testEnvResources.AuditLogBucket.ID())

		return nil
	})
}
