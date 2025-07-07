package main

import (
	"context"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-xray-sdk-go/xray"

	appconfig "github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/dbscanner/config"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/dbscanner/internal/handler"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/dbscanner/internal/repository"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/dbscanner/internal/service"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/dbscanner/pkg/logger"
)

// Global variables for reuse across invocations
var (
	lambdaHandler *handler.Handler
)

// init function runs during cold start
func init() {
	// Configure X-Ray
	xray.Configure(xray.Config{
		LogLevel: os.Getenv("LOG_LEVEL"),
	})

	// Load configuration
	cfg, err := appconfig.Load()
	if err != nil {
		// If configuration loading fails, log error and exit
		os.Exit(1)
	}

	// Initialize logger
	log, err := logger.NewLogger(cfg.LogLevel)
	if err != nil {
		// If logger initialization fails, exit
		os.Exit(1)
	}

	// Load AWS configuration
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalw("Failed to load AWS config", "error", err)
	}

	// Initialize AWS clients
	rdsClient := rds.NewFromConfig(awsCfg)
	sqsClient := sqs.NewFromConfig(awsCfg)

	// Initialize repository layer
	rdsRepo := repository.NewRDSRepository(rdsClient, log)
	sqsRepo := repository.NewSQSRepository(sqsClient, log)

	// Initialize service layer
	svc := service.New(rdsRepo, sqsRepo, log)

	// Initialize handler
	lambdaHandler = handler.New(svc, log, cfg.SQSQueueURL)
}

func main() {
	lambda.Start(lambdaHandler.Handle)
}
