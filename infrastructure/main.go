package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Create VPC and networking components
		vpcResources, err := createVpcResources(ctx)
		if err != nil {
			return err
		}

		// Create IAM roles and policies with VPC ID
		iamResources, err := createIamResources(ctx, vpcResources.vpc.ID())
		if err != nil {
			return err
		}

		// Create S3 bucket for audit logs
		s3Resources, err := createS3Resources(ctx)
		if err != nil {
			return err
		}

		// Create EC2 instance
		_, err = createEC2Instance(ctx, vpcResources, iamResources)
		if err != nil {
			return err
		}

		// Create Aurora MySQL cluster
		_, err = createAuroraCluster(ctx, vpcResources, iamResources, s3Resources)
		if err != nil {
			return err
		}

		// Create Log Backup infrastructure
		logBackupResources, err := createLogBackupInfrastructure(ctx)
		if err != nil {
			return err
		}

		// Export outputs
		ctx.Export("vpcId", vpcResources.vpc.ID())
		ctx.Export("publicSubnetId", vpcResources.publicSubnet.ID())
		ctx.Export("privateSubnet1Id", vpcResources.privateSubnet1.ID())
		ctx.Export("privateSubnet2Id", vpcResources.privateSubnet2.ID())
		ctx.Export("ec2SecurityGroupId", vpcResources.ec2SecurityGroup.ID())
		ctx.Export("auroraSecurityGroupId", vpcResources.auroraSecurityGroup.ID())
		ctx.Export("s3BucketName", s3Resources.bucket.ID())

		// Export Log Backup resources
		ctx.Export("logBackupBucketName", logBackupResources.LogBucket.ID())
		ctx.Export("logBackupDynamoTableName", logBackupResources.DynamoDBTable.Name)
		ctx.Export("logBackupSQSQueueUrl", logBackupResources.SQSQueue.Url)

		return nil
	})
}
