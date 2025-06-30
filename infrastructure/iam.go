package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// IamResources holds all the IAM resources
type IamResources struct {
	ec2Role            *iam.Role
	ec2InstanceProfile *iam.InstanceProfile
	auroraRole         *iam.Role
}

// createIamResources creates all IAM roles and policies
func createIamResources(ctx *pulumi.Context, vpcId pulumi.IDOutput) (*IamResources, error) {
	// Create EC2 role
	ec2Role, err := iam.NewRole(ctx, "ec2-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Action": "sts:AssumeRole",
				"Principal": {
					"Service": "ec2.amazonaws.com"
				},
				"Effect": "Allow",
				"Sid": ""
			}]
		}`),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-ec2-role"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Attach SSM policy to EC2 role
	_, err = iam.NewRolePolicyAttachment(ctx, "ec2-ssm-policy", &iam.RolePolicyAttachmentArgs{
		Role:      ec2Role.Name,
		PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"),
	})
	if err != nil {
		return nil, err
	}

	// Create policy for RDS IAM authentication
	rdsAuthPolicy, err := iam.NewPolicy(ctx, "rds-auth-policy", &iam.PolicyArgs{
		Description: pulumi.String("Policy for RDS IAM authentication"),
		Policy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Action": [
					"rds-db:connect"
				],
				"Effect": "Allow",
				"Resource": "*"
			}]
		}`),
	})
	if err != nil {
		return nil, err
	}

	// Attach RDS auth policy to EC2 role
	_, err = iam.NewRolePolicyAttachment(ctx, "ec2-rds-auth-policy", &iam.RolePolicyAttachmentArgs{
		Role:      ec2Role.Name,
		PolicyArn: rdsAuthPolicy.Arn,
	})
	if err != nil {
		return nil, err
	}

	// Create policy for S3 access (with VPC Endpoint conditions)
	s3AccessPolicy, err := iam.NewPolicy(ctx, "s3-access-policy", &iam.PolicyArgs{
		Description: pulumi.String("Policy for S3 access to audit logs via VPC Endpoint"),
		Policy: vpcId.ApplyT(func(id string) string {
			return `{
				"Version": "2012-10-17",
				"Statement": [
					{
						"Action": [
							"s3:GetObject",
							"s3:PutObject",
							"s3:ListBucket"
						],
						"Effect": "Allow",
						"Resource": [
							"arn:aws:s3:::zzhe-aurora-audit-log-lab-bucket",
							"arn:aws:s3:::zzhe-aurora-audit-log-lab-bucket/*"
						],
						"Condition": {
							"StringEquals": {
								"aws:SourceVpc": "` + id + `"
							}
						}
					},
					{
						"Action": [
							"s3:ListAllMyBuckets"
						],
						"Effect": "Allow",
						"Resource": "*",
						"Condition": {
							"StringEquals": {
								"aws:SourceVpc": "` + id + `"
							}
						}
					}
				]
			}`
		}).(pulumi.StringOutput),
	})
	if err != nil {
		return nil, err
	}

	// Attach S3 access policy to EC2 role
	_, err = iam.NewRolePolicyAttachment(ctx, "ec2-s3-access-policy", &iam.RolePolicyAttachmentArgs{
		Role:      ec2Role.Name,
		PolicyArn: s3AccessPolicy.Arn,
	})
	if err != nil {
		return nil, err
	}

	// Create policy for RDS describe access
	rdsDescribePolicy, err := iam.NewPolicy(ctx, "rds-describe-policy", &iam.PolicyArgs{
		Description: pulumi.String("Policy for describing RDS resources"),
		Policy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Action": [
					"rds:DescribeDBClusters",
					"rds:DescribeDBClusterParameters",
					"rds:DescribeDBClusterParameterGroups"
				],
				"Effect": "Allow",
				"Resource": "*"
			}]
		}`),
	})
	if err != nil {
		return nil, err
	}

	// Attach RDS describe policy to EC2 role
	_, err = iam.NewRolePolicyAttachment(ctx, "ec2-rds-describe-policy", &iam.RolePolicyAttachmentArgs{
		Role:      ec2Role.Name,
		PolicyArn: rdsDescribePolicy.Arn,
	})
	if err != nil {
		return nil, err
	}

	// Create policy for SSM Parameter Store access
	ssmPolicy, err := iam.NewPolicy(ctx, "ssm-parameter-policy", &iam.PolicyArgs{
		Description: pulumi.String("Policy for accessing SSM Parameter Store"),
		Policy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Action": [
					"ssm:GetParameter",
					"ssm:GetParameters",
					"ssm:GetParametersByPath"
				],
				"Effect": "Allow",
				"Resource": "arn:aws:ssm:*:*:parameter/aurora-audit-log-lab/*"
			}]
		}`),
	})
	if err != nil {
		return nil, err
	}

	// Attach SSM Parameter Store policy to EC2 role
	_, err = iam.NewRolePolicyAttachment(ctx, "ec2-ssm-parameter-policy", &iam.RolePolicyAttachmentArgs{
		Role:      ec2Role.Name,
		PolicyArn: ssmPolicy.Arn,
	})
	if err != nil {
		return nil, err
	}

	// Create EC2 instance profile
	ec2InstanceProfile, err := iam.NewInstanceProfile(ctx, "ec2-instance-profile", &iam.InstanceProfileArgs{
		Role: ec2Role.Name,
	})
	if err != nil {
		return nil, err
	}

	// Create Aurora role
	auroraRole, err := iam.NewRole(ctx, "aurora-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Action": "sts:AssumeRole",
				"Principal": {
					"Service": "rds.amazonaws.com"
				},
				"Effect": "Allow",
				"Sid": ""
			}]
		}`),
		Tags: pulumi.StringMap{
			"Name": pulumi.String("aurora-service-role"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Attach S3 access policy to Aurora role
	_, err = iam.NewRolePolicyAttachment(ctx, "aurora-s3-access-policy", &iam.RolePolicyAttachmentArgs{
		Role:      auroraRole.Name,
		PolicyArn: s3AccessPolicy.Arn,
	})
	if err != nil {
		return nil, err
	}

	return &IamResources{
		ec2Role:            ec2Role,
		ec2InstanceProfile: ec2InstanceProfile,
		auroraRole:         auroraRole,
	}, nil
}
