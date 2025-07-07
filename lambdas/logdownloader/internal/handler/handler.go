package handler

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-xray-sdk-go/xray"
	"go.uber.org/zap"

	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdownloader/internal/service"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdownloader/pkg/errors"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdownloader/pkg/logger"
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

// Handle processes DynamoDB stream events
func (h *Handler) Handle(ctx context.Context, event events.DynamoDBEvent) error {
	ctx, segment := xray.BeginSegment(ctx, "logdownloader.Handler.Handle")
	defer segment.Close(nil)

	// Add X-Ray trace ID to logs
	h.logger = logger.WithTraceID(ctx, h.logger)
	h.logger.Info("Starting Log File Downloader Lambda")
	h.logger.Debugw("Received DynamoDB event", "recordCount", len(event.Records))

	// Process each DynamoDB stream record
	for _, record := range event.Records {
		// Create subsegment for processing each record
		ctx, recordSubsegment := xray.BeginSubsegment(ctx, "ProcessDynamoDBRecord")

		// Add annotation for the event name
		xray.AddAnnotation(ctx, "event_name", record.EventName)
		h.logger.Debugw("Processing DynamoDB record",
			"eventName", record.EventName,
			"eventID", record.EventID)

		// Skip records that are not INSERT or MODIFY
		if record.EventName != "INSERT" && record.EventName != "MODIFY" {
			h.logger.Debugw("Skipping record with event type", "eventName", record.EventName)
			recordSubsegment.Close(nil)
			continue
		}

		// Convert DynamoDB attribute values to Go types
		newImage, err := convertDynamoDBAttributeValues(ctx, record.Change.NewImage)
		if err != nil {
			h.logger.Errorw("Error converting new image", "error", err)
			recordSubsegment.AddError(err)
			recordSubsegment.Close(err)
			continue
		}

		var oldImage map[string]interface{}
		if record.EventName == "MODIFY" {
			oldImage, err = convertDynamoDBAttributeValues(ctx, record.Change.OldImage)
			if err != nil {
				h.logger.Errorw("Error converting old image", "error", err)
				recordSubsegment.AddError(err)
				recordSubsegment.Close(err)
				continue
			}
		}

		// Extract DB instance ID and log file name
		dbInstanceID, ok := newImage["DBInstanceIdentifier"].(string)
		if !ok {
			err := errors.Wrap(errors.ErrInvalidInput, "missing or invalid DBInstanceIdentifier")
			h.logger.Errorw("Invalid record", "error", err)
			recordSubsegment.AddError(err)
			recordSubsegment.Close(err)
			continue
		}

		logFileName, ok := newImage["LogFileName"].(string)
		if !ok {
			err := errors.Wrap(errors.ErrInvalidInput, "missing or invalid LogFileName")
			h.logger.Errorw("Invalid record", "error", err)
			recordSubsegment.AddError(err)
			recordSubsegment.Close(err)
			continue
		}

		// Process the log file
		err = h.service.ProcessLogFile(ctx, dbInstanceID, logFileName, oldImage, newImage)
		if err != nil {
			h.logger.Errorw("Error processing log file",
				"dbInstanceID", dbInstanceID,
				"logFileName", logFileName,
				"error", err)
			recordSubsegment.AddError(err)
			recordSubsegment.Close(err)
			continue
		}

		recordSubsegment.Close(nil)
	}

	h.logger.Info("Log File Downloader Lambda completed successfully")
	return nil
}

// convertDynamoDBAttributeValues converts DynamoDB attribute values to Go types
func convertDynamoDBAttributeValues(ctx context.Context, attributes map[string]events.DynamoDBAttributeValue) (map[string]interface{}, error) {
	// Create subsegment for X-Ray tracking
	_, subsegment := xray.BeginSubsegment(ctx, "convertDynamoDBAttributeValues")
	defer subsegment.Close(nil)

	// Convert to JSON and back to handle the conversion
	jsonBytes, err := json.Marshal(attributes)
	if err != nil {
		subsegment.AddError(err)
		return nil, errors.Wrap(err, "failed to marshal DynamoDB attributes")
	}

	var result map[string]interface{}
	err = json.Unmarshal(jsonBytes, &result)
	if err != nil {
		subsegment.AddError(err)
		return nil, errors.Wrap(err, "failed to unmarshal DynamoDB attributes")
	}

	// Process the map to extract values from DynamoDB format
	processed := make(map[string]interface{})
	for k, v := range result {
		processed[k] = extractValue(v)
	}

	return processed, nil
}

// extractValue extracts the actual value from a DynamoDB attribute value
func extractValue(v interface{}) interface{} {
	// Check if the value is a map
	if m, ok := v.(map[string]interface{}); ok {
		// Check for DynamoDB types
		if s, ok := m["S"].(string); ok {
			return s
		} else if n, ok := m["N"].(string); ok {
			return n
		} else if b, ok := m["BOOL"].(bool); ok {
			return b
		} else if bs, ok := m["BS"].([]interface{}); ok {
			return bs
		} else if ns, ok := m["NS"].([]interface{}); ok {
			return ns
		} else if ss, ok := m["SS"].([]interface{}); ok {
			return ss
		} else if l, ok := m["L"].([]interface{}); ok {
			result := make([]interface{}, len(l))
			for i, item := range l {
				result[i] = extractValue(item)
			}
			return result
		} else if mv, ok := m["M"].(map[string]interface{}); ok {
			result := make(map[string]interface{})
			for k, item := range mv {
				result[k] = extractValue(item)
			}
			return result
		}
	}

	// Return the value as is if it's not a DynamoDB type
	return v
}
