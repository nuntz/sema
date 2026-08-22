package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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
}

type s3API interface {
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
			"order_pref = if_not_exists(order_pref, :order)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":email": &types.AttributeValueMemberS{Value: email},
			":now":   &types.AttributeValueMemberS{Value: now},
			":order": &types.AttributeValueMemberS{Value: string(domain.OrderChrono)},
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

func (s *Store) UpdateUser(ctx context.Context, userID string, order *domain.Order, boundary, position *string) error {
	sets := make([]string, 0, 3)
	values := make(map[string]types.AttributeValue)
	if order != nil {
		sets = append(sets, "order_pref = :order")
		values[":order"] = &types.AttributeValueMemberS{Value: string(*order)}
	}
	if boundary != nil {
		sets = append(sets, "read_boundary_ts = :boundary")
		values[":boundary"] = &types.AttributeValueMemberS{Value: *boundary}
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

func (s *Store) Items(ctx context.Context, userID string, order domain.Order, encodedCursor string, limit int) ([]domain.Item, string, error) {
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
		input.Limit = aws.Int32(int32(limit - len(items)))
		response, err := s.db.Query(ctx, input)
		if err != nil {
			return nil, "", err
		}
		var page []domain.Item
		if err := attributevalue.UnmarshalListOfMaps(response.Items, &page); err != nil {
			return nil, "", err
		}
		items = append(items, page...)
		last = response.LastEvaluatedKey
		if len(last) == 0 {
			break
		}
		input.ExclusiveStartKey = last
	}
	next, err := encodeCursor(last)
	return items, next, err
}

func (s *Store) Item(ctx context.Context, userID, itemID string) (domain.Item, error) {
	var start map[string]types.AttributeValue
	for {
		response, err := s.db.Query(ctx, &dynamodb.QueryInput{
			TableName: aws.String(s.table), KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
			FilterExpression: aws.String("item_id = :id AND #ttl > :now"), Limit: aws.Int32(100), ExclusiveStartKey: start,
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
	if value == 0 {
		_, err := s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.SignalSK(item.ItemID))})
		return err
	}
	signal := domain.Signal{
		PK: domain.UserPK(userID), SK: domain.SignalSK(item.ItemID), ItemID: item.ItemID, Value: value,
		Vector: item.Vector, Title: item.Title, FeedID: item.FeedID, CreatedAt: domain.Timestamp(time.Now()),
	}
	encoded, err := attributevalue.MarshalMap(signal)
	if err != nil {
		return err
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(s.table), Item: encoded})
	return err
}

func (s *Store) ResolveRead(ctx context.Context, userID string, order domain.Order, boundary string, items []domain.Item) error {
	if order == domain.OrderChrono {
		for i := range items {
			items[i].Read = boundary != "" && items[i].PublishedTS >= boundary
		}
		return nil
	}
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
