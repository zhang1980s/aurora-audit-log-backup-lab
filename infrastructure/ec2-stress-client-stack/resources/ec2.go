package resources

import (
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"ec2-stress-client-stack/config"
	"ec2-stress-client-stack/utils"
)

// EC2Resources holds all the resources for the EC2 stress client environment
type EC2Resources struct {
	EC2SecurityGroup   *ec2.SecurityGroup
	EC2Role            *iam.Role
	EC2InstanceProfile *iam.InstanceProfile
	EC2Instance        *ec2.Instance
}

// CreateEC2Resources creates the EC2 instance and related resources
func CreateEC2Resources(ctx *pulumi.Context, cfg *config.Config, networkStack *pulumi.StackReference, auroraStack *pulumi.StackReference) (*EC2Resources, error) {
	// Get network resources from network stack
	vpcId := networkStack.GetOutput(pulumi.String("vpcId"))
	publicSubnetId := networkStack.GetOutput(pulumi.String("publicSubnetId"))

	// Get Aurora resources from Aurora stack
	auroraSecurityGroupId := auroraStack.GetOutput(pulumi.String("auroraSecurityGroupId"))

	// Create EC2 security group
	ec2SecurityGroup, err := ec2.NewSecurityGroup(ctx, "ec2-sg", &ec2.SecurityGroupArgs{
		VpcId:       vpcId.ApplyT(func(v interface{}) string { return v.(string) }).(pulumi.StringOutput),
		Description: pulumi.String("Security group for EC2 stress client"),
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
		Tags: CreateResourceTags(ctx, cfg.Tags, "aurora-ec2-sg"),
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
		Tags: CreateResourceTags(ctx, cfg.Tags, "aurora-ec2-role"),
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
						"*"
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

	// Create user data script
	userData := pulumi.String(`#!/bin/bash
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

# Get AWS region using IMDSv2
TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
REGION=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/region)

# Get the Aurora endpoint from SSM Parameter Store
CLUSTER_ENDPOINT=$(aws ssm get-parameter --name "/aurora-audit-log-lab/aurora-endpoint" --region $REGION --query "Parameter.Value" --output text)

# Connect using the master password
echo "Connecting to Aurora using master password..."
MASTER_PASSWORD="Password123!"

# Create test database and user
mysql -h $CLUSTER_ENDPOINT -u admin -p$MASTER_PASSWORD << 'MYSQLEOF'
CREATE DATABASE IF NOT EXISTS sysbench_test;
CREATE USER IF NOT EXISTS 'sysbench'@'%' IDENTIFIED BY 'sysbench123';
GRANT ALL PRIVILEGES ON sysbench_test.* TO 'sysbench'@'%';
FLUSH PRIVILEGES;
MYSQLEOF

# Prepare sysbench OLTP tables
sysbench oltp_read_write --db-driver=mysql --mysql-host=$CLUSTER_ENDPOINT --mysql-user=sysbench --mysql-password='sysbench123' --mysql-db=sysbench_test --tables=10 --table-size=100000 --threads=4 prepare
EOF

# Create aurora stress test script
cat > /home/ec2-user/scripts/aurora_stress_test.sh << 'EOF'
#!/bin/bash
# Aurora Stress Test Script
# This script runs stress tests on Aurora MySQL instances with configurable parameters

# Default values
MODE="run"
TARGET_INSTANCE="writer"
WORKLOAD_TYPE="oltp_read_write"
INTENSITY="medium"
THREADS=20
TABLES=20
TABLE_SIZE=500000
DURATION=180
DB_USER="sysbench"
DB_PASSWORD="sysbench123"
DB_NAME="sysbench_test"
WRITER_ENDPOINT=""
READER_ENDPOINT=""

# Display help information
function show_help {
	   echo "Usage: $0 [OPTIONS]"
	   echo
	   echo "Options:"
	   echo "  --mode MODE                  Operation mode: setup, run, cleanup, all (default: run)"
	   echo "  --target-instance TARGET     Target instance: writer, reader, both (default: writer)"
	   echo "  --workload-type TYPE         Workload type: oltp_read_only, oltp_read_write, oltp_write_only (default: oltp_read_write)"
	   echo "  --intensity LEVEL            Intensity level: low, medium, high, custom (default: medium)"
	   echo "  --threads N                  Number of threads (default: 20)"
	   echo "  --tables N                   Number of tables (default: 20)"
	   echo "  --table-size N               Number of rows per table (default: 500000)"
	   echo "  --duration N                 Test duration in seconds (default: 180)"
	   echo "  --writer-endpoint HOST       Writer endpoint (required)"
	   echo "  --reader-endpoint HOST       Reader endpoint (required for reader or both target)"
	   echo "  --help                       Show this help message"
	   echo
	   echo "Examples:"
	   echo "  $0 --mode setup --writer-endpoint aurora-cluster.example.com --tables 50 --table-size 1000000"
	   echo "  $0 --mode run --target-instance writer --intensity high --writer-endpoint aurora-cluster.example.com"
	   echo "  $0 --mode run --target-instance reader --workload-type oltp_read_only --writer-endpoint aurora-cluster.example.com --reader-endpoint aurora-cluster-ro.example.com"
	   echo "  $0 --mode run --target-instance both --intensity custom --threads 100 --duration 600 --writer-endpoint aurora-cluster.example.com --reader-endpoint aurora-cluster-ro.example.com"
	   echo "  $0 --mode cleanup --writer-endpoint aurora-cluster.example.com"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
	   case "$1" in
	       --mode)
	           MODE="$2"
	           shift 2
	           ;;
	       --target-instance)
	           TARGET_INSTANCE="$2"
	           shift 2
	           ;;
	       --workload-type)
	           WORKLOAD_TYPE="$2"
	           shift 2
	           ;;
	       --intensity)
	           INTENSITY="$2"
	           shift 2
	           ;;
	       --threads)
	           THREADS="$2"
	           shift 2
	           ;;
	       --tables)
	           TABLES="$2"
	           shift 2
	           ;;
	       --table-size)
	           TABLE_SIZE="$2"
	           shift 2
	           ;;
	       --duration)
	           DURATION="$2"
	           shift 2
	           ;;
	       --writer-endpoint)
	           WRITER_ENDPOINT="$2"
	           shift 2
	           ;;
	       --reader-endpoint)
	           READER_ENDPOINT="$2"
	           shift 2
	           ;;
	       --help)
	           show_help
	           exit 0
	           ;;
	       *)
	           echo "Unknown option: $1"
	           show_help
	           exit 1
	           ;;
	   esac
done

# Validate required parameters
if [[ -z "$WRITER_ENDPOINT" ]]; then
	   echo "Error: Writer endpoint is required"
	   show_help
	   exit 1
fi

if [[ "$TARGET_INSTANCE" == "reader" || "$TARGET_INSTANCE" == "both" ]] && [[ -z "$READER_ENDPOINT" ]]; then
	   echo "Error: Reader endpoint is required when target instance is reader or both"
	   show_help
	   exit 1
fi

# Set parameters based on intensity level
if [[ "$INTENSITY" == "low" ]]; then
	   THREADS=5
	   TABLES=5
	   TABLE_SIZE=100000
	   DURATION=60
elif [[ "$INTENSITY" == "medium" ]]; then
	   THREADS=20
	   TABLES=20
	   TABLE_SIZE=500000
	   DURATION=180
elif [[ "$INTENSITY" == "high" ]]; then
	   THREADS=25
	   TABLES=25
	   TABLE_SIZE=1000000
	   DURATION=300
fi

# Function to setup the database
function setup_db {
	   local endpoint=$1
	   echo "Setting up database on $endpoint..."
	   
	   # Create test database and user
	   mysql -h $endpoint -u admin -pPassword123! << MYSQLEOF
CREATE DATABASE IF NOT EXISTS $DB_NAME;
CREATE USER IF NOT EXISTS '$DB_USER'@'%' IDENTIFIED BY '$DB_PASSWORD';
GRANT ALL PRIVILEGES ON $DB_NAME.* TO '$DB_USER'@'%';
FLUSH PRIVILEGES;
MYSQLEOF
	   
	   if [ $? -ne 0 ]; then
	       echo "Error: Failed to create database and user"
	       return 1
	   fi
	   
	   # Prepare sysbench tables
	   echo "Preparing sysbench tables with $TABLES tables of $TABLE_SIZE rows each..."
	   sysbench oltp_read_write \
	       --db-driver=mysql \
	       --mysql-host=$endpoint \
	       --mysql-user=$DB_USER \
	       --mysql-password=$DB_PASSWORD \
	       --mysql-db=$DB_NAME \
	       --tables=$TABLES \
	       --table-size=$TABLE_SIZE \
	       --threads=$THREADS \
	       prepare
	   
	   if [ $? -ne 0 ]; then
	       echo "Error: Failed to prepare sysbench tables"
	       return 1
	   fi
	   
	   echo "Database setup completed successfully"
	   return 0
}

# Function to run the stress test
function run_test {
	   local endpoint=$1
	   local workload=$2
	   
	   echo "Running $workload test on $endpoint..."
	   echo "Parameters: threads=$THREADS, tables=$TABLES, duration=$DURATION seconds"
	   
	   sysbench $workload \
	       --db-driver=mysql \
	       --mysql-host=$endpoint \
	       --mysql-user=$DB_USER \
	       --mysql-password=$DB_PASSWORD \
	       --mysql-db=$DB_NAME \
	       --tables=$TABLES \
	       --table-size=$TABLE_SIZE \
	       --threads=$THREADS \
	       --time=$DURATION \
	       run
	   
	   if [ $? -ne 0 ]; then
	       echo "Error: Test failed"
	       return 1
	   fi
	   
	   echo "Test completed successfully"
	   return 0
}

# Function to cleanup the database
function cleanup_db {
	   local endpoint=$1
	   echo "Cleaning up database on $endpoint..."
	   
	   # Cleanup sysbench tables
	   sysbench oltp_read_write \
	       --db-driver=mysql \
	       --mysql-host=$endpoint \
	       --mysql-user=$DB_USER \
	       --mysql-password=$DB_PASSWORD \
	       --mysql-db=$DB_NAME \
	       --tables=$TABLES \
	       cleanup
	   
	   if [ $? -ne 0 ]; then
	       echo "Error: Failed to cleanup sysbench tables"
	       return 1
	   fi
	   
	   # Drop database and user
	   mysql -h $endpoint -u admin -pPassword123! << MYSQLEOF
DROP DATABASE IF EXISTS $DB_NAME;
DROP USER IF EXISTS '$DB_USER'@'%';
MYSQLEOF
	   
	   if [ $? -ne 0 ]; then
	       echo "Error: Failed to drop database and user"
	       return 1
	   fi
	   
	   echo "Cleanup completed successfully"
	   return 0
}

# Main execution
echo "Aurora Stress Test"
echo "================="
echo "Mode: $MODE"
echo "Target Instance: $TARGET_INSTANCE"
echo "Workload Type: $WORKLOAD_TYPE"
echo "Intensity: $INTENSITY"
echo "Writer Endpoint: $WRITER_ENDPOINT"
echo "Reader Endpoint: $READER_ENDPOINT"
echo "================="

# Execute based on mode
case "$MODE" in
	   setup)
	       setup_db $WRITER_ENDPOINT
	       ;;
	   run)
	       if [[ "$TARGET_INSTANCE" == "writer" || "$TARGET_INSTANCE" == "both" ]]; then
	           echo "Running test on writer instance..."
	           run_test $WRITER_ENDPOINT $WORKLOAD_TYPE
	       fi
	       
	       if [[ "$TARGET_INSTANCE" == "reader" || "$TARGET_INSTANCE" == "both" ]]; then
	           echo "Running test on reader instance..."
	           run_test $READER_ENDPOINT $WORKLOAD_TYPE
	       fi
	       ;;
	   cleanup)
	       cleanup_db $WRITER_ENDPOINT
	       ;;
	   all)
	       setup_db $WRITER_ENDPOINT
	       
	       if [[ "$TARGET_INSTANCE" == "writer" || "$TARGET_INSTANCE" == "both" ]]; then
	           echo "Running test on writer instance..."
	           run_test $WRITER_ENDPOINT $WORKLOAD_TYPE
	       fi
	       
	       if [[ "$TARGET_INSTANCE" == "reader" || "$TARGET_INSTANCE" == "both" ]]; then
	           echo "Running test on reader instance..."
	           run_test $READER_ENDPOINT $WORKLOAD_TYPE
	       fi
	       
	       cleanup_db $WRITER_ENDPOINT
	       ;;
	   *)
	       echo "Error: Invalid mode '$MODE'"
	       show_help
	       exit 1
	       ;;
esac

echo "Aurora Stress Test completed"
exit 0
EOF

# Make scripts executable
chmod +x /home/ec2-user/scripts/setup_sysbench.sh
chmod +x /home/ec2-user/scripts/aurora_stress_test.sh

# Set ownership
chown -R ec2-user:ec2-user /home/ec2-user/scripts
`)

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

	// Create EC2 instance
	ec2Instance, err := ec2.NewInstance(ctx, "aurora-ec2", &ec2.InstanceArgs{
		Ami:                      pulumi.String(ami.Id),
		InstanceType:             pulumi.String(cfg.EC2.InstanceType),
		SubnetId:                 publicSubnetId.ApplyT(func(v interface{}) string { return v.(string) }).(pulumi.StringOutput),
		VpcSecurityGroupIds:      pulumi.StringArray{ec2SecurityGroup.ID()},
		AssociatePublicIpAddress: pulumi.Bool(true),
		KeyName:                  pulumi.String(cfg.EC2.KeyPairName),
		IamInstanceProfile:       ec2InstanceProfile.Name,
		UserData:                 userData,
		Tags:                     CreateResourceTags(ctx, cfg.Tags, "aurora-ec2"),
	})
	if err != nil {
		return nil, err
	}

	// Allow MySQL access from EC2 to Aurora
	_, err = ec2.NewSecurityGroupRule(ctx, "ec2-to-aurora-mysql", &ec2.SecurityGroupRuleArgs{
		Type:                  pulumi.String("ingress"),
		FromPort:              pulumi.Int(3306),
		ToPort:                pulumi.Int(3306),
		Protocol:              pulumi.String("tcp"),
		SourceSecurityGroupId: ec2SecurityGroup.ID(),
		SecurityGroupId:       auroraSecurityGroupId.ApplyT(func(v interface{}) string { return v.(string) }).(pulumi.StringOutput),
		Description:           pulumi.String("Allow MySQL from EC2 instance"),
	})
	if err != nil {
		return nil, err
	}

	return &EC2Resources{
		EC2SecurityGroup:   ec2SecurityGroup,
		EC2Role:            ec2Role,
		EC2InstanceProfile: ec2InstanceProfile,
		EC2Instance:        ec2Instance,
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