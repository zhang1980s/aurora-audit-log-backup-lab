package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"aurora-cluster-stack/config"
	"aurora-cluster-stack/resources"
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

		// Create Aurora resources
		auroraResources, err := resources.CreateAuroraResources(ctx, cfg, networkStack)
		if err != nil {
			return err
		}

		// Export Aurora outputs
		ctx.Export("auroraSecurityGroupId", auroraResources.AuroraSecurityGroup.ID())
		ctx.Export("auroraEndpoint", auroraResources.AuroraCluster.Endpoint)
		ctx.Export("auroraReadEndpoint", auroraResources.AuroraCluster.ReaderEndpoint)
		ctx.Export("auroraPrimaryId", auroraResources.AuroraPrimary.ID())
		ctx.Export("auroraReplicaId", auroraResources.AuroraReplica.ID())

		return nil
	})
}