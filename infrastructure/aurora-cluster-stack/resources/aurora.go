package resources

import (
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/rds"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ssm"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"aurora-cluster-stack/config"
	"aurora-cluster-stack/utils"
)

// AuroraResources holds all the resources for the Aurora cluster
type AuroraResources struct {
	AuroraSecurityGroup *ec2.SecurityGroup
	AuroraRole          *iam.Role
	AuditLogBucket      *s3.Bucket
	AuroraCluster       *rds.Cluster
	AuroraPrimary       *rds.ClusterInstance
	AuroraReplica       *rds.ClusterInstance
}

// CreateAuroraResources creates the Aurora cluster and related resources
func CreateAuroraResources(ctx *pulumi.Context, cfg *config.Config, networkStack *pulumi.StackReference) (*AuroraResources, error) {
	// Get network resources from network stack
	vpcId := networkStack.GetOutput(pulumi.String("vpcId"))
	privateSubnet1Id := networkStack.GetOutput(pulumi.String("privateSubnet1Id"))
	privateSubnet2Id := networkStack.GetOutput(pulumi.String("privateSubnet2Id"))

	// Create Aurora security group
	auroraSecurityGroup, err := ec2.NewSecurityGroup(ctx, "aurora-sg", &ec2.SecurityGroupArgs{
		VpcId:       vpcId.ApplyT(func(v interface{}) string { return v.(string) }).(pulumi.StringOutput),
		Description: pulumi.String("Security group for Aurora MySQL cluster"),
		Egress: ec2.SecurityGroupEgressArray{
			&ec2.SecurityGroupEgressArgs{
				Protocol:    pulumi.String("-1"),
				FromPort:    pulumi.Int(0),
				ToPort:      pulumi.Int(0),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				Description: pulumi.String("Allow all outbound traffic"),
			},
		},
		Tags: CreateResourceTags(ctx, cfg.Tags, "aurora-db-sg"),
	})
	if err != nil {
		return nil, err
	}

	// Create S3 bucket for audit logs
	auditLogBucket, err := s3.NewBucket(ctx, "audit-logs-bucket", &s3.BucketArgs{
		Acl: pulumi.String("private"),
		Tags: CreateResourceTags(ctx, cfg.Tags, "aurora-audit-logs"),
		// Configure server-side encryption
		ServerSideEncryptionConfiguration: &s3.BucketServerSideEncryptionConfigurationArgs{
			Rule: &s3.BucketServerSideEncryptionConfigurationRuleArgs{
				ApplyServerSideEncryptionByDefault: &s3.BucketServerSideEncryptionConfigurationRuleApplyServerSideEncryptionByDefaultArgs{
					SseAlgorithm: pulumi.String("AES256"),
				},
			},
		},
		// Configure lifecycle rules for log retention
		LifecycleRules: s3.BucketLifecycleRuleArray{
			&s3.BucketLifecycleRuleArgs{
				Id:      pulumi.String("expire-old-logs"),
				Enabled: pulumi.Bool(true),
				Expiration: &s3.BucketLifecycleRuleExpirationArgs{
					Days: pulumi.Int(90), // Keep logs for 90 days
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	// Create bucket policy to allow access from Aurora via VPC Endpoint
	_, err = s3.NewBucketPolicy(ctx, "audit-logs-bucket-policy", &s3.BucketPolicyArgs{
		Bucket: auditLogBucket.ID(),
		Policy: pulumi.All(auditLogBucket.Arn).ApplyT(func(args []interface{}) string {
			bucketArn := args[0].(string)
			return `{
				"Version": "2012-10-17",
				"Statement": [
					{
						"Effect": "Allow",
						"Principal": {
							"Service": "rds.amazonaws.com"
						},
						"Action": [
							"s3:PutObject",
							"s3:GetObject"
						],
						"Resource": "` + bucketArn + `/*"
					}
				]
			}`
		}).(pulumi.StringOutput),
	})
	if err != nil {
		return nil, err
	}

	// Create Aurora role
	auroraRole, err := iam.NewRole(ctx, "aurora-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Action": "sts:AssumeRole",
				"Principal": {
					"Service": "rds.amazonaws.com"
				},
				"Effect": "Allow",
				"Sid": ""
			}]
		}`),
		Tags: CreateResourceTags(ctx, cfg.Tags, "aurora-service-role"),
	})
	if err != nil {
		return nil, err
	}

	// Create policy for S3 access
	s3AccessPolicy, err := iam.NewPolicy(ctx, "s3-access-policy", &iam.PolicyArgs{
		Description: pulumi.String("Policy for S3 access to audit logs"),
		Policy: pulumi.All(auditLogBucket.Arn).ApplyT(func(args []interface{}) string {
			bucketArn := args[0].(string)
			return `{
				"Version": "2012-10-17",
				"Statement": [
					{
						"Action": [
							"s3:GetObject",
							"s3:PutObject",
							"s3:ListBucket"
						],
						"Effect": "Allow",
						"Resource": [
							"` + bucketArn + `",
							"` + bucketArn + `/*"
						]
					}
				]
			}`
		}).(pulumi.StringOutput),
	})
	if err != nil {
		return nil, err
	}

	// Attach S3 access policy to Aurora role
	_, err = iam.NewRolePolicyAttachment(ctx, "aurora-s3-access-policy", &iam.RolePolicyAttachmentArgs{
		Role:      auroraRole.Name,
		PolicyArn: s3AccessPolicy.Arn,
	})
	if err != nil {
		return nil, err
	}

	// Create subnet group for Aurora cluster
	subnetGroup, err := rds.NewSubnetGroup(ctx, "aurora-subnet-group", &rds.SubnetGroupArgs{
		SubnetIds: pulumi.StringArray{
			privateSubnet1Id.ApplyT(func(v interface{}) string { return v.(string) }).(pulumi.StringOutput),
			privateSubnet2Id.ApplyT(func(v interface{}) string { return v.(string) }).(pulumi.StringOutput),
		},
		Tags: CreateResourceTags(ctx, cfg.Tags, "aurora-subnet-group"),
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
		Tags: CreateResourceTags(ctx, cfg.Tags, "aurora-param-group"),
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
		VpcSecurityGroupIds:         pulumi.StringArray{auroraSecurityGroup.ID()},
		MasterUsername:              pulumi.String("admin"),
		MasterPassword:              pulumi.String("Password123!"), // Required by Aurora even with IAM auth
		SkipFinalSnapshot:           pulumi.Bool(true),
		BackupRetentionPeriod:       pulumi.Int(1), // Minimum backup retention period required by AWS
		// CloudWatch logs export disabled, but audit logging still enabled via parameter group
		EnabledCloudwatchLogsExports:     pulumi.StringArray{},
		IamDatabaseAuthenticationEnabled: pulumi.Bool(false), // Disable IAM authentication
		StorageEncrypted:                 pulumi.Bool(true),
		DeletionProtection:               pulumi.Bool(false), // Set to true in production
		Tags:                             CreateResourceTags(ctx, cfg.Tags, "aurora-cluster"),
	})
	if err != nil {
		return nil, err
	}

	// Create primary instance
	primary, err := rds.NewClusterInstance(ctx, "aurora-primary", &rds.ClusterInstanceArgs{
		ClusterIdentifier:          cluster.ID(),
		InstanceClass:              pulumi.String(cfg.Aurora.InstanceType),
		Engine:                     pulumi.String("aurora-mysql"),
		EngineVersion:              pulumi.String("8.0.mysql_aurora.3.04.0"),
		DbSubnetGroupName:          subnetGroup.Name,
		PubliclyAccessible:         pulumi.Bool(false),
		MonitoringInterval:         pulumi.Int(0), // Disable enhanced monitoring as per requirements
		PerformanceInsightsEnabled: pulumi.Bool(false),
		Tags:                       CreateResourceTags(ctx, cfg.Tags, "aurora-primary"),
	})
	if err != nil {
		return nil, err
	}

	// Create replica instance
	replica, err := rds.NewClusterInstance(ctx, "aurora-replica", &rds.ClusterInstanceArgs{
		ClusterIdentifier:          cluster.ID(),
		InstanceClass:              pulumi.String(cfg.Aurora.InstanceType),
		Engine:                     pulumi.String("aurora-mysql"),
		EngineVersion:              pulumi.String("8.0.mysql_aurora.3.04.0"),
		DbSubnetGroupName:          subnetGroup.Name,
		PubliclyAccessible:         pulumi.Bool(false),
		MonitoringInterval:         pulumi.Int(0), // Disable enhanced monitoring as per requirements
		PerformanceInsightsEnabled: pulumi.Bool(false),
		Tags:                       CreateResourceTags(ctx, cfg.Tags, "aurora-replica"),
	})
	if err != nil {
		return nil, err
	}

	// Store Aurora endpoint in SSM Parameter Store
	_, err = ssm.NewParameter(ctx, "aurora-endpoint-param", &ssm.ParameterArgs{
		Name:  pulumi.String("/aurora-audit-log-lab/aurora-endpoint"),
		Type:  pulumi.String("String"),
		Value: cluster.Endpoint,
		Tags:  CreateResourceTags(ctx, cfg.Tags, "aurora-endpoint"),
	})
	if err != nil {
		return nil, err
	}

	// Store S3 bucket name in SSM Parameter Store
	_, err = ssm.NewParameter(ctx, "s3-bucket-param", &ssm.ParameterArgs{
		Name:  pulumi.String("/aurora-audit-log-lab/s3-bucket-name"),
		Type:  pulumi.String("String"),
		Value: auditLogBucket.ID(),
		Tags:  CreateResourceTags(ctx, cfg.Tags, "s3-bucket-name"),
	})
	if err != nil {
		return nil, err
	}

	return &AuroraResources{
		AuroraSecurityGroup: auroraSecurityGroup,
		AuroraRole:          auroraRole,
		AuditLogBucket:      auditLogBucket,
		AuroraCluster:       cluster,
		AuroraPrimary:       primary,
		AuroraReplica:       replica,
	}, nil
}

// CreateResourceTags creates a pulumi.StringMap from the tags configuration
func CreateResourceTags(ctx *pulumi.Context, cfg utils.TagsConfig, resourceName string) pulumi.StringMap {
	tags := pulumi.StringMap{
		"Name":        pulumi.String(resourceName),
		"Environment": pulumi.String(cfg.Environment),
		"Project":     pulumi.String(cfg.Project),
		"Owner":       pulumi.String(cfg.Owner),
	}

	// Add custom tags
	for k, v := range cfg.CustomTags {
		tags[k] = pulumi.String(v)
	}

	return tags
}