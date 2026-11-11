package main

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/nuntz/sema/cmd/api/internal/requestsnapshot"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
	"github.com/nuntz/sema/internal/store"
)

type fakeAPIStore struct {
	ensureUser      func(ctx context.Context, userID, email string) error
	user            func(ctx context.Context, userID string) (domain.User, error)
	updateUser      func(ctx context.Context, userID string, order *domain.Order, position, tag, feed *string) error
	feed            func(ctx context.Context, userID, feedID string) (domain.Feed, error)
	feedItemCounts  func(ctx context.Context, userID string, window domain.FetchWindow, snapshot map[string]bool) (map[string]domain.FeedItemCount, error)
	itemsForFeeds   func(ctx context.Context, userID string, order domain.Order, encodedCursor string, limit int, includeRead, fillFilteredPage bool, allowedFeedIDs, excludeItemIDs map[string]bool, window domain.FetchWindow, snapshot map[string]bool) ([]domain.Item, string, *domain.Item, error)
	clusters        func(ctx context.Context, userID string) ([]domain.Cluster, error)
	searchItems     func(ctx context.Context, userID, prefix string, terms []string, limit int, allowedFeedIDs map[string]bool) ([]domain.Item, error)
	resolve         func(ctx context.Context, userID string, ids []string, snapshot map[string]bool) ([]domain.Item, error)
	item            func(ctx context.Context, userID, itemID string) (domain.Item, error)
	archives        func(ctx context.Context, userID, encodedCursor string, limit int) ([]domain.Item, string, error)
	archiveItem     func(ctx context.Context, userID, itemID string) (domain.Item, error)
	recordBehaviour func(ctx context.Context, userID string, item domain.Item, event store.BehaviourEvent) error
	model           func(ctx context.Context, userID string) (domain.Model, error)
	signalValues    func(ctx context.Context, userID string, itemIDs []string) (map[string]int, error)
	resolveRead     func(ctx context.Context, userID string, items []domain.Item, snapshot map[string]bool) error
	putContent      func(ctx context.Context, objectKey, contentType string, body []byte) error
	contentURL      func(objectKey string) string
	publicItem      func(item domain.Item) domain.Item
}

func (f *fakeAPIStore) EnsureUser(ctx context.Context, userID, email string) error {
	return f.ensureUser(ctx, userID, email)
}
func (f *fakeAPIStore) User(ctx context.Context, userID string) (domain.User, error) {
	return f.user(ctx, userID)
}
func (f *fakeAPIStore) UpdateUser(ctx context.Context, userID string, order *domain.Order, position, tag, feed *string) error {
	return f.updateUser(ctx, userID, order, position, tag, feed)
}
func (f *fakeAPIStore) Feed(ctx context.Context, userID, feedID string) (domain.Feed, error) {
	if f.feed == nil {
		return domain.Feed{}, store.ErrNotFound
	}
	return f.feed(ctx, userID, feedID)
}
func (f *fakeAPIStore) FeedItemCounts(ctx context.Context, userID string, window domain.FetchWindow, snapshot map[string]bool) (map[string]domain.FeedItemCount, error) {
	return f.feedItemCounts(ctx, userID, window, snapshot)
}
func (f *fakeAPIStore) ItemsForFeeds(ctx context.Context, userID string, order domain.Order, encodedCursor string, limit int, includeRead, fillFilteredPage bool, allowedFeedIDs, excludeItemIDs map[string]bool, window domain.FetchWindow, snapshot map[string]bool) ([]domain.Item, string, *domain.Item, error) {
	if f.itemsForFeeds == nil {
		return nil, "", nil, nil
	}
	return f.itemsForFeeds(ctx, userID, order, encodedCursor, limit, includeRead, fillFilteredPage, allowedFeedIDs, excludeItemIDs, window, snapshot)
}
func (f *fakeAPIStore) Clusters(ctx context.Context, userID string) ([]domain.Cluster, error) {
	if f.clusters == nil {
		return nil, nil
	}
	return f.clusters(ctx, userID)
}
func (f *fakeAPIStore) SearchItems(ctx context.Context, userID, prefix string, terms []string, limit int, allowedFeedIDs, snapshot map[string]bool) ([]domain.Item, error) {
	if f.searchItems == nil {
		return nil, nil
	}
	return f.searchItems(ctx, userID, prefix, terms, limit, allowedFeedIDs)
}
func (f *fakeAPIStore) ResolveItemIDs(ctx context.Context, userID string, ids []string, snapshot map[string]bool) ([]domain.Item, error) {
	if f.resolve == nil {
		return nil, nil
	}
	return f.resolve(ctx, userID, ids, snapshot)
}
func (f *fakeAPIStore) Item(ctx context.Context, userID, itemID string) (domain.Item, error) {
	return f.item(ctx, userID, itemID)
}
func (f *fakeAPIStore) Archives(ctx context.Context, userID, encodedCursor string, limit int) ([]domain.Item, string, error) {
	return f.archives(ctx, userID, encodedCursor, limit)
}
func (f *fakeAPIStore) ArchiveItem(ctx context.Context, userID, itemID string) (domain.Item, error) {
	return f.archiveItem(ctx, userID, itemID)
}
func (f *fakeAPIStore) RecordBehaviour(ctx context.Context, userID string, item domain.Item, event store.BehaviourEvent) error {
	return f.recordBehaviour(ctx, userID, item, event)
}
func (f *fakeAPIStore) Model(ctx context.Context, userID string) (domain.Model, error) {
	if f.model == nil {
		return domain.Model{}, score.ErrModelNotFound
	}
	return f.model(ctx, userID)
}
func (f *fakeAPIStore) SignalValues(ctx context.Context, userID string, itemIDs []string) (map[string]int, error) {
	if f.signalValues == nil {
		return map[string]int{}, nil
	}
	return f.signalValues(ctx, userID, itemIDs)
}
func (f *fakeAPIStore) ResolveRead(ctx context.Context, userID string, items []domain.Item, snapshot map[string]bool) error {
	return f.resolveRead(ctx, userID, items, snapshot)
}
func (f *fakeAPIStore) PutContent(ctx context.Context, objectKey, contentType string, body []byte) error {
	return f.putContent(ctx, objectKey, contentType, body)
}
func (f *fakeAPIStore) ContentURL(objectKey string) string {
	if f.contentURL == nil {
		return objectKey
	}
	return f.contentURL(objectKey)
}
func (f *fakeAPIStore) PublicItem(item domain.Item) domain.Item {
	if f.publicItem == nil {
		item.Vector = nil
		item.Hearted = item.Hearted || item.ArchiveSK != "" || domain.IsArchive(item)
		return item
	}
	return f.publicItem(item)
}

type fakeSessions struct {
	hash, deleted string
	row           store.Session
}

func (f *fakeSessions) PutSession(_ context.Context, hash string, row store.Session) error {
	f.hash, f.row = hash, row
	return nil
}
func (f *fakeSessions) Session(_ context.Context, hash string) (store.Session, error) {
	if hash != f.hash {
		return store.Session{}, store.ErrNotFound
	}
	return f.row, nil
}
func (f *fakeSessions) RenewSession(context.Context, string, int64, int64) error { return nil }
func (f *fakeSessions) DeleteSession(_ context.Context, hash string) error {
	f.deleted = hash
	return nil
}

// fakeSnapshotStore is the Request snapshot's store over state a test
// supplies. Its versions are always zero and its writes report that nothing
// moved, so it suits tests of one request's handling; a test that needs a
// write seen by a later load, on this instance or another, uses DynamoDB
// Local through internal/dynamotest instead.
type fakeSnapshotStore struct {
	markers   map[string]bool
	feeds     func(ctx context.Context, userID string) ([]domain.Feed, error)
	putFeed   func(ctx context.Context, feed domain.Feed) error
	setSignal func(ctx context.Context, userID string, item domain.Item, value int) error
}

var _ requestsnapshot.Store = (*fakeSnapshotStore)(nil)

// withFeeds returns a fakeSnapshotStore whose user has the Feeds feeds.
func withFeeds(feeds []domain.Feed) *fakeSnapshotStore {
	return &fakeSnapshotStore{feeds: func(context.Context, string) ([]domain.Feed, error) {
		return slices.Clone(feeds), nil
	}}
}

func (*fakeSnapshotStore) ReadVersion(context.Context, string) (int64, error) { return 0, nil }
func (f *fakeSnapshotStore) LoadReadMarkers(context.Context, string) (map[string]bool, error) {
	return maps.Clone(f.markers), nil
}
func (*fakeSnapshotStore) SetRead(context.Context, string, []string, bool) (int64, error) {
	return 0, nil
}
func (*fakeSnapshotStore) FeedVersion(context.Context, string) (int64, error) { return 0, nil }
func (f *fakeSnapshotStore) Feeds(ctx context.Context, userID string) ([]domain.Feed, error) {
	if f.feeds == nil {
		return nil, nil
	}
	return f.feeds(ctx, userID)
}

// SaveFeeds writes each Feed on its own, like the store, so a failed write
// leaves the others saved.
func (f *fakeSnapshotStore) SaveFeeds(ctx context.Context, _ string, feeds []domain.Feed) (int64, error) {
	if f.putFeed == nil {
		return 0, nil
	}
	var writeErrors []error
	for _, feed := range feeds {
		writeErrors = append(writeErrors, f.putFeed(ctx, feed))
	}
	return 0, errors.Join(writeErrors...)
}
func (*fakeSnapshotStore) DeleteFeed(context.Context, string, string) (int64, error) {
	return 0, nil
}
func (*fakeSnapshotStore) FeedbackVersion(context.Context, string) (int64, error) { return 0, nil }
func (*fakeSnapshotStore) SetHeart(context.Context, string, string, bool) (string, int, error) {
	return "", 0, nil
}
func (f *fakeSnapshotStore) SetSignal(ctx context.Context, userID string, item domain.Item, value int) error {
	if f.setSignal == nil {
		return nil
	}
	return f.setSignal(ctx, userID, item, value)
}
