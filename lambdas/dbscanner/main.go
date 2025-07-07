package main

import (
	"context"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-xray-sdk-go/xray"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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

// initLogger initializes the Zap logger with the appropriate log level
func initLogger() (*zap.SugaredLogger, error) {
	// Get log level from environment variable, default to "error"
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "error"
	}

	// Parse log level
	var level zapcore.Level
	err := level.UnmarshalText([]byte(logLevel))
	if err != nil {
		// If parsing fails, default to error level
		level = zap.ErrorLevel
	}

	// Create logger configuration
	config := zap.Config{
		Level:            zap.NewAtomicLevelAt(level),
		Development:      false,
		Encoding:         "json",
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	// Build logger
	logger, err := config.Build()
	if err != nil {
		return nil, err
	}

	// Return sugared logger for easier use
	return logger.Sugar(), nil
}

// init configures X-Ray
func init() {
	// Configure X-Ray
	xray.Configure(xray.Config{
		LogLevel: os.Getenv("LOG_LEVEL"),
	})
}

// Handler is the Lambda function handler
func Handler(ctx context.Context, event Event) (Response, error) {
	// Initialize logger
	logger, err := initLogger()
	if err != nil {
		// If logger initialization fails, fall back to basic logging
		return Response{}, err
	}
	defer logger.Sync()

	logger.Info("Starting DB Instance Scanner Lambda")

	// Get SQS queue URL from environment variable
	queueURL := os.Getenv("SQS_QUEUE_URL")
	if queueURL == "" {
		logger.Error("SQS_QUEUE_URL environment variable not set")
		return Response{}, nil
	}

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Errorw("Error loading AWS config", "error", err)
		return Response{}, err
	}

	// Create AWS SDK clients
	// Note: In AWS SDK v2, X-Ray tracing works through context propagation
	// We don't need to wrap the HTTP client like in SDK v1
	rdsClient := rds.NewFromConfig(cfg)
	sqsClient := sqs.NewFromConfig(cfg)

	// Add X-Ray trace ID to logs if available
	traceID := xray.TraceID(ctx)
	if traceID != "" {
		logger = logger.With(zap.String("xray_trace_id", traceID))
	}

	// Get all DB instances with X-Ray subsegment
	ctx, subsegment := xray.BeginSubsegment(ctx, "getDBInstances")
	instances, err := getDBInstances(ctx, rdsClient, logger)
	if err != nil {
		subsegment.AddError(err)
		logger.Errorw("Error getting DB instances", "error", err)
		subsegment.Close(err)
		return Response{}, err
	}
	subsegment.Close(nil)

	// Filter for Aurora MySQL instances with X-Ray subsegment
	ctx, filterSubsegment := xray.BeginSubsegment(ctx, "filterAuroraInstances")
	auroraInstances := filterAuroraInstances(instances, logger)
	logger.Infow("Found Aurora MySQL instances", "count", len(auroraInstances))

	// Add annotation for instance count
	xray.AddAnnotation(ctx, "aurora_instances_count", len(auroraInstances))
	filterSubsegment.Close(nil)

	// Send each instance ID to SQS with X-Ray subsegment
	ctx, sqsSubsegment := xray.BeginSubsegment(ctx, "sendToSQS")
	var sendErrors int
	for _, instance := range auroraInstances {
		err := sendToSQS(ctx, sqsClient, queueURL, *instance.DBInstanceIdentifier, logger)
		if err != nil {
			sendErrors++
			logger.Errorw("Error sending instance ID to SQS",
				"instanceID", *instance.DBInstanceIdentifier,
				"error", err)
			// Continue with other instances even if one fails
			continue
		}
	}

	// Add metadata about SQS operations
	xray.AddMetadata(ctx, "sqs_send_errors", sendErrors)
	sqsSubsegment.Close(nil)

	return Response{
		InstancesFound: len(auroraInstances),
		QueueURL:       queueURL,
		Message:        "Successfully sent Aurora MySQL instance IDs to SQS",
	}, nil
}

// getDBInstances gets all DB instances in the current region
func getDBInstances(ctx context.Context, client *rds.Client, logger *zap.SugaredLogger) ([]types.DBInstance, error) {
	logger.Debug("Getting all DB instances")

	var instances []types.DBInstance
	var marker *string
	var pageCount int

	// Use pagination to get all instances
	for {
		pageCount++
		// Create subsegment for each pagination request
		ctx, apiSubsegment := xray.BeginSubsegment(ctx, "DescribeDBInstances-Page"+string(rune(pageCount)))

		resp, err := client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
			Marker: marker,
		})
		if err != nil {
			apiSubsegment.AddError(err)
			apiSubsegment.Close(err)
			return nil, err
		}

		instances = append(instances, resp.DBInstances...)

		// Add metadata about the pagination
		xray.AddMetadata(ctx, "page_instance_count", len(resp.DBInstances))
		apiSubsegment.Close(nil)

		// Check if there are more pages
		if resp.Marker == nil {
			break
		}
		marker = resp.Marker
	}

	logger.Debugw("Found DB instances", "count", len(instances))
	// Add annotation for total instance count
	xray.AddAnnotation(ctx, "total_db_instances", len(instances))
	return instances, nil
}

// filterAuroraInstances filters for Aurora MySQL instances
func filterAuroraInstances(instances []types.DBInstance, logger *zap.SugaredLogger) []types.DBInstance {
	logger.Debug("Filtering for Aurora MySQL instances")

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
func sendToSQS(ctx context.Context, client *sqs.Client, queueURL string, instanceID string, logger *zap.SugaredLogger) error {
	logger.Debugw("Sending instance ID to SQS", "instanceID", instanceID)

	// Create subsegment for SQS operation
	ctx, sqsOpSubsegment := xray.BeginSubsegment(ctx, "SendMessage-"+instanceID)

	// Add annotation for the instance ID
	xray.AddAnnotation(ctx, "db_instance_id", instanceID)

	_, err := client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(instanceID),
	})

	if err != nil {
		sqsOpSubsegment.AddError(err)
		sqsOpSubsegment.Close(err)
		return err
	}

	sqsOpSubsegment.Close(nil)
	return nil
}

func main() {
	lambda.Start(Handler)
}
