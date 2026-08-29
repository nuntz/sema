package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/nuntz/sema/internal/auth"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
	"github.com/nuntz/sema/internal/vectorstore"
)

type apiDynamo struct {
	*dynamodb.Client
	batchGet func(*dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error)
	delete   func(*dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error)
	getItem  func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error)
	putItem  func(*dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error)
	query    func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error)
	update   func(*dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error)
}

func TestSimilarMatchesExcludeSelfWeakAndRespectLimit(t *testing.T) {
	got := similarMatches([]vectorstore.Match{
		{Key: "self", Similarity: 100},
		{Key: "weak", Similarity: 39},
		{Key: "one", Similarity: 91},
		{Key: "two", Similarity: 78},
	}, "self", 1)
	if len(got) != 1 || got[0].Key != "one" {
		t.Fatalf("similar matches = %#v", got)
	}
}

func TestResponseEncodesNilCollectionsAsArrays(t *testing.T) {
	got := response(http.StatusOK, struct {
		Items      []string       `json:"items"`
		Nested     map[string]any `json:"nested"`
		NextCursor *string        `json:"next_cursor"`
		Opaque     []byte         `json:"opaque"`
	}{Nested: map[string]any{"items": []int(nil)}})
	want := `{"items":[],"nested":{"items":[]},"next_cursor":null,"opaque":null}`
	if got.Body != want {
		t.Fatalf("response body = %s, want %s", got.Body, want)
	}
}

func TestGetSearchEncodesEmptyGroupsAsArrays(t *testing.T) {
	db := &apiDynamo{query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		return &dynamodb.QueryOutput{}, nil
	}}
	server := &server{store: store.New(db, nil, "table", "", "")}
	got := server.getSearch(context.Background(), "user", map[string]string{"q": "pulumi"})
	if got.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", got.StatusCode, got.Body)
	}
	var body struct {
		Matches struct {
			Window  json.RawMessage `json:"window"`
			Archive json.RawMessage `json:"archive"`
		} `json:"matches"`
		Related struct {
			Window  json.RawMessage `json:"window"`
			Archive json.RawMessage `json:"archive"`
		} `json:"related"`
	}
	if err := json.Unmarshal([]byte(got.Body), &body); err != nil {
		t.Fatal(err)
	}
	for name, encoded := range map[string]json.RawMessage{
		"matches.window": body.Matches.Window, "matches.archive": body.Matches.Archive,
		"related.window": body.Related.Window, "related.archive": body.Related.Archive,
	} {
		if string(encoded) != "[]" {
			t.Errorf("%s = %s, want []", name, encoded)
		}
	}
}

func TestGetItemsReturnsUnreadPageWithReadAnchor(t *testing.T) {
	now := time.Now().UTC()
	marshal := func(id string, published time.Time) map[string]types.AttributeValue {
		item, err := attributevalue.MarshalMap(domain.Item{
			PK: domain.UserPK("user"), SK: domain.ItemSK(published, id), ItemID: id,
			FeedID: "feed", URL: "https://example.com/" + id, Title: id,
			PublishedTS: domain.Timestamp(published), FetchedTS: domain.Timestamp(now),
			SummarySource: "", Size: "S", TTL: now.Add(time.Hour).Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	db := &apiDynamo{
		query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{
				marshal("new", now),
				marshal("anchor", now.Add(-time.Minute)),
				marshal("old", now.Add(-2*time.Minute)),
			}}, nil
		},
		batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
			request := input.RequestItems["table"]
			if aws.ToString(request.ProjectionExpression) == "SK" {
				return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{
					"table": {{"SK": &types.AttributeValueMemberS{Value: domain.ReadSK("anchor")}}},
				}}, nil
			}
			return &dynamodb.BatchGetItemOutput{}, nil
		},
	}
	server := &server{
		store: store.New(db, nil, "table", "", ""),
		feedCache: map[string]cachedFeedList{
			"user": {loaded: time.Now(), feeds: []domain.Feed{{FeedID: "feed"}}},
		},
	}
	got := server.getItems(context.Background(), "user", map[string]string{
		"order": "chrono", "limit": "2",
	})
	if got.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", got.StatusCode, got.Body)
	}
	var body struct {
		Items      []domain.Item `json:"items"`
		ReadAnchor struct {
			ItemID      string `json:"item_id"`
			PublishedTS string `json:"published_ts"`
		} `json:"read_anchor"`
	}
	if err := json.Unmarshal([]byte(got.Body), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 2 || body.Items[0].ItemID != "new" || body.Items[1].ItemID != "old" {
		t.Fatalf("items = %#v", body.Items)
	}
	if body.ReadAnchor.ItemID != "anchor" || body.ReadAnchor.PublishedTS == "" {
		t.Fatalf("read anchor = %#v", body.ReadAnchor)
	}
}

type apiQueue struct {
	input *sqs.SendMessageBatchInput
	send  func(*sqs.SendMessageBatchInput) (*sqs.SendMessageBatchOutput, error)
}

func (q *apiQueue) SendMessageBatch(_ context.Context, input *sqs.SendMessageBatchInput, _ ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
	if q.send != nil {
		return q.send(input)
	}
	q.input = input
	return &sqs.SendMessageBatchOutput{}, nil
}

func (f *apiDynamo) BatchGetItem(_ context.Context, input *dynamodb.BatchGetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error) {
	return f.batchGet(input)
}

func (f *apiDynamo) DeleteItem(_ context.Context, input *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	return f.delete(input)
}

func (f *apiDynamo) GetItem(_ context.Context, input *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if f.getItem != nil {
		return f.getItem(input)
	}
	return &dynamodb.GetItemOutput{}, nil
}

func (f *apiDynamo) PutItem(_ context.Context, input *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	return f.putItem(input)
}

func (f *apiDynamo) Query(_ context.Context, input *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	return f.query(input)
}

func (f *apiDynamo) UpdateItem(_ context.Context, input *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	if f.update == nil {
		return &dynamodb.UpdateItemOutput{}, nil
	}
	return f.update(input)
}

func captureFeedUpdate(t *testing.T, destination *domain.Feed) func(*dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
	t.Helper()
	return func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
		row := map[string]types.AttributeValue{"PK": input.Key["PK"], "SK": input.Key["SK"]}
		for alias, name := range input.ExpressionAttributeNames {
			placeholder := strings.Replace(alias, "#", ":", 1)
			if value, ok := input.ExpressionAttributeValues[placeholder]; ok {
				row[name] = value
			}
		}
		if err := attributevalue.UnmarshalMap(row, destination); err != nil {
			t.Fatal(err)
		}
		return &dynamodb.UpdateItemOutput{}, nil
	}
}

func TestSessionRoutesCreateAndDeleteFirstPartySession(t *testing.T) {
	profile, err := attributevalue.MarshalMap(domain.User{
		PK: "U#reader", SK: "PROFILE", Email: "reader@example.com", OrderPref: domain.OrderInterest,
	})
	if err != nil {
		t.Fatal(err)
	}
	var sessionItem map[string]types.AttributeValue
	var deleted map[string]types.AttributeValue
	updateCount := 0
	db := &apiDynamo{
		getItem: func(input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			sk := input.Key["SK"].(*types.AttributeValueMemberS).Value
			if sk == "PROFILE" {
				return &dynamodb.GetItemOutput{Item: profile}, nil
			}
			if sk == "MODEL" {
				return &dynamodb.GetItemOutput{}, nil
			}
			return &dynamodb.GetItemOutput{Item: sessionItem}, nil
		},
		putItem: func(input *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
			sessionItem = input.Item
			return &dynamodb.PutItemOutput{}, nil
		},
		query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			return &dynamodb.QueryOutput{}, nil
		},
		update: func(*dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			updateCount++
			return &dynamodb.UpdateItemOutput{}, nil
		},
		delete: func(input *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error) {
			deleted = input.Key
			return &dynamodb.DeleteItemOutput{}, nil
		},
	}
	repository := store.New(db, nil, "table", "", "")
	server := &server{
		store: repository, sessions: auth.NewSessions(repository),
		verifyGoogle: func(_ context.Context, credential string) (auth.Claims, error) {
			if credential != "google-token" {
				t.Fatalf("credential = %q", credential)
			}
			return auth.Claims{Subject: "reader", Email: "reader@example.com"}, nil
		},
	}
	created, err := server.handle(context.Background(), apiRequest(http.MethodPost, "/api/session", `{"credential":"google-token"}`))
	if err != nil || created.StatusCode != http.StatusOK {
		t.Fatalf("create session = %d, %s, %v", created.StatusCode, created.Body, err)
	}
	if len(created.Cookies) != 1 || !strings.HasPrefix(created.Cookies[0], auth.SessionCookieName+"=") {
		t.Fatalf("create cookies = %#v", created.Cookies)
	}
	setCookie, err := http.ParseSetCookie(created.Cookies[0])
	if err != nil {
		t.Fatal(err)
	}
	if setCookie.Path != "/api" || setCookie.MaxAge != int(auth.SessionLifetime.Seconds()) || !setCookie.Secure || !setCookie.HttpOnly || setCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %#v", setCookie)
	}
	if sessionItem["PK"].(*types.AttributeValueMemberS).Value == "SESSION#"+setCookie.Value {
		t.Fatal("session record used the raw credential as its key")
	}

	authenticated := apiRequest(http.MethodGet, "/api/not-found", "")
	authenticated.Cookies = []string{auth.SessionCookieName + "=" + setCookie.Value}
	notFound, err := server.handle(context.Background(), authenticated)
	if err != nil || notFound.StatusCode != http.StatusNotFound {
		t.Fatalf("authenticated response = %d, %s, %v", notFound.StatusCode, notFound.Body, err)
	}
	if updateCount != 1 {
		t.Fatalf("profile updates = %d, want login upsert only", updateCount)
	}

	items := apiRequest(http.MethodGet, "/api/items//", "")
	items.Cookies = []string{auth.SessionCookieName + "=" + setCookie.Value}
	itemsResponse, err := server.handle(context.Background(), items)
	if err != nil || itemsResponse.StatusCode != http.StatusOK {
		t.Fatalf("normalized items response = %d, %s, %v", itemsResponse.StatusCode, itemsResponse.Body, err)
	}

	request := apiRequest(http.MethodDelete, "/api/session", "")
	request.Cookies = []string{auth.SessionCookieName + "=" + setCookie.Value}
	cleared, err := server.handle(context.Background(), request)
	if err != nil || cleared.StatusCode != http.StatusNoContent {
		t.Fatalf("delete session = %d, %s, %v", cleared.StatusCode, cleared.Body, err)
	}
	if len(cleared.Cookies) != 1 || !strings.Contains(cleared.Cookies[0], "Max-Age=0") {
		t.Fatalf("clear cookies = %#v", cleared.Cookies)
	}
	if deleted["PK"].(*types.AttributeValueMemberS).Value != sessionItem["PK"].(*types.AttributeValueMemberS).Value {
		t.Fatalf("deleted key = %#v, session = %#v", deleted, sessionItem)
	}
}

func TestHandleRejectsCrossSiteMutationsBeforeAuthentication(t *testing.T) {
	request := apiRequest(http.MethodPost, "/api/session", `{"credential":"ignored"}`)
	request.Headers = map[string]string{"Sec-Fetch-Site": "cross-site"}
	got, err := (&server{}).handle(context.Background(), request)
	if err != nil || got.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site response = %d, %s, %v", got.StatusCode, got.Body, err)
	}
	for _, site := range []string{"", "same-origin", "none"} {
		if crossSiteMutation(http.MethodPost, map[string]string{"sec-fetch-site": site}) {
			t.Errorf("site %q was rejected", site)
		}
	}
	if crossSiteMutation(http.MethodGet, map[string]string{"sec-fetch-site": "cross-site"}) {
		t.Fatal("cross-site GET was rejected")
	}
}

func apiRequest(method, path, body string) events.APIGatewayV2HTTPRequest {
	return events.APIGatewayV2HTTPRequest{
		RawPath: path, Body: body,
		RequestContext: events.APIGatewayV2HTTPRequestContext{HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{Method: method}},
	}
}

func TestParseIncludeRead(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  bool
		err   bool
	}{
		{name: "default", value: "", want: false},
		{name: "unread only", value: "false", want: false},
		{name: "all items", value: "true", want: true},
		{name: "invalid", value: "yes", err: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseIncludeRead(test.value)
			if (err != nil) != test.err || got != test.want {
				t.Fatalf("parseIncludeRead(%q) = %v, %v", test.value, got, err)
			}
		})
	}
}

func TestNormalizeTagsAndFeedStatus(t *testing.T) {
	tags, err := normalizeTags([]string{" Dev ", "dev", "ニュース"})
	if err != nil || len(tags) != 2 || tags[0] != "dev" || tags[1] != "ニュース" {
		t.Fatalf("tags = %#v, err = %v", tags, err)
	}
	if got := feedStatus(domain.Feed{ErrorCount: 3}); got != "broken" {
		t.Fatalf("broken status = %q", got)
	}
	if got := feedStatus(domain.Feed{Muted: true, ErrorCount: 3}); got != "muted" {
		t.Fatalf("muted status = %q", got)
	}
}

func TestAddRedditFeedUsesCanonicalSortAndStableIdentity(t *testing.T) {
	var saved domain.Feed
	db := &apiDynamo{
		getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{}, nil
		},
		query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			return &dynamodb.QueryOutput{}, nil
		},
		update: captureFeedUpdate(t, &saved),
	}
	queue := &apiQueue{}
	server := &server{store: store.New(db, nil, "table", "", ""), queue: queue}
	got := server.addFeed(context.Background(), "user", `{
		"feed_url":"https://old.reddit.com/r/Castles/new.rss",
		"connector":"reddit",
		"title":"untrusted listing title",
		"site_url":"https://example.com/"
	}`)
	if got.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", got.StatusCode, got.Body)
	}
	wantID := domain.FeedID("https://www.reddit.com/r/castles/")
	if saved.FeedID != wantID || saved.Connector != domain.ConnectorReddit || saved.URL != "https://www.reddit.com/r/castles/new.rss" || saved.SiteURL != "https://www.reddit.com/r/castles/" || saved.Title != "r/castles" || saved.FetchIntervalH != 1 {
		t.Fatalf("saved feed = %#v", saved)
	}
	if queue.input == nil || len(queue.input.Entries) != 1 {
		t.Fatalf("queue input = %#v", queue.input)
	}
}

func TestPatchRedditCollectionKeepsIdentityAndQueuesFetch(t *testing.T) {
	feed := domain.Feed{
		PK: domain.UserPK("user"), SK: domain.FeedSK("legacy-id"), FeedID: "legacy-id",
		Connector: domain.ConnectorReddit, URL: "https://www.reddit.com/r/castles/top.rss?t=day",
		Title: "r/castles", SiteURL: "https://www.reddit.com/r/castles/", FetchIntervalH: 24,
		ETag: `"top"`, LastModified: "yesterday", ErrorCount: 3, LastError: "forbidden",
	}
	item, err := attributevalue.MarshalMap(feed)
	if err != nil {
		t.Fatal(err)
	}
	var saved domain.Feed
	db := &apiDynamo{
		getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: item}, nil
		},
		update: captureFeedUpdate(t, &saved),
	}
	queue := &apiQueue{}
	server := &server{store: store.New(db, nil, "table", "", ""), queue: queue}
	got := server.patchFeed(context.Background(), "user", "legacy-id", `{"url":"https://www.reddit.com/r/castles/.rss"}`)
	if got.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", got.StatusCode, got.Body)
	}
	if saved.FeedID != "legacy-id" || saved.URL != "https://www.reddit.com/r/castles/.rss" || saved.FetchIntervalH != 3 || saved.ETag != "" || saved.LastModified != "" || saved.ErrorCount != 0 || saved.LastError != "" || saved.LastStatus != "queued" {
		t.Fatalf("saved feed = %#v", saved)
	}
	if queue.input == nil || len(queue.input.Entries) != 1 {
		t.Fatalf("queue input = %#v", queue.input)
	}
}

func TestImportFeedsPreservesMutedFeedSettings(t *testing.T) {
	feedURL := "https://example.com/feed.xml"
	feedID := domain.FeedID(feedURL)
	existing := domain.Feed{
		PK: domain.UserPK("user"), SK: domain.FeedSK(feedID), FeedID: feedID,
		URL: feedURL, CustomTitle: "My title", Tags: []string{"saved"}, Muted: true,
		FetchIntervalH: 24, NextFetchAt: "2026-08-27T12:00:00Z",
	}
	item, err := attributevalue.MarshalMap(existing)
	if err != nil {
		t.Fatal(err)
	}
	var saved domain.Feed
	db := &apiDynamo{
		getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: item}, nil
		},
		update: captureFeedUpdate(t, &saved),
	}
	queue := &apiQueue{}
	server := &server{store: store.New(db, nil, "table", "", ""), queue: queue}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "subscriptions.opml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = part.Write([]byte(`<?xml version="1.0"?><opml version="2.0"><body><outline text="Imported title" xmlUrl="https://example.com/feed.xml" category="imported" sema:interval="1h"/></body></opml>`))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	got := server.importFeeds(context.Background(), "user", events.APIGatewayV2HTTPRequest{
		Body: body.String(), Headers: map[string]string{"content-type": writer.FormDataContentType()},
	})
	if got.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", got.StatusCode, got.Body)
	}
	if !saved.Muted || saved.FetchIntervalH != 24 || saved.NextFetchAt != existing.NextFetchAt {
		t.Fatalf("saved feed settings = %#v", saved)
	}
	if saved.CustomTitle != "My title" || strings.Join(saved.Tags, ",") != "imported,saved" {
		t.Fatalf("saved feed presentation = %#v", saved)
	}
	if queue.input != nil {
		t.Fatalf("muted feed was enqueued: %#v", queue.input)
	}
}

func TestImportFeedsInvalidatesCacheAfterPartialWriteFailure(t *testing.T) {
	writeFailure := errors.New("write failed")
	db := &apiDynamo{
		getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{}, nil
		},
		update: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			var feed domain.Feed
			capture := captureFeedUpdate(t, &feed)
			if _, err := capture(input); err != nil {
				return nil, err
			}
			if strings.Contains(feed.URL, "failed") {
				return nil, writeFailure
			}
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}
	server := &server{
		store: store.New(db, nil, "table", "", ""), queue: &apiQueue{},
		feedCache:       map[string]cachedFeedList{"user": {}},
		feedDetailCache: map[string]cachedFeedList{"user": {}},
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "subscriptions.opml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = part.Write([]byte(`<?xml version="1.0"?><opml version="2.0"><body><outline xmlUrl="https://example.com/saved.xml"/><outline xmlUrl="https://example.com/failed.xml"/></body></opml>`))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	got := server.importFeeds(context.Background(), "user", events.APIGatewayV2HTTPRequest{
		Body: body.String(), Headers: map[string]string{"content-type": writer.FormDataContentType()},
	})
	if got.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", got.StatusCode, got.Body)
	}
	if _, ok := server.feedCache["user"]; ok {
		t.Fatal("feed list cache was not invalidated")
	}
	if _, ok := server.feedDetailCache["user"]; ok {
		t.Fatal("feed detail cache was not invalidated")
	}
}

func TestEnqueueFeedsJoinsBatchErrors(t *testing.T) {
	first := errors.New("first batch failed")
	second := errors.New("second batch failed")
	queue := &apiQueue{send: func(input *sqs.SendMessageBatchInput) (*sqs.SendMessageBatchOutput, error) {
		if aws.ToString(input.Entries[0].Id) == "feed-0" {
			return nil, first
		}
		return nil, second
	}}
	messages := make([]domain.FeedMessage, 20)
	for index := range messages {
		messages[index] = domain.FeedMessage{User: "user", FeedID: string(rune('a' + index))}
	}
	err := (&server{queue: queue, feedsURL: "queue"}).enqueueFeeds(context.Background(), messages)
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("enqueue error = %v", err)
	}
}

func TestDecorateExtractionUsesLifetimeFeedCounters(t *testing.T) {
	feeds := []domain.Feed{
		{FeedID: "enough", ItemCount: 10, ExtractionSample: 10, ExtractionFailures: 2, ExtractionQualityTotal: 5.5},
		{FeedID: "new", ItemCount: 4, ExtractionSample: 4, ExtractionQualityTotal: 3.6},
	}
	decorateExtraction(feeds)
	if feeds[0].ExtractionSample != 10 || feeds[0].ExtractionRate == nil || *feeds[0].ExtractionRate != 0.8 || feeds[0].AverageQuality == nil || *feeds[0].AverageQuality != 0.55 {
		t.Fatalf("enough feed stats = %#v", feeds[0])
	}
	if feeds[1].ExtractionSample != 4 || feeds[1].ExtractionRate != nil || feeds[1].AverageQuality != nil {
		t.Fatalf("new feed stats = %#v", feeds[1])
	}
}

func TestDecorateFeedsUsesFeedRowsWithoutScanningItems(t *testing.T) {
	db := &apiDynamo{query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		t.Fatalf("decorate feeds queried items: %#v", input)
		return nil, nil
	}}
	want := []domain.Feed{{FeedID: "enough", ItemCount: 10, ExtractionSample: 10, ExtractionFailures: 2, ExtractionQualityTotal: 5.5}, {FeedID: "new", ItemCount: 4, ExtractionSample: 4, ExtractionQualityTotal: 3.6}}
	decorateExtraction(want)
	got := []domain.Feed{{FeedID: "enough", ItemCount: 10, ExtractionSample: 10, ExtractionFailures: 2, ExtractionQualityTotal: 5.5}, {FeedID: "new", ItemCount: 4, ExtractionSample: 4, ExtractionQualityTotal: 3.6}}
	server := &server{store: store.New(db, nil, "table", "", "")}
	if err := server.decorateFeeds(context.Background(), "user", got); err != nil {
		t.Fatal(err)
	}
	for index := range want {
		if got[index].ItemCount != want[index].ItemCount || got[index].ExtractionSample != want[index].ExtractionSample {
			t.Fatalf("feed %d counts = %#v, want %#v", index, got[index], want[index])
		}
		if (got[index].ExtractionRate == nil) != (want[index].ExtractionRate == nil) || (got[index].AverageQuality == nil) != (want[index].AverageQuality == nil) {
			t.Fatalf("feed %d optional stats = %#v, want %#v", index, got[index], want[index])
		}
		if got[index].ExtractionRate != nil && (*got[index].ExtractionRate != *want[index].ExtractionRate || *got[index].AverageQuality != *want[index].AverageQuality) {
			t.Fatalf("feed %d distribution = %#v, want %#v", index, got[index], want[index])
		}
	}
}

func TestPrepareItemsLoadsOnlyPageSignals(t *testing.T) {
	db := &apiDynamo{batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
		request := input.RequestItems["table"]
		if len(request.Keys) != 2 {
			t.Fatalf("signal keys = %#v", request.Keys)
		}
		return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": {
			{"SK": &types.AttributeValueMemberS{Value: "S#first"}, "value": &types.AttributeValueMemberN{Value: "1"}},
		}}}, nil
	}}
	server := &server{store: store.New(db, nil, "table", "", "/content")}
	items := []domain.Item{{ItemID: "first", Vector: []byte{1}}, {ItemID: "second", Vector: []byte{2}}}
	if err := server.prepareItems(context.Background(), "user", items); err != nil {
		t.Fatal(err)
	}
	if items[0].Signal != 1 || items[1].Signal != 0 || items[0].Vector != nil || items[1].Vector != nil {
		t.Fatalf("prepared items = %#v", items)
	}
}

func TestGetMeUsesProfileSignalCount(t *testing.T) {
	profile, err := attributevalue.MarshalMap(domain.User{
		PK: "U#user", SK: "PROFILE", Email: "reader@example.com", OrderPref: domain.OrderChrono, SignalCount: 12, HeartCount: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	db := &apiDynamo{
		getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: profile}, nil
		},
		batchGet: func(*dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
			t.Fatal("getMe unexpectedly loaded signals")
			return nil, nil
		},
	}
	response := (&server{store: store.New(db, nil, "table", "", "")}).getMe(context.Background(), "user")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, response.Body)
	}
	var body struct {
		SignalCount int         `json:"signal_count"`
		HeartCount  int         `json:"heart_count"`
		Profile     domain.User `json:"profile"`
	}
	if err := json.Unmarshal([]byte(response.Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.SignalCount != 12 || body.Profile.SignalCount != 12 || body.HeartCount != 3 {
		t.Fatalf("profile response = %#v", body)
	}
}

func TestBehaviourEventsValidateAndWriteMonotonicRow(t *testing.T) {
	item, err := attributevalue.MarshalMap(domain.Item{
		PK: "U#user", SK: domain.ItemSK(time.Now(), "item"), ItemID: "item", FeedID: "feed", Title: "Title",
		Vector: []byte{1, 2, 3}, TTL: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var updateInputs []*dynamodb.UpdateItemInput
	db := &apiDynamo{
		query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{item}}, nil
		},
		update: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			updateInputs = append(updateInputs, input)
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}
	server := &server{store: store.New(db, nil, "table", "", "")}
	response := server.itemRoute(context.Background(), "user", http.MethodPost, "item/events", `{"opened":true,"dwell_ms":31000,"clicked_through":true}`)
	if response.StatusCode != http.StatusOK || len(updateInputs) != 2 {
		t.Fatalf("events response = %d %s, updates %d", response.StatusCode, response.Body, len(updateInputs))
	}
	response = server.itemRoute(context.Background(), "user", http.MethodPost, "item/events", `{}`)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty event status = %d, body %s", response.StatusCode, response.Body)
	}
	response = server.itemRoute(context.Background(), "user", http.MethodPost, "item/events", `{"shared":true}`)
	if response.StatusCode != http.StatusOK || len(updateInputs) != 3 {
		t.Fatalf("share event response = %d %s, updates %d", response.StatusCode, response.Body, len(updateInputs))
	}
	shareUpdate := updateInputs[2]
	if shareUpdate.ExpressionAttributeNames["#shared"] != "shared" {
		t.Fatalf("share expression names = %#v", shareUpdate.ExpressionAttributeNames)
	}
	if value, ok := shareUpdate.ExpressionAttributeValues[":shared"].(*types.AttributeValueMemberBOOL); !ok || !value.Value {
		t.Fatalf("share expression values = %#v", shareUpdate.ExpressionAttributeValues)
	}
}

func TestRetryItemQueuesForcedExtractionAndSummary(t *testing.T) {
	item, err := attributevalue.MarshalMap(domain.Item{
		PK: "U#user", SK: domain.ItemSK(time.Now(), "item"), ItemID: "item", FeedID: "feed", URL: "https://example.com/story", Title: "Title", PublishedTS: "2026-08-20T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	db := &apiDynamo{query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{item}}, nil
	}}
	queue := &apiQueue{}
	server := &server{store: store.New(db, nil, "table", "", ""), queue: queue, itemsURL: "items-queue"}
	response := server.itemRoute(context.Background(), "user", http.MethodPost, "item/retry", "")
	if response.StatusCode != http.StatusAccepted || queue.input == nil || aws.ToString(queue.input.QueueUrl) != "items-queue" || len(queue.input.Entries) != 1 {
		t.Fatalf("response = %d %s, queue = %#v", response.StatusCode, response.Body, queue.input)
	}
	var message domain.ItemMessage
	if err := json.Unmarshal([]byte(aws.ToString(queue.input.Entries[0].MessageBody)), &message); err != nil {
		t.Fatal(err)
	}
	if !message.Reprocess || !message.ForceExtract || !message.ForceSummary || message.ItemID != "item" {
		t.Fatalf("retry message = %#v", message)
	}
}
