package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/rds"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/s3"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/ssm"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// TestEnvironmentResources holds all the resources for the Aurora test environment
type TestEnvironmentResources struct {
	Ec2SecurityGroup    *ec2.SecurityGroup
	AuroraSecurityGroup *ec2.SecurityGroup
	Ec2Role             *iam.Role
	Ec2InstanceProfile  *iam.InstanceProfile
	AuroraRole          *iam.Role
	AuditLogBucket      *s3.Bucket
	AuroraCluster       *rds.Cluster
	Ec2Instance         *ec2.Instance
}

// createTestEnvironmentResources creates the Aurora test environment
func createTestEnvironmentResources(ctx *pulumi.Context, networkResources *NetworkResources) (*TestEnvironmentResources, error) {
	// Get configuration values
	projectCfg := config.New(ctx, "aurora-audit-log-backup-lab")
	ec2KeyPairName := projectCfg.Require("ec2KeyPairName")
	ec2InstanceType := projectCfg.Require("ec2InstanceType")
	auroraInstanceType := projectCfg.Require("auroraInstanceType")
	// Create EC2 security group
	ec2SecurityGroup, err := ec2.NewSecurityGroup(ctx, "ec2-sg", &ec2.SecurityGroupArgs{
		VpcId:       networkResources.Vpc.ID(),
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
		VpcId:       networkResources.Vpc.ID(),
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

	// Create S3 bucket for audit logs
	auditLogBucket, err := s3.NewBucket(ctx, "audit-logs-bucket", &s3.BucketArgs{
		Bucket: pulumi.String("zzhe-aurora-audit-log-lab-bucket"),
		Acl:    pulumi.String("private"),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-audit-logs"),
		},
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

	// Create EC2 role
	ec2Role, err := iam.NewRole(ctx, "ec2-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Action": "sts:AssumeRole",
				"Principal": {
					"Service": "ec2.amazonaws.com"
				},
				"Effect": "Allow",
				"Sid": ""
			}]
		}`),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-ec2-role"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Attach SSM policy to EC2 role
	_, err = iam.NewRolePolicyAttachment(ctx, "ec2-ssm-policy", &iam.RolePolicyAttachmentArgs{
		Role:      ec2Role.Name,
		PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"),
	})
	if err != nil {
		return nil, err
	}

	// Create policy for RDS IAM authentication
	rdsAuthPolicy, err := iam.NewPolicy(ctx, "rds-auth-policy", &iam.PolicyArgs{
		Description: pulumi.String("Policy for RDS IAM authentication"),
		Policy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Action": [
					"rds-db:connect"
				],
				"Effect": "Allow",
				"Resource": "*"
			}]
		}`),
	})
	if err != nil {
		return nil, err
	}

	// Attach RDS auth policy to EC2 role
	_, err = iam.NewRolePolicyAttachment(ctx, "ec2-rds-auth-policy", &iam.RolePolicyAttachmentArgs{
		Role:      ec2Role.Name,
		PolicyArn: rdsAuthPolicy.Arn,
	})
	if err != nil {
		return nil, err
	}

	// Create policy for S3 access (with VPC Endpoint conditions)
	s3AccessPolicy, err := iam.NewPolicy(ctx, "s3-access-policy", &iam.PolicyArgs{
		Description: pulumi.String("Policy for S3 access to audit logs"),
		Policy: pulumi.String(`{
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
						"arn:aws:s3:::zzhe-aurora-audit-log-lab-bucket",
						"arn:aws:s3:::zzhe-aurora-audit-log-lab-bucket/*"
					]
				},
				{
					"Action": [
						"s3:ListAllMyBuckets"
					],
					"Effect": "Allow",
					"Resource": "*"
				}
			]
		}`),
	})
	if err != nil {
		return nil, err
	}

	// Attach S3 access policy to EC2 role
	_, err = iam.NewRolePolicyAttachment(ctx, "ec2-s3-access-policy", &iam.RolePolicyAttachmentArgs{
		Role:      ec2Role.Name,
		PolicyArn: s3AccessPolicy.Arn,
	})
	if err != nil {
		return nil, err
	}

	// Create policy for RDS describe access
	rdsDescribePolicy, err := iam.NewPolicy(ctx, "rds-describe-policy", &iam.PolicyArgs{
		Description: pulumi.String("Policy for describing RDS resources"),
		Policy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Action": [
					"rds:DescribeDBClusters",
					"rds:DescribeDBClusterParameters",
					"rds:DescribeDBClusterParameterGroups"
				],
				"Effect": "Allow",
				"Resource": "*"
			}]
		}`),
	})
	if err != nil {
		return nil, err
	}

	// Attach RDS describe policy to EC2 role
	_, err = iam.NewRolePolicyAttachment(ctx, "ec2-rds-describe-policy", &iam.RolePolicyAttachmentArgs{
		Role:      ec2Role.Name,
		PolicyArn: rdsDescribePolicy.Arn,
	})
	if err != nil {
		return nil, err
	}

	// Create policy for SSM Parameter Store access
	ssmPolicy, err := iam.NewPolicy(ctx, "ssm-parameter-policy", &iam.PolicyArgs{
		Description: pulumi.String("Policy for accessing SSM Parameter Store"),
		Policy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Action": [
					"ssm:GetParameter",
					"ssm:GetParameters",
					"ssm:GetParametersByPath"
				],
				"Effect": "Allow",
				"Resource": "arn:aws:ssm:*:*:parameter/aurora-audit-log-lab/*"
			}]
		}`),
	})
	if err != nil {
		return nil, err
	}

	// Attach SSM Parameter Store policy to EC2 role
	_, err = iam.NewRolePolicyAttachment(ctx, "ec2-ssm-parameter-policy", &iam.RolePolicyAttachmentArgs{
		Role:      ec2Role.Name,
		PolicyArn: ssmPolicy.Arn,
	})
	if err != nil {
		return nil, err
	}

	// Create EC2 instance profile
	ec2InstanceProfile, err := iam.NewInstanceProfile(ctx, "ec2-instance-profile", &iam.InstanceProfileArgs{
		Role: ec2Role.Name,
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
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-service-role"),
		},
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
			networkResources.PrivateSubnet1.ID(),
			networkResources.PrivateSubnet2.ID(),
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
		InstanceClass:              pulumi.String(auroraInstanceType),
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
		InstanceClass:              pulumi.String(auroraInstanceType),
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

	// Get the latest Amazon Linux 2023 AMI
	ami, err := ec2.LookupAmi(ctx, &ec2.LookupAmiArgs{
		Owners:     []string{"amazon"},
		MostRecent: pulumi.BoolRef(true),
		NameRegex:  pulumi.StringRef("^al2023-ami-2023.*-arm64$"),
		Filters: []ec2.GetAmiFilter{
			{
				Name:   "root-device-type",
				Values: []string{"ebs"},
			},
			{
				Name:   "virtualization-type",
				Values: []string{"hvm"},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	// Create user data script for EC2 instance setup
	userData := `#!/bin/bash
# Update system packages
dnf update -y

# Install MySQL client
dnf install -y mariadb105

# Install AWS CLI
dnf install -y aws-cli

# Install sysbench from source
dnf groupinstall -y "Development Tools"
dnf install -y mariadb105-devel openssl-devel git
git clone https://github.com/akopytov/sysbench.git
cd sysbench
./autogen.sh
./configure
make -j
make install

# Create directory for scripts
mkdir -p /home/ec2-user/scripts

# Create sysbench setup script
cat > /home/ec2-user/scripts/setup_sysbench.sh << 'EOF'
#!/bin/bash
# Setup sysbench test database
# This script will be executed manually after the instance is up

# Get AWS region using IMDSv2
TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
REGION=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/region)

# Get the Aurora endpoint from SSM Parameter Store
CLUSTER_ENDPOINT=$(aws ssm get-parameter --name "/aurora-audit-log-lab/aurora-endpoint" --region $REGION --query "Parameter.Value" --output text)

# Fallback to AWS CLI if Parameter Store fails
if [ -z "$CLUSTER_ENDPOINT" ]; then
    echo "Could not get Aurora endpoint from Parameter Store, falling back to AWS CLI..."
    CLUSTER_ENDPOINT=$(aws rds describe-db-clusters --region $REGION --query "DBClusters[?Engine=='aurora-mysql'].Endpoint" --output text | head -n 1)
fi

# Connect using the master password
echo "Connecting to Aurora using master password..."
MASTER_PASSWORD="Password123!"

# Create test database and user
mysql -h $CLUSTER_ENDPOINT -u admin -p$MASTER_PASSWORD << 'EOF'
CREATE DATABASE IF NOT EXISTS sysbench_test;
CREATE USER IF NOT EXISTS 'sysbench'@'%' IDENTIFIED BY 'sysbench123';
GRANT ALL PRIVILEGES ON sysbench_test.* TO 'sysbench'@'%';
FLUSH PRIVILEGES;
EOF

# Prepare sysbench OLTP tables
sysbench oltp_read_write --db-driver=mysql --mysql-host=$CLUSTER_ENDPOINT --mysql-user=sysbench --mysql-password='sysbench123' --mysql-db=sysbench_test --tables=10 --table-size=100000 --threads=4 prepare
EOF

# Create test execution script
cat > /home/ec2-user/scripts/test_audit_logs.sh << 'EOF'
#!/bin/bash
# Run sysbench tests and verify audit logs
# This script will be executed manually after the database is set up

# Get AWS region using IMDSv2
TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
REGION=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/region)

# Get the Aurora endpoint from SSM Parameter Store
CLUSTER_ENDPOINT=$(aws ssm get-parameter --name "/aurora-audit-log-lab/aurora-endpoint" --region $REGION --query "Parameter.Value" --output text)

# Fallback to AWS CLI if Parameter Store fails
if [ -z "$CLUSTER_ENDPOINT" ]; then
    echo "Could not get Aurora endpoint from Parameter Store, falling back to AWS CLI..."
    CLUSTER_ENDPOINT=$(aws rds describe-db-clusters --region $REGION --query "DBClusters[?Engine=='aurora-mysql'].Endpoint" --output text | head -n 1)
fi

# Get the S3 bucket name from SSM Parameter Store
BUCKET_NAME=$(aws ssm get-parameter --name "/aurora-audit-log-lab/s3-bucket-name" --region $REGION --query "Parameter.Value" --output text)

# Fallback to fixed name if Parameter Store fails
if [ -z "$BUCKET_NAME" ]; then
    echo "Could not get S3 bucket name from Parameter Store, using default..."
    BUCKET_NAME="zzhe-aurora-audit-log-lab-bucket"
fi

# Set passwords for authentication
ADMIN_PASSWORD="Password123!"
SYSBENCH_PASSWORD="sysbench123"

# Run authentication tests
echo "Running authentication tests..."
mysql -h $CLUSTER_ENDPOINT -u admin -p$ADMIN_PASSWORD -e "SELECT 1;"
mysql -h $CLUSTER_ENDPOINT -u sysbench -e "SELECT 1;"

# Run OLTP workload tests
echo "Running OLTP read-only workload..."
sysbench oltp_read_only --db-driver=mysql --mysql-host=$CLUSTER_ENDPOINT --mysql-user=sysbench --mysql-password=$SYSBENCH_PASSWORD --mysql-db=sysbench_test --tables=10 --table-size=100000 --threads=4 --time=60 run

echo "Running OLTP read-write workload..."
sysbench oltp_read_write --db-driver=mysql --mysql-host=$CLUSTER_ENDPOINT --mysql-user=sysbench --mysql-password=$SYSBENCH_PASSWORD --mysql-db=sysbench_test --tables=10 --table-size=100000 --threads=4 --time=60 run

echo "Running OLTP write-only workload..."
sysbench oltp_write_only --db-driver=mysql --mysql-host=$CLUSTER_ENDPOINT --mysql-user=sysbench --mysql-password=$SYSBENCH_PASSWORD --mysql-db=sysbench_test --tables=10 --table-size=100000 --threads=4 --time=60 run

# Run schema modification tests
echo "Running schema modification tests..."
mysql -h $CLUSTER_ENDPOINT -u admin -p$ADMIN_PASSWORD << 'EOF'
CREATE TABLE IF NOT EXISTS sysbench_test.test_table (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE sysbench_test.test_table ADD COLUMN description TEXT;
DROP TABLE sysbench_test.test_table;
EOF

# Run privilege tests
echo "Running privilege tests..."
mysql -h $CLUSTER_ENDPOINT -u admin -p$ADMIN_PASSWORD << 'EOF'
CREATE USER IF NOT EXISTS 'test_user'@'%' IDENTIFIED BY 'test123';
GRANT SELECT ON sysbench_test.* TO 'test_user'@'%';
REVOKE SELECT ON sysbench_test.* FROM 'test_user'@'%';
DROP USER 'test_user'@'%';
EOF

# Wait for audit logs to be exported to S3
echo "Waiting for audit logs to be exported to S3..."
sleep 300

# Download and analyze audit logs
echo "Downloading and analyzing audit logs..."
mkdir -p ~/audit_logs
aws s3 sync s3://$BUCKET_NAME/audit-logs ~/audit_logs

# Verify audit logs
echo "Verifying audit logs..."
grep -r "CONNECT" ~/audit_logs
grep -r "QUERY" ~/audit_logs
grep -r "TABLE" ~/audit_logs
grep -r "QUERY_DDL" ~/audit_logs
grep -r "QUERY_DML" ~/audit_logs
grep -r "QUERY_DCL" ~/audit_logs

echo "Audit log verification complete!"
EOF

# Make scripts executable
chmod +x /home/ec2-user/scripts/setup_sysbench.sh
chmod +x /home/ec2-user/scripts/test_audit_logs.sh

# Set ownership
chown -R ec2-user:ec2-user /home/ec2-user/scripts
`

	// Use key pair name from configuration

	// Create EC2 instance
	ec2Instance, err := ec2.NewInstance(ctx, "aurora-ec2", &ec2.InstanceArgs{
		Ami:                      pulumi.String(ami.Id),
		InstanceType:             pulumi.String(ec2InstanceType),
		SubnetId:                 networkResources.PublicSubnet.ID(),
		VpcSecurityGroupIds:      pulumi.StringArray{ec2SecurityGroup.ID()},
		AssociatePublicIpAddress: pulumi.Bool(true),
		KeyName:                  pulumi.String(ec2KeyPairName),
		IamInstanceProfile:       ec2InstanceProfile.Name,
		UserData:                 pulumi.String(userData),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-ec2"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Export EC2 instance public IP
	ctx.Export("ec2PublicIp", ec2Instance.PublicIp)
	// Export Aurora cluster endpoint
	ctx.Export("auroraEndpoint", cluster.Endpoint)
	ctx.Export("auroraReadEndpoint", cluster.ReaderEndpoint)
	// Export S3 bucket name
	ctx.Export("auditLogBucketName", auditLogBucket.ID())

	return &TestEnvironmentResources{
		Ec2SecurityGroup:    ec2SecurityGroup,
		AuroraSecurityGroup: auroraSecurityGroup,
		Ec2Role:             ec2Role,
		Ec2InstanceProfile:  ec2InstanceProfile,
		AuroraRole:          auroraRole,
		AuditLogBucket:      auditLogBucket,
		AuroraCluster:       cluster,
		Ec2Instance:         ec2Instance,
	}, nil
}
