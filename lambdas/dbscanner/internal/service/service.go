package service

import (
	"context"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-xray-sdk-go/xray"
	"go.uber.org/zap"

	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/dbscanner/internal/repository"
)

// Service defines the interface for the dbscanner service
type Service interface {
	ScanAndQueueDBInstances(ctx context.Context, queueURL string) (int, error)
}

// service implements Service
type service struct {
	rdsRepo repository.RDSRepository
	sqsRepo repository.SQSRepository
	logger  *zap.SugaredLogger
}

// New creates a new service
func New(rdsRepo repository.RDSRepository, sqsRepo repository.SQSRepository, logger *zap.SugaredLogger) Service {
	return &service{
		rdsRepo: rdsRepo,
		sqsRepo: sqsRepo,
		logger:  logger,
	}
}

// ScanAndQueueDBInstances scans for Aurora MySQL instances and sends them to SQS
func (s *service) ScanAndQueueDBInstances(ctx context.Context, queueURL string) (int, error) {
	// Get all DB instances with X-Ray subsegment
	ctx, subsegment := xray.BeginSubsegment(ctx, "getDBInstances")
	instances, err := s.rdsRepo.GetDBInstances(ctx)
	if err != nil {
		subsegment.AddError(err)
		s.logger.Errorw("Error getting DB instances", "error", err)
		subsegment.Close(err)
		return 0, err
	}
	subsegment.Close(nil)

	// Filter for Aurora MySQL instances with X-Ray subsegment
	ctx, filterSubsegment := xray.BeginSubsegment(ctx, "filterAuroraInstances")
	auroraInstances := s.filterAuroraInstances(instances)
	s.logger.Infow("Found Aurora MySQL instances", "count", len(auroraInstances))

	// Add annotation for instance count
	xray.AddAnnotation(ctx, "aurora_instances_count", len(auroraInstances))
	filterSubsegment.Close(nil)

	// Send each instance ID to SQS with X-Ray subsegment
	ctx, sqsSubsegment := xray.BeginSubsegment(ctx, "sendToSQS")
	var sendErrors int
	for _, instance := range auroraInstances {
		err := s.sqsRepo.SendMessage(ctx, queueURL, *instance.DBInstanceIdentifier)
		if err != nil {
			sendErrors++
			s.logger.Errorw("Error sending instance ID to SQS",
				"instanceID", *instance.DBInstanceIdentifier,
				"error", err)
			// Continue with other instances even if one fails
			continue
		}
	}

	// Add metadata about SQS operations
	xray.AddMetadata(ctx, "sqs_send_errors", sendErrors)
	sqsSubsegment.Close(nil)

	return len(auroraInstances), nil
}

// filterAuroraInstances filters DB instances based on engine type and blacklist
func (s *service) filterAuroraInstances(instances []types.DBInstance) []types.DBInstance {
	s.logger.Debug("Filtering DB instances")

	// Get allowed engine types from environment variable
	allowedEngines := os.Getenv("INSTANCE_ENGINE")
	if allowedEngines == "" {
		// Default to Aurora MySQL if not specified
		allowedEngines = "aurora-mysql,aurora"
	}
	engineList := strings.Split(allowedEngines, ",")

	// Create a map for faster lookups
	engineMap := make(map[string]bool)
	for _, engine := range engineList {
		engineMap[strings.TrimSpace(engine)] = true
	}

	// Get blacklisted instance IDs from environment variable
	blacklist := os.Getenv("BLACK_LIST")
	blacklistMap := make(map[string]bool)
	if blacklist != "" {
		blacklistItems := strings.Split(blacklist, ",")
		for _, item := range blacklistItems {
			blacklistMap[strings.TrimSpace(item)] = true
		}
	}

	var filteredInstances []types.DBInstance
	for _, instance := range instances {
		// Skip if instance is in blacklist
		if instance.DBInstanceIdentifier != nil && blacklistMap[*instance.DBInstanceIdentifier] {
			s.logger.Infow("Skipping blacklisted instance", "instanceID", *instance.DBInstanceIdentifier)
			continue
		}

		// Check if instance engine is in the allowed list
		if instance.Engine != nil && engineMap[*instance.Engine] {
			filteredInstances = append(filteredInstances, instance)
		}
	}

	s.logger.Infow("Filtered instances",
		"totalInstances", len(instances),
		"filteredInstances", len(filteredInstances),
		"allowedEngines", allowedEngines,
		"blacklistCount", len(blacklistMap))

	return filteredInstances
}
