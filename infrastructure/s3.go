package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// S3Resources holds all the S3 resources
type S3Resources struct {
	bucket *s3.Bucket
}

// createS3Resources creates the S3 bucket for audit logs
func createS3Resources(ctx *pulumi.Context) (*S3Resources, error) {
	// Use fixed bucket name
	bucketName := pulumi.String("zzhe-aurora-audit-log-lab-bucket")

	// Create S3 bucket for audit logs
	bucket, err := s3.NewBucket(ctx, "audit-logs-bucket", &s3.BucketArgs{
		Bucket: bucketName,
		Acl:    pulumi.String("private"),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-audit-logs"),
		},
		// Configure server-side encryption
		ServerSideEncryptionConfiguration: &s3.BucketServerSideEncryptionConfigurationArgs{
			Rule: &s3.BucketServerSideEncryptionConfigurationRuleArgs{
				ApplyServerSideEncryptionByDefault: &s3.BucketServerSideEncryptionConfigurationRuleApplyServerSideEncryptionByDefaultArgs{
					SseAlgorithm: pulumi.String("AES256"),
				},
			},
		},
		// Configure lifecycle rules for log retention (optional)
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

	// Create bucket policy to allow access from Aurora via VPC Endpoint
	_, err = s3.NewBucketPolicy(ctx, "audit-logs-bucket-policy", &s3.BucketPolicyArgs{
		Bucket: bucket.ID(),
		Policy: pulumi.All(bucket.Arn).ApplyT(func(args []interface{}) string {
			bucketArn := args[0].(string)
			return `{
				"Version": "2012-10-17",
				"Statement": [
					{
						"Effect": "Allow",
						"Principal": {
							"Service": "rds.amazonaws.com"
						},
						"Action": [
							"s3:PutObject",
							"s3:GetObject"
						],
						"Resource": "` + bucketArn + `/*"
					}
				]
			}`
		}).(pulumi.StringOutput),
	})
	if err != nil {
		return nil, err
	}

	return &S3Resources{
		bucket: bucket,
	}, nil
}
