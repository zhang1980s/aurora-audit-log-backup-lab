package resources

import (
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/rds"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ssm"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"aurora-audit-log-backup-lab/config"
	"aurora-audit-log-backup-lab/utils"
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
	AuroraPrimary       *rds.ClusterInstance
	AuroraReplica       *rds.ClusterInstance
	Ec2Instance         *ec2.Instance
	// Policy attachments - tracking these ensures proper deletion order
	SsmPolicyAttachment          *iam.RolePolicyAttachment
	RdsAuthPolicyAttachment      *iam.RolePolicyAttachment
	S3AccessPolicyAttachment     *iam.RolePolicyAttachment
	RdsDescribePolicyAttachment  *iam.RolePolicyAttachment
	SsmParameterPolicyAttachment *iam.RolePolicyAttachment
	AuroraS3PolicyAttachment     *iam.RolePolicyAttachment
}

// CreateTestEnvironmentResources creates the Aurora test environment
func CreateTestEnvironmentResources(ctx *pulumi.Context, cfg *config.Config, networkResources *NetworkResources) (*TestEnvironmentResources, error) {
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
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "aurora-ec2-sg"),
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
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "aurora-db-sg"),
	})
	if err != nil {
		return nil, err
	}

	// Create S3 bucket for audit logs
	auditLogBucket, err := s3.NewBucket(ctx, "audit-logs-bucket", &s3.BucketArgs{
		Bucket: pulumi.String("zzhe-aurora-audit-log-lab-bucket"),
		Acl:    pulumi.String("private"),
		Tags:   utils.CreateResourceTags(ctx, cfg.Tags, "aurora-audit-logs"),
		ServerSideEncryptionConfiguration: &s3.BucketServerSideEncryptionConfigurationArgs{
			Rule: &s3.BucketServerSideEncryptionConfigurationRuleArgs{
				ApplyServerSideEncryptionByDefault: &s3.BucketServerSideEncryptionConfigurationRuleApplyServerSideEncryptionByDefaultArgs{
					SseAlgorithm: pulumi.String("AES256"),
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
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "aurora-ec2-role"),
	})
	if err != nil {
		return nil, err
	}

	// Attach SSM policy to EC2 role
	ssmPolicyAttachment, err := iam.NewRolePolicyAttachment(ctx, "ec2-ssm-policy", &iam.RolePolicyAttachmentArgs{
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
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "rds-auth-policy"),
	})
	if err != nil {
		return nil, err
	}

	// Attach RDS auth policy to EC2 role
	rdsAuthPolicyAttachment, err := iam.NewRolePolicyAttachment(ctx, "ec2-rds-auth-policy", &iam.RolePolicyAttachmentArgs{
		Role:      ec2Role.Name,
		PolicyArn: rdsAuthPolicy.Arn,
	})
	if err != nil {
		return nil, err
	}

	// Create policy for S3 access
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
				}
			]
		}`),
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "s3-access-policy"),
	})
	if err != nil {
		return nil, err
	}

	// Attach S3 access policy to EC2 role
	s3AccessPolicyAttachment, err := iam.NewRolePolicyAttachment(ctx, "ec2-s3-access-policy", &iam.RolePolicyAttachmentArgs{
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
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "rds-describe-policy"),
	})
	if err != nil {
		return nil, err
	}

	// Attach RDS describe policy to EC2 role
	rdsDescribePolicyAttachment, err := iam.NewRolePolicyAttachment(ctx, "ec2-rds-describe-policy", &iam.RolePolicyAttachmentArgs{
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
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "ssm-parameter-policy"),
	})
	if err != nil {
		return nil, err
	}

	// Attach SSM Parameter Store policy to EC2 role
	ssmParameterPolicyAttachment, err := iam.NewRolePolicyAttachment(ctx, "ec2-ssm-parameter-policy", &iam.RolePolicyAttachmentArgs{
		Role:      ec2Role.Name,
		PolicyArn: ssmPolicy.Arn,
	})
	if err != nil {
		return nil, err
	}

	// Create EC2 instance profile with explicit dependencies on policy attachments
	// This ensures that policy attachments are created before the instance profile
	ec2InstanceProfile, err := iam.NewInstanceProfile(ctx, "ec2-instance-profile", &iam.InstanceProfileArgs{
		Role: ec2Role.Name,
	}, pulumi.DependsOn([]pulumi.Resource{
		ssmPolicyAttachment,
		rdsAuthPolicyAttachment,
		s3AccessPolicyAttachment,
		rdsDescribePolicyAttachment,
		ssmParameterPolicyAttachment,
	}))
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
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "aurora-service-role"),
	})
	if err != nil {
		return nil, err
	}

	// Attach S3 access policy to Aurora role
	auroraS3PolicyAttachment, err := iam.NewRolePolicyAttachment(ctx, "aurora-s3-access-policy", &iam.RolePolicyAttachmentArgs{
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
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "aurora-subnet-group"),
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
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "aurora-param-group"),
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
		Tags:                             utils.CreateResourceTags(ctx, cfg.Tags, "aurora-cluster"),
	})
	if err != nil {
		return nil, err
	}

	// Create primary instance - this is critical for the cluster to be usable
	primaryInstance, err := rds.NewClusterInstance(ctx, "aurora-primary", &rds.ClusterInstanceArgs{
		ClusterIdentifier:          cluster.ID(),
		InstanceClass:              pulumi.String(cfg.TestEnv.AuroraInstanceType),
		Engine:                     pulumi.String("aurora-mysql"),
		EngineVersion:              pulumi.String("8.0.mysql_aurora.3.04.0"),
		DbSubnetGroupName:          subnetGroup.Name,
		PubliclyAccessible:         pulumi.Bool(false),
		MonitoringInterval:         pulumi.Int(0), // Disable enhanced monitoring as per requirements
		PerformanceInsightsEnabled: pulumi.Bool(false),
		Tags:                       utils.CreateResourceTags(ctx, cfg.Tags, "aurora-primary"),
	}, pulumi.DependsOn([]pulumi.Resource{cluster}))
	if err != nil {
		return nil, err
	}

	// Create replica instance - this is important for high availability
	replicaInstance, err := rds.NewClusterInstance(ctx, "aurora-replica", &rds.ClusterInstanceArgs{
		ClusterIdentifier:          cluster.ID(),
		InstanceClass:              pulumi.String(cfg.TestEnv.AuroraInstanceType),
		Engine:                     pulumi.String("aurora-mysql"),
		EngineVersion:              pulumi.String("8.0.mysql_aurora.3.04.0"),
		DbSubnetGroupName:          subnetGroup.Name,
		PubliclyAccessible:         pulumi.Bool(false),
		MonitoringInterval:         pulumi.Int(0), // Disable enhanced monitoring as per requirements
		PerformanceInsightsEnabled: pulumi.Bool(false),
		Tags:                       utils.CreateResourceTags(ctx, cfg.Tags, "aurora-replica"),
	}, pulumi.DependsOn([]pulumi.Resource{cluster, primaryInstance}))
	if err != nil {
		return nil, err
	}

	// Store Aurora endpoint in SSM Parameter Store
	_, err = ssm.NewParameter(ctx, "aurora-endpoint-param", &ssm.ParameterArgs{
		Name:  pulumi.String("/aurora-audit-log-lab/aurora-endpoint"),
		Type:  pulumi.String("String"),
		Value: cluster.Endpoint,
		Tags:  utils.CreateResourceTags(ctx, cfg.Tags, "aurora-endpoint"),
	}, pulumi.DependsOn([]pulumi.Resource{cluster, primaryInstance}))
	if err != nil {
		return nil, err
	}

	// Store S3 bucket name in SSM Parameter Store
	_, err = ssm.NewParameter(ctx, "s3-bucket-param", &ssm.ParameterArgs{
		Name:  pulumi.String("/aurora-audit-log-lab/s3-bucket-name"),
		Type:  pulumi.String("String"),
		Value: pulumi.String("zzhe-aurora-audit-log-lab-bucket"),
		Tags:  utils.CreateResourceTags(ctx, cfg.Tags, "s3-bucket-name"),
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
mysql -h $CLUSTER_ENDPOINT -u admin -p$MASTER_PASSWORD << 'EOFMYSQL'
CREATE DATABASE IF NOT EXISTS sysbench_test;
CREATE USER IF NOT EXISTS 'sysbench'@'%' IDENTIFIED BY 'sysbench123';
GRANT ALL PRIVILEGES ON sysbench_test.* TO 'sysbench'@'%';
FLUSH PRIVILEGES;
EOFMYSQL

# Prepare sysbench OLTP tables with increased size
echo "Preparing sysbench tables (20 tables, 500,000 rows each)..."
sysbench oltp_read_write --db-driver=mysql --mysql-host=$CLUSTER_ENDPOINT --mysql-user=sysbench --mysql-password='sysbench123' --mysql-db=sysbench_test --tables=20 --table-size=500000 --threads=20 prepare

echo "Sysbench tables prepared successfully!"
EOF

# Create enhanced test execution script
cat > /home/ec2-user/scripts/test_audit_logs.sh << 'EOF'
#!/bin/bash
# Enhanced script to run sysbench tests and verify audit logs
# This script supports targeting specific Aurora nodes and generates more audit logs

# Get AWS region using IMDSv2
echo "Retrieving AWS region using IMDSv2..."
TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
REGION=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/region)

if [ -z "$REGION" ]; then
    echo "Warning: Could not retrieve AWS region from instance metadata."
    echo "Using default region: ap-southeast-1"
    REGION="ap-southeast-1"
fi

echo "Using AWS region: $REGION"

# Get the Aurora endpoints from AWS CLI
echo "Getting Aurora cluster endpoints..."
CLUSTER_ID=$(aws rds describe-db-clusters --region $REGION --query "DBClusters[?Engine=='aurora-mysql'].DBClusterIdentifier" --output text | head -n 1)

if [ -z "$CLUSTER_ID" ]; then
    echo "Error: Could not find any Aurora MySQL clusters in region $REGION."
    echo "Please ensure the Aurora cluster is running and try again."
    exit 1
fi

# Get both writer and reader endpoints
WRITER_ENDPOINT=$(aws rds describe-db-clusters --region $REGION --db-cluster-identifier $CLUSTER_ID --query "DBClusters[0].Endpoint" --output text)
READER_ENDPOINT=$(aws rds describe-db-clusters --region $REGION --db-cluster-identifier $CLUSTER_ID --query "DBClusters[0].ReaderEndpoint" --output text)

# Fallback to SSM Parameter Store for writer endpoint if AWS CLI fails
if [ -z "$WRITER_ENDPOINT" ]; then
    echo "Falling back to SSM Parameter Store for writer endpoint..."
    WRITER_ENDPOINT=$(aws ssm get-parameter --name "/aurora-audit-log-lab/aurora-endpoint" --region $REGION --query "Parameter.Value" --output text)
fi


echo "Aurora writer endpoint: $WRITER_ENDPOINT"
echo "Aurora reader endpoint: $READER_ENDPOINT"
echo "S3 bucket name: $BUCKET_NAME"

# Set passwords for authentication
echo "Setting passwords for authentication..."
export ADMIN_PASSWORD="Password123!"
export SYSBENCH_PASSWORD="sysbench123"

# Create test directory
TEST_DIR=$(mktemp -d)
echo "Using temporary directory: $TEST_DIR"

# Function to run sysbench test with specified endpoint
run_sysbench_test() {
    local test_type=$1
    local endpoint=$2
    local node_type=$3
    local threads=$4
    local tables=$5
    local table_size=$6
    local duration=$7
    
    echo "Running $test_type workload on $node_type node ($endpoint)..."
    echo "Parameters: threads=$threads, tables=$tables, table_size=$table_size, duration=$duration seconds"
    
    sysbench $test_type \
        --db-driver=mysql \
        --mysql-host=$endpoint \
        --mysql-user=sysbench \
        --mysql-password=$SYSBENCH_PASSWORD \
        --mysql-db=sysbench_test \
        --tables=$tables \
        --table-size=$table_size \
        --threads=$threads \
        --time=$duration \
        run
        
    echo "$test_type workload on $node_type node completed."
}

# Function to run custom SQL queries
run_custom_queries() {
    local endpoint=$1
    local node_type=$2
    local iterations=$3
    
    echo "Running custom queries on $node_type node ($endpoint)..."
    
    for i in $(seq 1 $iterations); do
        echo "Iteration $i of $iterations"
        mysql -h $endpoint -u sysbench -p$SYSBENCH_PASSWORD sysbench_test -e "
            SELECT COUNT(*) FROM sbtest1;
            SELECT AVG(k) FROM sbtest1;
            SELECT MAX(k), MIN(k) FROM sbtest1;
            SELECT COUNT(DISTINCT k) FROM sbtest1;
            SELECT COUNT(*) FROM sbtest1 WHERE k BETWEEN 1 AND 10000;
        "
    done
    
    echo "Custom queries on $node_type node completed."
}

# Run authentication tests
echo "Running authentication tests..."
echo "1. Testing admin authentication on writer node..."
mysql -h $WRITER_ENDPOINT -u admin -p$ADMIN_PASSWORD -e "SELECT 'Admin authentication on writer node successful';"

echo "2. Testing sysbench user authentication on writer node..."
mysql -h $WRITER_ENDPOINT -u sysbench -p$SYSBENCH_PASSWORD -e "SELECT 'Sysbench user authentication on writer node successful';"

echo "3. Testing sysbench user authentication on reader node..."
mysql -h $READER_ENDPOINT -u sysbench -p$SYSBENCH_PASSWORD -e "SELECT 'Sysbench user authentication on reader node successful';"

echo "4. Testing invalid credentials (should fail)..."
mysql -h $WRITER_ENDPOINT -u admin -p"wrong_password" -e "SELECT 1;" || echo "Invalid credentials test passed (expected failure)"

# Run OLTP workload tests with increased load
echo "Running enhanced OLTP workload tests..."

# Read-only workload on reader node (higher load)
run_sysbench_test oltp_read_only $READER_ENDPOINT "reader" 20 20 500000 180

# Read-write workload on writer node (higher load)
run_sysbench_test oltp_read_write $WRITER_ENDPOINT "writer" 20 20 500000 180

# Write-only workload on writer node (higher load)
run_sysbench_test oltp_write_only $WRITER_ENDPOINT "writer" 20 20 500000 180

# Run custom queries for additional workload
run_custom_queries $READER_ENDPOINT "reader" 20
run_custom_queries $WRITER_ENDPOINT "writer" 10

# Run more intensive schema modification tests on writer node
echo "Running intensive schema modification tests on writer node..."
mysql -h $WRITER_ENDPOINT -u admin -p$ADMIN_PASSWORD << 'EOFSCHEMA'
-- Create test tables
CREATE TABLE IF NOT EXISTS sysbench_test.audit_test1 (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255),
    description TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Insert some data
INSERT INTO sysbench_test.audit_test1 (name, description)
VALUES
    ('Test 1', 'Description for test 1'),
    ('Test 2', 'Description for test 2'),
    ('Test 3', 'Description for test 3');

-- Modify schema multiple times
ALTER TABLE sysbench_test.audit_test1 ADD COLUMN status VARCHAR(50);
ALTER TABLE sysbench_test.audit_test1 ADD INDEX idx_name (name);
ALTER TABLE sysbench_test.audit_test1 ADD COLUMN updated_at TIMESTAMP;
UPDATE sysbench_test.audit_test1 SET status = 'active';
ALTER TABLE sysbench_test.audit_test1 MODIFY COLUMN status VARCHAR(100);
ALTER TABLE sysbench_test.audit_test1 DROP COLUMN updated_at;
DROP TABLE sysbench_test.audit_test1;
EOFSCHEMA

# Run more user management tests for additional DCL audit logs
echo "Running enhanced user management tests..."
mysql -h $WRITER_ENDPOINT -u admin -p$ADMIN_PASSWORD << 'EOFDCL'
-- Create multiple test users
CREATE USER IF NOT EXISTS 'audit_user1'@'%' IDENTIFIED BY 'test123';
CREATE USER IF NOT EXISTS 'audit_user2'@'%' IDENTIFIED BY 'test123';
CREATE USER IF NOT EXISTS 'audit_user3'@'%' IDENTIFIED BY 'test123';

-- Grant different privileges
GRANT SELECT ON sysbench_test.* TO 'audit_user1'@'%';
GRANT INSERT, UPDATE ON sysbench_test.* TO 'audit_user2'@'%';
GRANT ALL PRIVILEGES ON sysbench_test.* TO 'audit_user3'@'%';

-- Modify privileges
REVOKE SELECT ON sysbench_test.* FROM 'audit_user1'@'%';
GRANT SELECT ON sysbench_test.* TO 'audit_user1'@'%';
REVOKE INSERT ON sysbench_test.* FROM 'audit_user2'@'%';

-- Drop users
DROP USER 'audit_user1'@'%';
DROP USER 'audit_user2'@'%';
DROP USER 'audit_user3'@'%';
EOFDCL

echo "All tests completed successfully!"
echo "Workload has been generated on both writer and reader nodes."
echo "Writer endpoint: $WRITER_ENDPOINT"
echo "Reader endpoint: $READER_ENDPOINT"
EOF

# Create cleanup script
cat > /home/ec2-user/scripts/cleanup_sysbench.sh << 'EOF'
#!/bin/bash
# Cleanup script for sysbench test database and user
# This script removes all resources created by setup_sysbench.sh

# Get AWS region using IMDSv2
echo "Retrieving AWS region using IMDSv2..."
TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
REGION=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/region)

if [ -z "$REGION" ]; then
    echo "Warning: Could not retrieve AWS region from instance metadata."
    echo "Using default region: ap-southeast-1"
    REGION="ap-southeast-1"
fi

echo "Using AWS region: $REGION"

# Get the Aurora endpoint from SSM Parameter Store
echo "Getting Aurora cluster endpoint from SSM Parameter Store..."
CLUSTER_ENDPOINT=$(aws ssm get-parameter --name "/aurora-audit-log-lab/aurora-endpoint" --region $REGION --query "Parameter.Value" --output text)

# Fallback to AWS CLI if Parameter Store fails
if [ -z "$CLUSTER_ENDPOINT" ]; then
    echo "Error: Could not get Aurora endpoint from SSM Parameter Store."
    echo "Falling back to AWS CLI to find Aurora cluster..."
    
    # Fallback to finding the cluster using AWS CLI
    CLUSTER_ENDPOINT=$(aws rds describe-db-clusters --region $REGION --query "DBClusters[?Engine=='aurora-mysql'].Endpoint" --output text | head -n 1)
    
    if [ -z "$CLUSTER_ENDPOINT" ]; then
        echo "Error: Could not find any Aurora MySQL clusters in region $REGION."
        echo "Please ensure the Aurora cluster is running and try again."
        exit 1
    fi
fi

echo "Aurora endpoint: $CLUSTER_ENDPOINT"

# Connect using the master password
echo "Connecting to Aurora using master password..."
MASTER_PASSWORD="Password123!"

# Drop database and user
echo "Dropping sysbench_test database and sysbench user..."
mysql -h $CLUSTER_ENDPOINT -u admin -p$MASTER_PASSWORD << 'EOFCLEANUP'
-- Drop all tables in sysbench_test database
DROP DATABASE IF EXISTS sysbench_test;

-- Drop sysbench user
DROP USER IF EXISTS 'sysbench'@'%';

-- Drop test users if they exist (in case test_audit_logs.sh was interrupted)
DROP USER IF EXISTS 'audit_user1'@'%';
DROP USER IF EXISTS 'audit_user2'@'%';
DROP USER IF EXISTS 'audit_user3'@'%';
DROP USER IF EXISTS 'test_user'@'%';
EOFCLEANUP

if [ $? -eq 0 ]; then
    echo "✅ Cleanup completed successfully!"
    echo "✅ Dropped sysbench_test database"
    echo "✅ Dropped sysbench user"
    echo "✅ Dropped any test users"
else
    echo "❌ Error: Cleanup failed."
    exit 1
fi
EOF

# Make cleanup script executable
chmod +x /home/ec2-user/scripts/cleanup_sysbench.sh
`

	// Create EC2 instance with explicit dependencies on Aurora cluster and instances
	// This ensures that the Aurora cluster and instances are created before the EC2 instance
	ec2Instance, err := ec2.NewInstance(ctx, "aurora-ec2", &ec2.InstanceArgs{
		Ami:                      pulumi.String(ami.Id),
		InstanceType:             pulumi.String(cfg.TestEnv.EC2InstanceType),
		SubnetId:                 networkResources.PublicSubnet.ID(),
		VpcSecurityGroupIds:      pulumi.StringArray{ec2SecurityGroup.ID()},
		AssociatePublicIpAddress: pulumi.Bool(true),
		KeyName:                  pulumi.String(cfg.TestEnv.EC2KeyPairName),
		IamInstanceProfile:       ec2InstanceProfile.Name,
		UserData:                 pulumi.String(userData),
		Tags:                     utils.CreateResourceTags(ctx, cfg.Tags, "aurora-ec2"),
	}, pulumi.DependsOn([]pulumi.Resource{
		ec2InstanceProfile,
		cluster,
		primaryInstance,
		replicaInstance,
	}))
	if err != nil {
		return nil, err
	}

	return &TestEnvironmentResources{
		Ec2SecurityGroup:             ec2SecurityGroup,
		AuroraSecurityGroup:          auroraSecurityGroup,
		Ec2Role:                      ec2Role,
		Ec2InstanceProfile:           ec2InstanceProfile,
		AuroraRole:                   auroraRole,
		AuditLogBucket:               auditLogBucket,
		AuroraCluster:                cluster,
		AuroraPrimary:                primaryInstance,
		AuroraReplica:                replicaInstance,
		Ec2Instance:                  ec2Instance,
		SsmPolicyAttachment:          ssmPolicyAttachment,
		RdsAuthPolicyAttachment:      rdsAuthPolicyAttachment,
		S3AccessPolicyAttachment:     s3AccessPolicyAttachment,
		RdsDescribePolicyAttachment:  rdsDescribePolicyAttachment,
		SsmParameterPolicyAttachment: ssmParameterPolicyAttachment,
		AuroraS3PolicyAttachment:     auroraS3PolicyAttachment,
	}, nil
}
