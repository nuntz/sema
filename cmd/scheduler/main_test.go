package main

import (
	"context"
	"errors"
	"testing"

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
	items := []map[string]types.AttributeValue{
		{"PK": &types.AttributeValueMemberS{Value: domain.UserPK("user")}, "SK": &types.AttributeValueMemberS{Value: domain.FeedSK("first")}},
		{"PK": &types.AttributeValueMemberS{Value: domain.UserPK("user")}, "SK": &types.AttributeValueMemberS{Value: domain.FeedSK("second")}},
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
	err := (&handler{store: store.New(db, nil, "table", "", "")}).run(context.Background())
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("run error = %v", err)
	}
}
