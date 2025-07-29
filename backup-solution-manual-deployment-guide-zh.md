# Aurora 审计日志备份解决方案手动部署指南

本指南将帮助您通过 AWS 控制台手动部署 Aurora 审计日志备份解决方案。该解决方案包含三个 Lambda 函数，用于扫描 Aurora 数据库实例、检测日志文件并将其下载到 S3 存储桶。

## 先决条件

1. 已有 AWS 账户并拥有管理员权限
2. 已创建 VPC 和私有子网
3. 已创建 Aurora MySQL 集群
4. 已在 ECR 中创建三个容器镜像仓库并上传了相应的镜像：
   - db-scanner
   - log-detector
   - log-downloader

## 部署步骤

### 1. 创建 S3 存储桶

1. 登录 AWS 控制台，导航至 S3 服务
2. 点击"创建存储桶"
3. 输入存储桶名称（例如：`aurora-log-backup-{账户ID}`）
4. 选择合适的区域
5. 保持默认设置或根据需要调整
6. 在"服务器端加密"部分，选择"启用"并选择"Amazon S3 密钥（SSE-S3）"
7. 点击"创建存储桶"
8. 创建完成后，导航到存储桶的"管理"选项卡
9. 在"生命周期规则"部分，点击"创建生命周期规则"
10. 输入规则名称（例如：`expire-old-logs`）
11. 设置过期时间为 7 天
12. 点击"创建规则"

### 2. 创建 DynamoDB 表

1. 导航至 DynamoDB 服务
2. 点击"创建表"
3. 输入表名（例如：`aurora-log-files`）
4. 分区键设置为 `DBInstanceIdentifier`（类型：字符串）
5. 排序键设置为 `LogFileName`（类型：字符串）
6. 点击"添加其他属性"，添加属性 `LastBackup`（类型：数字）
7. 保持默认设置或根据需要调整
8. 在"表设置"部分，选择"按需"容量模式
9. 在"表设置"部分，启用 DynamoDB 流并选择"新旧映像"
10. 点击"创建表"
11. 创建完成后，点击表名进入表详情页面
12. 在"其他设置"选项卡中，找到"生存时间 (TTL)"部分
13. 点击"启用 TTL"
14. 在 TTL 属性字段中输入 `LastBackup`
15. 点击"保存"

### 3. 创建 SQS 队列

1. 导航至 SQS 服务
2. 点击"创建队列"
3. 选择"标准队列"
4. 输入队列名称（例如：`aurora-db-instances`）
5. 在"配置"部分，设置"可见性超时"为 30 秒
6. 设置"消息保留期"为 24 小时（86400 秒）
7. 点击"创建队列"
8. 创建完成后，记录队列的 ARN

### 4. 创建 IAM 角色和策略

#### 4.1 创建 Lambda VPC 访问策略

1. 导航至 IAM 服务
2. 点击"策略"，然后点击"创建策略"
3. 选择 JSON 选项卡，并粘贴以下策略：

```json
{
  "Version": "2012-10-17",
  "Statement": [
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
}
```

4. 点击"下一步"
5. 输入策略名称（例如：`lambda-vpc-access-policy`）
6. 点击"创建策略"

#### 4.2 创建 DB Scanner 角色和策略

1. 导航至 IAM 服务
2. 点击"角色"，然后点击"创建角色"
3. 选择"AWS 服务"作为可信实体类型，然后选择"Lambda"
4. 点击"下一步"
5. 搜索并选择 `AWSLambdaBasicExecutionRole` 和之前创建的 `lambda-vpc-access-policy`
6. 点击"下一步"
7. 输入角色名称（例如：`aurora-db-scanner-role`）
8. 点击"创建角色"
9. 创建完成后，点击新创建的角色
10. 点击"添加权限"，然后选择"创建内联策略"
11. 选择 JSON 选项卡，并粘贴以下策略（替换 `{SQS_QUEUE_ARN}` 为您的 SQS 队列 ARN）：

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "rds:DescribeDBInstances"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "sqs:SendMessage"
      ],
      "Resource": "{SQS_QUEUE_ARN}"
    }
  ]
}
```

12. 点击"查看策略"
13. 输入策略名称（例如：`db-scanner-policy`）
14. 点击"创建策略"

#### 4.3 创建 Log Detector 角色和策略

1. 导航至 IAM 服务
2. 点击"角色"，然后点击"创建角色"
3. 选择"AWS 服务"作为可信实体类型，然后选择"Lambda"
4. 点击"下一步"
5. 搜索并选择 `AWSLambdaBasicExecutionRole` 和之前创建的 `lambda-vpc-access-policy`
6. 点击"下一步"
7. 输入角色名称（例如：`aurora-log-detector-role`）
8. 点击"创建角色"
9. 创建完成后，点击新创建的角色
10. 点击"添加权限"，然后选择"创建内联策略"
11. 选择 JSON 选项卡，并粘贴以下策略（替换 `{SQS_QUEUE_ARN}` 和 `{DYNAMODB_TABLE_ARN}` 为您的资源 ARN）：

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "rds:DescribeDBLogFiles"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage",
        "sqs:GetQueueAttributes"
      ],
      "Resource": "{SQS_QUEUE_ARN}"
    },
    {
      "Effect": "Allow",
      "Action": [
        "dynamodb:PutItem",
        "dynamodb:UpdateItem"
      ],
      "Resource": "{DYNAMODB_TABLE_ARN}"
    }
  ]
}
```

12. 点击"查看策略"
13. 输入策略名称（例如：`log-detector-policy`）
14. 点击"创建策略"

#### 4.4 创建 Log Downloader 角色和策略

1. 导航至 IAM 服务
2. 点击"角色"，然后点击"创建角色"
3. 选择"AWS 服务"作为可信实体类型，然后选择"Lambda"
4. 点击"下一步"
5. 搜索并选择 `AWSLambdaBasicExecutionRole` 和之前创建的 `lambda-vpc-access-policy`
6. 点击"下一步"
7. 输入角色名称（例如：`aurora-log-downloader-role`）
8. 点击"创建角色"
9. 创建完成后，点击新创建的角色
10. 点击"添加权限"，然后选择"创建内联策略"
11. 选择 JSON 选项卡，并粘贴以下策略（替换 `{DYNAMODB_TABLE_ARN}` 和 `{S3_BUCKET_ARN}` 为您的资源 ARN）：

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "rds:DownloadDBLogFilePortion",
        "rds:DownloadCompleteDBLogFile"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "dynamodb:GetItem",
        "dynamodb:UpdateItem",
        "dynamodb:GetRecords",
        "dynamodb:GetShardIterator",
        "dynamodb:DescribeStream",
        "dynamodb:ListStreams"
      ],
      "Resource": [
        "{DYNAMODB_TABLE_ARN}",
        "{DYNAMODB_TABLE_ARN}/stream/*"
      ]
    },
    {
      "Effect": "Allow",
      "Action": [
        "s3:PutObject"
      ],
      "Resource": "{S3_BUCKET_ARN}/*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "s3:ListBucket"
      ],
      "Resource": "{S3_BUCKET_ARN}"
    }
  ]
}
```

12. 点击"查看策略"
13. 输入策略名称（例如：`log-downloader-policy`）
14. 点击"创建策略"

### 5. 创建安全组

1. 导航至 EC2 服务
2. 在左侧导航栏中，点击"安全组"
3. 点击"创建安全组"
4. 输入安全组名称（例如：`lambda-sg`）
5. 输入描述（例如：`Security group for Lambda functions`）
6. 选择您的 VPC
7. 在"出站规则"部分，确保允许所有出站流量
8. 点击"创建安全组"

### 6. 创建 Lambda 函数

#### 6.1 创建 DB Scanner Lambda

1. 导航至 Lambda 服务
2. 点击"创建函数"
3. 选择"容器镜像"
4. 输入函数名称（例如：`aurora-db-scanner`）
5. 在"容器镜像 URI"中，输入您的 DB Scanner 镜像 URI
6. 在"架构"部分，选择"arm64"
7. 在"权限"部分，选择"使用现有角色"并选择之前创建的 `aurora-db-scanner-role`
8. 点击"创建函数"
9. 创建完成后，在"配置"选项卡中：
   - 设置内存为 512 MB（或根据需要调整）
   - 设置超时为 30 秒（或根据需要调整）
   - 在"环境变量"部分，添加以下变量：
     - `SQS_QUEUE_URL`：您的 SQS 队列 URL
     - `LOG_LEVEL`：`error`
10. 在"VPC"部分，选择您的 VPC、私有子网和之前创建的安全组
11. 在"监控和操作工具"部分，启用 X-Ray 跟踪
12. 点击"保存"
13. 在"版本"选项卡中，点击"发布新版本"
14. 输入版本描述并点击"发布"
15. 在"别名"选项卡中，点击"创建别名"
16. 输入别名名称（例如：`live`）
17. 选择刚刚发布的版本
18. 点击"保存"

#### 6.2 创建 Log Detector Lambda

1. 导航至 Lambda 服务
2. 点击"创建函数"
3. 选择"容器镜像"
4. 输入函数名称（例如：`aurora-log-detector`）
5. 在"容器镜像 URI"中，输入您的 Log Detector 镜像 URI
6. 在"架构"部分，选择"arm64"
7. 在"权限"部分，选择"使用现有角色"并选择之前创建的 `aurora-log-detector-role`
8. 点击"创建函数"
9. 创建完成后，在"配置"选项卡中：
   - 设置内存为 512 MB（或根据需要调整）
   - 设置超时为 30 秒（或根据需要调整）
   - 在"环境变量"部分，添加以下变量：
     - `DYNAMODB_TABLE_NAME`：您的 DynamoDB 表名
     - `LOG_LEVEL`：`error`
     - `BACKUP_LOGS`：`audit,error,general`（或根据需要调整）
     - `TTL_DAYS`：`7`（日志保留天数）
10. 在"VPC"部分，选择您的 VPC、私有子网和之前创建的安全组
11. 在"监控和操作工具"部分，启用 X-Ray 跟踪
12. 点击"保存"
13. 在"版本"选项卡中，点击"发布新版本"
14. 输入版本描述并点击"发布"
15. 在"别名"选项卡中，点击"创建别名"
16. 输入别名名称（例如：`live`）
17. 选择刚刚发布的版本
18. 点击"保存"

#### 6.3 创建 Log Downloader Lambda

1. 导航至 Lambda 服务
2. 点击"创建函数"
3. 选择"容器镜像"
4. 输入函数名称（例如：`aurora-log-downloader`）
5. 在"容器镜像 URI"中，输入您的 Log Downloader 镜像 URI
6. 在"架构"部分，选择"arm64"
7. 在"权限"部分，选择"使用现有角色"并选择之前创建的 `aurora-log-downloader-role`
8. 点击"创建函数"
9. 创建完成后，在"配置"选项卡中：
   - 设置内存为 512 MB（或根据需要调整）
   - 设置超时为 60 秒（或根据需要调整）
   - 在"环境变量"部分，添加以下变量：
     - `DYNAMODB_TABLE_NAME`：您的 DynamoDB 表名
     - `S3_BUCKET_NAME`：您的 S3 存储桶名称
     - `S3_PREFIX`：`aurora-logs/`（或根据需要调整）
     - `LOG_LEVEL`：`error`
     - `TTL_DAYS`：`7`（日志保留天数）
10. 在"VPC"部分，选择您的 VPC、私有子网和之前创建的安全组
11. 在"监控和操作工具"部分，启用 X-Ray 跟踪
12. 点击"保存"
13. 在"版本"选项卡中，点击"发布新版本"
14. 输入版本描述并点击"发布"
15. 在"别名"选项卡中，点击"创建别名"
16. 输入别名名称（例如：`live`）
17. 选择刚刚发布的版本
18. 点击"保存"

### 7. 创建事件源映射

#### 7.1 为 Log Detector 创建 SQS 事件源映射

1. 导航至 Lambda 服务
2. 点击您的 Log Detector 函数
3. 在"函数概述"部分，点击"添加触发器"
4. 选择"SQS"作为触发器类型
5. 在"SQS 队列"下拉菜单中，选择您之前创建的队列
6. 设置批处理大小为 10（或根据需要调整）
7. 点击"添加"

#### 7.2 为 Log Downloader 创建 DynamoDB 事件源映射

1. 导航至 Lambda 服务
2. 点击您的 Log Downloader 函数
3. 在"函数概述"部分，点击"添加触发器"
4. 选择"DynamoDB"作为触发器类型
5. 在"DynamoDB 表"下拉菜单中，选择您之前创建的表
6. 设置批处理大小为 10（或根据需要调整）
7. 设置起始位置为"最新"
8. 点击"添加"

### 8. 创建 EventBridge 规则

1. 导航至 EventBridge 服务
2. 点击"创建规则"
3. 输入规则名称（例如：`aurora-db-scanner-schedule`）
4. 输入描述（例如：`Trigger Aurora DB Scanner Lambda every 15 minutes`）
5. 在"定义模式"部分，选择"计划"
6. 选择"固定速率"并设置为 15 分钟（或根据需要调整）
7. 在"选择目标"部分，选择"Lambda 函数"
8. 在"函数"下拉菜单中，选择您的 DB Scanner 函数的别名（例如：`aurora-db-scanner:live`）
9. 点击"创建"
10. 创建完成后，规则默认是启用状态的。如果您想先禁用它，可以在规则列表中选择该规则，然后点击"禁用"

## 验证部署

1. 在 EventBridge 中启用规则（如果之前禁用了）
2. 等待几分钟，让 DB Scanner Lambda 执行
3. 检查 CloudWatch Logs 以确认 Lambda 函数正在运行
4. 检查 DynamoDB 表中是否有条目
5. 检查 S3 存储桶中是否有日志文件

## 故障排除

1. 如果 Lambda 函数失败，请检查 CloudWatch Logs 以获取错误详情
2. 确保所有 IAM 角色和策略都正确配置
3. 确保 VPC 配置允许 Lambda 函数访问 Aurora 数据库和其他 AWS 服务
4. 检查 Lambda 函数的环境变量是否正确设置

## 清理资源

如果您想删除此解决方案，请按以下顺序删除资源：

1. EventBridge 规则
2. Lambda 事件源映射
3. Lambda 函数和别名
4. IAM 角色和策略
5. SQS 队列
6. DynamoDB 表
7. S3 存储桶（先清空存储桶，然后再删除）
8. 安全组