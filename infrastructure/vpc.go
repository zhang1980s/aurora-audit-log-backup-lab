package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// VpcResources holds all the networking resources
type VpcResources struct {
	vpc                 *ec2.Vpc
	publicSubnet        *ec2.Subnet
	privateSubnet1      *ec2.Subnet
	privateSubnet2      *ec2.Subnet
	internetGateway     *ec2.InternetGateway
	s3VpcEndpoint       *ec2.VpcEndpoint
	publicRouteTable    *ec2.RouteTable
	privateRouteTable   *ec2.RouteTable
	ec2SecurityGroup    *ec2.SecurityGroup
	auroraSecurityGroup *ec2.SecurityGroup
}

// createVpcResources creates all VPC and networking components
func createVpcResources(ctx *pulumi.Context) (*VpcResources, error) {
	// Create VPC
	vpc, err := ec2.NewVpc(ctx, "aurora-vpc", &ec2.VpcArgs{
		CidrBlock: pulumi.String("10.0.0.0/16"),
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
		AvailabilityZone: pulumi.String("ap-southeast-1a"), // Singapore region AZ
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
		AvailabilityZone: pulumi.String("ap-southeast-1a"), // Same AZ as public subnet
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
		AvailabilityZone: pulumi.String("ap-southeast-1b"), // Different AZ
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

	// Create S3 VPC Endpoint
	s3VpcEndpoint, err := ec2.NewVpcEndpoint(ctx, "s3-vpc-endpoint", &ec2.VpcEndpointArgs{
		VpcId:           vpc.ID(),
		ServiceName:     pulumi.String("com.amazonaws.ap-southeast-1.s3"),
		VpcEndpointType: pulumi.String("Gateway"),
		RouteTableIds:   pulumi.StringArray{}, // We'll associate it with route tables later
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-s3-vpc-endpoint"),
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

	// Associate S3 VPC Endpoint with route tables
	_, err = ec2.NewVpcEndpointRouteTableAssociation(ctx, "s3-endpoint-public-rt", &ec2.VpcEndpointRouteTableAssociationArgs{
		RouteTableId:  publicRouteTable.ID(),
		VpcEndpointId: s3VpcEndpoint.ID(),
	})
	if err != nil {
		return nil, err
	}

	_, err = ec2.NewVpcEndpointRouteTableAssociation(ctx, "s3-endpoint-private-rt", &ec2.VpcEndpointRouteTableAssociationArgs{
		RouteTableId:  privateRouteTable.ID(),
		VpcEndpointId: s3VpcEndpoint.ID(),
	})
	if err != nil {
		return nil, err
	}

	// Create EC2 security group
	ec2SecurityGroup, err := ec2.NewSecurityGroup(ctx, "ec2-sg", &ec2.SecurityGroupArgs{
		VpcId:       vpc.ID(),
		Description: pulumi.String("Security group for EC2 instance"),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Protocol:    pulumi.String("tcp"),
				FromPort:    pulumi.Int(22),
				ToPort:      pulumi.Int(22),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				Description: pulumi.String("Allow SSH from anywhere"),
			},
		},
		Egress: ec2.SecurityGroupEgressArray{
			&ec2.SecurityGroupEgressArgs{
				Protocol:    pulumi.String("-1"),
				FromPort:    pulumi.Int(0),
				ToPort:      pulumi.Int(0),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				Description: pulumi.String("Allow all outbound traffic"),
			},
		},
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-ec2-sg"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create Aurora security group
	auroraSecurityGroup, err := ec2.NewSecurityGroup(ctx, "aurora-sg", &ec2.SecurityGroupArgs{
		VpcId:       vpc.ID(),
		Description: pulumi.String("Security group for Aurora MySQL cluster"),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Protocol:       pulumi.String("tcp"),
				FromPort:       pulumi.Int(3306),
				ToPort:         pulumi.Int(3306),
				SecurityGroups: pulumi.StringArray{ec2SecurityGroup.ID()},
				Description:    pulumi.String("Allow MySQL from EC2 instance"),
			},
		},
		Egress: ec2.SecurityGroupEgressArray{
			&ec2.SecurityGroupEgressArgs{
				Protocol:    pulumi.String("-1"),
				FromPort:    pulumi.Int(0),
				ToPort:      pulumi.Int(0),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				Description: pulumi.String("Allow all outbound traffic"),
			},
		},
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-db-sg"),
		},
	})
	if err != nil {
		return nil, err
	}

	return &VpcResources{
		vpc:                 vpc,
		publicSubnet:        publicSubnet,
		privateSubnet1:      privateSubnet1,
		privateSubnet2:      privateSubnet2,
		internetGateway:     igw,
		s3VpcEndpoint:       s3VpcEndpoint,
		publicRouteTable:    publicRouteTable,
		privateRouteTable:   privateRouteTable,
		ec2SecurityGroup:    ec2SecurityGroup,
		auroraSecurityGroup: auroraSecurityGroup,
	}, nil
}
