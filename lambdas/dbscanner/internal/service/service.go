package service

import (
	"context"

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

// filterAuroraInstances filters for Aurora MySQL instances
func (s *service) filterAuroraInstances(instances []types.DBInstance) []types.DBInstance {
	s.logger.Debug("Filtering for Aurora MySQL instances")

	var auroraInstances []types.DBInstance
	for _, instance := range instances {
		// Check if it's an Aurora MySQL instance
		if instance.Engine != nil && (*instance.Engine == "aurora-mysql" || *instance.Engine == "aurora") {
			auroraInstances = append(auroraInstances, instance)
		}
	}

	return auroraInstances
}
