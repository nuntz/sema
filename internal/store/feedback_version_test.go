package store

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
)

var errThrottled = errors.New("throttled")

// feedbackVersionFake records Signal writes and FEEDBACK_VERSION bumps in
// order. Every other read fails, and Signal writes fail when failWrites is
// set, so a call ends at its first store step past the write under test.
func feedbackVersionFake(failWrites, failBump bool) (*fakeDynamoDB, *[]string) {
	events := &[]string{}
	signalWrite := func() error {
		*events = append(*events, "write")
		if failWrites {
			return errThrottled
		}
		return nil
	}
	db := &fakeDynamoDB{
		putItem: func(*dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
			return &dynamodb.PutItemOutput{}, signalWrite()
		},
		deleteItem: func(*dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error) {
			return &dynamodb.DeleteItemOutput{}, signalWrite()
		},
		getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			return nil, errThrottled
		},
		updateItem: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			if input.Key["SK"].(*types.AttributeValueMemberS).Value != domain.FeedbackVersionSK {
				return nil, errThrottled
			}
			*events = append(*events, "bump")
			if failBump {
				return nil, errThrottled
			}
			return &dynamodb.UpdateItemOutput{Attributes: map[string]types.AttributeValue{
				"version": &types.AttributeValueMemberN{Value: "1"},
			}}, nil
		},
	}
	return db, events
}

// Boost, Bury, Keep and Unkeep each move FEEDBACK_VERSION once after their
// writes, so other API instances drop Stories that show the old feedback.
// A call that fails partway may have written, so it moves the version too;
// one that fails or is rejected before writing doesn't. The Keep and Unkeep
// tests in store_test.go cover a Keep that commits.
func TestFeedbackWritesBumpFeedbackVersion(t *testing.T) {
	item := domain.Item{ItemID: "item", FeedID: "feed", Vector: []byte{1}}
	for _, test := range []struct {
		name       string
		failWrites bool
		call       func(*Store) error
		want       []string
	}{
		{name: "boost", call: func(s *Store) error {
			return s.SetSignal(context.Background(), "user", item, 1)
		}, want: []string{"write", "bump"}},
		{name: "neutral signal", call: func(s *Store) error {
			return s.SetSignal(context.Background(), "user", item, 0)
		}, want: []string{"write", "bump"}},
		{name: "failed boost", failWrites: true, call: func(s *Store) error {
			return s.SetSignal(context.Background(), "user", item, 1)
		}, want: []string{"write", "bump"}},
		{name: "rejected bury on a kept item", call: func(s *Store) error {
			return s.SetSignal(context.Background(), "user", domain.Item{ItemID: "item", ArchiveSK: "A#item"}, -1)
		}, want: []string{}},
		{name: "keep failing before writing", call: func(s *Store) error {
			_, _, err := s.SetHeart(context.Background(), "user", "item", true)
			return err
		}, want: []string{}},
		{name: "unkeep failing before writing", call: func(s *Store) error {
			_, _, err := s.SetHeart(context.Background(), "user", "item", false)
			return err
		}, want: []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, events := feedbackVersionFake(test.failWrites, false)
			_ = test.call(New(db, nil, "table", "", ""))
			if !slices.Equal(*events, test.want) {
				t.Fatalf("events = %v, want %v", *events, test.want)
			}
		})
	}
}

// A Signal that committed but couldn't move the version reports the error,
// as SetRead does, so the client doesn't trust an unconfirmed write.
func TestSignalReportsAFailedFeedbackVersionBump(t *testing.T) {
	db, _ := feedbackVersionFake(false, true)
	err := New(db, nil, "table", "", "").SetSignal(context.Background(), "user", domain.Item{ItemID: "item", Vector: []byte{1}}, 0)
	if !errors.Is(err, errThrottled) {
		t.Fatalf("err = %v, want the bump error", err)
	}
}
