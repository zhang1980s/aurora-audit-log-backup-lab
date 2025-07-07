package main

import (
	"context"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-xray-sdk-go/xray"

	appconfig "github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdetector/internal/config"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdetector/internal/handler"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdetector/internal/repository"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdetector/internal/service"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdetector/pkg/logger"
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

	log.Info("Initializing Log Detector Lambda")

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
	rdsClient := rds.NewFromConfig(awsCfg, func(options *rds.Options) {
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
		rdsClient,
		dynamoClient,
		cfg.DynamoDBTableName,
		log,
	)

	// Initialize service
	svc := service.NewService(
		repo,
		cfg,
		log,
	)

	// Initialize handler
	lambdaHandler = handler.New(svc, log)

	// Start Lambda handler
	lambda.Start(lambdaHandler.Handle)
}
