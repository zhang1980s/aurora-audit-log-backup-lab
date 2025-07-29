package resources

import (
	"os"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"ec2-stress-client-stack/config"
	"ec2-stress-client-stack/utils"
)

// EC2Resources holds all the resources for the EC2 stress client environment
type EC2Resources struct {
	EC2SecurityGroup   *ec2.SecurityGroup
	EC2Role            *iam.Role
	EC2InstanceProfile *iam.InstanceProfile
	EC2Instance        *ec2.Instance
}

// CreateEC2Resources creates the EC2 instance and related resources
func CreateEC2Resources(ctx *pulumi.Context, cfg *config.Config, networkStack *pulumi.StackReference, auroraStack *pulumi.StackReference, backupStack *pulumi.StackReference) (*EC2Resources, error) {
	// Get network resources from network stack
	vpcId := networkStack.GetOutput(pulumi.String("vpcId"))
	publicSubnetId := networkStack.GetOutput(pulumi.String("publicSubnetId"))

	// Get Aurora resources from Aurora stack
	auroraSecurityGroupId := auroraStack.GetOutput(pulumi.String("auroraSecurityGroupId"))

	// Get S3 bucket name from backup stack
	logBucketName := backupStack.GetOutput(pulumi.String("logBucketName"))

	// Upload aurora_stress_test.sh script to S3 bucket
	scriptPath := "../../scripts/aurora_stress_test.sh"
	scriptContent, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil, err
	}

	_, err = s3.NewBucketObject(ctx, "aurora-stress-test-script", &s3.BucketObjectArgs{
		Bucket:      logBucketName.ApplyT(func(v interface{}) string { return v.(string) }).(pulumi.StringOutput),
		Key:         pulumi.String("scripts/aurora_stress_test.sh"),
		Content:     pulumi.String(string(scriptContent)),
		ContentType: pulumi.String("text/x-shellscript"),
		Acl:         pulumi.String("private"),
	})
	if err != nil {
		return nil, err
	}

	// Create EC2 security group
	ec2SecurityGroup, err := ec2.NewSecurityGroup(ctx, "ec2-sg", &ec2.SecurityGroupArgs{
		VpcId:       vpcId.ApplyT(func(v interface{}) string { return v.(string) }).(pulumi.StringOutput),
		Description: pulumi.String("Security group for EC2 stress client"),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Protocol:    pulumi.String("tcp"),
				FromPort:    pulumi.Int(22),
				ToPort:      pulumi.Int(22),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				Description: pulumi.String("Allow SSH from anywhere"),
			},
		},
		Egress: ec2.SecurityGroupEgressArray{
			&ec2.SecurityGroupEgressArgs{
				Protocol:    pulumi.String("-1"),
				FromPort:    pulumi.Int(0),
				ToPort:      pulumi.Int(0),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				Description: pulumi.String("Allow all outbound traffic"),
			},
		},
		Tags: CreateResourceTags(ctx, cfg.Tags, "aurora-ec2-sg"),
	})
	if err != nil {
		return nil, err
	}

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
		Tags: CreateResourceTags(ctx, cfg.Tags, "aurora-ec2-role"),
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

	// Create policy for S3 access
	s3AccessPolicy, err := iam.NewPolicy(ctx, "s3-access-policy", &iam.PolicyArgs{
		Description: pulumi.String("Policy for S3 access to audit logs"),
		Policy: pulumi.String(`{
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
						"*"
					]
				},
				{
					"Action": [
						"s3:ListAllMyBuckets"
					],
					"Effect": "Allow",
					"Resource": "*"
				}
			]
		}`),
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

	// S3 bucket name is already retrieved above as logBucketName

	// Create user data script
	userData := pulumi.All(logBucketName).ApplyT(func(args []interface{}) string {
		bucketName := args[0].(string)
		return `#!/bin/bash
# Update system packages
dnf update -y

# Install MySQL client
dnf install -y mariadb105

# Install AWS CLI
dnf install -y aws-cli

# Install sysbench from source
dnf groupinstall -y "Development Tools"
dnf install -y mariadb105-devel openssl-devel git
git clone https://github.com/akopytov/sysbench.git
cd sysbench
./autogen.sh
./configure
make -j
make install

# Create directory for scripts
mkdir -p /home/ec2-user/scripts

# Create sysbench setup script
cat > /home/ec2-user/scripts/setup_sysbench.sh << 'EOF'
#!/bin/bash
# Setup sysbench test database

# Get AWS region using IMDSv2
TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
REGION=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/region)

# Get the Aurora endpoint from SSM Parameter Store
CLUSTER_ENDPOINT=$(aws ssm get-parameter --name "/aurora-audit-log-lab/aurora-endpoint" --region $REGION --query "Parameter.Value" --output text)

# Connect using the master password
echo "Connecting to Aurora using master password..."
MASTER_PASSWORD="Password123!"

# Create test database and user
mysql -h $CLUSTER_ENDPOINT -u admin -p$MASTER_PASSWORD << 'MYSQLEOF'
CREATE DATABASE IF NOT EXISTS sysbench_test;
CREATE USER IF NOT EXISTS 'sysbench'@'%' IDENTIFIED BY 'sysbench123';
GRANT ALL PRIVILEGES ON sysbench_test.* TO 'sysbench'@'%';
FLUSH PRIVILEGES;
MYSQLEOF

# Prepare sysbench OLTP tables
sysbench oltp_read_write --db-driver=mysql --mysql-host=$CLUSTER_ENDPOINT --mysql-user=sysbench --mysql-password='sysbench123' --mysql-db=sysbench_test --tables=10 --table-size=100000 --threads=4 prepare
EOF

# Get AWS region using IMDSv2
TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
REGION=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/region)

# Download the aurora_stress_test.sh script from S3
aws s3 cp s3://` + bucketName + `/scripts/aurora_stress_test.sh /home/ec2-user/scripts/aurora_stress_test.sh --region $REGION

# Make scripts executable
chmod +x /home/ec2-user/scripts/setup_sysbench.sh
chmod +x /home/ec2-user/scripts/aurora_stress_test.sh

# Set ownership
chown -R ec2-user:ec2-user /home/ec2-user/scripts
`
	}).(pulumi.StringOutput)

	// Get the latest Amazon Linux 2023 AMI
	ami, err := ec2.LookupAmi(ctx, &ec2.LookupAmiArgs{
		Owners:     []string{"amazon"},
		MostRecent: pulumi.BoolRef(true),
		NameRegex:  pulumi.StringRef("^al2023-ami-2023.*-arm64$"),
		Filters: []ec2.GetAmiFilter{
			{
				Name:   "root-device-type",
				Values: []string{"ebs"},
			},
			{
				Name:   "virtualization-type",
				Values: []string{"hvm"},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	// Create EC2 instance
	ec2Instance, err := ec2.NewInstance(ctx, "aurora-ec2", &ec2.InstanceArgs{
		Ami:                      pulumi.String(ami.Id),
		InstanceType:             pulumi.String(cfg.EC2.InstanceType),
		SubnetId:                 publicSubnetId.ApplyT(func(v interface{}) string { return v.(string) }).(pulumi.StringOutput),
		VpcSecurityGroupIds:      pulumi.StringArray{ec2SecurityGroup.ID()},
		AssociatePublicIpAddress: pulumi.Bool(true),
		KeyName:                  pulumi.String(cfg.EC2.KeyPairName),
		IamInstanceProfile:       ec2InstanceProfile.Name,
		UserData:                 userData,
		Tags:                     CreateResourceTags(ctx, cfg.Tags, "aurora-ec2"),
	})
	if err != nil {
		return nil, err
	}

	// Allow MySQL access from EC2 to Aurora
	_, err = ec2.NewSecurityGroupRule(ctx, "ec2-to-aurora-mysql", &ec2.SecurityGroupRuleArgs{
		Type:                  pulumi.String("ingress"),
		FromPort:              pulumi.Int(3306),
		ToPort:                pulumi.Int(3306),
		Protocol:              pulumi.String("tcp"),
		SourceSecurityGroupId: ec2SecurityGroup.ID(),
		SecurityGroupId:       auroraSecurityGroupId.ApplyT(func(v interface{}) string { return v.(string) }).(pulumi.StringOutput),
		Description:           pulumi.String("Allow MySQL from EC2 instance"),
	})
	if err != nil {
		return nil, err
	}

	return &EC2Resources{
		EC2SecurityGroup:   ec2SecurityGroup,
		EC2Role:            ec2Role,
		EC2InstanceProfile: ec2InstanceProfile,
		EC2Instance:        ec2Instance,
	}, nil
}

// CreateResourceTags creates a pulumi.StringMap from the tags configuration
func CreateResourceTags(ctx *pulumi.Context, cfg utils.TagsConfig, resourceName string) pulumi.StringMap {
	tags := pulumi.StringMap{
		"Name":        pulumi.String(resourceName),
		"Environment": pulumi.String(cfg.Environment),
		"Project":     pulumi.String(cfg.Project),
		"Owner":       pulumi.String(cfg.Owner),
	}

	// Add custom tags
	for k, v := range cfg.CustomTags {
		tags[k] = pulumi.String(v)
	}

	return tags
}
