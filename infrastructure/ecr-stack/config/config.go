package config

import (
	"encoding/json"
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	// Use the correct module path for utils package
	"aurora-ecr/utils"
)

// EcrConfig represents the configuration for ECR repositories
type EcrConfig struct {
	// AWS region for resources
	Region string
	// Tags configuration
	Tags utils.TagsConfig
	// ECR repository configuration
	ScanOnPush         bool
	ImageTagMutability string
	// Lifecycle policy configuration
	MaxImageCount int
	// Repository names
	DbScannerRepo     string
	LogDetectorRepo   string
	LogDownloaderRepo string
}

// LoadConfig loads configuration from Pulumi config
func LoadConfig(ctx *pulumi.Context) (*EcrConfig, error) {
	// Initialize configuration
	cfg := config.New(ctx, "")
	awsCfg := config.New(ctx, "aws")

	// Load AWS region
	region := awsCfg.Require("region")

	// Load mandatory tags
	environment := cfg.Require("environment")
	project := cfg.Require("project")
	owner := cfg.Require("owner")

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

	// Load ECR configuration with defaults
	scanOnPush := true
	if cfg.Get("scanOnPush") != "" {
		scanOnPush = cfg.GetBool("scanOnPush")
	}

	imageTagMutability := "MUTABLE"
	if cfg.Get("imageTagMutability") != "" {
		imageTagMutability = cfg.Get("imageTagMutability")
	}

	maxImageCount := 10
	if cfg.Get("maxImageCount") != "" {
		maxImageCount = cfg.GetInt("maxImageCount")
	}

	// Load repository names with defaults
	dbScannerRepo := "aurora-db-scanner"
	if cfg.Get("dbScannerRepo") != "" {
		dbScannerRepo = cfg.Get("dbScannerRepo")
	}

	logDetectorRepo := "aurora-log-detector"
	if cfg.Get("logDetectorRepo") != "" {
		logDetectorRepo = cfg.Get("logDetectorRepo")
	}

	logDownloaderRepo := "aurora-log-downloader"
	if cfg.Get("logDownloaderRepo") != "" {
		logDownloaderRepo = cfg.Get("logDownloaderRepo")
	}

	// Create and return config
	return &EcrConfig{
		Region:             region,
		Tags:               tagsConfig,
		ScanOnPush:         scanOnPush,
		ImageTagMutability: imageTagMutability,
		MaxImageCount:      maxImageCount,
		DbScannerRepo:      dbScannerRepo,
		LogDetectorRepo:    logDetectorRepo,
		LogDownloaderRepo:  logDownloaderRepo,
	}, nil
}
