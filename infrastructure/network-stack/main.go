package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"aurora-network-stack/config"
	"aurora-network-stack/resources"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Load configuration
		cfg, err := config.LoadConfig(ctx)
		if err != nil {
			return err
		}

		// Create network resources
		networkResources, err := resources.CreateNetworkResources(ctx, cfg)
		if err != nil {
			return err
		}

		// Export network outputs
		ctx.Export("vpcId", networkResources.Vpc.ID())
		ctx.Export("publicSubnetId", networkResources.PublicSubnet.ID())
		ctx.Export("privateSubnet1Id", networkResources.PrivateSubnet1.ID())
		ctx.Export("privateSubnet2Id", networkResources.PrivateSubnet2.ID())
		ctx.Export("s3VpcEndpointId", networkResources.S3VpcEndpoint.ID())
		ctx.Export("dynamodbVpcEndpointId", networkResources.DynamoDBVpcEndpoint.ID())
		ctx.Export("rdsVpcEndpointId", networkResources.RDSVpcEndpoint.ID())
		ctx.Export("sqsVpcEndpointId", networkResources.SQSVpcEndpoint.ID())
		ctx.Export("publicRouteTableId", networkResources.PublicRouteTable.ID())
		ctx.Export("privateRouteTableId", networkResources.PrivateRouteTable.ID())

		return nil
	})
}