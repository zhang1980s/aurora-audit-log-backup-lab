package config

import (
	"encoding/json"
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"aurora-cluster-stack/utils"
)

// AuroraConfig represents the Aurora cluster configuration
type AuroraConfig struct {
	InstanceType string
}

// Config represents the complete configuration for the Aurora Cluster Stack
type Config struct {
	// AWS region
	Region string
	// Tags configuration
	Tags utils.TagsConfig
	// Aurora configuration
	Aurora AuroraConfig
}

// LoadConfig loads configuration from Pulumi config
func LoadConfig(ctx *pulumi.Context) (*Config, error) {
	// Initialize configuration
	cfg := config.New(ctx, "")
	awsCfg := config.New(ctx, "aws")
	auroraCfg := config.New(ctx, "aurora-cluster")

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

	// Load Aurora configuration
	auroraInstanceType := auroraCfg.Require("instanceType")

	// Create and return config
	return &Config{
		Region: region,
		Tags:   tagsConfig,
		Aurora: AuroraConfig{
			InstanceType: auroraInstanceType,
		},
	}, nil
}