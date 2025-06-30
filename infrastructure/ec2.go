package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// createEC2Instance creates the EC2 instance in the public subnet
func createEC2Instance(ctx *pulumi.Context, vpcResources *VpcResources, iamResources *IamResources) (*ec2.Instance, error) {
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

	// Use hardcoded key pair name as specified
	keyPairName := "keypair-sandbox0-sin-mymac.pem"

	// Create EC2 instance
	instance, err := ec2.NewInstance(ctx, "aurora-ec2", &ec2.InstanceArgs{
		Ami:                      pulumi.String(ami.Id),
		InstanceType:             pulumi.String("t4g.micro"),
		SubnetId:                 vpcResources.publicSubnet.ID(),
		VpcSecurityGroupIds:      pulumi.StringArray{vpcResources.ec2SecurityGroup.ID()},
		AssociatePublicIpAddress: pulumi.Bool(true),
		KeyName:                  pulumi.String(keyPairName),
		IamInstanceProfile:       iamResources.ec2InstanceProfile.Name,
		UserData:                 pulumi.String(userData),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-ec2"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Export EC2 instance public IP
	ctx.Export("ec2PublicIp", instance.PublicIp)

	return instance, nil
}
