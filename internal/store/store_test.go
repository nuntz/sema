package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/smithy-go"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
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
		want := name != "" && name != "-" && name != "vector" && name != "image_vector" && name != "search_text"
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

func TestFeedItemCountsUsesRetainedItemsAndReadMarkers(t *testing.T) {
	now := time.Now().UTC()
	marshal := func(id, feedID string) map[string]types.AttributeValue {
		item, err := attributevalue.MarshalMap(domain.Item{
			PK: domain.UserPK("user"), SK: domain.ItemSK(now, id), ItemID: id,
			FeedID: feedID, FetchedTS: now.Format(time.RFC3339Nano), TTL: now.Add(time.Hour).Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	db := &fakeDynamoDB{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		prefix := input.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS).Value
		if prefix == "R#" {
			return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{
				{"SK": &types.AttributeValueMemberS{Value: domain.ReadSK("read")}},
			}}, nil
		}
		if aws.ToBool(input.ConsistentRead) {
			t.Fatal("badge counts must use eventually consistent reads")
		}
		if aws.ToString(input.FilterExpression) != "#ttl > :now" || aws.ToString(input.ProjectionExpression) != "item_id, feed_id, fetched_ts" {
			t.Fatalf("item count query = %#v", input)
		}
		return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{
			marshal("read", "alpha"),
			marshal("unread", "alpha"),
			marshal("unread", "alpha"),
			marshal("other", "beta"),
		}}, nil
	}}

	got, err := New(db, nil, "table", "", "").FeedItemCounts(context.Background(), "user", domain.FetchWindow{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]domain.FeedItemCount{
		"alpha": {All: 2, Unread: 1},
		"beta":  {All: 1, Unread: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("feed item counts = %#v, want %#v", got, want)
	}
}

func TestFeedItemCountsExcludesOutsideWindow(t *testing.T) {
	now := time.Now().UTC()
	db := &fakeDynamoDB{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		if input.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS).Value == "R#" {
			return &dynamodb.QueryOutput{}, nil
		}
		var items []map[string]types.AttributeValue
		for i, ts := range []time.Time{now.Add(-time.Hour), now, now.Add(time.Hour)} {
			item, _ := attributevalue.MarshalMap(domain.Item{ItemID: strconv.Itoa(i), FeedID: "feed", FetchedTS: ts.Format(time.RFC3339Nano)})
			items = append(items, item)
		}
		return &dynamodb.QueryOutput{Items: items}, nil
	}}
	got, err := New(db, nil, "table", "", "").FeedItemCounts(context.Background(), "user", domain.FetchWindow{From: now, Before: now.Add(time.Hour)}, nil)
	if err != nil || got["feed"].All != 1 || got["feed"].Unread != 1 {
		t.Fatalf("counts = %#v, %v", got, err)
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
	first := key("U#one", "F#first")
	second := key("U#two", "F#second")
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
	want := []domain.Feed{{PK: "U#one", SK: "F#first", FeedID: "first"}, {PK: "U#two", SK: "F#second", FeedID: "second"}}
	if err != nil || !reflect.DeepEqual(got, want) || queries != 2 {
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
	items, cursor, _, err := repository.ItemsForFeeds(context.Background(), "user", domain.OrderChrono, "", 2, true, false, map[string]bool{"dev": true}, nil, domain.FetchWindow{}, nil)
	if err != nil || cursor != "" || calls != 2 || len(items) != 2 || items[0].FeedID != "dev" || items[1].FeedID != "dev" {
		t.Fatalf("items = %#v, cursor = %q, calls = %d, err = %v", items, cursor, calls, err)
	}
}

func TestItemsForFeedsFillsFilteredIncludeReadPageBeyondDefaultBudget(t *testing.T) {
	now := time.Now().UTC()
	calls := 0
	db := &fakeDynamoDB{query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		calls++
		page := make([]map[string]types.AttributeValue, 0, 100)
		for candidate := range 100 {
			id := fmt.Sprintf("skip-%d-%d", calls, candidate)
			feedID := "other"
			if calls == itemsForFeedsPageBudget+1 && candidate < 2 {
				id = fmt.Sprintf("keep-%d", candidate)
				feedID = "dev"
			}
			item, err := attributevalue.MarshalMap(domain.Item{
				PK: domain.UserPK("user"), SK: domain.ItemSK(now.Add(-time.Duration((calls-1)*100+candidate)*time.Second), id), ItemID: id,
				FeedID: feedID, Score: float64(1_000 - ((calls-1)*100 + candidate)), TTL: now.Add(time.Hour).Unix(),
			})
			if err != nil {
				t.Fatal(err)
			}
			page = append(page, item)
		}
		return &dynamodb.QueryOutput{
			Items:            page,
			LastEvaluatedKey: itemPageKey(page[len(page)-1], domain.OrderInterest),
		}, nil
	}}

	items, cursor, _, err := New(db, nil, "table", "", "").ItemsForFeeds(
		context.Background(), "user", domain.OrderInterest, "", 2, true, true, map[string]bool{"dev": true}, nil, domain.FetchWindow{}, nil)
	if err != nil || len(items) != 2 || items[0].ItemID != "keep-0" || items[1].ItemID != "keep-1" || cursor == "" || calls != itemsForFeedsPageBudget+1 {
		t.Fatalf("items = %#v, cursor = %q, calls = %d, err = %v", items, cursor, calls, err)
	}
}

func TestItemsForFeedsScalesFilteredPageBudget(t *testing.T) {
	for _, test := range []struct{ limit, pages int }{{1, 10}, {20, 20}, {100, 100}} {
		t.Run(fmt.Sprint(test.limit), func(t *testing.T) {
			for _, includeRead := range []bool{true, false} {
				calls := 0
				db := &fakeDynamoDB{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
					if prefix, ok := input.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS); ok && prefix.Value == "R#" {
						return &dynamodb.QueryOutput{}, nil
					}
					calls++
					id := fmt.Sprint(calls)
					feedID := "other"
					// Return a partial page before exhausting the budget.
					if test.limit > 1 && calls == 1 {
						feedID = "keep"
					}
					row, err := attributevalue.MarshalMap(domain.Item{PK: domain.UserPK("user"), SK: "I#" + id, ItemID: id, FeedID: feedID})
					if err != nil {
						t.Fatal(err)
					}
					return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{row}, LastEvaluatedKey: itemPageKey(row, domain.OrderChrono)}, nil
				}}
				items, cursor, _, err := New(db, nil, "table", "", "").ItemsForFeeds(context.Background(), "user", domain.OrderChrono, "", test.limit, includeRead, true, map[string]bool{"keep": true}, nil, domain.FetchWindow{}, nil)
				wantItems := 0
				if test.limit > 1 {
					wantItems = 1
				}
				if err != nil || calls != test.pages || len(items) != wantItems || cursor == "" {
					t.Fatalf("includeRead=%v calls=%d items=%v cursor=%q err=%v", includeRead, calls, items, cursor, err)
				}
			}
		})
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
		query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			prefix := input.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS).Value
			if prefix == "R#" {
				return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{
					{"SK": &types.AttributeValueMemberS{Value: domain.ReadSK("anchor")}},
				}}, nil
			}
			return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{
				marshal("new", now),
				marshal("anchor", now.Add(-time.Minute)),
				marshal("old", now.Add(-2*time.Minute)),
			}}, nil
		},
		batchGet: func(*dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
			t.Fatal("unread page used per-page BatchGet")
			return nil, nil
		},
	}
	repository := New(db, nil, "table", "", "")
	items, cursor, anchor, err := repository.ItemsForFeeds(context.Background(), "user", domain.OrderChrono, "", 2, false, false, nil, nil, domain.FetchWindow{}, nil)
	if err != nil || cursor != "" || len(items) != 2 || items[0].ItemID != "new" || items[1].ItemID != "old" {
		t.Fatalf("items = %#v, cursor = %q, err = %v", items, cursor, err)
	}
	if anchor == nil || anchor.ItemID != "anchor" || !anchor.Read {
		t.Fatalf("anchor = %#v", anchor)
	}
}

func TestItemsForFeedsReturnsBudgetCursorAndResumes(t *testing.T) {
	itemQueryCalls := 0
	readQueryCalls := 0
	db := &fakeDynamoDB{
		query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			prefix := input.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS).Value
			if prefix == "R#" {
				readQueryCalls++
				rows := make([]map[string]types.AttributeValue, 0, unreadItemsForFeedsPageBudget-1)
				for page := 1; page < unreadItemsForFeedsPageBudget; page++ {
					rows = append(rows, map[string]types.AttributeValue{
						"SK": &types.AttributeValueMemberS{Value: domain.ReadSK(fmt.Sprintf("read-%d", page))},
					})
				}
				return &dynamodb.QueryOutput{Items: rows}, nil
			}
			itemQueryCalls++
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
			if page >= unreadItemsForFeedsPageBudget {
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
			if page < unreadItemsForFeedsPageBudget+1 {
				output.LastEvaluatedKey = key(domain.UserPK("user"), fmt.Sprintf("I#page-%d", page))
			}
			return output, nil
		},
		batchGet: func(*dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
			t.Fatal("unread page used per-page BatchGet")
			return nil, nil
		},
	}
	repository := New(db, nil, "table", "", "")

	first, cursor, anchor, err := repository.ItemsForFeeds(context.Background(), "user", domain.OrderChrono, "", 100, false, false, nil, nil, domain.FetchWindow{}, nil)
	if err != nil || itemQueryCalls != unreadItemsForFeedsPageBudget || readQueryCalls != 1 || len(first) != 1 || first[0].ItemID != "unread-100" || cursor == "" {
		t.Fatalf("budget page = %#v, cursor = %q, item queries = %d, read queries = %d, err = %v", first, cursor, itemQueryCalls, readQueryCalls, err)
	}
	if anchor == nil || anchor.ItemID != "read-1" || !anchor.Read {
		t.Fatalf("budget page anchor = %#v", anchor)
	}

	second, cursor, anchor, err := repository.ItemsForFeeds(context.Background(), "user", domain.OrderChrono, cursor, 100, false, false, nil, nil, domain.FetchWindow{}, nil)
	if err != nil || itemQueryCalls != unreadItemsForFeedsPageBudget+1 || readQueryCalls != 2 || len(second) != 1 || second[0].ItemID != "unread-101" || cursor != "" || anchor != nil {
		t.Fatalf("resumed page = %#v, cursor = %q, anchor = %#v, item queries = %d, read queries = %d, err = %v", second, cursor, anchor, itemQueryCalls, readQueryCalls, err)
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
	items, err := New(db, nil, "table", "", "").SearchItems(context.Background(), "user", "I#", []string{"pulumi", "lambda"}, 2, nil)
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
	if err := New(db, nil, "table", "", "").ResolveRead(context.Background(), "user", items, nil); err != nil {
		t.Fatal(err)
	}
	if batchCalls != 1 || !items[0].Read || !items[1].Read {
		t.Fatalf("batch calls = %d, items = %#v", batchCalls, items)
	}
}

func TestResolveReadChunksAtDynamoBatchLimit(t *testing.T) {
	batchCalls := 0
	db := &fakeDynamoDB{batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
		batchCalls++
		request := input.RequestItems["table"]
		if len(request.Keys) == 0 || len(request.Keys) > 100 {
			t.Fatalf("read batch size = %d", len(request.Keys))
		}
		rows := make([]map[string]types.AttributeValue, 0, len(request.Keys))
		for _, itemKey := range request.Keys {
			rows = append(rows, map[string]types.AttributeValue{"SK": itemKey["SK"]})
		}
		return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": rows}}, nil
	}}
	items := make([]domain.Item, 205)
	for index := range items {
		items[index].ItemID = fmt.Sprintf("item-%03d", index)
	}
	if err := New(db, nil, "table", "", "").ResolveRead(context.Background(), "user", items, nil); err != nil {
		t.Fatal(err)
	}
	if batchCalls != 3 {
		t.Fatalf("batch calls = %d, want 3", batchCalls)
	}
	for _, item := range items {
		if !item.Read {
			t.Fatalf("item %q was not resolved read", item.ItemID)
		}
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
		if _, exists := input.TransactItems[1].Put.Item["image_vector"]; exists {
			t.Fatal("live item row contains image vector")
		}
		storedVector := input.TransactItems[2].Put.Item
		if got := storedVector["SK"].(*types.AttributeValueMemberS).Value; got != domain.ItemVectorSK("same") || string(storedVector["vector"].(*types.AttributeValueMemberB).Value) != "vector" || string(storedVector["image_vector"].(*types.AttributeValueMemberB).Value) != "image" || storedVector["image_model_version"].(*types.AttributeValueMemberS).Value != "image-v1" {
			t.Fatalf("vector row = %#v", storedVector)
		}
		counter := input.TransactItems[3].Update
		if got := counter.Key["SK"].(*types.AttributeValueMemberS).Value; got != domain.FeedSK("feed") {
			t.Fatalf("counter key = %q", got)
		}
		if aws.ToString(counter.UpdateExpression) != "ADD item_count :one, extraction_sample :one, extraction_failures :extraction_failure, media_failures :media_failure, extraction_quality_total :quality, link_item_count :link_item" {
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
	first := domain.Item{PK: domain.UserPK("user"), SK: domain.ItemSK(time.Now(), "same"), ItemID: "same", FeedID: "feed", Vector: []byte("vector"), ImageVector: []byte("image"), ImageModelVersion: "image-v1", TTL: time.Now().Add(time.Hour).Unix()}
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

func TestPutItemRetriesTransactionConflicts(t *testing.T) {
	calls := 0
	db := &fakeDynamoDB{transactWrite: func(*dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		calls++
		if calls <= 2 {
			return nil, transactionCanceled("None", "None", "None", "TransactionConflict")
		}
		return &dynamodb.TransactWriteItemsOutput{}, nil
	}}
	repository := New(db, nil, "table", "", "")
	repository.sleep = func(context.Context, time.Duration) error { return nil }

	written, err := repository.PutItem(context.Background(), putItemRetryFixture())
	if err != nil || !written {
		t.Fatalf("PutItem() = %v, %v; want true, nil", written, err)
	}
	if calls != 3 {
		t.Fatalf("TransactWriteItems calls = %d, want 3", calls)
	}
}

func TestPutItemReturnsPersistentTransactionConflict(t *testing.T) {
	conflict := transactionCanceled("None", "None", "None", "TransactionConflict")
	calls := 0
	db := &fakeDynamoDB{transactWrite: func(*dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		calls++
		return nil, conflict
	}}
	repository := New(db, nil, "table", "", "")
	repository.sleep = func(context.Context, time.Duration) error { return nil }

	written, err := repository.PutItem(context.Background(), putItemRetryFixture())
	if written || err != conflict {
		t.Fatalf("PutItem() = %v, %v; want false, conflict", written, err)
	}
	if calls != 5 {
		t.Fatalf("TransactWriteItems calls = %d, want 5", calls)
	}
}

func TestPutItemDoesNotRetryConditionalCheckFailure(t *testing.T) {
	calls := 0
	db := &fakeDynamoDB{transactWrite: func(*dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		calls++
		return nil, transactionCanceled("None", "ConditionalCheckFailed")
	}}
	repository := New(db, nil, "table", "", "")
	repository.sleep = func(context.Context, time.Duration) error {
		t.Fatal("slept after conditional check failure")
		return nil
	}

	written, err := repository.PutItem(context.Background(), putItemRetryFixture())
	if err != nil || written {
		t.Fatalf("PutItem() = %v, %v; want false, nil", written, err)
	}
	if calls != 1 {
		t.Fatalf("TransactWriteItems calls = %d, want 1", calls)
	}
}

func TestPutItemPrioritizesConditionalCheckFailureOverTransactionConflict(t *testing.T) {
	calls := 0
	db := &fakeDynamoDB{transactWrite: func(*dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		calls++
		return nil, transactionCanceled("ConditionalCheckFailed", "None", "None", "TransactionConflict")
	}}
	repository := New(db, nil, "table", "", "")
	repository.sleep = func(context.Context, time.Duration) error {
		t.Fatal("slept after conditional check failure")
		return nil
	}

	written, err := repository.PutItem(context.Background(), putItemRetryFixture())
	if err != nil || written {
		t.Fatalf("PutItem() = %v, %v; want false, nil", written, err)
	}
	if calls != 1 {
		t.Fatalf("TransactWriteItems calls = %d, want 1", calls)
	}
}

func putItemRetryFixture() domain.Item {
	return domain.Item{
		PK: domain.UserPK("user"), SK: domain.ItemSK(time.Now(), "item"), ItemID: "item", FeedID: "feed", TTL: time.Now().Add(time.Hour).Unix(),
	}
}

func transactionCanceled(codes ...string) *types.TransactionCanceledException {
	reasons := make([]types.CancellationReason, len(codes))
	for index, code := range codes {
		reasons[index].Code = aws.String(code)
	}
	return &types.TransactionCanceledException{CancellationReasons: reasons}
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

func TestItemExistsTreatsPermanentArchiveIdentityAsDuplicate(t *testing.T) {
	identity, err := attributevalue.MarshalMap(domain.ItemIdentity{
		PK: domain.UserPK("user"), SK: domain.ItemIdentitySK("hearted"),
		ItemSK: domain.ArchiveSK(time.Now().Add(-2*domain.Retention), "hearted"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := identity["ttl"]; ok {
		t.Fatal("permanent archive identity must have no ttl attribute")
	}
	db := &fakeDynamoDB{getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
		if got := input.Key["SK"].(*types.AttributeValueMemberS).Value; got != domain.ItemIdentitySK("hearted") {
			t.Fatalf("identity key = %q", got)
		}
		return &dynamodb.GetItemOutput{Item: identity}, nil
	}}
	exists, err := New(db, nil, "table", "", "").ItemExists(context.Background(), "user", "hearted")
	if err != nil || !exists {
		t.Fatalf("permanently hearted item must remain a duplicate: ItemExists = %v, %v", exists, err)
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
	gets = 0
	got, err = New(db, nil, "table", "", "").ItemByIdentity(context.Background(), "user", "same")
	if err != nil || got.SK != live.SK || string(got.Vector) != "vector" || gets != 3 {
		t.Fatalf("identity lookup = %#v, gets %d, %v", got, gets, err)
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
	stored, _ := attributevalue.MarshalMap(domain.ItemVector{PK: domain.UserPK("user"), SK: domain.ItemVectorSK("new"), Vector: []byte("separate"), ImageVector: []byte("image"), ImageModelVersion: "image-v1", TTL: ttl})
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
	if calls != 1 || string(items[0].Vector) != "separate" || string(items[0].ImageVector) != "image" || items[0].ImageModelVersion != "image-v1" || string(items[1].Vector) != "in-row" {
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

func TestItemMissingIdentityDoesNotScanLegacyLiveRow(t *testing.T) {
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
	if !errors.Is(err, ErrNotFound) || got.ItemID != "" || queries != 0 {
		t.Fatalf("Item = %#v, queries %d, %v", got, queries, err)
	}
}

func TestResolveItemIDsUsesIdentityRowsAndPreservesSemantics(t *testing.T) {
	now := time.Now()
	liveItems := []domain.Item{
		{PK: domain.UserPK("user"), SK: "I#live", ItemID: "live", TTL: now.Add(time.Hour).Unix()},
		{PK: domain.UserPK("user"), SK: "I#read", ItemID: "read", TTL: now.Add(time.Hour).Unix()},
		{PK: domain.UserPK("user"), SK: "I#archive", ItemID: "archive", ArchiveSK: domain.ArchiveSK(now, "archive"), TTL: now.Add(-time.Hour).Unix()},
	}
	identities := []domain.ItemIdentity{
		{PK: domain.UserPK("user"), SK: domain.ItemIdentitySK("archive"), ItemSK: "I#archive", TTL: now.Add(-time.Hour).Unix()},
		{PK: domain.UserPK("user"), SK: domain.ItemIdentitySK("live"), ItemSK: "I#live", TTL: liveItems[0].TTL},
		{PK: domain.UserPK("user"), SK: domain.ItemIdentitySK("read"), ItemSK: "I#read", TTL: liveItems[1].TTL},
		{PK: domain.UserPK("user"), SK: domain.ItemIdentitySK("missing"), ItemSK: "I#missing", TTL: now.Add(-time.Hour).Unix()},
	}
	archive := domain.Item{PK: domain.UserPK("user"), SK: domain.ArchiveSK(now, "archive"), ItemID: "archive", Read: true}
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
			if aws.ToBool(request.ConsistentRead) {
				t.Fatal("render lookup must use eventual consistency")
			}
			prefix := request.Keys[0]["SK"].(*types.AttributeValueMemberS).Value[:2]
			var rows []map[string]types.AttributeValue
			switch prefix {
			case "D#":
				rows = marshalList(identities)
			case "I#":
				rows = marshalList(liveItems)
			case "A#":
				if len(request.Keys) != 1 || request.Keys[0]["SK"].(*types.AttributeValueMemberS).Value != archive.SK {
					t.Fatalf("archive keys = %#v", request.Keys)
				}
				rows = marshalList([]domain.Item{archive})
			case "R#":
				rows = []map[string]types.AttributeValue{{"SK": &types.AttributeValueMemberS{Value: domain.ReadSK("read")}}}
			default:
				t.Fatalf("unexpected batch keys: %#v", request.Keys)
			}
			return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": rows}}, nil
		},
		query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			queries++
			t.Fatalf("resolution must not query partitions: %#v", input)
			return nil, nil
		},
	}
	got, err := New(db, nil, "table", "", "").ResolveItemIDs(context.Background(), "user", []string{"archive", "live", "archive", "read", "missing", "no-identity"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ItemID != "archive" || got[1].ItemID != "live" || got[2].ItemID != "read" {
		t.Fatalf("resolved items = %#v", got)
	}
	if !got[0].Archived || !got[0].Hearted || got[0].ArchiveSK != archive.SK || got[0].Read {
		t.Fatalf("archive flags = %#v", got[0])
	}
	if got[1].Read || !got[2].Read || batchCalls != 4 || queries != 0 {
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

func TestReplayOverwritePreservesConcurrentHeartState(t *testing.T) {
	for _, archive := range []string{"", "A#concurrent"} {
		t.Run(archive, func(t *testing.T) {
			calls := 0
			db := &fakeDynamoDB{transactWrite: func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
				calls++
				update := input.TransactItems[0].Update
				if update == nil || aws.ToString(update.ConditionExpression) != "attribute_exists(PK)" {
					t.Fatal("replay must update an existing row")
				}
				for _, field := range update.ExpressionAttributeNames {
					if field == "archive_sk" || field == "hearted_ts" || field == "hearted" || field == "read" || field == "signal" {
						t.Fatalf("replay modifies user state %s", field)
					}
				}
				if update.ExpressionAttributeValues[":summary"].(*types.AttributeValueMemberS).Value != "new summary" {
					t.Fatal("summary not updated")
				}
				if !strings.Contains(aws.ToString(update.UpdateExpression), "#media_key") {
					t.Fatal("stale media not cleared")
				}
				vector := input.TransactItems[1].Put.Item
				if string(vector["vector"].(*types.AttributeValueMemberB).Value) != "text" || string(vector["image_vector"].(*types.AttributeValueMemberB).Value) != "image" {
					t.Fatal("split embeddings not updated")
				}
				return &dynamodb.TransactWriteItemsOutput{}, nil
			}}
			item := domain.Item{PK: "U#user", SK: "I#item", ItemID: "item", Summary: "new summary", ArchiveSK: archive, Vector: []byte("text"), ImageVector: []byte("image")}
			for range 2 {
				if err := New(db, nil, "table", "", "").OverwriteItem(context.Background(), item); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 2 {
				t.Fatal(calls)
			}
		})
	}
}

func TestRecomputeModelPreservesSizeCutoffs(t *testing.T) {
	previous := domain.Model{
		PK: "U#user", SK: "MODEL", ComputedAt: "2026-09-18T09:01:00Z",
		SizeCutoffs:    &domain.SizeCutoffs{P60: 0.3, P90: 0.6},
		TagSizeCutoffs: map[string]*domain.SizeCutoffs{"tech": {P60: 0.4, P90: 0.7}},
	}
	row, err := attributevalue.MarshalMap(previous)
	if err != nil {
		t.Fatal(err)
	}
	db := &fakeDynamoDB{
		getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: row}, nil
		},
		putItem: func(input *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
			row = input.Item
			return &dynamodb.PutItemOutput{}, nil
		},
	}
	repository := New(db, nil, "table", "", "")
	rebuilt, err := repository.RecomputeModel(context.Background(), "user", "v1", "image-v1")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Model(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	for name, model := range map[string]domain.Model{"returned": rebuilt, "stored": stored} {
		if !reflect.DeepEqual(model.SizeCutoffs, previous.SizeCutoffs) || !reflect.DeepEqual(model.TagSizeCutoffs, previous.TagSizeCutoffs) {
			t.Errorf("%s model lost size cutoffs: global=%+v tags=%+v", name, model.SizeCutoffs, model.TagSizeCutoffs)
		}
	}
}

func TestUpdateItemRankingsRetriesThrottling(t *testing.T) {
	for _, throttle := range []error{
		&types.ProvisionedThroughputExceededException{},
		&smithy.GenericAPIError{Code: "ThrottlingException", Message: "slow down"},
	} {
		t.Run(fmt.Sprintf("%T", throttle), func(t *testing.T) {
			var mu sync.Mutex
			attempts := map[string]int{}
			written := map[string]bool{}
			db := &fakeDynamoDB{updateItem: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
				mu.Lock()
				defer mu.Unlock()
				id := input.Key["SK"].(*types.AttributeValueMemberS).Value
				attempts[id]++
				if attempts[id] <= 2 {
					return nil, fmt.Errorf("SDK retries exhausted: %w", throttle)
				}
				written[id] = true
				return &dynamodb.UpdateItemOutput{}, nil
			}}
			repository := New(db, nil, "table", "", "")
			repository.sleep = func(ctx context.Context, delay time.Duration) error {
				if delay < time.Second || delay > 4*time.Second {
					t.Errorf("retry delay = %s, want seconds of backoff", delay)
				}
				return ctx.Err()
			}
			items := make([]domain.Item, 40)
			for i := range items {
				items[i] = domain.Item{PK: "U#user", SK: fmt.Sprintf("I#%d", i), Score: 0.7, Size: "L"}
			}
			if err := repository.UpdateItemRankings(context.Background(), items); err != nil {
				t.Fatal(err)
			}
			if len(written) != len(items) {
				t.Fatalf("wrote %d of %d items", len(written), len(items))
			}
		})
	}
}

func TestUpdateItemRankingsStopsRetrying(t *testing.T) {
	for _, test := range []struct {
		name      string
		err       error
		wantCalls int
	}{
		{name: "non-throttling error", err: errors.New("access denied"), wantCalls: 1},
		{name: "persistent throttling", err: &types.ProvisionedThroughputExceededException{}, wantCalls: 4},
		{name: "deleted item", err: &types.ConditionalCheckFailedException{}, wantCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls, sleeps := 0, 0
			db := &fakeDynamoDB{updateItem: func(*dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
				calls++
				return nil, test.err
			}}
			repository := New(db, nil, "table", "", "")
			repository.sleep = func(_ context.Context, delay time.Duration) error {
				minimum := time.Second << sleeps
				if delay < minimum || delay >= 2*minimum {
					t.Errorf("delay %s outside [%s, %s)", delay, minimum, 2*minimum)
				}
				sleeps++
				return nil
			}
			err := repository.UpdateItemRankings(context.Background(), []domain.Item{{PK: "U#user", SK: "I#item"}})
			if test.name == "deleted item" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, test.err) {
				t.Fatalf("error = %v, want %v", err, test.err)
			}
			if calls != test.wantCalls || sleeps != test.wantCalls-1 {
				t.Fatalf("calls=%d sleeps=%d, want %d calls and %d sleeps", calls, sleeps, test.wantCalls, test.wantCalls-1)
			}
		})
	}
}

func TestUpdateItemRankingsCancelsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	db := &fakeDynamoDB{updateItem: func(*dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
		calls++
		cancel()
		return nil, &types.ProvisionedThroughputExceededException{}
	}}
	err := New(db, nil, "table", "", "").UpdateItemRankings(ctx, []domain.Item{{PK: "U#user", SK: "I#item"}})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("error=%v calls=%d, want cancellation after one call", err, calls)
	}
}

func TestUpdateItemRankingsPreservesLegacyInlineVector(t *testing.T) {
	for _, test := range []struct {
		name string
		why  *domain.Why
		want string
	}{
		{name: "removes stale explanation", want: "SET #score = :score, #size = :size REMOVE #why"},
		{name: "sets explanation", why: &domain.Why{Title: "Related"}, want: "SET #score = :score, #size = :size, #why = :why"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var update *dynamodb.UpdateItemInput
			db := &fakeDynamoDB{
				batchWrite: func(*dynamodb.BatchWriteItemInput) (*dynamodb.BatchWriteItemOutput, error) {
					t.Fatal("ranking update replaced the complete item")
					return nil, nil
				},
				updateItem: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
					update = input
					return &dynamodb.UpdateItemOutput{}, nil
				},
			}
			item := domain.Item{
				PK: "U#user", SK: "I#legacy", ItemID: "legacy", Score: 0.8, Size: "L", Why: test.why,
				Vector: []byte("legacy-inline-vector"), ArchiveSK: "A#kept", SearchText: "keep me",
			}
			if err := New(db, nil, "table", "", "").UpdateItemRankings(context.Background(), []domain.Item{item}); err != nil {
				t.Fatal(err)
			}
			if update == nil || aws.ToString(update.UpdateExpression) != test.want {
				t.Fatalf("ranking update = %#v, want %q", update, test.want)
			}
			if aws.ToString(update.ConditionExpression) != "attribute_exists(PK)" {
				t.Fatalf("ranking condition = %q", aws.ToString(update.ConditionExpression))
			}
			if _, exists := update.ExpressionAttributeNames["#vector"]; exists || len(update.ExpressionAttributeNames) != 3 {
				t.Fatalf("ranking names include non-derived fields: %#v", update.ExpressionAttributeNames)
			}
			if string(item.Vector) != "legacy-inline-vector" || item.ArchiveSK != "A#kept" || item.SearchText != "keep me" {
				t.Fatalf("input item was mutated: %#v", item)
			}
		})
	}
}

func TestPutItemVectorIfAbsentIsConditionalAndIdempotent(t *testing.T) {
	var put *dynamodb.PutItemInput
	db := &fakeDynamoDB{putItem: func(input *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
		put = input
		return &dynamodb.PutItemOutput{}, nil
	}}
	repository := New(db, nil, "table", "", "")
	written, err := repository.PutItemVectorIfAbsent(context.Background(), "user", "item", []byte("vector"), 42)
	if err != nil || !written {
		t.Fatalf("put vector = written %v, err %v", written, err)
	}
	if got := put.Item["PK"].(*types.AttributeValueMemberS).Value; got != "U#user" {
		t.Fatalf("vector PK = %q", got)
	}
	if got := put.Item["SK"].(*types.AttributeValueMemberS).Value; got != domain.ItemVectorSK("item") {
		t.Fatalf("vector SK = %q", got)
	}
	if got := string(put.Item["vector"].(*types.AttributeValueMemberB).Value); got != "vector" {
		t.Fatalf("vector data = %q", got)
	}
	if aws.ToString(put.ConditionExpression) != "attribute_not_exists(PK) OR #ttl <= :now" {
		t.Fatalf("vector condition = %q", aws.ToString(put.ConditionExpression))
	}

	db.putItem = func(*dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
		return nil, &types.ConditionalCheckFailedException{}
	}
	written, err = repository.PutItemVectorIfAbsent(context.Background(), "user", "item", []byte("vector"), 42)
	if err != nil || written {
		t.Fatalf("idempotent put = written %v, err %v", written, err)
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

func TestSignalValuesChunksAtDynamoBatchLimit(t *testing.T) {
	calls := 0
	db := &fakeDynamoDB{batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
		calls++
		request := input.RequestItems["table"]
		if len(request.Keys) == 0 || len(request.Keys) > 100 {
			t.Fatalf("signal batch size = %d", len(request.Keys))
		}
		rows := make([]map[string]types.AttributeValue, 0, len(request.Keys))
		for _, itemKey := range request.Keys {
			rows = append(rows, map[string]types.AttributeValue{
				"SK": itemKey["SK"], "value": &types.AttributeValueMemberN{Value: "1"},
			})
		}
		return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": rows}}, nil
	}}
	itemIDs := make([]string, 205)
	for index := range itemIDs {
		itemIDs[index] = fmt.Sprintf("item-%03d", index)
	}
	values, err := New(db, nil, "table", "", "").SignalValues(context.Background(), "user", itemIDs)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(values) != len(itemIDs) {
		t.Fatalf("calls = %d, values = %d", calls, len(values))
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
	item := domain.Item{ItemID: "item", FeedID: "feed", Title: "Title", Vector: []byte{1, 2}, ModelVersion: "v", ImageVector: []byte{3, 4}, ImageModelVersion: "image-v1"}
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
		"image_vector = if_not_exists(image_vector, :image_vector)",
		"image_model_version = if_not_exists(image_model_version, :image_version)",
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
	item := domain.Item{ItemID: "item", FeedID: "feed", Title: "title", Vector: []byte{1}, ImageVector: []byte{2}, ImageModelVersion: "image-v1"}
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
	if string(signal["image_vector"].(*types.AttributeValueMemberB).Value) != string(item.ImageVector) || signal["image_model_version"].(*types.AttributeValueMemberS).Value != item.ImageModelVersion {
		t.Fatalf("restored signal image embedding = %#v", signal)
	}
}

func TestSetItemImageVectorUpdatesExistingSplitRowOnly(t *testing.T) {
	var update *dynamodb.UpdateItemInput
	db := &fakeDynamoDB{updateItem: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
		update = input
		return nil, &types.ConditionalCheckFailedException{}
	}}
	if err := New(db, nil, "table", "", "").SetItemImageVector(context.Background(), "user", "item", []byte("image"), "image-v1"); err != nil {
		t.Fatal(err)
	}
	if got := update.Key["SK"].(*types.AttributeValueMemberS).Value; got != domain.ItemVectorSK("item") {
		t.Fatalf("image vector key = %q", got)
	}
	if aws.ToString(update.UpdateExpression) != "SET image_vector = :vector, image_model_version = :version" || aws.ToString(update.ConditionExpression) != "attribute_exists(PK)" {
		t.Fatalf("image vector update = %#v", update)
	}
}

func TestSetHeartCountsOnlyCreatedSignal(t *testing.T) {
	for _, test := range []struct {
		name     string
		fallback bool
		source   string
		value    int
	}{
		{name: "creates heart signal"},
		{name: "keeps existing signal", fallback: true, value: 1},
		{name: "overwrites bury signal", fallback: true, value: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := domain.Item{
				PK: "U#user", SK: domain.ItemSK(time.Now(), "item"), ItemID: "item", FeedID: "feed", Title: "TITLE", Summary: "Summary", TTL: time.Now().Add(time.Hour).Unix(),
				Vector: score.EncodeVector([]float32{1, 0}), ModelVersion: "text-v1", ImageVector: score.EncodeVector([]float32{1, 0}), ImageModelVersion: "image-v1",
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
			storedVector, err := attributevalue.MarshalMap(domain.ItemVector{PK: item.PK, SK: domain.ItemVectorSK(item.ItemID), Vector: item.Vector, ImageVector: item.ImageVector, ImageModelVersion: item.ImageModelVersion, TTL: item.TTL})
			if err != nil {
				t.Fatal(err)
			}
			old := domain.Signal{PK: item.PK, SK: domain.SignalSK(item.ItemID), ItemID: item.ItemID, Value: test.value, FeedID: "old-feed", CreatedAt: domain.Timestamp(time.Now()), Vector: score.EncodeVector([]float32{0, 1}), ModelVersion: "text-v1", ImageVector: score.EncodeVector([]float32{0, 1}), ImageModelVersion: "image-v1"}
			var seeded []domain.Signal
			var storedSignal map[string]types.AttributeValue
			if test.value != 0 {
				seeded = append(seeded, old)
				storedSignal, _ = attributevalue.MarshalMap(old)
			}
			model := score.BuildModel("user", seeded, nil, time.Now(), "text-v1", "image-v1")
			encodedModel, _ := attributevalue.MarshalMap(model)
			var updatedModel *domain.Model

			var transactions []*dynamodb.TransactWriteItemsInput
			db := &fakeDynamoDB{
				getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
					switch input.Key["SK"].(*types.AttributeValueMemberS).Value {
					case domain.SignalSK(item.ItemID):
						return &dynamodb.GetItemOutput{Item: storedSignal}, nil
					case "MODEL":
						return &dynamodb.GetItemOutput{Item: encodedModel}, nil
					case domain.BehaviourSK(item.ItemID):
						return &dynamodb.GetItemOutput{}, nil
					case domain.ItemIdentitySK(item.ItemID):
						return &dynamodb.GetItemOutput{Item: identity}, nil
					case item.SK:
						return &dynamodb.GetItemOutput{Item: encodedItem}, nil
					case domain.ItemVectorSK(item.ItemID):
						return &dynamodb.GetItemOutput{Item: storedVector}, nil
					default:
						return &dynamodb.GetItemOutput{Item: profile}, nil
					}
				},
				putItem: func(input *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
					var saved domain.Model
					if err := attributevalue.UnmarshalMap(input.Item, &saved); err != nil {
						t.Fatal(err)
					}
					updatedModel = &saved
					return &dynamodb.PutItemOutput{}, nil
				},
				transactWrite: func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
					transactions = append(transactions, input)
					if test.fallback && len(transactions) == 1 {
						return nil, &types.TransactionCanceledException{}
					}
					for _, write := range input.TransactItems {
						if write.Put != nil && write.Put.Item["SK"].(*types.AttributeValueMemberS).Value == old.SK {
							storedSignal = write.Put.Item
						}
					}
					return &dynamodb.TransactWriteItemsOutput{}, nil
				},
			}
			if _, _, err := New(db, nil, "table", "", "").SetHeart(context.Background(), "user", "item", true); err != nil {
				t.Fatal(err)
			}
			var saved domain.Signal
			if err := attributevalue.UnmarshalMap(storedSignal, &saved); err != nil {
				t.Fatal(err)
			}
			if saved.Value != 1 {
				t.Fatalf("signal = %#v", saved)
			}
			if test.value == 1 {
				if !reflect.DeepEqual(saved, old) || updatedModel != nil {
					t.Fatalf("existing boost changed: %#v, model %#v", saved, updatedModel)
				}
			} else {
				if saved.Source != "heart" || updatedModel == nil {
					t.Fatalf("heart signal/model = %#v, %#v", saved, updatedModel)
				}
				if updatedModel.ExplicitCount != 1 || updatedModel.LikedCount != 1 || updatedModel.DislikedCount != 0 || updatedModel.DislikedImageCount != 0 || updatedModel.FeedDislikes[old.FeedID] != 0 {
					t.Fatalf("old bury contribution was not removed: %#v", updatedModel)
				}
			}

			if len(transactions) != 1+boolInt(test.fallback) {
				t.Fatalf("transactions = %d", len(transactions))
			}
			if got := profileUpdateExpression(transactions[0]); got != "ADD heart_count :one, signal_count :one" {
				t.Fatalf("signal transaction profile update = %q", got)
			}
			archiveSearch := ""
			var signalImage []byte
			for _, write := range transactions[0].TransactItems {
				if write.Put == nil {
					continue
				}
				if value, ok := write.Put.Item["SK"].(*types.AttributeValueMemberS); ok && strings.HasPrefix(value.Value, "A#") {
					archiveSearch = write.Put.Item["search_text"].(*types.AttributeValueMemberS).Value
				} else if ok && strings.HasPrefix(value.Value, "S#") {
					signalImage = write.Put.Item["image_vector"].(*types.AttributeValueMemberB).Value
				}
			}
			for _, transaction := range transactions {
				found := false
				for _, write := range transaction.TransactItems {
					if write.Put == nil || write.Put.Item["SK"].(*types.AttributeValueMemberS).Value != domain.ItemIdentitySK(item.ItemID) {
						continue
					}
					found = true
					var identity domain.ItemIdentity
					if err := attributevalue.UnmarshalMap(write.Put.Item, &identity); err != nil {
						t.Fatal(err)
					}
					if _, ok := write.Put.Item["ttl"]; ok {
						t.Fatal("archive identity must have no ttl attribute")
					}
					if !strings.HasPrefix(identity.ItemSK, "A#") || identity.LiveSK != item.SK || identity.LiveTTL != item.TTL {
						t.Fatalf("archive identity = %#v", identity)
					}
				}
				if !found {
					t.Fatal("heart transaction did not write a permanent identity")
				}
			}
			if archiveSearch != "title summary" {
				t.Fatalf("archive search_text = %q", archiveSearch)
			}
			if !test.fallback && !reflect.DeepEqual(signalImage, item.ImageVector) {
				t.Fatalf("heart signal image vector = %q", signalImage)
			}
			if test.fallback {
				if got := profileUpdateExpression(transactions[1]); got != "ADD heart_count :one" {
					t.Fatalf("fallback profile update = %q", got)
				}
			}
		})
	}
}

func TestUnkeepRemovesAnySignal(t *testing.T) {
	for _, test := range []struct {
		name       string
		missing    bool
		concurrent string
		source     string
	}{
		{name: "deletes keep signal", source: "heart"},
		{name: "deletes prior boost"},
		{name: "missing signal", missing: true},
		{name: "concurrently removed signal", concurrent: "removed"},
		{name: "concurrently replaced signal", concurrent: "replaced"},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := domain.Item{PK: "U#user", SK: domain.ItemSK(time.Now(), "item"), ItemID: "item", ArchiveSK: "A#item", TTL: time.Now().Add(time.Hour).Unix()}
			archive := item
			archive.SK = item.ArchiveSK
			encodedItem, _ := attributevalue.MarshalMap(item)
			encodedArchive, _ := attributevalue.MarshalMap(archive)
			identity, _ := attributevalue.MarshalMap(domain.ItemIdentity{PK: item.PK, SK: domain.ItemIdentitySK(item.ItemID), ItemSK: item.SK, TTL: item.TTL})
			profile, _ := attributevalue.MarshalMap(domain.User{PK: "U#user", SK: "PROFILE"})

			old := domain.Signal{PK: item.PK, SK: domain.SignalSK("item"), ItemID: "item", Value: 1,
				Source: test.source, FeedID: "feed", CreatedAt: domain.Timestamp(time.Now().Add(-time.Hour)),
				Vector: score.EncodeVector([]float32{1, 0}), ModelVersion: "text-v1"}
			storedSignal, _ := attributevalue.MarshalMap(old)
			model := score.BuildModel("user", []domain.Signal{old}, nil, time.Now(), "text-v1", "")
			encodedModel, _ := attributevalue.MarshalMap(model)
			var updatedModel *domain.Model
			var transactions []*dynamodb.TransactWriteItemsInput
			db := &fakeDynamoDB{
				getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
					switch input.Key["SK"].(*types.AttributeValueMemberS).Value {
					case domain.ItemIdentitySK(item.ItemID):
						return &dynamodb.GetItemOutput{Item: identity}, nil
					case item.SK:
						return &dynamodb.GetItemOutput{Item: encodedItem}, nil
					case domain.SignalSK("item"):
						if test.missing {
							return &dynamodb.GetItemOutput{}, nil
						}
						return &dynamodb.GetItemOutput{Item: storedSignal}, nil
					case "MODEL":
						return &dynamodb.GetItemOutput{Item: encodedModel}, nil
					case domain.BehaviourSK("item"):
						return &dynamodb.GetItemOutput{}, nil
					case "PROFILE":
						return &dynamodb.GetItemOutput{Item: profile}, nil
					default:
						return &dynamodb.GetItemOutput{Item: encodedArchive}, nil
					}
				},
				putItem: func(input *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
					var saved domain.Model
					if err := attributevalue.UnmarshalMap(input.Item, &saved); err != nil {
						t.Fatal(err)
					}
					updatedModel = &saved
					return &dynamodb.PutItemOutput{}, nil
				},
				transactWrite: func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
					transactions = append(transactions, input)

					for _, write := range input.TransactItems {
						if write.ConditionCheck != nil && write.ConditionCheck.Key["SK"].(*types.AttributeValueMemberS).Value == old.SK {
							if aws.ToString(write.ConditionCheck.ConditionExpression) != "attribute_not_exists(SK)" {
								t.Fatal("fallback could preserve a signal")
							}
							if test.concurrent == "replaced" {
								return nil, &types.TransactionCanceledException{}
							}
						}
						if write.Delete != nil && write.Delete.Key["SK"].(*types.AttributeValueMemberS).Value == old.SK {
							values := write.Delete.ExpressionAttributeValues
							if strings.Contains(aws.ToString(write.Delete.ConditionExpression), "#source = :heart") && test.source != "heart" {
								t.Fatal("unkeep preserved a prior boost")
							}
							if values[":created_at"] == nil || values[":created_at"].(*types.AttributeValueMemberS).Value != old.CreatedAt {
								t.Fatal("signal deletion did not match the model snapshot")
							}
						}
					}
					if test.concurrent != "" && len(transactions) == 1 {
						return nil, &types.TransactionCanceledException{}
					}

					return &dynamodb.TransactWriteItemsOutput{}, nil
				},
			}
			_, _, err := New(db, nil, "table", "", "").SetHeart(context.Background(), "user", "item", false)
			if (err != nil) != (test.concurrent == "replaced") {
				t.Fatalf("unkeep error = %v", err)
			}
			if !test.missing && test.concurrent == "" {
				if updatedModel == nil || updatedModel.ExplicitCount != 0 || updatedModel.LikedCount != 0 || updatedModel.FeedLikes["feed"] != 0 {
					t.Fatalf("prior boost remains in model: %#v", updatedModel)
				}
			} else if updatedModel != nil {
				t.Fatal("model changed without deleting a signal")
			}
			if len(transactions) != 1+boolInt(test.concurrent != "") {
				t.Fatalf("transactions = %d", len(transactions))
			}

			want := "ADD heart_count :minus_one, signal_count :minus_one"
			if test.missing {
				want = "ADD heart_count :minus_one"
			}
			if got := profileUpdateExpression(transactions[0]); got != want {
				t.Fatalf("profile update = %q, want %q", got, want)
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

func TestItemByIdentityDoesNotQueryMissingOrTerminalItems(t *testing.T) {
	for _, test := range []struct {
		name     string
		identity *domain.ItemIdentity
	}{
		{name: "fresh"},
		{name: "terminal", identity: &domain.ItemIdentity{PK: "U#user", SK: domain.ItemIdentitySK("item"), TTL: time.Now().Add(time.Hour).Unix()}},
		{name: "expired", identity: &domain.ItemIdentity{PK: "U#user", SK: domain.ItemIdentitySK("item"), ItemSK: "I#old", TTL: time.Now().Add(-time.Hour).Unix()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			reads := 0
			db := &fakeDynamoDB{
				getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
					reads++
					if reads != 1 || input.Key["SK"].(*types.AttributeValueMemberS).Value != domain.ItemIdentitySK("item") || !aws.ToBool(input.ConsistentRead) {
						t.Fatal("expected one consistent identity read")
					}
					output := &dynamodb.GetItemOutput{}
					if test.identity != nil {
						output.Item, _ = attributevalue.MarshalMap(*test.identity)
					}
					return output, nil
				},
				query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
					t.Fatal("ingestion queried the user partition")
					return nil, nil
				},
			}
			_, err := New(db, nil, "table", "", "").ItemByIdentity(context.Background(), "user", "item")
			if !errors.Is(err, ErrNotFound) || reads != 1 {
				t.Fatalf("reads=%d err=%v", reads, err)
			}
		})
	}
}

func TestOverwriteItemOnlyMapsMissingLiveRowToNotFound(t *testing.T) {
	for _, test := range []struct {
		name    string
		err     error
		missing bool
	}{
		{"deleted", &types.TransactionCanceledException{CancellationReasons: []types.CancellationReason{{Code: aws.String("ConditionalCheckFailed")}, {Code: aws.String("None")}}}, true},
		{"conflict", &types.TransactionCanceledException{CancellationReasons: []types.CancellationReason{{Code: aws.String("TransactionConflict")}, {Code: aws.String("None")}}}, false},
		{"mixed", &types.TransactionCanceledException{CancellationReasons: []types.CancellationReason{{Code: aws.String("ConditionalCheckFailed")}, {Code: aws.String("ProvisionedThroughputExceeded")}}}, false},
		{"unknown", &types.TransactionCanceledException{}, false},
		{"transport", errors.New("unavailable"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := &fakeDynamoDB{transactWrite: func(*dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
				return nil, test.err
			}}
			err := New(db, nil, "table", "", "").OverwriteItem(context.Background(), domain.Item{PK: "U#user", SK: "I#item", ItemID: "item"})
			if errors.Is(err, ErrNotFound) != test.missing || (!test.missing && err != test.err) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestItemsForFeedsFiltersFetchWindowBeforePagination(t *testing.T) {
	for _, order := range []domain.Order{domain.OrderChrono, domain.OrderInterest} {
		t.Run(string(order), func(t *testing.T) {
			from := time.Date(2026, 9, 7, 7, 0, 0, 0, time.UTC)
			window := domain.FetchWindow{From: from, Before: from.AddDate(0, 0, 1)}
			marshal := func(id, feed string, fetched time.Time) map[string]types.AttributeValue {
				row, err := attributevalue.MarshalMap(domain.Item{
					PK: domain.UserPK("user"), SK: domain.ItemSK(from, id), ItemID: id, FeedID: feed,
					FetchedTS: domain.Timestamp(fetched), PublishedTS: domain.Timestamp(from.AddDate(0, 0, -3)),
					Score: 1, TTL: time.Now().Add(time.Hour).Unix(),
				})
				if err != nil {
					t.Fatal(err)
				}
				return row
			}
			outside := marshal("outside", "feed", from.Add(-time.Second))
			first := marshal("first", "feed", from)
			second := marshal("second", "feed", window.Before.Add(-time.Second))
			pageCalls := 0
			db := &fakeDynamoDB{
				query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
					pageCalls++
					if pageCalls <= 6 {
						return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{outside}, LastEvaluatedKey: itemPageKey(outside, order)}, nil
					}
					if pageCalls == 7 {
						return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{
							marshal("wrong-feed", "other", from), first, second, marshal("next-day", "feed", window.Before),
						}}, nil
					}
					if !reflect.DeepEqual(input.ExclusiveStartKey, itemPageKey(first, order)) {
						t.Fatalf("unexpected continuation: %#v", input.ExclusiveStartKey)
					}
					return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{second, marshal("next-day", "feed", window.Before)}}, nil
				},
				batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
					return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{
						"table": {key(domain.UserPK("user"), domain.ReadSK("first"))},
					}}, nil
				},
			}
			repository := New(db, nil, "table", "", "")
			items, cursor, _, err := repository.ItemsForFeeds(context.Background(), "user", order, "", 1, true, false, map[string]bool{"feed": true}, nil, window, nil)
			if err != nil || pageCalls != 7 || len(items) != 1 || items[0].ItemID != "first" || !items[0].Read || cursor == "" {
				t.Fatalf("first page = %#v, cursor = %q, calls = %d, err = %v", items, cursor, pageCalls, err)
			}
			items, cursor, _, err = repository.ItemsForFeeds(context.Background(), "user", order, cursor, 2, true, false, map[string]bool{"feed": true}, nil, window, nil)
			if err != nil || len(items) != 1 || items[0].ItemID != "second" || cursor != "" {
				t.Fatalf("second page = %#v, cursor = %q, err = %v", items, cursor, err)
			}
		})
	}
}

func TestTransactionConditionFailedForDeletedFeed(t *testing.T) {
	for _, code := range []string{"ConditionalCheckFailed", "TransactionConflict"} {
		err := &types.TransactionCanceledException{CancellationReasons: []types.CancellationReason{
			{Code: aws.String("None")}, {Code: aws.String("None")}, {Code: aws.String("None")}, {Code: aws.String(code)},
		}}
		if got := transactionConditionFailed(err); got != (code == "ConditionalCheckFailed") {
			t.Fatalf("%s: %v", code, got)
		}
	}
}

func TestSearchItemsScopedPageFill(t *testing.T) {
	for _, prefix := range []string{"I#", "A#"} {
		t.Run(prefix, func(t *testing.T) {
			calls := 0
			db := &fakeDynamoDB{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				calls++
				feed := "outside"
				if calls > 1 {
					feed = "inside"
				}
				row, err := attributevalue.MarshalMap(domain.Item{ItemID: strconv.Itoa(calls), FeedID: feed, SK: prefix + strconv.Itoa(calls)})
				if err != nil {
					t.Fatal(err)
				}
				return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{row}, LastEvaluatedKey: key("user", prefix+strconv.Itoa(calls))}, nil
			}}
			items, err := New(db, nil, "table", "", "").SearchItems(context.Background(), "user", prefix, []string{"topic"}, 2, map[string]bool{"inside": true})
			if err != nil || calls != 3 || len(items) != 2 || items[0].ItemID != "2" || items[1].ItemID != "3" {
				t.Fatalf("items = %#v, calls = %d, err = %v", items, calls, err)
			}
		})
	}
}

func TestInjectedReadMarkers(t *testing.T) {
	row, err := attributevalue.MarshalMap(domain.Item{PK: domain.UserPK("user"), SK: "I#item", ItemID: "item", FeedID: "feed"})
	if err != nil {
		t.Fatal(err)
	}
	db := &fakeDynamoDB{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		if prefix, ok := input.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS); ok && prefix.Value == "R#" {
			t.Fatal("injected source must replace marker queries")
		}
		return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{row}}, nil
	}}
	s := New(db, nil, "table", "", "")
	snapshot := map[string]bool{"item": true}
	items, _, anchor, err := s.ItemsForFeeds(context.Background(), "user", domain.OrderChrono, "", 100, false, false, nil, nil, domain.FetchWindow{}, snapshot)
	if err != nil || len(items) != 0 || anchor == nil || anchor.ItemID != "item" {
		t.Fatalf("items=%v anchor=%v err=%v", items, anchor, err)
	}
	counts, err := s.FeedItemCounts(context.Background(), "user", domain.FetchWindow{}, snapshot)
	if err != nil || counts["feed"].All != 1 || counts["feed"].Unread != 0 {
		t.Fatalf("counts=%v err=%v", counts, err)
	}
}

func TestSetSignalRejectsBuryOnKeptItem(t *testing.T) {
	for _, item := range []domain.Item{{ArchiveSK: "A#item"}, {Archived: true}} {
		if err := New(nil, nil, "table", "", "").SetSignal(context.Background(), "user", item, -1); err == nil {
			t.Fatal("bury accepted on kept item")
		}
	}
}

func TestReadExpiresWithLiveItem(t *testing.T) {
	expires := time.Now().Add(time.Hour).Unix()
	db := &fakeDynamoDB{batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
		rows := []map[string]types.AttributeValue{}
		for _, k := range input.RequestItems["table"].Keys {
			sk := k["SK"].(*types.AttributeValueMemberS).Value
			var value any
			switch sk {
			case "D#live":
				value = domain.ItemIdentity{SK: sk, ItemSK: "I#live", TTL: expires}
			case "I#live":
				value = domain.Item{SK: sk, ItemID: "live", TTL: expires, ArchiveSK: "A#kept"}
			default:
				continue
			}
			row, err := attributevalue.MarshalMap(value)
			if err != nil {
				t.Fatal(err)
			}
			rows = append(rows, row)
		}
		return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": rows}}, nil
	}}
	writes := 0
	db.batchWrite = func(input *dynamodb.BatchWriteItemInput) (*dynamodb.BatchWriteItemOutput, error) {
		for _, request := range input.RequestItems["table"] {
			var row domain.Read
			if err := attributevalue.UnmarshalMap(request.PutRequest.Item, &row); err != nil {
				t.Fatal(err)
			}
			if row.SK != "R#live" || row.TTL != expires {
				t.Fatalf("Read row = %+v", row)
			}
			writes++
		}
		return &dynamodb.BatchWriteItemOutput{}, nil
	}
	if err := New(db, nil, "table", "", "").SetRead(context.Background(), "user", []string{"live", "missing"}, true); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatalf("writes = %d", writes)
	}
}

func TestReadLegacyLiveItemWithoutIdentity(t *testing.T) {
	expires := time.Now().Add(time.Hour).Unix()
	queries, writes := 0, 0
	db := &fakeDynamoDB{
		batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
			if !aws.ToBool(input.RequestItems["table"].ConsistentRead) {
				t.Fatal("read writes require strong resolution")
			}
			return &dynamodb.BatchGetItemOutput{}, nil
		},
		query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			queries++
			if !aws.ToBool(input.ConsistentRead) {
				t.Fatal("legacy lookup must be consistent")
			}
			row, _ := attributevalue.MarshalMap(domain.Item{PK: "U#user", SK: "I#legacy", ItemID: "legacy", TTL: expires})
			expired, _ := attributevalue.MarshalMap(domain.Item{PK: "U#user", SK: "I#expired", ItemID: "expired", TTL: 1})
			return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{row, expired}}, nil
		},
		batchWrite: func(input *dynamodb.BatchWriteItemInput) (*dynamodb.BatchWriteItemOutput, error) {
			for _, request := range input.RequestItems["table"] {
				var row domain.Read
				if err := attributevalue.UnmarshalMap(request.PutRequest.Item, &row); err != nil {
					t.Fatal(err)
				}
				if row.SK != "R#legacy" || row.TTL != expires {
					t.Fatalf("Read = %+v", row)
				}
				writes++
			}
			return &dynamodb.BatchWriteItemOutput{}, nil
		},
	}
	repository := New(db, nil, "table", "", "")
	repository.legacyReadFallback = true
	if err := repository.SetRead(context.Background(), "user", []string{"legacy", "legacy", "expired", "missing"}, true); err != nil {
		t.Fatal(err)
	}
	if writes != 1 || queries != 1 {
		t.Fatalf("writes=%d queries=%d", writes, queries)
	}
}

func TestReadStaleIDsDoesNotQueryLivePartition(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint("legacy=", legacy), func(t *testing.T) {
			queries, writes := 0, 0
			db := &fakeDynamoDB{
				batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
					if !aws.ToBool(input.RequestItems["table"].ConsistentRead) {
						t.Fatal("resolution must be consistent")
					}
					rows := []map[string]types.AttributeValue{}
					for _, k := range input.RequestItems["table"].Keys {
						sk := k["SK"].(*types.AttributeValueMemberS).Value
						var identity domain.ItemIdentity
						switch sk {
						case "D#terminal":
							identity = domain.ItemIdentity{SK: sk, TTL: time.Now().Add(time.Hour).Unix()}
						case "D#expired":
							identity = domain.ItemIdentity{SK: sk, ItemSK: "I#expired", TTL: 1}
						default:
							continue
						}
						row, _ := attributevalue.MarshalMap(identity)
						rows = append(rows, row)
					}
					return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": rows}}, nil
				},
				query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
					queries++
					return &dynamodb.QueryOutput{}, nil
				},
				batchWrite: func(*dynamodb.BatchWriteItemInput) (*dynamodb.BatchWriteItemOutput, error) {
					writes++
					return &dynamodb.BatchWriteItemOutput{}, nil
				},
			}
			repository := New(db, nil, "table", "", "")
			repository.legacyReadFallback = legacy
			// Existing terminal/expired identities never need legacy recovery. With the
			// default configuration, absent (TTL-deleted) identities must not scan either.
			ids := []string{"terminal", "expired"}
			if !legacy {
				ids = append(ids, "ttl-deleted")
			}
			for attempt := 0; attempt < 3; attempt++ {
				if err := repository.SetRead(context.Background(), "user", ids, true); err != nil {
					t.Fatal(err)
				}
			}
			if queries != 0 || writes != 0 {
				t.Fatalf("queries=%d writes=%d", queries, writes)
			}
		})
	}
}

func TestLinkItemCountersAndBodyHistoryCommitTogether(t *testing.T) {
	for _, test := range []struct {
		name                  string
		link, body            bool
		media                 string
		wantFailure, wantLink string
	}{
		{"link omission", true, false, "", "0", "1"}, {"real failure", false, false, "", "1", "0"},
		{"link with body", true, true, "", "0", "0"}, {"video", false, false, "video", "0", "0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := &fakeDynamoDB{transactWrite: func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
				update := input.TransactItems[3].Update
				values := update.ExpressionAttributeValues
				for key, want := range map[string]string{":extraction_failure": test.wantFailure, ":link_item": test.wantLink, ":media_failure": "1", ":one": "1"} {
					if values[key].(*types.AttributeValueMemberN).Value != want {
						t.Fatalf("%s=%v want=%s", key, values[key], want)
					}
				}
				if values[":outcomes"].(*types.AttributeValueMemberS).Value != domain.AppendBodyOutcome(strings.Repeat("0", 50), test.body) || !strings.Contains(aws.ToString(update.ConditionExpression), "body_outcomes = :before") {
					t.Fatalf("update=%+v", update)
				}
				return &dynamodb.TransactWriteItemsOutput{}, nil
			}}
			item := putItemRetryFixture()
			item.LinkItem = test.link
			item.HasBody = test.body
			item.MediaType = test.media
			item.RecordBodyOutcome = true
			item.BodyOutcomesBefore = strings.Repeat("0", 50)
			written, err := New(db, nil, "table", "", "").PutItem(context.Background(), item)
			if err != nil || !written {
				t.Fatalf("written=%v err=%v", written, err)
			}
		})
	}
}

func TestBodyHistoryRefreshesAfterConcurrentItem(t *testing.T) {
	calls := 0
	db := &fakeDynamoDB{
		getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			if input.Key["PK"].(*types.AttributeValueMemberS).Value != "U#user" {
				t.Fatalf("key=%v", input.Key)
			}
			row, _ := attributevalue.MarshalMap(domain.Feed{PK: "U#user", FeedID: "feed", BodyOutcomes: "10"})
			return &dynamodb.GetItemOutput{Item: row}, nil
		},
		transactWrite: func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
			calls++
			if calls == 1 {
				return nil, transactionCanceled("None", "None", "None", "ConditionalCheckFailed")
			}
			update := input.TransactItems[3].Update
			if update.ExpressionAttributeValues[":outcomes"].(*types.AttributeValueMemberS).Value != "101" {
				t.Fatalf("lost concurrent outcome: %+v", update)
			}
			return &dynamodb.TransactWriteItemsOutput{}, nil
		},
	}
	item := putItemRetryFixture()
	item.RecordBodyOutcome = true
	item.BodyOutcomesBefore = "1"
	item.HasBody = true
	written, err := New(db, nil, "table", "", "").PutItem(context.Background(), item)
	if err != nil || !written || calls != 2 {
		t.Fatalf("written=%v calls=%d err=%v", written, calls, err)
	}
}

// The SDK's idempotency middleware pins ClientRequestToken on the input on
// the first call. DynamoDB rejects a reused token whose parameters changed,
// so a retry that swaps the counter update must clear the token.
func TestBodyHistoryRetryDoesNotReuseIdempotencyToken(t *testing.T) {
	calls := 0
	const pinned = "sdk-generated-token"
	db := &fakeDynamoDB{
		getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			row, _ := attributevalue.MarshalMap(domain.Feed{PK: "U#user", FeedID: "feed", BodyOutcomes: "10"})
			return &dynamodb.GetItemOutput{Item: row}, nil
		},
		transactWrite: func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
			calls++
			if calls == 1 {
				input.ClientRequestToken = aws.String(pinned)
				return nil, transactionCanceled("None", "None", "None", "ConditionalCheckFailed")
			}
			if aws.ToString(input.ClientRequestToken) == pinned {
				return nil, &types.IdempotentParameterMismatchException{Message: aws.String("Specified idempotent token was used with different request parameters within the idempotency window")}
			}
			return &dynamodb.TransactWriteItemsOutput{}, nil
		},
	}
	item := putItemRetryFixture()
	item.RecordBodyOutcome = true
	item.BodyOutcomesBefore = "1"
	item.HasBody = true
	written, err := New(db, nil, "table", "", "").PutItem(context.Background(), item)
	if err != nil || !written || calls != 2 {
		t.Fatalf("written=%v calls=%d err=%v", written, calls, err)
	}
}
