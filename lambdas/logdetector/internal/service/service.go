package service

import (
	"context"

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
	// Create a record for the log file
	record := models.LogFileRecord{
		DBInstanceIdentifier: dbInstanceID,
		LogFileName:          *logFile.LogFileName,
		Size:                 0, // Default value
		LastWritten:          0, // Default value
	}

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
			"lastWritten", record.LastWritten)
	} else if existingRecord.Size != record.Size || existingRecord.LastWritten != record.LastWritten {
		// Record exists but has changed, update it
		record.LastBackup = existingRecord.LastBackup                           // Preserve the LastBackup value
		record.HumanReadableLastBackup = existingRecord.HumanReadableLastBackup // Preserve the human-readable LastBackup
		record.SHA256Checksum = existingRecord.SHA256Checksum                   // Preserve the SHA256 checksum

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
		// Record exists and hasn't changed, skip it
		s.logger.Debugw("Log file hasn't changed, skipping",
			"logFile", record.LogFileName)
		xray.AddAnnotation(ctx, "operation", "skip")
	}

	return nil
}
