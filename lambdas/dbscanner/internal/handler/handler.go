package handler

import (
	"context"

	"github.com/aws/aws-xray-sdk-go/xray"
	"go.uber.org/zap"

	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/dbscanner/internal/service"
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

// Handler handles Lambda events
type Handler struct {
	service  service.Service
	logger   *zap.SugaredLogger
	queueURL string
}

// New creates a new handler
func New(svc service.Service, logger *zap.SugaredLogger, queueURL string) *Handler {
	return &Handler{
		service:  svc,
		logger:   logger,
		queueURL: queueURL,
	}
}

// Handle handles Lambda events
func (h *Handler) Handle(ctx context.Context, event Event) (Response, error) {
	h.logger.Info("Starting DB Instance Scanner Lambda")

	// Add X-Ray trace ID to logs if available
	traceID := xray.TraceID(ctx)
	if traceID != "" {
		h.logger = h.logger.With(zap.String("xray_trace_id", traceID))
	}

	// Scan for Aurora MySQL instances and send them to SQS
	instancesFound, err := h.service.ScanAndQueueDBInstances(ctx, h.queueURL)
	if err != nil {
		h.logger.Errorw("Error scanning and queueing DB instances", "error", err)
		return Response{}, err
	}

	return Response{
		InstancesFound: instancesFound,
		QueueURL:       h.queueURL,
		Message:        "Successfully sent Aurora MySQL instance IDs to SQS",
	}, nil
}
