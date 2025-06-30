package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/ecr"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// EcrResources holds all the ECR resources
type EcrResources struct {
	DbScannerRepository     *ecr.Repository
	LogDetectorRepository   *ecr.Repository
	LogDownloaderRepository *ecr.Repository
}

// createEcrResources creates ECR repositories for Lambda container images
func createEcrResources(ctx *pulumi.Context) (*EcrResources, error) {
	// Create ECR repository for DB Scanner Lambda
	dbScannerRepo, err := ecr.NewRepository(ctx, "aurora-db-scanner-repo", &ecr.RepositoryArgs{
		Name: pulumi.String("aurora-db-scanner"),
		ImageScanningConfiguration: &ecr.RepositoryImageScanningConfigurationArgs{
			ScanOnPush: pulumi.Bool(true),
		},
		ImageTagMutability: pulumi.String("MUTABLE"),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-db-scanner-repo"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create ECR repository for Log Detector Lambda
	logDetectorRepo, err := ecr.NewRepository(ctx, "aurora-log-detector-repo", &ecr.RepositoryArgs{
		Name: pulumi.String("aurora-log-detector"),
		ImageScanningConfiguration: &ecr.RepositoryImageScanningConfigurationArgs{
			ScanOnPush: pulumi.Bool(true),
		},
		ImageTagMutability: pulumi.String("MUTABLE"),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-log-detector-repo"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create ECR repository for Log Downloader Lambda
	logDownloaderRepo, err := ecr.NewRepository(ctx, "aurora-log-downloader-repo", &ecr.RepositoryArgs{
		Name: pulumi.String("aurora-log-downloader"),
		ImageScanningConfiguration: &ecr.RepositoryImageScanningConfigurationArgs{
			ScanOnPush: pulumi.Bool(true),
		},
		ImageTagMutability: pulumi.String("MUTABLE"),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-log-downloader-repo"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Export ECR repository URLs
	ctx.Export("dbScannerRepositoryUrl", dbScannerRepo.RepositoryUrl)
	ctx.Export("logDetectorRepositoryUrl", logDetectorRepo.RepositoryUrl)
	ctx.Export("logDownloaderRepositoryUrl", logDownloaderRepo.RepositoryUrl)

	return &EcrResources{
		DbScannerRepository:     dbScannerRepo,
		LogDetectorRepository:   logDetectorRepo,
		LogDownloaderRepository: logDownloaderRepo,
	}, nil
}
