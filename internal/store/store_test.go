package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
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

func TestListItemProjectionReflectsAllStoredFieldsExceptLargeSearchFields(t *testing.T) {
	projected := make(map[string]bool, len(listItemProjection.names))
	for alias, name := range listItemProjection.names {
		projected[name] = true
		if !strings.Contains(listItemProjection.expression, alias) {
			t.Fatalf("projection expression %q does not contain alias %q", listItemProjection.expression, alias)
		}
	}

	typeOfItem := reflect.TypeOf(domain.Item{})
	for index := range typeOfItem.NumField() {
		name := strings.Split(typeOfItem.Field(index).Tag.Get("dynamodbav"), ",")[0]
		want := name != "" && name != "-" && name != "vector" && name != "search_text"
		if projected[name] != want {
			t.Errorf("projected[%q] = %t, want %t", name, projected[name], want)
		}
	}
	for _, required := range []string{"PK", "SK", "score", "ttl"} {
		if !projected[required] {
			t.Errorf("required cursor/filter attribute %q is not projected", required)
		}
	}
}

func TestSessionStoreLifecycleUsesHashedPrimaryKey(t *testing.T) {
	var stored map[string]types.AttributeValue
	var renewal *dynamodb.UpdateItemInput
	var deletion *dynamodb.DeleteItemInput
	db := &fakeDynamoDB{
		putItem: func(input *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
			stored = input.Item
			if aws.ToString(input.ConditionExpression) != "attribute_not_exists(PK)" {
				t.Fatalf("put condition = %q", aws.ToString(input.ConditionExpression))
			}
			return &dynamodb.PutItemOutput{}, nil
		},
		getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			if !aws.ToBool(input.ConsistentRead) {
				t.Fatal("session lookup was not consistent")
			}
			return &dynamodb.GetItemOutput{Item: stored}, nil
		},
		updateItem: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			renewal = input
			return &dynamodb.UpdateItemOutput{}, nil
		},
		deleteItem: func(input *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error) {
			deletion = input
			return &dynamodb.DeleteItemOutput{}, nil
		},
	}
	repository := New(db, nil, "table", "", "")
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	record := Session{Subject: "reader", Email: "reader@example.com", CreatedAt: now.Unix(), RenewedAt: now.Unix(), ExpiresAt: now.Add(30 * 24 * time.Hour).Unix(), TTL: now.Add(30 * 24 * time.Hour).Unix()}
	if err := repository.PutSession(context.Background(), "hashed-id", record); err != nil {
		t.Fatal(err)
	}
	if stored["PK"].(*types.AttributeValueMemberS).Value != "SESSION#hashed-id" || stored["SK"].(*types.AttributeValueMemberS).Value != sessionSK {
		t.Fatalf("session key = %#v", stored)
	}
	if _, exists := stored["session_id"]; exists {
		t.Fatal("raw session ID was persisted")
	}
	got, err := repository.Session(context.Background(), "hashed-id")
	if err != nil || got.Subject != record.Subject || got.Email != record.Email || got.TTL != record.TTL {
		t.Fatalf("Session = %#v, %v", got, err)
	}
	renewedAt, expiresAt := now.Add(25*time.Hour).Unix(), now.Add(31*24*time.Hour).Unix()
	if err := repository.RenewSession(context.Background(), "hashed-id", renewedAt, expiresAt); err != nil {
		t.Fatal(err)
	}
	if aws.ToString(renewal.UpdateExpression) != "SET renewed_at = :renewed, expires_at = :expires, #ttl = :expires" || aws.ToString(renewal.ConditionExpression) != "attribute_exists(PK) AND #ttl > :renewed" {
		t.Fatalf("renewal = %#v", renewal)
	}
	if renewal.ExpressionAttributeValues[":expires"].(*types.AttributeValueMemberN).Value != strconv.FormatInt(expiresAt, 10) {
		t.Fatalf("renewal expiry = %#v", renewal.ExpressionAttributeValues)
	}
	if err := repository.DeleteSession(context.Background(), "hashed-id"); err != nil {
		t.Fatal(err)
	}
	if deletion.Key["PK"].(*types.AttributeValueMemberS).Value != "SESSION#hashed-id" || deletion.Key["SK"].(*types.AttributeValueMemberS).Value != sessionSK {
		t.Fatalf("delete key = %#v", deletion.Key)
	}
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

func TestItemsForFeedsFillsPageAfterFeedFiltering(t *testing.T) {
	marshal := func(id, feedID string, published time.Time) map[string]types.AttributeValue {
		item, err := attributevalue.MarshalMap(domain.Item{
			PK: domain.UserPK("user"), SK: domain.ItemSK(published, id), ItemID: id,
			FeedID: feedID, PublishedTS: domain.Timestamp(published), TTL: time.Now().Add(time.Hour).Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	now := time.Now().UTC()
	pages := [][]map[string]types.AttributeValue{
		{marshal("skip-1", "muted", now), marshal("skip-2", "other", now.Add(-time.Minute))},
		{marshal("keep-1", "dev", now.Add(-2*time.Minute)), marshal("keep-1", "dev", now.Add(-3*time.Minute)), marshal("keep-2", "dev", now.Add(-4*time.Minute))},
	}
	calls := 0
	db := &fakeDynamoDB{query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		page := pages[calls]
		calls++
		output := &dynamodb.QueryOutput{Items: page}
		if calls == 1 {
			output.LastEvaluatedKey = key(domain.UserPK("user"), "I#continue")
		}
		return output, nil
	}}
	repository := New(db, nil, "table", "", "")
	items, cursor, _, err := repository.ItemsForFeeds(context.Background(), "user", domain.OrderChrono, "", 2, true, map[string]bool{"dev": true})
	if err != nil || cursor != "" || calls != 2 || len(items) != 2 || items[0].FeedID != "dev" || items[1].FeedID != "dev" {
		t.Fatalf("items = %#v, cursor = %q, calls = %d, err = %v", items, cursor, calls, err)
	}
}

func TestItemsForFeedsReturnsNewestReadAnchorWhileFillingUnreadPage(t *testing.T) {
	marshal := func(id string, published time.Time) map[string]types.AttributeValue {
		item, err := attributevalue.MarshalMap(domain.Item{
			PK: domain.UserPK("user"), SK: domain.ItemSK(published, id), ItemID: id,
			FeedID: "feed", PublishedTS: domain.Timestamp(published), TTL: time.Now().Add(time.Hour).Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	now := time.Now().UTC()
	db := &fakeDynamoDB{
		query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{
				marshal("new", now),
				marshal("anchor", now.Add(-time.Minute)),
				marshal("old", now.Add(-2*time.Minute)),
			}}, nil
		},
		batchGet: func(*dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
			return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{
				"table": {{"SK": &types.AttributeValueMemberS{Value: domain.ReadSK("anchor")}}},
			}}, nil
		},
	}
	repository := New(db, nil, "table", "", "")
	items, cursor, anchor, err := repository.ItemsForFeeds(context.Background(), "user", domain.OrderChrono, "", 2, false, nil)
	if err != nil || cursor != "" || len(items) != 2 || items[0].ItemID != "new" || items[1].ItemID != "old" {
		t.Fatalf("items = %#v, cursor = %q, err = %v", items, cursor, err)
	}
	if anchor == nil || anchor.ItemID != "anchor" || !anchor.Read {
		t.Fatalf("anchor = %#v", anchor)
	}
}

func TestItemsForFeedsReturnsBudgetCursorAndResumes(t *testing.T) {
	queryCalls := 0
	db := &fakeDynamoDB{
		query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			queryCalls++
			page := 1
			if len(input.ExclusiveStartKey) > 0 {
				value := input.ExclusiveStartKey["SK"].(*types.AttributeValueMemberS).Value
				previous, err := strconv.Atoi(strings.TrimPrefix(value, "I#page-"))
				if err != nil {
					t.Fatalf("start key = %#v: %v", input.ExclusiveStartKey, err)
				}
				page = previous + 1
			}
			id := fmt.Sprintf("read-%d", page)
			if page >= itemsForFeedsPageBudget {
				id = fmt.Sprintf("unread-%d", page)
			}
			row, err := attributevalue.MarshalMap(domain.Item{
				PK: domain.UserPK("user"), SK: fmt.Sprintf("I#page-%d", page), ItemID: id,
				FeedID: "feed", Score: float64(page), TTL: time.Now().Add(time.Hour).Unix(),
			})
			if err != nil {
				t.Fatal(err)
			}
			output := &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{row}}
			if page < itemsForFeedsPageBudget+1 {
				output.LastEvaluatedKey = key(domain.UserPK("user"), fmt.Sprintf("I#page-%d", page))
			}
			return output, nil
		},
		batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
			rows := []map[string]types.AttributeValue{}
			for _, itemKey := range input.RequestItems["table"].Keys {
				sk := itemKey["SK"].(*types.AttributeValueMemberS).Value
				if strings.HasPrefix(sk, "R#read-") {
					rows = append(rows, map[string]types.AttributeValue{"SK": &types.AttributeValueMemberS{Value: sk}})
				}
			}
			return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": rows}}, nil
		},
	}
	repository := New(db, nil, "table", "", "")

	first, cursor, anchor, err := repository.ItemsForFeeds(context.Background(), "user", domain.OrderChrono, "", 2, false, nil)
	if err != nil || queryCalls != itemsForFeedsPageBudget || len(first) != 1 || first[0].ItemID != "unread-5" || cursor == "" {
		t.Fatalf("budget page = %#v, cursor = %q, queries = %d, err = %v", first, cursor, queryCalls, err)
	}
	if anchor == nil || anchor.ItemID != "read-1" || !anchor.Read {
		t.Fatalf("budget page anchor = %#v", anchor)
	}

	second, cursor, anchor, err := repository.ItemsForFeeds(context.Background(), "user", domain.OrderChrono, cursor, 2, false, nil)
	if err != nil || queryCalls != itemsForFeedsPageBudget+1 || len(second) != 1 || second[0].ItemID != "unread-6" || cursor != "" || anchor != nil {
		t.Fatalf("resumed page = %#v, cursor = %q, anchor = %#v, queries = %d, err = %v", second, cursor, anchor, queryCalls, err)
	}
}

func TestSearchItemsUsesMultiTermAndAndPageFills(t *testing.T) {
	marshal := func(id string) map[string]types.AttributeValue {
		item, err := attributevalue.MarshalMap(domain.Item{
			PK: domain.UserPK("user"), SK: "I#" + id, ItemID: id,
			SearchText: "pulumi lambda", TTL: time.Now().Add(time.Hour).Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	calls := 0
	db := &fakeDynamoDB{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		calls++
		filter := aws.ToString(input.FilterExpression)
		if !strings.Contains(filter, "contains(search_text, :term0) AND contains(search_text, :term1)") ||
			input.ExpressionAttributeValues[":term0"].(*types.AttributeValueMemberS).Value != "pulumi" ||
			input.ExpressionAttributeValues[":term1"].(*types.AttributeValueMemberS).Value != "lambda" {
			t.Fatalf("search query = %#v", input)
		}
		if calls == 1 {
			return &dynamodb.QueryOutput{LastEvaluatedKey: key(domain.UserPK("user"), "I#continue")}, nil
		}
		return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{marshal("one"), marshal("two")}}, nil
	}}
	items, err := New(db, nil, "table", "", "").SearchItems(context.Background(), "user", "I#", []string{"pulumi", "lambda"}, 2)
	if err != nil || calls != 2 || len(items) != 2 {
		t.Fatalf("SearchItems = %#v, calls %d, err %v", items, calls, err)
	}
}

func TestResolveReadDeduplicatesKeysAndFansOutState(t *testing.T) {
	batchCalls := 0
	db := &fakeDynamoDB{batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
		batchCalls++
		request := input.RequestItems["table"]
		if len(request.Keys) != 1 {
			t.Fatalf("read keys = %#v", request.Keys)
		}
		if got := request.Keys[0]["SK"].(*types.AttributeValueMemberS).Value; got != domain.ReadSK("same") {
			t.Fatalf("read key = %q", got)
		}
		return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": {
			{"SK": &types.AttributeValueMemberS{Value: domain.ReadSK("same")}},
		}}}, nil
	}}
	items := []domain.Item{{ItemID: "same"}, {ItemID: "same"}}
	if err := New(db, nil, "table", "", "").ResolveRead(context.Background(), "user", items); err != nil {
		t.Fatal(err)
	}
	if batchCalls != 1 || !items[0].Read || !items[1].Read {
		t.Fatalf("batch calls = %d, items = %#v", batchCalls, items)
	}
}

func TestPutItemWritesIdentityVectorAndFeedCountersAtomically(t *testing.T) {
	calls := 0
	db := &fakeDynamoDB{transactWrite: func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		calls++
		if len(input.TransactItems) != 4 {
			t.Fatalf("transaction = %#v", input.TransactItems)
		}
		identity := input.TransactItems[0].Put.Item
		if got := identity["SK"].(*types.AttributeValueMemberS).Value; got != domain.ItemIdentitySK("same") {
			t.Fatalf("identity key = %q", got)
		}
		if _, exists := input.TransactItems[1].Put.Item["vector"]; exists {
			t.Fatal("live item row contains vector")
		}
		storedVector := input.TransactItems[2].Put.Item
		if got := storedVector["SK"].(*types.AttributeValueMemberS).Value; got != domain.ItemVectorSK("same") || string(storedVector["vector"].(*types.AttributeValueMemberB).Value) != "vector" {
			t.Fatalf("vector row = %#v", storedVector)
		}
		counter := input.TransactItems[3].Update
		if got := counter.Key["SK"].(*types.AttributeValueMemberS).Value; got != domain.FeedSK("feed") {
			t.Fatalf("counter key = %q", got)
		}
		if aws.ToString(counter.UpdateExpression) != "ADD item_count :one, extraction_sample :one, extraction_failures :extraction_failure, media_failures :media_failure, extraction_quality_total :quality" {
			t.Fatalf("counter update = %#v", counter)
		}
		if counter.ExpressionAttributeValues[":extraction_failure"].(*types.AttributeValueMemberN).Value != "1" || counter.ExpressionAttributeValues[":media_failure"].(*types.AttributeValueMemberN).Value != "1" {
			t.Fatalf("counter outcomes = %#v", counter.ExpressionAttributeValues)
		}
		if calls == 2 {
			return nil, &types.TransactionCanceledException{CancellationReasons: []types.CancellationReason{
				{Code: aws.String("ConditionalCheckFailed")}, {Code: aws.String("None")},
			}}
		}
		return &dynamodb.TransactWriteItemsOutput{}, nil
	}}
	repository := New(db, nil, "table", "", "")
	first := domain.Item{PK: domain.UserPK("user"), SK: domain.ItemSK(time.Now(), "same"), ItemID: "same", FeedID: "feed", Vector: []byte("vector"), TTL: time.Now().Add(time.Hour).Unix()}
	written, err := repository.PutItem(context.Background(), first)
	if err != nil || !written {
		t.Fatalf("first put = %v, %v", written, err)
	}
	second := first
	second.SK = domain.ItemSK(time.Now().Add(time.Minute), "same")
	written, err = repository.PutItem(context.Background(), second)
	if err != nil || written {
		t.Fatalf("republished put = %v, %v", written, err)
	}
}

func TestPutItemFailureWritesExpiringIdentityMarker(t *testing.T) {
	ttl := time.Now().Add(domain.Retention).Unix()
	db := &fakeDynamoDB{putItem: func(input *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
		if got := input.Item["PK"].(*types.AttributeValueMemberS).Value; got != domain.UserPK("user") {
			t.Fatalf("PK = %q", got)
		}
		if got := input.Item["SK"].(*types.AttributeValueMemberS).Value; got != domain.ItemIdentitySK("item") {
			t.Fatalf("SK = %q", got)
		}
		if _, exists := input.Item["item_sk"]; exists {
			t.Fatal("terminal marker contains item_sk")
		}
		if got := input.Item["ttl"].(*types.AttributeValueMemberN).Value; got != strconv.FormatInt(ttl, 10) {
			t.Fatalf("ttl = %q", got)
		}
		if aws.ToString(input.ConditionExpression) != "attribute_not_exists(SK) OR #ttl <= :now" {
			t.Fatalf("condition = %q", aws.ToString(input.ConditionExpression))
		}
		return &dynamodb.PutItemOutput{}, nil
	}}
	if err := New(db, nil, "table", "", "").PutItemFailure(context.Background(), "user", "item", ttl); err != nil {
		t.Fatal(err)
	}
}

func TestItemExistsReadsStableIdentityMarker(t *testing.T) {
	db := &fakeDynamoDB{getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
		if got := input.Key["SK"].(*types.AttributeValueMemberS).Value; got != domain.ItemIdentitySK("same") {
			t.Fatalf("identity key = %q", got)
		}
		return &dynamodb.GetItemOutput{Item: map[string]types.AttributeValue{
			"PK":  &types.AttributeValueMemberS{Value: domain.UserPK("user")},
			"SK":  &types.AttributeValueMemberS{Value: domain.ItemIdentitySK("same")},
			"ttl": &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)},
		}}, nil
	}}
	exists, err := New(db, nil, "table", "", "").ItemExists(context.Background(), "user", "same")
	if err != nil || !exists {
		t.Fatalf("ItemExists = %v, %v", exists, err)
	}
}

func TestItemResolvesStableIdentityDirectly(t *testing.T) {
	now := time.Now()
	live := domain.Item{
		PK: domain.UserPK("user"), SK: domain.ItemSK(now, "same"), ItemID: "same", TTL: now.Add(time.Hour).Unix(),
	}
	identity, err := attributevalue.MarshalMap(domain.ItemIdentity{
		PK: live.PK, SK: domain.ItemIdentitySK(live.ItemID), ItemSK: live.SK, TTL: live.TTL,
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := attributevalue.MarshalMap(live)
	if err != nil {
		t.Fatal(err)
	}
	vector, err := attributevalue.MarshalMap(domain.ItemVector{PK: live.PK, SK: domain.ItemVectorSK(live.ItemID), Vector: []byte("vector"), TTL: live.TTL})
	if err != nil {
		t.Fatal(err)
	}
	gets := 0
	db := &fakeDynamoDB{
		getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			gets++
			if !aws.ToBool(input.ConsistentRead) {
				t.Fatal("item lookup was not consistent")
			}
			switch input.Key["SK"].(*types.AttributeValueMemberS).Value {
			case domain.ItemIdentitySK("same"):
				return &dynamodb.GetItemOutput{Item: identity}, nil
			case live.SK:
				return &dynamodb.GetItemOutput{Item: item}, nil
			case domain.ItemVectorSK("same"):
				return &dynamodb.GetItemOutput{Item: vector}, nil
			default:
				t.Fatalf("unexpected item key: %#v", input.Key)
				return nil, nil
			}
		},
		query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			t.Fatal("identity-backed item lookup scanned the partition")
			return nil, nil
		},
	}
	got, err := New(db, nil, "table", "", "").Item(context.Background(), "user", "same")
	if err != nil || got.SK != live.SK || string(got.Vector) != "vector" || gets != 3 {
		t.Fatalf("Item = %#v, gets %d, %v", got, gets, err)
	}
}

func TestItemFallsBackToLegacyInRowVector(t *testing.T) {
	now := time.Now()
	live := domain.Item{PK: domain.UserPK("user"), SK: domain.ItemSK(now, "legacy"), ItemID: "legacy", Vector: []byte("legacy-vector"), TTL: now.Add(time.Hour).Unix()}
	identity, _ := attributevalue.MarshalMap(domain.ItemIdentity{PK: live.PK, SK: domain.ItemIdentitySK(live.ItemID), ItemSK: live.SK, TTL: live.TTL})
	item, _ := attributevalue.MarshalMap(live)
	db := &fakeDynamoDB{getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
		switch input.Key["SK"].(*types.AttributeValueMemberS).Value {
		case domain.ItemIdentitySK(live.ItemID):
			return &dynamodb.GetItemOutput{Item: identity}, nil
		case live.SK:
			return &dynamodb.GetItemOutput{Item: item}, nil
		case domain.ItemVectorSK(live.ItemID):
			return &dynamodb.GetItemOutput{}, nil
		default:
			return nil, errors.New("unexpected key")
		}
	}}
	got, err := New(db, nil, "table", "", "").Item(context.Background(), "user", live.ItemID)
	if err != nil || string(got.Vector) != "legacy-vector" {
		t.Fatalf("Item = %#v, %v", got, err)
	}
}

func TestLoadItemVectorsUsesOneBatchAndPreservesLegacyFallback(t *testing.T) {
	ttl := time.Now().Add(time.Hour).Unix()
	stored, _ := attributevalue.MarshalMap(domain.ItemVector{PK: domain.UserPK("user"), SK: domain.ItemVectorSK("new"), Vector: []byte("separate"), TTL: ttl})
	calls := 0
	db := &fakeDynamoDB{batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
		calls++
		keys := input.RequestItems["table"].Keys
		if len(keys) != 2 || keys[0]["SK"].(*types.AttributeValueMemberS).Value != domain.ItemVectorSK("new") || keys[1]["SK"].(*types.AttributeValueMemberS).Value != domain.ItemVectorSK("legacy") {
			t.Fatalf("vector keys = %#v", keys)
		}
		return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": {stored}}}, nil
	}}
	items := []domain.Item{{ItemID: "new"}, {ItemID: "legacy", Vector: []byte("in-row")}}
	if err := New(db, nil, "table", "", "").LoadItemVectors(context.Background(), "user", items); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || string(items[0].Vector) != "separate" || string(items[1].Vector) != "in-row" {
		t.Fatalf("items = %#v, calls = %d", items, calls)
	}
}

func TestItemRejectsExpiredIdentityWithoutScanning(t *testing.T) {
	identity, err := attributevalue.MarshalMap(domain.ItemIdentity{
		PK: domain.UserPK("user"), SK: domain.ItemIdentitySK("expired"), ItemSK: "I#expired", TTL: time.Now().Add(-time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	db := &fakeDynamoDB{
		getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: identity}, nil
		},
		query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			t.Fatal("expired identity lookup scanned the partition")
			return nil, nil
		},
	}
	if _, err := New(db, nil, "table", "", "").Item(context.Background(), "user", "expired"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Item error = %v, want ErrNotFound", err)
	}
}

func TestItemFallsBackForLegacyLiveRow(t *testing.T) {
	live, err := attributevalue.MarshalMap(domain.Item{
		PK: domain.UserPK("user"), SK: "I#legacy", ItemID: "legacy", TTL: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	queries := 0
	db := &fakeDynamoDB{
		getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{}, nil
		},
		query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			queries++
			if input.ExpressionAttributeValues[":id"].(*types.AttributeValueMemberS).Value != "legacy" {
				t.Fatalf("legacy query = %#v", input)
			}
			return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{live}}, nil
		},
	}
	got, err := New(db, nil, "table", "", "").Item(context.Background(), "user", "legacy")
	if err != nil || got.ItemID != "legacy" || queries != 1 {
		t.Fatalf("Item = %#v, queries %d, %v", got, queries, err)
	}
}

func TestResolveItemIDsUsesIdentityRowsAndPreservesSemantics(t *testing.T) {
	now := time.Now()
	liveItems := []domain.Item{
		{PK: domain.UserPK("user"), SK: "I#live", ItemID: "live", TTL: now.Add(time.Hour).Unix()},
		{PK: domain.UserPK("user"), SK: "I#read", ItemID: "read", TTL: now.Add(time.Hour).Unix()},
	}
	identities := []domain.ItemIdentity{
		{PK: domain.UserPK("user"), SK: domain.ItemIdentitySK("archive"), ItemSK: "I#archive", TTL: now.Add(-time.Hour).Unix()},
		{PK: domain.UserPK("user"), SK: domain.ItemIdentitySK("live"), ItemSK: "I#live", TTL: liveItems[0].TTL},
		{PK: domain.UserPK("user"), SK: domain.ItemIdentitySK("read"), ItemSK: "I#read", TTL: liveItems[1].TTL},
		{PK: domain.UserPK("user"), SK: domain.ItemIdentitySK("missing"), ItemSK: "I#missing", TTL: now.Add(-time.Hour).Unix()},
	}
	archive := domain.Item{PK: domain.UserPK("user"), SK: "A#archive", ItemID: "archive", Read: true}
	marshalList := func(values any) []map[string]types.AttributeValue {
		rows, err := attributevalue.MarshalList(values)
		if err != nil {
			t.Fatal(err)
		}
		result := make([]map[string]types.AttributeValue, 0, len(rows))
		for _, row := range rows {
			result = append(result, row.(*types.AttributeValueMemberM).Value)
		}
		return result
	}
	batchCalls := 0
	queries := 0
	db := &fakeDynamoDB{
		batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
			batchCalls++
			request := input.RequestItems["table"]
			if !aws.ToBool(request.ConsistentRead) && aws.ToString(request.ProjectionExpression) == "" {
				t.Fatal("item batch lookup was not consistent")
			}
			prefix := request.Keys[0]["SK"].(*types.AttributeValueMemberS).Value[:2]
			var rows []map[string]types.AttributeValue
			switch prefix {
			case "D#":
				rows = marshalList(identities)
			case "I#":
				rows = marshalList(liveItems)
			case "R#":
				rows = []map[string]types.AttributeValue{{"SK": &types.AttributeValueMemberS{Value: domain.ReadSK("read")}}}
			default:
				t.Fatalf("unexpected batch keys: %#v", request.Keys)
			}
			return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": rows}}, nil
		},
		query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			queries++
			if input.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS).Value != "A#" {
				t.Fatalf("unexpected fallback query: %#v", input)
			}
			return &dynamodb.QueryOutput{Items: marshalList([]domain.Item{archive})}, nil
		},
	}
	got, err := New(db, nil, "table", "", "").ResolveItemIDs(context.Background(), "user", []string{"archive", "live", "archive", "read", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ItemID != "archive" || got[1].ItemID != "live" || got[2].ItemID != "read" {
		t.Fatalf("resolved items = %#v", got)
	}
	if !got[0].Archived || !got[0].Hearted || got[0].ArchiveSK != archive.SK || got[0].Read {
		t.Fatalf("archive flags = %#v", got[0])
	}
	if got[1].Read || !got[2].Read || batchCalls != 3 || queries != 1 {
		t.Fatalf("read state = %#v, batch calls %d, queries %d", got, batchCalls, queries)
	}
}

func TestArchiveItemUsesLiveArchivePointer(t *testing.T) {
	now := time.Now()
	live := domain.Item{
		PK: domain.UserPK("user"), SK: "I#live", ItemID: "same", ArchiveSK: "A#archive", TTL: now.Add(time.Hour).Unix(),
	}
	identity := domain.ItemIdentity{
		PK: live.PK, SK: domain.ItemIdentitySK(live.ItemID), ItemSK: live.SK, TTL: live.TTL,
	}
	archive := domain.Item{PK: live.PK, SK: live.ArchiveSK, ItemID: live.ItemID}
	rows := map[string]any{identity.SK: identity, live.SK: live, archive.SK: archive}
	db := &fakeDynamoDB{
		getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			sk := input.Key["SK"].(*types.AttributeValueMemberS).Value
			row, ok := rows[sk]
			if !ok {
				return &dynamodb.GetItemOutput{}, nil
			}
			encoded, err := attributevalue.MarshalMap(row)
			if err != nil {
				t.Fatal(err)
			}
			return &dynamodb.GetItemOutput{Item: encoded}, nil
		},
		query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			t.Fatal("archive pointer lookup scanned the partition")
			return nil, nil
		},
	}
	got, err := New(db, nil, "table", "", "").ArchiveItem(context.Background(), "user", "same")
	if err != nil || got.SK != archive.SK || !got.Archived || !got.Hearted {
		t.Fatalf("ArchiveItem = %#v, %v", got, err)
	}
}

func TestReconcileItemIdentityPreservesArchiveAndDeletesDuplicate(t *testing.T) {
	var transaction *dynamodb.TransactWriteItemsInput
	db := &fakeDynamoDB{transactWrite: func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		transaction = input
		return &dynamodb.TransactWriteItemsOutput{}, nil
	}}
	canonical := domain.Item{
		PK: domain.UserPK("user"), SK: "I#new", ItemID: "same", TTL: time.Now().Add(time.Hour).Unix(),
	}
	duplicate := domain.Item{
		PK: domain.UserPK("user"), SK: "I#old", ItemID: "same", ArchiveSK: "A#kept",
	}
	if err := New(db, nil, "table", "", "").ReconcileItemIdentity(context.Background(), "user", canonical, []domain.Item{duplicate}); err != nil {
		t.Fatal(err)
	}
	if transaction == nil || len(transaction.TransactItems) != 3 {
		t.Fatalf("transaction = %#v", transaction)
	}
	marker := transaction.TransactItems[0].Put.Item
	if got := marker["SK"].(*types.AttributeValueMemberS).Value; got != domain.ItemIdentitySK("same") {
		t.Fatalf("marker key = %q", got)
	}
	if got := transaction.TransactItems[1].Delete.Key["SK"].(*types.AttributeValueMemberS).Value; got != "I#old" {
		t.Fatalf("deleted key = %q", got)
	}
	update := transaction.TransactItems[2].Update
	if aws.ToString(update.UpdateExpression) != "SET archive_sk = :archive" || update.ExpressionAttributeValues[":archive"].(*types.AttributeValueMemberS).Value != "A#kept" {
		t.Fatalf("canonical update = %#v", update)
	}
}

func TestUserIDsCombineProfileMarkersAndLegacyFeeds(t *testing.T) {
	calls := 0
	db := &fakeDynamoDB{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		calls++
		value := input.ExpressionAttributeValues[":index"].(*types.AttributeValueMemberS).Value
		if calls == 1 && value != userIndexPK || calls == 2 && value != feedIndexPK {
			t.Fatalf("index query %d = %q", calls, value)
		}
		items := []map[string]types.AttributeValue{{"PK": &types.AttributeValueMemberS{Value: "U#one"}}}
		if value == feedIndexPK {
			items = append(items, map[string]types.AttributeValue{"PK": &types.AttributeValueMemberS{Value: "U#two"}})
		}
		return &dynamodb.QueryOutput{Items: items}, nil
	}}
	users, err := New(db, nil, "table", "", "").UserIDs(context.Background())
	if err != nil || calls != 2 || len(users) != 2 || users[0] != "one" || users[1] != "two" {
		t.Fatalf("UserIDs = %#v, calls %d, %v", users, calls, err)
	}
}

func TestFeedWritesMaintainSparseIndexKey(t *testing.T) {
	var update *dynamodb.UpdateItemInput
	db := &fakeDynamoDB{
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
	indexed := false
	for alias, name := range update.ExpressionAttributeNames {
		if feedCounterAttributes[name] {
			t.Fatalf("feed write replaces counter %q", name)
		}
		if name == "gsi1pk" && strings.Contains(aws.ToString(update.UpdateExpression), alias+" = ") {
			indexed = true
		}
	}
	if !indexed {
		t.Fatalf("feed write did not set sparse index: %#v", update)
	}
	feed.Muted = true
	if err := repository.PutFeed(context.Background(), feed); err != nil {
		t.Fatal(err)
	}
	removed := false
	for alias, name := range update.ExpressionAttributeNames {
		if name == "gsi1pk" && strings.Contains(aws.ToString(update.UpdateExpression), "REMOVE") && strings.Contains(aws.ToString(update.UpdateExpression), alias) {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("muted feed retained sparse index: %#v", update)
	}
	next := time.Date(2026, 8, 22, 13, 0, 0, 0, time.UTC)
	if err := repository.ScheduleFeed(context.Background(), "user", "feed", next); err != nil {
		t.Fatal(err)
	}
	if aws.ToString(update.UpdateExpression) != "SET next_fetch_at = :next, gsi1pk = :feed" || update.ExpressionAttributeValues[":feed"].(*types.AttributeValueMemberS).Value != feedIndexPK {
		t.Fatalf("schedule update = %#v", update)
	}
}

func TestClaimFeedConditionallyLeasesDueFeed(t *testing.T) {
	now := time.Date(2026, 8, 23, 14, 20, 0, 0, time.UTC)
	next := now.Add(5 * time.Minute)
	calls := 0
	db := &fakeDynamoDB{updateItem: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
		calls++
		if aws.ToString(input.UpdateExpression) != "SET next_fetch_at = :next, gsi1pk = :feed" || aws.ToString(input.ConditionExpression) != "next_fetch_at <= :due AND (attribute_not_exists(muted) OR muted = :false)" {
			t.Fatalf("claim update = %#v", input)
		}
		values := input.ExpressionAttributeValues
		if values[":due"].(*types.AttributeValueMemberS).Value != domain.Timestamp(now) || values[":next"].(*types.AttributeValueMemberS).Value != domain.Timestamp(next) || values[":feed"].(*types.AttributeValueMemberS).Value != feedIndexPK || values[":false"].(*types.AttributeValueMemberBOOL).Value {
			t.Fatalf("claim values = %#v", values)
		}
		if calls == 2 {
			return nil, &types.ConditionalCheckFailedException{}
		}
		return &dynamodb.UpdateItemOutput{}, nil
	}}
	repository := New(db, nil, "table", "", "")

	claimed, err := repository.ClaimFeed(context.Background(), "user", "feed", now, next)
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v", claimed, err)
	}
	claimed, err = repository.ClaimFeed(context.Background(), "user", "feed", now, next)
	if err != nil || claimed {
		t.Fatalf("stale claim = %v, %v", claimed, err)
	}
}

func TestReplayOverwriteIsIdempotent(t *testing.T) {
	var writes []map[string]types.AttributeValue
	db := &fakeDynamoDB{transactWrite: func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		if len(input.TransactItems) != 2 {
			t.Fatalf("replay transaction = %#v", input.TransactItems)
		}
		for _, write := range input.TransactItems {
			if write.Put.ConditionExpression != nil {
				t.Fatalf("replay overwrite retained dedupe condition %q", aws.ToString(write.Put.ConditionExpression))
			}
			writes = append(writes, write.Put.Item)
		}
		return &dynamodb.TransactWriteItemsOutput{}, nil
	}}
	repository := New(db, nil, "table", "", "")
	item := domain.Item{
		PK: "U#user", SK: "I#item", ItemID: "item", Score: 0.8, Size: "L", Summary: "new summary", Vector: []byte{1, 2},
		Read: true, Signal: -1, Hearted: true, ArchiveSK: "A#kept", HeartedTS: "2026-08-20T12:00:00Z",
	}
	if err := repository.OverwriteItem(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if err := repository.OverwriteItem(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 4 || writes[0]["score"].(*types.AttributeValueMemberN).Value != writes[2]["score"].(*types.AttributeValueMemberN).Value {
		t.Fatalf("double replay writes = %#v", writes)
	}
	if _, exists := writes[0]["vector"]; exists || string(writes[1]["vector"].(*types.AttributeValueMemberB).Value) != string(item.Vector) {
		t.Fatalf("replay vector rows = %#v", writes)
	}
	for _, transient := range []string{"read", "signal", "hearted"} {
		if _, ok := writes[0][transient]; ok {
			t.Fatalf("replay embedded separate %s state in item row: %#v", transient, writes[0])
		}
	}
	if writes[0]["archive_sk"].(*types.AttributeValueMemberS).Value != "A#kept" || writes[0]["hearted_ts"].(*types.AttributeValueMemberS).Value != item.HeartedTS {
		t.Fatalf("replay did not preserve heart pointer: %#v", writes[0])
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

func TestBehaviourMergeIsMonotonic(t *testing.T) {
	var updates []*dynamodb.UpdateItemInput
	db := &fakeDynamoDB{updateItem: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
		updates = append(updates, input)
		return &dynamodb.UpdateItemOutput{}, nil
	}}
	dwell := int64(31_000)
	item := domain.Item{ItemID: "item", FeedID: "feed", Title: "Title", Vector: []byte{1, 2}, ModelVersion: "v"}
	err := New(db, nil, "table", "", "").RecordBehaviour(context.Background(), "user", item, BehaviourEvent{
		Opened: true, DwellMS: &dwell, ClickedThrough: true, Shared: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 2 {
		t.Fatalf("updates = %d, want metadata/flags plus dwell max", len(updates))
	}
	first := aws.ToString(updates[0].UpdateExpression)
	for _, expression := range []string{
		"opened_at = if_not_exists(opened_at, :opened)",
		"#vector = if_not_exists(#vector, :vector)",
		"#ttl = if_not_exists(#ttl, :ttl)",
		"clicked_through = :clicked",
		"#shared = :shared",
	} {
		if !strings.Contains(first, expression) {
			t.Fatalf("first update %q lacks %q", first, expression)
		}
	}
	if got := updates[0].ExpressionAttributeNames["#ttl"]; got != "ttl" {
		t.Fatalf("ttl alias = %q, want ttl", got)
	}
	if got := updates[0].ExpressionAttributeNames["#shared"]; got != "shared" {
		t.Fatalf("shared alias = %q, want shared", got)
	}
	if got := aws.ToString(updates[1].ConditionExpression); got != "attribute_not_exists(dwell_ms) OR dwell_ms < :dwell" {
		t.Fatalf("dwell condition = %q", got)
	}

	updates = nil
	if err := New(db, nil, "table", "", "").RecordBehaviour(context.Background(), "user", item, BehaviourEvent{Opened: true}); err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 {
		t.Fatalf("open updates = %d, want one", len(updates))
	}
	if _, exists := updates[0].ExpressionAttributeNames["#shared"]; exists {
		t.Fatal("non-share update includes unused #shared alias")
	}
}

func TestSetSignalMaintainsProfileCount(t *testing.T) {
	var signal map[string]types.AttributeValue
	var deltas []string
	db := &fakeDynamoDB{
		putItem: func(input *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
			if input.Item["SK"].(*types.AttributeValueMemberS).Value == "MODEL" {
				return &dynamodb.PutItemOutput{}, nil
			}
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
				PK: "U#user", SK: domain.ItemSK(time.Now(), "item"), ItemID: "item", FeedID: "feed", Title: "TITLE", Summary: "Summary", TTL: time.Now().Add(time.Hour).Unix(),
			}
			encodedItem, err := attributevalue.MarshalMap(item)
			if err != nil {
				t.Fatal(err)
			}
			profile, err := attributevalue.MarshalMap(domain.User{PK: "U#user", SK: "PROFILE", HeartCount: 1, SignalCount: 1})
			if err != nil {
				t.Fatal(err)
			}
			identity, err := attributevalue.MarshalMap(domain.ItemIdentity{PK: item.PK, SK: domain.ItemIdentitySK(item.ItemID), ItemSK: item.SK, TTL: item.TTL})
			if err != nil {
				t.Fatal(err)
			}
			var transactions []*dynamodb.TransactWriteItemsInput
			db := &fakeDynamoDB{
				getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
					switch input.Key["SK"].(*types.AttributeValueMemberS).Value {
					case domain.ItemIdentitySK(item.ItemID):
						return &dynamodb.GetItemOutput{Item: identity}, nil
					case item.SK:
						return &dynamodb.GetItemOutput{Item: encodedItem}, nil
					default:
						return &dynamodb.GetItemOutput{Item: profile}, nil
					}
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
			archiveSearch := ""
			for _, write := range transactions[0].TransactItems {
				if write.Put == nil {
					continue
				}
				if value, ok := write.Put.Item["SK"].(*types.AttributeValueMemberS); ok && strings.HasPrefix(value.Value, "A#") {
					archiveSearch = write.Put.Item["search_text"].(*types.AttributeValueMemberS).Value
				}
			}
			if archiveSearch != "title summary" {
				t.Fatalf("archive search_text = %q", archiveSearch)
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
			identity, _ := attributevalue.MarshalMap(domain.ItemIdentity{PK: item.PK, SK: domain.ItemIdentitySK(item.ItemID), ItemSK: item.SK, TTL: item.TTL})
			profile, _ := attributevalue.MarshalMap(domain.User{PK: "U#user", SK: "PROFILE"})
			var transactions []*dynamodb.TransactWriteItemsInput
			db := &fakeDynamoDB{
				getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
					switch input.Key["SK"].(*types.AttributeValueMemberS).Value {
					case domain.ItemIdentitySK(item.ItemID):
						return &dynamodb.GetItemOutput{Item: identity}, nil
					case item.SK:
						return &dynamodb.GetItemOutput{Item: encodedItem}, nil
					case "PROFILE":
						return &dynamodb.GetItemOutput{Item: profile}, nil
					default:
						return &dynamodb.GetItemOutput{Item: encodedArchive}, nil
					}
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
