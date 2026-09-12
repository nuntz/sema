package store

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
	"testing"
	"time"
)

func TestPublishedWindowFiltersBeforePagination(t *testing.T) {
	for _, order := range []domain.Order{domain.OrderChrono, domain.OrderInterest} {
		t.Run(string(order), func(t *testing.T) {
			from := time.Now().UTC().Add(-domain.Retention)
			filter := domain.ItemFilter{Published: domain.FetchWindow{From: from, Before: from.Add(48 * time.Hour)}, Unkept: true, Ascending: true}
			row := func(id string, published time.Time, kept bool) map[string]types.AttributeValue {
				item := domain.Item{PK: domain.UserPK("user"), SK: domain.ItemSK(published, id), ItemID: id, FeedID: "feed", PublishedTS: domain.Timestamp(published), FetchedTS: domain.Timestamp(time.Now()), Score: 1}
				if kept {
					item.ArchiveSK = "A#kept"
				}
				result, err := attributevalue.MarshalMap(item)
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			first := row("first", from.Add(time.Hour), false)
			second := row("second", from.Add(2*time.Hour), false)
			calls := 0
			db := &fakeDynamoDB{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				if v, ok := input.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS); ok && v.Value == "R#" {
					return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{key(domain.UserPK("user"), domain.ReadSK("read"))}}, nil
				}
				calls++
				if order == domain.OrderChrono {
					if aws.ToString(input.KeyConditionExpression) != "PK = :pk AND SK BETWEEN :published_from AND :published_before" || !aws.ToBool(input.ScanIndexForward) {
						t.Fatalf("not an ascending key range: %#v", input)
					}
					if got := input.ExpressionAttributeValues[":published_before"].(*types.AttributeValueMemberS).Value; got != domain.ItemSK(filter.Published.Before, "") {
						t.Fatal(got)
					}
				}
				if calls == 1 {
					return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{row("old", from.Add(-time.Hour), false), row("kept", from.Add(time.Hour), true), row("read", from.Add(time.Hour), false)}, LastEvaluatedKey: itemPageKey(row("read", from.Add(time.Hour), false), order)}, nil
				}
				if calls == 2 {
					return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{first, second, row("at-end", filter.Published.Before, false)}}, nil
				}
				return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{second, row("at-end", filter.Published.Before, false)}}, nil
			}}
			store := New(db, nil, "table", "", "")
			items, cursor, _, err := store.ItemsForFeeds(context.Background(), "user", order, "", 1, false, false, nil, nil, domain.FetchWindow{}, filter)
			if err != nil || len(items) != 1 || items[0].ItemID != "first" || cursor == "" || calls != 2 {
				t.Fatalf("first: %+v %q %v", items, cursor, err)
			}
			items, cursor, _, err = store.ItemsForFeeds(context.Background(), "user", order, cursor, 1, false, false, nil, nil, domain.FetchWindow{}, filter)
			if err != nil || len(items) != 1 || items[0].ItemID != "second" {
				t.Fatalf("second: %+v %q %v", items, cursor, err)
			}
		})
	}
}

func TestPublishedCountsWalkAllPagesAndExcludeReadAndKept(t *testing.T) {
	now := time.Now().UTC()
	from := now.Add(-domain.Retention)
	filter := domain.ItemFilter{Published: domain.FetchWindow{From: from, Before: from.Add(48 * time.Hour)}, Unkept: true, TonightBefore: now.Add(12 * time.Hour)}
	calls := 0
	db := &fakeDynamoDB{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		if input.ExpressionAttributeValues[":prefix"].(*types.AttributeValueMemberS).Value == "R#" {
			return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{key(domain.UserPK("user"), domain.ReadSK("read"))}}, nil
		}
		calls++
		rows := []map[string]types.AttributeValue{}
		for _, item := range []domain.Item{{ItemID: "tonight", FeedID: "feed", PublishedTS: domain.Timestamp(from.Add(time.Hour))}, {ItemID: "tomorrow", FeedID: "feed", PublishedTS: domain.Timestamp(from.Add(24 * time.Hour))}, {ItemID: "kept", FeedID: "feed", PublishedTS: domain.Timestamp(from.Add(time.Hour)), ArchiveSK: "A#kept"}, {ItemID: "read", FeedID: "feed", PublishedTS: domain.Timestamp(from.Add(time.Hour))}, {ItemID: "fresh", FeedID: "feed", PublishedTS: domain.Timestamp(now)}} {
			r, _ := attributevalue.MarshalMap(item)
			rows = append(rows, r)
		}
		if calls == 1 {
			return &dynamodb.QueryOutput{Items: rows[:1], LastEvaluatedKey: key(domain.UserPK("user"), "I#next")}, nil
		}
		return &dynamodb.QueryOutput{Items: rows}, nil
	}}
	counts, err := New(db, nil, "table", "", "").FeedItemCounts(context.Background(), "user", domain.FetchWindow{}, filter)
	if err != nil || calls != 2 || counts["feed"].Unread != 2 || counts["feed"].Tonight != 1 {
		t.Fatalf("counts %+v: %v", counts, err)
	}
}
