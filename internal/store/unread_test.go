package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
)

// liveReadFake resolves every requested ID to a live I# row and records each
// SetRead transaction.
func liveReadFake(t *testing.T, transact func(*dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error)) *fakeDynamoDB {
	t.Helper()
	expires := time.Now().Add(time.Hour).Unix()
	return &fakeDynamoDB{
		batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
			rows := []map[string]types.AttributeValue{}
			for _, k := range input.RequestItems["table"].Keys {
				sk := k["SK"].(*types.AttributeValueMemberS).Value
				var value any
				switch {
				case strings.HasPrefix(sk, "D#"):
					id := strings.TrimPrefix(sk, "D#")
					value = domain.ItemIdentity{SK: sk, ItemSK: "I#" + id, TTL: expires}
				case strings.HasPrefix(sk, "I#"):
					value = domain.Item{PK: domain.UserPK("user"), SK: sk, ItemID: strings.TrimPrefix(sk, "I#"), TTL: expires}
				}
				row, err := attributevalue.MarshalMap(value)
				if err != nil {
					t.Fatal(err)
				}
				rows = append(rows, row)
			}
			return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": rows}}, nil
		},
		transactWrite: transact,
	}
}

func readIDs(count int) []string {
	ids := make([]string, count)
	for index := range ids {
		ids[index] = fmt.Sprintf("item-%03d", index)
	}
	return ids
}

func TestSetReadSplitsTransactionsAtActionLimit(t *testing.T) {
	sizes := []int{}
	db := liveReadFake(t, func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		sizes = append(sizes, len(input.TransactItems))
		return &dynamodb.TransactWriteItemsOutput{}, nil
	})
	if _, err := New(db, nil, "table", "", "").SetRead(context.Background(), "user", readIDs(120), true); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sizes) != "[100 100 40]" {
		t.Fatalf("transaction sizes = %v", sizes)
	}
}

func TestSetReadDropsItemWhoseLiveRowVanished(t *testing.T) {
	attempts := 0
	var committed []string
	db := liveReadFake(t, func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		attempts++
		reasons := make([]types.CancellationReason, len(input.TransactItems))
		failed := false
		for index, action := range input.TransactItems {
			reasons[index] = types.CancellationReason{Code: aws.String("None")}
			if action.Update != nil && action.Update.Key["SK"].(*types.AttributeValueMemberS).Value == "I#item-001" {
				reasons[index] = types.CancellationReason{Code: aws.String("ConditionalCheckFailed")}
				failed = true
			}
		}
		if failed {
			return nil, &types.TransactionCanceledException{CancellationReasons: reasons}
		}
		for _, action := range input.TransactItems {
			if action.Put != nil {
				committed = append(committed, action.Put.Item["SK"].(*types.AttributeValueMemberS).Value)
			}
		}
		return &dynamodb.TransactWriteItemsOutput{}, nil
	})
	if _, err := New(db, nil, "table", "", "").SetRead(context.Background(), "user", readIDs(3), true); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || fmt.Sprint(committed) != "[R#item-000 R#item-002]" {
		t.Fatalf("attempts = %d, committed = %v", attempts, committed)
	}
}

func TestSetReadRetriesTransactionConflict(t *testing.T) {
	attempts := 0
	db := liveReadFake(t, func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		attempts++
		if attempts == 1 {
			reasons := make([]types.CancellationReason, len(input.TransactItems))
			for index := range reasons {
				reasons[index] = types.CancellationReason{Code: aws.String("None")}
			}
			reasons[1] = types.CancellationReason{Code: aws.String("TransactionConflict")}
			return nil, &types.TransactionCanceledException{CancellationReasons: reasons}
		}
		if len(input.TransactItems) != 4 {
			t.Fatalf("retry dropped work: %d actions", len(input.TransactItems))
		}
		return &dynamodb.TransactWriteItemsOutput{}, nil
	})
	repository := New(db, nil, "table", "", "")
	repository.sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := repository.SetRead(context.Background(), "user", readIDs(2), false); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestSetReadReturnsNonConflictCancellation(t *testing.T) {
	attempts := 0
	db := liveReadFake(t, func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		attempts++
		reasons := make([]types.CancellationReason, len(input.TransactItems))
		for index := range reasons {
			reasons[index] = types.CancellationReason{Code: aws.String("ThrottlingError")}
		}
		return nil, &types.TransactionCanceledException{CancellationReasons: reasons}
	})
	repository := New(db, nil, "table", "", "")
	repository.sleep = func(context.Context, time.Duration) error {
		t.Fatal("backed off on a non-conflict cancellation")
		return nil
	}
	if _, err := repository.SetRead(context.Background(), "user", readIDs(1), true); err == nil || attempts != 1 {
		t.Fatalf("attempts = %d, err = %v", attempts, err)
	}
}

func TestSetReadBumpsReadVersionOnlyAfterCommit(t *testing.T) {
	var events []string
	failSecond := false
	chunks := 0
	db := liveReadFake(t, func(*dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		chunks++
		if failSecond && chunks == 2 {
			return nil, fmt.Errorf("throttled")
		}
		events = append(events, "commit")
		return &dynamodb.TransactWriteItemsOutput{}, nil
	})
	version := int64(0)
	db.updateItem = func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
		if sk := input.Key["SK"].(*types.AttributeValueMemberS).Value; sk != domain.ReadVersionSK {
			t.Fatalf("updated %s", sk)
		}
		events = append(events, "bump")
		version++
		return &dynamodb.UpdateItemOutput{Attributes: map[string]types.AttributeValue{
			"version": &types.AttributeValueMemberN{Value: fmt.Sprint(version)},
		}}, nil
	}
	repository := New(db, nil, "table", "", "")
	ctx := context.Background()

	got, err := repository.SetRead(ctx, "user", readIDs(2), true)
	if err != nil || got != 1 || fmt.Sprint(events) != "[commit bump]" {
		t.Fatalf("version = %d, err = %v, events = %v", got, err, events)
	}
	if got, err := repository.SetRead(ctx, "user", nil, true); err != nil || got != 0 || version != 1 {
		t.Fatalf("empty SetRead: version = %d, err = %v, bumps = %d", got, err, version)
	}
	// A failed later chunk still leaves earlier markers committed.
	events, chunks, failSecond = nil, 0, true
	if _, err := repository.SetRead(ctx, "user", readIDs(60), true); err == nil {
		t.Fatal("want the chunk error")
	}
	if fmt.Sprint(events) != "[commit bump]" {
		t.Fatalf("events = %v", events)
	}
}

func TestReadStateLoadsAreStronglyConsistent(t *testing.T) {
	db := &fakeDynamoDB{
		getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			if !aws.ToBool(input.ConsistentRead) {
				t.Fatal("ReadVersion read eventually consistent")
			}
			return &dynamodb.GetItemOutput{}, nil
		},
		query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			if !aws.ToBool(input.ConsistentRead) {
				t.Fatal("LoadReadMarkers read eventually consistent")
			}
			return &dynamodb.QueryOutput{}, nil
		},
	}
	repository := New(db, nil, "table", "", "")
	if version, err := repository.ReadVersion(context.Background(), "user"); err != nil || version != 0 {
		t.Fatalf("missing row: version = %d, err = %v", version, err)
	}
	if _, err := repository.LoadReadMarkers(context.Background(), "user"); err != nil {
		t.Fatal(err)
	}
}
