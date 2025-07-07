package utils

import (
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// TagsConfig represents the tag configuration for resources
type TagsConfig struct {
	Environment string
	Project     string
	Owner       string
	CustomTags  map[string]string
}

// ValidateTags ensures that all mandatory tags are present
func ValidateTags(tags TagsConfig) error {
	if tags.Environment == "" {
		return fmt.Errorf("mandatory tag 'Environment' is missing")
	}
	if tags.Project == "" {
		return fmt.Errorf("mandatory tag 'Project' is missing")
	}
	if tags.Owner == "" {
		return fmt.Errorf("mandatory tag 'Owner' is missing")
	}
	return nil
}

// CreateResourceTags creates a pulumi.StringMap with all required and custom tags
func CreateResourceTags(ctx *pulumi.Context, tags TagsConfig, resourceName string) pulumi.StringMap {
	// Start with mandatory tags
	resourceTags := pulumi.StringMap{
		"Environment": pulumi.String(tags.Environment),
		"Project":     pulumi.String(tags.Project),
		"Owner":       pulumi.String(tags.Owner),
		"Name":        pulumi.String(resourceName),
	}

	// Add custom tags if provided
	if tags.CustomTags != nil {
		for key, value := range tags.CustomTags {
			resourceTags[key] = pulumi.String(value)
		}
	}

	// Log tagging operation for debugging
	ctx.Log.Info(fmt.Sprintf("Applying tags to resource '%s': %v", resourceName, resourceTags), nil)

	return resourceTags
}
