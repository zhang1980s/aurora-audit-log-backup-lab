package main

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// NetworkResources holds all the networking resources
type NetworkResources struct {
	Vpc                 *ec2.Vpc
	PublicSubnet        *ec2.Subnet
	PrivateSubnet1      *ec2.Subnet
	PrivateSubnet2      *ec2.Subnet
	InternetGateway     *ec2.InternetGateway
	S3VpcEndpoint       *ec2.VpcEndpoint
	DynamoDBVpcEndpoint *ec2.VpcEndpoint
	RDSVpcEndpoint      *ec2.VpcEndpoint
	PublicRouteTable    *ec2.RouteTable
	PrivateRouteTable   *ec2.RouteTable
}

// createNetworkResources creates all VPC and networking components
func createNetworkResources(ctx *pulumi.Context) (*NetworkResources, error) {
	// Get configuration values
	awsCfg := config.New(ctx, "aws")
	region := awsCfg.Require("region")

	projectCfg := config.New(ctx, "aurora-audit-log-backup-lab")
	az1 := projectCfg.Require("availabilityZone1")
	az2 := projectCfg.Require("availabilityZone2")
	// Create VPC
	vpc, err := ec2.NewVpc(ctx, "aurora-vpc", &ec2.VpcArgs{
		CidrBlock:          pulumi.String("10.0.0.0/16"),
		EnableDnsSupport:   pulumi.Bool(true),
		EnableDnsHostnames: pulumi.Bool(true),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-vpc"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create public subnet in AZ-a
	publicSubnet, err := ec2.NewSubnet(ctx, "public-subnet", &ec2.SubnetArgs{
		VpcId:            vpc.ID(),
		CidrBlock:        pulumi.String("10.0.0.0/24"),
		AvailabilityZone: pulumi.String(az1),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-public-subnet"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create private subnet 1 in AZ-a
	privateSubnet1, err := ec2.NewSubnet(ctx, "private-subnet-1", &ec2.SubnetArgs{
		VpcId:            vpc.ID(),
		CidrBlock:        pulumi.String("10.0.1.0/24"),
		AvailabilityZone: pulumi.String(az1), // Same AZ as public subnet
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-private-subnet-1"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create private subnet 2 in AZ-b
	privateSubnet2, err := ec2.NewSubnet(ctx, "private-subnet-2", &ec2.SubnetArgs{
		VpcId:            vpc.ID(),
		CidrBlock:        pulumi.String("10.0.2.0/24"),
		AvailabilityZone: pulumi.String(az2), // Different AZ
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-private-subnet-2"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create Internet Gateway
	igw, err := ec2.NewInternetGateway(ctx, "aurora-igw", &ec2.InternetGatewayArgs{
		VpcId: vpc.ID(),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-igw"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create S3 VPC Endpoint for private subnets only
	s3VpcEndpoint, err := ec2.NewVpcEndpoint(ctx, "s3-vpc-endpoint", &ec2.VpcEndpointArgs{
		VpcId:           vpc.ID(),
		ServiceName:     pulumi.String(fmt.Sprintf("com.amazonaws.%s.s3", region)),
		VpcEndpointType: pulumi.String("Gateway"),
		RouteTableIds:   pulumi.StringArray{}, // We'll associate it with private route table later
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-s3-vpc-endpoint"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create DynamoDB VPC Endpoint for private subnets
	dynamoDBVpcEndpoint, err := ec2.NewVpcEndpoint(ctx, "dynamodb-vpc-endpoint", &ec2.VpcEndpointArgs{
		VpcId:           vpc.ID(),
		ServiceName:     pulumi.String(fmt.Sprintf("com.amazonaws.%s.dynamodb", region)),
		VpcEndpointType: pulumi.String("Gateway"),
		RouteTableIds:   pulumi.StringArray{}, // We'll associate it with private route table later
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-dynamodb-vpc-endpoint"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create security group for VPC endpoints
	vpcEndpointSG, err := ec2.NewSecurityGroup(ctx, "vpc-endpoint-sg", &ec2.SecurityGroupArgs{
		VpcId:       vpc.ID(),
		Description: pulumi.String("Security group for VPC endpoints"),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Protocol:    pulumi.String("tcp"),
				FromPort:    pulumi.Int(443),
				ToPort:      pulumi.Int(443),
				CidrBlocks:  pulumi.StringArray{pulumi.String("10.0.0.0/16")}, // Allow HTTPS from within the VPC
				Description: pulumi.String("Allow HTTPS from VPC"),
			},
		},
		Tags: pulumi.StringMap{
			"Name": pulumi.String("vpc-endpoint-sg"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create RDS API VPC Endpoint
	rdsVpcEndpoint, err := ec2.NewVpcEndpoint(ctx, "rds-vpc-endpoint", &ec2.VpcEndpointArgs{
		VpcId:             vpc.ID(),
		ServiceName:       pulumi.String(fmt.Sprintf("com.amazonaws.%s.rds", region)),
		VpcEndpointType:   pulumi.String("Interface"),
		SubnetIds:         pulumi.StringArray{privateSubnet1.ID(), privateSubnet2.ID()},
		SecurityGroupIds:  pulumi.StringArray{vpcEndpointSG.ID()},
		PrivateDnsEnabled: pulumi.Bool(true),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-rds-vpc-endpoint"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create public route table
	publicRouteTable, err := ec2.NewRouteTable(ctx, "public-rt", &ec2.RouteTableArgs{
		VpcId: vpc.ID(),
		Routes: ec2.RouteTableRouteArray{
			&ec2.RouteTableRouteArgs{
				CidrBlock: pulumi.String("0.0.0.0/0"),
				GatewayId: igw.ID(),
			},
		},
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-public-rt"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create private route table (without NAT Gateway route)
	privateRouteTable, err := ec2.NewRouteTable(ctx, "private-rt", &ec2.RouteTableArgs{
		VpcId: vpc.ID(),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-private-rt"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Associate public subnet with public route table
	_, err = ec2.NewRouteTableAssociation(ctx, "public-rt-assoc", &ec2.RouteTableAssociationArgs{
		SubnetId:     publicSubnet.ID(),
		RouteTableId: publicRouteTable.ID(),
	})
	if err != nil {
		return nil, err
	}

	// Associate private subnet 1 with private route table
	_, err = ec2.NewRouteTableAssociation(ctx, "private-rt-assoc-1", &ec2.RouteTableAssociationArgs{
		SubnetId:     privateSubnet1.ID(),
		RouteTableId: privateRouteTable.ID(),
	})
	if err != nil {
		return nil, err
	}

	// Associate private subnet 2 with private route table
	_, err = ec2.NewRouteTableAssociation(ctx, "private-rt-assoc-2", &ec2.RouteTableAssociationArgs{
		SubnetId:     privateSubnet2.ID(),
		RouteTableId: privateRouteTable.ID(),
	})
	if err != nil {
		return nil, err
	}

	// Associate S3 VPC Endpoint with private route table only
	_, err = ec2.NewVpcEndpointRouteTableAssociation(ctx, "s3-endpoint-private-rt", &ec2.VpcEndpointRouteTableAssociationArgs{
		RouteTableId:  privateRouteTable.ID(),
		VpcEndpointId: s3VpcEndpoint.ID(),
	})
	if err != nil {
		return nil, err
	}

	// Associate DynamoDB VPC Endpoint with private route table only
	_, err = ec2.NewVpcEndpointRouteTableAssociation(ctx, "dynamodb-endpoint-private-rt", &ec2.VpcEndpointRouteTableAssociationArgs{
		RouteTableId:  privateRouteTable.ID(),
		VpcEndpointId: dynamoDBVpcEndpoint.ID(),
	})
	if err != nil {
		return nil, err
	}

	return &NetworkResources{
		Vpc:                 vpc,
		PublicSubnet:        publicSubnet,
		PrivateSubnet1:      privateSubnet1,
		PrivateSubnet2:      privateSubnet2,
		InternetGateway:     igw,
		S3VpcEndpoint:       s3VpcEndpoint,
		DynamoDBVpcEndpoint: dynamoDBVpcEndpoint,
		RDSVpcEndpoint:      rdsVpcEndpoint,
		PublicRouteTable:    publicRouteTable,
		PrivateRouteTable:   privateRouteTable,
	}, nil
}
