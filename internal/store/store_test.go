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

func TestSignalValuesRetriesUnprocessedKeys(t *testing.T) {
	first := map[string]types.AttributeValue{
		"SK": &types.AttributeValueMemberS{Value: "S#first"}, "value": &types.AttributeValueMemberN{Value: "1"},
	}
	second := map[string]types.AttributeValue{
		"SK": &types.AttributeValueMemberS{Value: "S#second"}, "value": &types.AttributeValueMemberN{Value: "-1"},
	}
	secondKey := key("U#user", "S#second")
	calls := 0
	db := &fakeDynamoDB{batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
		calls++
		request := input.RequestItems["table"]
		if aws.ToString(request.ProjectionExpression) != "SK, #value" || request.ExpressionAttributeNames["#value"] != "value" {
			t.Fatalf("batch get projection = %#v", request)
		}
		if calls == 1 {
			if len(request.Keys) != 2 {
				t.Fatalf("first keys = %#v", request.Keys)
			}
			return &dynamodb.BatchGetItemOutput{
				Responses:       map[string][]map[string]types.AttributeValue{"table": {first}},
				UnprocessedKeys: map[string]types.KeysAndAttributes{"table": {Keys: []map[string]types.AttributeValue{secondKey}}},
			}, nil
		}
		if len(request.Keys) != 1 || request.Keys[0]["SK"].(*types.AttributeValueMemberS).Value != "S#second" {
			t.Fatalf("retry keys = %#v", request.Keys)
		}
		return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": {second}}}, nil
	}}

	values, err := New(db, nil, "table", "", "").SignalValues(context.Background(), "user", []string{"first", "second", "first"})
	if err != nil || calls != 2 || len(values) != 2 || values["first"] != 1 || values["second"] != -1 {
		t.Fatalf("SignalValues = %#v, calls %d, %v", values, calls, err)
	}
}

func TestSignalValuesSkipsEmptyBatch(t *testing.T) {
	db := &fakeDynamoDB{batchGet: func(*dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
		t.Fatal("unexpected BatchGetItem")
		return nil, nil
	}}
	values, err := New(db, nil, "table", "", "").SignalValues(context.Background(), "user", nil)
	if err != nil || len(values) != 0 {
		t.Fatalf("SignalValues = %#v, %v", values, err)
	}
}

func TestSetSignalMaintainsProfileCount(t *testing.T) {
	var signal map[string]types.AttributeValue
	var deltas []string
	db := &fakeDynamoDB{
		putItem: func(input *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
			if input.ReturnValues != types.ReturnValueAllOld {
				t.Fatalf("put return values = %q", input.ReturnValues)
			}
			old := signal
			signal = input.Item
			return &dynamodb.PutItemOutput{Attributes: old}, nil
		},
		deleteItem: func(input *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error) {
			if input.ReturnValues != types.ReturnValueAllOld {
				t.Fatalf("delete return values = %q", input.ReturnValues)
			}
			old := signal
			signal = nil
			return &dynamodb.DeleteItemOutput{Attributes: old}, nil
		},
		updateItem: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			if aws.ToString(input.UpdateExpression) != "ADD signal_count :delta" {
				t.Fatalf("count update = %#v", input)
			}
			deltas = append(deltas, input.ExpressionAttributeValues[":delta"].(*types.AttributeValueMemberN).Value)
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}
	repository := New(db, nil, "table", "", "")
	item := domain.Item{ItemID: "item", FeedID: "feed", Title: "title", Vector: []byte{1}}
	for _, value := range []int{1, -1, 0, 0} {
		if err := repository.SetSignal(context.Background(), "user", item, value); err != nil {
			t.Fatalf("SetSignal(%d): %v", value, err)
		}
	}
	item.ArchiveSK = "A#item"
	if err := repository.SetSignal(context.Background(), "user", item, 0); err != nil {
		t.Fatal(err)
	}
	if len(deltas) != 3 || deltas[0] != "1" || deltas[1] != "-1" || deltas[2] != "1" {
		t.Fatalf("signal count deltas = %#v", deltas)
	}
	if signal["source"].(*types.AttributeValueMemberS).Value != "heart" {
		t.Fatalf("restored signal = %#v", signal)
	}
}

func TestSetHeartCountsOnlyCreatedSignal(t *testing.T) {
	for _, test := range []struct {
		name     string
		fallback bool
	}{
		{name: "creates heart signal"},
		{name: "keeps existing signal", fallback: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := domain.Item{
				PK: "U#user", SK: domain.ItemSK(time.Now(), "item"), ItemID: "item", FeedID: "feed", Title: "title", TTL: time.Now().Add(time.Hour).Unix(),
			}
			encodedItem, err := attributevalue.MarshalMap(item)
			if err != nil {
				t.Fatal(err)
			}
			profile, err := attributevalue.MarshalMap(domain.User{PK: "U#user", SK: "PROFILE", HeartCount: 1, SignalCount: 1})
			if err != nil {
				t.Fatal(err)
			}
			var transactions []*dynamodb.TransactWriteItemsInput
			db := &fakeDynamoDB{
				query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
					return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{encodedItem}}, nil
				},
				getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
					return &dynamodb.GetItemOutput{Item: profile}, nil
				},
				transactWrite: func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
					transactions = append(transactions, input)
					if test.fallback && len(transactions) == 1 {
						return nil, &types.TransactionCanceledException{}
					}
					return &dynamodb.TransactWriteItemsOutput{}, nil
				},
			}
			if _, _, err := New(db, nil, "table", "", "").SetHeart(context.Background(), "user", "item", true); err != nil {
				t.Fatal(err)
			}
			if len(transactions) != 1+boolInt(test.fallback) {
				t.Fatalf("transactions = %d", len(transactions))
			}
			if got := profileUpdateExpression(transactions[0]); got != "ADD heart_count :one, signal_count :one" {
				t.Fatalf("signal transaction profile update = %q", got)
			}
			if test.fallback {
				if got := profileUpdateExpression(transactions[1]); got != "ADD heart_count :one" {
					t.Fatalf("fallback profile update = %q", got)
				}
			}
		})
	}
}

func TestRemoveHeartCountsOnlyDeletedHeartSignal(t *testing.T) {
	for _, test := range []struct {
		name     string
		fallback bool
	}{
		{name: "deletes heart signal"},
		{name: "keeps explicit signal", fallback: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := domain.Item{PK: "U#user", SK: domain.ItemSK(time.Now(), "item"), ItemID: "item", ArchiveSK: "A#item", TTL: time.Now().Add(time.Hour).Unix()}
			archive := item
			archive.SK = item.ArchiveSK
			encodedItem, _ := attributevalue.MarshalMap(item)
			encodedArchive, _ := attributevalue.MarshalMap(archive)
			profile, _ := attributevalue.MarshalMap(domain.User{PK: "U#user", SK: "PROFILE"})
			var transactions []*dynamodb.TransactWriteItemsInput
			db := &fakeDynamoDB{
				query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
					return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{encodedItem}}, nil
				},
				getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
					if input.Key["SK"].(*types.AttributeValueMemberS).Value == "PROFILE" {
						return &dynamodb.GetItemOutput{Item: profile}, nil
					}
					return &dynamodb.GetItemOutput{Item: encodedArchive}, nil
				},
				transactWrite: func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
					transactions = append(transactions, input)
					if test.fallback && len(transactions) == 1 {
						return nil, &types.TransactionCanceledException{}
					}
					return &dynamodb.TransactWriteItemsOutput{}, nil
				},
			}
			if _, _, err := New(db, nil, "table", "", "").SetHeart(context.Background(), "user", "item", false); err != nil {
				t.Fatal(err)
			}
			if len(transactions) != 1+boolInt(test.fallback) {
				t.Fatalf("transactions = %d", len(transactions))
			}
			if got := profileUpdateExpression(transactions[0]); got != "ADD heart_count :minus_one, signal_count :minus_one" {
				t.Fatalf("signal transaction profile update = %q", got)
			}
			if test.fallback {
				if got := profileUpdateExpression(transactions[1]); got != "ADD heart_count :minus_one" {
					t.Fatalf("fallback profile update = %q", got)
				}
			}
		})
	}
}

func profileUpdateExpression(input *dynamodb.TransactWriteItemsInput) string {
	for _, write := range input.TransactItems {
		if write.Update != nil && write.Update.Key["SK"].(*types.AttributeValueMemberS).Value == "PROFILE" {
			return aws.ToString(write.Update.UpdateExpression)
		}
	}
	return ""
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
