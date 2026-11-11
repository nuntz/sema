package requestsnapshot_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/cmd/api/internal/requestsnapshot"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/dynamotest"
	"github.com/nuntz/sema/internal/store"
)

// clock is a test's time, moved only by the test.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time           { return c.now }
func (c *clock) Advance(by time.Duration) { c.now = c.now.Add(by) }
func newClock() *clock                    { return &clock{now: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)} }

// table is one table on DynamoDB Local, so the real store's bump rules drive
// the protocol. Two Caches over its store are two API instances.
type table struct {
	store *store.Store
	db    *dynamodb.Client
	name  string
}

func newTable(t *testing.T) table {
	t.Helper()
	db, name := dynamotest.NewTable(t)
	return table{store: store.New(db, nil, name, "", ""), db: db, name: name}
}

// putLiveItems writes live Items, which a Read needs.
func (tb table) putLiveItems(t *testing.T, userID string, ids ...string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := tb.store.PutFeed(ctx, domain.Feed{
		PK: domain.UserPK(userID), SK: domain.FeedSK("feed"), FeedID: "feed",
		URL: "https://example.com/feed", NextFetchAt: domain.Timestamp(now),
	}); err != nil {
		t.Fatalf("put feed: %v", err)
	}
	for _, id := range ids {
		written, err := tb.store.PutItem(ctx, domain.Item{
			PK: domain.UserPK(userID), SK: domain.ItemSK(now, id), FeedPK: "F#feed", ItemID: id, FeedID: "feed",
			URL: "https://example.com/" + id, Title: id, PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now),
			Score: 1, Size: "M", TTL: now.Add(domain.Retention).Unix(),
		})
		if err != nil || !written {
			t.Fatalf("put %s = %v, %v", id, written, err)
		}
	}
}

// readWithoutBump marks ids Read the way an instance running code from
// before READ_VERSION did: the markers change but the version doesn't, so
// only a reload sees them and a kept copy hides them.
func (tb table) readWithoutBump(t *testing.T, userID string, ids ...string) {
	t.Helper()
	ctx := context.Background()
	before, err := tb.store.ReadVersion(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tb.store.SetRead(ctx, userID, ids, true); err != nil {
		t.Fatal(err)
	}
	if _, err := tb.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(tb.name),
		Item: map[string]types.AttributeValue{
			"PK":      &types.AttributeValueMemberS{Value: domain.UserPK(userID)},
			"SK":      &types.AttributeValueMemberS{Value: domain.ReadVersionSK},
			"version": &types.AttributeValueMemberN{Value: strconv.FormatInt(before, 10)},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func readMarkers(t *testing.T, snapshot *requestsnapshot.Snapshot) map[string]bool {
	t.Helper()
	markers, _, err := snapshot.ReadMarkers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return markers
}

func setRead(t *testing.T, snapshot *requestsnapshot.Snapshot, read bool, ids ...string) {
	t.Helper()
	if err := snapshot.SetRead(context.Background(), ids, read); err != nil {
		t.Fatal(err)
	}
}

func wantMarkers(t *testing.T, got map[string]bool, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Read markers = %v, want %v", got, want)
	}
	for _, id := range want {
		if !got[id] {
			t.Fatalf("Read markers = %v, want %v", got, want)
		}
	}
}

func TestReadOnOneInstanceIsSeenByAnother(t *testing.T) {
	tb := newTable(t)
	tb.putLiveItems(t, "user", "a")
	clock := newClock()
	self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
	wantMarkers(t, readMarkers(t, self.Snapshot("user")))

	setRead(t, other.Snapshot("user"), true, "a")

	wantMarkers(t, readMarkers(t, self.Snapshot("user")), "a")
}

func TestUnreadOnOneInstanceIsSeenByAnother(t *testing.T) {
	tb := newTable(t)
	tb.putLiveItems(t, "user", "a", "b")
	clock := newClock()
	self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
	setRead(t, self.Snapshot("user"), true, "a", "b")
	wantMarkers(t, readMarkers(t, self.Snapshot("user")), "a", "b")

	setRead(t, other.Snapshot("user"), false, "a")

	wantMarkers(t, readMarkers(t, self.Snapshot("user")), "b")
}

func TestOwnReadMovingTheVersionByOnePatchesTheCopyForward(t *testing.T) {
	tb := newTable(t)
	tb.putLiveItems(t, "user", "a", "unbumped")
	clock := newClock()
	self := requestsnapshot.NewCache(tb.store, clock.Now)
	wantMarkers(t, readMarkers(t, self.Snapshot("user")))
	tb.readWithoutBump(t, "user", "unbumped")

	setRead(t, self.Snapshot("user"), true, "a")

	// The copy was kept and patched, so the unbumped write stays hidden.
	wantMarkers(t, readMarkers(t, self.Snapshot("user")), "a")
}

func TestOwnReadAfterAnotherInstanceWroteDropsTheCopy(t *testing.T) {
	tb := newTable(t)
	tb.putLiveItems(t, "user", "a", "b", "unbumped")
	clock := newClock()
	self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
	wantMarkers(t, readMarkers(t, self.Snapshot("user")))
	tb.readWithoutBump(t, "user", "unbumped")
	setRead(t, other.Snapshot("user"), true, "b")

	setRead(t, self.Snapshot("user"), true, "a")

	// The version jumped by two, so the copy was dropped and reloaded.
	wantMarkers(t, readMarkers(t, self.Snapshot("user")), "a", "b", "unbumped")
}

func TestRequestSeesItsOwnReads(t *testing.T) {
	tb := newTable(t)
	tb.putLiveItems(t, "user", "a", "b")
	clock := newClock()
	cache := requestsnapshot.NewCache(tb.store, clock.Now)
	request := cache.Snapshot("user")
	wantMarkers(t, readMarkers(t, request))

	setRead(t, request, true, "a", "b")
	wantMarkers(t, readMarkers(t, request), "a", "b")
	setRead(t, request, false, "a")
	wantMarkers(t, readMarkers(t, request), "b")
}

func TestRequestSeesItsOwnReadAfterAnotherInstanceWrote(t *testing.T) {
	tb := newTable(t)
	tb.putLiveItems(t, "user", "a", "b")
	clock := newClock()
	self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
	request := self.Snapshot("user")
	wantMarkers(t, readMarkers(t, request))
	setRead(t, other.Snapshot("user"), true, "b")

	setRead(t, request, true, "a")

	wantMarkers(t, readMarkers(t, request), "a", "b")
}

func TestReadMarkersHandedOutStayUnchangedByLaterWrites(t *testing.T) {
	tb := newTable(t)
	tb.putLiveItems(t, "user", "a")
	clock := newClock()
	request := requestsnapshot.NewCache(tb.store, clock.Now).Snapshot("user")
	before := readMarkers(t, request)

	setRead(t, request, true, "a")

	wantMarkers(t, before)
}

func TestRequestSeesOneReadVersion(t *testing.T) {
	tb := newTable(t)
	tb.putLiveItems(t, "user", "a")
	clock := newClock()
	self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
	request := self.Snapshot("user")
	_, before, err := request.ReadMarkers(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	setRead(t, other.Snapshot("user"), true, "a")

	markers, after, err := request.ReadMarkers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("READ_VERSION moved from %d to %d within one request", before, after)
	}
	wantMarkers(t, markers)
}

func TestReadMarkersExpireAfterAMinute(t *testing.T) {
	tb := newTable(t)
	tb.putLiveItems(t, "user", "unbumped")
	clock := newClock()
	cache := requestsnapshot.NewCache(tb.store, clock.Now)
	wantMarkers(t, readMarkers(t, cache.Snapshot("user")))
	tb.readWithoutBump(t, "user", "unbumped")

	clock.Advance(time.Minute - time.Second)
	wantMarkers(t, readMarkers(t, cache.Snapshot("user")))

	clock.Advance(time.Second)
	wantMarkers(t, readMarkers(t, cache.Snapshot("user")), "unbumped")
}

func TestReadMarkerCacheEvictsItsOldestUserBeyond128(t *testing.T) {
	tb := newTable(t)
	tb.putLiveItems(t, "user", "unbumped")
	clock := newClock()
	cache := requestsnapshot.NewCache(tb.store, clock.Now)
	wantMarkers(t, readMarkers(t, cache.Snapshot("user")))
	tb.readWithoutBump(t, "user", "unbumped")
	for i := range 127 {
		clock.Advance(time.Millisecond)
		readMarkers(t, cache.Snapshot(fmt.Sprint("other-", i)))
	}
	wantMarkers(t, readMarkers(t, cache.Snapshot("user")))

	clock.Advance(time.Millisecond)
	readMarkers(t, cache.Snapshot("one-too-many"))

	wantMarkers(t, readMarkers(t, cache.Snapshot("user")), "unbumped")
}

func TestReadCacheOutcomes(t *testing.T) {
	tb := newTable(t)
	tb.putLiveItems(t, "user", "a")
	clock := newClock()
	self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
	outcome := func() string {
		t.Helper()
		request := self.Snapshot("user")
		readMarkers(t, request)
		return request.CacheOutcomes()["ReadCache"]
	}
	if got := outcome(); got != "miss" {
		t.Fatalf("first load = %q, want miss", got)
	}
	if got := outcome(); got != "hit" {
		t.Fatalf("second load = %q, want hit", got)
	}
	setRead(t, other.Snapshot("user"), true, "a")
	if got := outcome(); got != "stale" {
		t.Fatalf("load after another instance's Read = %q, want stale", got)
	}
	clock.Advance(time.Minute)
	if got := outcome(); got != "miss" {
		t.Fatalf("load after the TTL = %q, want miss", got)
	}
	if got := self.Snapshot("user").CacheOutcomes()["ReadCache"]; got != "" {
		t.Fatalf("request that never read markers recorded ReadCache = %q", got)
	}
}

func TestReadCacheOutcomeIsTheRequestsFirst(t *testing.T) {
	tb := newTable(t)
	tb.putLiveItems(t, "user", "a", "b")
	clock := newClock()
	self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
	request := self.Snapshot("user")
	readMarkers(t, request)
	setRead(t, other.Snapshot("user"), true, "b")
	readMarkers(t, self.Snapshot("user"))
	// The instance's copy is one behind this write and patches forward; the
	// request's markers are two behind and reload from that copy: a hit.
	setRead(t, request, true, "a")
	wantMarkers(t, readMarkers(t, request), "a", "b")

	if got := request.CacheOutcomes()["ReadCache"]; got != "miss" {
		t.Fatalf("ReadCache = %q, want the first load's miss", got)
	}
}
