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
	
	# Update Pulumi configuration with new version
	cd infrastructure/backup-solution-stack && \
	pulumi config set backup-solution:dbScannerImageVersion $(VERSION) && \
	pulumi config set backup-solution:logDetectorImageVersion $(VERSION) && \
	pulumi config set backup-solution:logDownloaderImageVersion $(VERSION)

# Deploy all stacks in order
deploy-all: deploy-ecr deploy-network deploy-aurora deploy-backup deploy-ec2

# Deploy individual stacks
deploy-ecr:
	cd infrastructure/ecr-stack && pulumi up --yes

deploy-network:
	cd infrastructure/network-stack && pulumi up --yes

deploy-aurora: deploy-network
	cd infrastructure/aurora-cluster-stack && pulumi up --yes

deploy-backup: deploy-network
	cd infrastructure/backup-solution-stack && pulumi up --yes

deploy-ec2: deploy-network deploy-aurora
	cd infrastructure/ec2-testing-stack && pulumi up --yes

# Destroy all stacks in reverse order
destroy-all: destroy-ec2 destroy-backup destroy-aurora destroy-network destroy-ecr

# Destroy individual stacks
destroy-ec2:
	cd infrastructure/ec2-testing-stack && pulumi destroy --yes

destroy-backup:
	cd infrastructure/backup-solution-stack && pulumi destroy --yes

destroy-aurora:
	cd infrastructure/aurora-cluster-stack && pulumi destroy --yes

destroy-network:
	cd infrastructure/network-stack && pulumi destroy --yes

destroy-ecr:
	cd infrastructure/ecr-stack && pulumi destroy --yes

# Initialize all stacks
init-all: init-ecr init-network init-aurora init-backup init-ec2

# Initialize individual stacks
init-ecr:
	cd infrastructure/ecr-stack && pulumi stack init dev

init-network:
	cd infrastructure/network-stack && pulumi stack init dev

init-aurora:
	cd infrastructure/aurora-cluster-stack && pulumi stack init dev

init-backup:
	cd infrastructure/backup-solution-stack && pulumi stack init dev

init-ec2:
	cd infrastructure/ec2-testing-stack && pulumi stack init dev

.PHONY: build-and-push-versioned deploy-all deploy-ecr deploy-network deploy-aurora deploy-backup deploy-ec2 destroy-all destroy-ec2 destroy-backup destroy-aurora destroy-network destroy-ecr init-all init-ecr init-network init-aurora init-backup init-ec2