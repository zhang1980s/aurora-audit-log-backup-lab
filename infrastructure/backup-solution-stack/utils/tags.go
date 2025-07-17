package utils

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// TagsConfig represents the configuration for resource tags
type TagsConfig struct {
	Environment string
	Project     string
	Owner       string
	CustomTags  map[string]string
}

// ValidateTags validates the tags configuration
func ValidateTags(cfg TagsConfig) error {
	// No validation needed for now
	return nil
}

// CreateResourceTags creates a pulumi.StringMap from the tags configuration
func CreateResourceTags(ctx *pulumi.Context, cfg TagsConfig, resourceName string) pulumi.StringMap {
	tags := pulumi.StringMap{
		"Name":        pulumi.String(resourceName),
		"Environment": pulumi.String(cfg.Environment),
		"Project":     pulumi.String(cfg.Project),
		"Owner":       pulumi.String(cfg.Owner),
	}

	// Add custom tags
	for k, v := range cfg.CustomTags {
		tags[k] = pulumi.String(v)
	}

	return tags
}