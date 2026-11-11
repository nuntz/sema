// Package dynamotest creates throwaway Sema tables on DynamoDB Local for tests.
// Tests that use it skip unless DYNAMODB_ENDPOINT is set; `make check` sets it
// (scripts/with-dynamodb-local.sh) together with SEMA_REQUIRE_DYNAMODB, which
// turns that skip into a failure.
package dynamotest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// NewTable creates a table with Sema's key schema and indexes, drops it when
// the test finishes, and returns a client for it. It skips the test unless
// DYNAMODB_ENDPOINT is set, or fails it when SEMA_REQUIRE_DYNAMODB is set.
// Every worktree shares one DynamoDB Local namespace, so the name carries a
// random suffix as well as the time.
func NewTable(t testing.TB) (*dynamodb.Client, string) {
	t.Helper()
	endpoint := os.Getenv("DYNAMODB_ENDPOINT")
	if endpoint == "" {
		if os.Getenv("SEMA_REQUIRE_DYNAMODB") != "" {
			t.Fatal("DYNAMODB_ENDPOINT is not configured, but SEMA_REQUIRE_DYNAMODB requires DynamoDB Local")
		}
		t.Skip("DYNAMODB_ENDPOINT is not configured")
	}
	ctx := context.Background()
	awsConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		config.WithBaseEndpoint(endpoint),
	)
	if err != nil {
		t.Fatal(err)
	}
	db := dynamodb.NewFromConfig(awsConfig)
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	table := fmt.Sprintf("sema-test-%d-%s", time.Now().UnixNano(), hex.EncodeToString(suffix))
	_, err = db.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   aws.String(table),
		BillingMode: types.BillingModePayPerRequest,
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("PK"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("SK"), KeyType: types.KeyTypeRange},
		},
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("PK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("SK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("score"), AttributeType: types.ScalarAttributeTypeN},
			{AttributeName: aws.String("gsi1pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("next_fetch_at"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("unread_pk"), AttributeType: types.ScalarAttributeTypeS},
		},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{IndexName: aws.String("by-score"), KeySchema: []types.KeySchemaElement{{AttributeName: aws.String("PK"), KeyType: types.KeyTypeHash}, {AttributeName: aws.String("score"), KeyType: types.KeyTypeRange}}, Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll}},
			{IndexName: aws.String("by-next-fetch"), KeySchema: []types.KeySchemaElement{{AttributeName: aws.String("gsi1pk"), KeyType: types.KeyTypeHash}, {AttributeName: aws.String("next_fetch_at"), KeyType: types.KeyTypeRange}}, Projection: &types.Projection{ProjectionType: types.ProjectionTypeKeysOnly}},
			{IndexName: aws.String("unread-by-score"), KeySchema: []types.KeySchemaElement{{AttributeName: aws.String("unread_pk"), KeyType: types.KeyTypeHash}, {AttributeName: aws.String("score"), KeyType: types.KeyTypeRange}}, Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.DeleteTable(context.Background(), &dynamodb.DeleteTableInput{TableName: aws.String(table)})
	})
	if err := dynamodb.NewTableExistsWaiter(db).Wait(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(table)}, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	return db, table
}
