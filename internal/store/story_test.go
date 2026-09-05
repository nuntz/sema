package store

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
)

func TestStoryLifecycle(t *testing.T) {
	now := time.Now().UTC()
	want := domain.Story{
		PK: domain.UserPK("user"), SK: domain.StorySK("founder"), StoryID: "founder",
		MemberIDs: []string{"founder", "second"}, CreatedAt: domain.Timestamp(now), UpdatedAt: domain.Timestamp(now), TTL: now.Add(time.Hour).Unix(),
	}
	var stored map[string]types.AttributeValue
	var deleted map[string]types.AttributeValue
	db := &fakeDynamoDB{
		putItem: func(input *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
			stored = input.Item
			return &dynamodb.PutItemOutput{}, nil
		},
		getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			if !aws.ToBool(input.ConsistentRead) {
				t.Fatal("Story lookup must be consistent")
			}
			return &dynamodb.GetItemOutput{Item: stored}, nil
		},
		deleteItem: func(input *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error) {
			deleted = input.Key
			return &dynamodb.DeleteItemOutput{}, nil
		},
	}
	repository := New(db, nil, "table", "", "")
	if err := repository.PutStory(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := repository.Story(context.Background(), "user", "founder")
	if err != nil || got.StoryID != want.StoryID || len(got.MemberIDs) != 2 || got.TTL != want.TTL {
		t.Fatalf("Story = %#v, %v", got, err)
	}
	if err := repository.DeleteStory(context.Background(), "user", "founder"); err != nil {
		t.Fatal(err)
	}
	if deleted["SK"].(*types.AttributeValueMemberS).Value != domain.StorySK("founder") {
		t.Fatalf("deleted key = %#v", deleted)
	}
}

func TestStoriesQueriesLivePrefix(t *testing.T) {
	now := time.Now().UTC()
	encoded, err := attributevalue.MarshalMap(domain.Story{
		PK: domain.UserPK("user"), SK: domain.StorySK("story"), StoryID: "story", MemberIDs: []string{"a", "b"}, TTL: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	db := &fakeDynamoDB{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		if aws.ToString(input.KeyConditionExpression) != "PK = :pk AND begins_with(SK, :prefix)" || input.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS).Value != "T#" {
			t.Fatalf("query = %#v", input)
		}
		if aws.ToString(input.FilterExpression) != "#ttl > :now" {
			t.Fatalf("filter = %q", aws.ToString(input.FilterExpression))
		}
		return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{encoded}}, nil
	}}
	stories, err := New(db, nil, "table", "", "").Stories(context.Background(), "user")
	if err != nil || len(stories) != 1 || stories[0].StoryID != "story" {
		t.Fatalf("Stories = %#v, %v", stories, err)
	}
}

func TestAddStoryMemberKeepsLargestTTL(t *testing.T) {
	calls := 0
	db := &fakeDynamoDB{updateItem: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
		calls++
		if calls == 1 {
			if input.ExpressionAttributeValues[":ttl"].(*types.AttributeValueMemberN).Value != strconv.FormatInt(42, 10) {
				t.Fatalf("ttl = %#v", input.ExpressionAttributeValues)
			}
			return nil, &types.ConditionalCheckFailedException{}
		}
		if aws.ToString(input.UpdateExpression) != "ADD member_ids :member SET updated_at = :updated" {
			t.Fatalf("fallback update = %q", aws.ToString(input.UpdateExpression))
		}
		return &dynamodb.UpdateItemOutput{}, nil
	}}
	if err := New(db, nil, "table", "", "").AddStoryMember(context.Background(), "user", "story", "item", 42); err != nil || calls != 2 {
		t.Fatalf("AddStoryMember calls = %d, err = %v", calls, err)
	}
}

func TestSetItemStorySetsAndClearsAttribute(t *testing.T) {
	var updates []*dynamodb.UpdateItemInput
	db := &fakeDynamoDB{updateItem: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
		updates = append(updates, input)
		return &dynamodb.UpdateItemOutput{}, nil
	}}
	repository := New(db, nil, "table", "", "")
	item := domain.Item{PK: domain.UserPK("user"), SK: "I#row", ItemID: "item"}
	if err := repository.SetItemStory(context.Background(), item, "story"); err != nil {
		t.Fatal(err)
	}
	if err := repository.SetItemStory(context.Background(), item, ""); err != nil {
		t.Fatal(err)
	}
	if aws.ToString(updates[0].UpdateExpression) != "SET story_id = :story" || aws.ToString(updates[1].UpdateExpression) != "REMOVE story_id" {
		t.Fatalf("updates = %#v", updates)
	}
}

func TestItemsForFeedsExcludesStoryMembers(t *testing.T) {
	now := time.Now().UTC()
	rows := make([]map[string]types.AttributeValue, 0, 2)
	for _, id := range []string{"hidden", "visible"} {
		row, err := attributevalue.MarshalMap(domain.Item{PK: domain.UserPK("user"), SK: domain.ItemSK(now, id), ItemID: id, FeedID: "feed", TTL: now.Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	db := &fakeDynamoDB{query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		return &dynamodb.QueryOutput{Items: rows}, nil
	}}
	items, _, _, err := New(db, nil, "table", "", "").ItemsForFeeds(context.Background(), "user", domain.OrderChrono, "", 100, true, nil, map[string]bool{"hidden": true})
	if err != nil || len(items) != 1 || items[0].ItemID != "visible" {
		t.Fatalf("items = %#v, err = %v", items, err)
	}
}

func TestStoryNotFound(t *testing.T) {
	db := &fakeDynamoDB{getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) { return &dynamodb.GetItemOutput{}, nil }}
	_, err := New(db, nil, "table", "", "").Story(context.Background(), "user", "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Story error = %v", err)
	}
}
