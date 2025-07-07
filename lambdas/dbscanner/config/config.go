package config

import (
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap/zapcore"
)

// Config holds all configuration for the Lambda function
type Config struct {
	SQSQueueURL string
	LogLevel    zapcore.Level
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	// Get SQS queue URL from environment variable
	queueURL := os.Getenv("SQS_QUEUE_URL")
	if queueURL == "" {
		return nil, fmt.Errorf("SQS_QUEUE_URL environment variable not set")
	}

	// Get log level from environment variable, default to "error"
	logLevelStr := os.Getenv("LOG_LEVEL")
	if logLevelStr == "" {
		logLevelStr = "error"
	}

	// Parse log level
	var logLevel zapcore.Level
	err := logLevel.UnmarshalText([]byte(strings.ToLower(logLevelStr)))
	if err != nil {
		// If parsing fails, default to error level
		logLevel = zapcore.ErrorLevel
	}

	return &Config{
		SQSQueueURL: queueURL,
		LogLevel:    logLevel,
	}, nil
}
