#!/bin/bash
# Setup sysbench test database

# Get AWS region using IMDSv2
echo "Retrieving AWS region using IMDSv2..."
TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
AWS_REGION=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/region)

if [ -z "$AWS_REGION" ]; then
    echo "Warning: Could not retrieve AWS region from instance metadata."
    echo "Using default region: ap-southeast-1"
    AWS_REGION="ap-southeast-1"
fi

echo "Using AWS region: $AWS_REGION"

# Get the Aurora endpoint from SSM Parameter Store
echo "Getting Aurora cluster endpoint from SSM Parameter Store..."
CLUSTER_ENDPOINT=$(aws ssm get-parameter --name "/aurora-audit-log-lab/aurora-endpoint" --region $AWS_REGION --query "Parameter.Value" --output text)

if [ -z "$CLUSTER_ENDPOINT" ]; then
    echo "Error: Could not get Aurora endpoint from SSM Parameter Store."
    echo "Falling back to AWS CLI to find Aurora cluster..."
    
    # Fallback to finding the cluster using AWS CLI
    CLUSTER_ENDPOINT=$(aws rds describe-db-clusters --region $AWS_REGION --query "DBClusters[?Engine=='aurora-mysql'].Endpoint" --output text | head -n 1)
    
    if [ -z "$CLUSTER_ENDPOINT" ]; then
        echo "Error: Could not find any Aurora MySQL clusters in region $AWS_REGION."
        echo "Please ensure the Aurora cluster is running and try again."
        exit 1
    fi
fi

echo "Aurora endpoint: $CLUSTER_ENDPOINT"

# Connect using the master password
echo "Connecting to Aurora using master password..."
# Use default password directly to avoid read issues
MASTER_PASSWORD="Password123!"
echo "Using password: Password123!"

# Create test database and user
echo "Creating test database and user..."
mysql -h $CLUSTER_ENDPOINT -u admin -p$MASTER_PASSWORD << 'EOF'
CREATE DATABASE IF NOT EXISTS sysbench_test;
CREATE USER IF NOT EXISTS 'sysbench'@'%' IDENTIFIED BY 'sysbench123';
GRANT ALL PRIVILEGES ON sysbench_test.* TO 'sysbench'@'%';
FLUSH PRIVILEGES;
EOF

if [ $? -ne 0 ]; then
    echo "Error: Failed to create database and user."
    exit 1
fi

echo "Database and user created successfully."

# Set sysbench password
export MYSQL_PWD="sysbench123"

# Verify password authentication works
echo "Verifying password authentication..."
mysql -h $CLUSTER_ENDPOINT -u sysbench -p$MYSQL_PWD -e "SELECT 'Password authentication successful';"

# Prepare sysbench OLTP tables
echo "Preparing sysbench OLTP tables..."
sysbench oltp_read_write \
    --db-driver=mysql \
    --mysql-host=$CLUSTER_ENDPOINT \
    --mysql-user=sysbench \
    --mysql-password=$MYSQL_PWD \
    --mysql-db=sysbench_test \
    --tables=10 \
    --table-size=100000 \
    --threads=4 \
    prepare

if [ $? -ne 0 ]; then
    echo "Error: Failed to prepare sysbench tables."
    exit 1
fi

echo "Sysbench setup completed successfully!"