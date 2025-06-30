package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/rds"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/ssm"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// createAuroraCluster creates the Aurora MySQL cluster in the private subnets
func createAuroraCluster(ctx *pulumi.Context, vpcResources *VpcResources, iamResources *IamResources, s3Resources *S3Resources) (*rds.Cluster, error) {
	// Create subnet group for Aurora cluster
	subnetGroup, err := rds.NewSubnetGroup(ctx, "aurora-subnet-group", &rds.SubnetGroupArgs{
		SubnetIds: pulumi.StringArray{
			vpcResources.privateSubnet1.ID(),
			vpcResources.privateSubnet2.ID(),
		},
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-subnet-group"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create parameter group for Aurora cluster
	parameterGroup, err := rds.NewClusterParameterGroup(ctx, "aurora-param-group", &rds.ClusterParameterGroupArgs{
		Family: pulumi.String("aurora-mysql8.0"),
		Parameters: rds.ClusterParameterGroupParameterArray{
			&rds.ClusterParameterGroupParameterArgs{
				Name:  pulumi.String("server_audit_events"),
				Value: pulumi.String("CONNECT,QUERY,TABLE,QUERY_DDL,QUERY_DML,QUERY_DCL"),
			},
			&rds.ClusterParameterGroupParameterArgs{
				Name:  pulumi.String("server_audit_logging"),
				Value: pulumi.String("1"),
			},
		},
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-param-group"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create Aurora cluster
	cluster, err := rds.NewCluster(ctx, "aurora-cluster", &rds.ClusterArgs{
		Engine:                      pulumi.String("aurora-mysql"),
		EngineVersion:               pulumi.String("8.0.mysql_aurora.3.04.0"),
		DbSubnetGroupName:           subnetGroup.Name,
		DbClusterParameterGroupName: parameterGroup.Name,
		VpcSecurityGroupIds:         pulumi.StringArray{vpcResources.auroraSecurityGroup.ID()},
		MasterUsername:              pulumi.String("admin"),
		MasterPassword:              pulumi.String("Password123!"), // Required by Aurora even with IAM auth
		SkipFinalSnapshot:           pulumi.Bool(true),
		BackupRetentionPeriod:       pulumi.Int(0), // Disable backups as per requirements
		// CloudWatch logs export disabled, but audit logging still enabled via parameter group
		EnabledCloudwatchLogsExports:     pulumi.StringArray{},
		IamDatabaseAuthenticationEnabled: pulumi.Bool(false), // Disable IAM authentication
		StorageEncrypted:                 pulumi.Bool(true),
		DeletionProtection:               pulumi.Bool(false), // Set to true in production
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-cluster"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create primary instance
	_, err = rds.NewClusterInstance(ctx, "aurora-primary", &rds.ClusterInstanceArgs{
		ClusterIdentifier:          cluster.ID(),
		InstanceClass:              pulumi.String("db.t4g.medium"),
		Engine:                     pulumi.String("aurora-mysql"),
		EngineVersion:              pulumi.String("8.0.mysql_aurora.3.04.0"),
		DbSubnetGroupName:          subnetGroup.Name,
		PubliclyAccessible:         pulumi.Bool(false),
		MonitoringInterval:         pulumi.Int(0), // Disable enhanced monitoring as per requirements
		PerformanceInsightsEnabled: pulumi.Bool(false),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-primary"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create replica instance
	_, err = rds.NewClusterInstance(ctx, "aurora-replica", &rds.ClusterInstanceArgs{
		ClusterIdentifier:          cluster.ID(),
		InstanceClass:              pulumi.String("db.t4g.medium"),
		Engine:                     pulumi.String("aurora-mysql"),
		EngineVersion:              pulumi.String("8.0.mysql_aurora.3.04.0"),
		DbSubnetGroupName:          subnetGroup.Name,
		PubliclyAccessible:         pulumi.Bool(false),
		MonitoringInterval:         pulumi.Int(0), // Disable enhanced monitoring as per requirements
		PerformanceInsightsEnabled: pulumi.Bool(false),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-replica"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Export Aurora cluster endpoint
	ctx.Export("auroraEndpoint", cluster.Endpoint)
	ctx.Export("auroraReadEndpoint", cluster.ReaderEndpoint)

	// Store Aurora endpoint in SSM Parameter Store
	_, err = ssm.NewParameter(ctx, "aurora-endpoint-param", &ssm.ParameterArgs{
		Name:  pulumi.String("/aurora-audit-log-lab/aurora-endpoint"),
		Type:  pulumi.String("String"),
		Value: cluster.Endpoint,
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-endpoint"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Store S3 bucket name in SSM Parameter Store
	_, err = ssm.NewParameter(ctx, "s3-bucket-param", &ssm.ParameterArgs{
		Name:  pulumi.String("/aurora-audit-log-lab/s3-bucket-name"),
		Type:  pulumi.String("String"),
		Value: pulumi.String("zzhe-aurora-audit-log-lab-bucket"),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("s3-bucket-name"),
		},
	})
	if err != nil {
		return nil, err
	}

	return cluster, nil
}
