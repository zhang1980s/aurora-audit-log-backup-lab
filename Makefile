.PHONY: build clean push-images get-ecr-urls

# Build Docker images for Lambda functions
build:
	@echo "Building Lambda Docker images..."
	@echo "Building DB Scanner Lambda image..."
	docker build -t aurora-db-scanner:latest ./lambdas/dbscanner
	@echo "Building Log Detector Lambda image..."
	docker build -t aurora-log-detector:latest ./lambdas/logdetector
	@echo "Building Log Downloader Lambda image..."
	docker build -t aurora-log-downloader:latest ./lambdas/logdownloader
	@echo "Lambda Docker images built successfully!"

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
	
	@echo "Tagging and pushing DB Scanner image..."
	docker tag aurora-db-scanner:latest $(DB_SCANNER_REPO):latest
	docker push $(DB_SCANNER_REPO):latest
	
	@echo "Tagging and pushing Log Detector image..."
	docker tag aurora-log-detector:latest $(LOG_DETECTOR_REPO):latest
	docker push $(LOG_DETECTOR_REPO):latest
	
	@echo "Tagging and pushing Log Downloader image..."
	docker tag aurora-log-downloader:latest $(LOG_DOWNLOADER_REPO):latest
	docker push $(LOG_DOWNLOADER_REPO):latest
	
	@echo "All images pushed successfully!"

# Clean build artifacts
clean:
	@echo "Cleaning Docker images..."
	docker rmi -f aurora-db-scanner:latest || true
	docker rmi -f aurora-log-detector:latest || true
	docker rmi -f aurora-log-downloader:latest || true
	docker rmi -f $(DB_SCANNER_REPO):latest || true
	docker rmi -f $(LOG_DETECTOR_REPO):latest || true
	docker rmi -f $(LOG_DOWNLOADER_REPO):latest || true
	@echo "Clean complete!"

# Build and push workflow
build-and-push: build get-ecr-urls push-images
	@echo "Build and push completed successfully!"