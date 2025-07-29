# Makefile for Aurora Audit Log Backup Lab

# Variables
VERSION ?= latest

# Build and push Lambda container images
build-and-push-versioned:
	@echo "Building and pushing Lambda container images with version $(VERSION)"
	# Build and tag images
	docker build -t aurora-db-scanner:$(VERSION) ./lambdas/dbscanner
	docker build -t aurora-log-detector:$(VERSION) ./lambdas/logdetector
	docker build -t aurora-log-downloader:$(VERSION) ./lambdas/logdownloader
	
	# Get ECR repository URLs
	$(eval DB_SCANNER_REPO := $(shell cd infrastructure/ecr-stack && pulumi stack output dbScannerRepositoryUrl))
	$(eval LOG_DETECTOR_REPO := $(shell cd infrastructure/ecr-stack && pulumi stack output logDetectorRepositoryUrl))
	$(eval LOG_DOWNLOADER_REPO := $(shell cd infrastructure/ecr-stack && pulumi stack output logDownloaderRepositoryUrl))
	
	# Tag images with ECR repository URLs
	docker tag aurora-db-scanner:$(VERSION) $(DB_SCANNER_REPO):$(VERSION)
	docker tag aurora-log-detector:$(VERSION) $(LOG_DETECTOR_REPO):$(VERSION)
	docker tag aurora-log-downloader:$(VERSION) $(LOG_DOWNLOADER_REPO):$(VERSION)
	
	# Authenticate with ECR
	@echo "Authenticating with ECR..."
	aws ecr get-login-password --region $$(aws configure get region) | docker login --username AWS --password-stdin $$(echo $(DB_SCANNER_REPO) | cut -d'/' -f1)
	
	# Push images to ECR
	docker push $(DB_SCANNER_REPO):$(VERSION)
	docker push $(LOG_DETECTOR_REPO):$(VERSION)
	docker push $(LOG_DOWNLOADER_REPO):$(VERSION)
	
	# Update Pulumi configuration with the new version
	@echo "Updating Pulumi configuration with version $(VERSION)..."
	$(MAKE) update-pulumi-config VERSION=$(VERSION)

# Update Pulumi configuration with the specified version
update-pulumi-config:
	@echo "Updating Pulumi configuration for backup-solution-stack with version $(VERSION)"
	cd infrastructure/backup-solution-stack && \
	pulumi config set backup-solution:dbScannerImageVersion $(VERSION) && \
	pulumi config set backup-solution:logDetectorImageVersion $(VERSION) && \
	pulumi config set backup-solution:logDownloaderImageVersion $(VERSION)
	@echo "Pulumi configuration updated successfully"

# Build Lambda container images locally
build:
	@echo "Building Lambda container images locally"
	docker build -t aurora-db-scanner:local ./lambdas/dbscanner
	docker build -t aurora-log-detector:local ./lambdas/logdetector
	docker build -t aurora-log-downloader:local ./lambdas/logdownloader

# Get ECR repository URLs from the ECR stack
get-ecr-urls:
	@echo "Getting ECR repository URLs from the ECR stack"
	@cd infrastructure/ecr-stack && pulumi stack output dbScannerRepositoryUrl
	@cd infrastructure/ecr-stack && pulumi stack output logDetectorRepositoryUrl
	@cd infrastructure/ecr-stack && pulumi stack output logDownloaderRepositoryUrl

# Push Lambda container images to ECR
push-images:
	@echo "Pushing Lambda container images to ECR"
	$(eval DB_SCANNER_REPO := $(shell cd infrastructure/ecr-stack && pulumi stack output dbScannerRepositoryUrl))
	$(eval LOG_DETECTOR_REPO := $(shell cd infrastructure/ecr-stack && pulumi stack output logDetectorRepositoryUrl))
	$(eval LOG_DOWNLOADER_REPO := $(shell cd infrastructure/ecr-stack && pulumi stack output logDownloaderRepositoryUrl))
	
	docker tag aurora-db-scanner:local $(DB_SCANNER_REPO):latest
	docker tag aurora-log-detector:local $(LOG_DETECTOR_REPO):latest
	docker tag aurora-log-downloader:local $(LOG_DOWNLOADER_REPO):latest
	
	aws ecr get-login-password --region $$(aws configure get region) | docker login --username AWS --password-stdin $$(echo $(DB_SCANNER_REPO) | cut -d'/' -f1)
	
	docker push $(DB_SCANNER_REPO):latest
	docker push $(LOG_DETECTOR_REPO):latest
	docker push $(LOG_DOWNLOADER_REPO):latest

# Clean up Docker images
clean:
	@echo "Cleaning up Docker images"
	docker rmi -f aurora-db-scanner:local || true
	docker rmi -f aurora-log-detector:local || true
	docker rmi -f aurora-log-downloader:local || true
	docker rmi -f $(shell cd infrastructure/ecr-stack && pulumi stack output dbScannerRepositoryUrl):latest || true
	docker rmi -f $(shell cd infrastructure/ecr-stack && pulumi stack output logDetectorRepositoryUrl):latest || true
	docker rmi -f $(shell cd infrastructure/ecr-stack && pulumi stack output logDownloaderRepositoryUrl):latest || true

# Display version information
version-info:
	@echo "Current Lambda image versions:"
	@echo "DB Scanner: $$(cd infrastructure/backup-solution-stack && pulumi config get backup-solution:dbScannerImageVersion)"
	@echo "Log Detector: $$(cd infrastructure/backup-solution-stack && pulumi config get backup-solution:logDetectorImageVersion)"
	@echo "Log Downloader: $$(cd infrastructure/backup-solution-stack && pulumi config get backup-solution:logDownloaderImageVersion)"

# Increment version (patch level)
increment-version:
	@echo "Incrementing version..."
	$(eval CURRENT_VERSION := $(shell cd infrastructure/backup-solution-stack && pulumi config get backup-solution:dbScannerImageVersion))
	$(eval MAJOR := $(shell echo $(CURRENT_VERSION) | cut -d'.' -f1 | tr -d 'v'))
	$(eval MINOR := $(shell echo $(CURRENT_VERSION) | cut -d'.' -f2))
	$(eval PATCH := $(shell echo $(CURRENT_VERSION) | cut -d'.' -f3))
	$(eval NEW_PATCH := $(shell echo $$(($(PATCH) + 1))))
	$(eval NEW_VERSION := v$(MAJOR).$(MINOR).$(NEW_PATCH))
	@echo "Current version: $(CURRENT_VERSION), New version: $(NEW_VERSION)"
	$(MAKE) build-and-push-versioned VERSION=$(NEW_VERSION)

# Full workflow: build, push, and update configuration
deploy-new-version:
	@echo "Starting full deployment workflow..."
	$(MAKE) increment-version
	@echo "Deployment workflow completed. Run 'pulumi up' in the backup-solution-stack directory to apply changes."

.PHONY: build-and-push-versioned update-pulumi-config build get-ecr-urls push-images clean version-info increment-version deploy-new-version