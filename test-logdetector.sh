#!/bin/bash

# Set up environment
echo "Setting up environment..."
export AWS_REGION=$(curl -s http://169.254.169.254/latest/meta-data/placement/region)

# Get the DynamoDB table name
echo "Getting DynamoDB table name..."
export DYNAMODB_TABLE_NAME=$(aws dynamodb list-tables --query "TableNames[?contains(@, 'aurora-log-files')]|[0]" --output text)
echo "DynamoDB Table Name: $DYNAMODB_TABLE_NAME"

# Get the SQS queue URL
echo "Getting SQS queue URL..."
export SQS_QUEUE_URL=$(aws sqs list-queues --queue-name-prefix aurora-db-instances --query "QueueUrls[0]" --output text)
echo "SQS Queue URL: $SQS_QUEUE_URL"

# Build the Lambda function
echo "Building Log Detector Lambda function..."
cd lambdas/logdetector
go build -o logdetector main.go

# Create a test message for the SQS queue (DB instance ID)
echo "Creating test message..."
DB_INSTANCE_ID=$(aws rds describe-db-instances --query "DBInstances[?Engine=='aurora-mysql'].DBInstanceIdentifier|[0]" --output text)
echo "Using DB Instance ID: $DB_INSTANCE_ID"

# Send a test message to the SQS queue
echo "Sending test message to SQS queue..."
aws sqs send-message --queue-url $SQS_QUEUE_URL --message-body "$DB_INSTANCE_ID"

# Create test event with SQS message
echo "Creating test event..."
cat > test-event.json << EOF
{
  "Records": [
    {
      "messageId": "test-message-id",
      "receiptHandle": "test-receipt-handle",
      "body": "$DB_INSTANCE_ID",
      "attributes": {
        "ApproximateReceiveCount": "1",
        "SentTimestamp": "$(date +%s)000",
        "SenderId": "test-sender-id",
        "ApproximateFirstReceiveTimestamp": "$(date +%s)000"
      },
      "messageAttributes": {},
      "md5OfBody": "test-md5",
      "eventSource": "aws:sqs",
      "eventSourceARN": "arn:aws:sqs:$AWS_REGION:000000000000:aurora-db-instances",
      "awsRegion": "$AWS_REGION"
    }
  ]
}
EOF

# Run the Lambda function
echo "Running Log Detector Lambda function..."
./logdetector test-event.json

# Wait for a moment
echo "Waiting for log files to be processed..."
sleep 5

# Check the DynamoDB table for log file records
echo "Checking DynamoDB table for log file records..."
aws dynamodb scan --table-name $DYNAMODB_TABLE_NAME --select "COUNT"

# Show a sample of the records
echo "Showing a sample of the records..."
aws dynamodb scan --table-name $DYNAMODB_TABLE_NAME --limit 5

echo "Test completed!"