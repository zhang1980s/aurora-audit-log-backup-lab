package repository

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-xray-sdk-go/xray"
	"go.uber.org/zap"

	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdownloader/internal/models"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdownloader/pkg/errors"
)

// Repository defines the interface for data access operations
type Repository interface {
	UpdateLastBackup(ctx context.Context, dbInstanceID, logFileName, sha256sum string) error
	UploadToS3(ctx context.Context, key string, content []byte, sha256sum string) error
	GetLogFileRecord(ctx context.Context, dbInstanceID, logFileName string) (*models.LogFileRecord, error)
}

// DynamoDBRepository implements the Repository interface
type DynamoDBRepository struct {
	dynamoClient *dynamodb.Client
	s3Client     *s3.Client
	tableName    string
	bucketName   string
	logger       *zap.SugaredLogger
}

// NewRepository creates a new repository instance
func NewRepository(
	dynamoClient *dynamodb.Client,
	s3Client *s3.Client,
	tableName string,
	bucketName string,
	logger *zap.SugaredLogger,
) Repository {
	return &DynamoDBRepository{
		dynamoClient: dynamoClient,
		s3Client:     s3Client,
		tableName:    tableName,
		bucketName:   bucketName,
		logger:       logger,
	}
}

// UpdateLastBackup updates the LastBackup timestamp and SHA256 checksum in DynamoDB
func (r *DynamoDBRepository) UpdateLastBackup(ctx context.Context, dbInstanceID, logFileName, sha256sum string) error {
	// Create subsegment for the entire function
	ctx, functionSubsegment := xray.BeginSubsegment(ctx, "repository.UpdateLastBackup")
	defer functionSubsegment.Close(nil)

	// Record start time for performance metrics
	functionStartTime := time.Now()

	r.logger.Debugw("Updating LastBackup timestamp and SHA256 checksum for log file",
		"logFile", logFileName)

	// Add X-Ray annotations for the update operation
	xray.AddAnnotation(ctx, "db_instance_id", dbInstanceID)
	xray.AddAnnotation(ctx, "log_file_name", logFileName)
	xray.AddAnnotation(ctx, "table_name", r.tableName)

	// Get TTL_DAYS from environment variable (default to 7 if not set)
	ttlDaysStr := os.Getenv("TTL_DAYS")
	ttlDays := 7
	if ttlDaysStr != "" {
		var err error
		ttlDays, err = strconv.Atoi(ttlDaysStr)
		if err != nil {
			r.logger.Warnw("Invalid TTL_DAYS value, using default of 7 days", "value", ttlDaysStr, "error", err)
			ttlDays = 7
		}
	}

	// Calculate current time and expiration time (current time + TTL_DAYS)
	now := time.Now()
	currentMillis := now.UnixMilli()
	humanReadableCurrentTime := now.Format(time.RFC3339)

	expirationTime := now.AddDate(0, 0, ttlDays)
	expirationMillis := expirationTime.UnixMilli()
	humanReadableExpirationTime := expirationTime.Format(time.RFC3339)

	xray.AddMetadata(ctx, "ttl_days", ttlDays)
	xray.AddMetadata(ctx, "current_millis", currentMillis)
	xray.AddMetadata(ctx, "current_human", humanReadableCurrentTime)
	xray.AddMetadata(ctx, "expiration_millis", expirationMillis)
	xray.AddMetadata(ctx, "expiration_human", humanReadableExpirationTime)
	r.logger.Debugw("Setting backup timestamps",
		"ttlDays", ttlDays,
		"currentMillis", currentMillis,
		"humanReadableCurrent", humanReadableCurrentTime,
		"expirationMillis", expirationMillis,
		"humanReadableExpiration", humanReadableExpirationTime)

	// Get the record to retrieve the SHA256 checksum
	ctx, getItemSubsegment := xray.BeginSubsegment(ctx, "GetItem")
	getItemStartTime := time.Now()

	_, err := r.dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"DBInstanceIdentifier": &types.AttributeValueMemberS{Value: dbInstanceID},
			"LogFileName":          &types.AttributeValueMemberS{Value: logFileName},
		},
	})

	// Record GetItem duration
	getItemDuration := time.Since(getItemStartTime)
	xray.AddMetadata(ctx, "get_item_duration_ms", getItemDuration.Milliseconds())
	getItemSubsegment.Close(nil)

	if err != nil {
		r.logger.Errorw("Failed to get log file record",
			"error", err,
			"dbInstanceID", dbInstanceID,
			"logFileName", logFileName)
		functionSubsegment.AddError(err)
		return errors.Wrap(err, "failed to get log file record")
	}

	// Create update expression with SHA256 checksum if available
	updateExpression := "SET LastBackup = :lastBackup, HumanReadableLastBackup = :humanReadableLastBackup, ExpirationTime = :expirationTime, HumanReadableExpiration = :humanReadableExpiration"
	expressionAttributeValues := map[string]types.AttributeValue{
		":lastBackup":              &types.AttributeValueMemberN{Value: strconv.FormatInt(currentMillis, 10)},
		":humanReadableLastBackup": &types.AttributeValueMemberS{Value: humanReadableCurrentTime},
		":expirationTime":          &types.AttributeValueMemberN{Value: strconv.FormatInt(expirationMillis, 10)},
		":humanReadableExpiration": &types.AttributeValueMemberS{Value: humanReadableExpirationTime},
	}

	// Add SHA256 checksum if provided
	if sha256sum != "" {
		updateExpression += ", SHA256Checksum = :sha256"
		expressionAttributeValues[":sha256"] = &types.AttributeValueMemberS{Value: sha256sum}
		r.logger.Debugw("Including SHA256 checksum in update", "sha256", sha256sum)
	}

	// Perform the DynamoDB update
	ctx, updateItemSubsegment := xray.BeginSubsegment(ctx, "UpdateItem")
	updateItemStartTime := time.Now()

	_, err = r.dynamoClient.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"DBInstanceIdentifier": &types.AttributeValueMemberS{Value: dbInstanceID},
			"LogFileName":          &types.AttributeValueMemberS{Value: logFileName},
		},
		UpdateExpression:          aws.String(updateExpression),
		ExpressionAttributeValues: expressionAttributeValues,
	})

	// Record UpdateItem duration
	updateItemDuration := time.Since(updateItemStartTime)
	xray.AddMetadata(ctx, "update_item_duration_ms", updateItemDuration.Milliseconds())
	updateItemSubsegment.Close(nil)

	// Record error in X-Ray if one occurred
	if err != nil {
		xray.AddError(ctx, err)
		functionSubsegment.AddError(err)
		r.logger.Errorw("Failed to update LastBackup timestamp",
			"error", err,
			"dbInstanceID", dbInstanceID,
			"logFileName", logFileName)
		return errors.Wrap(err, "failed to update LastBackup timestamp")
	}

	// Record total function duration
	functionDuration := time.Since(functionStartTime)
	xray.AddMetadata(ctx, "total_function_duration_ms", functionDuration.Milliseconds())

	return nil
}

// UploadToS3 uploads a log file to S3 with SHA256 checksum
func (r *DynamoDBRepository) UploadToS3(ctx context.Context, key string, content []byte, sha256sum string) error {
	ctx, subsegment := xray.BeginSubsegment(ctx, "repository.UploadToS3")
	defer subsegment.Close(nil)

	r.logger.Debugw("Uploading log file to S3",
		"bucket", r.bucketName,
		"key", key,
		"sha256", sha256sum)

	// Add X-Ray annotations for the S3 upload operation
	xray.AddAnnotation(ctx, "bucket", r.bucketName)
	xray.AddAnnotation(ctx, "key", key)
	xray.AddMetadata(ctx, "content_size", len(content))
	xray.AddMetadata(ctx, "content_sha256", sha256sum)

	// Convert hexadecimal SHA256 to binary and then to base64 for S3 API
	// AWS S3 expects the ChecksumSHA256 to be base64 encoded, not hex
	var binarySha256 []byte
	var err error

	// Log the incoming SHA256 format for debugging
	r.logger.Debugw("SHA256 checksum format",
		"hex_sha256", sha256sum,
		"length", len(sha256sum))

	// Convert hex string to binary
	binarySha256, err = hex.DecodeString(sha256sum)
	if err != nil {
		r.logger.Errorw("Failed to decode hex SHA256 checksum",
			"error", err,
			"sha256", sha256sum)
		xray.AddError(ctx, err)
		subsegment.AddError(err)
		return errors.Wrap(errors.ErrUpload, "failed to decode hex SHA256 checksum")
	}

	// Convert binary to base64
	base64Sha256 := base64.StdEncoding.EncodeToString(binarySha256)

	r.logger.Debugw("Converted SHA256 checksum",
		"original_hex", sha256sum,
		"base64", base64Sha256)

	// Perform the S3 upload with SHA256 checksum as metadata
	_, err = r.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(r.bucketName),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("text/plain"),
		Metadata: map[string]string{
			"SHA256Checksum": sha256sum, // Keep original hex in metadata
		},
		ChecksumSHA256: aws.String(base64Sha256), // Use base64 for S3 API
	})

	// Record error in X-Ray if one occurred
	if err != nil {
		xray.AddError(ctx, err)
		subsegment.AddError(err)
		r.logger.Errorw("Failed to upload log file to S3",
			"error", err,
			"bucket", r.bucketName,
			"key", key)
		return errors.Wrap(errors.ErrUpload, err.Error())
	}

	return nil
}

// GetLogFileRecord retrieves a log file record from DynamoDB
func (r *DynamoDBRepository) GetLogFileRecord(ctx context.Context, dbInstanceID, logFileName string) (*models.LogFileRecord, error) {
	ctx, subsegment := xray.BeginSubsegment(ctx, "repository.GetLogFileRecord")
	defer subsegment.Close(nil)

	r.logger.Debugw("Getting log file record",
		"dbInstanceID", dbInstanceID,
		"logFileName", logFileName)

	// Add X-Ray annotations
	xray.AddAnnotation(ctx, "db_instance_id", dbInstanceID)
	xray.AddAnnotation(ctx, "log_file_name", logFileName)

	// Get the item from DynamoDB
	result, err := r.dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"DBInstanceIdentifier": &types.AttributeValueMemberS{Value: dbInstanceID},
			"LogFileName":          &types.AttributeValueMemberS{Value: logFileName},
		},
	})

	if err != nil {
		xray.AddError(ctx, err)
		subsegment.AddError(err)
		r.logger.Errorw("Failed to get log file record",
			"error", err,
			"dbInstanceID", dbInstanceID,
			"logFileName", logFileName)
		return nil, errors.Wrap(err, "failed to get log file record")
	}

	// Check if item exists
	if result.Item == nil {
		err := errors.Wrap(errors.ErrNotFound, "log file record not found")
		xray.AddError(ctx, err)
		subsegment.AddError(err)
		return nil, err
	}

	// Unmarshal the item
	var record models.LogFileRecord
	err = attributevalue.UnmarshalMap(result.Item, &record)
	if err != nil {
		xray.AddError(ctx, err)
		subsegment.AddError(err)
		r.logger.Errorw("Failed to unmarshal log file record",
			"error", err,
			"dbInstanceID", dbInstanceID,
			"logFileName", logFileName)
		return nil, errors.Wrap(err, "failed to unmarshal log file record")
	}

	return &record, nil
}
