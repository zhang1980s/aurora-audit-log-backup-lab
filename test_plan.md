# Aurora Audit Log Backup Lab - Test Plan

This document provides step-by-step guidance for testing the Aurora MySQL cluster connectivity and audit log functionality.

## Architecture Overview

This lab uses the following AWS resources:
- VPC with public and private subnets
- Internet Gateway for public subnet access
- VPC Endpoint for S3 access (instead of NAT Gateway)
- EC2 instance in the public subnet
- Aurora MySQL cluster in private subnets
- S3 bucket for audit logs

## Prerequisites

- AWS CLI configured
- Pulumi stack deployed
- EC2 instance running
- Aurora MySQL cluster running

## Test 1: Verify EC2 Instance Metadata

**Objective**: Ensure the EC2 instance can retrieve its region from the metadata service.

1. SSH into the EC2 instance:
   ```bash
   ssh -i your-key.pem ec2-user@<ec2-public-ip>
   ```

2. Use IMDSv2 approach to retrieve the region:
   ```bash
   TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
   curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/region
   ```

3. If the approach doesn't return a region, manually set the region:
   ```bash
   export AWS_REGION="ap-southeast-1"
   ```

## Test 2: Verify Aurora Connectivity

**Objective**: Ensure the EC2 instance can connect to the Aurora MySQL cluster.

1. Get the Aurora endpoint from SSM Parameter Store:
   ```bash
   # First get the AWS region
   TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
   AWS_REGION=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/region)
   
   # Then get the Aurora endpoint from Parameter Store
   CLUSTER_ENDPOINT=$(aws ssm get-parameter --name "/aurora-audit-log-lab/aurora-endpoint" --region $AWS_REGION --query "Parameter.Value" --output text)
   echo $CLUSTER_ENDPOINT
   ```

2. Test network connectivity:
   ```bash
   nc -zv $CLUSTER_ENDPOINT 3306
   ```

3. Test admin connection using password authentication:
   ```bash
   # Connect using the master password
   mysql -h $CLUSTER_ENDPOINT -u admin -pPassword123! -e "SELECT 'Admin password authentication successful';"
   ```

## Test 3: Verify Sysbench User Authentication

**Objective**: Ensure the sysbench user can connect to the Aurora cluster.

1. Create the sysbench user if it doesn't exist:
   ```bash
   # Create database and user
   mysql -h $CLUSTER_ENDPOINT -u admin -pPassword123! <<EOF
   CREATE DATABASE IF NOT EXISTS sysbench_test;
   CREATE USER IF NOT EXISTS 'sysbench'@'%' IDENTIFIED BY 'sysbench123';
   GRANT ALL PRIVILEGES ON sysbench_test.* TO 'sysbench'@'%';
   FLUSH PRIVILEGES;
   EOF
   ```

2. Test sysbench user connection with password:
   ```bash
   mysql -h $CLUSTER_ENDPOINT -u sysbench -psysbench123 -e "SELECT 'Sysbench user authentication successful';"
   ```

## Test 4: Run Sysbench Tests

**Objective**: Generate database activity for audit logging.

1. Prepare sysbench test tables:
   ```bash
   sysbench oltp_common \
       --db-driver=mysql \
       --mysql-host=$CLUSTER_ENDPOINT \
       --mysql-user=sysbench \
       --mysql-password=sysbench123 \
       --mysql-db=sysbench_test \
       --tables=10 \
       --table-size=100000 \
       prepare
   ```

2. Run read-only workload:
   ```bash
   sysbench oltp_read_only \
       --db-driver=mysql \
       --mysql-host=$CLUSTER_ENDPOINT \
       --mysql-user=sysbench \
       --mysql-password=sysbench123 \
       --mysql-db=sysbench_test \
       --tables=10 \
       --table-size=100000 \
       --threads=4 \
       --time=30 \
       run
   ```

3. Run read-write workload:
   ```bash
   sysbench oltp_read_write \
       --db-driver=mysql \
       --mysql-host=$CLUSTER_ENDPOINT \
       --mysql-user=sysbench \
       --mysql-password=sysbench123 \
       --mysql-db=sysbench_test \
       --tables=10 \
       --table-size=100000 \
       --threads=4 \
       --time=30 \
       run
   ```

## Test 5: Verify Audit Logs

**Objective**: Ensure audit logs are being generated and exported to S3.

1. Get the S3 bucket name from SSM Parameter Store:
   ```bash
   # Get the S3 bucket name from Parameter Store
   S3_BUCKET_NAME=$(aws ssm get-parameter --name "/aurora-audit-log-lab/s3-bucket-name" --region $AWS_REGION --query "Parameter.Value" --output text)
   echo $S3_BUCKET_NAME
   
   # Fallback to fixed name if Parameter Store fails
   if [ -z "$S3_BUCKET_NAME" ]; then
     echo "Using default bucket name: zzhe-aurora-audit-log-lab-bucket"
     S3_BUCKET_NAME="zzhe-aurora-audit-log-lab-bucket"
   fi
   ```

2. Wait for audit logs to be exported (this may take a few minutes):
   ```bash
   echo "Waiting for audit logs to be exported to S3..."
   sleep 300  # Wait for 5 minutes
   ```

3. Download audit logs from S3:
   ```bash
   TEST_DIR=$(mktemp -d)
   mkdir -p $TEST_DIR/audit_logs
   aws s3 sync s3://$S3_BUCKET_NAME/audit-logs $TEST_DIR/audit_logs --region $AWS_REGION
   ```

4. Check for connection events:
   ```bash
   grep -r "CONNECT" $TEST_DIR/audit_logs
   CONNECTION_COUNT=$(grep -r "CONNECT" $TEST_DIR/audit_logs | wc -l)
   echo "Found $CONNECTION_COUNT connection events."
   ```

5. Check for query events:
   ```bash
   grep -r "QUERY" $TEST_DIR/audit_logs
   QUERY_COUNT=$(grep -r "QUERY" $TEST_DIR/audit_logs | wc -l)
   echo "Found $QUERY_COUNT query events."
   ```

## Troubleshooting

### Region Issues

If the EC2 instance cannot retrieve its region from the metadata service:

1. Ensure you're using IMDSv2 (token-based approach):
   ```bash
   TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
   curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/region
   ```

2. Manually set the region in your environment:
   ```bash
   export AWS_REGION="ap-southeast-1"
   ```

3. Add the region parameter to AWS CLI commands:
   ```bash
   aws rds generate-db-auth-token --hostname $CLUSTER_ENDPOINT --port 3306 --username sysbench --region ap-southeast-1
   ```

### Authentication Issues

If you encounter authentication issues:

1. Verify you have the correct admin password (default is "Password123!").

2. Check if the sysbench user exists:
   ```bash
   # Check if user exists
   mysql -h $CLUSTER_ENDPOINT -u admin -pPassword123! -e "SELECT User FROM mysql.user WHERE User='sysbench';"
   ```

3. Verify the EC2 instance has the correct IAM role:
   ```bash
   curl -s http://169.254.169.254/latest/meta-data/iam/security-credentials/
   ```

4. Check if the IAM role has the necessary permissions:
   ```bash
   aws iam get-role-policy --role-name <role-name> --policy-name <policy-name> --region $AWS_REGION
   ```

### Network Issues

If you encounter network connectivity issues:

1. Check security group rules:
   ```bash
   aws ec2 describe-security-groups --group-ids <security-group-id> --region $AWS_REGION
   ```

2. Verify the EC2 instance is in the correct VPC:
   ```bash
   aws ec2 describe-instances --instance-ids $(curl -s http://169.254.169.254/latest/meta-data/instance-id) --query 'Reservations[0].Instances[0].VpcId' --region $AWS_REGION
   ```

3. Check the route tables:
   ```bash
   aws ec2 describe-route-tables --filters "Name=vpc-id,Values=<vpc-id>" --region $AWS_REGION
   ```

4. Verify the VPC Endpoint for S3 is properly configured:
   ```bash
   aws ec2 describe-vpc-endpoints --filters "Name=vpc-id,Values=<vpc-id>" --region $AWS_REGION
   ```

### S3 Access Issues

If you encounter issues accessing the S3 bucket for audit logs:

1. Verify the VPC Endpoint for S3 is working:
   ```bash
   aws s3 ls s3://zzhe-aurora-audit-log-lab-bucket --region $AWS_REGION
   ```

2. Check the S3 bucket policy:
   ```bash
   aws s3api get-bucket-policy --bucket zzhe-aurora-audit-log-lab-bucket --region $AWS_REGION
   ```

3. Ensure the Aurora cluster has the necessary IAM permissions:
   ```bash
   aws rds describe-db-clusters --db-cluster-identifier <cluster-id> --query "DBClusters[0].AssociatedRoles" --region $AWS_REGION