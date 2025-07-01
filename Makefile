.PHONY: build clean push-images get-ecr-urls update-pulumi-config build-and-push-versioned

# Default version if not specified
VERSION ?= latest

# Build Docker images for Lambda functions
build:
	@echo "Building Lambda Docker images with version $(VERSION)..."
	@echo "Building DB Scanner Lambda image..."
	docker build -t aurora-db-scanner:$(VERSION) ./lambdas/dbscanner
	@echo "Building Log Detector Lambda image..."
	docker build -t aurora-log-detector:$(VERSION) ./lambdas/logdetector
	@echo "Building Log Downloader Lambda image..."
	docker build -t aurora-log-downloader:$(VERSION) ./lambdas/logdownloader
	@echo "Lambda Docker images built successfully with version $(VERSION)!"

# Get ECR repository URLs from ECR stack outputs
get-ecr-urls:
	@echo "Getting ECR repository URLs from ECR stack..."
	$(eval DB_SCANNER_REPO=$(shell cd infrastructure/ecr-stack && pulumi stack output dbScannerRepositoryUrl))
	$(eval LOG_DETECTOR_REPO=$(shell cd infrastructure/ecr-stack && pulumi stack output logDetectorRepositoryUrl))
	$(eval LOG_DOWNLOADER_REPO=$(shell cd infrastructure/ecr-stack && pulumi stack output logDownloaderRepositoryUrl))
	@echo "DB Scanner Repository: $(DB_SCANNER_REPO)"
	@echo "Log Detector Repository: $(LOG_DETECTOR_REPO)"
	@echo "Log Downloader Repository: $(LOG_DOWNLOADER_REPO)"

# Push Docker images to ECR
push-images: get-ecr-urls
	@echo "Logging in to ECR..."
	aws ecr get-login-password --region $$(aws configure get region) | docker login --username AWS --password-stdin $$(echo $(DB_SCANNER_REPO) | cut -d'/' -f1)
	
	@echo "Tagging and pushing DB Scanner image with version $(VERSION)..."
	docker tag aurora-db-scanner:$(VERSION) $(DB_SCANNER_REPO):$(VERSION)
	docker push $(DB_SCANNER_REPO):$(VERSION)
	
	@echo "Tagging and pushing Log Detector image with version $(VERSION)..."
	docker tag aurora-log-detector:$(VERSION) $(LOG_DETECTOR_REPO):$(VERSION)
	docker push $(LOG_DETECTOR_REPO):$(VERSION)
	
	@echo "Tagging and pushing Log Downloader image with version $(VERSION)..."
	docker tag aurora-log-downloader:$(VERSION) $(LOG_DOWNLOADER_REPO):$(VERSION)
	docker push $(LOG_DOWNLOADER_REPO):$(VERSION)
	
	@echo "All images pushed successfully with version $(VERSION)!"

# Clean build artifacts
clean:
	@echo "Cleaning Docker images..."
	docker rmi -f aurora-db-scanner:$(VERSION) || true
	docker rmi -f aurora-log-detector:$(VERSION) || true
	docker rmi -f aurora-log-downloader:$(VERSION) || true
	docker rmi -f $(DB_SCANNER_REPO):$(VERSION) || true
	docker rmi -f $(LOG_DETECTOR_REPO):$(VERSION) || true
	docker rmi -f $(LOG_DOWNLOADER_REPO):$(VERSION) || true
	@echo "Clean complete!"

# Update Pulumi config with new image versions
update-pulumi-config:
	@echo "Updating Pulumi config with version $(VERSION)..."
	cd infrastructure/aurora-log-backup-lab-stack && \
	pulumi config set aurora-audit-log-backup-lab:dbScannerImageVersion $(VERSION) && \
	pulumi config set aurora-audit-log-backup-lab:logDetectorImageVersion $(VERSION) && \
	pulumi config set aurora-audit-log-backup-lab:logDownloaderImageVersion $(VERSION)
	@echo "Pulumi config updated successfully!"

# Build and push workflow
build-and-push: build get-ecr-urls push-images
	@echo "Build and push completed successfully!"

# Build, push, and update config workflow
build-and-push-versioned: build get-ecr-urls push-images update-pulumi-config
	@echo "Build and push with version $(VERSION) completed successfully!"