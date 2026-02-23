package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-xray-sdk-go/xray"
	"go.uber.org/zap"

	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdownloader/internal/config"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdownloader/internal/repository"
	"github.com/zhang1980s/aurora-audit-log-backup-lab/lambdas/logdownloader/pkg/errors"
)

// Service defines the interface for business logic operations
type Service interface {
	ProcessLogFile(ctx context.Context, dbInstanceID, logFileName string, oldImage, newImage map[string]interface{}) error
	DownloadLogFile(ctx context.Context, dbInstanceID, logFileName string) ([]byte, string, error)
	ShouldDownload(ctx context.Context, oldImage, newImage map[string]interface{}) (bool, error)
}

// LogDownloaderService implements the Service interface
type LogDownloaderService struct {
	repo   repository.Repository
	cfg    *config.Config
	logger *zap.SugaredLogger
	awsCfg aws.Config
}

// NewService creates a new service instance
func NewService(
	repo repository.Repository,
	cfg *config.Config,
	awsCfg aws.Config,
	logger *zap.SugaredLogger,
) Service {
	return &LogDownloaderService{
		repo:   repo,
		cfg:    cfg,
		logger: logger,
		awsCfg: awsCfg,
	}
}

// ProcessLogFile processes a log file record from DynamoDB
func (s *LogDownloaderService) ProcessLogFile(
	ctx context.Context,
	dbInstanceID string,
	logFileName string,
	oldImage map[string]interface{},
	newImage map[string]interface{},
) error {
	ctx, subsegment := xray.BeginSubsegment(ctx, "service.ProcessLogFile")
	defer subsegment.Close(nil)

	// Add annotations for the log file record
	xray.AddAnnotation(ctx, "db_instance_id", dbInstanceID)
	xray.AddAnnotation(ctx, "log_file_name", logFileName)
	xray.AddMetadata(ctx, "log_file_record", newImage)

	// Check if we should download this log file
	shouldDownload, err := s.ShouldDownload(ctx, oldImage, newImage)
	if err != nil {
		subsegment.AddError(err)
		return err
	}

	if !shouldDownload {
		s.logger.Debugw("Skipping download for log file, no significant changes",
			"logFile", logFileName)
		xray.AddAnnotation(ctx, "action", "skip")
		return nil
	}

	xray.AddAnnotation(ctx, "action", "download")

	// Download the log file
	logContent, sha256sum, err := s.DownloadLogFile(ctx, dbInstanceID, logFileName)
	if err != nil {
		s.logger.Errorw("Error downloading log file",
			"logFile", logFileName,
			"error", err)
		subsegment.AddError(err)
		return err
	}

	// Upload file to S3
	ctx, uploadSubsegment := xray.BeginSubsegment(ctx, "UploadToS3")
	s3Key := fmt.Sprintf("%s/%s/%s", s.cfg.S3Prefix, dbInstanceID, logFileName)
	err = s.repo.UploadToS3(ctx, s3Key, logContent, sha256sum)
	if err != nil {
		s.logger.Errorw("Error uploading log file to S3",
			"logFile", logFileName,
			"error", err)
		uploadSubsegment.AddError(err)
		uploadSubsegment.Close(err)
		subsegment.AddError(err)
		return err
	}
	xray.AddMetadata(ctx, "s3_key", s3Key)
	uploadSubsegment.Close(nil)

	// Update LastBackup timestamp in DynamoDB
	ctx, updateSubsegment := xray.BeginSubsegment(ctx, "UpdateLastBackup")
	err = s.repo.UpdateLastBackup(ctx, dbInstanceID, logFileName, sha256sum)
	if err != nil {
		s.logger.Errorw("Error updating LastBackup timestamp",
			"logFile", logFileName,
			"error", err)
		updateSubsegment.AddError(err)
		updateSubsegment.Close(err)
		subsegment.AddError(err)
		return err
	}
	updateSubsegment.Close(nil)

	s.logger.Debugw("Successfully processed log file",
		"logFile", logFileName,
		"instance", dbInstanceID)

	return nil
}

// DownloadLogFile downloads a complete log file using the REST endpoint approach
func (s *LogDownloaderService) DownloadLogFile(ctx context.Context, dbInstanceID, logFileName string) ([]byte, string, error) {
	ctx, subsegment := xray.BeginSubsegment(ctx, "service.DownloadLogFile")
	defer subsegment.Close(nil)

	s.logger.Debugw("Downloading complete log file using REST endpoint",
		"logFile", logFileName,
		"instanceID", dbInstanceID)

	// Add X-Ray annotations for the download operation
	xray.AddAnnotation(ctx, "db_instance_id", dbInstanceID)
	xray.AddAnnotation(ctx, "log_file_name", logFileName)
	xray.AddAnnotation(ctx, "download_method", "rest_endpoint")

	region := s.awsCfg.Region
	service := "rds"
	xray.AddMetadata(ctx, "aws_region", region)
	xray.AddMetadata(ctx, "aws_service", service)

	// Create subsegment for credential retrieval
	ctx, credSubsegment := xray.BeginSubsegment(ctx, "RetrieveCredentials")

	// Get credentials
	credentials, err := s.awsCfg.Credentials.Retrieve(ctx)
	if err != nil {
		credSubsegment.AddError(err)
		credSubsegment.Close(err)
		subsegment.AddError(err)
		return nil, "", errors.Wrap(errors.ErrDependency, "failed to retrieve credentials")
	}
	credSubsegment.Close(nil)

	// Format host and canonical URI based on endpoint type
	var host string
	if s.cfg.RDSEndpointType == "private" {
		host = s.cfg.RDSVpcEndpointURL
		s.logger.Debugw("Using private VPC endpoint for RDS API", "vpcEndpoint", host)
	} else {
		host = fmt.Sprintf("rds.%s.amazonaws.com", region)
	}
	xray.AddMetadata(ctx, "rds_endpoint_type", s.cfg.RDSEndpointType)
	xray.AddMetadata(ctx, "rds_host", host)
	canonicalURI := fmt.Sprintf("/v13/downloadCompleteLogFile/%s/%s", dbInstanceID, logFileName)

	// Create timestamp
	t := time.Now().UTC()
	amzDate := t.Format("20060102T150405Z")
	datestamp := t.Format("20060102")

	// Create credential scope
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", datestamp, region, service)

	// Create canonical headers
	canonicalHeaders := fmt.Sprintf("host:%s\n", host)
	signedHeaders := "host"

	// Create hashed payload (SHA256 hash of empty string)
	hashedPayload := fmt.Sprintf("%x", sha256.Sum256([]byte("")))

	// Create canonical query string with AWS SigV4 parameters
	algorithm := "AWS4-HMAC-SHA256"
	queryParams := map[string]string{
		"X-Amz-Algorithm":     algorithm,
		"X-Amz-Credential":    url.QueryEscape(credentials.AccessKeyID + "/" + credentialScope),
		"X-Amz-Date":          amzDate,
		"X-Amz-Expires":       "30",
		"X-Amz-SignedHeaders": signedHeaders,
	}

	// Add security token if present
	if credentials.SessionToken != "" {
		queryParams["X-Amz-Security-Token"] = url.QueryEscape(credentials.SessionToken)
	}

	// Sort query parameters
	var queryKeys []string
	for k := range queryParams {
		queryKeys = append(queryKeys, k)
	}
	sort.Strings(queryKeys)

	// Build canonical query string
	var canonicalQueryParts []string
	for _, k := range queryKeys {
		canonicalQueryParts = append(canonicalQueryParts, k+"="+queryParams[k])
	}
	canonicalQueryString := strings.Join(canonicalQueryParts, "&")

	// Create canonical request
	canonicalRequest := strings.Join([]string{
		"GET",
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		hashedPayload,
	}, "\n")

	s.logger.Debugw("Canonical request", "request", canonicalRequest)

	// Create string to sign
	stringToSign := strings.Join([]string{
		algorithm,
		amzDate,
		credentialScope,
		fmt.Sprintf("%x", sha256.Sum256([]byte(canonicalRequest))),
	}, "\n")

	s.logger.Debugw("String to sign", "stringToSign", stringToSign)

	// Calculate signature
	// Step 1: Create date key
	dateKey := s.signMessage([]byte("AWS4"+credentials.SecretAccessKey), datestamp)
	// Step 2: Create region key
	regionKey := s.signMessage(dateKey, region)
	// Step 3: Create service key
	serviceKey := s.signMessage(regionKey, service)
	// Step 4: Create signing key
	signingKey := s.signMessage(serviceKey, "aws4_request")
	// Step 5: Calculate signature
	signature := hex.EncodeToString(s.signMessage(signingKey, stringToSign))

	s.logger.Debugw("Signature", "signature", signature)

	// Add signature to query string
	canonicalQueryString += "&X-Amz-Signature=" + signature

	// Construct final URL
	endpoint := "https://" + host
	finalURL := endpoint + canonicalURI + "?" + canonicalQueryString

	s.logger.Debugw("Final URL", "url", finalURL)

	// Create subsegment for HTTP request
	ctx, httpSubsegment := xray.BeginSubsegment(ctx, "HTTPRequest")

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", finalURL, nil)
	if err != nil {
		httpSubsegment.AddError(err)
		httpSubsegment.Close(err)
		subsegment.AddError(err)
		return nil, "", errors.Wrap(errors.ErrDownload, "failed to create HTTP request")
	}

	// Add X-Ray context to the request
	xray.AddMetadata(ctx, "http_url", finalURL)
	xray.AddMetadata(ctx, "http_method", "GET")

	// Create an HTTP client
	httpClient := &http.Client{
		Timeout: s.cfg.DownloadTimeout, // Use configured timeout
	}

	// Record request start time for metrics
	requestStart := time.Now()

	// Send the request
	s.logger.Debug("Sending HTTP request to download complete log file")
	resp, err := httpClient.Do(req)
	if err != nil {
		httpSubsegment.AddError(err)
		httpSubsegment.Close(err)
		subsegment.AddError(err)
		return nil, "", errors.Wrap(errors.ErrDownload, fmt.Sprintf("failed to send HTTP request: %v", err))
	}
	defer resp.Body.Close()

	// Record request duration
	requestDuration := time.Since(requestStart)
	xray.AddMetadata(ctx, "http_request_duration_ms", requestDuration.Milliseconds())

	// Add response metadata
	xray.AddMetadata(ctx, "http_status_code", resp.StatusCode)
	xray.AddMetadata(ctx, "http_status", resp.Status)
	xray.AddMetadata(ctx, "http_content_length", resp.ContentLength)

	// Check response status
	if resp.StatusCode != http.StatusOK {
		// Read error response body for more details
		errorBody, _ := io.ReadAll(resp.Body)
		errorMsg := fmt.Sprintf("REST endpoint returned non-OK status: %s, body: %s", resp.Status, string(errorBody))
		err := errors.Wrap(errors.ErrDownload, errorMsg)
		xray.AddError(ctx, err)
		httpSubsegment.AddError(err)
		httpSubsegment.Close(err)
		subsegment.AddError(err)
		return nil, "", err
	}

	// Create subsegment for reading response body
	ctx, readBodySubsegment := xray.BeginSubsegment(ctx, "ReadResponseBody")

	// Read the response body
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		readBodySubsegment.AddError(err)
		readBodySubsegment.Close(err)
		httpSubsegment.AddError(err)
		httpSubsegment.Close(err)
		subsegment.AddError(err)
		return nil, "", errors.Wrap(errors.ErrDownload, fmt.Sprintf("failed to read response body: %v", err))
	}

	// Add content metadata
	contentSize := len(content)
	xray.AddMetadata(ctx, "content_size_bytes", contentSize)

	// Calculate SHA256 for verification
	contentSHA256 := s.calculateSHA256(content)
	xray.AddMetadata(ctx, "content_sha256", contentSHA256)

	s.logger.Debugw("Successfully downloaded complete log file using REST endpoint",
		"bytes", contentSize,
		"sha256", contentSHA256)

	readBodySubsegment.Close(nil)
	httpSubsegment.Close(nil)

	return content, contentSHA256, nil
}

// ShouldDownload determines if a log file should be downloaded based on changes
func (s *LogDownloaderService) ShouldDownload(ctx context.Context, oldImage, newImage map[string]interface{}) (bool, error) {
	ctx, subsegment := xray.BeginSubsegment(ctx, "service.ShouldDownload")
	defer subsegment.Close(nil)

	// Record start time for performance metrics
	startTime := time.Now()

	// Extract log file name for logging context
	var logFileName string
	if name, ok := newImage["LogFileName"]; ok {
		logFileName = name.(string)
		xray.AddAnnotation(ctx, "log_file_name", logFileName)
	}

	// If oldImage is nil, this is a new record, so download
	if oldImage == nil {
		s.logger.Debugw("New log file record, will download",
			"logFile", logFileName)

		// Record duration before returning
		duration := time.Since(startTime)
		xray.AddMetadata(ctx, "decision_duration_ms", duration.Milliseconds())
		return true, nil
	}

	// Check if Size has changed
	if oldSize, ok := oldImage["Size"]; ok {
		if newSize, ok := newImage["Size"]; ok {
			if oldSize != newSize {
				s.logger.Debugw("Size changed, will download log file",
					"logFile", logFileName,
					"oldSize", oldSize,
					"newSize", newSize)

				// Record duration before returning
				duration := time.Since(startTime)
				xray.AddMetadata(ctx, "decision_duration_ms", duration.Milliseconds())
				return true, nil
			}
		}
	}

	// Check if LastWritten has changed
	if oldLastWritten, ok := oldImage["LastWritten"]; ok {
		if newLastWritten, ok := newImage["LastWritten"]; ok {
			if oldLastWritten != newLastWritten {
				s.logger.Debugw("LastWritten changed, will download log file",
					"logFile", logFileName,
					"oldLastWritten", oldLastWritten,
					"newLastWritten", newLastWritten)

				// Record duration before returning
				duration := time.Since(startTime)
				xray.AddMetadata(ctx, "decision_duration_ms", duration.Milliseconds())
				return true, nil
			}
		}
	}

	// Check if LastBackup exists
	lastBackup, exists := newImage["LastBackup"]
	if !exists {
		s.logger.Debugw("LastBackup doesn't exist, will download log file",
			"logFile", logFileName)

		// Record duration before returning
		duration := time.Since(startTime)
		xray.AddMetadata(ctx, "decision_duration_ms", duration.Milliseconds())
		return true, nil
	}

	// Parse LastBackup timestamp
	var lastBackupVal int64
	switch v := lastBackup.(type) {
	case int64:
		lastBackupVal = v
	case float64:
		lastBackupVal = int64(v)
	case string:
		var err error
		lastBackupVal, err = parseInt64(v)
		if err != nil {
			s.logger.Errorw("Error parsing LastBackup",
				"error", err,
				"logFile", logFileName,
				"lastBackup", v)

			// Record duration before returning
			duration := time.Since(startTime)
			xray.AddMetadata(ctx, "decision_duration_ms", duration.Milliseconds())
			return true, nil
		}
	default:
		s.logger.Errorw("Unexpected LastBackup type",
			"logFile", logFileName,
			"type", fmt.Sprintf("%T", lastBackup))

		// Record duration before returning
		duration := time.Since(startTime)
		xray.AddMetadata(ctx, "decision_duration_ms", duration.Milliseconds())
		return true, nil
	}

	// Check if LastBackup is older than 24 hours (in milliseconds)
	twentyFourHoursAgoMillis := time.Now().UnixMilli() - (24 * 60 * 60 * 1000)
	if lastBackupVal < twentyFourHoursAgoMillis {
		// Get human-readable format for logging
		lastBackupTime := time.UnixMilli(lastBackupVal).Format(time.RFC3339)
		thresholdTime := time.UnixMilli(twentyFourHoursAgoMillis).Format(time.RFC3339)

		s.logger.Debugw("LastBackup is older than 24 hours, will download log file",
			"logFile", logFileName,
			"lastBackupMillis", lastBackupVal,
			"lastBackupTime", lastBackupTime,
			"thresholdMillis", twentyFourHoursAgoMillis,
			"thresholdTime", thresholdTime)

		// Record duration before returning
		duration := time.Since(startTime)
		xray.AddMetadata(ctx, "decision_duration_ms", duration.Milliseconds())
		return true, nil
	}

	// No need to download
	s.logger.Debugw("No significant changes, skipping download",
		"logFile", logFileName)

	// Record duration and add as metadata
	duration := time.Since(startTime)
	xray.AddMetadata(ctx, "decision_duration_ms", duration.Milliseconds())

	return false, nil
}

// calculateSHA256 calculates the SHA256 hash of a byte array
func (s *LogDownloaderService) calculateSHA256(data []byte) string {
	// Create context for X-Ray
	ctx := context.Background()

	// Add metadata about the data being hashed
	dataSize := len(data)
	xray.AddMetadata(ctx, "sha256_data_size_bytes", dataSize)

	// Record start time for performance metrics
	startTime := time.Now()

	// Calculate SHA256 hash
	hash := sha256.Sum256(data)
	hashString := fmt.Sprintf("%x", hash)

	// Record duration
	duration := time.Since(startTime)
	xray.AddMetadata(ctx, "sha256_calculation_duration_ms", duration.Milliseconds())

	return hashString
}

// signMessage creates an HMAC-SHA256 signature of a message using a key
func (s *LogDownloaderService) signMessage(key []byte, msg string) []byte {
	// Create context for X-Ray
	ctx := context.Background()

	// Add metadata about the signing operation (without exposing sensitive data)
	xray.AddMetadata(ctx, "message_length", len(msg))

	// Record start time for performance metrics
	startTime := time.Now()

	// Create HMAC-SHA256 signature
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	signature := h.Sum(nil)

	// Record duration
	duration := time.Since(startTime)
	xray.AddMetadata(ctx, "signing_duration_ms", duration.Milliseconds())

	return signature
}

// parseInt64 parses a string to int64
func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
