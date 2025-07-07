package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	// Maximum number of items in a batch write request
	maxBatchSize = 25
	// Maximum number of concurrent workers
	maxWorkers = 10
)

func main() {
	// Define command-line flags
	var (
		tableName          string
		region             string
		dryRun             bool
		verbose            bool
		showHelp           bool
		confirmation       bool
		batchSize          int
		workers            int
		dbInstanceID       string
		filterByDBInstance bool
	)

	// Define flags with both short and long forms
	flag.StringVar(&tableName, "table", "", "DynamoDB table name to delete records from")
	flag.StringVar(&tableName, "t", "", "DynamoDB table name (short form)")

	flag.StringVar(&dbInstanceID, "db-instance", "", "Filter records by DB instance identifier")
	flag.StringVar(&dbInstanceID, "db", "", "Filter records by DB instance identifier (short form)")

	flag.StringVar(&region, "region", "", "AWS region to use (defaults to AWS_REGION env var)")
	flag.StringVar(&region, "r", "", "AWS region to use (short form)")

	flag.BoolVar(&dryRun, "dry-run", false, "Perform a dry run without deleting any records")
	flag.BoolVar(&dryRun, "d", false, "Dry run (short form)")

	flag.BoolVar(&verbose, "verbose", false, "Enable verbose output")
	flag.BoolVar(&verbose, "v", false, "Enable verbose output (short form)")

	flag.BoolVar(&confirmation, "yes", false, "Skip confirmation prompt")
	flag.BoolVar(&confirmation, "y", false, "Skip confirmation prompt (short form)")

	flag.IntVar(&batchSize, "batch-size", maxBatchSize, "Number of items to delete in each batch (max 25)")
	flag.IntVar(&batchSize, "b", maxBatchSize, "Batch size (short form)")

	flag.IntVar(&workers, "workers", 5, "Number of concurrent delete workers (max 10)")
	flag.IntVar(&workers, "w", 5, "Workers (short form)")

	flag.BoolVar(&showHelp, "help", false, "Show help information")
	flag.BoolVar(&showHelp, "h", false, "Show help information (short form)")

	// Customize usage output
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "DynamoDB Record Deletion Tool\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  Delete all records from a table:\n")
		fmt.Fprintf(os.Stderr, "    %s -t AuroraLogFiles -y\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Perform a dry run without deleting records:\n")
		fmt.Fprintf(os.Stderr, "    %s -t AuroraLogFiles -d\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Delete records with 10 concurrent workers:\n")
		fmt.Fprintf(os.Stderr, "    %s -t AuroraLogFiles -w 10 -y\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Delete records for a specific DB instance only:\n")
		fmt.Fprintf(os.Stderr, "    %s -t AuroraLogFiles -db my-aurora-instance -y\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nNote: This tool is designed to work with the DynamoDB table schema used by the logdetector lambda,\n")
		fmt.Fprintf(os.Stderr, "which has a composite primary key of DBInstanceIdentifier and LogFileName.\n")
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

	// Validate batch size
	if batchSize <= 0 || batchSize > maxBatchSize {
		fmt.Printf("Error: Batch size must be between 1 and %d\n", maxBatchSize)
		os.Exit(1)
	}

	// Validate workers
	if workers <= 0 || workers > maxWorkers {
		fmt.Printf("Error: Number of workers must be between 1 and %d\n", maxWorkers)
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

	// Set filterByDBInstance flag if dbInstanceID is provided
	filterByDBInstance = dbInstanceID != ""
	if filterByDBInstance && verbose {
		fmt.Printf("Filtering records for DB instance: %s\n", dbInstanceID)
	}

	// Create DynamoDB client
	ddbClient := dynamodb.NewFromConfig(cfg)

	// Check if the table exists
	_, err = ddbClient.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: &tableName,
	})
	if err != nil {
		fmt.Printf("Error: Failed to describe table %s: %v\n", tableName, err)
		os.Exit(1)
	}

	// Count the number of items in the table
	itemCount, err := countItems(ctx, ddbClient, tableName, filterByDBInstance, dbInstanceID)
	if err != nil {
		fmt.Printf("Error counting items: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d items in table %s\n", itemCount, tableName)

	if itemCount == 0 {
		fmt.Println("No items to delete.")
		os.Exit(0)
	}

	// Confirm deletion if not in dry run mode and confirmation not provided
	if !dryRun && !confirmation {
		fmt.Printf("WARNING: This will delete all %d items from table %s.\n", itemCount, tableName)
		fmt.Print("Are you sure you want to continue? (y/N): ")

		var response string
		fmt.Scanln(&response)

		if response != "y" && response != "Y" {
			fmt.Println("Operation cancelled.")
			os.Exit(0)
		}
	}

	// Delete all items
	if dryRun {
		fmt.Println("Dry run mode - no items will be deleted.")
	} else {
		if filterByDBInstance {
			fmt.Printf("Deleting %d items for DB instance %s from table %s using %d workers...\n",
				itemCount, dbInstanceID, tableName, workers)
		} else {
			fmt.Printf("Deleting %d items from table %s using %d workers...\n",
				itemCount, tableName, workers)
		}

		startTime := time.Now()
		deletedCount, err := deleteAllItems(ctx, ddbClient, tableName, batchSize, workers, verbose, filterByDBInstance, dbInstanceID)
		if err != nil {
			fmt.Printf("Error deleting items: %v\n", err)
			os.Exit(1)
		}

		duration := time.Since(startTime)
		fmt.Printf("Successfully deleted %d items in %s\n", deletedCount, duration)
	}
}

// countItems counts the number of items in the table
func countItems(ctx context.Context, ddbClient *dynamodb.Client, tableName string, filterByDBInstance bool, dbInstanceID string) (int, error) {
	var count int
	var lastKey map[string]types.AttributeValue

	for {
		input := &dynamodb.ScanInput{
			TableName:         &tableName,
			Select:            types.SelectCount,
			ConsistentRead:    aws.Bool(true),
			ExclusiveStartKey: lastKey,
		}

		// Add filter expression if filtering by DB instance
		if filterByDBInstance {
			input.FilterExpression = aws.String("DBInstanceIdentifier = :dbid")
			input.ExpressionAttributeValues = map[string]types.AttributeValue{
				":dbid": &types.AttributeValueMemberS{Value: dbInstanceID},
			}
		}

		result, err := ddbClient.Scan(ctx, input)
		if err != nil {
			return 0, fmt.Errorf("failed to scan table: %w", err)
		}

		count += int(result.Count)

		// Check if there are more items to scan
		lastKey = result.LastEvaluatedKey
		if len(lastKey) == 0 {
			break
		}
	}

	return count, nil
}

// deleteAllItems deletes all items from the table using multiple workers
func deleteAllItems(ctx context.Context, ddbClient *dynamodb.Client, tableName string, batchSize, workers int, verbose bool, filterByDBInstance bool, dbInstanceID string) (int, error) {
	// Create a channel to send keys to workers
	keysChan := make(chan []map[string]types.AttributeValue)

	// Create a channel for workers to report results
	resultsChan := make(chan int)

	// Create a channel for errors
	errorsChan := make(chan error)

	// Start worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for keys := range keysChan {
				if verbose {
					fmt.Printf("Worker %d processing batch of %d items\n", workerID, len(keys))
				}

				// Create batch write request
				writeRequests := make([]types.WriteRequest, len(keys))
				for i, key := range keys {
					writeRequests[i] = types.WriteRequest{
						DeleteRequest: &types.DeleteRequest{
							Key: key,
						},
					}
				}

				// Execute batch write
				_, err := ddbClient.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
					RequestItems: map[string][]types.WriteRequest{
						tableName: writeRequests,
					},
				})

				if err != nil {
					errorsChan <- fmt.Errorf("worker %d failed to delete batch: %w", workerID, err)
					return
				}

				resultsChan <- len(keys)
			}
		}(i)
	}

	// Start a goroutine to close channels when all workers are done
	go func() {
		wg.Wait()
		close(resultsChan)
		close(errorsChan)
	}()

	// Start a goroutine to scan the table and send keys to workers
	go func() {
		defer close(keysChan)

		var lastKey map[string]types.AttributeValue
		for {
			input := &dynamodb.ScanInput{
				TableName:         &tableName,
				ExclusiveStartKey: lastKey,
			}

			// Add filter expression if filtering by DB instance
			if filterByDBInstance {
				input.FilterExpression = aws.String("DBInstanceIdentifier = :dbid")
				input.ExpressionAttributeValues = map[string]types.AttributeValue{
					":dbid": &types.AttributeValueMemberS{Value: dbInstanceID},
				}
			}

			result, err := ddbClient.Scan(ctx, input)
			if err != nil {
				errorsChan <- fmt.Errorf("failed to scan table: %w", err)
				return
			}

			// Extract keys from items
			var batch []map[string]types.AttributeValue
			for _, item := range result.Items {
				// Extract the key attributes (DBInstanceIdentifier and LogFileName)
				key := extractKeyAttributes(item)

				// Verify that we have both key attributes
				if _, hasDBID := key["DBInstanceIdentifier"]; !hasDBID {
					if verbose {
						fmt.Println("Warning: Item missing DBInstanceIdentifier, skipping")
					}
					continue
				}

				if _, hasLogFileName := key["LogFileName"]; !hasLogFileName {
					if verbose {
						fmt.Println("Warning: Item missing LogFileName, skipping")
					}
					continue
				}

				batch = append(batch, key)

				// Send batch when it reaches the batch size
				if len(batch) >= batchSize {
					keysChan <- batch
					batch = nil
				}
			}

			// Send any remaining items
			if len(batch) > 0 {
				keysChan <- batch
			}

			// Check if there are more items to scan
			lastKey = result.LastEvaluatedKey
			if len(lastKey) == 0 {
				break
			}
		}
	}()

	// Process results and errors
	var totalDeleted int
	var lastError error

	for {
		select {
		case count, ok := <-resultsChan:
			if !ok {
				// Channel closed, no more results
				resultsChan = nil
			} else {
				totalDeleted += count
				if verbose {
					fmt.Printf("Progress: %d items deleted\n", totalDeleted)
				}
			}

		case err, ok := <-errorsChan:
			if !ok {
				// Channel closed, no more errors
				errorsChan = nil
			} else {
				lastError = err
				fmt.Printf("Error: %v\n", err)
			}
		}

		// Exit when both channels are closed
		if resultsChan == nil && errorsChan == nil {
			break
		}
	}

	if lastError != nil {
		return totalDeleted, lastError
	}

	return totalDeleted, nil
}

// extractKeyAttributes extracts the key attributes from an item
// Based on the logdetector lambda's DynamoDB schema, the primary key consists of:
// - DBInstanceIdentifier (partition key)
// - LogFileName (sort key)
func extractKeyAttributes(item map[string]types.AttributeValue) map[string]types.AttributeValue {
	key := make(map[string]types.AttributeValue)

	// Extract DBInstanceIdentifier (partition key)
	if dbID, ok := item["DBInstanceIdentifier"]; ok {
		key["DBInstanceIdentifier"] = dbID
	}

	// Extract LogFileName (sort key)
	if logFileName, ok := item["LogFileName"]; ok {
		key["LogFileName"] = logFileName
	}

	return key
}

// aws is a helper package to provide AWS SDK utilities
var aws = struct {
	Bool   func(b bool) *bool
	String func(s string) *string
}{
	Bool:   func(b bool) *bool { return &b },
	String: func(s string) *string { return &s },
}
