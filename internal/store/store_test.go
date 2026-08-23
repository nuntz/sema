package store

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
)

type fakeDynamoDB struct {
	batchGet      func(*dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error)
	batchWrite    func(*dynamodb.BatchWriteItemInput) (*dynamodb.BatchWriteItemOutput, error)
	deleteItem    func(*dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error)
	getItem       func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error)
	putItem       func(*dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error)
	query         func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error)
	updateItem    func(*dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error)
	transactWrite func(*dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error)
}

func (f *fakeDynamoDB) BatchGetItem(_ context.Context, input *dynamodb.BatchGetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error) {
	if f.batchGet != nil {
		return f.batchGet(input)
	}
	return &dynamodb.BatchGetItemOutput{}, nil
}

func (f *fakeDynamoDB) BatchWriteItem(_ context.Context, input *dynamodb.BatchWriteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	if f.batchWrite != nil {
		return f.batchWrite(input)
	}
	return &dynamodb.BatchWriteItemOutput{}, nil
}

func (f *fakeDynamoDB) DeleteItem(_ context.Context, input *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	if f.deleteItem != nil {
		return f.deleteItem(input)
	}
	return &dynamodb.DeleteItemOutput{}, nil
}

func (f *fakeDynamoDB) GetItem(_ context.Context, input *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if f.getItem != nil {
		return f.getItem(input)
	}
	return &dynamodb.GetItemOutput{}, nil
}

func (f *fakeDynamoDB) PutItem(_ context.Context, input *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	if f.putItem != nil {
		return f.putItem(input)
	}
	return &dynamodb.PutItemOutput{}, nil
}

func (f *fakeDynamoDB) Query(_ context.Context, input *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	if f.query != nil {
		return f.query(input)
	}
	return &dynamodb.QueryOutput{}, nil
}

func (f *fakeDynamoDB) UpdateItem(_ context.Context, input *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	if f.updateItem != nil {
		return f.updateItem(input)
	}
	return &dynamodb.UpdateItemOutput{}, nil
}

func (f *fakeDynamoDB) TransactWriteItems(_ context.Context, input *dynamodb.TransactWriteItemsInput, _ ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	if f.transactWrite != nil {
		return f.transactWrite(input)
	}
	return &dynamodb.TransactWriteItemsOutput{}, nil
}

func TestDueFeedsQueriesSparseIndex(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	feeds := []domain.Feed{
		{PK: "U#one", SK: "F#first", FeedID: "first", NextFetchAt: domain.Timestamp(now.Add(-time.Hour))},
		{PK: "U#two", SK: "F#second", FeedID: "second", NextFetchAt: domain.Timestamp(now)},
	}
	first, err := attributevalue.MarshalMap(feeds[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := attributevalue.MarshalMap(feeds[1])
	if err != nil {
		t.Fatal(err)
	}
	pageKey := key("U#one", "F#first")
	queries := 0
	db := &fakeDynamoDB{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		queries++
		if aws.ToString(input.IndexName) != "by-next-fetch" || aws.ToString(input.KeyConditionExpression) != "gsi1pk = :feed AND next_fetch_at <= :now" {
			t.Fatalf("query = %#v", input)
		}
		if input.ExpressionAttributeValues[":feed"].(*types.AttributeValueMemberS).Value != feedIndexPK || input.ExpressionAttributeValues[":now"].(*types.AttributeValueMemberS).Value != domain.Timestamp(now) {
			t.Fatalf("query values = %#v", input.ExpressionAttributeValues)
		}
		if queries == 1 {
			if len(input.ExclusiveStartKey) != 0 {
				t.Fatalf("first start key = %#v", input.ExclusiveStartKey)
			}
			return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{first}, LastEvaluatedKey: pageKey}, nil
		}
		if input.ExclusiveStartKey["SK"].(*types.AttributeValueMemberS).Value != "F#first" {
			t.Fatalf("second start key = %#v", input.ExclusiveStartKey)
		}
		return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{second}}, nil
	}}

	got, err := New(db, nil, "table", "", "").DueFeeds(context.Background(), now)
	if err != nil || len(got) != 2 || got[0].FeedID != "first" || got[1].FeedID != "second" || queries != 2 {
		t.Fatalf("DueFeeds = %#v, queries %d, %v", got, queries, err)
	}
}

func TestFeedWritesMaintainSparseIndexKey(t *testing.T) {
	var put *dynamodb.PutItemInput
	var update *dynamodb.UpdateItemInput
	db := &fakeDynamoDB{
		putItem: func(input *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
			put = input
			return &dynamodb.PutItemOutput{}, nil
		},
		updateItem: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			update = input
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}
	repository := New(db, nil, "table", "", "")
	feed := domain.Feed{PK: "U#user", SK: "F#feed", FeedID: "feed", NextFetchAt: domain.Timestamp(time.Now())}
	if err := repository.PutFeed(context.Background(), feed); err != nil {
		t.Fatal(err)
	}
	if put.Item["gsi1pk"].(*types.AttributeValueMemberS).Value != feedIndexPK {
		t.Fatalf("put gsi1pk = %#v", put.Item["gsi1pk"])
	}
	next := time.Date(2026, 8, 22, 13, 0, 0, 0, time.UTC)
	if err := repository.ScheduleFeed(context.Background(), "user", "feed", next); err != nil {
		t.Fatal(err)
	}
	if aws.ToString(update.UpdateExpression) != "SET next_fetch_at = :next, gsi1pk = :feed" || update.ExpressionAttributeValues[":feed"].(*types.AttributeValueMemberS).Value != feedIndexPK {
		t.Fatalf("schedule update = %#v", update)
	}
}
