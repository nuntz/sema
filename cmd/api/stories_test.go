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

type pruneDynamo struct {
	*apiDynamo
	prune func(context.Context, *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error)
}

func (db *pruneDynamo) UpdateItem(ctx context.Context, input *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	return db.prune(ctx, input)
}

func TestStoryPruningCompletesBeforeRenderingReturns(t *testing.T) {
	now := time.Now()
	var expected string
	for _, outcome := range []string{"success", "error", "timeout"} {
		t.Run(outcome, func(t *testing.T) {
			marshal := func(value any) map[string]types.AttributeValue {
				row, err := attributevalue.MarshalMap(value)
				if err != nil {
					t.Fatal(err)
				}
				return row
			}
			// Resolved archives and out-of-window live items must not be pruned.
			rows := map[string]map[string]types.AttributeValue{
				"D#outside":                      marshal(domain.ItemIdentity{PK: "U#user", SK: "D#outside", ItemSK: "I#outside", TTL: now.Add(time.Hour).Unix()}),
				"I#outside":                      marshal(domain.Item{PK: "U#user", SK: "I#outside", ItemID: "outside", FetchedTS: domain.Timestamp(now.Add(-24 * time.Hour)), TTL: now.Add(time.Hour).Unix()}),
				"D#archive":                      marshal(domain.ItemIdentity{PK: "U#user", SK: "D#archive", ItemSK: domain.ArchiveSK(now, "archive")}),
				domain.ArchiveSK(now, "archive"): marshal(domain.Item{PK: "U#user", SK: domain.ArchiveSK(now, "archive"), ItemID: "archive"}),
			}
			for _, id := range []string{"lead", "member"} {
				rows["D#"+id] = marshal(domain.ItemIdentity{PK: "U#user", SK: "D#" + id, ItemSK: "I#" + id, TTL: now.Add(time.Hour).Unix()})
				rows["I#"+id] = marshal(domain.Item{PK: "U#user", SK: "I#" + id, ItemID: id, FeedID: id, FetchedTS: domain.Timestamp(now), PublishedTS: domain.Timestamp(now), TTL: now.Add(time.Hour).Unix()})
			}
			updates, completed := 0, false
			db := &pruneDynamo{
				apiDynamo: &apiDynamo{
					query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
						if input.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS).Value != "T#" {
							return &dynamodb.QueryOutput{}, nil
						}
						return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{
							marshal(domain.Story{StoryID: "first", MemberIDs: []string{"dead", "outside", "archive", "lead", "member"}}),
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
				},
				prune: func(ctx context.Context, input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
					updates++
					defer func() { completed = true }()
					dead := input.ExpressionAttributeValues[":dead"].(*types.AttributeValueMemberSS).Value
					if input.Key["SK"].(*types.AttributeValueMemberS).Value != "T#first" || len(dead) != 1 || dead[0] != "dead" {
						t.Fatalf("pruned filtered/resolved members: %#v", input)
					}
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 500*time.Millisecond {
						t.Fatal("prune must have a 500 ms deadline")
					}
					switch outcome {
					case "error":
						return nil, errors.New("cleanup failure")
					case "timeout":
						<-ctx.Done()
						return nil, ctx.Err()
					default:
						return &dynamodb.UpdateItemOutput{}, nil
					}
				},
			}
			s := &server{store: store.New(db, nil, "table", "", ""), feedCache: map[string]cachedFeedList{"user": {loaded: now}}}
			window := domain.FetchWindow{From: now.Add(-time.Hour), Before: now.Add(time.Hour)}
			started := time.Now()
			stories, hidden, err := s.loadAndRenderStories(context.Background(), "user", nil, true, domain.Model{}, "", window)
			if err != nil {
				t.Fatal(err)
			}
			if !completed || updates != 1 {
				t.Fatalf("prune did not finish before return: completed=%v updates=%d", completed, updates)
			}
			if time.Since(started) > 2*time.Second {
				t.Fatal("prune exceeded bounded request delay")
			}
			if len(stories) != 1 || len(stories[0].Items) != 2 || !hidden["lead"] || !hidden["member"] {
				t.Fatalf("render changed: stories=%#v hidden=%v", stories, hidden)
			}
			body := response(200, map[string]any{"stories": stories}).Body
			if outcome == "success" {
				expected = body
			} else if body != expected {
				t.Fatalf("prune failure changed response: %s", body)
			}
			if _, _, err := s.loadAndRenderStories(context.Background(), "user", nil, true, domain.Model{}, "", window); err != nil {
				t.Fatal(err)
			}
			if updates != 1 {
				t.Fatal("cache hit scheduled another prune")
			}
		})
	}
}
