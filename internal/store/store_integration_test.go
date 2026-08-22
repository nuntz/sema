package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
)

// newIntegrationStore creates an isolated table on DynamoDB Local and returns a
// store bound to it. The table is dropped when the test finishes.
func newIntegrationStore(t *testing.T) (context.Context, *Store) {
	t.Helper()
	endpoint := os.Getenv("DYNAMODB_ENDPOINT")
	if endpoint == "" {
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
	table := fmt.Sprintf("sema-test-%d", time.Now().UnixNano())
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
			{AttributeName: aws.String("feed_pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("published_ts"), AttributeType: types.ScalarAttributeTypeS},
		},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{IndexName: aws.String("by-score"), KeySchema: []types.KeySchemaElement{{AttributeName: aws.String("PK"), KeyType: types.KeyTypeHash}, {AttributeName: aws.String("score"), KeyType: types.KeyTypeRange}}, Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll}},
			{IndexName: aws.String("by-feed"), KeySchema: []types.KeySchemaElement{{AttributeName: aws.String("feed_pk"), KeyType: types.KeyTypeHash}, {AttributeName: aws.String("published_ts"), KeyType: types.KeyTypeRange}}, Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll}},
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

	return ctx, New(db, nil, table, "", "")
}

func TestDynamoAccessPatterns(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	if err := repository.EnsureUser(ctx, "user", "reader@example.com"); err != nil {
		t.Fatal(err)
	}
	user, err := repository.User(ctx, "user")
	if err != nil || user.OrderPref != domain.OrderChrono {
		t.Fatalf("profile = %#v, %v", user, err)
	}
	now := time.Now().UTC()
	items := []domain.Item{
		{PK: domain.UserPK("user"), SK: domain.ItemSK(now.Add(-time.Hour), "old"), FeedPK: "F#feed", ItemID: "old", FeedID: "feed", URL: "https://example.com/old", Title: "Old", PublishedTS: domain.Timestamp(now.Add(-time.Hour)), FetchedTS: domain.Timestamp(now), Score: 0.2, Size: "S", TTL: now.Add(domain.Retention).Unix(), Vector: []byte{1, 2}},
		{PK: domain.UserPK("user"), SK: domain.ItemSK(now, "new"), FeedPK: "F#feed", ItemID: "new", FeedID: "feed", URL: "https://example.com/new", Title: "New", PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now), Score: 0.9, Size: "L", TTL: now.Add(domain.Retention).Unix(), Vector: []byte{3, 4}},
		{PK: domain.UserPK("user"), SK: domain.ItemSK(now.Add(-8*24*time.Hour), "expired"), FeedPK: "F#feed", ItemID: "expired", FeedID: "feed", URL: "https://example.com/expired", Title: "Expired", PublishedTS: domain.Timestamp(now.Add(-8 * 24 * time.Hour)), FetchedTS: domain.Timestamp(now), Score: 1, Size: "L", TTL: now.Add(-time.Hour).Unix()},
	}
	for _, item := range items {
		written, putErr := repository.PutItem(ctx, item)
		if putErr != nil || !written {
			t.Fatalf("put item = %v, %v", written, putErr)
		}
	}
	if written, err := repository.PutItem(ctx, items[0]); err != nil || written {
		t.Fatalf("conditional dedupe = %v, %v", written, err)
	}
	chrono, _, err := repository.Items(ctx, "user", domain.OrderChrono, "", 100)
	if err != nil || len(chrono) != 2 || chrono[0].ItemID != "new" {
		t.Fatalf("chrono = %#v, %v", chrono, err)
	}
	interest, _, err := repository.Items(ctx, "user", domain.OrderInterest, "", 100)
	if err != nil || len(interest) != 2 || interest[0].ItemID != "new" {
		t.Fatalf("interest = %#v, %v", interest, err)
	}
	if err := repository.SetRead(ctx, "user", []string{"new"}, true); err != nil {
		t.Fatal(err)
	}
	if err := repository.ResolveRead(ctx, "user", domain.OrderInterest, "", chrono); err != nil || !chrono[0].Read {
		t.Fatalf("read state = %#v, %v", chrono, err)
	}
	if err := repository.SetSignal(ctx, "user", items[1], 1); err != nil {
		t.Fatal(err)
	}
	signals, err := repository.Signals(ctx, "user")
	if err != nil || len(signals) != 1 || signals[0].Value != 1 {
		t.Fatalf("signals = %#v, %v", signals, err)
	}
}

// A table with no rows must still produce empty JSON arrays. The API encodes
// these slices straight into its responses, and Go renders a nil slice as
// null, which the client cannot consume.
func TestDynamoEmptyDatabase(t *testing.T) {
	ctx, repository := newIntegrationStore(t)

	for _, order := range []domain.Order{domain.OrderChrono, domain.OrderInterest} {
		items, cursor, err := repository.Items(ctx, "nobody", order, "", 100)
		if err != nil {
			t.Fatalf("items(%s) = %v", order, err)
		}
		if len(items) != 0 || cursor != "" {
			t.Fatalf("items(%s) = %#v, cursor %q", order, items, cursor)
		}
		assertEncodesAsArray(t, fmt.Sprintf("items(%s)", order), items)
	}

	feeds, err := repository.Feeds(ctx, "nobody")
	if err != nil || len(feeds) != 0 {
		t.Fatalf("feeds = %#v, %v", feeds, err)
	}
	assertEncodesAsArray(t, "feeds", feeds)

	signals, err := repository.Signals(ctx, "nobody")
	if err != nil || len(signals) != 0 {
		t.Fatalf("signals = %#v, %v", signals, err)
	}
	assertEncodesAsArray(t, "signals", signals)

	if _, err := repository.Item(ctx, "nobody", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("item = %v, want ErrNotFound", err)
	}
}

func assertEncodesAsArray(t *testing.T, label string, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", label, err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("%s encoded as %s, want []", label, encoded)
	}
}
