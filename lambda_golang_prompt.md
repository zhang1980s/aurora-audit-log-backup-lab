
## 1. File and Directory Structure

### Project Organization

```
lambda-function/
├── cmd/
│   └── main.go                 # Entry point with minimal code
├── internal/
│   ├── handler/
│   │   └── handler.go          # Lambda handler implementation
│   ├── config/
│   │   └── config.go           # Configuration management
│   ├── models/
│   │   └── models.go           # Data models and types
│   ├── service/
│   │   └── service.go          # Business logic
│   └── repository/
│       └── repository.go       # Data access layer
├── pkg/
│   ├── logger/
│   │   └── logger.go           # Logging utilities
│   └── errors/
│       └── errors.go           # Error handling utilities
├── go.mod
├── go.sum
└── Dockerfile                  # For containerized deployment
```

### Best Practices for File Structure

1. **Separation of Concerns**:
   - Keep the `main.go` file minimal, focusing only on Lambda initialization
   - Separate business logic (service) from data access (repository)
   - Isolate the Lambda handler from implementation details

2. **Dependency Injection**:
   - Initialize dependencies in `main.go` and pass them to the handler
   - Use interfaces for AWS services to enable mocking in tests

3. **Configuration Management**:
   - Create a dedicated package for configuration
   - Use environment variables for Lambda configuration
   - Validate configuration at startup

### Example `main.go`

```go
package main

import (
	"context"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	
	"your-project/internal/handler"
	"your-project/internal/service"
	"your-project/internal/repository"
	"your-project/pkg/logger"
)

func main() {
	// Initialize logger
	log, err := logger.NewLogger()
	if err != nil {
		os.Exit(1)
	}
	defer log.Sync()

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalw("Failed to load AWS config", "error", err)
	}

	// Initialize AWS clients
	dynamoClient := dynamodb.NewFromConfig(cfg)

	// Initialize repository and service layers
	repo := repository.New(dynamoClient)
	svc := service.New(repo, log)

	// Initialize and start Lambda handler
	h := handler.New(svc, log)
	lambda.Start(h.Handle)
}
```

## 2. Logging with uber-go/zap

### Logger Initialization

Create a dedicated logger package that initializes zap based on environment variables:

```go
// pkg/logger/logger.go
package logger

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewLogger creates a new zap logger configured based on environment variables
func NewLogger() (*zap.SugaredLogger, error) {
	// Get log level from environment variable, default to "info"
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	// Parse log level
	var level zapcore.Level
	err := level.UnmarshalText([]byte(strings.ToLower(logLevel)))
	if err != nil {
		// If parsing fails, default to info level
		level = zap.InfoLevel
	}

	// Configure encoder for structured JSON logging
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// Create logger configuration
	config := zap.Config{
		Level:            zap.NewAtomicLevelAt(level),
		Development:      false,
		Encoding:         "json",
		EncoderConfig:    encoderConfig,
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	// Build logger
	logger, err := config.Build(
		zap.AddCallerSkip(1),
		zap.AddStacktrace(zapcore.ErrorLevel),
	)
	if err != nil {
		return nil, err
	}

	// Add Lambda request ID to all logs if available
	requestID := os.Getenv("AWS_LAMBDA_REQUEST_ID")
	if requestID != "" {
		logger = logger.With(zap.String("lambda_request_id", requestID))
	}

	// Return sugared logger for easier use
	return logger.Sugar(), nil
}
```

### Logging Best Practices

1. **Use Structured Logging**:
   - Always use structured logging with key-value pairs
   - Include relevant context in every log entry
   - Use consistent field names across all logs

2. **Log Levels**:
   - `Debug`: Detailed information for debugging
   - `Info`: General operational information
   - `Warn`: Warning conditions that don't cause errors
   - `Error`: Error conditions that affect operation
   - `Fatal`: Critical errors that require immediate attention

3. **Context and Correlation**:
   - Include request IDs in all logs
   - Add correlation IDs for tracing requests across services
   - Log entry and exit points of major functions

4. **Performance Considerations**:
   - Use SugaredLogger for simpler syntax and better performance
   - Avoid expensive operations in log statements
   - Use `Debugw` with lazy evaluation for verbose logging

### Example Usage

```go
// Good logging practices
logger.Infow("Processing request", "requestID", requestID)
logger.Debugw("Database query parameters", "table", tableName, "key", key)
logger.Errorw("Failed to process record", "recordID", id, "error", err)

// Avoid expensive operations in log statements
// Instead of:
logger.Debugf("Request body: %s", string(largeJSONBody))
// Do:
if logger.Level() <= zapcore.DebugLevel {
    logger.Debugw("Request body", "body", string(largeJSONBody))
}
```

## 3. Distributed Tracing with AWS X-Ray

### X-Ray Integration

AWS X-Ray provides distributed tracing capabilities that help developers analyze and debug applications, especially in microservices architectures. Integrating X-Ray with Lambda functions enables:

- End-to-end tracing of requests
- Visual representation of service dependencies
- Performance insights and bottleneck identification
- Error and exception tracking

#### X-Ray Setup in Lambda Functions

1. **Add X-Ray SDK Dependencies**:

```go
// Add to go.mod
go get github.com/aws/aws-xray-sdk-go/xray
```

2. **Initialize X-Ray in Lambda Handler**:

```go
// Add to imports
import (
    "github.com/aws/aws-xray-sdk-go/xray"
    "github.com/aws/aws-xray-sdk-go/awsplugins/ec2"
    "github.com/aws/aws-xray-sdk-go/awsplugins/ecs"
)

func init() {
    // Configure X-Ray
    ec2.Init()  // For EC2 metadata
    ecs.Init()  // For ECS metadata
    
    // Configure sampling rules (optional)
    xray.Configure(xray.Config{
        LogLevel: "info", // Set based on LOG_LEVEL env var
    })
}
```

3. **Instrument AWS SDK Clients**:

```go
// Instead of:
cfg, err := config.LoadDefaultConfig(ctx)
rdsClient := rds.NewFromConfig(cfg)

// Use:
cfg, err := config.LoadDefaultConfig(ctx)
// Wrap AWS clients with X-Ray
rdsClient := rds.NewFromConfig(cfg, func(options *rds.Options) {
    options.HTTPClient = xray.Client(options.HTTPClient)
})
```

### Creating Custom Segments and Subsegments

For important operations, create subsegments to track specific parts of your code:

```go
// For important operations, create subsegments
ctx, subsegment := xray.BeginSubsegment(ctx, "getDBInstances")
defer subsegment.Close(nil) // Close with error if applicable

// If an error occurs:
if err != nil {
    subsegment.AddError(err)
    return nil, err
}
```

### Adding Annotations and Metadata

Enhance traces with searchable annotations and additional metadata:

```go
// Add annotations (indexed and searchable)
xray.AddAnnotation(ctx, "instanceCount", len(instances))

// Add metadata (not indexed but visible in traces)
xray.AddMetadata(ctx, "instances", instances)
```

### Integration with Zap Logging

Integrate X-Ray with Zap logging to correlate logs with traces:

```go
// Add X-Ray trace ID to logs
traceID := xray.TraceID(ctx)
if traceID != "" {
    logger = logger.With(zap.String("xray_trace_id", traceID))
}
```

### Pulumi Configuration for X-Ray

Enable X-Ray tracing in your Lambda function configuration:

```go
// In your Pulumi Lambda function definition
TracingConfig: &lambda.FunctionTracingConfigArgs{
    Mode: pulumi.String("Active"),
},
```

Add necessary IAM permissions for X-Ray:

```go
// Add to Lambda IAM policy
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
```

## 4. Robust Error Handling

### Error Types and Wrapping

Create a dedicated errors package for custom error types and error handling utilities:

```go
// pkg/errors/errors.go
package errors

import (
	"errors"
	"fmt"
)

// Common error types
var (
	ErrNotFound      = errors.New("resource not found")
	ErrInvalidInput  = errors.New("invalid input")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrInternal      = errors.New("internal error")
	ErrDependency    = errors.New("dependency failure")
)

// Wrap adds context to an error
func Wrap(err error, message string) error {
	return fmt.Errorf("%s: %w", message, err)
}

// Is checks if an error is of a specific type
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// New creates a new error
func New(message string) error {
	return errors.New(message)
}

// IsNotFound checks if an error is a not found error
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsInvalidInput checks if an error is an invalid input error
func IsInvalidInput(err error) bool {
	return errors.Is(err, ErrInvalidInput)
}
```

### Error Handling Best Practices

1. **Use Error Types**:
   - Define custom error types for different error categories
   - Use error wrapping to add context while preserving the original error
   - Check error types with `errors.Is()` and `errors.As()`

2. **Graceful Degradation**:
   - Handle partial failures gracefully
   - Implement circuit breakers for external dependencies
   - Use timeouts for all external calls

3. **Error Response Mapping**:
   - Map internal errors to appropriate HTTP status codes
   - Sanitize error messages before returning to clients
   - Include error codes for programmatic handling

4. **Retry Logic**:
   - Implement exponential backoff for retryable errors
   - Set maximum retry limits
   - Use idempotent operations when possible

### Example Handler with Error Handling

```go
// internal/handler/handler.go
package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
	"go.uber.org/zap"
	
	"your-project/internal/service"
	"your-project/pkg/errors"
)

type Handler struct {
	service service.Service
	logger  *zap.SugaredLogger
}

func New(svc service.Service, logger *zap.SugaredLogger) *Handler {
	return &Handler{
		service: svc,
		logger:  logger,
	}
}

func (h *Handler) Handle(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Extract request ID for correlation
	requestID := request.RequestContext.RequestID
	h.logger.Infow("Processing request", "requestID", requestID, "path", request.Path)

	// Process request
	result, err := h.service.ProcessRequest(ctx, request.Body)
	if err != nil {
		return h.handleError(err, requestID)
	}

	// Return successful response
	responseBody, err := json.Marshal(result)
	if err != nil {
		h.logger.Errorw("Failed to marshal response", "requestID", requestID, "error", err)
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       `{"error":"Internal server error"}`,
		}, nil
	}

	h.logger.Infow("Request processed successfully", "requestID", requestID)
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Body:       string(responseBody),
	}, nil
}

func (h *Handler) handleError(err error, requestID string) (events.APIGatewayProxyResponse, error) {
	// Log the error with context
	h.logger.Errorw("Error processing request", "requestID", requestID, "error", err)

	// Map error types to appropriate HTTP status codes
	statusCode := http.StatusInternalServerError
	errorMessage := "Internal server error"

	if errors.IsNotFound(err) {
		statusCode = http.StatusNotFound
		errorMessage = "Resource not found"
	} else if errors.IsInvalidInput(err) {
		statusCode = http.StatusBadRequest
		errorMessage = "Invalid input"
	} else if errors.Is(err, errors.ErrUnauthorized) {
		statusCode = http.StatusUnauthorized
		errorMessage = "Unauthorized"
	}

	// Return error response
	return events.APIGatewayProxyResponse{
		StatusCode: statusCode,
		Body:       `{"error":"` + errorMessage + `"}`,
	}, nil
}
```

## 4. AWS Service Interaction

### Client Initialization

Initialize AWS clients in `main.go` and pass them to the repository layer:

```go
// internal/repository/repository.go
package repository

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/aws"
)

type Repository interface {
	GetItem(ctx context.Context, id string) (*Item, error)
	PutItem(ctx context.Context, item *Item) error
}

type DynamoDBRepository struct {
	client    *dynamodb.Client
	tableName string
}

func New(client *dynamodb.Client) Repository {
	return &DynamoDBRepository{
		client:    client,
		tableName: os.Getenv("TABLE_NAME"),
	}
}

func (r *DynamoDBRepository) GetItem(ctx context.Context, id string) (*Item, error) {
	// Implementation with proper error handling
}
```

### AWS Service Best Practices

1. **Use Interfaces**:
   - Define interfaces for AWS services to enable mocking in tests
   - Inject AWS clients via dependency injection

2. **Timeouts and Retries**:
   - Set appropriate timeouts for all AWS service calls
   - Configure retry policies for transient failures
   - Use context with deadlines for all operations

3. **Pagination Handling**:
   - Properly handle pagination for AWS service responses
   - Use pagination tokens correctly
   - Consider Lambda execution time limits when paginating

## 5. Testing

### Test Structure

```
lambda-function/
├── ...
└── internal/
    ├── handler/
    │   ├── handler.go
    │   └── handler_test.go    # Handler unit tests
    ├── service/
    │   ├── service.go
    │   └── service_test.go    # Service unit tests
    └── repository/
        ├── repository.go
        └── repository_test.go # Repository unit tests
```

### Testing Best Practices

1. **Unit Testing**:
   - Test each layer in isolation
   - Use mocks for AWS services and dependencies
   - Test error handling paths

2. **Integration Testing**:
   - Test integration with actual AWS services in a test environment
   - Clean up resources after tests

3. **Mocking**:
   - Use interfaces to enable mocking
   - Consider using testify/mock for mocking

### Example Test

```go
// internal/handler/handler_test.go
package handler

import (
	"context"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap/zaptest"

	"your-project/internal/service"
	"your-project/pkg/errors"
)

// MockService is a mock implementation of the Service interface
type MockService struct {
	mock.Mock
}

func (m *MockService) ProcessRequest(ctx context.Context, input string) (interface{}, error) {
	args := m.Called(ctx, input)
	return args.Get(0), args.Error(1)
}

func TestHandler_Handle_Success(t *testing.T) {
	// Setup
	mockSvc := new(MockService)
	logger := zaptest.NewLogger(t).Sugar()
	h := New(mockSvc, logger)

	// Mock expectations
	mockSvc.On("ProcessRequest", mock.Anything, "test-input").Return(map[string]string{"result": "success"}, nil)

	// Execute
	request := events.APIGatewayProxyRequest{
		Body: "test-input",
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
		},
	}
	response, err := h.Handle(context.Background(), request)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)
	assert.Contains(t, response.Body, "success")
	mockSvc.AssertExpectations(t)
}

func TestHandler_Handle_Error(t *testing.T) {
	// Setup
	mockSvc := new(MockService)
	logger := zaptest.NewLogger(t).Sugar()
	h := New(mockSvc, logger)

	// Mock expectations
	mockSvc.On("ProcessRequest", mock.Anything, "test-input").Return(nil, errors.ErrNotFound)

	// Execute
	request := events.APIGatewayProxyRequest{
		Body: "test-input",
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
		},
	}
	response, err := h.Handle(context.Background(), request)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 404, response.StatusCode)
	assert.Contains(t, response.Body, "not found")
	mockSvc.AssertExpectations(t)
}
```

## 6. Performance Optimization

### Cold Start Optimization

1. **Minimize Dependencies**:
   - Only import necessary packages
   - Use lightweight dependencies

2. **Initialization**:
   - Initialize clients and connections outside the handler function
   - Use global variables for reuse across invocations

3. **Memory Allocation**:
   - Pre-allocate slices and maps when size is known
   - Reuse buffers when processing large amounts of data

### Example Optimized Handler

```go
// Optimized for cold start performance
package main

import (
	"context"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"go.uber.org/zap"
)

// Global variables for reuse across invocations
var (
	logger *zap.SugaredLogger
	client *dynamodb.Client
)

// init function runs during cold start
func init() {
	// Initialize logger
	var err error
	logger, err = initLogger()
	if err != nil {
		os.Exit(1)
	}

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		logger.Fatalw("Failed to load AWS config", "error", err)
	}

	// Initialize AWS clients
	client = dynamodb.NewFromConfig(cfg)
}

func handler(ctx context.Context, event Event) (Response, error) {
	// Use pre-initialized clients and logger
	// Implementation here
}

func main() {
	lambda.Start(handler)
}
```

## 7. Security Considerations

### Environment Variables

1. **Sensitive Data**:
   - Never hardcode sensitive data
   - Use AWS Secrets Manager or Parameter Store for secrets
   - Validate environment variables at startup

2. **Least Privilege**:
   - Use IAM roles with minimal permissions
   - Scope permissions to specific resources
   - Regularly audit and rotate credentials

### Example Environment Variable Validation

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	TableName   string
	Region      string
	LogLevel    string
	Timeout     int
}

func Load() (*Config, error) {
	cfg := &Config{
		TableName:   os.Getenv("TABLE_NAME"),
		Region:      os.Getenv("AWS_REGION"),
		LogLevel:    strings.ToLower(os.Getenv("LOG_LEVEL")),
		Timeout:     30, // Default timeout
	}

	// Validate required configuration
	if cfg.TableName == "" {
		return nil, fmt.Errorf("TABLE_NAME environment variable is required")
	}

	// Set defaults for optional configuration
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}

	return cfg, nil
}
```

## 8. Deployment Best Practices

### Container Images

1. **AWS Base Images**:
   - Use AWS provided.al2023 OS-only runtime as the default Lambda container image
   - Use ARM architecture (arm64) for better performance and cost efficiency
   - Leverage AWS-optimized base images for better compatibility with Lambda environment

2. **Multi-stage Builds**:
   - Use multi-stage Docker builds to minimize image size
   - Include only necessary runtime dependencies

### Example Dockerfile with AWS Base Image and ARM Support

```dockerfile
# Build stage
FROM public.ecr.aws/docker/library/golang:1.21 AS build

WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application for ARM64 architecture
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /lambda ./cmd/main.go

# Runtime stage using AWS provided.al2023 base image
FROM public.ecr.aws/lambda/provided:al2023-arm64

# Copy the binary from the build stage
COPY --from=build /lambda /lambda

# Set the Lambda handler
ENTRYPOINT ["/lambda"]
```
