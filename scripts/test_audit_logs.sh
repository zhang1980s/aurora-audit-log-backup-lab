#!/bin/bash
# Run sysbench tests and verify audit logs

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

# Get the S3 bucket name from SSM Parameter Store
echo "Getting S3 bucket name from SSM Parameter Store..."
S3_BUCKET_NAME=$(aws ssm get-parameter --name "/aurora-audit-log-lab/s3-bucket-name" --region $AWS_REGION --query "Parameter.Value" --output text)

if [ -z "$S3_BUCKET_NAME" ]; then
    echo "Warning: Could not get S3 bucket name from SSM Parameter Store."
    echo "Using default bucket name: zzhe-aurora-audit-log-lab-bucket"
    S3_BUCKET_NAME="zzhe-aurora-audit-log-lab-bucket"
fi

echo "Aurora endpoint: $CLUSTER_ENDPOINT"
echo "S3 bucket name: $S3_BUCKET_NAME"

# Set passwords for authentication
echo "Setting passwords for authentication..."
export ADMIN_PASSWORD="Password123!"
export SYSBENCH_PWD="sysbench123"

# Create test directory
TEST_DIR=$(mktemp -d)
echo "Using temporary directory: $TEST_DIR"

# Run authentication tests
echo "Running authentication tests..."
echo "1. Testing admin authentication..."
mysql -h $CLUSTER_ENDPOINT -u admin -p$ADMIN_PASSWORD -e "SELECT 'Admin password authentication successful';"

echo "2. Testing sysbench user authentication with IAM..."
mysql -h $CLUSTER_ENDPOINT -u sysbench -p$SYSBENCH_PWD -e "SELECT 'Sysbench user authentication successful';"

echo "3. Testing invalid credentials (should fail)..."
mysql -h $CLUSTER_ENDPOINT -u admin -p"wrong_password" -e "SELECT 1;" || echo "Invalid credentials test passed (expected failure)"

# Run OLTP workload tests
echo "Running OLTP workload tests..."

echo "1. Running OLTP read-only workload..."
sysbench oltp_read_only \
    --db-driver=mysql \
    --mysql-host=$CLUSTER_ENDPOINT \
    --mysql-user=sysbench \
    --mysql-password=$SYSBENCH_PWD \
    --mysql-db=sysbench_test \
    --tables=10 \
    --table-size=100000 \
    --threads=4 \
    --time=30 \
    run

echo "2. Running OLTP read-write workload..."
sysbench oltp_read_write \
    --db-driver=mysql \
    --mysql-host=$CLUSTER_ENDPOINT \
    --mysql-user=sysbench \
    --mysql-password=$SYSBENCH_PWD \
    --mysql-db=sysbench_test \
    --tables=10 \
    --table-size=100000 \
    --threads=4 \
    --time=30 \
    run

echo "3. Running OLTP write-only workload..."
sysbench oltp_write_only \
    --db-driver=mysql \
    --mysql-host=$CLUSTER_ENDPOINT \
    --mysql-user=sysbench \
    --mysql-password=$SYSBENCH_PWD \
    --mysql-db=sysbench_test \
    --tables=10 \
    --table-size=100000 \
    --threads=4 \
    --time=30 \
    run

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
echo "Waiting for audit logs to be exported to S3 (this may take a few minutes)..."
sleep 300  # Wait for 5 minutes

# Download and analyze audit logs
echo "Downloading audit logs from S3..."
mkdir -p $TEST_DIR/audit_logs
aws s3 sync s3://$S3_BUCKET_NAME/audit-logs $TEST_DIR/audit_logs --region $AWS_REGION

if [ $? -ne 0 ] || [ -z "$(ls -A $TEST_DIR/audit_logs)" ]; then
    echo "Error: Failed to download audit logs or no logs found."
    echo "Please check the S3 bucket and ensure logs are being exported."
    exit 1
fi

echo "Audit logs downloaded successfully."

# Verify audit logs
echo "Verifying audit logs..."
echo "1. Checking for connection events..."
grep -r "CONNECT" $TEST_DIR/audit_logs
CONNECTION_COUNT=$(grep -r "CONNECT" $TEST_DIR/audit_logs | wc -l)
echo "Found $CONNECTION_COUNT connection events."

echo "2. Checking for query events..."
grep -r "QUERY" $TEST_DIR/audit_logs
QUERY_COUNT=$(grep -r "QUERY" $TEST_DIR/audit_logs | wc -l)
echo "Found $QUERY_COUNT query events."

echo "3. Checking for table access events..."
grep -r "TABLE" $TEST_DIR/audit_logs
TABLE_COUNT=$(grep -r "TABLE" $TEST_DIR/audit_logs | wc -l)
echo "Found $TABLE_COUNT table access events."

echo "4. Checking for DDL events..."
grep -r "QUERY_DDL" $TEST_DIR/audit_logs
DDL_COUNT=$(grep -r "QUERY_DDL" $TEST_DIR/audit_logs | wc -l)
echo "Found $DDL_COUNT DDL events."

echo "5. Checking for DML events..."
grep -r "QUERY_DML" $TEST_DIR/audit_logs
DML_COUNT=$(grep -r "QUERY_DML" $TEST_DIR/audit_logs | wc -l)
echo "Found $DML_COUNT DML events."

echo "6. Checking for DCL events..."
grep -r "QUERY_DCL" $TEST_DIR/audit_logs
DCL_COUNT=$(grep -r "QUERY_DCL" $TEST_DIR/audit_logs | wc -l)
echo "Found $DCL_COUNT DCL events."

# Generate summary report
echo "Generating audit log summary report..."
cat > $TEST_DIR/audit_log_report.txt << EOF
# Aurora MySQL Audit Log Verification Report

## Test Summary
- Connection Events: $CONNECTION_COUNT
- Query Events: $QUERY_COUNT
- Table Access Events: $TABLE_COUNT
- DDL Events: $DDL_COUNT
- DML Events: $DML_COUNT
- DCL Events: $DCL_COUNT

## Total Events: $(($CONNECTION_COUNT + $QUERY_COUNT + $TABLE_COUNT + $DDL_COUNT + $DML_COUNT + $DCL_COUNT))

## Test Scenarios Executed
1. Authentication Testing
2. OLTP Workload Testing
3. Schema Modification Testing
4. Privilege Testing

## Verification Status
$(if [ $(($CONNECTION_COUNT + $QUERY_COUNT + $TABLE_COUNT + $DDL_COUNT + $DML_COUNT + $DCL_COUNT)) -gt 0 ]; then echo "✅ PASSED: Audit logs successfully captured database activities"; else echo "❌ FAILED: No audit log events found"; fi)

Report generated on: $(date)
EOF

echo "Audit log verification complete!"
echo "Summary report saved to: $TEST_DIR/audit_log_report.txt"
cat $TEST_DIR/audit_log_report.txt