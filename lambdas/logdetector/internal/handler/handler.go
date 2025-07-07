package handler

import (
	"context"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-xray-sdk-go/xray"
	"go.uber.org/zap"

	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdetector/internal/service"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdetector/pkg/logger"
)

// Handler handles Lambda events
type Handler struct {
	service service.Service
	logger  *zap.SugaredLogger
}

// New creates a new handler
func New(svc service.Service, logger *zap.SugaredLogger) *Handler {
	return &Handler{
		service: svc,
		logger:  logger,
	}
}

// Handle processes SQS events
func (h *Handler) Handle(ctx context.Context, sqsEvent events.SQSEvent) error {
	ctx, segment := xray.BeginSegment(ctx, "logdetector.Handler.Handle")
	defer segment.Close(nil)

	// Add X-Ray trace ID to logs
	h.logger = logger.WithTraceID(ctx, h.logger)
	h.logger.Info("Starting Log File Detector Lambda")
	h.logger.Debugw("Received SQS event", "recordCount", len(sqsEvent.Records))

	// Process each SQS message
	for _, message := range sqsEvent.Records {
		// Create subsegment for processing each message
		ctx, messageSubsegment := xray.BeginSubsegment(ctx, "ProcessSQSMessage")

		// The message body contains the DB instance ID
		dbInstanceID := message.Body
		h.logger.Infow("Processing DB instance", "instanceID", dbInstanceID)

		// Add annotation for the instance ID
		xray.AddAnnotation(ctx, "db_instance_id", dbInstanceID)

		// Process the DB instance
		err := h.service.ProcessDBInstance(ctx, dbInstanceID)
		if err != nil {
			h.logger.Errorw("Error processing DB instance",
				"instanceID", dbInstanceID,
				"error", err)
			messageSubsegment.AddError(err)
			messageSubsegment.Close(err)
			// Continue processing other messages even if one fails
			continue
		}

		messageSubsegment.Close(nil)
	}

	h.logger.Info("Log File Detector Lambda completed successfully")
	return nil
}
