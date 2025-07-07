
## File and Directory Structure

- Organize the project in a clear, modular, and human-readable structure.
- Use a `main.go` file as the entry point.
- Place resource definitions in separate files under a `resources/` directory (e.g., `resources/vpc.go`, `resources/aurora.go`).
- Include a `config/` directory for stack-specific configuration files or utilities (e.g., `config/config.go`).
- Include a `utils/` directory with a `tags.go` file for centralized tag management.
- Ensure the structure is intuitive and scalable for multi-stack projects.

## Pulumi Configuration

- Avoid hardcoding parameters in the code.
- Use Pulumi Config (`pulumi.Config`) to define stack-specific variables (e.g., AWS region, instance types, database settings).
- Store configuration values in `Pulumi.<stack>.yaml` files (e.g., `Pulumi.dev.yaml`, `Pulumi.prod.yaml`).
- Include mandatory tags (environment, project, owner) and optional customTags in the configuration.
- Use `pulumi.Config.RequireSecret` for sensitive data (e.g., database passwords, API keys).
- Provide clear documentation in the code for required configuration keys, including tags and secrets, and their purposes.

## Tag Enforcement

- Implement a centralized tagging utility in `utils/tags.go` to apply consistent tags to all AWS resources.
- Enforce mandatory tags: Environment (e.g., dev, prod), Project (e.g., project name), Owner (e.g., team or individual), and Name (resource-specific).
- Support optional customTags (e.g., CostCenter, Application) defined in `Pulumi.<stack>.yaml`.
- Validate tags in `config/config.go` to ensure mandatory tags are present, returning errors for missing tags.
- Apply tags to every AWS resource during creation using the tagging utility.
- Log tagging operations for debugging (e.g., using `ctx.Log.Info`).
- Ensure tags support AWS IAM policy conditions for tag-based access control.

## Security Considerations

### IAM Least Privilege

- Create fine-grained IAM roles and policies for each resource type (e.g., Aurora, S3) using `aws.iam` package.
- Avoid overly permissive policies; specify exact actions and resources (e.g., `rds:DescribeDBClusters` for Aurora).
- Attach roles dynamically to resources (e.g., Aurora cluster role for audit log access).

### Secret Management

- Store sensitive data (e.g., database credentials) in AWS Secrets Manager or Parameter Store and reference via Pulumi Config secrets.
- Encrypt secrets in Pulumi state using `pulumi.Config.RequireSecret`.

### Network Security

- Place sensitive resources (e.g., Aurora clusters) in private subnets within a VPC.
- Define security groups with least privilege rules (e.g., allow port 5432 for Aurora only from specific CIDRs).
- Create VPC endpoints for AWS services (e.g., S3, Secrets Manager) to avoid internet-bound traffic.


## Error Handling

- Implement robust error handling for all resource creation, configuration, tagging, and security operations.
- Use Go's error handling patterns (e.g., checking errors with `if err != nil`).
- Return errors from functions and handle them appropriately in the main Pulumi program.
- Include meaningful error messages for missing or invalid tags, missing secrets, or insecure configurations (e.g., "encryption must be enabled for Aurora cluster").
- Validate security configurations (e.g., encryption, IAM policies) in `config/config.go`.

## Resource Creation Order

- Ensure resources are created in the correct order to respect dependencies (e.g., create IAM roles and KMS keys before Aurora clusters, VPC before subnets).
- Use Pulumi's dependency management (e.g., `pulumi.ResourceOption` with `DependsOn`) to enforce creation order.
- Ensure resources can be deleted without conflicts by properly managing dependencies.
- Apply tags and security configurations (e.g., encryption, security groups) during resource creation.

## General Best Practices

- Use meaningful variable and function names that reflect their purpose.
- Add comments to explain the purpose of resources, functions, configuration, tagging, and security logic.
- Follow Go conventions for code formatting and style.
- Import only necessary Pulumi and AWS SDK packages.
- Ensure the code is compatible with Pulumi's Go SDK and AWS provider.
