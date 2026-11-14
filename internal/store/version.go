package store

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
)

// Per-user versions (READ_VERSION, FEED_VERSION, FEEDBACK_VERSION) tell other
// API instances when to drop their cached copies; see ADRs 0005-0007. Every
// writer that moves one does so only through change, which holds the one
// rule they share.

// change runs write against a copy of the Store whose DynamoDB writes are
// recorded. If write attempted any, it then moves the user's version row sk
// once and returns the new version; otherwise it returns 0 and leaves the row
// alone. A write that failed may still have committed, so it moves the
// version too: a spare bump costs other instances one reload, a missing one
// serves them stale data until their caches expire. A failed bump fails the
// call, so the client retries rather than trusting an unannounced write; when
// both fail, the errors are joined.
//
// write must do all its work through the Store it is given: writes made
// through the outer Store go unrecorded. S3 writes don't count, because the
// versions guard only what DynamoDB rows show.
func (s *Store) change(ctx context.Context, userID, sk string, write func(*Store) error) (int64, error) {
	recorder := &writeRecorder{dynamoAPI: s.db}
	recorded := *s
	recorded.db = recorder
	err := write(&recorded)
	if !recorder.attempted.Load() {
		return 0, err
	}
	version, bumpErr := s.bumpVersion(ctx, userID, sk)
	if err != nil {
		return 0, errors.Join(err, bumpErr)
	}
	return version, bumpErr
}

// writeRecorder notes whether any DynamoDB write was attempted through it.
type writeRecorder struct {
	dynamoAPI
	attempted atomic.Bool
}

func (r *writeRecorder) BatchWriteItem(ctx context.Context, input *dynamodb.BatchWriteItemInput, options ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	r.attempted.Store(true)
	return r.dynamoAPI.BatchWriteItem(ctx, input, options...)
}

func (r *writeRecorder) DeleteItem(ctx context.Context, input *dynamodb.DeleteItemInput, options ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	r.attempted.Store(true)
	return r.dynamoAPI.DeleteItem(ctx, input, options...)
}

func (r *writeRecorder) PutItem(ctx context.Context, input *dynamodb.PutItemInput, options ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	r.attempted.Store(true)
	return r.dynamoAPI.PutItem(ctx, input, options...)
}

func (r *writeRecorder) UpdateItem(ctx context.Context, input *dynamodb.UpdateItemInput, options ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	r.attempted.Store(true)
	return r.dynamoAPI.UpdateItem(ctx, input, options...)
}

func (r *writeRecorder) TransactWriteItems(ctx context.Context, input *dynamodb.TransactWriteItemsInput, options ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	r.attempted.Store(true)
	return r.dynamoAPI.TransactWriteItems(ctx, input, options...)
}

// version reads a per-user change counter strongly consistent; a missing row
// reads as 0.
func (s *Store) version(ctx context.Context, userID, sk string) (int64, error) {
	response, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), sk),
		ProjectionExpression: aws.String("version"), ConsistentRead: aws.Bool(true),
	})
	if err != nil || response.Item == nil {
		return 0, err
	}
	var row struct {
		Version int64 `dynamodbav:"version"`
	}
	err = attributevalue.UnmarshalMap(response.Item, &row)
	return row.Version, err
}

// bumpVersion moves a version row by one and returns its new value. Call it
// only from change, so no writer re-derives when to bump.
func (s *Store) bumpVersion(ctx context.Context, userID, sk string) (int64, error) {
	response, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), sk),
		UpdateExpression:          aws.String("ADD version :one"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":one": &types.AttributeValueMemberN{Value: "1"}},
		ReturnValues:              types.ReturnValueUpdatedNew,
	})
	if err != nil {
		return 0, err
	}
	var row struct {
		Version int64 `dynamodbav:"version"`
	}
	err = attributevalue.UnmarshalMap(response.Attributes, &row)
	return row.Version, err
}
