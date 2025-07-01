#!/bin/bash

# Set up environment
echo "Setting up environment..."
export AWS_REGION=$(curl -s http://169.254.169.254/latest/meta-data/placement/region)

# Get the DynamoDB table name
echo "Getting DynamoDB table name..."
export DYNAMODB_TABLE_NAME=$(aws dynamodb list-tables --query "TableNames[?contains(@, 'aurora-log-files')]|[0]" --output text)
echo "DynamoDB Table Name: $DYNAMODB_TABLE_NAME"

# Get the S3 bucket name
echo "Getting S3 bucket name..."
export S3_BUCKET_NAME=$(aws s3api list-buckets --query "Buckets[?contains(Name, 'aurora-log-backup')].Name|[0]" --output text)
echo "S3 Bucket Name: $S3_BUCKET_NAME"

# Set S3 prefix
export S3_PREFIX="logs"
echo "S3 Prefix: $S3_PREFIX"

# Build the Lambda function
echo "Building Log Downloader Lambda function..."
cd lambdas/logdownloader
go build -o logdownloader main.go

# Get a DB instance ID
DB_INSTANCE_ID=$(aws rds describe-db-instances --query "DBInstances[?Engine=='aurora-mysql'].DBInstanceIdentifier|[0]" --output text)
echo "Using DB Instance ID: $DB_INSTANCE_ID"

# Get a log file name
LOG_FILE_NAME="audit/server_audit.log"
echo "Using Log File Name: $LOG_FILE_NAME"

# Create a test DynamoDB stream event
echo "Creating test event..."
CURRENT_TIME=$(date +%s)
cat > test-event.json << EOF
{
  "Records": [
    {
      "eventID": "test-event-id",
      "eventName": "INSERT",
      "eventVersion": "1.1",
      "eventSource": "aws:dynamodb",
      "awsRegion": "$AWS_REGION",
      "dynamodb": {
        "ApproximateCreationDateTime": $CURRENT_TIME,
        "Keys": {
          "DBInstanceIdentifier": {
            "S": "$DB_INSTANCE_ID"
          },
          "LogFileName": {
            "S": "$LOG_FILE_NAME"
          }
        },
        "NewImage": {
          "DBInstanceIdentifier": {
            "S": "$DB_INSTANCE_ID"
          },
          "LogFileName": {
            "S": "$LOG_FILE_NAME"
          },
          "Size": {
            "N": "1024"
          },
          "LastWritten": {
            "N": "$CURRENT_TIME"
          }
        },
        "SequenceNumber": "000000000000000000000",
        "SizeBytes": 100,
        "StreamViewType": "NEW_AND_OLD_IMAGES"
      },
      "eventSourceARN": "arn:aws:dynamodb:$AWS_REGION:000000000000:table/$DYNAMODB_TABLE_NAME/stream/$(date +%Y-%m-%dT%H:%M:%S)"
    }
  ]
}
EOF

# Run the Lambda function
echo "Running Log Downloader Lambda function..."
./logdownloader test-event.json

# Wait for a moment
echo "Waiting for log file to be downloaded and uploaded to S3..."
sleep 10

# Check if the log file was uploaded to S3
echo "Checking S3 bucket for uploaded log file..."
aws s3 ls s3://$S3_BUCKET_NAME/$S3_PREFIX/$DB_INSTANCE_ID/$LOG_FILE_NAME

# Check if the DynamoDB record was updated with LastBackup timestamp
echo "Checking DynamoDB record for LastBackup timestamp..."
aws dynamodb get-item \
  --table-name $DYNAMODB_TABLE_NAME \
  --key "{\"DBInstanceIdentifier\":{\"S\":\"$DB_INSTANCE_ID\"},\"LogFileName\":{\"S\":\"$LOG_FILE_NAME\"}}" \
  --projection-expression "LastBackup"

echo "Test completed!"