package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
	"github.com/nuntz/sema/internal/vectorstore"
)

type apiDynamo struct {
	*dynamodb.Client
	batchGet func(*dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error)
	getItem  func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error)
	query    func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error)
	update   func(*dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error)
}

func TestSimilarMatchesExcludeSelfWeakAndRespectLimit(t *testing.T) {
	got := similarMatches([]vectorstore.Match{
		{Key: "self", Similarity: 100},
		{Key: "weak", Similarity: 39},
		{Key: "one", Similarity: 91},
		{Key: "two", Similarity: 78},
	}, "self", 1)
	if len(got) != 1 || got[0].Key != "one" {
		t.Fatalf("similar matches = %#v", got)
	}
}

func TestResponseEncodesNilCollectionsAsArrays(t *testing.T) {
	got := response(http.StatusOK, struct {
		Items      []string       `json:"items"`
		Nested     map[string]any `json:"nested"`
		NextCursor *string        `json:"next_cursor"`
		Opaque     []byte         `json:"opaque"`
	}{Nested: map[string]any{"items": []int(nil)}})
	want := `{"items":[],"nested":{"items":[]},"next_cursor":null,"opaque":null}`
	if got.Body != want {
		t.Fatalf("response body = %s, want %s", got.Body, want)
	}
}

func TestGetSearchEncodesEmptyGroupsAsArrays(t *testing.T) {
	db := &apiDynamo{query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		return &dynamodb.QueryOutput{}, nil
	}}
	server := &server{store: store.New(db, nil, "table", "", "")}
	got := server.getSearch(context.Background(), "user", map[string]string{"q": "pulumi"})
	if got.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", got.StatusCode, got.Body)
	}
	var body struct {
		Matches struct {
			Window  json.RawMessage `json:"window"`
			Archive json.RawMessage `json:"archive"`
		} `json:"matches"`
		Related struct {
			Window  json.RawMessage `json:"window"`
			Archive json.RawMessage `json:"archive"`
		} `json:"related"`
	}
	if err := json.Unmarshal([]byte(got.Body), &body); err != nil {
		t.Fatal(err)
	}
	for name, encoded := range map[string]json.RawMessage{
		"matches.window": body.Matches.Window, "matches.archive": body.Matches.Archive,
		"related.window": body.Related.Window, "related.archive": body.Related.Archive,
	} {
		if string(encoded) != "[]" {
			t.Errorf("%s = %s, want []", name, encoded)
		}
	}
}

type apiQueue struct{ input *sqs.SendMessageBatchInput }

func (q *apiQueue) SendMessageBatch(_ context.Context, input *sqs.SendMessageBatchInput, _ ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
	q.input = input
	return &sqs.SendMessageBatchOutput{}, nil
}

func (f *apiDynamo) BatchGetItem(_ context.Context, input *dynamodb.BatchGetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error) {
	return f.batchGet(input)
}

func (f *apiDynamo) GetItem(_ context.Context, input *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return f.getItem(input)
}

func (f *apiDynamo) Query(_ context.Context, input *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	return f.query(input)
}

func (f *apiDynamo) UpdateItem(_ context.Context, input *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	return f.update(input)
}

func TestParseIncludeRead(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  bool
		err   bool
	}{
		{name: "default", value: "", want: false},
		{name: "unread only", value: "false", want: false},
		{name: "all items", value: "true", want: true},
		{name: "invalid", value: "yes", err: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseIncludeRead(test.value)
			if (err != nil) != test.err || got != test.want {
				t.Fatalf("parseIncludeRead(%q) = %v, %v", test.value, got, err)
			}
		})
	}
}

func TestNormalizeTagsAndFeedStatus(t *testing.T) {
	tags, err := normalizeTags([]string{" Dev ", "dev", "ニュース"})
	if err != nil || len(tags) != 2 || tags[0] != "dev" || tags[1] != "ニュース" {
		t.Fatalf("tags = %#v, err = %v", tags, err)
	}
	if got := feedStatus(domain.Feed{ErrorCount: 3}); got != "broken" {
		t.Fatalf("broken status = %q", got)
	}
	if got := feedStatus(domain.Feed{Muted: true, ErrorCount: 3}); got != "muted" {
		t.Fatalf("muted status = %q", got)
	}
}

func TestDecorateExtractionComputesPerFeedDistribution(t *testing.T) {
	feeds := []domain.Feed{{FeedID: "enough"}, {FeedID: "new"}}
	items := make([]domain.Item, 0, 14)
	for index := range 10 {
		items = append(items, domain.Item{FeedID: "enough", HasBody: index < 8, ExtractQuality: float64(index+1) / 10})
	}
	for range 4 {
		items = append(items, domain.Item{FeedID: "new", HasBody: true, ExtractQuality: 0.9})
	}
	decorateExtraction(feeds, items)
	if feeds[0].ExtractionSample != 10 || feeds[0].ExtractionRate == nil || *feeds[0].ExtractionRate != 0.8 || feeds[0].MedianQuality == nil || *feeds[0].MedianQuality != 0.55 {
		t.Fatalf("enough feed stats = %#v", feeds[0])
	}
	if feeds[1].ExtractionSample != 4 || feeds[1].ExtractionRate != nil || feeds[1].MedianQuality != nil {
		t.Fatalf("new feed stats = %#v", feeds[1])
	}
}

func TestPrepareItemsLoadsOnlyPageSignals(t *testing.T) {
	db := &apiDynamo{batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
		request := input.RequestItems["table"]
		if len(request.Keys) != 2 {
			t.Fatalf("signal keys = %#v", request.Keys)
		}
		return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": {
			{"SK": &types.AttributeValueMemberS{Value: "S#first"}, "value": &types.AttributeValueMemberN{Value: "1"}},
		}}}, nil
	}}
	server := &server{store: store.New(db, nil, "table", "", "/content")}
	items := []domain.Item{{ItemID: "first", Vector: []byte{1}}, {ItemID: "second", Vector: []byte{2}}}
	if err := server.prepareItems(context.Background(), "user", items); err != nil {
		t.Fatal(err)
	}
	if items[0].Signal != 1 || items[1].Signal != 0 || items[0].Vector != nil || items[1].Vector != nil {
		t.Fatalf("prepared items = %#v", items)
	}
}

func TestGetMeUsesProfileSignalCount(t *testing.T) {
	profile, err := attributevalue.MarshalMap(domain.User{
		PK: "U#user", SK: "PROFILE", Email: "reader@example.com", OrderPref: domain.OrderChrono, SignalCount: 12, HeartCount: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	db := &apiDynamo{
		getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: profile}, nil
		},
		batchGet: func(*dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
			t.Fatal("getMe unexpectedly loaded signals")
			return nil, nil
		},
	}
	response := (&server{store: store.New(db, nil, "table", "", "")}).getMe(context.Background(), "user")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, response.Body)
	}
	var body struct {
		SignalCount int         `json:"signal_count"`
		HeartCount  int         `json:"heart_count"`
		Profile     domain.User `json:"profile"`
	}
	if err := json.Unmarshal([]byte(response.Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.SignalCount != 12 || body.Profile.SignalCount != 12 || body.HeartCount != 3 {
		t.Fatalf("profile response = %#v", body)
	}
}

func TestBehaviourEventsValidateAndWriteMonotonicRow(t *testing.T) {
	item, err := attributevalue.MarshalMap(domain.Item{
		PK: "U#user", SK: domain.ItemSK(time.Now(), "item"), ItemID: "item", FeedID: "feed", Title: "Title",
		Vector: []byte{1, 2, 3}, TTL: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var updateInputs []*dynamodb.UpdateItemInput
	db := &apiDynamo{
		query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{item}}, nil
		},
		update: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			updateInputs = append(updateInputs, input)
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}
	server := &server{store: store.New(db, nil, "table", "", "")}
	response := server.itemRoute(context.Background(), "user", http.MethodPost, "item/events", `{"opened":true,"dwell_ms":31000,"clicked_through":true}`)
	if response.StatusCode != http.StatusOK || len(updateInputs) != 2 {
		t.Fatalf("events response = %d %s, updates %d", response.StatusCode, response.Body, len(updateInputs))
	}
	response = server.itemRoute(context.Background(), "user", http.MethodPost, "item/events", `{}`)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty event status = %d, body %s", response.StatusCode, response.Body)
	}
	response = server.itemRoute(context.Background(), "user", http.MethodPost, "item/events", `{"shared":true}`)
	if response.StatusCode != http.StatusOK || len(updateInputs) != 3 {
		t.Fatalf("share event response = %d %s, updates %d", response.StatusCode, response.Body, len(updateInputs))
	}
	shareUpdate := updateInputs[2]
	if shareUpdate.ExpressionAttributeNames["#shared"] != "shared" {
		t.Fatalf("share expression names = %#v", shareUpdate.ExpressionAttributeNames)
	}
	if value, ok := shareUpdate.ExpressionAttributeValues[":shared"].(*types.AttributeValueMemberBOOL); !ok || !value.Value {
		t.Fatalf("share expression values = %#v", shareUpdate.ExpressionAttributeValues)
	}
}

func TestRetryItemQueuesForcedExtractionAndSummary(t *testing.T) {
	item, err := attributevalue.MarshalMap(domain.Item{
		PK: "U#user", SK: domain.ItemSK(time.Now(), "item"), ItemID: "item", FeedID: "feed", URL: "https://example.com/story", Title: "Title", PublishedTS: "2026-08-20T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	db := &apiDynamo{query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{item}}, nil
	}}
	queue := &apiQueue{}
	server := &server{store: store.New(db, nil, "table", "", ""), queue: queue, itemsURL: "items-queue"}
	response := server.itemRoute(context.Background(), "user", http.MethodPost, "item/retry", "")
	if response.StatusCode != http.StatusAccepted || queue.input == nil || aws.ToString(queue.input.QueueUrl) != "items-queue" || len(queue.input.Entries) != 1 {
		t.Fatalf("response = %d %s, queue = %#v", response.StatusCode, response.Body, queue.input)
	}
	var message domain.ItemMessage
	if err := json.Unmarshal([]byte(aws.ToString(queue.input.Entries[0].MessageBody)), &message); err != nil {
		t.Fatal(err)
	}
	if !message.Reprocess || !message.ForceExtract || !message.ForceSummary || message.ItemID != "item" {
		t.Fatalf("retry message = %#v", message)
	}
}
