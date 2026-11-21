package main

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
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

func TestRunEmitsFailingConnectors(t *testing.T) {
	feed := func(id, connector string, errorCount int) map[string]types.AttributeValue {
		return map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: domain.UserPK("user")}, "SK": &types.AttributeValueMemberS{Value: domain.FeedSK(id)},
			"connector": &types.AttributeValueMemberS{Value: connector}, "error_count": &types.AttributeValueMemberN{Value: strconv.Itoa(errorCount)},
		}
	}
	cases := []struct {
		name      string
		feeds     []map[string]types.AttributeValue
		count     float64
		connector string
	}{
		{"one dead connector", []map[string]types.AttributeValue{feed("a", domain.ConnectorYouTube, 4), feed("b", domain.ConnectorYouTube, 2), feed("c", domain.ConnectorRSS, 0)}, 1, domain.ConnectorYouTube},
		{"all healthy", []map[string]types.AttributeValue{feed("a", domain.ConnectorYouTube, 1), feed("c", domain.ConnectorRSS, 0)}, 0, ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			db := &schedulerDynamo{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				if aws.ToString(input.IndexName) == "feed-status" {
					return &dynamodb.QueryOutput{Items: test.feeds}, nil
				}
				return &dynamodb.QueryOutput{}, nil
			}}
			var metrics map[string]float64
			var fields map[string]string
			run := &handler{store: store.New(db, nil, "table", "", ""), emit: func(m map[string]float64, f map[string]string) { metrics, fields = m, f }}
			if err := run.run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if metrics["ConnectorsFailing"] != test.count || fields["FailingConnectors"] != test.connector {
				t.Fatalf("emitted %v %v, want ConnectorsFailing %v naming %q", metrics, fields, test.count, test.connector)
			}
			if _, ok := metrics["FeedsEnqueued"]; !ok {
				t.Fatalf("emitted %v, want FeedsEnqueued alongside", metrics)
			}
		})
	}
}

func TestRunStillEnqueuesWhenFeedStatusesFails(t *testing.T) {
	db := &schedulerDynamo{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		if aws.ToString(input.IndexName) == "feed-status" {
			return nil, errors.New("index is still being created")
		}
		return &dynamodb.QueryOutput{}, nil
	}}
	var metrics map[string]float64
	run := &handler{store: store.New(db, nil, "table", "", ""), emit: func(m map[string]float64, _ map[string]string) { metrics = m }}
	if err := run.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := metrics["ConnectorsFailing"]; ok {
		t.Fatalf("emitted %v, want no ConnectorsFailing when Feed Status could not be read", metrics)
	}
	if _, ok := metrics["FeedsEnqueued"]; !ok {
		t.Fatalf("emitted %v, want FeedsEnqueued", metrics)
	}
}
