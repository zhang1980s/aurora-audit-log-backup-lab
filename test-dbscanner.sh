#!/bin/bash

# Set up environment
echo "Setting up environment..."
export AWS_REGION=$(curl -s http://169.254.169.254/latest/meta-data/placement/region)

# Get the SQS queue URL
echo "Getting SQS queue URL..."
export SQS_QUEUE_URL=$(aws sqs list-queues --queue-name-prefix aurora-db-instances --query "QueueUrls[0]" --output text)
echo "SQS Queue URL: $SQS_QUEUE_URL"

# Purge the queue to start fresh
echo "Purging SQS queue..."
aws sqs purge-queue --queue-url $SQS_QUEUE_URL

# Build the Lambda function
echo "Building DB scanner Lambda function..."
cd lambdas/dbscanner
go build -o dbscanner main.go

# Create test event
echo "Creating test event..."
echo '{}' > test-event.json

# Run the Lambda function
echo "Running DB scanner Lambda function..."
./dbscanner test-event.json

# Wait for a moment
echo "Waiting for messages to be processed..."
sleep 5

# Check the SQS queue
echo "Checking SQS queue for messages..."
aws sqs get-queue-attributes --queue-url $SQS_QUEUE_URL --attribute-names ApproximateNumberOfMessages

# Receive messages from the queue
echo "Receiving messages from the queue..."
aws sqs receive-message --queue-url $SQS_QUEUE_URL --max-number-of-messages 10 --wait-time-seconds 5

echo "Test completed!"