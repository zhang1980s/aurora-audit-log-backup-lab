package config

import (
	"encoding/json"
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"ec2-stress-client-stack/utils"
)

// EC2Config represents the EC2 instance configuration
type EC2Config struct {
	KeyPairName  string
	InstanceType string
}

// Config represents the complete configuration for the EC2 Stress Client Stack
type Config struct {
	Region string
	Tags   utils.TagsConfig
	EC2    EC2Config
}

// LoadConfig loads configuration from Pulumi config
func LoadConfig(ctx *pulumi.Context) (*Config, error) {
	cfg := config.New(ctx, "")
	awsCfg := config.New(ctx, "aws")
	ec2Cfg := config.New(ctx, "ec2-stress-client")

	region := awsCfg.Require("region")

	environment := cfg.Get("environment")
	if environment == "" {
		environment = "dev"
	}

	project := cfg.Get("project")
	if project == "" {
		project = "aurora-audit-log-backup"
	}

	owner := cfg.Get("owner")
	if owner == "" {
		owner = "cloud-engineering-team"
	}

	var customTags map[string]string
	customTagsStr := cfg.Get("customTags")
	if customTagsStr != "" {
		if err := json.Unmarshal([]byte(customTagsStr), &customTags); err != nil {
			return nil, fmt.Errorf("failed to parse customTags: %v", err)
		}
	}

	tagsConfig := utils.TagsConfig{
		Environment: environment,
		Project:     project,
		Owner:       owner,
		CustomTags:  customTags,
	}

	keyPairName := ec2Cfg.Require("keyPairName")
	instanceType := ec2Cfg.Require("instanceType")

	return &Config{
		Region: region,
		Tags:   tagsConfig,
		EC2: EC2Config{
			KeyPairName:  keyPairName,
			InstanceType: instanceType,
		},
	}, nil
}