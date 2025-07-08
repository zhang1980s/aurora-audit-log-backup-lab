package config

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"aurora-audit-log-backup-lab/utils"
)

// NetworkConfig represents the network configuration
type NetworkConfig struct {
	AvailabilityZone1 string
	AvailabilityZone2 string
}

// LambdaConfig represents the Lambda function configuration
type LambdaConfig struct {
	DBScannerMemory      int
	DBScannerTimeout     int
	LogDetectorMemory    int
	LogDetectorTimeout   int
	LogDownloaderMemory  int
	LogDownloaderTimeout int
	SQSVisibilityTimeout int // Visibility timeout for SQS queue (in seconds)
	BatchSize            int
	EventBridgeSchedule  string
	S3LogPrefix          string
	PublishVersions      bool
	BackupLogTypes       string // Types of logs to backup (audit, error, instance)
}

// TestEnvConfig represents the test environment configuration
type TestEnvConfig struct {
	EC2KeyPairName     string
	EC2InstanceType    string
	AuroraInstanceType string
}

// ImageConfig represents the container image configuration
type ImageConfig struct {
	DBScannerVersion     string
	LogDetectorVersion   string
	LogDownloaderVersion string
}

// Config represents the complete configuration for the Aurora Log Backup Lab
type Config struct {
	// AWS region
	Region string
	// Tags configuration
	Tags utils.TagsConfig
	// Network configuration
	Network NetworkConfig
	// Lambda configuration
	Lambda LambdaConfig
	// Test environment configuration
	TestEnv TestEnvConfig
	// Image configuration
	Images ImageConfig
}

// LoadConfig loads configuration from Pulumi config
func LoadConfig(ctx *pulumi.Context) (*Config, error) {
	// Initialize configuration
	cfg := config.New(ctx, "")
	awsCfg := config.New(ctx, "aws")
	projectCfg := config.New(ctx, "aurora-audit-log-backup-lab")

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

	// Load network configuration
	az1 := projectCfg.Require("availabilityZone1")
	az2 := projectCfg.Require("availabilityZone2")

	// Load Lambda configuration
	dbScannerMemory, err := strconv.Atoi(projectCfg.Require("dbScannerMemory"))
	if err != nil {
		return nil, fmt.Errorf("invalid dbScannerMemory: %v", err)
	}

	dbScannerTimeout, err := strconv.Atoi(projectCfg.Require("dbScannerTimeout"))
	if err != nil {
		return nil, fmt.Errorf("invalid dbScannerTimeout: %v", err)
	}

	logDetectorMemory, err := strconv.Atoi(projectCfg.Require("logDetectorMemory"))
	if err != nil {
		return nil, fmt.Errorf("invalid logDetectorMemory: %v", err)
	}

	logDetectorTimeout, err := strconv.Atoi(projectCfg.Require("logDetectorTimeout"))
	if err != nil {
		return nil, fmt.Errorf("invalid logDetectorTimeout: %v", err)
	}

	logDownloaderMemory, err := strconv.Atoi(projectCfg.Require("logDownloaderMemory"))
	if err != nil {
		return nil, fmt.Errorf("invalid logDownloaderMemory: %v", err)
	}

	logDownloaderTimeout, err := strconv.Atoi(projectCfg.Require("logDownloaderTimeout"))
	if err != nil {
		return nil, fmt.Errorf("invalid logDownloaderTimeout: %v", err)
	}

	eventBridgeSchedule := projectCfg.Require("eventBridgeSchedule")
	s3LogPrefix := projectCfg.Require("s3LogPrefix")

	lambdaBatchSize, err := strconv.Atoi(projectCfg.Require("lambdaBatchSize"))
	if err != nil {
		return nil, fmt.Errorf("invalid lambdaBatchSize: %v", err)
	}

	// Get SQS visibility timeout, default to logDetectorTimeout if not specified
	sqsVisibilityTimeout := logDetectorTimeout // Default to match the Lambda timeout
	if sqsVisibilityTimeoutStr := projectCfg.Get("sqsVisibilityTimeout"); sqsVisibilityTimeoutStr != "" {
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
	backupLogTypes := projectCfg.Get("backupLogTypes")
	if backupLogTypes == "" {
		backupLogTypes = "audit" // Default to audit logs for backward compatibility
	}

	// Check if we should publish Lambda versions
	publishVersions := false
	if publishVersionsStr := projectCfg.Get("publishLambdaVersions"); publishVersionsStr == "true" {
		publishVersions = true
	}

	// Load test environment configuration
	ec2KeyPairName := projectCfg.Require("ec2KeyPairName")
	ec2InstanceType := projectCfg.Require("ec2InstanceType")
	auroraInstanceType := projectCfg.Require("auroraInstanceType")

	// Load image versions
	dbScannerImageVersion := projectCfg.Get("dbScannerImageVersion")
	if dbScannerImageVersion == "" {
		dbScannerImageVersion = "latest" // Fallback to latest if not specified
	}

	logDetectorImageVersion := projectCfg.Get("logDetectorImageVersion")
	if logDetectorImageVersion == "" {
		logDetectorImageVersion = "latest"
	}

	logDownloaderImageVersion := projectCfg.Get("logDownloaderImageVersion")
	if logDownloaderImageVersion == "" {
		logDownloaderImageVersion = "latest"
	}

	// Create and return config
	return &Config{
		Region: region,
		Tags:   tagsConfig,
		Network: NetworkConfig{
			AvailabilityZone1: az1,
			AvailabilityZone2: az2,
		},
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
		TestEnv: TestEnvConfig{
			EC2KeyPairName:     ec2KeyPairName,
			EC2InstanceType:    ec2InstanceType,
			AuroraInstanceType: auroraInstanceType,
		},
		Images: ImageConfig{
			DBScannerVersion:     dbScannerImageVersion,
			LogDetectorVersion:   logDetectorImageVersion,
			LogDownloaderVersion: logDownloaderImageVersion,
		},
	}, nil
}
