package store

import (
	"context"
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
	items, cursor, err := repository.ItemsForFeeds(context.Background(), "user", domain.OrderChrono, "", 2, true, map[string]bool{"dev": true})
	if err != nil || cursor != "" || calls != 2 || len(items) != 2 || items[0].FeedID != "dev" || items[1].FeedID != "dev" {
		t.Fatalf("items = %#v, cursor = %q, calls = %d, err = %v", items, cursor, calls, err)
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

func TestPutItemEnforcesStableIdentityAcrossPublishedTimestamps(t *testing.T) {
	calls := 0
	db := &fakeDynamoDB{transactWrite: func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		calls++
		if len(input.TransactItems) != 2 {
			t.Fatalf("transaction = %#v", input.TransactItems)
		}
		identity := input.TransactItems[0].Put.Item
		if got := identity["SK"].(*types.AttributeValueMemberS).Value; got != domain.ItemIdentitySK("same") {
			t.Fatalf("identity key = %q", got)
		}
		if calls == 2 {
			return nil, &types.TransactionCanceledException{CancellationReasons: []types.CancellationReason{
				{Code: aws.String("ConditionalCheckFailed")}, {Code: aws.String("None")},
			}}
		}
		return &dynamodb.TransactWriteItemsOutput{}, nil
	}}
	repository := New(db, nil, "table", "", "")
	first := domain.Item{PK: domain.UserPK("user"), SK: domain.ItemSK(time.Now(), "same"), ItemID: "same", TTL: time.Now().Add(time.Hour).Unix()}
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
	feed.Muted = true
	if err := repository.PutFeed(context.Background(), feed); err != nil {
		t.Fatal(err)
	}
	if _, indexed := put.Item["gsi1pk"]; indexed {
		t.Fatalf("muted feed retained sparse index: %#v", put.Item["gsi1pk"])
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
	db := &fakeDynamoDB{putItem: func(input *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
		if input.ConditionExpression != nil {
			t.Fatalf("replay overwrite retained dedupe condition %q", aws.ToString(input.ConditionExpression))
		}
		writes = append(writes, input.Item)
		return &dynamodb.PutItemOutput{}, nil
	}}
	repository := New(db, nil, "table", "", "")
	item := domain.Item{PK: "U#user", SK: "I#item", ItemID: "item", Score: 0.8, Size: "L"}
	if err := repository.OverwriteItem(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if err := repository.OverwriteItem(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 || writes[0]["score"].(*types.AttributeValueMemberN).Value != writes[1]["score"].(*types.AttributeValueMemberN).Value {
		t.Fatalf("double replay writes = %#v", writes)
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
