package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

type readMarkerDynamo struct {
	*apiDynamo
	writeErr error
}

func (d *readMarkerDynamo) BatchWriteItem(context.Context, *dynamodb.BatchWriteItemInput, ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	return &dynamodb.BatchWriteItemOutput{}, d.writeErr
}

func TestUnreadItemsCacheAndReadBatch(t *testing.T) {
	queries := 0
	row, err := attributevalue.MarshalMap(domain.Item{
		PK: domain.UserPK("user"), SK: "I#item", ItemID: "item", FeedID: "feed", TTL: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	db := &readMarkerDynamo{apiDynamo: &apiDynamo{
		query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			prefix, _ := input.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS)
			if prefix != nil {
				switch prefix.Value {
				case "R#":
					queries++
					return &dynamodb.QueryOutput{}, nil
				case "F#":
					return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{{"feed_id": &types.AttributeValueMemberS{Value: "feed"}}}}, nil
				case "I#":
					return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{row}}, nil
				}
			}
			return &dynamodb.QueryOutput{}, nil
		},
		batchGet: func(*dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
			return &dynamodb.BatchGetItemOutput{}, nil
		},
	}}
	s := &server{store: store.New(db, nil, "table", "", "")}
	ctx := context.Background()
	load := func(want int) {
		t.Helper()
		result := s.getItems(ctx, "user", map[string]string{"include_read": "false"})
		var page struct {
			Items []domain.Item `json:"items"`
		}
		if result.StatusCode != 200 {
			t.Fatalf("items: %s", result.Body)
		}
		if err := json.Unmarshal([]byte(result.Body), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != want {
			t.Fatalf("items = %#v, want %d", page.Items, want)
		}
	}
	load(1)
	load(1)
	if queries != 1 {
		t.Fatalf("marker queries = %d", queries)
	}
	snapshot, err := s.loadReadMarkers(ctx, "user")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		body string
		want int
	}{
		{`{"ids":["item"]}`, 0},
		{`{"ids":["item"],"read":false}`, 1},
	} {
		result := s.readBatch(ctx, "user", change.body)
		if result.StatusCode != 200 {
			t.Fatalf("read batch: %s", result.Body)
		}
		if snapshot["item"] {
			t.Fatal("mutation changed an existing request snapshot")
		}
		load(change.want)
		if queries != 1 {
			t.Fatalf("mutation reloaded markers: %d", queries)
		}
	}
	if snapshot["item"] {
		t.Fatal("mutation changed an existing request snapshot")
	}
	db.getItem = func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
		if input.Key["SK"].(*types.AttributeValueMemberS).Value == "D#item" {
			identity, err := attributevalue.MarshalMap(domain.ItemIdentity{PK: "U#user", SK: "D#item", ItemSK: "I#item", TTL: time.Now().Add(time.Hour).Unix()})
			return &dynamodb.GetItemOutput{Item: identity}, err
		}
		return &dynamodb.GetItemOutput{Item: row}, nil
	}
	if result := s.itemRoute(ctx, "user", "POST", "item/read", `{"read":true}`); result.StatusCode != 200 {
		t.Fatalf("item read: %s", result.Body)
	}
	load(0)
	if queries != 1 {
		t.Fatalf("per-item mutation reloaded markers: %d", queries)
	}

	db.writeErr = errors.New("write failed")
	if result := s.readBatch(ctx, "user", `{"ids":["item"]}`); result.StatusCode != 500 {
		t.Fatalf("failed mutation: %s", result.Body)
	}
	load(1)
	if queries != 2 {
		t.Fatalf("failed mutation did not invalidate cache: %d", queries)
	}
}

func TestReadMarkerCacheExpiryAndBound(t *testing.T) {
	calls := 0
	source := store.New(nil, nil, "table", "", "")
	source.ReadMarkers = func(context.Context, string) (map[string]bool, error) {
		calls++
		return map[string]bool{"read": true}, nil
	}
	s := &server{store: source}
	ctx := context.Background()
	for i := 0; i < 129; i++ {
		if _, err := s.loadReadMarkers(ctx, fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.readCache) != 128 {
		t.Fatalf("cache size = %d", len(s.readCache))
	}
	if _, ok := s.readCache["0"]; ok {
		t.Fatal("oldest user was not evicted")
	}
	entry := s.readCache["128"]
	entry.loaded = time.Now().Add(-readCacheTTL)
	s.readCache["128"] = entry
	if _, err := s.loadReadMarkers(ctx, "128"); err != nil {
		t.Fatal(err)
	}
	if calls != 130 {
		t.Fatalf("expired entry not reloaded: %d", calls)
	}
}

func TestRequestReadMarkerSnapshot(t *testing.T) {
	calls := 0
	source := store.New(nil, nil, "table", "", "")
	source.ReadMarkers = func(context.Context, string) (map[string]bool, error) {
		calls++
		return map[string]bool{"read": true}, nil
	}
	s := &server{store: source}
	requestStore := s.markerStore(context.Background())
	ctx := context.WithValue(context.Background(), readMarkerStoreKey{}, requestStore)
	first, err := requestStore.LoadReadMarkers(ctx, "user")
	if err != nil {
		t.Fatal(err)
	}
	delete(s.readCache, "user") // Even a cache eviction cannot change this request's snapshot.
	members := []domain.Item{{ItemID: "read"}, {ItemID: "unread"}}
	if err := s.markerStore(ctx).ResolveRead(ctx, "user", members); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !first["read"] || !members[0].Read || members[1].Read {
		t.Fatalf("calls=%d members=%v", calls, members)
	}
}
