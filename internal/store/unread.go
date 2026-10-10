package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
)

// UnreadMembership compares the unread index with Read markers for one user.
// Missing rows are unread but unindexed; Surplus rows are Read but indexed.
// Repaired counts the rows an apply run changed.
type UnreadMembership struct {
	Live     int
	Missing  int
	Surplus  int
	Repaired int
}

// ReconcileUnreadMembership reports, and with apply repairs, live rows whose
// unread_pk disagrees with the R# marker. Each repair is conditioned on the
// marker in the same transaction, so a concurrent read or unread wins.
func (s *Store) ReconcileUnreadMembership(ctx context.Context, userID string, apply bool) (UnreadMembership, error) {
	live, err := s.LiveItems(ctx, userID)
	if err != nil {
		return UnreadMembership{}, err
	}
	markers, err := s.LoadReadMarkers(ctx, userID)
	if err != nil {
		return UnreadMembership{}, err
	}
	report := UnreadMembership{Live: len(live)}
	now := &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Unix(), 10)}
	for _, item := range live {
		read, indexed := markers[item.ItemID], item.UnreadPK != ""
		if read != indexed {
			continue
		}
		// Match LoadReadMarkers: an expired marker awaiting TTL deletion is unread.
		marker := "attribute_not_exists(SK) OR #ttl <= :now"
		if read {
			report.Surplus++
			marker = "attribute_exists(SK) AND #ttl > :now"
		} else {
			report.Missing++
		}
		if !apply {
			continue
		}
		err := s.transact(ctx, []types.TransactWriteItem{
			{ConditionCheck: &types.ConditionCheck{
				TableName: aws.String(s.table), Key: key(item.PK, domain.ReadSK(item.ItemID)), ConditionExpression: aws.String(marker),
				ExpressionAttributeNames: map[string]string{"#ttl": "ttl"}, ExpressionAttributeValues: map[string]types.AttributeValue{":now": now},
			}},
			{Update: s.unreadPKUpdate(item.PK, item.SK, !read)},
		})
		switch {
		case err == nil:
			report.Repaired++
		case !canceledBecause(err, "ConditionalCheckFailed"):
			return report, fmt.Errorf("reconcile %s: %w", item.ItemID, err)
		}
	}
	return report, nil
}

// unreadPKUpdate adds or removes a live row's unread index membership without
// recreating a row that has expired or been deleted.
func (s *Store) unreadPKUpdate(pk, sk string, unread bool) *types.Update {
	update := &types.Update{
		TableName: aws.String(s.table), Key: key(pk, sk),
		UpdateExpression: aws.String("REMOVE unread_pk"), ConditionExpression: aws.String("attribute_exists(PK)"),
	}
	if unread {
		update.UpdateExpression = aws.String("SET unread_pk = :pk")
		update.ExpressionAttributeValues = map[string]types.AttributeValue{":pk": &types.AttributeValueMemberS{Value: pk}}
	}
	return update
}

// canceledBecause reports whether a transaction was canceled with code as at
// least one action's reason.
func canceledBecause(err error, code string) bool {
	var canceled *types.TransactionCanceledException
	if !errors.As(err, &canceled) {
		return false
	}
	for _, reason := range canceled.CancellationReasons {
		if aws.ToString(reason.Code) == code {
			return true
		}
	}
	return false
}
