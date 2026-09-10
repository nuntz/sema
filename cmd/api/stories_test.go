package main

import (
	"context"
	"errors"
	"testing"
	"time"

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
