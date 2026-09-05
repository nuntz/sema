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
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
)

type dynamoAPI interface {
	BatchGetItem(context.Context, *dynamodb.BatchGetItemInput, ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error)
	BatchWriteItem(context.Context, *dynamodb.BatchWriteItemInput, ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
	DeleteItem(context.Context, *dynamodb.DeleteItemInput, ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	Query(context.Context, *dynamodb.QueryInput, ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	UpdateItem(context.Context, *dynamodb.UpdateItemInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	TransactWriteItems(context.Context, *dynamodb.TransactWriteItemsInput, ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
}

type s3API interface {
	CopyObject(context.Context, *s3.CopyObjectInput, ...func(*s3.Options)) (*s3.CopyObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type Store struct {
	db         dynamoAPI
	s3         s3API
	table      string
	bucket     string
	contentURL string
}

const (
	feedIndexPK             = "FEED"
	userIndexPK             = "USER"
	itemsForFeedsPageBudget = 5
)

type itemProjection struct {
	expression string
	names      map[string]string
}

var listItemProjection = newItemProjection("vector", "search_text")

var feedCounterAttributes = map[string]bool{
	"item_count": true, "extraction_failures": true, "media_failures": true,
	"extraction_quality_total": true, "extraction_sample": true,
}

func newItemProjection(excluded ...string) itemProjection {
	exclusions := make(map[string]bool, len(excluded))
	for _, name := range excluded {
		exclusions[name] = true
	}

	typeOfItem := reflect.TypeOf(domain.Item{})
	names := make(map[string]string, typeOfItem.NumField())
	aliases := make([]string, 0, typeOfItem.NumField())
	for index := range typeOfItem.NumField() {
		tag := typeOfItem.Field(index).Tag.Get("dynamodbav")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" || exclusions[name] {
			continue
		}
		alias := fmt.Sprintf("#item%d", index)
		if name == "ttl" {
			alias = "#ttl"
		}
		names[alias] = name
		aliases = append(aliases, alias)
	}
	return itemProjection{expression: strings.Join(aliases, ", "), names: names}
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
			"order_pref = if_not_exists(order_pref, :order), heart_count = if_not_exists(heart_count, :zero), signal_count = if_not_exists(signal_count, :zero), " +
			"gsi1pk = :users, next_fetch_at = if_not_exists(next_fetch_at, :now) REMOVE #read_boundary"),
		ExpressionAttributeNames: map[string]string{"#read_boundary": "read_boundary_ts"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":email": &types.AttributeValueMemberS{Value: email},
			":now":   &types.AttributeValueMemberS{Value: now},
			":order": &types.AttributeValueMemberS{Value: string(domain.OrderInterest)},
			":zero":  &types.AttributeValueMemberN{Value: "0"},
			":users": &types.AttributeValueMemberS{Value: userIndexPK},
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

func (s *Store) UpdateUser(ctx context.Context, userID string, order *domain.Order, position, tag *string) error {
	sets := make([]string, 0, 3)
	values := make(map[string]types.AttributeValue)
	if order != nil {
		sets = append(sets, "order_pref = :order")
		values[":order"] = &types.AttributeValueMemberS{Value: string(*order)}
	}
	if position != nil {
		sets = append(sets, "interest_position = :position")
		values[":position"] = &types.AttributeValueMemberS{Value: *position}
	}
	if tag != nil {
		sets = append(sets, "tag_pref = :tag")
		values[":tag"] = &types.AttributeValueMemberS{Value: *tag}
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
	feed.Connector = domain.FeedConnector(feed)
	if feed.FetchIntervalH == 0 {
		feed.FetchIntervalH = 1
	}
	if feed.Muted {
		feed.GSI1PK = ""
	} else {
		feed.GSI1PK = feedIndexPK
	}
	item, err := attributevalue.MarshalMap(feed)
	if err != nil {
		return err
	}
	names := make(map[string]string)
	values := make(map[string]types.AttributeValue)
	sets, removes := []string{}, []string{}
	typeOfFeed := reflect.TypeOf(domain.Feed{})
	for index := range typeOfFeed.NumField() {
		name := strings.Split(typeOfFeed.Field(index).Tag.Get("dynamodbav"), ",")[0]
		if name == "" || name == "-" || name == "PK" || name == "SK" || feedCounterAttributes[name] {
			continue
		}
		alias := fmt.Sprintf("#feed%d", index)
		names[alias] = name
		if value, ok := item[name]; ok {
			placeholder := fmt.Sprintf(":feed%d", index)
			sets = append(sets, alias+" = "+placeholder)
			values[placeholder] = value
		} else {
			removes = append(removes, alias)
		}
	}
	expression := "SET " + strings.Join(sets, ", ")
	if len(removes) > 0 {
		expression += " REMOVE " + strings.Join(removes, ", ")
	}
	_, err = s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(feed.PK, feed.SK), UpdateExpression: aws.String(expression),
		ExpressionAttributeNames: names, ExpressionAttributeValues: values,
	})
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
	if err := attributevalue.UnmarshalMap(response.Item, &feed); err != nil {
		return domain.Feed{}, err
	}
	if feed.FetchIntervalH == 0 {
		feed.FetchIntervalH = 1
	}
	feed.Connector = domain.FeedConnector(feed)
	return feed, nil
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
		for i := range page {
			page[i].Connector = domain.FeedConnector(page[i])
			if page[i].FetchIntervalH == 0 {
				page[i].FetchIntervalH = 1
			}
			if page[i].Tags == nil {
				page[i].Tags = []string{}
			}
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
	type feedKey struct {
		PK string `dynamodbav:"PK"`
		SK string `dynamodbav:"SK"`
	}
	var start map[string]types.AttributeValue
	for {
		response, err := s.db.Query(ctx, &dynamodb.QueryInput{
			TableName: aws.String(s.table), IndexName: aws.String("by-next-fetch"),
			KeyConditionExpression: aws.String("gsi1pk = :feed AND next_fetch_at <= :now"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":feed": &types.AttributeValueMemberS{Value: feedIndexPK}, ":now": &types.AttributeValueMemberS{Value: domain.Timestamp(now)},
			}, ExclusiveStartKey: start,
		})
		if err != nil {
			return nil, err
		}
		var page []feedKey
		if err := attributevalue.UnmarshalListOfMaps(response.Items, &page); err != nil {
			return nil, err
		}
		for _, feed := range page {
			result = append(result, domain.Feed{PK: feed.PK, SK: feed.SK, FeedID: strings.TrimPrefix(feed.SK, "F#")})
		}
		start = response.LastEvaluatedKey
		if len(start) == 0 {
			return result, nil
		}
	}
}

func (s *Store) ScheduleFeed(ctx context.Context, userID, feedID string, next time.Time) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.FeedSK(feedID)),
		UpdateExpression: aws.String("SET next_fetch_at = :next, gsi1pk = :feed"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":next": &types.AttributeValueMemberS{Value: domain.Timestamp(next)}, ":feed": &types.AttributeValueMemberS{Value: feedIndexPK},
		},
	})
	return err
}

func (s *Store) ClaimFeed(ctx context.Context, userID, feedID string, due, next time.Time) (bool, error) {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.FeedSK(feedID)),
		UpdateExpression:    aws.String("SET next_fetch_at = :next, gsi1pk = :feed"),
		ConditionExpression: aws.String("next_fetch_at <= :due AND (attribute_not_exists(muted) OR muted = :false)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":due":   &types.AttributeValueMemberS{Value: domain.Timestamp(due)},
			":next":  &types.AttributeValueMemberS{Value: domain.Timestamp(next)},
			":feed":  &types.AttributeValueMemberS{Value: feedIndexPK},
			":false": &types.AttributeValueMemberBOOL{Value: false},
		},
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

func (s *Store) DeleteFeed(ctx context.Context, userID, feedID string) error {
	_, err := s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.FeedSK(feedID))})
	return err
}

func (s *Store) ItemExists(ctx context.Context, userID, itemID string) (bool, error) {
	response, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.ItemIdentitySK(itemID)),
		ProjectionExpression: aws.String("PK, #ttl"), ExpressionAttributeNames: map[string]string{"#ttl": "ttl"},
	})
	if err != nil {
		return false, err
	}
	if len(response.Item) == 0 {
		return false, nil
	}
	var identity domain.ItemIdentity
	if err := attributevalue.UnmarshalMap(response.Item, &identity); err != nil {
		return false, err
	}
	return identity.TTL == 0 || identity.TTL > time.Now().Unix(), nil
}

func (s *Store) PutItem(ctx context.Context, item domain.Item) (bool, error) {
	vector, err := attributevalue.MarshalMap(domain.ItemVector{
		PK: item.PK, SK: domain.ItemVectorSK(item.ItemID), Vector: item.Vector, TTL: item.TTL,
	})
	if err != nil {
		return false, err
	}
	item.Vector = nil
	encoded, err := attributevalue.MarshalMap(item)
	if err != nil {
		return false, err
	}
	identity, err := attributevalue.MarshalMap(domain.ItemIdentity{
		PK: item.PK, SK: domain.ItemIdentitySK(item.ItemID), ItemSK: item.SK, TTL: item.TTL,
	})
	if err != nil {
		return false, err
	}
	now := &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Unix(), 10)}
	_, err = s.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: []types.TransactWriteItem{
		{Put: &types.Put{
			TableName: aws.String(s.table), Item: identity, ConditionExpression: aws.String("attribute_not_exists(SK) OR #ttl <= :now"),
			ExpressionAttributeNames: map[string]string{"#ttl": "ttl"}, ExpressionAttributeValues: map[string]types.AttributeValue{":now": now},
		}},
		{Put: &types.Put{
			TableName: aws.String(s.table), Item: encoded, ConditionExpression: aws.String("attribute_not_exists(SK) OR #ttl <= :now"),
			ExpressionAttributeNames: map[string]string{"#ttl": "ttl"}, ExpressionAttributeValues: map[string]types.AttributeValue{":now": now},
		}},
		{Put: &types.Put{
			TableName: aws.String(s.table), Item: vector, ConditionExpression: aws.String("attribute_not_exists(SK) OR #ttl <= :now"),
			ExpressionAttributeNames: map[string]string{"#ttl": "ttl"}, ExpressionAttributeValues: map[string]types.AttributeValue{":now": now},
		}},
		{Update: feedCounterUpdate(s.table, item)},
	}})
	if err == nil {
		return true, nil
	}
	if transactionConditionFailed(err) {
		return false, nil
	}
	return false, err
}

// PutItemFailure records a terminal validation failure at the same stable
// identity key used by successful items so feed fetches do not enqueue it
// again during the retention window.
func (s *Store) PutItemFailure(ctx context.Context, userID, itemID string, ttl int64) error {
	if userID == "" || itemID == "" || ttl == 0 {
		return errors.New("item failure identity and ttl are required")
	}
	marker, err := attributevalue.MarshalMap(domain.ItemIdentity{
		PK: domain.UserPK(userID), SK: domain.ItemIdentitySK(itemID), TTL: ttl,
	})
	if err != nil {
		return err
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.table), Item: marker,
		ConditionExpression:      aws.String("attribute_not_exists(SK) OR #ttl <= :now"),
		ExpressionAttributeNames: map[string]string{"#ttl": "ttl"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":now": &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Unix(), 10)},
		},
	})
	var conditional *types.ConditionalCheckFailedException
	if errors.As(err, &conditional) {
		return nil
	}
	return err
}

// ReconcileItemIdentity creates or refreshes the stable identity marker and
// removes known duplicate live rows as one operation. The caller chooses the
// canonical row; read, signal, behaviour, and archive state are all keyed by
// item_id and therefore remain shared by every duplicate.
func (s *Store) ReconcileItemIdentity(ctx context.Context, userID string, canonical domain.Item, duplicates []domain.Item) error {
	if canonical.ItemID == "" || canonical.PK != domain.UserPK(userID) {
		return errors.New("invalid canonical item identity")
	}
	identity, err := attributevalue.MarshalMap(domain.ItemIdentity{
		PK: canonical.PK, SK: domain.ItemIdentitySK(canonical.ItemID), ItemSK: canonical.SK, TTL: canonical.TTL,
	})
	if err != nil {
		return err
	}
	writes := []types.TransactWriteItem{{Put: &types.Put{TableName: aws.String(s.table), Item: identity}}}

	archiveSK, heartedTS := canonical.ArchiveSK, canonical.HeartedTS
	for _, duplicate := range duplicates {
		if duplicate.ItemID != canonical.ItemID || duplicate.PK != canonical.PK || duplicate.SK == canonical.SK {
			return errors.New("invalid duplicate item identity")
		}
		if archiveSK == "" && duplicate.ArchiveSK != "" {
			archiveSK = duplicate.ArchiveSK
		}
		if heartedTS == "" && duplicate.HeartedTS != "" {
			heartedTS = duplicate.HeartedTS
		}
		writes = append(writes, types.TransactWriteItem{Delete: &types.Delete{
			TableName: aws.String(s.table), Key: key(duplicate.PK, duplicate.SK),
			ConditionExpression: aws.String("item_id = :item_id"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":item_id": &types.AttributeValueMemberS{Value: canonical.ItemID},
			},
		}})
	}
	if archiveSK != canonical.ArchiveSK || heartedTS != canonical.HeartedTS {
		sets := make([]string, 0, 2)
		values := map[string]types.AttributeValue{":item_id": &types.AttributeValueMemberS{Value: canonical.ItemID}}
		if archiveSK != "" {
			sets = append(sets, "archive_sk = :archive")
			values[":archive"] = &types.AttributeValueMemberS{Value: archiveSK}
		}
		if heartedTS != "" {
			sets = append(sets, "hearted_ts = :hearted")
			values[":hearted"] = &types.AttributeValueMemberS{Value: heartedTS}
		}
		writes = append(writes, types.TransactWriteItem{Update: &types.Update{
			TableName: aws.String(s.table), Key: key(canonical.PK, canonical.SK),
			UpdateExpression:    aws.String("SET " + strings.Join(sets, ", ")),
			ConditionExpression: aws.String("item_id = :item_id"), ExpressionAttributeValues: values,
		}})
	}
	if len(writes) > 100 {
		return fmt.Errorf("item %s has too many duplicate rows to reconcile atomically", canonical.ItemID)
	}
	_, err = s.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: writes})
	return err
}

// OverwriteItem is reserved for replay: unlike PutItem it intentionally
// replaces the existing live row at its stable key.
func (s *Store) OverwriteItem(ctx context.Context, item domain.Item) error {
	vector, err := attributevalue.MarshalMap(domain.ItemVector{
		PK: item.PK, SK: domain.ItemVectorSK(item.ItemID), Vector: item.Vector, TTL: item.TTL,
	})
	if err != nil {
		return err
	}
	item.Vector = nil
	encoded, err := attributevalue.MarshalMap(item)
	if err != nil {
		return err
	}
	_, err = s.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: []types.TransactWriteItem{
		{Put: &types.Put{TableName: aws.String(s.table), Item: encoded}},
		{Put: &types.Put{TableName: aws.String(s.table), Item: vector}},
	}})
	return err
}

func feedCounterUpdate(table string, item domain.Item) *types.Update {
	extractionFailure, mediaFailure := 0, 0
	if !item.HasBody {
		extractionFailure = 1
	}
	if item.MediaKey == "" {
		mediaFailure = 1
	}
	return &types.Update{
		TableName: aws.String(table), Key: key(item.PK, domain.FeedSK(item.FeedID)),
		UpdateExpression:    aws.String("ADD item_count :one, extraction_sample :one, extraction_failures :extraction_failure, media_failures :media_failure, extraction_quality_total :quality"),
		ConditionExpression: aws.String("attribute_exists(PK)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":one":                &types.AttributeValueMemberN{Value: "1"},
			":extraction_failure": &types.AttributeValueMemberN{Value: strconv.Itoa(extractionFailure)},
			":media_failure":      &types.AttributeValueMemberN{Value: strconv.Itoa(mediaFailure)},
			":quality":            &types.AttributeValueMemberN{Value: strconv.FormatFloat(item.ExtractQuality, 'f', -1, 64)},
		},
	}
}

type cursor struct {
	PK    string   `json:"p"`
	SK    string   `json:"k"`
	Score *float64 `json:"s,omitempty"`
}

func (s *Store) Items(ctx context.Context, userID string, order domain.Order, encodedCursor string, limit int, includeRead bool) ([]domain.Item, string, error) {
	items, next, _, err := s.ItemsForFeeds(ctx, userID, order, encodedCursor, limit, includeRead, nil, nil)
	return items, next, err
}

// ItemsForFeeds fills a page after applying read-state and feed membership.
// A nil allowedFeedIDs map disables feed filtering; an empty map returns no
// items while still walking the underlying pages until the end or page budget.
func (s *Store) ItemsForFeeds(ctx context.Context, userID string, order domain.Order, encodedCursor string, limit int, includeRead bool, allowedFeedIDs, excludeItemIDs map[string]bool) ([]domain.Item, string, *domain.Item, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	start, err := decodeCursor(encodedCursor)
	if err != nil {
		return nil, "", nil, err
	}
	if len(start) > 0 {
		pk, ok := start["PK"].(*types.AttributeValueMemberS)
		_, hasScore := start["score"]
		if !ok || pk.Value != domain.UserPK(userID) || (order == domain.OrderInterest) != hasScore {
			return nil, "", nil, ErrInvalidCursor
		}
	}
	input := &dynamodb.QueryInput{
		TableName: aws.String(s.table), ScanIndexForward: aws.Bool(false), ExclusiveStartKey: start,
		KeyConditionExpression:   aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		FilterExpression:         aws.String("#ttl > :now"),
		ProjectionExpression:     aws.String(listItemProjection.expression),
		ExpressionAttributeNames: listItemProjection.names,
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
	seenItemIDs := make(map[string]bool)
	var newestRead *domain.Item
	var last map[string]types.AttributeValue
	for pages := 0; len(items) < limit && pages < itemsForFeedsPageBudget; pages++ {
		input.Limit = aws.Int32(100)
		response, err := s.db.Query(ctx, input)
		if err != nil {
			return nil, "", nil, err
		}
		var page []domain.Item
		if err := attributevalue.UnmarshalListOfMaps(response.Items, &page); err != nil {
			return nil, "", nil, err
		}
		if err := s.ResolveRead(ctx, userID, page); err != nil {
			return nil, "", nil, err
		}
		for i, item := range page {
			if allowedFeedIDs != nil && !allowedFeedIDs[item.FeedID] {
				continue
			}
			if excludeItemIDs[item.ItemID] {
				continue
			}
			if seenItemIDs[item.ItemID] {
				continue
			}
			seenItemIDs[item.ItemID] = true
			if item.Read && newestRead == nil {
				anchor := item
				newestRead = &anchor
			}
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
	return items, next, newestRead, err
}

func (s *Store) PutStory(ctx context.Context, story domain.Story) error {
	if story.PK == "" || story.SK == "" || story.StoryID == "" || len(story.MemberIDs) == 0 {
		return errors.New("story key, ID, and members are required")
	}
	encoded, err := attributevalue.MarshalMap(story)
	if err != nil {
		return err
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(s.table), Item: encoded})
	return err
}

func (s *Store) Story(ctx context.Context, userID, storyID string) (domain.Story, error) {
	response, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.StorySK(storyID)), ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return domain.Story{}, err
	}
	if len(response.Item) == 0 {
		return domain.Story{}, ErrNotFound
	}
	var row domain.Story
	return row, attributevalue.UnmarshalMap(response.Item, &row)
}

func (s *Store) Stories(ctx context.Context, userID string) ([]domain.Story, error) {
	rows := []domain.Story{}
	var start map[string]types.AttributeValue
	for {
		response, err := s.db.Query(ctx, &dynamodb.QueryInput{
			TableName: aws.String(s.table), KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
			FilterExpression: aws.String("#ttl > :now"), ExpressionAttributeNames: map[string]string{"#ttl": "ttl"},
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": &types.AttributeValueMemberS{Value: domain.UserPK(userID)}, ":prefix": &types.AttributeValueMemberS{Value: "T#"},
				":now": &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Unix(), 10)},
			},
			ExclusiveStartKey: start, ConsistentRead: aws.Bool(true),
		})
		if err != nil {
			return nil, err
		}
		var page []domain.Story
		if err := attributevalue.UnmarshalListOfMaps(response.Items, &page); err != nil {
			return nil, err
		}
		rows = append(rows, page...)
		start = response.LastEvaluatedKey
		if len(start) == 0 {
			return rows, nil
		}
	}
}

func (s *Store) AddStoryMember(ctx context.Context, userID, storyID, itemID string, ttl int64) error {
	if userID == "" || storyID == "" || itemID == "" || ttl == 0 {
		return errors.New("story member identity and ttl are required")
	}
	now := domain.Timestamp(time.Now())
	values := map[string]types.AttributeValue{
		":member":  &types.AttributeValueMemberSS{Value: []string{itemID}},
		":updated": &types.AttributeValueMemberS{Value: now},
		":ttl":     &types.AttributeValueMemberN{Value: strconv.FormatInt(ttl, 10)},
	}
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.StorySK(storyID)),
		UpdateExpression:         aws.String("ADD member_ids :member SET updated_at = :updated, #ttl = :ttl"),
		ConditionExpression:      aws.String("attribute_exists(PK) AND #ttl <= :ttl"),
		ExpressionAttributeNames: map[string]string{"#ttl": "ttl"}, ExpressionAttributeValues: values,
	})
	var conditional *types.ConditionalCheckFailedException
	if !errors.As(err, &conditional) {
		return err
	}
	_, err = s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.StorySK(storyID)),
		UpdateExpression:          aws.String("ADD member_ids :member SET updated_at = :updated"),
		ConditionExpression:       aws.String("attribute_exists(PK)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":member": values[":member"], ":updated": values[":updated"]},
	})
	return err
}

func (s *Store) SetItemStory(ctx context.Context, item domain.Item, storyID string) error {
	if item.PK == "" || item.SK == "" || item.ItemID == "" {
		return errors.New("item key and ID are required")
	}
	input := &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(item.PK, item.SK),
		ConditionExpression:       aws.String("attribute_exists(PK) AND item_id = :item"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":item": &types.AttributeValueMemberS{Value: item.ItemID}},
	}
	if storyID == "" {
		input.UpdateExpression = aws.String("REMOVE story_id")
	} else {
		input.UpdateExpression = aws.String("SET story_id = :story")
		input.ExpressionAttributeValues[":story"] = &types.AttributeValueMemberS{Value: storyID}
	}
	_, err := s.db.UpdateItem(ctx, input)
	return err
}

func (s *Store) DeleteStory(ctx context.Context, userID, storyID string) error {
	_, err := s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.StorySK(storyID)),
	})
	return err
}

func (s *Store) LiveItems(ctx context.Context, userID string) ([]domain.Item, error) {
	items := []domain.Item{}
	var start map[string]types.AttributeValue
	for {
		response, err := s.db.Query(ctx, &dynamodb.QueryInput{
			TableName:                aws.String(s.table),
			KeyConditionExpression:   aws.String("PK = :pk AND begins_with(SK, :prefix)"),
			FilterExpression:         aws.String("#ttl > :now"),
			ExpressionAttributeNames: map[string]string{"#ttl": "ttl"},
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk":     &types.AttributeValueMemberS{Value: domain.UserPK(userID)},
				":prefix": &types.AttributeValueMemberS{Value: "I#"},
				":now":    &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Unix(), 10)},
			},
			ExclusiveStartKey: start,
			ConsistentRead:    aws.Bool(true),
		})
		if err != nil {
			return nil, err
		}
		var page []domain.Item
		if err := attributevalue.UnmarshalListOfMaps(response.Items, &page); err != nil {
			return nil, err
		}
		items = append(items, page...)
		start = response.LastEvaluatedKey
		if len(start) == 0 {
			return items, nil
		}
	}
}

// ArchiveItems returns every permanent archive row for schema backfills and
// vector indexing. It intentionally has no public cursor or page-size cap.
func (s *Store) ArchiveItems(ctx context.Context, userID string) ([]domain.Item, error) {
	items := []domain.Item{}
	var start map[string]types.AttributeValue
	for {
		response, err := s.db.Query(ctx, &dynamodb.QueryInput{
			TableName: aws.String(s.table), KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": &types.AttributeValueMemberS{Value: domain.UserPK(userID)}, ":prefix": &types.AttributeValueMemberS{Value: "A#"},
			},
			ExclusiveStartKey: start, ConsistentRead: aws.Bool(true), ScanIndexForward: aws.Bool(false),
		})
		if err != nil {
			return nil, err
		}
		var page []domain.Item
		if err := attributevalue.UnmarshalListOfMaps(response.Items, &page); err != nil {
			return nil, err
		}
		items = append(items, page...)
		start = response.LastEvaluatedKey
		if len(start) == 0 {
			return items, nil
		}
	}
}

// UpdateSearchText rewrites only the derived search attribute, preserving all
// mutable item state accumulated since ingest.
func (s *Store) UpdateSearchText(ctx context.Context, item domain.Item) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(item.PK, item.SK),
		UpdateExpression:    aws.String("SET search_text = :search"),
		ConditionExpression: aws.String("attribute_exists(PK) AND item_id = :item"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":search": &types.AttributeValueMemberS{Value: domain.DeriveSearchText(item.Title, item.Summary)},
			":item":   &types.AttributeValueMemberS{Value: item.ItemID},
		},
	})
	return err
}

// UpdateMediaVariants changes only the media dimensions and responsive asset
// manifest so a backfill cannot overwrite mutable read, heart, or rank state.
func (s *Store) UpdateMediaVariants(ctx context.Context, item domain.Item, variants []domain.MediaVariant, width, height int) error {
	encoded, err := attributevalue.Marshal(variants)
	if err != nil {
		return err
	}
	_, err = s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(item.PK, item.SK),
		UpdateExpression:    aws.String("SET media_variants = :variants, media_w = :width, media_h = :height"),
		ConditionExpression: aws.String("attribute_exists(PK) AND item_id = :item"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":variants": encoded,
			":width":    &types.AttributeValueMemberN{Value: strconv.Itoa(width)},
			":height":   &types.AttributeValueMemberN{Value: strconv.Itoa(height)},
			":item":     &types.AttributeValueMemberS{Value: item.ItemID},
		},
	})
	return err
}

// SearchItems page-fills after DynamoDB applies the multi-term contains
// post-filter. Results retain partition order (newest publication/heart first).
func (s *Store) SearchItems(ctx context.Context, userID, prefix string, terms []string, limit int) ([]domain.Item, error) {
	if prefix != "I#" && prefix != "A#" {
		return nil, errors.New("search prefix must be I# or A#")
	}
	if limit < 1 || limit > 30 {
		limit = 30
	}
	values := map[string]types.AttributeValue{
		":pk": &types.AttributeValueMemberS{Value: domain.UserPK(userID)}, ":prefix": &types.AttributeValueMemberS{Value: prefix},
	}
	filters := make([]string, 0, len(terms)+1)
	for i, term := range terms {
		key := fmt.Sprintf(":term%d", i)
		values[key] = &types.AttributeValueMemberS{Value: term}
		filters = append(filters, "contains(search_text, "+key+")")
	}
	if prefix == "I#" {
		values[":now"] = &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Unix(), 10)}
		filters = append(filters, "#ttl > :now")
	}
	input := &dynamodb.QueryInput{
		TableName: aws.String(s.table), KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		FilterExpression: aws.String(strings.Join(filters, " AND ")), ExpressionAttributeValues: values,
		ScanIndexForward: aws.Bool(false), Limit: aws.Int32(100),
	}
	if prefix == "I#" {
		input.ExpressionAttributeNames = map[string]string{"#ttl": "ttl"}
	}
	items := []domain.Item{}
	seen := make(map[string]bool)
	for len(items) < limit {
		response, err := s.db.Query(ctx, input)
		if err != nil {
			return nil, err
		}
		var page []domain.Item
		if err := attributevalue.UnmarshalListOfMaps(response.Items, &page); err != nil {
			return nil, err
		}
		for _, item := range page {
			if seen[item.ItemID] {
				continue
			}
			seen[item.ItemID] = true
			if prefix == "A#" {
				item.ArchiveSK, item.Hearted, item.Archived, item.Read = item.SK, true, true, false
			}
			items = append(items, item)
			if len(items) == limit {
				break
			}
		}
		input.ExclusiveStartKey = response.LastEvaluatedKey
		if len(input.ExclusiveStartKey) == 0 {
			break
		}
	}
	if prefix == "I#" {
		if err := s.ResolveRead(ctx, userID, items); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// ResolveItemIDs returns currently searchable rows in the requested order.
// Live rows win while they exist; once their TTL passes, the permanent archive
// copy becomes the resolution target.
func (s *Store) ResolveItemIDs(ctx context.Context, userID string, ids []string) ([]domain.Item, error) {
	if len(ids) == 0 {
		return []domain.Item{}, nil
	}
	orderedIDs := make([]string, 0, len(ids))
	requested := make(map[string]bool, len(ids))
	for _, id := range ids {
		if requested[id] {
			continue
		}
		requested[id] = true
		orderedIDs = append(orderedIDs, id)
	}
	live, err := s.resolveLiveItemIDs(ctx, userID, orderedIDs)
	if err != nil {
		return nil, err
	}
	archive := make(map[string]domain.Item)
	if len(live) < len(orderedIDs) {
		archivedItems, archiveErr := s.ArchiveItems(ctx, userID)
		if archiveErr != nil {
			return nil, archiveErr
		}
		for _, item := range archivedItems {
			if !requested[item.ItemID] {
				continue
			}
			item.ArchiveSK, item.Hearted, item.Archived, item.Read = item.SK, true, true, false
			archive[item.ItemID] = item
		}
	}
	result := make([]domain.Item, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		if item, ok := live[id]; ok {
			result = append(result, item)
		} else if item, ok := archive[id]; ok {
			result = append(result, item)
		}
	}
	window := make([]domain.Item, 0, len(result))
	windowIndexes := make([]int, 0, len(result))
	for index := range result {
		if strings.HasPrefix(result[index].SK, "I#") {
			window = append(window, result[index])
			windowIndexes = append(windowIndexes, index)
		}
	}
	if err := s.ResolveRead(ctx, userID, window); err != nil {
		return nil, err
	}
	for index, resultIndex := range windowIndexes {
		result[resultIndex].Read = window[index].Read
	}
	return result, nil
}

func (s *Store) resolveLiveItemIDs(ctx context.Context, userID string, ids []string) (map[string]domain.Item, error) {
	pk := domain.UserPK(userID)
	identityKeys := make([]map[string]types.AttributeValue, 0, len(ids))
	for _, id := range ids {
		identityKeys = append(identityKeys, key(pk, domain.ItemIdentitySK(id)))
	}
	rows, err := s.batchGetRows(ctx, identityKeys)
	if err != nil {
		return nil, err
	}
	identities := make(map[string]domain.ItemIdentity, len(rows))
	for _, row := range rows {
		var identity domain.ItemIdentity
		if err := attributevalue.UnmarshalMap(row, &identity); err != nil {
			return nil, err
		}
		identities[strings.TrimPrefix(identity.SK, "D#")] = identity
	}

	now := time.Now().Unix()
	liveKeys := make([]map[string]types.AttributeValue, 0, len(identities))
	legacyIDs := make(map[string]bool)
	for _, id := range ids {
		identity, ok := identities[id]
		if !ok {
			legacyIDs[id] = true
			continue
		}
		if identity.TTL > now && strings.HasPrefix(identity.ItemSK, "I#") {
			liveKeys = append(liveKeys, key(pk, identity.ItemSK))
		}
	}
	liveRows, err := s.batchGetRows(ctx, liveKeys)
	if err != nil {
		return nil, err
	}
	live := make(map[string]domain.Item, len(liveRows))
	for _, row := range liveRows {
		var item domain.Item
		if err := attributevalue.UnmarshalMap(row, &item); err != nil {
			return nil, err
		}
		identity, ok := identities[item.ItemID]
		if ok && identity.ItemSK == item.SK && item.TTL > now {
			live[item.ItemID] = item
		}
	}
	if len(legacyIDs) > 0 {
		legacy, err := s.LiveItems(ctx, userID)
		if err != nil {
			return nil, err
		}
		for _, item := range legacy {
			if legacyIDs[item.ItemID] {
				live[item.ItemID] = item
			}
		}
	}
	return live, nil
}

func (s *Store) batchGetRows(ctx context.Context, keys []map[string]types.AttributeValue) ([]map[string]types.AttributeValue, error) {
	rows := make([]map[string]types.AttributeValue, 0, len(keys))
	for offset := 0; offset < len(keys); offset += 100 {
		pending := keys[offset:min(offset+100, len(keys))]
		for attempt := 0; len(pending) > 0 && attempt < 4; attempt++ {
			response, err := s.db.BatchGetItem(ctx, &dynamodb.BatchGetItemInput{RequestItems: map[string]types.KeysAndAttributes{s.table: {
				Keys: pending, ConsistentRead: aws.Bool(true),
			}}})
			if err != nil {
				return nil, err
			}
			rows = append(rows, response.Responses[s.table]...)
			pending = response.UnprocessedKeys[s.table].Keys
		}
		if len(pending) > 0 {
			return nil, fmt.Errorf("%d item lookups were throttled", len(pending))
		}
	}
	return rows, nil
}

func (s *Store) UserIDs(ctx context.Context) ([]string, error) {
	seen := make(map[string]bool)
	users := []string{}
	// FEED covers profiles created before the USER index marker was deployed.
	for _, indexPK := range []string{userIndexPK, feedIndexPK} {
		var start map[string]types.AttributeValue
		for {
			response, err := s.db.Query(ctx, &dynamodb.QueryInput{
				TableName: aws.String(s.table), IndexName: aws.String("by-next-fetch"),
				KeyConditionExpression:    aws.String("gsi1pk = :index"),
				ExpressionAttributeValues: map[string]types.AttributeValue{":index": &types.AttributeValueMemberS{Value: indexPK}},
				ProjectionExpression:      aws.String("PK"), ExclusiveStartKey: start,
			})
			if err != nil {
				return nil, err
			}
			for _, item := range response.Items {
				pk, ok := item["PK"].(*types.AttributeValueMemberS)
				if !ok || !strings.HasPrefix(pk.Value, "U#") {
					continue
				}
				userID := strings.TrimPrefix(pk.Value, "U#")
				if !seen[userID] {
					seen[userID] = true
					users = append(users, userID)
				}
			}
			start = response.LastEvaluatedKey
			if len(start) == 0 {
				break
			}
		}
	}
	return users, nil
}

const rankingUpdateConcurrency = 16

// UpdateItemRankings persists only attributes derived by rescore. Item rows
// also carry migration and archive state that may change independently, so a
// full replacement can erase vectors or race with another item action.
func (s *Store) UpdateItemRankings(ctx context.Context, items []domain.Item) error {
	if len(items) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan domain.Item)
	var workers sync.WaitGroup
	var firstErr error
	var firstErrOnce sync.Once
	for range min(rankingUpdateConcurrency, len(items)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				if err := s.updateItemRanking(ctx, item); err != nil {
					firstErrOnce.Do(func() {
						firstErr = err
						cancel()
					})
					return
				}
			}
		}()
	}

sendItems:
	for _, item := range items {
		select {
		case jobs <- item:
		case <-ctx.Done():
			break sendItems
		}
	}
	close(jobs)
	workers.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func (s *Store) updateItemRanking(ctx context.Context, item domain.Item) error {
	scoreValue, err := attributevalue.Marshal(item.Score)
	if err != nil {
		return fmt.Errorf("marshal score for %s: %w", item.ItemID, err)
	}
	sizeValue, err := attributevalue.Marshal(item.Size)
	if err != nil {
		return fmt.Errorf("marshal size for %s: %w", item.ItemID, err)
	}
	names := map[string]string{"#score": "score", "#size": "size", "#why": "why"}
	values := map[string]types.AttributeValue{":score": scoreValue, ":size": sizeValue}
	expression := "SET #score = :score, #size = :size REMOVE #why"
	if item.Why != nil {
		whyValue, marshalErr := attributevalue.Marshal(item.Why)
		if marshalErr != nil {
			return fmt.Errorf("marshal why for %s: %w", item.ItemID, marshalErr)
		}
		values[":why"] = whyValue
		expression = "SET #score = :score, #size = :size, #why = :why"
	}
	_, err = s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(item.PK, item.SK),
		UpdateExpression: aws.String(expression), ConditionExpression: aws.String("attribute_exists(PK)"),
		ExpressionAttributeNames: names, ExpressionAttributeValues: values,
	})
	var conditional *types.ConditionalCheckFailedException
	if errors.As(err, &conditional) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("update ranking for %s: %w", item.ItemID, err)
	}
	return nil
}

func (s *Store) StartReplay(ctx context.Context, userID, version string, at time.Time) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), "MODEL"),
		UpdateExpression: aws.String("SET replay_ts = :now, replay_version = :version"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":now":     &types.AttributeValueMemberS{Value: domain.Timestamp(at)},
			":version": &types.AttributeValueMemberS{Value: version},
		},
	})
	return err
}

func (s *Store) UpdateSignalEmbedding(ctx context.Context, userID, itemID string, vector []byte, version string) error {
	return s.updateStoredEmbedding(ctx, userID, domain.SignalSK(itemID), vector, version)
}

func (s *Store) UpdateBehaviourEmbedding(ctx context.Context, userID, itemID string, vector []byte, version string) error {
	return s.updateStoredEmbedding(ctx, userID, domain.BehaviourSK(itemID), vector, version)
}

func (s *Store) updateStoredEmbedding(ctx context.Context, userID, sk string, vector []byte, version string) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), sk),
		UpdateExpression:         aws.String("SET #vector = :vector, model_version = :version"),
		ConditionExpression:      aws.String("attribute_exists(PK)"),
		ExpressionAttributeNames: map[string]string{"#vector": "vector"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":vector":  &types.AttributeValueMemberB{Value: vector},
			":version": &types.AttributeValueMemberS{Value: version},
		},
	})
	var conditional *types.ConditionalCheckFailedException
	if errors.As(err, &conditional) {
		return nil
	}
	return err
}

func itemPageKey(item map[string]types.AttributeValue, order domain.Order) map[string]types.AttributeValue {
	result := map[string]types.AttributeValue{"PK": item["PK"], "SK": item["SK"]}
	if order == domain.OrderInterest {
		result["score"] = item["score"]
	}
	return result
}

func (s *Store) Item(ctx context.Context, userID, itemID string) (domain.Item, error) {
	now := time.Now().Unix()
	response, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.ItemIdentitySK(itemID)), ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return domain.Item{}, err
	}
	if len(response.Item) == 0 {
		return s.legacyItem(ctx, userID, itemID, now)
	}
	var identity domain.ItemIdentity
	if err := attributevalue.UnmarshalMap(response.Item, &identity); err != nil {
		return domain.Item{}, err
	}
	if identity.TTL <= now || !strings.HasPrefix(identity.ItemSK, "I#") {
		return domain.Item{}, ErrNotFound
	}
	response, err = s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), identity.ItemSK), ConsistentRead: aws.Bool(true),
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
	if item.ItemID != itemID || item.TTL <= now {
		return domain.Item{}, ErrNotFound
	}
	return s.loadItemVector(ctx, item, now)
}

func (s *Store) loadItemVector(ctx context.Context, item domain.Item, now int64) (domain.Item, error) {
	response, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table), Key: key(item.PK, domain.ItemVectorSK(item.ItemID)), ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return domain.Item{}, err
	}
	if len(response.Item) == 0 {
		return item, nil
	}
	var stored domain.ItemVector
	if err := attributevalue.UnmarshalMap(response.Item, &stored); err != nil {
		return domain.Item{}, err
	}
	if stored.TTL == 0 || stored.TTL > now {
		item.Vector = stored.Vector
	}
	return item, nil
}

// LoadItemVectors batch-loads vectors for bulk targeted work such as rescore.
// A missing V# row leaves any legacy in-row vector intact during migration.
func (s *Store) LoadItemVectors(ctx context.Context, userID string, items []domain.Item) error {
	pk := domain.UserPK(userID)
	keys := make([]map[string]types.AttributeValue, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item.ItemID] {
			continue
		}
		seen[item.ItemID] = true
		keys = append(keys, key(pk, domain.ItemVectorSK(item.ItemID)))
	}
	rows, err := s.batchGetRows(ctx, keys)
	if err != nil {
		return err
	}
	vectors := make(map[string][]byte, len(rows))
	now := time.Now().Unix()
	for _, row := range rows {
		var stored domain.ItemVector
		if err := attributevalue.UnmarshalMap(row, &stored); err != nil {
			return err
		}
		if stored.TTL == 0 || stored.TTL > now {
			vectors[strings.TrimPrefix(stored.SK, "V#")] = stored.Vector
		}
	}
	for index := range items {
		if vector, ok := vectors[items[index].ItemID]; ok {
			items[index].Vector = vector
		}
	}
	return nil
}

// PutItemVectorIfAbsent creates the split vector row used by targeted item
// reads. An unexpired row wins over a migration write so replay and ingest can
// safely run at the same time as a backfill.
func (s *Store) PutItemVectorIfAbsent(ctx context.Context, userID, itemID string, vector []byte, ttl int64) (bool, error) {
	if userID == "" || itemID == "" || len(vector) == 0 || ttl == 0 {
		return false, errors.New("item vector identity, data, and ttl are required")
	}
	encoded, err := attributevalue.MarshalMap(domain.ItemVector{
		PK: domain.UserPK(userID), SK: domain.ItemVectorSK(itemID), Vector: vector, TTL: ttl,
	})
	if err != nil {
		return false, err
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.table), Item: encoded,
		ConditionExpression:      aws.String("attribute_not_exists(PK) OR #ttl <= :now"),
		ExpressionAttributeNames: map[string]string{"#ttl": "ttl"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":now": &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Unix(), 10)},
		},
	})
	var conditional *types.ConditionalCheckFailedException
	if errors.As(err, &conditional) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) legacyItem(ctx context.Context, userID, itemID string, now int64) (domain.Item, error) {
	var start map[string]types.AttributeValue
	for {
		response, err := s.db.Query(ctx, &dynamodb.QueryInput{
			TableName: aws.String(s.table), KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
			FilterExpression: aws.String("item_id = :id AND #ttl > :now"), Limit: aws.Int32(100), ExclusiveStartKey: start,
			ConsistentRead:           aws.Bool(true),
			ExpressionAttributeNames: map[string]string{"#ttl": "ttl"},
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": &types.AttributeValueMemberS{Value: domain.UserPK(userID)}, ":prefix": &types.AttributeValueMemberS{Value: "I#"}, ":id": &types.AttributeValueMemberS{Value: itemID},
				":now": &types.AttributeValueMemberN{Value: strconv.FormatInt(now, 10)},
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
		TableName:                aws.String(s.table),
		KeyConditionExpression:   aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ProjectionExpression:     aws.String(listItemProjection.expression),
		ExpressionAttributeNames: listItemProjection.names,
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
		items[i].Archived = true
		items[i].Read = false
	}
	next, err := encodeCursor(response.LastEvaluatedKey)
	return items, next, err
}

func (s *Store) ArchiveItem(ctx context.Context, userID, itemID string) (domain.Item, error) {
	item, err := s.Item(ctx, userID, itemID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return domain.Item{}, err
	}
	if err == nil && item.ArchiveSK != "" {
		archive, archiveErr := s.archiveItemBySK(ctx, userID, item.ArchiveSK)
		if archiveErr == nil || !errors.Is(archiveErr, ErrNotFound) {
			return archive, archiveErr
		}
	}
	return s.scanArchiveItem(ctx, userID, itemID)
}

func (s *Store) scanArchiveItem(ctx context.Context, userID, itemID string) (domain.Item, error) {
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
			item.Archived = true
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
	item.Archived = true
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
	archive.SearchText = domain.DeriveSearchText(item.Title, item.Summary)
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
	archive.MediaVariants = nil
	if item.MediaKey != "" {
		destination := ArchiveMediaKey(userID, item.ItemID)
		copied, copyErr := s.copyContent(ctx, item.MediaKey, destination)
		if copyErr != nil {
			return "", 0, copyErr
		}
		if copied {
			archive.MediaKey = destination
			for _, variant := range item.MediaVariants {
				if variant.Key == item.MediaKey {
					archive.MediaVariants = append(archive.MediaVariants, domain.MediaVariant{Key: destination, Width: variant.Width, Height: variant.Height})
					continue
				}
				variantDestination := MediaVariantKey(destination, variant.Width)
				variantCopied, variantErr := s.copyContent(ctx, variant.Key, variantDestination)
				if variantErr != nil {
					return "", 0, variantErr
				}
				if variantCopied {
					archive.MediaVariants = append(archive.MediaVariants, domain.MediaVariant{Key: variantDestination, Width: variant.Width, Height: variant.Height})
				}
			}
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
		Vector: item.Vector, Title: item.Title, FeedID: item.FeedID, CreatedAt: domain.Timestamp(now), Source: "heart", ModelVersion: item.ModelVersion,
	})
	if err != nil {
		return "", 0, err
	}
	values := map[string]types.AttributeValue{
		":archive": &types.AttributeValueMemberS{Value: archiveSK},
		":one":     &types.AttributeValueMemberN{Value: "1"},
	}
	stateWrites := []types.TransactWriteItem{
		{Put: &types.Put{TableName: aws.String(s.table), Item: encodedArchive, ConditionExpression: aws.String("attribute_not_exists(SK)")}},
		{Update: &types.Update{
			TableName: aws.String(s.table), Key: key(domain.UserPK(userID), item.SK),
			UpdateExpression:          aws.String("SET archive_sk = :archive REMOVE hearted"),
			ConditionExpression:       aws.String("attribute_exists(PK) AND attribute_not_exists(archive_sk)"),
			ExpressionAttributeValues: map[string]types.AttributeValue{":archive": values[":archive"]},
		}},
	}
	baseWrites := append(append([]types.TransactWriteItem{}, stateWrites...), types.TransactWriteItem{Update: &types.Update{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), "PROFILE"),
		UpdateExpression: aws.String("ADD heart_count :one"), ExpressionAttributeValues: map[string]types.AttributeValue{":one": values[":one"]},
	}})
	withSignal := append(append([]types.TransactWriteItem{}, stateWrites...), types.TransactWriteItem{Update: &types.Update{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), "PROFILE"),
		UpdateExpression: aws.String("ADD heart_count :one, signal_count :one"), ExpressionAttributeValues: map[string]types.AttributeValue{":one": values[":one"]},
	}}, types.TransactWriteItem{Put: &types.Put{
		TableName: aws.String(s.table), Item: heartSignal, ConditionExpression: aws.String("attribute_not_exists(SK)"),
	}})
	err = s.transact(ctx, withSignal)
	signalCreated := err == nil
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
	if signalCreated {
		signal := domain.Signal{
			PK: domain.UserPK(userID), SK: domain.SignalSK(item.ItemID), ItemID: item.ItemID, Value: 1,
			Vector: item.Vector, Title: item.Title, FeedID: item.FeedID, CreatedAt: domain.Timestamp(now), Source: "heart", ModelVersion: item.ModelVersion,
		}
		if modelErr := s.applyExplicitModelUpdate(ctx, userID, nil, &signal, item.ModelVersion); modelErr != nil {
			slog.ErrorContext(ctx, "increment heart ranking model", "user", userID, "item_id", itemID, "error", modelErr)
		}
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
	stateWrites := append(append([]types.TransactWriteItem{}, baseWrites...), types.TransactWriteItem{Update: &types.Update{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), "PROFILE"),
		UpdateExpression:          aws.String("ADD heart_count :minus_one"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":minus_one": minusOne},
	}})
	withSignalDelete := append(append([]types.TransactWriteItem{}, baseWrites...), types.TransactWriteItem{Update: &types.Update{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), "PROFILE"),
		UpdateExpression:          aws.String("ADD heart_count :minus_one, signal_count :minus_one"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":minus_one": minusOne},
	}}, types.TransactWriteItem{Delete: &types.Delete{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.SignalSK(itemID)),
		ConditionExpression:       aws.String("#source = :heart"),
		ExpressionAttributeNames:  map[string]string{"#source": "source"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":heart": &types.AttributeValueMemberS{Value: "heart"}},
	}})
	err = s.transact(ctx, withSignalDelete)
	signalDeleted := err == nil
	if isTransactionCanceled(err) {
		// Missing or explicit signals must survive un-hearting.
		err = s.transact(ctx, stateWrites)
	}
	if err != nil {
		if _, currentErr := s.ArchiveItem(ctx, userID, itemID); errors.Is(currentErr, ErrNotFound) {
			count, countErr := s.heartCount(ctx, userID)
			return "", count, countErr
		}
		return "", 0, err
	}
	if signalDeleted {
		old := domain.Signal{
			PK: domain.UserPK(userID), SK: domain.SignalSK(itemID), ItemID: itemID, Value: 1,
			Vector: archive.Vector, Title: archive.Title, FeedID: archive.FeedID, CreatedAt: archive.HeartedTS, Source: "heart", ModelVersion: archive.ModelVersion,
		}
		if modelErr := s.applyExplicitModelUpdate(ctx, userID, &old, nil, archive.ModelVersion); modelErr != nil {
			slog.ErrorContext(ctx, "decrement heart ranking model", "user", userID, "item_id", itemID, "error", modelErr)
		}
	}
	s.deleteArchiveContent(ctx, userID, itemID, archive.MediaVariants)
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

func transactionConditionFailed(err error) bool {
	var canceled *types.TransactionCanceledException
	if !errors.As(err, &canceled) {
		return false
	}
	for index, reason := range canceled.CancellationReasons {
		if index < 3 && aws.ToString(reason.Code) == "ConditionalCheckFailed" {
			return true
		}
	}
	return false
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

func (s *Store) deleteArchiveContent(ctx context.Context, userID, itemID string, variants []domain.MediaVariant) {
	if s.s3 == nil || s.bucket == "" {
		return
	}
	keys := []string{ArchiveBodyKey(userID, itemID), ArchiveMediaKey(userID, itemID)}
	for _, variant := range variants {
		if variant.Key != "" && variant.Key != ArchiveMediaKey(userID, itemID) {
			keys = append(keys, variant.Key)
		}
	}
	for _, objectKey := range keys {
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
			}, ExclusiveStartKey: start, ConsistentRead: aws.Bool(true),
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

func (s *Store) Behaviours(ctx context.Context, userID string) ([]domain.Behaviour, error) {
	result := []domain.Behaviour{}
	var start map[string]types.AttributeValue
	for {
		response, err := s.db.Query(ctx, &dynamodb.QueryInput{
			TableName: aws.String(s.table), KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": &types.AttributeValueMemberS{Value: domain.UserPK(userID)}, ":prefix": &types.AttributeValueMemberS{Value: "B#"},
			}, ExclusiveStartKey: start, ConsistentRead: aws.Bool(true),
		})
		if err != nil {
			return nil, err
		}
		var page []domain.Behaviour
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

func (s *Store) Behaviour(ctx context.Context, userID, itemID string) (domain.Behaviour, error) {
	response, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.BehaviourSK(itemID)), ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return domain.Behaviour{}, err
	}
	if len(response.Item) == 0 {
		return domain.Behaviour{}, ErrNotFound
	}
	var row domain.Behaviour
	return row, attributevalue.UnmarshalMap(response.Item, &row)
}

type BehaviourEvent struct {
	Opened         bool
	DwellMS        *int64
	ClickedThrough bool
	Shared         bool
}

// RecordBehaviour uses independent monotonic updates: flags only ever become
// true, and the dwell update is conditional on increasing the stored maximum.
func (s *Store) RecordBehaviour(ctx context.Context, userID string, item domain.Item, event BehaviourEvent) error {
	now := time.Now().UTC()
	names := map[string]string{
		"#vector": "vector",
		"#ttl":    "ttl",
	}
	values := map[string]types.AttributeValue{
		":opened": &types.AttributeValueMemberS{Value: domain.Timestamp(now)},
		":item":   &types.AttributeValueMemberS{Value: item.ItemID},
		":feed":   &types.AttributeValueMemberS{Value: item.FeedID},
		":title":  &types.AttributeValueMemberS{Value: item.Title},
		":vector": &types.AttributeValueMemberB{Value: item.Vector},
		":ttl":    &types.AttributeValueMemberN{Value: strconv.FormatInt(now.Add(90*24*time.Hour).Unix(), 10)},
	}
	sets := []string{
		"opened_at = if_not_exists(opened_at, :opened)",
		"item_id = if_not_exists(item_id, :item)",
		"feed_id = if_not_exists(feed_id, :feed)",
		"title = if_not_exists(title, :title)",
		"#vector = if_not_exists(#vector, :vector)",
		"#ttl = if_not_exists(#ttl, :ttl)",
	}
	if item.ModelVersion != "" {
		values[":version"] = &types.AttributeValueMemberS{Value: item.ModelVersion}
		sets = append(sets, "model_version = if_not_exists(model_version, :version)")
	}
	if event.Opened {
		values[":open"] = &types.AttributeValueMemberBOOL{Value: true}
		sets = append(sets, "opened = :open")
	}
	if event.ClickedThrough {
		values[":clicked"] = &types.AttributeValueMemberBOOL{Value: true}
		sets = append(sets, "clicked_through = :clicked")
	}
	if event.Shared {
		names["#shared"] = "shared"
		values[":shared"] = &types.AttributeValueMemberBOOL{Value: true}
		sets = append(sets, "#shared = :shared")
	}
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.BehaviourSK(item.ItemID)),
		UpdateExpression: aws.String("SET " + strings.Join(sets, ", ")), ExpressionAttributeNames: names, ExpressionAttributeValues: values,
	})
	if err != nil {
		return err
	}
	if event.DwellMS == nil {
		return nil
	}
	_, err = s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.BehaviourSK(item.ItemID)),
		UpdateExpression:          aws.String("SET dwell_ms = :dwell"),
		ConditionExpression:       aws.String("attribute_not_exists(dwell_ms) OR dwell_ms < :dwell"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":dwell": &types.AttributeValueMemberN{Value: strconv.FormatInt(*event.DwellMS, 10)}},
	})
	var conditional *types.ConditionalCheckFailedException
	if errors.As(err, &conditional) {
		return nil
	}
	return err
}

func (s *Store) Model(ctx context.Context, userID string) (domain.Model, error) {
	response, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), "MODEL"), ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return domain.Model{}, err
	}
	if len(response.Item) == 0 {
		return domain.Model{}, score.ErrModelNotFound
	}
	var model domain.Model
	return model, attributevalue.UnmarshalMap(response.Item, &model)
}

func (s *Store) PutModel(ctx context.Context, model domain.Model) error {
	encoded, err := attributevalue.MarshalMap(model)
	if err != nil {
		return err
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(s.table), Item: encoded})
	return err
}

func (s *Store) RecomputeModel(ctx context.Context, userID, version string) (domain.Model, error) {
	for attempt := 0; attempt < 4; attempt++ {
		previous, getErr := s.Model(ctx, userID)
		if getErr != nil && !errors.Is(getErr, score.ErrModelNotFound) {
			return domain.Model{}, getErr
		}
		signals, err := s.Signals(ctx, userID)
		if err != nil {
			return domain.Model{}, err
		}
		behaviours, err := s.Behaviours(ctx, userID)
		if err != nil {
			return domain.Model{}, err
		}
		model := score.BuildModel(userID, signals, behaviours, time.Now().UTC(), version)
		if getErr == nil {
			model.ReplayTS, model.ReplayVersion = previous.ReplayTS, previous.ReplayVersion
		}
		err = s.putModelIfUnchanged(ctx, model, previous.ComputedAt)
		var conditional *types.ConditionalCheckFailedException
		if errors.As(err, &conditional) {
			continue
		}
		return model, err
	}
	return domain.Model{}, errors.New("ranking model changed too frequently")
}

func (s *Store) SignalValues(ctx context.Context, userID string, itemIDs []string) (map[string]int, error) {
	result := make(map[string]int, len(itemIDs))
	if len(itemIDs) == 0 {
		return result, nil
	}
	keys := make([]map[string]types.AttributeValue, 0, len(itemIDs))
	seen := make(map[string]bool, len(itemIDs))
	for _, itemID := range itemIDs {
		if seen[itemID] {
			continue
		}
		seen[itemID] = true
		keys = append(keys, key(domain.UserPK(userID), domain.SignalSK(itemID)))
	}
	pending := keys
	for attempt := 0; len(pending) > 0 && attempt < 4; attempt++ {
		response, err := s.db.BatchGetItem(ctx, &dynamodb.BatchGetItemInput{RequestItems: map[string]types.KeysAndAttributes{s.table: {
			Keys: pending, ProjectionExpression: aws.String("SK, #value"), ExpressionAttributeNames: map[string]string{"#value": "value"},
		}}})
		if err != nil {
			return nil, err
		}
		for _, row := range response.Responses[s.table] {
			var signal struct {
				SK    string `dynamodbav:"SK"`
				Value int    `dynamodbav:"value"`
			}
			if err := attributevalue.UnmarshalMap(row, &signal); err != nil {
				return nil, err
			}
			result[strings.TrimPrefix(signal.SK, "S#")] = signal.Value
		}
		pending = response.UnprocessedKeys[s.table].Keys
	}
	if len(pending) > 0 {
		return nil, fmt.Errorf("%d signal lookups were throttled", len(pending))
	}
	return result, nil
}

func (s *Store) SetSignal(ctx context.Context, userID string, item domain.Item, value int) error {
	heartSource := false
	if value == 0 {
		if item.ArchiveSK != "" {
			value = 1
			heartSource = true
		} else {
			response, err := s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
				TableName: aws.String(s.table), Key: key(domain.UserPK(userID), domain.SignalSK(item.ItemID)), ReturnValues: types.ReturnValueAllOld,
			})
			if err != nil || len(response.Attributes) == 0 {
				return err
			}
			var old domain.Signal
			if err := attributevalue.UnmarshalMap(response.Attributes, &old); err != nil {
				return err
			}
			if err := s.addSignalCount(ctx, userID, -1); err != nil {
				return err
			}
			return s.applyExplicitModelUpdate(ctx, userID, &old, nil, item.ModelVersion)
		}
	}
	signal := domain.Signal{
		PK: domain.UserPK(userID), SK: domain.SignalSK(item.ItemID), ItemID: item.ItemID, Value: value,
		Vector: item.Vector, Title: item.Title, FeedID: item.FeedID, CreatedAt: domain.Timestamp(time.Now()), ModelVersion: item.ModelVersion,
	}
	if heartSource {
		signal.Source = "heart"
	}
	encoded, err := attributevalue.MarshalMap(signal)
	if err != nil {
		return err
	}
	response, err := s.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(s.table), Item: encoded, ReturnValues: types.ReturnValueAllOld})
	if err != nil {
		return err
	}
	var old *domain.Signal
	if len(response.Attributes) > 0 {
		old = &domain.Signal{}
		if err := attributevalue.UnmarshalMap(response.Attributes, old); err != nil {
			return err
		}
	} else if err := s.addSignalCount(ctx, userID, 1); err != nil {
		return err
	}
	return s.applyExplicitModelUpdate(ctx, userID, old, &signal, item.ModelVersion)
}

func (s *Store) applyExplicitModelUpdate(ctx context.Context, userID string, oldSignal, newSignal *domain.Signal, version string) error {
	var behaviour *domain.Behaviour
	if row, getErr := s.Behaviour(ctx, userID, firstSignalItemID(oldSignal, newSignal)); getErr == nil {
		behaviour = &row
	} else if !errors.Is(getErr, ErrNotFound) {
		return getErr
	}
	for attempt := 0; attempt < 4; attempt++ {
		model, err := s.Model(ctx, userID)
		if err != nil {
			if !errors.Is(err, score.ErrModelNotFound) {
				return err
			}
			if version == "" {
				version = score.LegacyEmbeddingVersion
			}
			_, err = s.RecomputeModel(ctx, userID, version)
			return err
		}
		if version == "" {
			version = model.Version
		}
		if model.Version != version && version != "" {
			_, err = s.RecomputeModel(ctx, userID, version)
			return err
		}
		previousComputedAt := model.ComputedAt
		if !score.ApplyExplicit(&model, oldSignal, newSignal, behaviour, time.Now().UTC()) {
			_, err = s.RecomputeModel(ctx, userID, version)
			return err
		}
		err = s.putModelIfUnchanged(ctx, model, previousComputedAt)
		var conditional *types.ConditionalCheckFailedException
		if errors.As(err, &conditional) {
			continue
		}
		return err
	}
	return errors.New("ranking model changed too frequently")
}

func (s *Store) putModelIfUnchanged(ctx context.Context, model domain.Model, previousComputedAt string) error {
	encoded, err := attributevalue.MarshalMap(model)
	if err != nil {
		return err
	}
	input := &dynamodb.PutItemInput{
		TableName: aws.String(s.table), Item: encoded,
		ExpressionAttributeNames: map[string]string{"#computed": "computed_at"},
	}
	if previousComputedAt == "" {
		input.ConditionExpression = aws.String("attribute_not_exists(#computed)")
	} else {
		input.ConditionExpression = aws.String("#computed = :previous")
		input.ExpressionAttributeValues = map[string]types.AttributeValue{
			":previous": &types.AttributeValueMemberS{Value: previousComputedAt},
		}
	}
	_, err = s.db.PutItem(ctx, input)
	return err
}

func firstSignalItemID(oldSignal, newSignal *domain.Signal) string {
	if newSignal != nil {
		return newSignal.ItemID
	}
	if oldSignal != nil {
		return oldSignal.ItemID
	}
	return ""
}

func (s *Store) addSignalCount(ctx context.Context, userID string, delta int) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table), Key: key(domain.UserPK(userID), "PROFILE"),
		UpdateExpression:          aws.String("ADD signal_count :delta"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":delta": &types.AttributeValueMemberN{Value: strconv.Itoa(delta)}},
	})
	return err
}

func (s *Store) ResolveRead(ctx context.Context, userID string, items []domain.Item) error {
	if len(items) == 0 {
		return nil
	}
	keys := make([]map[string]types.AttributeValue, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item.ItemID] {
			continue
		}
		seen[item.ItemID] = true
		keys = append(keys, key(domain.UserPK(userID), domain.ReadSK(item.ItemID)))
	}
	read := make(map[string]bool)
	for offset := 0; offset < len(keys); offset += 100 {
		pending := keys[offset:min(offset+100, len(keys))]
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

func (s *Store) ContentExists(ctx context.Context, objectKey string) (bool, error) {
	if objectKey == "" {
		return false, nil
	}
	_, err := s.s3.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objectKey)})
	if err == nil {
		return true, nil
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "404") {
		return false, nil
	}
	return false, err
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
	item.MediaVariants = append([]domain.MediaVariant(nil), item.MediaVariants...)
	for index := range item.MediaVariants {
		item.MediaVariants[index].Key = s.ContentURL(item.MediaVariants[index].Key)
	}
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
