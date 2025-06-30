#!/bin/bash
# Script to build Lambda functions for Aurora Audit Log Backup

set -e

echo "Building Lambda functions..."

# Create build directory
mkdir -p build

# Build DB Scanner Lambda
echo "Building DB Scanner Lambda..."
cd lambdas/dbscanner
GOOS=linux GOARCH=amd64 go build -o main main.go
zip main.zip main
mv main.zip ../../build/dbscanner.zip
rm main
cd ../..

# Build Log Detector Lambda
echo "Building Log Detector Lambda..."
cd lambdas/logdetector
GOOS=linux GOARCH=amd64 go build -o main main.go
zip main.zip main
mv main.zip ../../build/logdetector.zip
rm main
cd ../..

# Build Log Downloader Lambda
echo "Building Log Downloader Lambda..."
cd lambdas/logdownloader
GOOS=linux GOARCH=amd64 go build -o main main.go
zip main.zip main
mv main.zip ../../build/logdownloader.zip
rm main
cd ../..

echo "Lambda functions built successfully!"
echo "ZIP files are in the build directory:"
ls -la build/

echo "Done!"