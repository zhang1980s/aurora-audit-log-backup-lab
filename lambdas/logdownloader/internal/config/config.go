package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdownloader/pkg/errors"
)

// Config holds all configuration for the Lambda function
type Config struct {
	DynamoDBTableName string
	S3BucketName      string
	S3Prefix          string
	LogLevel          string
	DownloadTimeout   time.Duration
	RetryAttempts     int
	RetryDelay        time.Duration
	Region            string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		DynamoDBTableName: os.Getenv("DYNAMODB_TABLE_NAME"),
		S3BucketName:      os.Getenv("S3_BUCKET_NAME"),
		S3Prefix:          os.Getenv("S3_PREFIX"),
		LogLevel:          os.Getenv("LOG_LEVEL"),
		Region:            os.Getenv("AWS_REGION"),
	}

	// Set defaults
	if cfg.S3Prefix == "" {
		cfg.S3Prefix = "logs" // Default prefix
	}

	if cfg.LogLevel == "" {
		cfg.LogLevel = "info" // Default log level
	}

	// Parse download timeout (default: 5 minutes)
	timeoutStr := os.Getenv("DOWNLOAD_TIMEOUT_SECONDS")
	if timeoutStr == "" {
		cfg.DownloadTimeout = 5 * time.Minute
	} else {
		timeoutSec, err := strconv.Atoi(timeoutStr)
		if err != nil {
			return nil, errors.Wrap(err, "invalid DOWNLOAD_TIMEOUT_SECONDS")
		}
		cfg.DownloadTimeout = time.Duration(timeoutSec) * time.Second
	}

	// Parse retry attempts (default: 3)
	retryStr := os.Getenv("RETRY_ATTEMPTS")
	if retryStr == "" {
		cfg.RetryAttempts = 3
	} else {
		retries, err := strconv.Atoi(retryStr)
		if err != nil {
			return nil, errors.Wrap(err, "invalid RETRY_ATTEMPTS")
		}
		cfg.RetryAttempts = retries
	}

	// Parse retry delay (default: 1 second)
	delayStr := os.Getenv("RETRY_DELAY_MS")
	if delayStr == "" {
		cfg.RetryDelay = 1 * time.Second
	} else {
		delayMs, err := strconv.Atoi(delayStr)
		if err != nil {
			return nil, errors.Wrap(err, "invalid RETRY_DELAY_MS")
		}
		cfg.RetryDelay = time.Duration(delayMs) * time.Millisecond
	}

	// Validate required configuration
	if cfg.DynamoDBTableName == "" {
		return nil, errors.Wrap(errors.ErrConfiguration, "DYNAMODB_TABLE_NAME environment variable is required")
	}

	if cfg.S3BucketName == "" {
		return nil, errors.Wrap(errors.ErrConfiguration, "S3_BUCKET_NAME environment variable is required")
	}

	return cfg, nil
}

// String returns a string representation of the configuration
func (c *Config) String() string {
	return fmt.Sprintf(
		"Config{DynamoDBTableName: %s, S3BucketName: %s, S3Prefix: %s, LogLevel: %s, DownloadTimeout: %v, RetryAttempts: %d, RetryDelay: %v, Region: %s}",
		c.DynamoDBTableName,
		c.S3BucketName,
		c.S3Prefix,
		c.LogLevel,
		c.DownloadTimeout,
		c.RetryAttempts,
		c.RetryDelay,
		c.Region,
	)
}
