package main

import (
	"context"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// Event represents the input event for the Lambda function
type Event struct {
	// Empty for EventBridge scheduled events
}

// Response represents the output of the Lambda function
type Response struct {
	InstancesFound int    `json:"instancesFound"`
	QueueURL       string `json:"queueUrl"`
	Message        string `json:"message"`
}

// Handler is the Lambda function handler
func Handler(ctx context.Context, event Event) (Response, error) {
	// Initialize logger
	logger := log.New(os.Stdout, "", log.LstdFlags)
	logger.Println("Starting DB Instance Scanner Lambda")

	// Get SQS queue URL from environment variable
	queueURL := os.Getenv("SQS_QUEUE_URL")
	if queueURL == "" {
		logger.Println("Error: SQS_QUEUE_URL environment variable not set")
		return Response{}, nil
	}

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Printf("Error loading AWS config: %v\n", err)
		return Response{}, err
	}

	// Create RDS client
	rdsClient := rds.NewFromConfig(cfg)

	// Create SQS client
	sqsClient := sqs.NewFromConfig(cfg)

	// Get all DB instances
	instances, err := getDBInstances(ctx, rdsClient, logger)
	if err != nil {
		logger.Printf("Error getting DB instances: %v\n", err)
		return Response{}, err
	}

	// Filter for Aurora MySQL instances
	auroraInstances := filterAuroraInstances(instances, logger)
	logger.Printf("Found %d Aurora MySQL instances\n", len(auroraInstances))

	// Send each instance ID to SQS
	for _, instance := range auroraInstances {
		err := sendToSQS(ctx, sqsClient, queueURL, *instance.DBInstanceIdentifier, logger)
		if err != nil {
			logger.Printf("Error sending instance ID to SQS: %v\n", err)
			// Continue with other instances even if one fails
			continue
		}
	}

	return Response{
		InstancesFound: len(auroraInstances),
		QueueURL:       queueURL,
		Message:        "Successfully sent Aurora MySQL instance IDs to SQS",
	}, nil
}

// getDBInstances gets all DB instances in the current region
func getDBInstances(ctx context.Context, client *rds.Client, logger *log.Logger) ([]types.DBInstance, error) {
	logger.Println("Getting all DB instances")

	var instances []types.DBInstance
	var marker *string

	// Use pagination to get all instances
	for {
		resp, err := client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
			Marker: marker,
		})
		if err != nil {
			return nil, err
		}

		instances = append(instances, resp.DBInstances...)

		// Check if there are more pages
		if resp.Marker == nil {
			break
		}
		marker = resp.Marker
	}

	logger.Printf("Found %d DB instances total\n", len(instances))
	return instances, nil
}

// filterAuroraInstances filters for Aurora MySQL instances
func filterAuroraInstances(instances []types.DBInstance, logger *log.Logger) []types.DBInstance {
	logger.Println("Filtering for Aurora MySQL instances")

	var auroraInstances []types.DBInstance
	for _, instance := range instances {
		// Check if it's an Aurora MySQL instance
		if instance.Engine != nil && (*instance.Engine == "aurora-mysql" || *instance.Engine == "aurora") {
			auroraInstances = append(auroraInstances, instance)
		}
	}

	return auroraInstances
}

// sendToSQS sends a DB instance ID to the SQS queue
func sendToSQS(ctx context.Context, client *sqs.Client, queueURL string, instanceID string, logger *log.Logger) error {
	logger.Printf("Sending instance ID %s to SQS\n", instanceID)

	_, err := client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(instanceID),
	})

	return err
}

func main() {
	lambda.Start(Handler)
}
