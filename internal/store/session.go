package store

import (
	"context"
	"errors"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const sessionSK = "SESSION"

// Session contains only the hashed session identifier and its server-side
// identity. The browser's bearer value is never persisted.
type Session struct {
	PK        string `dynamodbav:"PK"`
	SK        string `dynamodbav:"SK"`
	Subject   string `dynamodbav:"sub"`
	Email     string `dynamodbav:"email,omitempty"`
	CreatedAt int64  `dynamodbav:"created_at"`
	RenewedAt int64  `dynamodbav:"renewed_at"`
	ExpiresAt int64  `dynamodbav:"expires_at"`
	TTL       int64  `dynamodbav:"ttl"`
}

func sessionPK(hash string) string { return "SESSION#" + hash }

func (s *Store) PutSession(ctx context.Context, hash string, session Session) error {
	session.PK, session.SK = sessionPK(hash), sessionSK
	item, err := attributevalue.MarshalMap(session)
	if err != nil {
		return err
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.table), Item: item, ConditionExpression: aws.String("attribute_not_exists(PK)"),
	})
	return err
}

func (s *Store) Session(ctx context.Context, hash string) (Session, error) {
	result, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table), Key: key(sessionPK(hash), sessionSK), ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return Session{}, err
	}
	if len(result.Item) == 0 {
		return Session{}, ErrNotFound
	}
	var session Session
	return session, attributevalue.UnmarshalMap(result.Item, &session)
}

func (s *Store) RenewSession(ctx context.Context, hash string, renewedAt, expiresAt int64) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(sessionPK(hash), sessionSK),
		UpdateExpression:    aws.String("SET renewed_at = :renewed, expires_at = :expires, #ttl = :expires"),
		ConditionExpression: aws.String("attribute_exists(PK) AND #ttl > :renewed"),
		ExpressionAttributeNames: map[string]string{
			"#ttl": "ttl",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":renewed": &types.AttributeValueMemberN{Value: strconv.FormatInt(renewedAt, 10)},
			":expires": &types.AttributeValueMemberN{Value: strconv.FormatInt(expiresAt, 10)},
		},
	})
	if err == nil {
		return nil
	}
	var conditional *types.ConditionalCheckFailedException
	if errors.As(err, &conditional) {
		return ErrNotFound
	}
	return err
}

func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.table), Key: key(sessionPK(hash), sessionSK),
	})
	return err
}
