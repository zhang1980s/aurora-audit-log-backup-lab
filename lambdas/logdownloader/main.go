package main

import (
	"bytes"
	"context"
	"crypto/md5"
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
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
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

		// Download the log file using both methods
		logger.Printf("Downloading log file %s using both methods\n", logFileRecord.LogFileName)

		// Method 1: Using DownloadDBLogFilePortion API (SDK) with pagination
		sdkLogContent, err := downloadLogFile(ctx, rdsClient, logFileRecord.DBInstanceIdentifier, logFileRecord.LogFileName, logger)
		if err != nil {
			logger.Printf("Error downloading log file using SDK method: %v\n", err)
			continue
		}

		// Calculate MD5 checksum for SDK method
		sdkMD5 := calculateMD5(sdkLogContent)
		logger.Printf("SDK method MD5 checksum: %s\n", sdkMD5)

		// Method 2: Using DownloadDBLogFilePortion API with NumberOfLines=0 and Marker=0
		restLogContent, err := downloadCompleteLogFile(ctx, rdsClient, logFileRecord.DBInstanceIdentifier, logFileRecord.LogFileName, logger)
		if err != nil {
			logger.Printf("Error downloading log file using REST endpoint method: %v\n", err)
			// Continue with just the SDK method if REST endpoint method fails
		} else {
			// Calculate MD5 checksum for REST endpoint method
			restMD5 := calculateMD5(restLogContent)
			logger.Printf("REST endpoint method MD5 checksum: %s\n", restMD5)

			// Compare checksums
			if sdkMD5 == restMD5 {
				logger.Printf("MD5 checksums match between methods: %s\n", sdkMD5)
			} else {
				logger.Printf("WARNING: MD5 checksums do not match between methods!\n")
				logger.Printf("SDK: %s\n", sdkMD5)
				logger.Printf("REST: %s\n", restMD5)
			}
		}

		// Upload both files to S3
		// 1. Upload SDK method result
		sdkS3Key := fmt.Sprintf("%s/%s/%s-sdk", s3Prefix, logFileRecord.DBInstanceIdentifier, logFileRecord.LogFileName)
		err = uploadToS3(ctx, s3Client, bucketName, sdkS3Key, sdkLogContent, logger)
		if err != nil {
			logger.Printf("Error uploading SDK method result to S3: %v\n", err)
			continue
		}

		// 2. Upload REST endpoint method result if available
		if restLogContent != nil {
			restS3Key := fmt.Sprintf("%s/%s/%s-rest", s3Prefix, logFileRecord.DBInstanceIdentifier, logFileRecord.LogFileName)
			err = uploadToS3(ctx, s3Client, bucketName, restS3Key, restLogContent, logger)
			if err != nil {
				logger.Printf("Error uploading REST endpoint method result to S3: %v\n", err)
				// Continue anyway since we at least uploaded the SDK method result
			}
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

	// Special handling for numeric fields that might be strings
	_, isLogRecord := out.(*LogFileRecord)

	for k, v := range image {
		switch v.DataType() {
		case events.DataTypeString:
			// Special handling for numeric fields that might be strings
			if isLogRecord && (k == "Size" || k == "LastWritten" || k == "LastBackup") {
				// Try to convert string to int64
				val, err := strconv.ParseInt(v.String(), 10, 64)
				if err == nil {
					item[k] = val
				} else {
					// If conversion fails, use the string value
					item[k] = v.String()
				}
			} else {
				item[k] = v.String()
			}
		case events.DataTypeNumber:
			// For numeric fields, ensure they're parsed as int64
			if isLogRecord && (k == "Size" || k == "LastWritten" || k == "LastBackup") {
				val, err := strconv.ParseInt(v.Number(), 10, 64)
				if err == nil {
					item[k] = val
				} else {
					item[k] = v.Number()
				}
			} else {
				item[k] = v.Number()
			}
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

// downloadLogFile downloads a log file from an Aurora DB instance using binary operations
func downloadLogFile(ctx context.Context, client *rds.Client, dbInstanceID, logFileName string, logger *log.Logger) ([]byte, error) {
	logger.Printf("Downloading log file %s from instance %s using SDK method with pagination\n", logFileName, dbInstanceID)

	// Get log file info first to verify size and other metrics
	logFileInfo, err := getLogFileInfo(ctx, client, dbInstanceID, logFileName, logger)
	if err != nil {
		logger.Printf("Error getting log file info: %v\n", err)
		return nil, fmt.Errorf("failed to get log file info: %w", err)
	}

	var expectedSize int64
	if logFileInfo.Size != nil {
		expectedSize = *logFileInfo.Size
		logger.Printf("Expected log file size: %d bytes\n", expectedSize)
	} else {
		logger.Printf("Expected log file size not available\n")
	}

	// Use binary buffer for content
	var logContent bytes.Buffer
	if expectedSize > 0 {
		logContent.Grow(int(expectedSize)) // Pre-allocate buffer to expected size if possible
	}

	// Start with marker="0" to get from the beginning of the file
	marker := aws.String("0")

	// Track metrics for verification
	portionCount := 0
	totalBytes := 0
	lineCount := 0

	// Use pagination to download the entire log file
	for {
		portionCount++

		// Add timeout for each portion download
		downloadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		resp, err := client.DownloadDBLogFilePortion(downloadCtx, &rds.DownloadDBLogFilePortionInput{
			DBInstanceIdentifier: aws.String(dbInstanceID),
			LogFileName:          aws.String(logFileName),
			Marker:               marker,
			NumberOfLines:        aws.Int32(10000), // Request larger chunks (note: AWS will limit to 1MB per response)
		})

		if err != nil {
			logger.Printf("Error downloading portion %d: %v\n", portionCount, err)
			return nil, fmt.Errorf("failed to download portion %d: %w", portionCount, err)
		}

		// Append the log file portion to the buffer using binary operations
		if resp.LogFileData != nil {
			portionSize := len(*resp.LogFileData)

			// Check for empty portions
			if portionSize == 0 {
				logger.Printf("Warning: Received empty portion %d\n", portionCount)
				// Continue to next portion if this one is empty
				if resp.AdditionalDataPending != nil && *resp.AdditionalDataPending {
					marker = resp.Marker
					continue
				} else {
					break
				}
			}

			// Convert to binary data and write to buffer
			portionData := []byte(*resp.LogFileData)

			// Count lines in this portion for verification
			portionLineCount := countLines(portionData)
			lineCount += portionLineCount

			// Write binary data to buffer
			_, writeErr := logContent.Write(portionData)
			if writeErr != nil {
				logger.Printf("Error writing portion data to buffer: %v\n", writeErr)
				return nil, fmt.Errorf("failed to write portion data: %w", writeErr)
			}

			totalBytes += portionSize
			logger.Printf("Downloaded portion %d: %d bytes, %d lines\n",
				portionCount, portionSize, portionLineCount)

			// Check for potential truncation (1MB limit per portion)
			if portionSize >= 1000000 {
				logger.Printf("Warning: Portion %d size (%d bytes) suggests possible truncation\n",
					portionCount, portionSize)
			}
		}

		// Check if there are more pages
		if resp.AdditionalDataPending == nil || !*resp.AdditionalDataPending {
			logger.Printf("No more data pending after portion %d\n", portionCount)
			break
		}

		// Verify marker is not empty and is changing
		if resp.Marker == nil || *resp.Marker == "" {
			logger.Printf("Error: Received empty marker but AdditionalDataPending is true\n")
			return nil, fmt.Errorf("pagination error: empty marker with more data pending")
		}

		// Use the marker from the response for the next request
		marker = resp.Marker
		logger.Printf("Moving to next portion with marker: %s\n", *marker)
	}

	// Verify downloaded content
	finalContent := logContent.Bytes()
	finalSize := len(finalContent)

	// Log verification metrics
	logger.Printf("Download complete: %d bytes in %d portions, %d lines for log file %s\n",
		finalSize, portionCount, lineCount, logFileName)

	// Check if size matches expected (with some tolerance)
	if expectedSize > 0 && (float64(finalSize) < float64(expectedSize)*0.9) {
		logger.Printf("Warning: Downloaded size (%d bytes) is significantly less than expected size (%d bytes)\n",
			finalSize, expectedSize)
	}

	// Calculate and log MD5 hash for verification
	md5sum := calculateMD5(finalContent)
	logger.Printf("File MD5 checksum: %s\n", md5sum)

	return finalContent, nil
}

// getLogFileInfo retrieves information about a log file
func getLogFileInfo(ctx context.Context, client *rds.Client, dbInstanceID, logFileName string, logger *log.Logger) (*rdstypes.DescribeDBLogFilesDetails, error) {
	resp, err := client.DescribeDBLogFiles(ctx, &rds.DescribeDBLogFilesInput{
		DBInstanceIdentifier: aws.String(dbInstanceID),
		FilenameContains:     aws.String(logFileName),
	})

	if err != nil {
		return nil, err
	}

	for _, logFile := range resp.DescribeDBLogFiles {
		if *logFile.LogFileName == logFileName {
			return &logFile, nil
		}
	}

	return nil, fmt.Errorf("log file not found: %s", logFileName)
}

// countLines counts the number of lines in a byte array
func countLines(data []byte) int {
	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	return count
}

// calculateMD5 calculates the MD5 hash of a byte array
func calculateMD5(data []byte) string {
	hash := md5.Sum(data)
	return fmt.Sprintf("%x", hash)
}

// downloadCompleteLogFile downloads a complete log file using the RDS API directly
func downloadCompleteLogFile(ctx context.Context, client *rds.Client, dbInstanceID, logFileName string, logger *log.Logger) ([]byte, error) {
	logger.Printf("Downloading complete log file %s from instance %s using RDS API directly\n", logFileName, dbInstanceID)

	// Use the DownloadDBLogFilePortion API with NumberOfLines=0 and Marker=0
	// This is equivalent to downloading the complete log file
	logger.Printf("Using DownloadDBLogFilePortion API with NumberOfLines=0 and Marker=0\n")

	// Create the request
	input := &rds.DownloadDBLogFilePortionInput{
		DBInstanceIdentifier: aws.String(dbInstanceID),
		LogFileName:          aws.String(logFileName),
		Marker:               aws.String("0"), // Start from the beginning of the file
		NumberOfLines:        aws.Int32(0),    // 0 means download the entire file
	}

	// Download the log file
	resp, err := client.DownloadDBLogFilePortion(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to download log file: %w", err)
	}

	// Check if we got any data
	if resp.LogFileData == nil {
		return nil, fmt.Errorf("no log file data returned")
	}

	// Convert the log file data to bytes
	content := []byte(*resp.LogFileData)

	logger.Printf("Successfully downloaded complete log file: %d bytes\n", len(content))

	return content, nil
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
