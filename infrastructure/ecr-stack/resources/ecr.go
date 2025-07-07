package resources

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ecr"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"aurora-ecr/config"
	"aurora-ecr/utils"
)

// EcrRepositories holds the created ECR repository resources
type EcrRepositories struct {
	DbScannerRepo     *ecr.Repository
	LogDetectorRepo   *ecr.Repository
	LogDownloaderRepo *ecr.Repository
}

// CreateEcrRepositories creates ECR repositories based on the provided configuration
func CreateEcrRepositories(ctx *pulumi.Context, cfg *config.EcrConfig) (*EcrRepositories, error) {
	// Create lifecycle policy for repositories
	lifecyclePolicy := createLifecyclePolicy(cfg.MaxImageCount)

	// Create ECR repository for DB Scanner Lambda
	dbScannerRepo, err := createRepository(
		ctx,
		"db-scanner-repo",
		cfg.DbScannerRepo,
		cfg.ScanOnPush,
		cfg.ImageTagMutability,
		lifecyclePolicy,
		cfg.Tags,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create DB Scanner repository: %v", err)
	}

	// Create ECR repository for Log Detector Lambda
	logDetectorRepo, err := createRepository(
		ctx,
		"log-detector-repo",
		cfg.LogDetectorRepo,
		cfg.ScanOnPush,
		cfg.ImageTagMutability,
		lifecyclePolicy,
		cfg.Tags,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Log Detector repository: %v", err)
	}

	// Create ECR repository for Log Downloader Lambda
	logDownloaderRepo, err := createRepository(
		ctx,
		"log-downloader-repo",
		cfg.LogDownloaderRepo,
		cfg.ScanOnPush,
		cfg.ImageTagMutability,
		lifecyclePolicy,
		cfg.Tags,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Log Downloader repository: %v", err)
	}

	return &EcrRepositories{
		DbScannerRepo:     dbScannerRepo,
		LogDetectorRepo:   logDetectorRepo,
		LogDownloaderRepo: logDownloaderRepo,
	}, nil
}

// createRepository is a helper function to create an ECR repository with consistent settings
func createRepository(
	ctx *pulumi.Context,
	name string,
	repoName string,
	scanOnPush bool,
	imageTagMutability string,
	lifecyclePolicy string,
	tagsConfig utils.TagsConfig,
) (*ecr.Repository, error) {
	// Generate resource name
	resourceName := fmt.Sprintf("%s-repo", name)

	// Generate tags using the correct function name
	tags := utils.CreateResourceTags(ctx, tagsConfig, resourceName)

	// Create repository
	repo, err := ecr.NewRepository(ctx, resourceName, &ecr.RepositoryArgs{
		Name: pulumi.String(repoName),
		ImageScanningConfiguration: &ecr.RepositoryImageScanningConfigurationArgs{
			ScanOnPush: pulumi.Bool(scanOnPush),
		},
		ImageTagMutability: pulumi.String(imageTagMutability),
		Tags:               tags,
	})

	if err != nil {
		return nil, err
	}

	// Create lifecycle policy as a separate resource
	_, err = ecr.NewLifecyclePolicy(ctx, fmt.Sprintf("%s-lifecycle", resourceName), &ecr.LifecyclePolicyArgs{
		Repository: repo.Name,
		Policy:     pulumi.String(lifecyclePolicy),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create lifecycle policy: %v", err)
	}

	return repo, nil
}

// createLifecyclePolicy creates a lifecycle policy JSON string for ECR repositories
func createLifecyclePolicy(maxImageCount int) string {
	return fmt.Sprintf(`{
		"rules": [
			{
				"rulePriority": 1,
				"description": "Keep only %d images, expire all others",
				"selection": {
					"tagStatus": "any",
					"countType": "imageCountMoreThan",
					"countNumber": %d
				},
				"action": {
					"type": "expire"
				}
			}
		]
	}`, maxImageCount, maxImageCount)
}
