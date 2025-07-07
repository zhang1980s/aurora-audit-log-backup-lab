package models

import (
	"time"
)

// LogFileRecord represents a record in the DynamoDB table
type LogFileRecord struct {
	DBInstanceIdentifier     string `dynamodbav:"DBInstanceIdentifier"`
	LogFileName              string `dynamodbav:"LogFileName"`
	Size                     int64  `dynamodbav:"Size"`
	LastWritten              int64  `dynamodbav:"LastWritten"` // Milliseconds since epoch (from RDS API)
	HumanReadableLastWritten string `dynamodbav:"HumanReadableLastWritten,omitempty"`
	LastBackup               int64  `dynamodbav:"LastBackup,omitempty"` // Milliseconds since epoch
	HumanReadableLastBackup  string `dynamodbav:"HumanReadableLastBackup,omitempty"`
	SHA256Checksum           string `dynamodbav:"SHA256Checksum,omitempty"` // SHA256 checksum of the log file
}

// FormatTime converts a Unix millisecond timestamp to a human-readable format
func FormatTime(milliseconds int64) string {
	return time.UnixMilli(milliseconds).Format(time.RFC3339)
}

// CurrentTimeMillis returns the current time in milliseconds since epoch
func CurrentTimeMillis() int64 {
	return time.Now().UnixMilli()
}

// CurrentTimeFormatted returns the current time in RFC3339 format
func CurrentTimeFormatted() string {
	return time.Now().Format(time.RFC3339)
}
