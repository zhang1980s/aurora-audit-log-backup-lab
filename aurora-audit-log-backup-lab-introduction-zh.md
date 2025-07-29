# Aurora 审计日志备份解决方案

## 0. 项目介绍

Aurora 审计日志备份解决方案是一个基于 AWS 无服务器架构的自动化系统，用于将 Aurora MySQL 数据库的审计日志备份到 S3 存储桶中。该解决方案使用 Lambda 函数、DynamoDB、SQS 和 S3 等 AWS 服务，实现了高效、可靠的日志备份流程。

该项目的主要功能包括：

- 自动扫描 Aurora MySQL 数据库实例
- 检测新的审计日志文件
- 将日志文件下载并备份到 S3 存储桶
- 使用 DynamoDB 跟踪日志文件的备份状态
- 支持多种日志类型的备份（审计日志、错误日志、实例日志）
- 通过 TTL 机制自动清理过期的日志记录

该解决方案特别适用于需要长期保存数据库审计日志以满足合规要求的场景。如果审计日志有实时读取并分析的需求，建议采用AWS默认的Cloudwatch log方式获取实时日志。如果日志内容只用于存档且非常少机会被读取，本解决方案可大幅降低Cloudwatch日志注入和存储成本。

## 1. 架构

### 1.1 整体架构

该项目的架构分为三个主要组件：

1. **基础网络环境**：VPC、子网、路由表和 VPC 端点，用于安全的私有访问
2. **日志备份资源**：带版本控制的 Lambda 函数、DynamoDB 表、SQS 队列和用于日志备份的 S3 存储桶
3. **Aurora 测试环境**：启用了审计日志记录的 Aurora MySQL 集群

### 1.2 架构图

![Aurora 审计日志备份架构](generated-diagrams/aurora-audit-log-backup-architecture.png)

### 1.3 组件说明

#### 1.3.1 Lambda 函数

1. **DB Scanner**：扫描 Aurora 数据库实例并将其 ID 发送到 SQS 队列
2. **Log Detector**：从队列中处理数据库实例 ID 并检测新的审计日志文件
3. **Log Downloader**：由 DynamoDB 流触发，将检测到的日志文件从数据库实例下载下来并且上传到 S3

所有 Lambda 函数都使用带版本控制和别名的容器镜像，以实现受控部署。

#### 1.3.2 Lambda 函数架构

Lambda 函数遵循清晰的架构方法：

- **目录结构**：组织为 `cmd/`、`internal/` 和 `pkg/` 目录
- **关注点分离**：使用处理程序、服务和存储库层实现
- **依赖注入**：用于 AWS 客户端和服务
- **结构化日志记录**：使用 zap logger 增强
- **X-Ray 跟踪**：详细的子段和注释，提高可观察性
- **错误处理**：自定义错误类型和包装
- **AWS SDK v6**：使用最新版本的 AWS SDK

#### 1.3.3 基础设施资源

- 带有公共和私有子网的 VPC
- S3 VPC 端点（仅从私有子网访问）
- DynamoDB VPC 端点（仅从私有子网访问）
- RDS VPC 端点（仅从私有子网访问）
- SQS VPC 端点（仅从私有子网访问）
- 启用了审计日志记录的 Aurora MySQL 集群
- 用于测试的 EC2 实例
- 用于审计日志备份的 S3 存储桶
- 用于跟踪日志文件的 DynamoDB 表
- 用于数据库实例 ID 的 SQS 队列
- 用于调度 DB Scanner Lambda 的 EventBridge 规则

### 1.4 数据流程

1. EventBridge 规则定期触发 DB Scanner Lambda
2. DB Scanner Lambda 扫描 Aurora 数据库实例并将实例 ID 发送到 SQS 队列
3. Log Detector Lambda 从队列中接收实例 ID，检测新的日志文件，并将元数据存储在 DynamoDB 中
4. DynamoDB 流触发 Log Downloader Lambda
5. Log Downloader Lambda 下载日志文件并将其上传到 S3 存储桶
6. Log Downloader Lambda 更新 DynamoDB 中的记录状态

## 2. 参数说明

### 2.1 Pulumi 配置参数

以下是 `Pulumi.dev.yaml` 中的配置参数：

| 参数名 | 描述 | 默认值 | 示例 |
|-------|------|--------|------|
| aws:region | AWS 区域 | - | ap-southeast-1 |
| backup-solution:dbScannerMemory | DB Scanner Lambda 的内存大小（MB） | 512 | 512 |
| backup-solution:dbScannerTimeout | DB Scanner Lambda 的超时时间（秒） | 60 | 60 |
| backup-solution:logDetectorMemory | Log Detector Lambda 的内存大小（MB） | 1024 | 1024 |
| backup-solution:logDetectorTimeout | Log Detector Lambda 的超时时间（秒） | 300 | 300 |
| backup-solution:logDownloaderMemory | Log Downloader Lambda 的内存大小（MB） | 1024 | 1024 |
| backup-solution:logDownloaderTimeout | Log Downloader Lambda 的超时时间（秒） | 300 | 300 |
| backup-solution:sqsVisibilityTimeout | SQS 消息可见性超时（秒） | 300 | 300 |
| backup-solution:lambdaBatchSize | Lambda 事件源映射的批处理大小 | 10 | 10 |
| backup-solution:eventBridgeSchedule | EventBridge 规则的调度表达式 | rate(15 minutes) | rate(15 minutes) |
| backup-solution:s3LogPrefix | S3 日志文件的前缀 | aurora-logs | aurora-logs |
| backup-solution:publishLambdaVersions | 是否发布 Lambda 版本 | true | true |
| backup-solution:backupLogTypes | 要备份的日志类型（逗号分隔） | audit | audit,error,instance |
| backup-solution:instanceEngine | 要处理的数据库实例引擎类型（逗号分隔） | aurora-mysql | aurora-mysql,aurora-postgresql |
| backup-solution:blackList | 要排除的数据库实例 ID（逗号分隔） | - | instance1,instance2 |
| backup-solution:ttlDays | DynamoDB 记录的 TTL 天数 | 2 | 3 |
| backup-solution:dbScannerImageVersion | DB Scanner 镜像版本 | - | v1.0.6 |
| backup-solution:logDetectorImageVersion | Log Detector 镜像版本 | - | v1.0.6 |
| backup-solution:logDownloaderImageVersion | Log Downloader 镜像版本 | - | v1.0.6 |
| backup-solution:environment | 环境名称 | dev | dev |
| backup-solution:owner | 所有者 | - | zzhe |
| backup-solution:project | 项目名称 | - | aurora-audit-log-backup-lab |

### 2.2 Lambda 环境变量

#### 2.2.1 DB Scanner Lambda

| 环境变量 | 描述 | 默认值 | 示例 |
|---------|------|--------|------|
| SQS_QUEUE_URL | SQS 队列的 URL | - | https://sqs.ap-southeast-1.amazonaws.com/123456789012/aurora-db-instances |
| LOG_LEVEL | 日志级别 | error | debug |
| INSTANCE_ENGINE | 要处理的数据库实例引擎类型（逗号分隔） | aurora-mysql | aurora-mysql,aurora-postgresql |
| BLACK_LIST | 要排除的数据库实例 ID（逗号分隔） | - | instance1,instance2 |

#### 2.2.2 Log Detector Lambda

| 环境变量 | 描述 | 默认值 | 示例 |
|---------|------|--------|------|
| DYNAMODB_TABLE_NAME | DynamoDB 表名 | - | aurora-log-files |
| LOG_LEVEL | 日志级别 | error | debug |
| BACKUP_LOGS | 要备份的日志类型（逗号分隔） | audit | audit,error,instance |
| TTL_DAYS | DynamoDB 记录的 TTL 天数 | 5 | 3 |

#### 2.2.3 Log Downloader Lambda

| 环境变量 | 描述 | 默认值 | 示例 |
|---------|------|--------|------|
| DYNAMODB_TABLE_NAME | DynamoDB 表名 | - | aurora-log-files |
| S3_BUCKET_NAME | S3 存储桶名称 | - | aurora-log-backup-123456789012 |
| S3_PREFIX | S3 日志文件的前缀 | aurora-logs | aurora-logs |
| LOG_LEVEL | 日志级别 | error | debug |
| TTL_DAYS | DynamoDB 记录的 TTL 天数 | 5 | 3 |

## 3. Pulumi 安装指南

### 3.1 安装 Pulumi CLI

#### 3.1.1 Linux

```bash
curl -fsSL https://get.pulumi.com | sh
```

#### 3.1.2 macOS

```bash
brew install pulumi
```

#### 3.1.3 Windows

```powershell
choco install pulumi
```

或者使用 PowerShell：

```powershell
iwr https://get.pulumi.com/install.ps1 -OutFile install.ps1
.\install.ps1
```

### 3.2 配置 Pulumi

1. 登录 Pulumi：

```bash
pulumi login
```

2. 配置 AWS 凭证：

确保您已经配置了 AWS CLI 凭证，或者设置以下环境变量：

```bash
export AWS_ACCESS_KEY_ID=<YOUR_ACCESS_KEY>
export AWS_SECRET_ACCESS_KEY=<YOUR_SECRET_KEY>
export AWS_REGION=<YOUR_REGION>
```

### 3.3 部署流程

部署过程分为多个步骤，以处理 ECR 存储库和 Lambda 函数之间的循环依赖：

#### 3.3.1 部署 ECR 存储库

首先，部署 ECR 堆栈以创建存储库：

```bash
cd infrastructure/ecr-stack
pulumi stack init dev
pulumi up
```

#### 3.3.2 构建和推送 Lambda 镜像

构建并推送带版本控制的 Lambda 容器镜像到 ECR 存储库：

```bash
# 使用版本标签构建和推送 Lambda 容器镜像
make build-and-push-versioned VERSION=v1.0.0
```

这将：
- 使用指定的版本标签构建 Lambda 容器镜像
- 将镜像推送到 ECR
- 使用新版本更新 Pulumi 配置

#### 3.3.3 部署主要基础设施

部署引用现有 ECR 存储库的 Aurora 堆栈：

```bash
cd infrastructure/aurora-log-backup-lab-stack
pulumi stack init dev
pulumi up
```

## 4. 备份解决方案手动部署指南

### 4.1 先决条件

1. 已有 AWS 账户并拥有管理员权限
2. 已创建 VPC 和私有子网
3. 已创建 Aurora MySQL 集群
4. 已在 ECR 中创建三个容器镜像仓库并上传了相应的镜像：
   - db-scanner
   - log-detector
   - log-downloader

### 4.2 手动创建 ECR 存储库

1. 登录 AWS 控制台，导航至 ECR 服务
2. 点击"创建存储库"
3. 选择"私有"存储库类型
4. 输入存储库名称（需要创建三个存储库）：
   - `db-scanner`
   - `log-detector`
   - `log-downloader`
5. 在"标签不可变性"部分，选择"启用"以防止覆盖镜像标签
6. 在"扫描设置"部分，选择"启用扫描"以自动扫描推送的镜像
7. 点击"创建存储库"
8. 对每个存储库重复上述步骤

### 4.3 使用 Makefile 构建 Docker 镜像并推送到 ECR

1. 确保已安装 Docker 和 AWS CLI，并已配置 AWS 凭证
2. 获取 ECR 登录命令并执行：

```bash
aws ecr get-login-password --region <您的区域> | docker login --username AWS --password-stdin <您的账户ID>.dkr.ecr.<您的区域>.amazonaws.com
```

3. 查看项目根目录中的 Makefile，了解可用的构建命令
4. 构建并推送所有 Lambda 镜像（带版本标签）：

```bash
# 设置环境变量
export AWS_ACCOUNT_ID=<您的账户ID>
export AWS_REGION=<您的区域>
export VERSION=v1.0.0

# 构建并推送所有镜像
make build-and-push-versioned
```

5. 或者，单独构建和推送每个镜像：

```bash
# 构建并推送 DB Scanner 镜像
make build-and-push-dbscanner VERSION=v1.0.0

# 构建并推送 Log Detector 镜像
make build-and-push-logdetector VERSION=v1.0.0

# 构建并推送 Log Downloader 镜像
make build-and-push-logdownloader VERSION=v1.0.0
```

6. 验证镜像已成功推送到 ECR 存储库：

```bash
aws ecr describe-images --repository-name db-scanner --region <您的区域>
aws ecr describe-images --repository-name log-detector --region <您的区域>
aws ecr describe-images --repository-name log-downloader --region <您的区域>
```

### 4.4 手动创建 VPC 端点

#### 4.4.1 创建 S3 VPC 端点

1. 登录 AWS 控制台，导航至 VPC 服务
2. 在左侧导航栏中，点击"端点"
3. 点击"创建端点"
4. 在"服务类别"部分，选择"AWS 服务"
5. 在搜索框中输入"s3"，然后选择"com.amazonaws.<您的区域>.s3"服务
6. 选择您的 VPC
7. 在"配置路由表"部分，选择与您的私有子网关联的路由表
8. 在"策略"部分，选择"完全访问"
9. 点击"创建端点"

#### 4.4.2 创建 DynamoDB VPC 端点

1. 导航至 VPC 服务的"端点"部分
2. 点击"创建端点"
3. 在"服务类别"部分，选择"AWS 服务"
4. 在搜索框中输入"dynamodb"，然后选择"com.amazonaws.<您的区域>.dynamodb"服务
5. 选择您的 VPC
6. 在"配置路由表"部分，选择与您的私有子网关联的路由表
7. 在"策略"部分，选择"完全访问"
8. 点击"创建端点"

#### 4.4.3 创建 SQS VPC 端点

1. 导航至 VPC 服务的"端点"部分
2. 点击"创建端点"
3. 在"服务类别"部分，选择"AWS 服务"
4. 在搜索框中输入"sqs"，然后选择"com.amazonaws.<您的区域>.sqs"服务
5. 选择您的 VPC
6. 在"子网"部分，选择您的私有子网
7. 在"安全组"部分，选择允许所需流量的安全组
8. 在"策略"部分，选择"完全访问"
9. 点击"创建端点"

#### 4.4.4 创建 RDS VPC 端点

1. 导航至 VPC 服务的"端点"部分
2. 点击"创建端点"
3. 在"服务类别"部分，选择"AWS 服务"
4. 在搜索框中输入"rds"，然后选择"com.amazonaws.<您的区域>.rds"服务
5. 选择您的 VPC
6. 在"子网"部分，选择您的私有子网
7. 在"安全组"部分，选择允许所需流量的安全组
8. 在"策略"部分，选择"完全访问"
9. 点击"创建端点"

### 4.5 创建 S3 存储桶

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
11. 设置过期时间为 3 天
12. 点击"创建规则"

### 4.6 创建 DynamoDB 表

1. 导航至 DynamoDB 服务
2. 点击"创建表"
3. 输入表名（例如：`aurora-log-files`）
4. 分区键设置为 `DBInstanceIdentifier`（类型：字符串）
5. 排序键设置为 `LogFileName`（类型：字符串）
6. 点击"创建表"
7. 创建完成后，点击表名进入表详情页面
8. 在"其他设置"选项卡中，找到"生存时间 (TTL)"部分
9. 点击"启用 TTL"
10. 在 TTL 属性字段中输入 `ExpirationTime`
11. 点击"保存"

### 4.7 创建 SQS 队列

1. 导航至 SQS 服务
2. 点击"创建队列"
3. 选择"标准队列"
4. 输入队列名称（例如：`aurora-db-instances`）
5. 在"配置"部分，设置"可见性超时"为 300 秒
6. 设置"消息保留期"为 24 小时（86400 秒）
7. 点击"创建队列"
8. 创建完成后，记录队列的 ARN

### 4.8 创建 IAM 角色和策略

为 Lambda 函数创建所需的 IAM 角色和策略，包括：

1. Lambda VPC 访问策略
2. DB Scanner 角色和策略
3. Log Detector 角色和策略
4. Log Downloader 角色和策略

每个角色都需要适当的权限来访问相关的 AWS 服务和资源。

### 4.9 创建安全组

为 Lambda 函数创建安全组，允许所需的出站流量。

### 4.10 创建 Lambda 函数

创建三个 Lambda 函数：

1. DB Scanner Lambda
2. Log Detector Lambda
3. Log Downloader Lambda

每个函数都使用相应的容器镜像、内存设置、超时设置、环境变量和 IAM 角色。

### 4.11 创建事件源映射

#### 4.11.1 为 Log Detector 创建 SQS 事件源映射

1. 导航至 Lambda 服务，选择 Log Detector 函数
2. 点击"添加触发器"，选择"SQS"
3. 选择之前创建的 SQS 队列
4. 设置批处理大小为 10（或根据需要调整）
5. 点击"添加"

#### 4.11.2 为 Log Downloader 创建 DynamoDB 事件源映射

1. 导航至 Lambda 服务，选择 Log Downloader 函数
2. 点击"添加触发器"，选择"DynamoDB"
3. 选择之前创建的 DynamoDB 表
4. 设置"起始位置"为"最新"
5. 设置批处理大小为 10（或根据需要调整）
6. 点击"添加"

### 4.12 创建 EventBridge 规则

1. 导航至 EventBridge 服务
2. 点击"创建规则"
3. 输入规则名称（例如：`aurora-db-scanner-schedule`）
4. 选择"计划"作为规则类型
5. 设置计划表达式（例如：`rate(15 minutes)`）
6. 在"目标"部分，选择"Lambda 函数"
7. 选择 DB Scanner Lambda 函数
8. 点击"创建"

## 5. 测试部署

### 5.1 测试 DB Scanner Lambda

1. 导航至 Lambda 服务，选择 DB Scanner 函数
2. 点击"测试"选项卡
3. 创建一个空的测试事件
4. 点击"测试"
5. 验证函数是否成功执行并将数据库实例 ID 发送到 SQS 队列

### 5.2 测试 Log Detector Lambda

1. 确保 SQS 队列中有消息
2. 导航至 Lambda 服务，选择 Log Detector 函数
3. 监控函数的执行情况
4. 验证 DynamoDB 表中是否有新的记录

### 5.3 测试 Log Downloader Lambda

1. 确保 DynamoDB 表中有记录
2. 导航至 Lambda 服务，选择 Log Downloader 函数
3. 监控函数的执行情况
4. 验证 S3 存储桶中是否有新的日志文件

## 6. 监控和故障排除

### 6.1 监控工具

1. **CloudWatch 日志**：查看 Lambda 函数的日志
2. **X-Ray 跟踪**：分析函数执行的详细信息
3. **CloudWatch 指标**：监控函数的执行时间、内存使用和错误率
4. **DynamoDB 控制台**：查看表中的记录和 TTL 状态
5. **S3 控制台**：验证日志文件是否已成功上传

### 6.2 常见问题排查

1. **Lambda 函数超时**：增加函数的超时设置或优化代码
2. **内存不足**：增加函数的内存分配
3. **权限问题**：检查 IAM 角色和策略
4. **VPC 配置问题**：确保 Lambda 函数可以访问所需的 AWS 服务
5. **DynamoDB TTL 未生效**：确保 TTL 属性正确设置为 `ExpirationTime`

## 7. 总结

Aurora 审计日志备份解决方案提供了一个可靠、可扩展的方式来自动备份 Aurora MySQL 数据库的审计日志。通过使用无服务器架构和容器化的 Lambda 函数，该解决方案具有高可用性、低维护成本和良好的可观察性。

主要优势包括：

1. **自动化**：无需人工干预即可定期备份审计日志
2. **可扩展性**：可以处理多个数据库实例和大量日志文件
3. **成本效益**：使用无服务器架构，按实际使用付费
4. **安全性**：通过 VPC 端点和 IAM 角色实现安全访问
5. **可观察性**：通过 X-Ray 跟踪和 CloudWatch 日志提供详细的监控
6. **合规性**：通过长期保存审计日志，满足合规要求

通过本文档中的指南，您可以使用 Pulumi 自动部署整个解决方案，或者按照手动部署指南在 AWS 控制台中创建所需的资源。