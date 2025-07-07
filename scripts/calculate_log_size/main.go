package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"
)

// LogType represents the type of log to process
type LogType string

const (
	LogTypeAudit    LogType = "audit"
	LogTypeError    LogType = "error"
	LogTypeInstance LogType = "instance"
	LogTypeAll      LogType = "all"
)

// HourlyTotal represents the total log size for a specific hour
type HourlyTotal struct {
	Date      string
	Hour      string
	TotalSize int64
	FileCount int // Number of log files in this hour
}

// DisplayKey returns a formatted display key for the hourly total
func (ht HourlyTotal) DisplayKey() string {
	return fmt.Sprintf("%s %s:00", ht.Date, ht.Hour)
}

// FormatSize returns a human-readable size string
func FormatSize(size int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	if size >= GB {
		return fmt.Sprintf("%.2f GB", float64(size)/float64(GB))
	} else if size >= MB {
		return fmt.Sprintf("%.2f MB", float64(size)/float64(MB))
	} else if size >= KB {
		return fmt.Sprintf("%.2f KB", float64(size)/float64(KB))
	}
	return fmt.Sprintf("%d bytes", size)
}

// ShouldProcessLogFile checks if a log file should be processed based on the log type
func ShouldProcessLogFile(logFileName string, logType LogType) bool {
	if logType == LogTypeAll {
		return true
	}

	logFileName = strings.ToLower(logFileName)
	switch logType {
	case LogTypeAudit:
		return strings.HasPrefix(logFileName, "audit") || strings.HasPrefix(logFileName, "audit/")
	case LogTypeError:
		return strings.HasPrefix(logFileName, "error") || strings.HasPrefix(logFileName, "error/")
	case LogTypeInstance:
		return strings.HasPrefix(logFileName, "instance") || strings.HasPrefix(logFileName, "instance/")
	default:
		return false
	}
}

// ProcessDBInstance processes a single DB instance and returns hourly totals
func ProcessDBInstance(ctx context.Context, rdsSvc *rds.Client, dbInstanceID string, logType LogType) (map[string]HourlyTotal, error) {
	fmt.Println("========================================================")
	fmt.Printf("Processing DB Instance: %s (Log Type: %s)\n", dbInstanceID, logType)
	fmt.Println("========================================================")

	// Fetch log files information
	fmt.Println("Fetching log files information...")
	logFilesOutput, err := rdsSvc.DescribeDBLogFiles(ctx, &rds.DescribeDBLogFilesInput{
		DBInstanceIdentifier: &dbInstanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe DB log files: %w", err)
	}

	if len(logFilesOutput.DescribeDBLogFiles) == 0 {
		fmt.Printf("No log files found for DB instance: %s\n\n", dbInstanceID)
		return nil, nil
	}

	// Show sample of the data
	fmt.Println("Sample of raw data:")
	sampleCount := 0
	for _, logFile := range logFilesOutput.DescribeDBLogFiles {
		if ShouldProcessLogFile(*logFile.LogFileName, logType) {
			fmt.Printf("%s\t%d\t%s\t%d\n", "DESCRIBEDBLOGFILES", *logFile.LastWritten, *logFile.LogFileName, *logFile.Size)
			sampleCount++
			if sampleCount >= 3 {
				break
			}
		}
	}

	fmt.Println("Calculating log sizes by hour...")

	// Extract hour information
	hourlyTotals := make(map[string]HourlyTotal)
	dateHourRegex := regexp.MustCompile(`(\d{4}-\d{2}-\d{2})-(\d{2})`)
	processedCount := 0
	unmatchedCount := 0

	for _, logFile := range logFilesOutput.DescribeDBLogFiles {
		// Skip files that don't match the requested log type
		if !ShouldProcessLogFile(*logFile.LogFileName, logType) {
			continue
		}

		matches := dateHourRegex.FindStringSubmatch(*logFile.LogFileName)
		if len(matches) == 3 {
			date := matches[1]
			hour := matches[2]
			hourKey := fmt.Sprintf("%s_%s", date, hour)

			total, exists := hourlyTotals[hourKey]
			if !exists {
				total = HourlyTotal{
					Date: date,
					Hour: hour,
				}
			}
			total.TotalSize += *logFile.Size
			total.FileCount++ // Increment file count
			hourlyTotals[hourKey] = total
			processedCount++
		} else {
			fmt.Printf("Unmatched filename: %s\n", *logFile.LogFileName)
			unmatchedCount++
		}
	}

	// Display hourly breakdown
	fmt.Printf("\nHourly breakdown for DB Instance: %s\n", dbInstanceID)
	fmt.Printf("Log Type: %s, Processed: %d, Unmatched: %d\n", logType, processedCount, unmatchedCount)
	fmt.Println("Date Hour, Files, Size (bytes), Size (human readable)")
	fmt.Println("--------------------------------------------------")

	if len(hourlyTotals) == 0 {
		fmt.Println("No log files matched the date/hour patterns!")
	} else {
		// Sort hour keys for consistent output
		var hourKeys []string
		for hourKey := range hourlyTotals {
			hourKeys = append(hourKeys, hourKey)
		}
		sort.Strings(hourKeys)

		for _, hourKey := range hourKeys {
			total := hourlyTotals[hourKey]
			fmt.Printf("%-20s %6d %12d %20s\n", total.DisplayKey(), total.FileCount, total.TotalSize, FormatSize(total.TotalSize))
		}
	}

	fmt.Println()
	return hourlyTotals, nil
}

// GetAllDBInstances gets a list of all DB instances
func GetAllDBInstances(ctx context.Context, rdsSvc *rds.Client) ([]string, error) {
	fmt.Println("Fetching all DB instances...")

	var dbInstanceIDs []string
	var marker *string

	for {
		output, err := rdsSvc.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
			Marker: marker,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to describe DB instances: %w", err)
		}

		for _, instance := range output.DBInstances {
			dbInstanceIDs = append(dbInstanceIDs, *instance.DBInstanceIdentifier)
		}

		if output.Marker == nil {
			break
		}
		marker = output.Marker
	}

	return dbInstanceIDs, nil
}

// DisplayCombinedTotals displays the combined totals across all DB instances
func DisplayCombinedTotals(allHourlyTotals []map[string]HourlyTotal) {
	fmt.Println("========================================================")
	fmt.Println("COMBINED TOTALS ACROSS ALL DB INSTANCES")
	fmt.Println("========================================================")
	fmt.Println("Date Hour, Files, Size (bytes), Size (human readable)")
	fmt.Println("--------------------------------------------------")

	// Combine totals from all instances
	combinedTotals := make(map[string]HourlyTotal)
	for _, instanceTotals := range allHourlyTotals {
		for hourKey, hourTotal := range instanceTotals {
			combined, exists := combinedTotals[hourKey]
			if !exists {
				combined = HourlyTotal{
					Date: hourTotal.Date,
					Hour: hourTotal.Hour,
				}
			}
			combined.TotalSize += hourTotal.TotalSize
			combined.FileCount += hourTotal.FileCount // Add file counts
			combinedTotals[hourKey] = combined
		}
	}

	if len(combinedTotals) == 0 {
		fmt.Println("No data available for combined totals.")
		return
	}

	// Sort hour keys for consistent output
	var hourKeys []string
	for hourKey := range combinedTotals {
		hourKeys = append(hourKeys, hourKey)
	}
	sort.Strings(hourKeys)

	for _, hourKey := range hourKeys {
		total := combinedTotals[hourKey]
		fmt.Printf("%-20s %6d %12d %20s\n", total.DisplayKey(), total.FileCount, total.TotalSize, FormatSize(total.TotalSize))
	}
	fmt.Println()
}

func main() {
	// Define command-line flags
	var (
		logTypeStr   string
		dbInstanceID string
		allInstances bool
		showHelp     bool
		region       string
		verbose      bool
	)

	// Define flags with both short and long forms
	flag.StringVar(&logTypeStr, "type", "audit", "Log type to process (audit, error, instance, all)")
	flag.StringVar(&logTypeStr, "t", "audit", "Log type to process (short form)")

	flag.StringVar(&dbInstanceID, "instance", "", "DB instance ID to process (comma-separated for multiple)")
	flag.StringVar(&dbInstanceID, "i", "", "DB instance ID to process (short form)")

	flag.BoolVar(&allInstances, "all", false, "Process all DB instances")
	flag.BoolVar(&allInstances, "a", false, "Process all DB instances (short form)")

	flag.StringVar(&region, "region", "", "AWS region to use (defaults to AWS_REGION env var)")
	flag.StringVar(&region, "r", "", "AWS region to use (short form)")

	flag.BoolVar(&verbose, "verbose", false, "Enable verbose output")
	flag.BoolVar(&verbose, "v", false, "Enable verbose output (short form)")

	flag.BoolVar(&showHelp, "help", false, "Show help information")
	flag.BoolVar(&showHelp, "h", false, "Show help information (short form)")

	// Customize usage output
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Aurora Log Size Calculator\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  Calculate audit log sizes for a specific instance:\n")
		fmt.Fprintf(os.Stderr, "    %s -t audit -i my-db-instance\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Calculate all log types for all instances:\n")
		fmt.Fprintf(os.Stderr, "    %s -t all -a\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Calculate error logs for multiple instances:\n")
		fmt.Fprintf(os.Stderr, "    %s -t error -i instance1,instance2,instance3\n", os.Args[0])
	}

	flag.Parse()

	// Show help if requested
	if showHelp {
		flag.Usage()
		os.Exit(0)
	}

	// Validate log type
	logType := LogType(strings.ToLower(logTypeStr))
	switch logType {
	case LogTypeAudit, LogTypeError, LogTypeInstance, LogTypeAll:
		// Valid log type
	default:
		fmt.Printf("Invalid log type: %s. Valid types are: audit, error, instance, all\n", logTypeStr)
		os.Exit(1)
	}

	// Load AWS configuration
	ctx := context.Background()

	// Configure AWS SDK options
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		fmt.Printf("Failed to load AWS config: %v\n", err)
		os.Exit(1)
	}

	if verbose {
		fmt.Printf("Using AWS region: %s\n", cfg.Region)
	}

	// Create RDS client
	rdsSvc := rds.NewFromConfig(cfg)

	// Determine which DB instances to process
	var dbInstanceIDs []string

	if allInstances {
		// Get all DB instances
		dbInstanceIDs, err = GetAllDBInstances(ctx, rdsSvc)
		if err != nil {
			fmt.Printf("Error fetching DB instances: %v\n", err)
			os.Exit(1)
		}

		if len(dbInstanceIDs) == 0 {
			fmt.Println("No DB instances found.")
			os.Exit(0)
		}
	} else if dbInstanceID != "" {
		// Process specified DB instances
		dbInstanceIDs = strings.Split(dbInstanceID, ",")
	} else {
		// No instances specified and not all instances, show usage
		fmt.Println("No DB instances specified. Use -instance=<id> or -all")
		flag.Usage()
		os.Exit(1)
	}

	fmt.Printf("Processing %d DB instance(s) with log type: %s\n\n", len(dbInstanceIDs), logType)

	if verbose {
		fmt.Println("DB Instances to process:")
		for i, id := range dbInstanceIDs {
			fmt.Printf("  %d. %s\n", i+1, id)
		}
		fmt.Println()
	}

	// Process each DB instance
	var allHourlyTotals []map[string]HourlyTotal
	for _, id := range dbInstanceIDs {
		hourlyTotals, err := ProcessDBInstance(ctx, rdsSvc, id, logType)
		if err != nil {
			fmt.Printf("Error processing DB instance %s: %v\n", id, err)
			continue
		}
		if hourlyTotals != nil {
			allHourlyTotals = append(allHourlyTotals, hourlyTotals)
		}
	}

	// Display combined totals if we processed multiple instances
	if len(dbInstanceIDs) > 1 {
		DisplayCombinedTotals(allHourlyTotals)
	}

	fmt.Println("All DB instances processed!")
}
