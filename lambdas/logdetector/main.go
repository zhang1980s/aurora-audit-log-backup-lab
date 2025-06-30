package main

import (
	"context"
	"log"
	"os"
	"strconv"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
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
func Handler(ctx context.Context, sqsEvent events.SQSEvent) error {
	// Initialize logger
	logger := log.New(os.Stdout, "", log.LstdFlags)
	logger.Println("Starting Log File Detector Lambda")

	// Get DynamoDB table name from environment variable
	tableName := os.Getenv("DYNAMODB_TABLE_NAME")
	if tableName == "" {
		logger.Println("Error: DYNAMODB_TABLE_NAME environment variable not set")
		return nil
	}

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Printf("Error loading AWS config: %v\n", err)
		return err
	}

	// Create RDS client
	rdsClient := rds.NewFromConfig(cfg)

	// Create DynamoDB client
	dynamoClient := dynamodb.NewFromConfig(cfg)

	// Process each SQS message
	for _, message := range sqsEvent.Records {
		// The message body contains the DB instance ID
		dbInstanceID := message.Body
		logger.Printf("Processing DB instance: %s\n", dbInstanceID)

		// Get log files for the DB instance
		logFiles, err := getDBLogFiles(ctx, rdsClient, dbInstanceID, logger)
		if err != nil {
			logger.Printf("Error getting log files for instance %s: %v\n", dbInstanceID, err)
			continue
		}

		// Process each log file
		for _, logFile := range logFiles {
			// Check if the log file is an audit log
			if !isAuditLog(logFile.LogFileName) {
				continue
			}

			// Create a record for the log file
			record := LogFileRecord{
				DBInstanceIdentifier: dbInstanceID,
				LogFileName:          logFile.LogFileName,
				Size:                 logFile.Size,
				LastWritten:          logFile.LastWritten,
			}

			// Check if the record already exists in DynamoDB
			existingRecord, err := getLogFileRecord(ctx, dynamoClient, tableName, dbInstanceID, logFile.LogFileName, logger)
			if err != nil {
				logger.Printf("Error checking for existing record: %v\n", err)
				continue
			}

			if existingRecord == nil {
				// Record doesn't exist, create a new one
				err = createLogFileRecord(ctx, dynamoClient, tableName, record, logger)
				if err != nil {
					logger.Printf("Error creating record: %v\n", err)
					continue
				}
			} else if existingRecord.Size != record.Size || existingRecord.LastWritten != record.LastWritten {
				// Record exists but has changed, update it
				record.LastBackup = existingRecord.LastBackup // Preserve the LastBackup value
				err = updateLogFileRecord(ctx, dynamoClient, tableName, record, logger)
				if err != nil {
					logger.Printf("Error updating record: %v\n", err)
					continue
				}
			} else {
				// Record exists and hasn't changed, skip it
				logger.Printf("Log file %s hasn't changed, skipping\n", logFile.LogFileName)
			}
		}
	}

	return nil
}

// getDBLogFiles gets all log files for a DB instance
func getDBLogFiles(ctx context.Context, client *rds.Client, dbInstanceID string, logger *log.Logger) ([]rds.DescribeDBLogFilesDetails, error) {
	logger.Printf("Getting log files for DB instance %s\n", dbInstanceID)

	var logFiles []rds.DescribeDBLogFilesDetails
	var marker *string

	// Use pagination to get all log files
	for {
		resp, err := client.DescribeDBLogFiles(ctx, &rds.DescribeDBLogFilesInput{
			DBInstanceIdentifier: aws.String(dbInstanceID),
			Marker:               marker,
		})
		if err != nil {
			return nil, err
		}

		logFiles = append(logFiles, resp.DescribeDBLogFiles...)

		// Check if there are more pages
		if resp.Marker == nil {
			break
		}
		marker = resp.Marker
	}

	logger.Printf("Found %d log files for DB instance %s\n", len(logFiles), dbInstanceID)
	return logFiles, nil
}

// isAuditLog checks if a log file is an audit log
func isAuditLog(logFileName string) bool {
	// Check if the log file name contains "audit" or has a specific pattern
	// This will depend on your Aurora MySQL audit log naming convention
	return logFileName == "audit.log" ||
		logFileName == "audit/server_audit.log" ||
		logFileName == "error/mysql-audit.log" ||
		(len(logFileName) >= 5 && logFileName[0:5] == "audit")
}

// getLogFileRecord gets a log file record from DynamoDB
func getLogFileRecord(ctx context.Context, client *dynamodb.Client, tableName string, dbInstanceID string, logFileName string, logger *log.Logger) (*LogFileRecord, error) {
	logger.Printf("Checking for existing record for log file %s\n", logFileName)

	resp, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"DBInstanceIdentifier": &types.AttributeValueMemberS{Value: dbInstanceID},
			"LogFileName":          &types.AttributeValueMemberS{Value: logFileName},
		},
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Item) == 0 {
		// Item not found
		return nil, nil
	}

	// Unmarshal the item into a LogFileRecord
	var record LogFileRecord
	err = attributevalue.UnmarshalMap(resp.Item, &record)
	if err != nil {
		return nil, err
	}

	return &record, nil
}

// createLogFileRecord creates a new log file record in DynamoDB
func createLogFileRecord(ctx context.Context, client *dynamodb.Client, tableName string, record LogFileRecord, logger *log.Logger) error {
	logger.Printf("Creating new record for log file %s\n", record.LogFileName)

	item, err := attributevalue.MarshalMap(record)
	if err != nil {
		return err
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(tableName),
		Item:      item,
	})

	return err
}

// updateLogFileRecord updates an existing log file record in DynamoDB
func updateLogFileRecord(ctx context.Context, client *dynamodb.Client, tableName string, record LogFileRecord, logger *log.Logger) error {
	logger.Printf("Updating record for log file %s\n", record.LogFileName)

	// Create update expression
	updateExpression := "SET #size = :size, #lastWritten = :lastWritten"
	expressionAttributeNames := map[string]string{
		"#size":        "Size",
		"#lastWritten": "LastWritten",
	}
	expressionAttributeValues := map[string]types.AttributeValue{
		":size":        &types.AttributeValueMemberN{Value: strconv.FormatInt(record.Size, 10)},
		":lastWritten": &types.AttributeValueMemberN{Value: strconv.FormatInt(record.LastWritten, 10)},
	}

	// Include LastBackup if it exists
	if record.LastBackup > 0 {
		updateExpression += ", #lastBackup = :lastBackup"
		expressionAttributeNames["#lastBackup"] = "LastBackup"
		expressionAttributeValues[":lastBackup"] = &types.AttributeValueMemberN{Value: strconv.FormatInt(record.LastBackup, 10)}
	}

	_, err := client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"DBInstanceIdentifier": &types.AttributeValueMemberS{Value: record.DBInstanceIdentifier},
			"LogFileName":          &types.AttributeValueMemberS{Value: record.LogFileName},
		},
		UpdateExpression:          aws.String(updateExpression),
		ExpressionAttributeNames:  expressionAttributeNames,
		ExpressionAttributeValues: expressionAttributeValues,
	})

	return err
}

func main() {
	lambda.Start(Handler)
}
