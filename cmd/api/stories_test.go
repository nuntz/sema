package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

func TestRenderedStoriesCache(t *testing.T) {
	calls := 0
	fail := false
	db := &apiDynamo{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		if prefix, ok := input.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS); !ok || prefix.Value != "T#" {
			return &dynamodb.QueryOutput{}, nil
		}
		calls++
		if fail {
			return nil, errors.New("unavailable")
		}
		return &dynamodb.QueryOutput{}, nil
	}}
	s := &server{store: store.New(db, nil, "table", "", "")}
	load := func(user string, allowed map[string]bool, unread bool, tag string, window domain.FetchWindow) {
		t.Helper()
		if _, _, err := s.loadAndRenderStories(context.Background(), user, allowed, unread, domain.Model{}, tag, window); err != nil {
			t.Fatal(err)
		}
	}
	load("user", map[string]bool{"a": true, "b": true}, true, "", domain.FetchWindow{})
	load("user", map[string]bool{"b": true, "a": true}, true, "", domain.FetchWindow{})
	if calls != 1 {
		t.Fatalf("two consecutive renders loaded store %d times", calls)
	}
	load("other", map[string]bool{"a": true, "b": true}, true, "", domain.FetchWindow{})
	load("user", map[string]bool{"a": true}, true, "", domain.FetchWindow{})
	load("user", map[string]bool{"a": true}, false, "", domain.FetchWindow{})
	load("user", map[string]bool{"a": true}, false, "tag", domain.FetchWindow{})
	load("user", map[string]bool{"a": true}, false, "tag", domain.FetchWindow{From: time.Now()})
	if calls != 6 {
		t.Fatalf("filters shared cached results: calls = %d", calls)
	}
	s.invalidateStories("user")
	load("other", map[string]bool{"a": true, "b": true}, true, "", domain.FetchWindow{})
	load("user", map[string]bool{"a": true, "b": true}, true, "", domain.FetchWindow{})
	if calls != 7 {
		t.Fatalf("invalidation calls = %d", calls)
	}
	for key, entry := range s.storyCache {
		entry.loaded = time.Now().Add(-storyCacheTTL)
		s.storyCache[key] = entry
	}
	load("user", map[string]bool{"a": true, "b": true}, true, "", domain.FetchWindow{})
	if calls != 8 {
		t.Fatalf("expired cache calls = %d", calls)
	}
	s.invalidateStories("user")
	fail = true
	if _, _, err := s.loadAndRenderStories(context.Background(), "user", nil, true, domain.Model{}, "", domain.FetchWindow{}); err == nil {
		t.Fatal("expected store error")
	}
	fail = false
	load("user", nil, true, "", domain.FetchWindow{})
	if calls != 10 {
		t.Fatalf("error was cached: calls = %d", calls)
	}
}

func TestStoryPruningIsBoundedAndDoesNotBlockRendering(t *testing.T) {
	now := time.Now()
	marshal := func(value any) map[string]types.AttributeValue {
		row, err := attributevalue.MarshalMap(value)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	// The expired archive pointer and out-of-window live item both resolve.
	// Neither should be pruned just because it is excluded from rendering.
	rows := map[string]map[string]types.AttributeValue{
		"D#outside":                      marshal(domain.ItemIdentity{PK: "U#user", SK: "D#outside", ItemSK: "I#outside", TTL: now.Add(time.Hour).Unix()}),
		"I#outside":                      marshal(domain.Item{PK: "U#user", SK: "I#outside", ItemID: "outside", FetchedTS: domain.Timestamp(now.Add(-24 * time.Hour)), TTL: now.Add(time.Hour).Unix()}),
		"D#archive":                      marshal(domain.ItemIdentity{PK: "U#user", SK: "D#archive", ItemSK: "I#archive", TTL: now.Add(-time.Hour).Unix()}),
		"I#archive":                      marshal(domain.Item{PK: "U#user", SK: "I#archive", ItemID: "archive", ArchiveSK: domain.ArchiveSK(now, "archive"), TTL: now.Add(-time.Hour).Unix()}),
		domain.ArchiveSK(now, "archive"): marshal(domain.Item{PK: "U#user", SK: domain.ArchiveSK(now, "archive"), ItemID: "archive"}),
	}
	updates := make(chan *dynamodb.UpdateItemInput, 2)
	release := make(chan struct{})
	defer close(release)
	db := &apiDynamo{
		query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			if input.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS).Value != "T#" {
				return &dynamodb.QueryOutput{}, nil
			}
			return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{
				marshal(domain.Story{StoryID: "first", MemberIDs: []string{"dead", "outside", "archive"}}),
				marshal(domain.Story{StoryID: "second", MemberIDs: []string{"also-dead"}}),
			}}, nil
		},
		batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
			result := []map[string]types.AttributeValue{}
			for _, key := range input.RequestItems["table"].Keys {
				if row, ok := rows[key["SK"].(*types.AttributeValueMemberS).Value]; ok {
					result = append(result, row)
				}
			}
			return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": result}}, nil
		},
		update: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			updates <- input
			<-release
			return nil, errors.New("best-effort cleanup failure")
		},
	}
	s := &server{store: store.New(db, nil, "table", "", ""), feedCache: map[string]cachedFeedList{"user": {loaded: now}}}
	result := make(chan error, 1)
	go func() {
		_, _, err := s.loadAndRenderStories(context.Background(), "user", nil, true, domain.Model{}, "", domain.FetchWindow{From: now.Add(-time.Hour), Before: now.Add(time.Hour)})
		result <- err
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("pruning blocked rendering")
	}
	select {
	case input := <-updates:
		dead := input.ExpressionAttributeValues[":dead"].(*types.AttributeValueMemberSS).Value
		if input.Key["SK"].(*types.AttributeValueMemberS).Value != "T#first" || len(dead) != 1 || dead[0] != "dead" {
			t.Fatalf("pruned filtered/resolved members: %#v", input)
		}
	case <-time.After(time.Second):
		t.Fatal("pruning was not scheduled")
	}
	// Another identical call is cached and must not schedule another prune.
	if _, _, err := s.loadAndRenderStories(context.Background(), "user", nil, true, domain.Model{}, "", domain.FetchWindow{From: now.Add(-time.Hour), Before: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updates:
		t.Fatal("more than one story pruned")
	default:
	}
}
