package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"aurora-ecr/config"
	"aurora-ecr/resources"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Load configuration
		cfg, err := config.LoadConfig(ctx)
		if err != nil {
			return err
		}

		// Create ECR repositories
		repos, err := resources.CreateEcrRepositories(ctx, cfg)
		if err != nil {
			return err
		}

		// Export ECR repository URLs
		ctx.Export("dbScannerRepositoryUrl", repos.DbScannerRepo.RepositoryUrl)
		ctx.Export("logDetectorRepositoryUrl", repos.LogDetectorRepo.RepositoryUrl)
		ctx.Export("logDownloaderRepositoryUrl", repos.LogDownloaderRepo.RepositoryUrl)

		return nil
	})
}
