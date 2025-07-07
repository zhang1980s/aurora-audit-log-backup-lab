package repository

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-xray-sdk-go/xray"
	"go.uber.org/zap"
)

// RDSRepository defines the interface for RDS operations
type RDSRepository interface {
	GetDBInstances(ctx context.Context) ([]types.DBInstance, error)
}

// SQSRepository defines the interface for SQS operations
type SQSRepository interface {
	SendMessage(ctx context.Context, queueURL string, messageBody string) error
}

// rdsRepository implements RDSRepository
type rdsRepository struct {
	client *rds.Client
	logger *zap.SugaredLogger
}

// sqsRepository implements SQSRepository
type sqsRepository struct {
	client *sqs.Client
	logger *zap.SugaredLogger
}

// NewRDSRepository creates a new RDS repository
func NewRDSRepository(client *rds.Client, logger *zap.SugaredLogger) RDSRepository {
	return &rdsRepository{
		client: client,
		logger: logger,
	}
}

// NewSQSRepository creates a new SQS repository
func NewSQSRepository(client *sqs.Client, logger *zap.SugaredLogger) SQSRepository {
	return &sqsRepository{
		client: client,
		logger: logger,
	}
}

// GetDBInstances gets all DB instances in the current region
func (r *rdsRepository) GetDBInstances(ctx context.Context) ([]types.DBInstance, error) {
	r.logger.Debug("Getting all DB instances")

	var instances []types.DBInstance
	var marker *string
	var pageCount int

	// Use pagination to get all instances
	for {
		pageCount++
		// Create subsegment for each pagination request
		ctx, apiSubsegment := xray.BeginSubsegment(ctx, "DescribeDBInstances-Page"+string(rune(pageCount)))

		resp, err := r.client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
			Marker: marker,
		})
		if err != nil {
			apiSubsegment.AddError(err)
			apiSubsegment.Close(err)
			return nil, err
		}

		instances = append(instances, resp.DBInstances...)

		// Add metadata about the pagination
		xray.AddMetadata(ctx, "page_instance_count", len(resp.DBInstances))
		apiSubsegment.Close(nil)

		// Check if there are more pages
		if resp.Marker == nil {
			break
		}
		marker = resp.Marker
	}

	r.logger.Debugw("Found DB instances", "count", len(instances))
	// Add annotation for total instance count
	xray.AddAnnotation(ctx, "total_db_instances", len(instances))
	return instances, nil
}

// SendMessage sends a message to an SQS queue
func (r *sqsRepository) SendMessage(ctx context.Context, queueURL string, messageBody string) error {
	r.logger.Debugw("Sending message to SQS", "messageBody", messageBody)

	// Create subsegment for SQS operation
	ctx, sqsOpSubsegment := xray.BeginSubsegment(ctx, "SendMessage-"+messageBody)

	// Add annotation for the message body
	xray.AddAnnotation(ctx, "message_body", messageBody)

	_, err := r.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(messageBody),
	})

	if err != nil {
		sqsOpSubsegment.AddError(err)
		sqsOpSubsegment.Close(err)
		return err
	}

	sqsOpSubsegment.Close(nil)
	return nil
}
