.PHONY: build clean deploy

# Build all Lambda functions
build:
	@echo "Building Lambda functions..."
	@mkdir -p build
	@echo "Building DB Scanner Lambda..."
	cd lambdas/dbscanner && GOOS=linux GOARCH=amd64 go build -o ../../build/dbscanner/main main.go
	@echo "Building Log Detector Lambda..."
	cd lambdas/logdetector && GOOS=linux GOARCH=amd64 go build -o ../../build/logdetector/main main.go
	@echo "Building Log Downloader Lambda..."
	cd lambdas/logdownloader && GOOS=linux GOARCH=amd64 go build -o ../../build/logdownloader/main main.go
	@echo "Lambda functions built successfully!"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf build
	@echo "Clean complete!"

# Deploy infrastructure with Pulumi
deploy: build
	@echo "Deploying infrastructure with Pulumi..."
	cd infrastructure && pulumi up
	@echo "Deployment complete!"

# Preview infrastructure changes
preview: build
	@echo "Previewing infrastructure changes with Pulumi..."
	cd infrastructure && pulumi preview
	@echo "Preview complete!"

# Destroy infrastructure
destroy:
	@echo "Destroying infrastructure with Pulumi..."
	cd infrastructure && pulumi destroy
	@echo "Destruction complete!"