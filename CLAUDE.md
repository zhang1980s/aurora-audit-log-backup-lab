# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Automated backup solution for Aurora database audit logs. An event-driven pipeline scans Aurora instances, detects new log files, and downloads them to S3. All code is Go. Infrastructure is managed with Pulumi (Go SDK).

## Build and Deploy Commands

### Lambda Container Images

```bash
# Build all three Lambda images locally
make build

# Build, push to ECR, and update Pulumi config with a specific version
make build-and-push-versioned VERSION=v1.0.7

# Auto-increment patch version, build, push, and update config
make deploy-new-version

# Check current image versions
make version-info
```

### Infrastructure (Pulumi)

Each stack is independent and lives under `infrastructure/`. Deploy from within each stack directory:

```bash
cd infrastructure/<stack-name>
pulumi up
```

Stacks must be deployed in this order:
1. `ecr-stack` — ECR repositories for Lambda images
2. `network-stack` — VPC, subnets, VPC endpoints
3. `aurora-cluster-stack` — Aurora MySQL cluster with audit logging
4. `backup-solution-stack` — Lambda functions, DynamoDB, SQS, S3, EventBridge (references network-stack and ecr-stack outputs)
5. `ec2-stress-client-stack` — EC2 instance for stress testing

### Go Modules

Each Lambda, infrastructure stack, and script has its own `go.mod`. There is no top-level module. Run Go commands from within each subdirectory:

```bash
cd lambdas/dbscanner && go build ./...
cd lambdas/dbscanner && go test ./...
```

## Architecture

### Event-Driven Pipeline

```
EventBridge (rate: 15 min)
  → DB Scanner Lambda → SQS queue
    → Log Detector Lambda → DynamoDB (log file metadata)
      → DynamoDB Streams → Log Downloader Lambda → S3 bucket
```

- **DB Scanner** (`lambdas/dbscanner/`): Lists Aurora instances matching configured engine types (aurora-mysql, aurora-postgresql), sends instance IDs to SQS. Supports blacklisting specific instances.
- **Log Detector** (`lambdas/logdetector/`): Receives instance IDs from SQS, detects new/changed audit log files, writes metadata records to DynamoDB with TTL.
- **Log Downloader** (`lambdas/logdownloader/`): Triggered by DynamoDB Streams, downloads log file content from RDS API, uploads to S3.

### Lambda Internal Structure

All three Lambdas follow the same clean architecture layout:

```
cmd/main.go              # Entry point, dependency wiring
internal/
  handler/               # AWS event handler (SQS, DynamoDB Streams, etc.)
  service/               # Business logic
  repository/            # AWS SDK interactions
  models/                # Data structures
config/                  # Environment variable loading
pkg/
  logger/                # Zap structured JSON logger
  errors/                # Custom error types
```

Key conventions:
- AWS SDK v2 with X-Ray instrumentation on all clients
- Zap SugaredLogger for structured JSON logging (level controlled by `LOG_LEVEL` env var)
- Container images built on `provided:al2023-arm64` base, ARM64 architecture
- Lambda versioning with `live` aliases; event sources point to aliases

### Infrastructure Stacks

Pulumi stacks use Go SDK with `resources/` subdirectories for each AWS resource type. Configuration is in `Pulumi.dev.yaml` — no hardcoded values. All resources get mandatory tags (environment, project, owner).

The backup-solution-stack reads ECR repo URLs and VPC config from other stacks via `pulumi.StackReference`.

### Key Config Parameters (backup-solution-stack)

Configured in `infrastructure/backup-solution-stack/Pulumi.dev.yaml`:
- `backupLogTypes`: comma-separated log types to back up (audit, error, instance)
- `instanceEngine`: comma-separated engine filters (aurora-mysql, aurora-postgresql)
- `blackList`: comma-separated instance IDs to skip
- `ttlDays`: DynamoDB record TTL
- `eventBridgeSchedule`: scan frequency (default: rate(15 minutes))
- `*ImageVersion`: semantic version tags for Lambda container images

## Utility Scripts

- `scripts/calculate_log_size/` — Calculate Aurora log file sizes for specific instances
- `scripts/calculate_ddb_log_size/` — Analyze DynamoDB table contents and timing
- `scripts/delete_ddb_records/` — Parallel DynamoDB record deletion with dry-run mode
- `scripts/aurora_stress_test.sh` — Stress testing Aurora with sysbench (setup/run/cleanup modes)

## Development Conventions

- Follow `lambda_golang_prompt.md` for Lambda function patterns (DI, error wrapping, X-Ray subsegments, Zap logging)
- Follow `pulumi_prompt.md` for infrastructure patterns (centralized tags in `utils/tags.go`, config validation, resource ordering)
- Lambda images use semantic versioning (current: v1.0.6); all three Lambdas are versioned in lockstep
- Go 1.24.4; Pulumi AWS SDK v6; Pulumi SDK v3
