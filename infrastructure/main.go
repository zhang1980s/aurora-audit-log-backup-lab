package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// 1. Create fundamental network environment
		networkResources, err := createNetworkResources(ctx)
		if err != nil {
			return err
		}

		// 2. Create log backup resources
		logBackupResources, err := createLogBackupResources(ctx, networkResources)
		if err != nil {
			return err
		}

		// 3. Create Aurora test environment
		testEnvResources, err := createTestEnvironmentResources(ctx, networkResources)
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
		ctx.Export("auditLogBucketName", testEnvResources.AuditLogBucket.ID())

		return nil
	})
}
