package repository

import (
	"context"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-xray-sdk-go/xray"
	"go.uber.org/zap"

	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdetector/internal/models"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdetector/pkg/errors"
)

// Repository defines the interface for data access operations
type Repository interface {
	GetDBLogFiles(ctx context.Context, dbInstanceID string) ([]rdstypes.DescribeDBLogFilesDetails, error)
	GetLogFileRecord(ctx context.Context, dbInstanceID, logFileName string) (*models.LogFileRecord, error)
	CreateLogFileRecord(ctx context.Context, record models.LogFileRecord) error
	UpdateLogFileRecord(ctx context.Context, record models.LogFileRecord) error
}

// RDSRepository implements the Repository interface
type RDSRepository struct {
	rdsClient    *rds.Client
	dynamoClient *dynamodb.Client
	tableName    string
	logger       *zap.SugaredLogger
}

// NewRepository creates a new repository instance
func NewRepository(
	rdsClient *rds.Client,
	dynamoClient *dynamodb.Client,
	tableName string,
	logger *zap.SugaredLogger,
) Repository {
	return &RDSRepository{
		rdsClient:    rdsClient,
		dynamoClient: dynamoClient,
		tableName:    tableName,
		logger:       logger,
	}
}

// GetDBLogFiles gets all log files for a DB instance
func (r *RDSRepository) GetDBLogFiles(ctx context.Context, dbInstanceID string) ([]rdstypes.DescribeDBLogFilesDetails, error) {
	r.logger.Debugw("Getting log files for DB instance", "instanceID", dbInstanceID)

	var logFiles []rdstypes.DescribeDBLogFilesDetails
	var marker *string
	var pageCount int

	// Use pagination to get all log files
	for {
		pageCount++
		// Create subsegment for each pagination request
		ctx, apiSubsegment := xray.BeginSubsegment(ctx, "DescribeDBLogFiles-Page"+string(rune(pageCount)))

		resp, err := r.rdsClient.DescribeDBLogFiles(ctx, &rds.DescribeDBLogFilesInput{
			DBInstanceIdentifier: aws.String(dbInstanceID),
			Marker:               marker,
		})
		if err != nil {
			apiSubsegment.AddError(err)
			apiSubsegment.Close(err)
			return nil, errors.Wrap(errors.ErrRDSAPI, err.Error())
		}

		logFiles = append(logFiles, resp.DescribeDBLogFiles...)

		// Add metadata about the pagination
		xray.AddMetadata(ctx, "page_log_files_count", len(resp.DescribeDBLogFiles))
		apiSubsegment.Close(nil)

		// Check if there are more pages
		if resp.Marker == nil {
			break
		}
		marker = resp.Marker
	}

	r.logger.Debugw("Found log files for DB instance",
		"instanceID", dbInstanceID,
		"count", len(logFiles))

	// Add annotation for total log files count
	xray.AddAnnotation(ctx, "total_log_files", len(logFiles))
	return logFiles, nil
}

// GetLogFileRecord gets a log file record from DynamoDB
func (r *RDSRepository) GetLogFileRecord(ctx context.Context, dbInstanceID, logFileName string) (*models.LogFileRecord, error) {
	r.logger.Debugw("Checking for existing record for log file", "logFile", logFileName)

	// Add annotations for the query parameters
	xray.AddAnnotation(ctx, "db_instance_id", dbInstanceID)
	xray.AddAnnotation(ctx, "log_file_name", logFileName)

	resp, err := r.dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"DBInstanceIdentifier": &types.AttributeValueMemberS{Value: dbInstanceID},
			"LogFileName":          &types.AttributeValueMemberS{Value: logFileName},
		},
	})
	if err != nil {
		return nil, errors.Wrap(errors.ErrDynamoDB, err.Error())
	}

	if len(resp.Item) == 0 {
		// Item not found
		xray.AddMetadata(ctx, "record_exists", false)
		return nil, nil
	}

	// Unmarshal the item into a LogFileRecord
	var record models.LogFileRecord
	err = attributevalue.UnmarshalMap(resp.Item, &record)
	if err != nil {
		return nil, errors.Wrap(errors.ErrDynamoDB, "failed to unmarshal record: "+err.Error())
	}

	xray.AddMetadata(ctx, "record_exists", true)
	xray.AddMetadata(ctx, "record_size", record.Size)
	xray.AddMetadata(ctx, "record_last_written", record.LastWritten)
	return &record, nil
}

// CreateLogFileRecord creates a new log file record in DynamoDB
func (r *RDSRepository) CreateLogFileRecord(ctx context.Context, record models.LogFileRecord) error {
	r.logger.Infow("Creating new record for log file",
		"logFile", record.LogFileName,
		"size", record.Size,
		"lastWritten", record.LastWritten,
		"humanReadableLastWritten", record.HumanReadableLastWritten)

	// Add metadata about the record being created
	xray.AddMetadata(ctx, "record", record)

	item, err := attributevalue.MarshalMap(record)
	if err != nil {
		return errors.Wrap(errors.ErrDynamoDB, "failed to marshal record: "+err.Error())
	}

	_, err = r.dynamoClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.tableName),
		Item:      item,
	})

	if err != nil {
		return errors.Wrap(errors.ErrDynamoDB, "failed to put item: "+err.Error())
	}

	return nil
}

// UpdateLogFileRecord updates an existing log file record in DynamoDB
func (r *RDSRepository) UpdateLogFileRecord(ctx context.Context, record models.LogFileRecord) error {
	r.logger.Infow("Updating record for log file",
		"logFile", record.LogFileName,
		"size", record.Size,
		"lastWritten", record.LastWritten,
		"humanReadableLastWritten", record.HumanReadableLastWritten)

	// Add metadata about the record being updated
	xray.AddMetadata(ctx, "record", record)

	// Create update expression
	updateExpression := "SET #size = :size, #lastWritten = :lastWritten, #humanReadableLastWritten = :humanReadableLastWritten"
	expressionAttributeNames := map[string]string{
		"#size":                     "Size",
		"#lastWritten":              "LastWritten",
		"#humanReadableLastWritten": "HumanReadableLastWritten",
	}
	expressionAttributeValues := map[string]types.AttributeValue{
		":size":                     &types.AttributeValueMemberN{Value: strconv.FormatInt(record.Size, 10)},
		":lastWritten":              &types.AttributeValueMemberN{Value: strconv.FormatInt(record.LastWritten, 10)},
		":humanReadableLastWritten": &types.AttributeValueMemberS{Value: record.HumanReadableLastWritten},
	}

	// Include LastBackup if it exists
	if record.LastBackup > 0 {
		updateExpression += ", #lastBackup = :lastBackup, #humanReadableLastBackup = :humanReadableLastBackup"
		expressionAttributeNames["#lastBackup"] = "LastBackup"
		expressionAttributeNames["#humanReadableLastBackup"] = "HumanReadableLastBackup"
		expressionAttributeValues[":lastBackup"] = &types.AttributeValueMemberN{Value: strconv.FormatInt(record.LastBackup, 10)}

		// Generate human-readable timestamp if not already set
		if record.HumanReadableLastBackup == "" {
			record.HumanReadableLastBackup = time.UnixMilli(record.LastBackup).Format(time.RFC3339)
		}
		expressionAttributeValues[":humanReadableLastBackup"] = &types.AttributeValueMemberS{Value: record.HumanReadableLastBackup}
	}

	// Include SHA256Checksum if it exists
	if record.SHA256Checksum != "" {
		updateExpression += ", #sha256Checksum = :sha256Checksum"
		expressionAttributeNames["#sha256Checksum"] = "SHA256Checksum"
		expressionAttributeValues[":sha256Checksum"] = &types.AttributeValueMemberS{Value: record.SHA256Checksum}
	}

	_, err := r.dynamoClient.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"DBInstanceIdentifier": &types.AttributeValueMemberS{Value: record.DBInstanceIdentifier},
			"LogFileName":          &types.AttributeValueMemberS{Value: record.LogFileName},
		},
		UpdateExpression:          aws.String(updateExpression),
		ExpressionAttributeNames:  expressionAttributeNames,
		ExpressionAttributeValues: expressionAttributeValues,
	})

	if err != nil {
		return errors.Wrap(errors.ErrDynamoDB, "failed to update item: "+err.Error())
	}

	return nil
}
