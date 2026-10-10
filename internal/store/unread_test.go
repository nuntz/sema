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
	if err := New(db, nil, "table", "", "").SetRead(context.Background(), "user", readIDs(120), true); err != nil {
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
	if err := New(db, nil, "table", "", "").SetRead(context.Background(), "user", readIDs(3), true); err != nil {
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
	if err := repository.SetRead(context.Background(), "user", readIDs(2), false); err != nil {
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
	if err := repository.SetRead(context.Background(), "user", readIDs(1), true); err == nil || attempts != 1 {
		t.Fatalf("attempts = %d, err = %v", attempts, err)
	}
}
