package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
)

func (s *Store) Destination(ctx context.Context, userID string) (domain.Destination, error) {
	response, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.DestinationSK), ConsistentRead: aws.Bool(true)})
	if err != nil {
		return domain.Destination{}, err
	}
	if len(response.Item) == 0 {
		return domain.Destination{}, ErrNotFound
	}
	var destination domain.Destination
	return destination, attributevalue.UnmarshalMap(response.Item, &destination)
}

// PutDestination writes the user-editable settings. The receiver user id is
// minted once and kept, and delivery bookkeeping is left untouched.
func (s *Store) PutDestination(ctx context.Context, userID string, destination domain.Destination) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                aws.String(s.table),
		Key:                      key(domain.UserPK(userID), domain.DestinationSK),
		UpdateExpression:         aws.String("SET #url = :url, #secret = :secret, #label = :label, #enabled = :enabled, #receiver = if_not_exists(#receiver, :receiver)"),
		ExpressionAttributeNames: map[string]string{"#url": "url", "#secret": "secret", "#label": "label", "#enabled": "enabled", "#receiver": "receiver_user_id"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":url":      &types.AttributeValueMemberS{Value: destination.URL},
			":secret":   &types.AttributeValueMemberS{Value: destination.Secret},
			":label":    &types.AttributeValueMemberS{Value: destination.Label},
			":enabled":  &types.AttributeValueMemberBOOL{Value: destination.Enabled},
			":receiver": &types.AttributeValueMemberS{Value: destination.ReceiverUserID},
		},
	})
	return err
}

func (s *Store) DeleteDestination(ctx context.Context, userID string) error {
	_, err := s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.DestinationSK)})
	return err
}

// RecordDelivery stores the latest result for display in Settings. A
// Destination removed mid-delivery stays removed.
func (s *Store) RecordDelivery(ctx context.Context, userID string, at time.Time, status string) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           aws.String(s.table),
		Key:                 key(domain.UserPK(userID), domain.DestinationSK),
		UpdateExpression:    aws.String("SET last_delivery_at = :at, last_status = :status"),
		ConditionExpression: aws.String("attribute_exists(PK)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":at":     &types.AttributeValueMemberS{Value: domain.Timestamp(at)},
			":status": &types.AttributeValueMemberS{Value: status},
		},
	})
	var conditional *types.ConditionalCheckFailedException
	if errors.As(err, &conditional) {
		return nil
	}
	return err
}

// ReserveSend counts one Send or ping against the user's hourly allowance,
// reporting false once limit is reached within the current UTC hour. The
// count lives in its own row so removing a Destination cannot reset it.
func (s *Store) ReserveSend(ctx context.Context, userID string, now time.Time, limit int) (bool, error) {
	window := now.UTC().Format("2006-01-02T15")
	values := map[string]types.AttributeValue{
		":window": &types.AttributeValueMemberS{Value: window},
		":one":    &types.AttributeValueMemberN{Value: "1"},
		":limit":  &types.AttributeValueMemberN{Value: strconv.Itoa(limit)},
	}
	attempts := []struct{ update, condition string }{
		{"SET send_count = send_count + :one", "send_window = :window AND send_count < :limit"},
		{"SET send_window = :window, send_count = :one", "(attribute_not_exists(send_window) OR send_window <> :window) AND :one <= :limit"},
	}
	for _, attempt := range attempts {
		_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
			TableName:                 aws.String(s.table),
			Key:                       key(domain.UserPK(userID), domain.SendQuotaSK),
			UpdateExpression:          aws.String(attempt.update),
			ConditionExpression:       aws.String(attempt.condition),
			ExpressionAttributeValues: values,
		})
		var conditional *types.ConditionalCheckFailedException
		if err == nil {
			return true, nil
		}
		if !errors.As(err, &conditional) {
			return false, err
		}
	}
	return false, nil
}

// SendCopyKey names a short-lived copy of a stored image handed to a Send
// receiver. It carries no user or item identifier; send/ expires after a day.
func SendCopyKey(id, extension string) string {
	return "send/" + id + extension
}

// CopyForSend copies a stored image to a neutral send/ key, reporting false
// when the source has already expired.
func (s *Store) CopyForSend(ctx context.Context, source, destination string) (bool, error) {
	return s.copyContent(ctx, source, destination)
}
