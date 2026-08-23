package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/nuntz/sema/internal/domain"
)

type dynamoAPI interface {
	BatchGetItem(context.Context, *dynamodb.BatchGetItemInput, ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error)
	BatchWriteItem(context.Context, *dynamodb.BatchWriteItemInput, ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
	DeleteItem(context.Context, *dynamodb.DeleteItemInput, ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	Query(context.Context, *dynamodb.QueryInput, ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	Scan(context.Context, *dynamodb.ScanInput, ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	UpdateItem(context.Context, *dynamodb.UpdateItemInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	TransactWriteItems(context.Context, *dynamodb.TransactWriteItemsInput, ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
}

type s3API interface {
	CopyObject(context.Context, *s3.CopyObjectInput, ...func(*s3.Options)) (*s3.CopyObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type Store struct {
	db         dynamoAPI
	s3         s3API
	table      string
	bucket     string
	contentURL string
}

func New(db dynamoAPI, objects s3API, table, bucket, contentURL string) *Store {
	return &Store{db: db, s3: objects, table: table, bucket: bucket, contentURL: strings.TrimRight(contentURL, "/")}
}

func (s *Store) EnsureUser(ctx context.Context, userID, email string) error {
	now := domain.Timestamp(time.Now())
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table),
		Key:       key(domain.UserPK(userID), "PROFILE"),
		UpdateExpression: aws.String("SET email = if_not_exists(email, :email), created_at = if_not_exists(created_at, :now), " +
			"order_pref = if_not_exists(order_pref, :order), heart_count = if_not_exists(heart_count, :zero) REMOVE #read_boundary"),
		ExpressionAttributeNames: map[string]string{"#read_boundary": "read_boundary_ts"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":email": &types.AttributeValueMemberS{Value: email},
			":now":   &types.AttributeValueMemberS{Value: now},
			":order": &types.AttributeValueMemberS{Value: string(domain.OrderChrono)},
			":zero":  &types.AttributeValueMemberN{Value: "0"},
		},
	})
	return err
}

func (s *Store) User(ctx context.Context, userID string) (domain.User, error) {
	response, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(s.table), Key: key(domain.UserPK(userID), "PROFILE"), ConsistentRead: aws.Bool(true)})
	if err != nil {
		return domain.User{}, err
	}
	if len(response.Item) == 0 {
		return domain.User{}, ErrNotFound
	}
	var user domain.User
	return user, attributevalue.UnmarshalMap(response.Item, &user)
}

func (s *Store) UpdateUser(ctx context.Context, userID string, order *domain.Order, position *string) error {
	sets := make([]string, 0, 2)
	values := make(map[string]types.AttributeValue)
	if order != nil {
		sets = append(sets, "order_pref = :order")
		values[":order"] = &types.AttributeValueMemberS{Value: string(*order)}
	}
	if position != nil {
		sets = append(sets, "interest_position = :position")
		values[":position"] = &types.AttributeValueMemberS{Value: *position}
	}
	if len(sets) == 0 {
		return nil
	}
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), "PROFILE"),
		UpdateExpression: aws.String("SET " + strings.Join(sets, ", ")), ExpressionAttributeValues: values,
	})
	return err
}

func (s *Store) PutFeed(ctx context.Context, feed domain.Feed) error {
	item, err := attributevalue.MarshalMap(feed)
	if err != nil {
		return err
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(s.table), Item: item})
	return err
}

func (s *Store) Feed(ctx context.Context, userID, feedID string) (domain.Feed, error) {
	response, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.FeedSK(feedID)), ConsistentRead: aws.Bool(true)})
	if err != nil {
		return domain.Feed{}, err
	}
	if len(response.Item) == 0 {
		return domain.Feed{}, ErrNotFound
	}
	var feed domain.Feed
	return feed, attributevalue.UnmarshalMap(response.Item, &feed)
}

func (s *Store) Feeds(ctx context.Context, userID string) ([]domain.Feed, error) {
	feeds := []domain.Feed{}
	var start map[string]types.AttributeValue
	for {
		response, err := s.db.Query(ctx, &dynamodb.QueryInput{
			TableName: aws.String(s.table), KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": &types.AttributeValueMemberS{Value: domain.UserPK(userID)}, ":prefix": &types.AttributeValueMemberS{Value: "F#"},
			}, ExclusiveStartKey: start,
		})
		if err != nil {
			return nil, err
		}
		var page []domain.Feed
		if err := attributevalue.UnmarshalListOfMaps(response.Items, &page); err != nil {
			return nil, err
		}
		feeds = append(feeds, page...)
		start = response.LastEvaluatedKey
		if len(start) == 0 {
			return feeds, nil
		}
	}
}

func (s *Store) DueFeeds(ctx context.Context, now time.Time) ([]domain.Feed, error) {
	result := []domain.Feed{}
	var start map[string]types.AttributeValue
	for {
		response, err := s.db.Scan(ctx, &dynamodb.ScanInput{
			TableName:        aws.String(s.table),
			FilterExpression: aws.String("begins_with(SK, :feed) AND next_fetch_at <= :now"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":feed": &types.AttributeValueMemberS{Value: "F#"}, ":now": &types.AttributeValueMemberS{Value: domain.Timestamp(now)},
			}, ExclusiveStartKey: start,
		})
		if err != nil {
			return nil, err
		}
		var page []domain.Feed
		if err := attributevalue.UnmarshalListOfMaps(response.Items, &page); err != nil {
			return nil, err
		}
		result = append(result, page...)
		start = response.LastEvaluatedKey
		if len(start) == 0 {
			return result, nil
		}
	}
}

func (s *Store) ScheduleFeed(ctx context.Context, userID, feedID string, next time.Time) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.FeedSK(feedID)),
		UpdateExpression:          aws.String("SET next_fetch_at = :next"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":next": &types.AttributeValueMemberS{Value: domain.Timestamp(next)}},
	})
	return err
}

func (s *Store) DeleteFeed(ctx context.Context, userID, feedID string) error {
	_, err := s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.FeedSK(feedID))})
	return err
}

func (s *Store) ItemExists(ctx context.Context, userID, itemID string, published time.Time) (bool, error) {
	response, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.ItemSK(published, itemID)), ProjectionExpression: aws.String("PK"),
	})
	if err != nil {
		return false, err
	}
	return len(response.Item) > 0, nil
}

func (s *Store) PutItem(ctx context.Context, item domain.Item) (bool, error) {
	encoded, err := attributevalue.MarshalMap(item)
	if err != nil {
		return false, err
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.table), Item: encoded, ConditionExpression: aws.String("attribute_not_exists(SK)"),
	})
	if err == nil {
		return true, nil
	}
	var conditional *types.ConditionalCheckFailedException
	if errors.As(err, &conditional) {
		return false, nil
	}
	return false, err
}

type cursor struct {
	PK    string   `json:"p"`
	SK    string   `json:"k"`
	Score *float64 `json:"s,omitempty"`
}

func (s *Store) Items(ctx context.Context, userID string, order domain.Order, encodedCursor string, limit int, includeRead bool) ([]domain.Item, string, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	start, err := decodeCursor(encodedCursor)
	if err != nil {
		return nil, "", err
	}
	if len(start) > 0 {
		pk, ok := start["PK"].(*types.AttributeValueMemberS)
		_, hasScore := start["score"]
		if !ok || pk.Value != domain.UserPK(userID) || (order == domain.OrderInterest) != hasScore {
			return nil, "", ErrInvalidCursor
		}
	}
	input := &dynamodb.QueryInput{
		TableName: aws.String(s.table), ScanIndexForward: aws.Bool(false), Limit: aws.Int32(int32(limit)), ExclusiveStartKey: start,
		KeyConditionExpression:   aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		FilterExpression:         aws.String("#ttl > :now"),
		ExpressionAttributeNames: map[string]string{"#ttl": "ttl"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk": &types.AttributeValueMemberS{Value: domain.UserPK(userID)}, ":prefix": &types.AttributeValueMemberS{Value: "I#"},
			":now": &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Unix(), 10)},
		},
	}
	if order == domain.OrderInterest {
		input.IndexName = aws.String("by-score")
		input.KeyConditionExpression = aws.String("PK = :pk")
		delete(input.ExpressionAttributeValues, ":prefix")
	}
	items := []domain.Item{}
	var last map[string]types.AttributeValue
	for len(items) < limit {
		input.Limit = aws.Int32(100)
		response, err := s.db.Query(ctx, input)
		if err != nil {
			return nil, "", err
		}
		var page []domain.Item
		if err := attributevalue.UnmarshalListOfMaps(response.Items, &page); err != nil {
			return nil, "", err
		}
		if err := s.ResolveRead(ctx, userID, page); err != nil {
			return nil, "", err
		}
		for i, item := range page {
			if !includeRead && item.Read {
				continue
			}
			items = append(items, item)
			if len(items) == limit {
				if i < len(page)-1 {
					last = itemPageKey(response.Items[i], order)
				} else {
					last = response.LastEvaluatedKey
				}
				break
			}
		}
		if len(items) == limit {
			break
		}
		last = response.LastEvaluatedKey
		if len(last) == 0 {
			break
		}
		input.ExclusiveStartKey = last
	}
	next, err := encodeCursor(last)
	return items, next, err
}

func itemPageKey(item map[string]types.AttributeValue, order domain.Order) map[string]types.AttributeValue {
	result := map[string]types.AttributeValue{"PK": item["PK"], "SK": item["SK"]}
	if order == domain.OrderInterest {
		result["score"] = item["score"]
	}
	return result
}

func (s *Store) Item(ctx context.Context, userID, itemID string) (domain.Item, error) {
	var start map[string]types.AttributeValue
	for {
		response, err := s.db.Query(ctx, &dynamodb.QueryInput{
			TableName: aws.String(s.table), KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
			FilterExpression: aws.String("item_id = :id AND #ttl > :now"), Limit: aws.Int32(100), ExclusiveStartKey: start,
			ConsistentRead:           aws.Bool(true),
			ExpressionAttributeNames: map[string]string{"#ttl": "ttl"},
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": &types.AttributeValueMemberS{Value: domain.UserPK(userID)}, ":prefix": &types.AttributeValueMemberS{Value: "I#"}, ":id": &types.AttributeValueMemberS{Value: itemID},
				":now": &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Unix(), 10)},
			},
		})
		if err != nil {
			return domain.Item{}, err
		}
		if len(response.Items) > 0 {
			var item domain.Item
			return item, attributevalue.UnmarshalMap(response.Items[0], &item)
		}
		start = response.LastEvaluatedKey
		if len(start) == 0 {
			return domain.Item{}, ErrNotFound
		}
	}
}

// Archives lists permanent copies newest-heart-first. Archive rows deliberately
// have no ttl attribute and do not resolve read state.
func (s *Store) Archives(ctx context.Context, userID, encodedCursor string, limit int) ([]domain.Item, string, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	start, err := decodeCursor(encodedCursor)
	if err != nil {
		return nil, "", err
	}
	if len(start) > 0 {
		pk, pkOK := start["PK"].(*types.AttributeValueMemberS)
		sk, skOK := start["SK"].(*types.AttributeValueMemberS)
		_, hasScore := start["score"]
		if !pkOK || pk.Value != domain.UserPK(userID) || !skOK || !strings.HasPrefix(sk.Value, "A#") || hasScore {
			return nil, "", ErrInvalidCursor
		}
	}
	response, err := s.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.table),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: domain.UserPK(userID)},
			":prefix": &types.AttributeValueMemberS{Value: "A#"},
		},
		ExclusiveStartKey: start,
		Limit:             aws.Int32(int32(limit)),
		ScanIndexForward:  aws.Bool(false),
	})
	if err != nil {
		return nil, "", err
	}
	items := []domain.Item{}
	if err := attributevalue.UnmarshalListOfMaps(response.Items, &items); err != nil {
		return nil, "", err
	}
	for i := range items {
		items[i].ArchiveSK = items[i].SK
		items[i].Hearted = true
		items[i].Read = false
	}
	next, err := encodeCursor(response.LastEvaluatedKey)
	return items, next, err
}

// ArchiveItem finds an archived item by item_id. The query is intentionally a
// partition scan; archive sizes are expected to remain small for this drop.
func (s *Store) ArchiveItem(ctx context.Context, userID, itemID string) (domain.Item, error) {
	var start map[string]types.AttributeValue
	for {
		response, err := s.db.Query(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(s.table),
			KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
			FilterExpression:       aws.String("item_id = :id"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk":     &types.AttributeValueMemberS{Value: domain.UserPK(userID)},
				":prefix": &types.AttributeValueMemberS{Value: "A#"},
				":id":     &types.AttributeValueMemberS{Value: itemID},
			},
			ExclusiveStartKey: start,
			Limit:             aws.Int32(100),
			ConsistentRead:    aws.Bool(true),
		})
		if err != nil {
			return domain.Item{}, err
		}
		if len(response.Items) > 0 {
			var item domain.Item
			if err := attributevalue.UnmarshalMap(response.Items[0], &item); err != nil {
				return domain.Item{}, err
			}
			item.ArchiveSK = item.SK
			item.Hearted = true
			return item, nil
		}
		start = response.LastEvaluatedKey
		if len(start) == 0 {
			return domain.Item{}, ErrNotFound
		}
	}
}

func (s *Store) archiveItemBySK(ctx context.Context, userID, archiveSK string) (domain.Item, error) {
	response, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.table),
		Key:            key(domain.UserPK(userID), archiveSK),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return domain.Item{}, err
	}
	if len(response.Item) == 0 {
		return domain.Item{}, ErrNotFound
	}
	var item domain.Item
	if err := attributevalue.UnmarshalMap(response.Item, &item); err != nil {
		return domain.Item{}, err
	}
	item.ArchiveSK = item.SK
	item.Hearted = true
	return item, nil
}

// SetHeart synchronously copies durable content and atomically changes the
// archive row, live item pointer, profile count, and implied ranking signal.
func (s *Store) SetHeart(ctx context.Context, userID, itemID string, hearted bool) (string, int, error) {
	if !hearted {
		return s.removeHeart(ctx, userID, itemID)
	}
	item, err := s.Item(ctx, userID, itemID)
	if err != nil {
		return "", 0, err
	}
	if item.ArchiveSK != "" {
		count, countErr := s.heartCount(ctx, userID)
		return item.ArchiveSK, count, countErr
	}

	now := time.Now().UTC()
	archiveSK := domain.ArchiveSK(now, item.ItemID)
	archive := item
	archive.SK = archiveSK
	archive.TTL = 0
	archive.ArchiveSK = ""
	archive.HeartedTS = domain.Timestamp(now)
	archive.Read = false
	archive.Signal = 0
	archive.Hearted = false

	archive.BodyKey = ""
	if item.HasBody {
		source := item.BodyKey
		if source == "" {
			source = BodyKey(userID, item.ItemID)
		}
		destination := ArchiveBodyKey(userID, item.ItemID)
		copied, copyErr := s.copyContent(ctx, source, destination)
		if copyErr != nil {
			return "", 0, copyErr
		}
		archive.HasBody = copied
		if copied {
			archive.BodyKey = destination
		}
	} else {
		archive.HasBody = false
	}

	archive.MediaKey = ""
	if item.MediaKey != "" {
		destination := ArchiveMediaKey(userID, item.ItemID)
		copied, copyErr := s.copyContent(ctx, item.MediaKey, destination)
		if copyErr != nil {
			return "", 0, copyErr
		}
		if copied {
			archive.MediaKey = destination
		} else {
			archive.MediaW = 0
			archive.MediaH = 0
		}
	} else {
		archive.MediaW = 0
		archive.MediaH = 0
	}

	encodedArchive, err := attributevalue.MarshalMap(archive)
	if err != nil {
		return "", 0, err
	}
	heartSignal, err := attributevalue.MarshalMap(domain.Signal{
		PK: domain.UserPK(userID), SK: domain.SignalSK(item.ItemID), ItemID: item.ItemID, Value: 1,
		Vector: item.Vector, Title: item.Title, FeedID: item.FeedID, CreatedAt: domain.Timestamp(now), Source: "heart",
	})
	if err != nil {
		return "", 0, err
	}
	values := map[string]types.AttributeValue{
		":archive": &types.AttributeValueMemberS{Value: archiveSK},
		":one":     &types.AttributeValueMemberN{Value: "1"},
	}
	baseWrites := []types.TransactWriteItem{
		{Put: &types.Put{TableName: aws.String(s.table), Item: encodedArchive, ConditionExpression: aws.String("attribute_not_exists(SK)")}},
		{Update: &types.Update{
			TableName: aws.String(s.table), Key: key(domain.UserPK(userID), item.SK),
			UpdateExpression:          aws.String("SET archive_sk = :archive REMOVE hearted"),
			ConditionExpression:       aws.String("attribute_exists(PK) AND attribute_not_exists(archive_sk)"),
			ExpressionAttributeValues: map[string]types.AttributeValue{":archive": values[":archive"]},
		}},
		{Update: &types.Update{
			TableName: aws.String(s.table), Key: key(domain.UserPK(userID), "PROFILE"),
			UpdateExpression: aws.String("ADD heart_count :one"), ExpressionAttributeValues: map[string]types.AttributeValue{":one": values[":one"]},
		}},
	}
	withSignal := append(append([]types.TransactWriteItem{}, baseWrites...), types.TransactWriteItem{Put: &types.Put{
		TableName: aws.String(s.table), Item: heartSignal, ConditionExpression: aws.String("attribute_not_exists(SK)"),
	}})
	err = s.transact(ctx, withSignal)
	if isTransactionCanceled(err) {
		// An existing explicit signal wins. The other conditions remain in the
		// retry so the archive state still changes as one atomic unit.
		err = s.transact(ctx, baseWrites)
	}
	if err != nil {
		// A concurrent identical heart may have won between the read and the
		// transaction. Report that durable state as the idempotent result.
		current, currentErr := s.Item(ctx, userID, itemID)
		if currentErr == nil && current.ArchiveSK != "" {
			count, countErr := s.heartCount(ctx, userID)
			return current.ArchiveSK, count, countErr
		}
		return "", 0, err
	}
	count, err := s.heartCount(ctx, userID)
	return archiveSK, count, err
}

func (s *Store) removeHeart(ctx context.Context, userID, itemID string) (string, int, error) {
	item, itemErr := s.Item(ctx, userID, itemID)
	var archive domain.Item
	var err error
	if itemErr == nil && item.ArchiveSK != "" {
		archive, err = s.archiveItemBySK(ctx, userID, item.ArchiveSK)
	} else {
		archive, err = s.ArchiveItem(ctx, userID, itemID)
	}
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			count, countErr := s.heartCount(ctx, userID)
			return "", count, countErr
		}
		return "", 0, err
	}

	minusOne := &types.AttributeValueMemberN{Value: "-1"}
	baseWrites := []types.TransactWriteItem{
		{Delete: &types.Delete{
			TableName: aws.String(s.table), Key: key(domain.UserPK(userID), archive.SK),
			ConditionExpression: aws.String("attribute_exists(SK)"),
		}},
	}
	if itemErr == nil {
		baseWrites = append(baseWrites, types.TransactWriteItem{Update: &types.Update{
			TableName: aws.String(s.table), Key: key(domain.UserPK(userID), item.SK),
			UpdateExpression:          aws.String("REMOVE archive_sk, hearted"),
			ConditionExpression:       aws.String("archive_sk = :archive"),
			ExpressionAttributeValues: map[string]types.AttributeValue{":archive": &types.AttributeValueMemberS{Value: archive.SK}},
		}})
	}
	baseWrites = append(baseWrites, types.TransactWriteItem{Update: &types.Update{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), "PROFILE"),
		UpdateExpression:          aws.String("ADD heart_count :minus_one"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":minus_one": minusOne},
	}})
	withSignalDelete := append(append([]types.TransactWriteItem{}, baseWrites...), types.TransactWriteItem{Delete: &types.Delete{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.SignalSK(itemID)),
		ConditionExpression:       aws.String("#source = :heart"),
		ExpressionAttributeNames:  map[string]string{"#source": "source"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":heart": &types.AttributeValueMemberS{Value: "heart"}},
	}})
	err = s.transact(ctx, withSignalDelete)
	if isTransactionCanceled(err) {
		// Missing or explicit signals must survive un-hearting.
		err = s.transact(ctx, baseWrites)
	}
	if err != nil {
		if _, currentErr := s.ArchiveItem(ctx, userID, itemID); errors.Is(currentErr, ErrNotFound) {
			count, countErr := s.heartCount(ctx, userID)
			return "", count, countErr
		}
		return "", 0, err
	}
	s.deleteArchiveContent(ctx, userID, itemID)
	count, err := s.heartCount(ctx, userID)
	return "", count, err
}

func (s *Store) transact(ctx context.Context, writes []types.TransactWriteItem) error {
	_, err := s.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: writes})
	return err
}

func isTransactionCanceled(err error) bool {
	var canceled *types.TransactionCanceledException
	return errors.As(err, &canceled)
}

func (s *Store) heartCount(ctx context.Context, userID string) (int, error) {
	user, err := s.User(ctx, userID)
	if err != nil {
		return 0, err
	}
	return user.HeartCount, nil
}

func (s *Store) copyContent(ctx context.Context, source, destination string) (bool, error) {
	if s.s3 == nil || s.bucket == "" {
		return false, fmt.Errorf("content storage is not configured")
	}
	_, err := s.s3.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(destination),
		CopySource: aws.String(url.PathEscape(s.bucket + "/" + strings.TrimLeft(source, "/"))),
	})
	if err == nil {
		return true, nil
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "404") {
		return false, nil
	}
	return false, err
}

func (s *Store) deleteArchiveContent(ctx context.Context, userID, itemID string) {
	if s.s3 == nil || s.bucket == "" {
		return
	}
	for _, objectKey := range []string{ArchiveBodyKey(userID, itemID), ArchiveMediaKey(userID, itemID)} {
		if _, err := s.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objectKey)}); err != nil {
			slog.ErrorContext(ctx, "delete archived content", "key", objectKey, "error", err)
		}
	}
}

func (s *Store) Signals(ctx context.Context, userID string) ([]domain.Signal, error) {
	result := []domain.Signal{}
	var start map[string]types.AttributeValue
	for {
		response, err := s.db.Query(ctx, &dynamodb.QueryInput{
			TableName: aws.String(s.table), KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": &types.AttributeValueMemberS{Value: domain.UserPK(userID)}, ":prefix": &types.AttributeValueMemberS{Value: "S#"},
			}, ExclusiveStartKey: start,
		})
		if err != nil {
			return nil, err
		}
		var page []domain.Signal
		if err := attributevalue.UnmarshalListOfMaps(response.Items, &page); err != nil {
			return nil, err
		}
		result = append(result, page...)
		start = response.LastEvaluatedKey
		if len(start) == 0 {
			return result, nil
		}
	}
}

func (s *Store) SetSignal(ctx context.Context, userID string, item domain.Item, value int) error {
	heartSource := false
	if value == 0 {
		if item.ArchiveSK != "" {
			value = 1
			heartSource = true
		} else {
			_, err := s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.SignalSK(item.ItemID))})
			return err
		}
	}
	signal := domain.Signal{
		PK: domain.UserPK(userID), SK: domain.SignalSK(item.ItemID), ItemID: item.ItemID, Value: value,
		Vector: item.Vector, Title: item.Title, FeedID: item.FeedID, CreatedAt: domain.Timestamp(time.Now()),
	}
	if heartSource {
		signal.Source = "heart"
	}
	encoded, err := attributevalue.MarshalMap(signal)
	if err != nil {
		return err
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(s.table), Item: encoded})
	return err
}

func (s *Store) ResolveRead(ctx context.Context, userID string, items []domain.Item) error {
	if len(items) == 0 {
		return nil
	}
	keys := make([]map[string]types.AttributeValue, len(items))
	for i, item := range items {
		keys[i] = key(domain.UserPK(userID), domain.ReadSK(item.ItemID))
	}
	read := make(map[string]bool)
	pending := keys
	for attempt := 0; len(pending) > 0 && attempt < 4; attempt++ {
		response, err := s.db.BatchGetItem(ctx, &dynamodb.BatchGetItemInput{RequestItems: map[string]types.KeysAndAttributes{s.table: {Keys: pending, ProjectionExpression: aws.String("SK")}}})
		if err != nil {
			return err
		}
		for _, row := range response.Responses[s.table] {
			if value, ok := row["SK"].(*types.AttributeValueMemberS); ok {
				read[strings.TrimPrefix(value.Value, "R#")] = true
			}
		}
		pending = response.UnprocessedKeys[s.table].Keys
	}
	if len(pending) > 0 {
		return fmt.Errorf("%d read-state lookups were throttled", len(pending))
	}
	for i := range items {
		items[i].Read = read[items[i].ItemID]
	}
	return nil
}

func (s *Store) SetRead(ctx context.Context, userID string, ids []string, read bool) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC()
	requests := make([]types.WriteRequest, 0, len(ids))
	for _, id := range ids {
		if read {
			row, err := attributevalue.MarshalMap(domain.Read{PK: domain.UserPK(userID), SK: domain.ReadSK(id), ReadAt: domain.Timestamp(now), TTL: now.Add(domain.Retention).Unix()})
			if err != nil {
				return err
			}
			requests = append(requests, types.WriteRequest{PutRequest: &types.PutRequest{Item: row}})
		} else {
			requests = append(requests, types.WriteRequest{DeleteRequest: &types.DeleteRequest{Key: key(domain.UserPK(userID), domain.ReadSK(id))}})
		}
	}
	for len(requests) > 0 {
		count := min(25, len(requests))
		pending := requests[:count]
		for attempt := 0; len(pending) > 0 && attempt < 4; attempt++ {
			response, err := s.db.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{RequestItems: map[string][]types.WriteRequest{s.table: pending}})
			if err != nil {
				return err
			}
			pending = response.UnprocessedItems[s.table]
		}
		if len(pending) > 0 {
			return fmt.Errorf("%d read-state writes were throttled", len(pending))
		}
		requests = requests[count:]
	}
	return nil
}

func (s *Store) PutContent(ctx context.Context, objectKey, contentType string, body []byte) error {
	if s.s3 == nil || s.bucket == "" {
		return fmt.Errorf("content storage is not configured")
	}
	_, err := s.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(objectKey), Body: bytes.NewReader(body), ContentType: aws.String(contentType),
		CacheControl: aws.String("public,max-age=604800,immutable"),
	})
	return err
}

func (s *Store) Content(ctx context.Context, objectKey string) ([]byte, string, error) {
	response, err := s.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objectKey)})
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return body, aws.ToString(response.ContentType), err
}

func (s *Store) ContentURL(objectKey string) string {
	if objectKey == "" {
		return ""
	}
	return s.contentURL + "/" + strings.TrimLeft(objectKey, "/")
}

func (s *Store) PublicItem(item domain.Item) domain.Item {
	item.Hearted = item.ArchiveSK != "" || item.HeartedTS != ""
	item.MediaKey = s.ContentURL(item.MediaKey)
	item.BodyKey = s.ContentURL(item.BodyKey)
	item.FaviconKey = s.ContentURL(item.FaviconKey)
	item.Vector = nil
	return item
}

func key(pk, sk string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"PK": &types.AttributeValueMemberS{Value: pk}, "SK": &types.AttributeValueMemberS{Value: sk}}
}

func encodeCursor(last map[string]types.AttributeValue) (string, error) {
	if len(last) == 0 {
		return "", nil
	}
	value := cursor{}
	if pk, ok := last["PK"].(*types.AttributeValueMemberS); ok {
		value.PK = pk.Value
	}
	if sk, ok := last["SK"].(*types.AttributeValueMemberS); ok {
		value.SK = sk.Value
	}
	if score, ok := last["score"].(*types.AttributeValueMemberN); ok {
		parsed, err := strconv.ParseFloat(score.Value, 64)
		if err != nil {
			return "", err
		}
		value.Score = &parsed
	}
	raw, err := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(raw), err
}

func decodeCursor(encoded string) (map[string]types.AttributeValue, error) {
	if encoded == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	var value cursor
	if err := json.Unmarshal(raw, &value); err != nil || value.PK == "" || value.SK == "" {
		return nil, ErrInvalidCursor
	}
	result := key(value.PK, value.SK)
	if value.Score != nil {
		result["score"] = &types.AttributeValueMemberN{Value: strconv.FormatFloat(*value.Score, 'g', -1, 64)}
	}
	return result, nil
}

var (
	ErrNotFound      = errors.New("not found")
	ErrInvalidCursor = errors.New("invalid cursor")
)
