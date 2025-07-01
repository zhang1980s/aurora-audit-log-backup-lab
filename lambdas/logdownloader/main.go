package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// LogFileRecord represents a record in the DynamoDB table
type LogFileRecord struct {
	DBInstanceIdentifier string `dynamodbav:"DBInstanceIdentifier"`
	LogFileName          string `dynamodbav:"LogFileName"`
	Size                 int64  `dynamodbav:"Size"`
	LastWritten          int64  `dynamodbav:"LastWritten"`
	LastBackup           int64  `dynamodbav:"LastBackup,omitempty"`
}

// Handler is the Lambda function handler
func Handler(ctx context.Context, event events.DynamoDBEvent) error {
	// Initialize logger
	logger := log.New(os.Stdout, "", log.LstdFlags)
	logger.Println("Starting Log File Downloader Lambda")

	// Get environment variables
	tableName := os.Getenv("DYNAMODB_TABLE_NAME")
	if tableName == "" {
		logger.Println("Error: DYNAMODB_TABLE_NAME environment variable not set")
		return nil
	}

	bucketName := os.Getenv("S3_BUCKET_NAME")
	if bucketName == "" {
		logger.Println("Error: S3_BUCKET_NAME environment variable not set")
		return nil
	}

	s3Prefix := os.Getenv("S3_PREFIX")
	if s3Prefix == "" {
		s3Prefix = "logs" // Default prefix
	}

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Printf("Error loading AWS config: %v\n", err)
		return err
	}

	// Create clients
	rdsClient := rds.NewFromConfig(cfg)
	s3Client := s3.NewFromConfig(cfg)
	dynamoClient := dynamodb.NewFromConfig(cfg)

	// Process each DynamoDB stream record
	for _, record := range event.Records {
		// Skip records that are not INSERT or MODIFY
		if record.EventName != "INSERT" && record.EventName != "MODIFY" {
			continue
		}

		// Parse the DynamoDB record
		var logFileRecord LogFileRecord
		err := unmarshalDynamoDBEvent(record.Change.NewImage, &logFileRecord)
		if err != nil {
			logger.Printf("Error unmarshalling DynamoDB record: %v\n", err)
			continue
		}

		// Skip if LastBackup is recent and Size/LastWritten haven't changed
		if record.EventName == "MODIFY" && !shouldDownload(record.Change.OldImage, record.Change.NewImage, logger) {
			logger.Printf("Skipping download for %s, no significant changes\n", logFileRecord.LogFileName)
			continue
		}

		// Download the log file
		logContent, err := downloadLogFile(ctx, rdsClient, logFileRecord.DBInstanceIdentifier, logFileRecord.LogFileName, logger)
		if err != nil {
			logger.Printf("Error downloading log file: %v\n", err)
			continue
		}

		// Upload to S3
		s3Key := fmt.Sprintf("%s/%s/%s", s3Prefix, logFileRecord.DBInstanceIdentifier, logFileRecord.LogFileName)
		err = uploadToS3(ctx, s3Client, bucketName, s3Key, logContent, logger)
		if err != nil {
			logger.Printf("Error uploading to S3: %v\n", err)
			continue
		}

		// Update LastBackup timestamp in DynamoDB
		err = updateLastBackup(ctx, dynamoClient, tableName, logFileRecord.DBInstanceIdentifier, logFileRecord.LogFileName, logger)
		if err != nil {
			logger.Printf("Error updating LastBackup timestamp: %v\n", err)
			continue
		}

		logger.Printf("Successfully processed log file %s for instance %s\n", logFileRecord.LogFileName, logFileRecord.DBInstanceIdentifier)
	}

	return nil
}

// unmarshalDynamoDBEvent unmarshals a DynamoDB event record into a struct
func unmarshalDynamoDBEvent(image map[string]events.DynamoDBAttributeValue, out interface{}) error {
	// Convert events.DynamoDBAttributeValue to map[string]interface{}
	item := make(map[string]interface{})

	for k, v := range image {
		switch v.DataType() {
		case events.DataTypeString:
			item[k] = v.String()
		case events.DataTypeNumber:
			item[k] = v.Number()
		case events.DataTypeBinary:
			item[k] = v.Binary()
		case events.DataTypeBoolean:
			item[k] = v.Boolean()
		case events.DataTypeNull:
			item[k] = nil
		case events.DataTypeList:
			list := make([]interface{}, len(v.List()))
			for i, lv := range v.List() {
				var err error
				list[i], err = convertDynamoDBAttributeValue(lv)
				if err != nil {
					return err
				}
			}
			item[k] = list
		case events.DataTypeMap:
			m := make(map[string]interface{})
			for mk, mv := range v.Map() {
				var err error
				m[mk], err = convertDynamoDBAttributeValue(mv)
				if err != nil {
					return err
				}
			}
			item[k] = m
		default:
			return fmt.Errorf("unsupported data type: %s", v.DataType())
		}
	}

	// Use attributevalue to unmarshal the map into the struct
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return err
	}

	return attributevalue.UnmarshalMap(av, out)
}

// convertDynamoDBAttributeValue converts a DynamoDB attribute value to a Go type
func convertDynamoDBAttributeValue(v events.DynamoDBAttributeValue) (interface{}, error) {
	switch v.DataType() {
	case events.DataTypeString:
		return v.String(), nil
	case events.DataTypeNumber:
		return v.Number(), nil
	case events.DataTypeBinary:
		return v.Binary(), nil
	case events.DataTypeBoolean:
		return v.Boolean(), nil
	case events.DataTypeNull:
		return nil, nil
	case events.DataTypeList:
		list := make([]interface{}, len(v.List()))
		for i, lv := range v.List() {
			var err error
			list[i], err = convertDynamoDBAttributeValue(lv)
			if err != nil {
				return nil, err
			}
		}
		return list, nil
	case events.DataTypeMap:
		m := make(map[string]interface{})
		for mk, mv := range v.Map() {
			var err error
			m[mk], err = convertDynamoDBAttributeValue(mv)
			if err != nil {
				return nil, err
			}
		}
		return m, nil
	default:
		return nil, fmt.Errorf("unsupported data type: %s", v.DataType())
	}
}

// shouldDownload determines if a log file should be downloaded based on changes
func shouldDownload(oldImage, newImage map[string]events.DynamoDBAttributeValue, logger *log.Logger) bool {
	// If Size or LastWritten has changed, download the log file
	if oldSize, ok := oldImage["Size"]; ok {
		if newSize, ok := newImage["Size"]; ok {
			if oldSize.Number() != newSize.Number() {
				return true
			}
		}
	}

	if oldLastWritten, ok := oldImage["LastWritten"]; ok {
		if newLastWritten, ok := newImage["LastWritten"]; ok {
			if oldLastWritten.Number() != newLastWritten.Number() {
				return true
			}
		}
	}

	// If LastBackup doesn't exist or is older than 24 hours, download the log file
	lastBackup, exists := newImage["LastBackup"]
	if !exists {
		return true
	}

	lastBackupStr := lastBackup.Number()
	lastBackupVal, err := strconv.ParseInt(lastBackupStr, 10, 64)
	if err != nil {
		logger.Printf("Error parsing LastBackup: %v\n", err)
		return true
	}

	// If LastBackup is older than 24 hours, download the log file
	twentyFourHoursAgo := time.Now().Unix() - 24*60*60
	return lastBackupVal < twentyFourHoursAgo
}

// downloadLogFile downloads a log file from an Aurora DB instance
func downloadLogFile(ctx context.Context, client *rds.Client, dbInstanceID, logFileName string, logger *log.Logger) ([]byte, error) {
	logger.Printf("Downloading log file %s from instance %s\n", logFileName, dbInstanceID)

	var logContent bytes.Buffer
	var marker *string

	// Use pagination to download the entire log file
	for {
		resp, err := client.DownloadDBLogFilePortion(ctx, &rds.DownloadDBLogFilePortionInput{
			DBInstanceIdentifier: aws.String(dbInstanceID),
			LogFileName:          aws.String(logFileName),
			Marker:               marker,
		})
		if err != nil {
			return nil, err
		}

		// Append the log file portion to the buffer
		if resp.LogFileData != nil {
			logContent.WriteString(*resp.LogFileData)
		}

		// Check if there are more pages
		if resp.AdditionalDataPending == nil || !*resp.AdditionalDataPending {
			break
		}
		marker = resp.Marker
	}

	logger.Printf("Downloaded %d bytes from log file %s\n", logContent.Len(), logFileName)
	return logContent.Bytes(), nil
}

// uploadToS3 uploads a log file to S3
func uploadToS3(ctx context.Context, client *s3.Client, bucketName, key string, content []byte, logger *log.Logger) error {
	logger.Printf("Uploading log file to S3: s3://%s/%s\n", bucketName, key)

	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("text/plain"),
	})

	return err
}

// updateLastBackup updates the LastBackup timestamp in DynamoDB
func updateLastBackup(ctx context.Context, client *dynamodb.Client, tableName, dbInstanceID, logFileName string, logger *log.Logger) error {
	logger.Printf("Updating LastBackup timestamp for log file %s\n", logFileName)

	now := time.Now().Unix()

	_, err := client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"DBInstanceIdentifier": &types.AttributeValueMemberS{Value: dbInstanceID},
			"LogFileName":          &types.AttributeValueMemberS{Value: logFileName},
		},
		UpdateExpression: aws.String("SET LastBackup = :lastBackup"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":lastBackup": &types.AttributeValueMemberN{Value: strconv.FormatInt(now, 10)},
		},
	})

	return err
}

func main() {
	lambda.Start(Handler)
}
