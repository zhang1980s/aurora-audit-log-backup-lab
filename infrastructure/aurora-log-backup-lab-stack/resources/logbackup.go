package resources

import (
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/dynamodb"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"aurora-audit-log-backup-lab/config"
	"aurora-audit-log-backup-lab/utils"
)

// LogBackupResources holds all the resources for the log backup solution
type LogBackupResources struct {
	LogBucket                *s3.Bucket
	DynamoDBTable            *dynamodb.Table
	SQSQueue                 *sqs.Queue
	LambdaRole               *iam.Role
	DBScannerLambda          *lambda.Function
	DBScannerLambdaAlias     *lambda.Alias
	LogDetectorLambda        *lambda.Function
	LogDetectorLambdaAlias   *lambda.Alias
	LogDownloaderLambda      *lambda.Function
	LogDownloaderLambdaAlias *lambda.Alias
	EventBridgeRule          *cloudwatch.EventRule
}

// CreateLogBackupResources creates all the resources for the log backup solution
func CreateLogBackupResources(ctx *pulumi.Context, cfg *config.Config, networkResources *NetworkResources, ecrStack *pulumi.StackReference) (*LogBackupResources, error) {
	// Get ECR repository URLs from ECR stack
	dbScannerRepoUrl := ecrStack.GetOutput(pulumi.String("dbScannerRepositoryUrl"))
	logDetectorRepoUrl := ecrStack.GetOutput(pulumi.String("logDetectorRepositoryUrl"))
	logDownloaderRepoUrl := ecrStack.GetOutput(pulumi.String("logDownloaderRepositoryUrl"))

	// Create S3 bucket for log backups
	logBucket, err := s3.NewBucket(ctx, "aurora-log-backup-bucket", &s3.BucketArgs{
		Acl:  pulumi.String("private"),
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "aurora-log-backup"),
		// Configure server-side encryption
		ServerSideEncryptionConfiguration: &s3.BucketServerSideEncryptionConfigurationArgs{
			Rule: &s3.BucketServerSideEncryptionConfigurationRuleArgs{
				ApplyServerSideEncryptionByDefault: &s3.BucketServerSideEncryptionConfigurationRuleApplyServerSideEncryptionByDefaultArgs{
					SseAlgorithm: pulumi.String("AES256"),
				},
			},
		},
		// Configure lifecycle rules for log retention
		LifecycleRules: s3.BucketLifecycleRuleArray{
			&s3.BucketLifecycleRuleArgs{
				Id:      pulumi.String("expire-old-logs"),
				Enabled: pulumi.Bool(true),
				Expiration: &s3.BucketLifecycleRuleExpirationArgs{
					Days: pulumi.Int(90), // Keep logs for 90 days
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	// Create DynamoDB table for tracking log files
	dynamoTable, err := dynamodb.NewTable(ctx, "aurora-log-files", &dynamodb.TableArgs{
		Attributes: dynamodb.TableAttributeArray{
			&dynamodb.TableAttributeArgs{
				Name: pulumi.String("DBInstanceIdentifier"),
				Type: pulumi.String("S"),
			},
			&dynamodb.TableAttributeArgs{
				Name: pulumi.String("LogFileName"),
				Type: pulumi.String("S"),
			},
		},
		HashKey:        pulumi.String("DBInstanceIdentifier"),
		RangeKey:       pulumi.String("LogFileName"),
		BillingMode:    pulumi.String("PAY_PER_REQUEST"),
		StreamEnabled:  pulumi.Bool(true),
		StreamViewType: pulumi.String("NEW_AND_OLD_IMAGES"),
		Tags:           utils.CreateResourceTags(ctx, cfg.Tags, "aurora-log-files"),
	})
	if err != nil {
		return nil, err
	}

	// Create SQS queue for DB instance IDs
	queue, err := sqs.NewQueue(ctx, "aurora-db-instances", &sqs.QueueArgs{
		VisibilityTimeoutSeconds: pulumi.Int(300),   // 5 minutes
		MessageRetentionSeconds:  pulumi.Int(86400), // 24 hours
		Tags:                     utils.CreateResourceTags(ctx, cfg.Tags, "aurora-db-instances"),
	})
	if err != nil {
		return nil, err
	}

	// Create IAM role for Lambda functions
	lambdaRole, err := iam.NewRole(ctx, "aurora-log-backup-lambda-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Action": "sts:AssumeRole",
				"Principal": {
					"Service": "lambda.amazonaws.com"
				},
				"Effect": "Allow",
				"Sid": ""
			}]
		}`),
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "aurora-log-backup-lambda-role"),
	})
	if err != nil {
		return nil, err
	}

	// Attach policies to Lambda role
	_, err = iam.NewRolePolicyAttachment(ctx, "lambda-basic-execution", &iam.RolePolicyAttachmentArgs{
		Role:      lambdaRole.Name,
		PolicyArn: pulumi.String("arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"),
	})
	if err != nil {
		return nil, err
	}

	// Create custom policy for Lambda functions with least privilege
	lambdaPolicy, err := iam.NewPolicy(ctx, "aurora-log-backup-lambda-policy", &iam.PolicyArgs{
		Description: pulumi.String("Policy for Aurora log backup Lambda functions"),
		Policy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Action": [
						"rds:DescribeDBInstances",
						"rds:DescribeDBLogFiles",
						"rds:DownloadDBLogFilePortion",
						"rds:DownloadCompleteDBLogFile"
					],
					"Resource": "*"
				},
				{
					"Effect": "Allow",
					"Action": [
						"dynamodb:GetItem",
						"dynamodb:PutItem",
						"dynamodb:UpdateItem",
						"dynamodb:Query",
						"dynamodb:Scan",
						"dynamodb:GetRecords",
						"dynamodb:GetShardIterator",
						"dynamodb:DescribeStream",
						"dynamodb:ListStreams"
					],
					"Resource": "*"
				},
				{
					"Effect": "Allow",
					"Action": [
						"sqs:SendMessage",
						"sqs:ReceiveMessage",
						"sqs:DeleteMessage",
						"sqs:GetQueueAttributes"
					],
					"Resource": "*"
				},
				{
					"Effect": "Allow",
					"Action": [
						"s3:PutObject",
						"s3:GetObject",
						"s3:ListBucket"
					],
					"Resource": [
						"*"
					]
				},
				{
					"Effect": "Allow",
					"Action": [
						"ec2:CreateNetworkInterface",
						"ec2:DescribeNetworkInterfaces",
						"ec2:DeleteNetworkInterface",
						"ec2:AssignPrivateIpAddresses",
						"ec2:UnassignPrivateIpAddresses",
						"ec2:DescribeSubnets",
						"ec2:DescribeSecurityGroups",
						"ec2:DescribeVpcs"
					],
					"Resource": "*"
				},
				{
					"Effect": "Allow",
					"Action": [
						"xray:PutTraceSegments",
						"xray:PutTelemetryRecords",
						"xray:GetSamplingRules",
						"xray:GetSamplingTargets",
						"xray:GetSamplingStatisticSummaries"
					],
					"Resource": "*"
				}
			]
		}`),
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "aurora-log-backup-lambda-policy"),
	})
	if err != nil {
		return nil, err
	}

	// Attach custom policy to Lambda role
	_, err = iam.NewRolePolicyAttachment(ctx, "lambda-custom-policy", &iam.RolePolicyAttachmentArgs{
		Role:      lambdaRole.Name,
		PolicyArn: lambdaPolicy.Arn,
	})
	if err != nil {
		return nil, err
	}

	// Create security group for Lambda functions
	lambdaSecurityGroup, err := ec2.NewSecurityGroup(ctx, "lambda-sg", &ec2.SecurityGroupArgs{
		VpcId:       networkResources.Vpc.ID(),
		Description: pulumi.String("Security group for Lambda functions"),
		Egress: ec2.SecurityGroupEgressArray{
			&ec2.SecurityGroupEgressArgs{
				Protocol:    pulumi.String("-1"),
				FromPort:    pulumi.Int(0),
				ToPort:      pulumi.Int(0),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				Description: pulumi.String("Allow all outbound traffic"),
			},
		},
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "lambda-sg"),
	})
	if err != nil {
		return nil, err
	}

	// Create DB Scanner Lambda function with container image
	dbScannerLambda, err := lambda.NewFunction(ctx, "aurora-db-scanner", &lambda.FunctionArgs{
		PackageType: pulumi.String("Image"),
		ImageUri:    pulumi.Sprintf("%s:%s", dbScannerRepoUrl, cfg.Images.DBScannerVersion),
		Role:        lambdaRole.Arn,
		MemorySize:  pulumi.Int(cfg.Lambda.DBScannerMemory),
		Timeout:     pulumi.Int(cfg.Lambda.DBScannerTimeout),
		Publish:     pulumi.Bool(cfg.Lambda.PublishVersions),
		Description: pulumi.Sprintf("Aurora DB Scanner Lambda - Version %s", cfg.Images.DBScannerVersion),
		Architectures: pulumi.StringArray{
			pulumi.String("arm64"),
		},
		VpcConfig: &lambda.FunctionVpcConfigArgs{
			SubnetIds: pulumi.StringArray{
				networkResources.PrivateSubnet1.ID(),
				networkResources.PrivateSubnet2.ID(),
			},
			SecurityGroupIds: pulumi.StringArray{
				lambdaSecurityGroup.ID(),
			},
		},
		Environment: &lambda.FunctionEnvironmentArgs{
			Variables: pulumi.StringMap{
				"SQS_QUEUE_URL": queue.Url,
				"LOG_LEVEL":     pulumi.String("error"),
			},
		},
		TracingConfig: &lambda.FunctionTracingConfigArgs{
			Mode: pulumi.String("Active"),
		},
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "aurora-db-scanner"),
	})
	if err != nil {
		return nil, err
	}

	// Create an alias for the DB Scanner Lambda
	dbScannerAlias, err := lambda.NewAlias(ctx, "aurora-db-scanner-alias", &lambda.AliasArgs{
		FunctionName:    dbScannerLambda.Name,
		FunctionVersion: pulumi.String("$LATEST"), // Use $LATEST or a specific version
		Name:            pulumi.String("live"),
		Description:     pulumi.String("Production alias for Aurora DB Scanner Lambda"),
	}, pulumi.DependsOn([]pulumi.Resource{dbScannerLambda}))
	if err != nil {
		return nil, err
	}

	// Create Log Detector Lambda function with container image
	logDetectorLambda, err := lambda.NewFunction(ctx, "aurora-log-detector", &lambda.FunctionArgs{
		PackageType: pulumi.String("Image"),
		ImageUri:    pulumi.Sprintf("%s:%s", logDetectorRepoUrl, cfg.Images.LogDetectorVersion),
		Role:        lambdaRole.Arn,
		MemorySize:  pulumi.Int(cfg.Lambda.LogDetectorMemory),
		Timeout:     pulumi.Int(cfg.Lambda.LogDetectorTimeout),
		Publish:     pulumi.Bool(cfg.Lambda.PublishVersions),
		Description: pulumi.Sprintf("Aurora Log Detector Lambda - Version %s", cfg.Images.LogDetectorVersion),
		Architectures: pulumi.StringArray{
			pulumi.String("arm64"),
		},
		VpcConfig: &lambda.FunctionVpcConfigArgs{
			SubnetIds: pulumi.StringArray{
				networkResources.PrivateSubnet1.ID(),
				networkResources.PrivateSubnet2.ID(),
			},
			SecurityGroupIds: pulumi.StringArray{
				lambdaSecurityGroup.ID(),
			},
		},
		Environment: &lambda.FunctionEnvironmentArgs{
			Variables: pulumi.StringMap{
				"DYNAMODB_TABLE_NAME": dynamoTable.Name,
				"LOG_LEVEL":           pulumi.String("error"),
				"BACKUP_LOGS":         pulumi.String(cfg.Lambda.BackupLogTypes),
			},
		},
		TracingConfig: &lambda.FunctionTracingConfigArgs{
			Mode: pulumi.String("Active"),
		},
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "aurora-log-detector"),
	})
	if err != nil {
		return nil, err
	}

	// Create an alias for the Log Detector Lambda
	logDetectorAlias, err := lambda.NewAlias(ctx, "aurora-log-detector-alias", &lambda.AliasArgs{
		FunctionName:    logDetectorLambda.Name,
		FunctionVersion: pulumi.String("$LATEST"), // Use $LATEST or a specific version
		Name:            pulumi.String("live"),
		Description:     pulumi.String("Production alias for Aurora Log Detector Lambda"),
	}, pulumi.DependsOn([]pulumi.Resource{logDetectorLambda}))
	if err != nil {
		return nil, err
	}

	// Create Log Downloader Lambda function with container image
	logDownloaderLambda, err := lambda.NewFunction(ctx, "aurora-log-downloader", &lambda.FunctionArgs{
		PackageType: pulumi.String("Image"),
		ImageUri:    pulumi.Sprintf("%s:%s", logDownloaderRepoUrl, cfg.Images.LogDownloaderVersion),
		Role:        lambdaRole.Arn,
		MemorySize:  pulumi.Int(cfg.Lambda.LogDownloaderMemory),
		Timeout:     pulumi.Int(cfg.Lambda.LogDownloaderTimeout),
		Publish:     pulumi.Bool(cfg.Lambda.PublishVersions),
		Description: pulumi.Sprintf("Aurora Log Downloader Lambda - Version %s", cfg.Images.LogDownloaderVersion),
		Architectures: pulumi.StringArray{
			pulumi.String("arm64"),
		},
		VpcConfig: &lambda.FunctionVpcConfigArgs{
			SubnetIds: pulumi.StringArray{
				networkResources.PrivateSubnet1.ID(),
				networkResources.PrivateSubnet2.ID(),
			},
			SecurityGroupIds: pulumi.StringArray{
				lambdaSecurityGroup.ID(),
			},
		},
		Environment: &lambda.FunctionEnvironmentArgs{
			Variables: pulumi.StringMap{
				"DYNAMODB_TABLE_NAME": dynamoTable.Name,
				"S3_BUCKET_NAME":      logBucket.ID(),
				"S3_PREFIX":           pulumi.String(cfg.Lambda.S3LogPrefix),
				"LOG_LEVEL":           pulumi.String("error"),
			},
		},
		TracingConfig: &lambda.FunctionTracingConfigArgs{
			Mode: pulumi.String("Active"),
		},
		Tags: utils.CreateResourceTags(ctx, cfg.Tags, "aurora-log-downloader"),
	})
	if err != nil {
		return nil, err
	}

	// Create an alias for the Log Downloader Lambda
	logDownloaderAlias, err := lambda.NewAlias(ctx, "aurora-log-downloader-alias", &lambda.AliasArgs{
		FunctionName:    logDownloaderLambda.Name,
		FunctionVersion: pulumi.String("$LATEST"), // Use $LATEST or a specific version
		Name:            pulumi.String("live"),
		Description:     pulumi.String("Production alias for Aurora Log Downloader Lambda"),
	}, pulumi.DependsOn([]pulumi.Resource{logDownloaderLambda}))
	if err != nil {
		return nil, err
	}

	// Create EventBridge rule to trigger DB Scanner Lambda (initially disabled)
	eventRule, err := cloudwatch.NewEventRule(ctx, "aurora-db-scanner-schedule", &cloudwatch.EventRuleArgs{
		ScheduleExpression: pulumi.String(cfg.Lambda.EventBridgeSchedule),
		Description:        pulumi.String("Trigger Aurora DB Scanner Lambda every 15 minutes"),
		Tags:               utils.CreateResourceTags(ctx, cfg.Tags, "aurora-db-scanner-schedule"),
		State:              pulumi.String("DISABLED"), // Create the rule in disabled state
	})
	if err != nil {
		return nil, err
	}

	// Add EventBridge target for DB Scanner Lambda (using alias)
	_, err = cloudwatch.NewEventTarget(ctx, "aurora-db-scanner-target", &cloudwatch.EventTargetArgs{
		Rule: eventRule.Name,
		Arn:  dbScannerAlias.Arn, // Use alias ARN instead of function ARN
	}, pulumi.DependsOn([]pulumi.Resource{dbScannerAlias}))
	if err != nil {
		return nil, err
	}

	// Allow EventBridge to invoke DB Scanner Lambda (using alias)
	_, err = lambda.NewPermission(ctx, "aurora-db-scanner-permission", &lambda.PermissionArgs{
		Action:    pulumi.String("lambda:InvokeFunction"),
		Function:  dbScannerLambda.Name,
		Qualifier: dbScannerAlias.Name, // Add qualifier for the alias
		Principal: pulumi.String("events.amazonaws.com"),
		SourceArn: eventRule.Arn,
	}, pulumi.DependsOn([]pulumi.Resource{dbScannerAlias}))
	if err != nil {
		return nil, err
	}

	// Create SQS event source mapping for Log Detector Lambda (using alias)
	_, err = lambda.NewEventSourceMapping(ctx, "aurora-log-detector-sqs-mapping", &lambda.EventSourceMappingArgs{
		EventSourceArn: queue.Arn,
		FunctionName:   logDetectorAlias.Arn, // Use alias ARN instead of function ARN
		BatchSize:      pulumi.Int(cfg.Lambda.BatchSize),
	}, pulumi.DependsOn([]pulumi.Resource{logDetectorAlias}))
	if err != nil {
		return nil, err
	}

	// Create DynamoDB event source mapping for Log Downloader Lambda (using alias)
	_, err = lambda.NewEventSourceMapping(ctx, "aurora-log-downloader-dynamodb-mapping", &lambda.EventSourceMappingArgs{
		EventSourceArn:   dynamoTable.StreamArn,
		FunctionName:     logDownloaderAlias.Arn, // Use alias ARN instead of function ARN
		StartingPosition: pulumi.String("LATEST"),
		BatchSize:        pulumi.Int(cfg.Lambda.BatchSize),
	}, pulumi.DependsOn([]pulumi.Resource{logDownloaderAlias}))
	if err != nil {
		return nil, err
	}

	return &LogBackupResources{
		LogBucket:                logBucket,
		DynamoDBTable:            dynamoTable,
		SQSQueue:                 queue,
		LambdaRole:               lambdaRole,
		DBScannerLambda:          dbScannerLambda,
		DBScannerLambdaAlias:     dbScannerAlias,
		LogDetectorLambda:        logDetectorLambda,
		LogDetectorLambdaAlias:   logDetectorAlias,
		LogDownloaderLambda:      logDownloaderLambda,
		LogDownloaderLambdaAlias: logDownloaderAlias,
		EventBridgeRule:          eventRule,
	}, nil
}
