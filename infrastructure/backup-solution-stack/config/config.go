package config

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"backup-solution-stack/utils"
)

// LambdaConfig represents the Lambda function configuration
type LambdaConfig struct {
	DBScannerMemory      int
	DBScannerTimeout     int
	LogDetectorMemory    int
	LogDetectorTimeout   int
	LogDownloaderMemory  int
	LogDownloaderTimeout int
	SQSVisibilityTimeout int
	BatchSize            int
	EventBridgeSchedule  string
	S3LogPrefix          string
	PublishVersions      bool
	BackupLogTypes       string
}

// ImageConfig represents the container image configuration
type ImageConfig struct {
	DBScannerVersion     string
	LogDetectorVersion   string
	LogDownloaderVersion string
}

// Config represents the complete configuration for the Backup Solution Stack
type Config struct {
	// AWS region
	Region string
	// Tags configuration
	Tags utils.TagsConfig
	// Lambda configuration
	Lambda LambdaConfig
	// Image configuration
	Images ImageConfig
}

// LoadConfig loads configuration from Pulumi config
func LoadConfig(ctx *pulumi.Context) (*Config, error) {
	// Initialize configuration
	cfg := config.New(ctx, "")
	awsCfg := config.New(ctx, "aws")
	backupCfg := config.New(ctx, "backup-solution")

	// Load AWS region
	region := awsCfg.Require("region")

	// Load mandatory tags
	environment := cfg.Get("environment")
	if environment == "" {
		environment = "dev" // Default to dev if not specified
	}

	project := cfg.Get("project")
	if project == "" {
		project = "aurora-audit-log-backup" // Default project name
	}

	owner := cfg.Get("owner")
	if owner == "" {
		owner = "cloud-engineering-team" // Default owner
	}

	// Load custom tags if provided
	var customTags map[string]string
	customTagsStr := cfg.Get("customTags")
	if customTagsStr != "" {
		if err := json.Unmarshal([]byte(customTagsStr), &customTags); err != nil {
			return nil, fmt.Errorf("failed to parse customTags: %v", err)
		}
	}

	// Create tags config
	tagsConfig := utils.TagsConfig{
		Environment: environment,
		Project:     project,
		Owner:       owner,
		CustomTags:  customTags,
	}

	// Validate tags
	if err := utils.ValidateTags(tagsConfig); err != nil {
		return nil, err
	}

	// Load Lambda configuration
	dbScannerMemory, err := strconv.Atoi(backupCfg.Require("dbScannerMemory"))
	if err != nil {
		return nil, fmt.Errorf("invalid dbScannerMemory: %v", err)
	}

	dbScannerTimeout, err := strconv.Atoi(backupCfg.Require("dbScannerTimeout"))
	if err != nil {
		return nil, fmt.Errorf("invalid dbScannerTimeout: %v", err)
	}

	logDetectorMemory, err := strconv.Atoi(backupCfg.Require("logDetectorMemory"))
	if err != nil {
		return nil, fmt.Errorf("invalid logDetectorMemory: %v", err)
	}

	logDetectorTimeout, err := strconv.Atoi(backupCfg.Require("logDetectorTimeout"))
	if err != nil {
		return nil, fmt.Errorf("invalid logDetectorTimeout: %v", err)
	}

	logDownloaderMemory, err := strconv.Atoi(backupCfg.Require("logDownloaderMemory"))
	if err != nil {
		return nil, fmt.Errorf("invalid logDownloaderMemory: %v", err)
	}

	logDownloaderTimeout, err := strconv.Atoi(backupCfg.Require("logDownloaderTimeout"))
	if err != nil {
		return nil, fmt.Errorf("invalid logDownloaderTimeout: %v", err)
	}

	eventBridgeSchedule := backupCfg.Require("eventBridgeSchedule")
	s3LogPrefix := backupCfg.Require("s3LogPrefix")

	lambdaBatchSize, err := strconv.Atoi(backupCfg.Require("lambdaBatchSize"))
	if err != nil {
		return nil, fmt.Errorf("invalid lambdaBatchSize: %v", err)
	}

	// Get SQS visibility timeout, default to logDetectorTimeout if not specified
	sqsVisibilityTimeout := logDetectorTimeout // Default to match the Lambda timeout
	if sqsVisibilityTimeoutStr := backupCfg.Get("sqsVisibilityTimeout"); sqsVisibilityTimeoutStr != "" {
		sqsVisibilityTimeout, err = strconv.Atoi(sqsVisibilityTimeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid sqsVisibilityTimeout: %v", err)
		}
	}

	// Ensure SQS visibility timeout is at least as long as the Lambda function timeout
	if sqsVisibilityTimeout < logDetectorTimeout {
		fmt.Printf("Warning: SQS visibility timeout (%d) is less than LogDetector Lambda timeout (%d). Increasing to match.\n",
			sqsVisibilityTimeout, logDetectorTimeout)
		sqsVisibilityTimeout = logDetectorTimeout
	}

	// Get backup log types, default to "audit" if not specified
	backupLogTypes := backupCfg.Get("backupLogTypes")
	if backupLogTypes == "" {
		backupLogTypes = "audit" // Default to audit logs for backward compatibility
	}

	// Check if we should publish Lambda versions
	publishVersions := false
	if publishVersionsStr := backupCfg.Get("publishLambdaVersions"); publishVersionsStr == "true" {
		publishVersions = true
	}

	// Load image versions
	dbScannerImageVersion := backupCfg.Get("dbScannerImageVersion")
	if dbScannerImageVersion == "" {
		dbScannerImageVersion = "latest" // Fallback to latest if not specified
	}

	logDetectorImageVersion := backupCfg.Get("logDetectorImageVersion")
	if logDetectorImageVersion == "" {
		logDetectorImageVersion = "latest"
	}

	logDownloaderImageVersion := backupCfg.Get("logDownloaderImageVersion")
	if logDownloaderImageVersion == "" {
		logDownloaderImageVersion = "latest"
	}

	// Create and return config
	return &Config{
		Region: region,
		Tags:   tagsConfig,
		Lambda: LambdaConfig{
			DBScannerMemory:      dbScannerMemory,
			DBScannerTimeout:     dbScannerTimeout,
			LogDetectorMemory:    logDetectorMemory,
			LogDetectorTimeout:   logDetectorTimeout,
			LogDownloaderMemory:  logDownloaderMemory,
			LogDownloaderTimeout: logDownloaderTimeout,
			SQSVisibilityTimeout: sqsVisibilityTimeout,
			BatchSize:            lambdaBatchSize,
			EventBridgeSchedule:  eventBridgeSchedule,
			S3LogPrefix:          s3LogPrefix,
			PublishVersions:      publishVersions,
			BackupLogTypes:       backupLogTypes,
		},
		Images: ImageConfig{
			DBScannerVersion:     dbScannerImageVersion,
			LogDetectorVersion:   logDetectorImageVersion,
			LogDownloaderVersion: logDownloaderImageVersion,
		},
	}, nil
}