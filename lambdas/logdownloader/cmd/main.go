package main

import (
	"context"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-xray-sdk-go/xray"

	appconfig "github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdownloader/internal/config"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdownloader/internal/handler"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdownloader/internal/repository"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdownloader/internal/service"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdownloader/pkg/logger"
)

// Global variables for reuse across invocations
var (
	lambdaHandler *handler.Handler
)

// init function runs during cold start
func init() {
	// Configure X-Ray logging
	xray.Configure(xray.Config{
		LogLevel: os.Getenv("LOG_LEVEL"),
	})
}

func main() {
	// Initialize logger
	log, err := logger.NewLogger()
	if err != nil {
		os.Exit(1)
	}
	defer log.Sync()

	log.Info("Initializing Log Downloader Lambda")

	// Load configuration
	cfg, err := appconfig.Load()
	if err != nil {
		log.Fatalw("Failed to load configuration", "error", err)
	}

	log.Debugw("Configuration loaded", "config", cfg.String())

	// Load AWS configuration
	ctx := context.Background()
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalw("Failed to load AWS config", "error", err)
	}

	// Initialize AWS clients with X-Ray instrumentation
	s3Client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		if httpClient, ok := options.HTTPClient.(*http.Client); ok {
			options.HTTPClient = xray.Client(httpClient)
		}
	})

	dynamoClient := dynamodb.NewFromConfig(awsCfg, func(options *dynamodb.Options) {
		if httpClient, ok := options.HTTPClient.(*http.Client); ok {
			options.HTTPClient = xray.Client(httpClient)
		}
	})

	// Initialize repository
	repo := repository.NewRepository(
		dynamoClient,
		s3Client,
		cfg.DynamoDBTableName,
		cfg.S3BucketName,
		log,
	)

	// Initialize service
	svc := service.NewService(
		repo,
		cfg,
		awsCfg,
		log,
	)

	// Initialize handler
	lambdaHandler = handler.New(svc, log)

	// Start Lambda handler
	lambda.Start(lambdaHandler.Handle)
}
