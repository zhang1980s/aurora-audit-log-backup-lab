# Lambda Versioning Strategy

This document explains the Lambda versioning strategy implemented in this project and how to use it.

## Overview

The project uses AWS Lambda functions with container images. To ensure proper versioning and deployment control, we've implemented:

1. **Specific version tags** for container images
2. **Lambda versioning** for tracking function changes
3. **Lambda aliases** for stable references to functions
4. **Configuration-driven versioning** to avoid hardcoding

## Configuration

Version parameters are stored in `infrastructure/backup-solution-stack/Pulumi.dev.yaml`:

```yaml
backup-solution:dbScannerImageVersion: "v1.0.0"
backup-solution:logDetectorImageVersion: "v1.0.0"
backup-solution:logDownloaderImageVersion: "v1.0.0"
backup-solution:publishLambdaVersions: "true"
```

## How It Works

1. **Container Images**: Each Lambda function uses a container image with a specific version tag
2. **Lambda Versions**: When `publishLambdaVersions` is set to `true`, Pulumi creates a new Lambda version when the function changes
3. **Lambda Aliases**: Each function has a `live` alias that points to the latest version
4. **Event Sources**: All event sources (EventBridge, SQS, DynamoDB) point to the aliases instead of directly to the functions

## Deployment Workflow

### Building and Pushing New Versions

To update Lambda functions with new code:

1. Make your code changes to the Lambda function(s)
2. Use one of the following methods to build, push, and update configuration:

#### Method 1: Specify a Version Manually

```bash
make build-and-push-versioned VERSION=v1.0.1
```

This will:
- Build Docker images with the specified version tag
- Push the images to ECR
- Update the Pulumi configuration with the new version

#### Method 2: Automatically Increment Version

```bash
make increment-version
```

This will:
- Read the current version from Pulumi configuration
- Increment the patch version (e.g., v1.0.0 → v1.0.1)
- Build and push images with the new version
- Update the Pulumi configuration with the new version

#### Method 3: Full Deployment Workflow

```bash
make deploy-new-version
```

This will:
- Increment the version automatically
- Build and push images with the new version
- Update the Pulumi configuration with the new version

3. Deploy with Pulumi:

```bash
cd infrastructure/backup-solution-stack
pulumi up
```

Pulumi will:
- Detect the change in image version
- Update the Lambda function to use the new image
- Create a new version of the Lambda function (if `publishLambdaVersions` is true)

### Additional Makefile Targets

The project includes several utility targets to help with Lambda versioning:

- `update-pulumi-config`: Updates the Pulumi configuration with a specified version
  ```bash
  make update-pulumi-config VERSION=v1.0.1
  ```

- `version-info`: Displays the current Lambda image versions from Pulumi configuration
  ```bash
  make version-info
  ```

- `build`: Builds Lambda container images locally with the "local" tag
  ```bash
  make build
  ```

- `push-images`: Pushes locally built images to ECR with the "latest" tag
  ```bash
  make push-images
  ```

- `clean`: Removes local and ECR-tagged Docker images
  ```bash
  make clean
  ```

### Rolling Back

To roll back to a previous version:

1. Update the version in Pulumi.dev.yaml:

```bash
cd infrastructure/backup-solution-stack
pulumi config set backup-solution:dbScannerImageVersion v1.0.0
pulumi up
```

## Best Practices

1. **Use Semantic Versioning**: Follow semantic versioning (v1.0.0, v1.0.1, etc.) for your image tags
2. **Consistent Versioning**: Use the same version number for all functions that need to work together
3. **Test Before Deployment**: Test your Lambda functions locally before deploying
4. **Monitor After Deployment**: Monitor your Lambda functions after deployment to ensure they're working correctly
5. **Document Changes**: Document what changes were made in each version

## Troubleshooting

If you encounter issues with Lambda versioning:

1. **Check Image Versions**: Ensure the correct image versions are specified in Pulumi.dev.yaml
2. **Verify ECR Images**: Verify that the images with the specified versions exist in ECR
3. **Check Lambda Aliases**: Verify that the Lambda aliases are pointing to the correct versions
4. **Review Event Sources**: Ensure that event sources are correctly configured to use the aliases

## Additional Resources

- [AWS Lambda Versioning and Aliases](https://docs.aws.amazon.com/lambda/latest/dg/configuration-versions.html)
- [Pulumi Lambda Documentation](https://www.pulumi.com/registry/packages/aws/api-docs/lambda/)
- [Container Image Support for AWS Lambda](https://docs.aws.amazon.com/lambda/latest/dg/lambda-images.html)