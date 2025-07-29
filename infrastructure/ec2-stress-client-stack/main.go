package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"ec2-stress-client-stack/config"
	"ec2-stress-client-stack/resources"
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

		// Get Aurora stack reference
		auroraStack, err := pulumi.NewStackReference(ctx, "zhang1980s/aurora-cluster/dev", nil)
		if err != nil {
			return err
		}

		// Get backup solution stack reference
		backupStack, err := pulumi.NewStackReference(ctx, "zhang1980s/backup-solution/dev", nil)
		if err != nil {
			return err
		}

		// Create EC2 resources
		ec2Resources, err := resources.CreateEC2Resources(ctx, cfg, networkStack, auroraStack, backupStack)
		if err != nil {
			return err
		}

		// Export EC2 outputs
		ctx.Export("ec2SecurityGroupId", ec2Resources.EC2SecurityGroup.ID())
		ctx.Export("ec2PublicIp", ec2Resources.EC2Instance.PublicIp)
		ctx.Export("ec2PrivateIp", ec2Resources.EC2Instance.PrivateIp)

		return nil
	})
}
