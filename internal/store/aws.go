package store

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func FromEnv(ctx context.Context) (*Store, aws.Config, error) {
	table, bucket := os.Getenv("TABLE_NAME"), os.Getenv("CONTENT_BUCKET")
	if table == "" {
		return nil, aws.Config{}, fmt.Errorf("TABLE_NAME is required")
	}
	config, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, aws.Config{}, err
	}
	return New(dynamodb.NewFromConfig(config), s3.NewFromConfig(config), table, bucket, os.Getenv("CONTENT_BASE_URL")), config, nil
}
