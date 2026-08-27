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

type schedulerDynamo struct {
	*dynamodb.Client
	query  func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error)
	update func(*dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error)
}

func (d *schedulerDynamo) Query(_ context.Context, input *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	return d.query(input)
}

func (d *schedulerDynamo) UpdateItem(_ context.Context, input *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	return d.update(input)
}

func TestRunJoinsClaimErrors(t *testing.T) {
	first := errors.New("first claim failed")
	second := errors.New("second claim failed")
	feeds, err := attributevalue.MarshalList([]domain.Feed{
		{PK: domain.UserPK("user"), SK: domain.FeedSK("first"), FeedID: "first", NextFetchAt: domain.Timestamp(time.Now())},
		{PK: domain.UserPK("user"), SK: domain.FeedSK("second"), FeedID: "second", NextFetchAt: domain.Timestamp(time.Now())},
	})
	if err != nil {
		t.Fatal(err)
	}
	items := make([]map[string]types.AttributeValue, 0, len(feeds))
	for _, feed := range feeds {
		items = append(items, feed.(*types.AttributeValueMemberM).Value)
	}
	db := &schedulerDynamo{
		query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			return &dynamodb.QueryOutput{Items: items}, nil
		},
		update: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			if input.Key["SK"].(*types.AttributeValueMemberS).Value == domain.FeedSK("first") {
				return nil, first
			}
			return nil, second
		},
	}
	err = (&handler{store: store.New(db, nil, "table", "", "")}).run(context.Background())
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("run error = %v", err)
	}
}
