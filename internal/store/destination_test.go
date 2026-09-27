package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func TestReserveSendCountsWithinTheHourAndResetsOnANewHour(t *testing.T) {
	now := time.Date(2026, 9, 26, 18, 4, 11, 0, time.UTC)
	for _, test := range []struct {
		name       string
		failures   int
		want       bool
		wantUpdate []string
	}{
		{name: "same hour under the limit", want: true, wantUpdate: []string{"SET send_count = send_count + :one"}},
		{name: "new hour starts a fresh window", failures: 1, want: true, wantUpdate: []string{"SET send_count = send_count + :one", "SET send_window = :window, send_count = :one"}},
		{name: "limit reached", failures: 2, want: false, wantUpdate: []string{"SET send_count = send_count + :one", "SET send_window = :window, send_count = :one"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var updates []string
			db := &fakeDynamoDB{updateItem: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
				updates = append(updates, aws.ToString(input.UpdateExpression))
				// The quota outlives the Destination, so removing and re-adding
				// one cannot reset it.
				if sk := input.Key["SK"].(*types.AttributeValueMemberS).Value; sk != "SEND_QUOTA" {
					t.Fatalf("quota row SK = %q", sk)
				}
				if strings.Contains(aws.ToString(input.ConditionExpression), "attribute_exists(PK)") {
					t.Fatalf("quota must not depend on an existing row: %s", aws.ToString(input.ConditionExpression))
				}
				if window := input.ExpressionAttributeValues[":window"].(*types.AttributeValueMemberS).Value; window != "2026-09-26T18" {
					t.Fatalf("window = %q", window)
				}
				if len(updates) <= test.failures {
					return nil, &types.ConditionalCheckFailedException{}
				}
				return &dynamodb.UpdateItemOutput{}, nil
			}}

			got, err := New(db, nil, "table", "", "").ReserveSend(context.Background(), "user", now, 60)

			if err != nil || got != test.want {
				t.Fatalf("ReserveSend = %v, %v; want %v", got, err, test.want)
			}
			if len(updates) != len(test.wantUpdate) {
				t.Fatalf("updates = %q, want %q", updates, test.wantUpdate)
			}
			for index := range updates {
				if updates[index] != test.wantUpdate[index] {
					t.Fatalf("updates = %q, want %q", updates, test.wantUpdate)
				}
			}
		})
	}
}

func TestDeleteDestinationKeepsTheSendQuota(t *testing.T) {
	var deleted []string
	db := &fakeDynamoDB{deleteItem: func(input *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error) {
		deleted = append(deleted, input.Key["SK"].(*types.AttributeValueMemberS).Value)
		return &dynamodb.DeleteItemOutput{}, nil
	}}

	if err := New(db, nil, "table", "", "").DeleteDestination(context.Background(), "user"); err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != "DESTINATION" {
		t.Fatalf("deleted = %v", deleted)
	}
}
