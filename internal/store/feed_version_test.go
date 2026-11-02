package store

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
)

// feedVersionFake records Feed writes and FEED_VERSION bumps in order, and
// fails the Feed writes whose ID is listed in fail.
func feedVersionFake(t *testing.T, fail map[string]bool) (*fakeDynamoDB, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var events []string
	version := 0
	db := &fakeDynamoDB{
		updateItem: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			mu.Lock()
			defer mu.Unlock()
			sk := input.Key["SK"].(*types.AttributeValueMemberS).Value
			if sk == domain.FeedVersionSK {
				version++
				events = append(events, "bump")
				return &dynamodb.UpdateItemOutput{Attributes: map[string]types.AttributeValue{
					"version": &types.AttributeValueMemberN{Value: fmt.Sprint(version)},
				}}, nil
			}
			if fail[strings.TrimPrefix(sk, "F#")] {
				return nil, fmt.Errorf("throttled")
			}
			events = append(events, "write")
			return &dynamodb.UpdateItemOutput{}, nil
		},
		deleteItem: func(*dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, "delete")
			return &dynamodb.DeleteItemOutput{}, nil
		},
	}
	return db, func() []string {
		mu.Lock()
		defer mu.Unlock()
		recorded := events
		events = nil
		return recorded
	}
}

func userFeed(id string) domain.Feed {
	return domain.Feed{PK: domain.UserPK("user"), SK: domain.FeedSK(id), FeedID: id}
}

func TestSaveFeedsBumpsFeedVersionOnceAfterTheWrites(t *testing.T) {
	db, events := feedVersionFake(t, nil)
	repository := New(db, nil, "table", "", "")
	version, err := repository.SaveFeeds(context.Background(), "user", []domain.Feed{userFeed("a"), userFeed("b")})
	if err != nil || version != 1 || fmt.Sprint(events()) != "[write write bump]" {
		t.Fatalf("version = %d, err = %v", version, err)
	}
	if version, err := repository.SaveFeeds(context.Background(), "user", nil); err != nil || version != 0 || len(events()) != 0 {
		t.Fatalf("empty SaveFeeds: version = %d, err = %v", version, err)
	}
}

// An import that fails partway has still committed its other Feeds, so other
// instances must see them.
func TestSaveFeedsBumpsWhenSomeWritesFail(t *testing.T) {
	db, events := feedVersionFake(t, map[string]bool{"b": true})
	repository := New(db, nil, "table", "", "")
	if _, err := repository.SaveFeeds(context.Background(), "user", []domain.Feed{userFeed("a"), userFeed("b")}); err == nil {
		t.Fatal("want the write error")
	}
	if got := fmt.Sprint(events()); got != "[write bump]" {
		t.Fatalf("events = %s", got)
	}
	db, events = feedVersionFake(t, map[string]bool{"a": true})
	if _, err := New(db, nil, "table", "", "").SaveFeeds(context.Background(), "user", []domain.Feed{userFeed("a")}); err == nil {
		t.Fatal("want the write error")
	}
	if got := events(); len(got) != 0 {
		t.Fatalf("bumped with nothing committed: %v", got)
	}
}

func TestSaveFeedsRefusesAnotherUsersFeed(t *testing.T) {
	db, events := feedVersionFake(t, nil)
	other := userFeed("a")
	other.PK = domain.UserPK("other")
	if _, err := New(db, nil, "table", "", "").SaveFeeds(context.Background(), "user", []domain.Feed{userFeed("b"), other}); err == nil {
		t.Fatal("saved another user's Feed")
	}
	if got := events(); len(got) != 0 {
		t.Fatalf("events = %v", got)
	}
}

func TestDeleteFeedBumpsFeedVersionAfterTheDelete(t *testing.T) {
	db, events := feedVersionFake(t, nil)
	version, err := New(db, nil, "table", "", "").DeleteFeed(context.Background(), "user", "a")
	if err != nil || version != 1 || fmt.Sprint(events()) != "[delete bump]" {
		t.Fatalf("version = %d, err = %v", version, err)
	}
}

// A worker fetch must not move the version, or every instance's Feed cache
// would go stale after each fetch.
func TestPutFeedLeavesFeedVersionAlone(t *testing.T) {
	db, events := feedVersionFake(t, nil)
	if err := New(db, nil, "table", "", "").PutFeed(context.Background(), userFeed("a")); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(events()); got != "[write]" {
		t.Fatalf("events = %s", got)
	}
}

func TestFeedLoadsAreStronglyConsistent(t *testing.T) {
	db := &fakeDynamoDB{
		getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			if !aws.ToBool(input.ConsistentRead) {
				t.Fatal("FeedVersion read eventually consistent")
			}
			return &dynamodb.GetItemOutput{}, nil
		},
		query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			if !aws.ToBool(input.ConsistentRead) {
				t.Fatal("Feeds read eventually consistent")
			}
			return &dynamodb.QueryOutput{}, nil
		},
	}
	repository := New(db, nil, "table", "", "")
	if version, err := repository.FeedVersion(context.Background(), "user"); err != nil || version != 0 {
		t.Fatalf("missing row: version = %d, err = %v", version, err)
	}
	if _, err := repository.Feeds(context.Background(), "user"); err != nil {
		t.Fatal(err)
	}
}

// A bump that fails after the Feeds committed fails the call, so the client
// retries rather than trusting other instances to have seen the edit.
func TestFeedWritesReportAFailedBump(t *testing.T) {
	db, _ := feedVersionFake(t, map[string]bool{"b": true})
	write := db.updateItem
	db.updateItem = func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
		if input.Key["SK"].(*types.AttributeValueMemberS).Value == domain.FeedVersionSK {
			return nil, fmt.Errorf("bump failed")
		}
		return write(input)
	}
	repository := New(db, nil, "table", "", "")
	if _, err := repository.SaveFeeds(context.Background(), "user", []domain.Feed{userFeed("a")}); err == nil || !strings.Contains(err.Error(), "bump failed") {
		t.Fatalf("SaveFeeds err = %v", err)
	}
	if _, err := repository.SaveFeeds(context.Background(), "user", []domain.Feed{userFeed("a"), userFeed("b")}); err == nil || !strings.Contains(err.Error(), "bump failed") || !strings.Contains(err.Error(), "throttled") {
		t.Fatalf("partial SaveFeeds err = %v, want both causes", err)
	}
	if _, err := repository.DeleteFeed(context.Background(), "user", "a"); err == nil {
		t.Fatal("DeleteFeed hid the failed bump")
	}
}
