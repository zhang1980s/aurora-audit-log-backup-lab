package config

import (
	"encoding/json"
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"aurora-network-stack/utils"
)

// NetworkConfig represents the network configuration
type NetworkConfig struct {
	AvailabilityZone1 string
	AvailabilityZone2 string
}

// Config represents the complete configuration for the Network Stack
type Config struct {
	// AWS region
	Region string
	// Tags configuration
	Tags utils.TagsConfig
	// Network configuration
	Network NetworkConfig
}

// LoadConfig loads configuration from Pulumi config
func LoadConfig(ctx *pulumi.Context) (*Config, error) {
	// Initialize configuration
	cfg := config.New(ctx, "")
	awsCfg := config.New(ctx, "aws")
	networkCfg := config.New(ctx, "aurora-network")

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
	az1 := networkCfg.Require("availabilityZone1")
	az2 := networkCfg.Require("availabilityZone2")

	// Create and return config
	return &Config{
		Region: region,
		Tags:   tagsConfig,
		Network: NetworkConfig{
			AvailabilityZone1: az1,
			AvailabilityZone2: az2,
		},
	}, nil
}