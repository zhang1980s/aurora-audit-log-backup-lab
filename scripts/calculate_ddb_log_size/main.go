package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	timeFormat = "2006-01-02 15:04:05"
)

func main() {
	// Define command-line flags
	var (
		tableName string
		region    string
		verbose   bool
		showHelp  bool
	)

	// Define flags with both short and long forms
	flag.StringVar(&tableName, "table", "", "DynamoDB table name to analyze")
	flag.StringVar(&tableName, "t", "", "DynamoDB table name to analyze (short form)")

	flag.StringVar(&region, "region", "", "AWS region to use (defaults to AWS_REGION env var)")
	flag.StringVar(&region, "r", "", "AWS region to use (short form)")

	flag.BoolVar(&verbose, "verbose", false, "Enable verbose output")
	flag.BoolVar(&verbose, "v", false, "Enable verbose output (short form)")

	flag.BoolVar(&showHelp, "help", false, "Show help information")
	flag.BoolVar(&showHelp, "h", false, "Show help information (short form)")

	// Customize usage output
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "DynamoDB Log Backup Time Calculator\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  Analyze a specific DynamoDB table:\n")
		fmt.Fprintf(os.Stderr, "    %s -t my-table-name\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Analyze a table in a specific region:\n")
		fmt.Fprintf(os.Stderr, "    %s -t my-table-name -r us-west-2\n", os.Args[0])
	}

	flag.Parse()

	// Show help if requested
	if showHelp {
		flag.Usage()
		os.Exit(0)
	}

	// Validate required parameters
	if tableName == "" {
		fmt.Println("Error: DynamoDB table name is required")
		flag.Usage()
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

	// Create DynamoDB client
	ddbClient := dynamodb.NewFromConfig(cfg)

	// Analyze the table
	fmt.Printf("Analyzing DynamoDB table: %s\n", tableName)
	fmt.Println("========================================================")

	// Check if the table exists
	_, err = ddbClient.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: &tableName,
	})
	if err != nil {
		fmt.Printf("Error: Failed to describe table %s: %v\n", tableName, err)
		os.Exit(1)
	}

	// Scan the table to analyze HumanReadableLastBackup attributes
	emptyCount, minTime, maxTime, totalRecords, err := analyzeTable(ctx, ddbClient, tableName, verbose)
	if err != nil {
		fmt.Printf("Error analyzing table: %v\n", err)
		os.Exit(1)
	}

	// Print results
	fmt.Println("\nAnalysis Results:")
	fmt.Println("========================================================")
	fmt.Printf("Total records scanned: %d\n", totalRecords)
	fmt.Printf("Records with empty HumanReadableLastBackup: %d (%.2f%%)\n",
		emptyCount,
		float64(emptyCount)/float64(totalRecords)*100)

	if minTime != nil && maxTime != nil {
		timeDiff := maxTime.Sub(*minTime)

		fmt.Printf("\nTime range analysis:\n")
		fmt.Printf("Earliest time: %s\n", minTime.Format(timeFormat))
		fmt.Printf("Latest time: %s\n", maxTime.Format(timeFormat))
		fmt.Printf("Time difference: %s\n", formatDuration(timeDiff))
	} else {
		fmt.Println("\nUnable to calculate time difference: no valid timestamps found")
	}
}

// analyzeTable scans the DynamoDB table and analyzes the HumanReadableLastBackup attribute
func analyzeTable(ctx context.Context, ddbClient *dynamodb.Client, tableName string, verbose bool) (int, *time.Time, *time.Time, int, error) {
	var (
		emptyCount   int
		totalRecords int
		minTime      *time.Time
		maxTime      *time.Time
		lastKey      map[string]types.AttributeValue
	)

	// Scan the table in pages
	for {
		input := &dynamodb.ScanInput{
			TableName:         &tableName,
			ExclusiveStartKey: lastKey,
		}

		result, err := ddbClient.Scan(ctx, input)
		if err != nil {
			return 0, nil, nil, 0, fmt.Errorf("failed to scan table: %w", err)
		}

		totalRecords += len(result.Items)

		// Process each item
		for _, item := range result.Items {
			// Check if HumanReadableLastBackup attribute exists and is not empty
			if attr, ok := item["HumanReadableLastBackup"]; ok {
				if s, ok := attr.(*types.AttributeValueMemberS); ok {
					if s.Value == "" {
						emptyCount++
						if verbose {
							fmt.Println("Found record with empty HumanReadableLastBackup")
						}
					} else {
						// Parse the time string
						t, err := parseTimeString(s.Value)
						if err != nil {
							if verbose {
								fmt.Printf("Warning: Could not parse time string '%s': %v\n", s.Value, err)
							}
							continue
						}

						// Update min and max times
						if minTime == nil || t.Before(*minTime) {
							minTime = &t
						}
						if maxTime == nil || t.After(*maxTime) {
							maxTime = &t
						}
					}
				} else {
					// Attribute exists but is not a string
					emptyCount++
					if verbose {
						fmt.Println("Found record with non-string HumanReadableLastBackup")
					}
				}
			} else {
				// Attribute doesn't exist
				emptyCount++
				if verbose {
					fmt.Println("Found record without HumanReadableLastBackup attribute")
				}
			}
		}

		// Check if there are more items to scan
		lastKey = result.LastEvaluatedKey
		if len(lastKey) == 0 {
			break
		}

		if verbose {
			fmt.Printf("Scanned %d records so far...\n", totalRecords)
		}
	}

	return emptyCount, minTime, maxTime, totalRecords, nil
}

// parseTimeString attempts to parse a time string in various formats
func parseTimeString(timeStr string) (time.Time, error) {
	// Try common time formats
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.000",
		"2006-01-02T15:04:05.000Z",
	}

	var lastErr error
	for _, format := range formats {
		t, err := time.Parse(format, timeStr)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}

	return time.Time{}, fmt.Errorf("could not parse time string '%s': %w", timeStr, lastErr)
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	parts := []string{}
	if days > 0 {
		if days == 1 {
			parts = append(parts, "1 day")
		} else {
			parts = append(parts, fmt.Sprintf("%d days", days))
		}
	}
	if hours > 0 {
		if hours == 1 {
			parts = append(parts, "1 hour")
		} else {
			parts = append(parts, fmt.Sprintf("%d hours", hours))
		}
	}
	if minutes > 0 {
		if minutes == 1 {
			parts = append(parts, "1 minute")
		} else {
			parts = append(parts, fmt.Sprintf("%d minutes", minutes))
		}
	}
	if seconds > 0 || len(parts) == 0 {
		if seconds == 1 {
			parts = append(parts, "1 second")
		} else {
			parts = append(parts, fmt.Sprintf("%d seconds", seconds))
		}
	}

	return strings.Join(parts, ", ")
}
