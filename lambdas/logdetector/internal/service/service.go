package service

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-xray-sdk-go/xray"
	"go.uber.org/zap"

	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdetector/internal/config"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdetector/internal/models"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdetector/internal/repository"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdetector/pkg/errors"
)

// Service defines the interface for business logic operations
type Service interface {
	ProcessDBInstance(ctx context.Context, dbInstanceID string) error
}

// LogDetectorService implements the Service interface
type LogDetectorService struct {
	repo   repository.Repository
	cfg    *config.Config
	logger *zap.SugaredLogger
}

// NewService creates a new service instance
func NewService(
	repo repository.Repository,
	cfg *config.Config,
	logger *zap.SugaredLogger,
) Service {
	return &LogDetectorService{
		repo:   repo,
		cfg:    cfg,
		logger: logger,
	}
}

// ProcessDBInstance processes a DB instance to detect and record log files
func (s *LogDetectorService) ProcessDBInstance(ctx context.Context, dbInstanceID string) error {
	ctx, subsegment := xray.BeginSubsegment(ctx, "service.ProcessDBInstance")
	defer subsegment.Close(nil)

	s.logger.Infow("Processing DB instance", "instanceID", dbInstanceID)

	// Add annotation for the instance ID
	xray.AddAnnotation(ctx, "db_instance_id", dbInstanceID)

	// Get log files for the DB instance
	ctx, logFilesSubsegment := xray.BeginSubsegment(ctx, "GetDBLogFiles")
	logFiles, err := s.repo.GetDBLogFiles(ctx, dbInstanceID)
	if err != nil {
		logFilesSubsegment.AddError(err)
		s.logger.Errorw("Error getting log files for instance",
			"instanceID", dbInstanceID,
			"error", err)
		logFilesSubsegment.Close(err)
		subsegment.AddError(err)
		return err
	}

	// Add metadata about log files
	xray.AddMetadata(ctx, "log_files_count", len(logFiles))
	logFilesSubsegment.Close(nil)

	// Process each log file
	var processedFiles, skippedFiles, errorFiles int
	for _, logFile := range logFiles {
		// Create subsegment for processing each log file
		ctx, fileSubsegment := xray.BeginSubsegment(ctx, "ProcessLogFile")

		// Check if the log file should be backed up based on configuration
		if logFile.LogFileName == nil || !s.cfg.ShouldBackupLog(*logFile.LogFileName) {
			skippedFiles++
			fileSubsegment.Close(nil)
			continue
		}

		// Add annotation for the log file name
		if logFile.LogFileName != nil {
			xray.AddAnnotation(ctx, "log_file_name", *logFile.LogFileName)
		}

		// Process the log file
		err := s.processLogFile(ctx, dbInstanceID, logFile)
		if err != nil {
			errorFiles++
			fileSubsegment.AddError(err)
			fileSubsegment.Close(err)
			continue
		}

		processedFiles++
		fileSubsegment.Close(nil)
	}

	// Add metadata about processing results
	xray.AddMetadata(ctx, "processed_files", processedFiles)
	xray.AddMetadata(ctx, "skipped_files", skippedFiles)
	xray.AddMetadata(ctx, "error_files", errorFiles)

	s.logger.Infow("Completed processing DB instance",
		"instanceID", dbInstanceID,
		"processedFiles", processedFiles,
		"skippedFiles", skippedFiles,
		"errorFiles", errorFiles)

	return nil
}

// processLogFile processes a single log file
func (s *LogDetectorService) processLogFile(ctx context.Context, dbInstanceID string, logFile types.DescribeDBLogFilesDetails) error {
	// Calculate expiration time (current time + TTL_DAYS)
	now := time.Now()
	expirationTime := now.AddDate(0, 0, s.cfg.TTLDays)
	expirationMillis := expirationTime.UnixMilli()
	humanReadableExpiration := expirationTime.Format(time.RFC3339)

	// Create a record for the log file
	record := models.LogFileRecord{
		DBInstanceIdentifier:    dbInstanceID,
		LogFileName:             *logFile.LogFileName,
		Size:                    0,                          // Default value
		LastWritten:             0,                          // Default value
		LastBackup:              models.CurrentTimeMillis(), // Current time
		HumanReadableLastBackup: models.CurrentTimeFormatted(),
		ExpirationTime:          expirationMillis, // Set TTL expiration time
		HumanReadableExpiration: humanReadableExpiration,
		ScanCount:               1, // Initialize scan count to 1 for new records
	}

	// Add TTL metadata
	xray.AddMetadata(ctx, "ttl_days", s.cfg.TTLDays)
	xray.AddMetadata(ctx, "expiration_millis", expirationMillis)
	xray.AddMetadata(ctx, "expiration_human", humanReadableExpiration)
	s.logger.Debugw("Setting TTL expiration time",
		"ttlDays", s.cfg.TTLDays,
		"expirationMillis", expirationMillis,
		"humanReadable", humanReadableExpiration)

	// Handle nullable Size field
	if logFile.Size != nil {
		record.Size = *logFile.Size
		xray.AddMetadata(ctx, "log_file_size", *logFile.Size)
	}

	// Handle nullable LastWritten field
	if logFile.LastWritten != nil {
		record.LastWritten = *logFile.LastWritten
		// Add human-readable timestamp
		record.HumanReadableLastWritten = models.FormatTime(*logFile.LastWritten)
	}

	// Check if the record already exists in DynamoDB
	ctx, getRecordSubsegment := xray.BeginSubsegment(ctx, "GetLogFileRecord")
	existingRecord, err := s.repo.GetLogFileRecord(ctx, dbInstanceID, *logFile.LogFileName)
	if err != nil {
		getRecordSubsegment.AddError(err)
		s.logger.Errorw("Error checking for existing record",
			"logFile", *logFile.LogFileName,
			"error", err)
		getRecordSubsegment.Close(err)
		return errors.Wrap(err, "failed to check for existing record")
	}
	getRecordSubsegment.Close(nil)

	if existingRecord == nil {
		// Record doesn't exist, create a new one
		ctx, createSubsegment := xray.BeginSubsegment(ctx, "CreateLogFileRecord")
		err = s.repo.CreateLogFileRecord(ctx, record)
		if err != nil {
			createSubsegment.AddError(err)
			s.logger.Errorw("Error creating record",
				"logFile", record.LogFileName,
				"error", err)
			createSubsegment.Close(err)
			return errors.Wrap(err, "failed to create record")
		}
		createSubsegment.Close(nil)
		xray.AddAnnotation(ctx, "operation", "create")
		s.logger.Infow("Created new record for log file",
			"logFile", record.LogFileName,
			"size", record.Size,
			"lastWritten", record.LastWritten,
			"scanCount", record.ScanCount)
	} else {
		// Record exists, increment the scan count
		record.ScanCount = existingRecord.ScanCount + 1

		// Add scan count metadata
		xray.AddMetadata(ctx, "scan_count", record.ScanCount)
		s.logger.Debugw("Incrementing scan count",
			"logFile", record.LogFileName,
			"oldScanCount", existingRecord.ScanCount,
			"newScanCount", record.ScanCount)

		if existingRecord.Size != record.Size || existingRecord.LastWritten != record.LastWritten {
			// Record has changed, update it
			// Update the LastBackup field with current time and ExpirationTime with new expiration time
			now := time.Now()
			currentMillis := now.UnixMilli()
			humanReadableCurrentTime := now.Format(time.RFC3339)

			expirationTime := now.AddDate(0, 0, s.cfg.TTLDays)
			expirationMillis := expirationTime.UnixMilli()
			humanReadableExpirationTime := expirationTime.Format(time.RFC3339)

			record.LastBackup = currentMillis
			record.HumanReadableLastBackup = humanReadableCurrentTime
			record.ExpirationTime = expirationMillis
			record.HumanReadableExpiration = humanReadableExpirationTime
			record.SHA256Checksum = existingRecord.SHA256Checksum // Preserve the SHA256 checksum

			// Add TTL metadata
			xray.AddMetadata(ctx, "ttl_days", s.cfg.TTLDays)
			xray.AddMetadata(ctx, "current_millis", currentMillis)
			xray.AddMetadata(ctx, "current_human", humanReadableCurrentTime)
			xray.AddMetadata(ctx, "expiration_millis", expirationMillis)
			xray.AddMetadata(ctx, "expiration_human", humanReadableExpirationTime)
			s.logger.Debugw("Updating backup timestamps",
				"ttlDays", s.cfg.TTLDays,
				"currentMillis", currentMillis,
				"humanReadableCurrent", humanReadableCurrentTime,
				"expirationMillis", expirationMillis,
				"humanReadableExpiration", humanReadableExpirationTime)

			ctx, updateSubsegment := xray.BeginSubsegment(ctx, "UpdateLogFileRecord")
			err = s.repo.UpdateLogFileRecord(ctx, record)
			if err != nil {
				updateSubsegment.AddError(err)
				s.logger.Errorw("Error updating record",
					"logFile", record.LogFileName,
					"error", err)
				updateSubsegment.Close(err)
				return errors.Wrap(err, "failed to update record")
			}
			updateSubsegment.Close(nil)
			xray.AddAnnotation(ctx, "operation", "update")
			s.logger.Infow("Updated record for log file",
				"logFile", record.LogFileName,
				"oldSize", existingRecord.Size,
				"newSize", record.Size,
				"oldLastWritten", existingRecord.LastWritten,
				"newLastWritten", record.LastWritten)
		} else {
			// Record exists and hasn't changed, but we still update the scan count
			s.logger.Debugw("Log file hasn't changed, updating scan count only",
				"logFile", record.LogFileName,
				"scanCount", record.ScanCount)
		}

		// Update the record with the new scan count
		ctx, updateSubsegment := xray.BeginSubsegment(ctx, "UpdateLogFileRecord")
		err = s.repo.UpdateLogFileRecord(ctx, record)
		if err != nil {
			updateSubsegment.AddError(err)
			s.logger.Errorw("Error updating record",
				"logFile", record.LogFileName,
				"error", err)
			updateSubsegment.Close(err)
			return errors.Wrap(err, "failed to update record")
		}
		updateSubsegment.Close(nil)
		xray.AddAnnotation(ctx, "operation", "update")
	}

	return nil
}
