# Testing Lambda Functions Locally on EC2

This guide explains how to test the Aurora Audit Log Backup Lambda functions locally on the EC2 instance created by the lab.

## Prerequisites

1. SSH access to the EC2 instance
2. AWS CLI configured with appropriate permissions
3. Go installed on the EC2 instance (should be pre-installed)

## SSH into the EC2 Instance

First, get the EC2 instance's public IP address from the Pulumi output:

```bash
cd infrastructure/aurora-log-backup-lab-stack
pulumi stack output ec2PublicIp
```

Then connect using SSH:

```bash
ssh -i "keypair-sandbox0-sin-mymac.pem" ec2-user@<EC2_PUBLIC_IP>
```

## Testing Scripts

We've provided three testing scripts for each Lambda function:

1. `test-dbscanner.sh` - Tests the DB Scanner Lambda function
2. `test-logdetector.sh` - Tests the Log Detector Lambda function
3. `test-logdownloader.sh` - Tests the Log Downloader Lambda function

### Make the Scripts Executable

```bash
chmod +x test-dbscanner.sh test-logdetector.sh test-logdownloader.sh
```

## Testing the DB Scanner Lambda

The DB Scanner Lambda function scans for Aurora MySQL DB instances and sends their IDs to an SQS queue.

```bash
./test-dbscanner.sh
```

This script will:
1. Get the SQS queue URL
2. Purge the queue to start fresh
3. Build the DB scanner Lambda function
4. Run the Lambda function with an empty test event
5. Check the SQS queue for messages

## Testing the Log Detector Lambda

The Log Detector Lambda function receives DB instance IDs from the SQS queue, gets the log files for each instance, and stores their metadata in DynamoDB.

```bash
./test-logdetector.sh
```

This script will:
1. Get the DynamoDB table name and SQS queue URL
2. Build the Log Detector Lambda function
3. Send a test message to the SQS queue with a DB instance ID
4. Run the Lambda function with a test SQS event
5. Check the DynamoDB table for log file records

## Testing the Log Downloader Lambda

The Log Downloader Lambda function is triggered by DynamoDB stream events when log file records are created or updated. It downloads the log files and uploads them to S3.

```bash
./test-logdownloader.sh
```

This script will:
1. Get the DynamoDB table name and S3 bucket name
2. Build the Log Downloader Lambda function
3. Create a test DynamoDB stream event
4. Run the Lambda function with the test event
5. Check if the log file was uploaded to S3
6. Check if the DynamoDB record was updated with a LastBackup timestamp

## Troubleshooting

If you encounter any issues:

1. Check the Lambda function logs for errors
2. Verify that the environment variables are set correctly
3. Ensure that the AWS CLI has the necessary permissions
4. Check the AWS resources (SQS, DynamoDB, S3) to ensure they exist and are accessible

## Advanced Testing

For more advanced testing, you can modify the test events to simulate different scenarios:

- For DB Scanner: Test with different EventBridge events
- For Log Detector: Test with multiple SQS messages
- For Log Downloader: Test with different DynamoDB stream events (INSERT, MODIFY)

You can also use the AWS Lambda Local tool for more comprehensive testing:

```bash
# Install AWS Lambda Local
npm install -g aws-lambda-local

# Run the Lambda function with AWS Lambda Local
aws-lambda-local -l <lambda-binary> -h Handler -e test-event.json -t 30
```

## End-to-End Testing

To test the entire workflow:

1. Run the DB Scanner Lambda to find Aurora MySQL instances
2. Run the Log Detector Lambda to find log files
3. Run the Log Downloader Lambda to download and upload log files to S3

This simulates the complete audit log backup process.