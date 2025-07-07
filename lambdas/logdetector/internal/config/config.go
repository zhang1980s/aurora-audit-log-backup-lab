package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdetector/pkg/errors"
)

// Config holds all configuration for the Lambda function
type Config struct {
	DynamoDBTableName string
	LogLevel          string
	BackupLogTypes    []string // Types of logs to backup (audit, error, instance)
	Region            string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		DynamoDBTableName: os.Getenv("DYNAMODB_TABLE_NAME"),
		LogLevel:          os.Getenv("LOG_LEVEL"),
		Region:            os.Getenv("AWS_REGION"),
	}

	// Set defaults
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info" // Default log level
	}

	// Get log types to backup from environment variable, default to "audit" if not set
	backupLogs := os.Getenv("BACKUP_LOGS")
	if backupLogs == "" {
		backupLogs = "audit" // Default to audit logs for backward compatibility
	}

	// Split the backupLogs string by comma to get individual log types
	logTypes := strings.Split(backupLogs, ",")

	// Trim spaces from each log type
	for _, logType := range logTypes {
		cfg.BackupLogTypes = append(cfg.BackupLogTypes, strings.TrimSpace(logType))
	}

	// Validate required configuration
	if cfg.DynamoDBTableName == "" {
		return nil, errors.Wrap(errors.ErrConfiguration, "DYNAMODB_TABLE_NAME environment variable is required")
	}

	return cfg, nil
}

// String returns a string representation of the configuration
func (c *Config) String() string {
	return fmt.Sprintf(
		"Config{DynamoDBTableName: %s, LogLevel: %s, BackupLogTypes: %v, Region: %s}",
		c.DynamoDBTableName,
		c.LogLevel,
		c.BackupLogTypes,
		c.Region,
	)
}

// ShouldBackupLog checks if a log file should be backed up based on the configuration
func (c *Config) ShouldBackupLog(logFileName string) bool {
	for _, logType := range c.BackupLogTypes {
		switch logType {
		case "audit":
			// Check for audit logs
			if strings.HasPrefix(logFileName, "audit/") ||
				(len(logFileName) >= 5 && logFileName[0:5] == "audit") {
				return true
			}
		case "error":
			// Check for error logs
			if strings.HasPrefix(logFileName, "error/") ||
				(len(logFileName) >= 5 && logFileName[0:5] == "error") {
				return true
			}
		case "instance":
			// Check for instance logs
			if strings.HasPrefix(logFileName, "instance/") ||
				(len(logFileName) >= 8 && logFileName[0:8] == "instance") {
				return true
			}
		}
	}

	// If we get here, the log file didn't match any of the configured log types
	return false
}
