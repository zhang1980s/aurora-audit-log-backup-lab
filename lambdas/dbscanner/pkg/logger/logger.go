package logger

import (
	"context"
	"os"

	"github.com/aws/aws-xray-sdk-go/xray"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewLogger creates a new zap logger configured based on the provided log level
func NewLogger(level zapcore.Level) (*zap.SugaredLogger, error) {
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

// WithTraceID adds X-Ray trace ID to the logger if available
func WithTraceID(ctx context.Context, logger *zap.SugaredLogger) *zap.SugaredLogger {
	traceID := xray.TraceID(ctx)
	if traceID != "" {
		return logger.With(zap.String("xray_trace_id", traceID))
	}
	return logger
}
