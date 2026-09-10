package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
)

func TestPermanentArchiveIdentityResolution(t *testing.T) {
	now := time.Now()
	for _, mode := range []string{"archive only", "expired live", "live wins", "deleted live"} {
		t.Run(mode, func(t *testing.T) {
			archiveSK := domain.ArchiveSK(now.Add(-30*24*time.Hour), "item")
			identity := domain.ItemIdentity{PK: "U#user", SK: "D#item", ItemSK: archiveSK}
			live := domain.Item{PK: "U#user", SK: "I#live", ItemID: "item", TTL: now.Add(time.Hour).Unix(), ArchiveSK: archiveSK}
			if mode != "archive only" {
				identity.LiveSK, identity.LiveTTL = live.SK, live.TTL
			}
			if mode == "expired live" {
				identity.LiveTTL = now.Add(-time.Hour).Unix()
			}
			values := map[string]any{
				identity.SK: identity,
				archiveSK:   domain.Item{PK: "U#user", SK: archiveSK, ItemID: "item"},
			}
			if mode == "live wins" {
				values[live.SK] = live
			}
			archiveReads, liveReads := 0, 0
			db := &fakeDynamoDB{
				query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
					t.Fatal("identity resolution must not query partitions")
					return nil, nil
				},
				batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
					request := input.RequestItems["table"]
					if aws.ToBool(request.ConsistentRead) {
						t.Fatal("render reads must be eventual")
					}
					rows := []map[string]types.AttributeValue{}
					for _, key := range request.Keys {
						sk := key["SK"].(*types.AttributeValueMemberS).Value
						if strings.HasPrefix(sk, "A#") {
							archiveReads++
						}
						if strings.HasPrefix(sk, "I#") {
							liveReads++
						}
						if value, ok := values[sk]; ok {
							row, err := attributevalue.MarshalMap(value)
							if err != nil {
								t.Fatal(err)
							}
							rows = append(rows, row)
						}
					}
					return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": rows}}, nil
				},
			}
			got, err := New(db, nil, "table", "", "").ResolveItemIDs(context.Background(), "user", []string{"item", "no-identity", "item"})
			if err != nil || len(got) != 1 {
				t.Fatalf("resolution = %#v, %v", got, err)
			}
			if mode == "live wins" {
				if got[0].SK != live.SK || got[0].Archived || archiveReads != 0 || liveReads != 1 {
					t.Fatalf("live resolution = %#v, archive reads = %d, live reads = %d", got, archiveReads, liveReads)
				}
			} else {
				if !got[0].Archived || !got[0].Hearted || got[0].ArchiveSK != archiveSK || got[0].Read || archiveReads != 1 {
					t.Fatalf("archive resolution = %#v, archive reads = %d", got, archiveReads)
				}
				if mode != "deleted live" && liveReads != 0 {
					t.Fatalf("unnecessary live reads = %d", liveReads)
				}
			}
		})
	}
}

func TestItemUsesPermanentIdentityLiveMetadata(t *testing.T) {
	now := time.Now()
	item := domain.Item{PK: "U#user", SK: "I#live", ItemID: "item", TTL: now.Add(time.Hour).Unix()}
	identity := archiveIdentity("user", domain.ArchiveSK(now, "item"), item)
	db := &fakeDynamoDB{getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
		if !aws.ToBool(input.ConsistentRead) {
			t.Fatal("Item must stay strong")
		}
		var value any
		switch input.Key["SK"].(*types.AttributeValueMemberS).Value {
		case "D#item":
			value = identity
		case item.SK:
			value = item
		default:
			return &dynamodb.GetItemOutput{}, nil
		}
		row, err := attributevalue.MarshalMap(value)
		return &dynamodb.GetItemOutput{Item: row}, err
	}}
	got, err := New(db, nil, "table", "", "").Item(context.Background(), "user", "item")
	if err != nil || got.SK != item.SK {
		t.Fatalf("Item = %#v, %v", got, err)
	}
}

func TestBackfillArchiveIdentity(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "live"}[existing], func(t *testing.T) {
			var stored map[string]types.AttributeValue
			if existing {
				stored, _ = attributevalue.MarshalMap(domain.ItemIdentity{PK: "U#user", SK: "D#item", ItemSK: "I#live", TTL: 42})
			}
			writes := 0
			db := &fakeDynamoDB{
				getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
					return &dynamodb.GetItemOutput{Item: stored}, nil
				},
				transactWrite: func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
					writes++
					if len(input.TransactItems) != 2 || input.TransactItems[0].ConditionCheck == nil {
						t.Fatal("backfill must check archive still exists")
					}
					put := input.TransactItems[1].Put
					if aws.ToString(put.ConditionExpression) == "" {
						t.Fatal("backfill must check previous identity")
					}
					if _, ok := put.Item["ttl"]; ok {
						t.Fatal("permanent identity has ttl")
					}
					var identity domain.ItemIdentity
					if err := attributevalue.UnmarshalMap(put.Item, &identity); err != nil {
						t.Fatal(err)
					}
					if identity.ItemSK != "A#archive" || (existing && (identity.LiveSK != "I#live" || identity.LiveTTL != 42)) {
						t.Fatalf("identity = %#v", identity)
					}
					stored = put.Item
					return &dynamodb.TransactWriteItemsOutput{}, nil
				},
			}
			repository := New(db, nil, "table", "", "")
			for range 2 {
				if err := repository.BackfillArchiveIdentity(context.Background(), "user", domain.Item{PK: "U#user", SK: "A#archive", ItemID: "item"}); err != nil {
					t.Fatal(err)
				}
			}
			if writes != 1 {
				t.Fatalf("not idempotent: writes = %d", writes)
			}
		})
	}
}

func TestBackfillArchiveIdentityConcurrentChangeAndFailure(t *testing.T) {
	for _, failure := range []error{
		&types.TransactionCanceledException{CancellationReasons: []types.CancellationReason{{Code: aws.String("ConditionalCheckFailed")}}},
		errors.New("unavailable"),
	} {
		db := &fakeDynamoDB{
			getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) { return &dynamodb.GetItemOutput{}, nil },
			transactWrite: func(*dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
				return nil, failure
			},
		}
		err := New(db, nil, "table", "", "").BackfillArchiveIdentity(context.Background(), "user", domain.Item{PK: "U#user", SK: "A#archive", ItemID: "item"})
		if transactionConditionFailed(failure) {
			if err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, failure) {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestUnheartPermanentIdentityLifecycle(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "expired", true: "live"}[active], func(t *testing.T) {
			item := domain.Item{PK: "U#user", SK: "I#live", ItemID: "item", TTL: time.Now().Add(time.Hour).Unix(), ArchiveSK: "A#archive"}
			identity := archiveIdentity("user", item.ArchiveSK, item)
			if !active {
				identity.LiveTTL = time.Now().Add(-time.Hour).Unix()
			}
			archive := domain.Item{PK: item.PK, SK: item.ArchiveSK, ItemID: item.ItemID}
			var transaction *dynamodb.TransactWriteItemsInput
			db := &fakeDynamoDB{
				getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
					var value any
					switch input.Key["SK"].(*types.AttributeValueMemberS).Value {
					case "D#item":
						value = identity
					case item.SK:
						value = item
					case archive.SK:
						value = archive
					case "PROFILE":
						value = domain.User{PK: "U#user", SK: "PROFILE"}
					default:
						return &dynamodb.GetItemOutput{}, nil
					}
					row, err := attributevalue.MarshalMap(value)
					return &dynamodb.GetItemOutput{Item: row}, err
				},
				query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
					t.Fatal("unheart should follow the archive identity")
					return nil, nil
				},
				transactWrite: func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
					transaction = input
					return &dynamodb.TransactWriteItemsOutput{}, nil
				},
			}
			// Avoid ranking-model reads by making the explicit-signal preservation
			// retry win, as happens when no heart-derived signal exists.
			calls := 0
			db.transactWrite = func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
				calls++
				if calls == 1 {
					return nil, &types.TransactionCanceledException{}
				}
				transaction = input
				return &dynamodb.TransactWriteItemsOutput{}, nil
			}
			_, _, err := New(db, nil, "table", "", "").SetHeart(context.Background(), "user", "item", false)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, write := range transaction.TransactItems {
				if active && write.Put != nil && write.Put.Item["SK"].(*types.AttributeValueMemberS).Value == "D#item" {
					found = true
					var restored domain.ItemIdentity
					if err := attributevalue.UnmarshalMap(write.Put.Item, &restored); err != nil {
						t.Fatal(err)
					}
					if restored.ItemSK != item.SK || restored.TTL != item.TTL || restored.LiveSK != "" {
						t.Fatalf("restored identity = %#v", restored)
					}
				}
				if !active && write.Delete != nil && write.Delete.Key["SK"].(*types.AttributeValueMemberS).Value == "D#item" {
					found = true
					if !strings.Contains(aws.ToString(write.Delete.ConditionExpression), "item_sk = :archive") {
						t.Fatal("unconditional identity deletion")
					}
				}
			}
			if !found {
				t.Fatal("unheart did not restore/remove permanent identity")
			}
		})
	}
}
