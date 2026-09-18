package main

import (
	"context"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
	"github.com/nuntz/sema/internal/store"
)

type fakeAPIStore struct {
	ensureUser      func(ctx context.Context, userID, email string) error
	user            func(ctx context.Context, userID string) (domain.User, error)
	updateUser      func(ctx context.Context, userID string, order *domain.Order, position, tag, feed *string) error
	putFeed         func(ctx context.Context, feed domain.Feed) error
	feed            func(ctx context.Context, userID, feedID string) (domain.Feed, error)
	feeds           func(ctx context.Context, userID string) ([]domain.Feed, error)
	deleteFeed      func(ctx context.Context, userID, feedID string) error
	feedItemCounts  func(ctx context.Context, userID string, window domain.FetchWindow, snapshot map[string]bool) (map[string]domain.FeedItemCount, error)
	itemsForFeeds   func(ctx context.Context, userID string, order domain.Order, encodedCursor string, limit int, includeRead, fillFilteredPage bool, allowedFeedIDs, excludeItemIDs map[string]bool, window domain.FetchWindow, snapshot map[string]bool) ([]domain.Item, string, *domain.Item, error)
	loadMarkers     func(ctx context.Context, userID string) (map[string]bool, error)
	clusters        func(ctx context.Context, userID string) ([]domain.Cluster, error)
	searchItems     func(ctx context.Context, userID, prefix string, terms []string, limit int, allowedFeedIDs map[string]bool) ([]domain.Item, error)
	resolve         func(ctx context.Context, userID string, ids []string, snapshot map[string]bool) ([]domain.Item, error)
	item            func(ctx context.Context, userID, itemID string) (domain.Item, error)
	archives        func(ctx context.Context, userID, encodedCursor string, limit int) ([]domain.Item, string, error)
	archiveItem     func(ctx context.Context, userID, itemID string) (domain.Item, error)
	setHeart        func(ctx context.Context, userID, itemID string, hearted bool) (string, int, error)
	recordBehaviour func(ctx context.Context, userID string, item domain.Item, event store.BehaviourEvent) error
	model           func(ctx context.Context, userID string) (domain.Model, error)
	signalValues    func(ctx context.Context, userID string, itemIDs []string) (map[string]int, error)
	setSignal       func(ctx context.Context, userID string, item domain.Item, value int) error
	resolveRead     func(ctx context.Context, userID string, items []domain.Item, snapshot map[string]bool) error
	setRead         func(ctx context.Context, userID string, ids []string, read bool) error
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
func (f *fakeAPIStore) PutFeed(ctx context.Context, feed domain.Feed) error {
	return f.putFeed(ctx, feed)
}
func (f *fakeAPIStore) Feed(ctx context.Context, userID, feedID string) (domain.Feed, error) {
	if f.feed == nil {
		return domain.Feed{}, store.ErrNotFound
	}
	return f.feed(ctx, userID, feedID)
}
func (f *fakeAPIStore) Feeds(ctx context.Context, userID string) ([]domain.Feed, error) {
	if f.feeds == nil {
		return nil, nil
	}
	return f.feeds(ctx, userID)
}
func (f *fakeAPIStore) DeleteFeed(ctx context.Context, userID, feedID string) error {
	return f.deleteFeed(ctx, userID, feedID)
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
func (f *fakeAPIStore) LoadReadMarkers(ctx context.Context, userID string) (map[string]bool, error) {
	if f.loadMarkers == nil {
		return map[string]bool{}, nil
	}
	return f.loadMarkers(ctx, userID)
}
func (f *fakeAPIStore) Clusters(ctx context.Context, userID string) ([]domain.Cluster, error) {
	if f.clusters == nil {
		return nil, nil
	}
	return f.clusters(ctx, userID)
}
func (f *fakeAPIStore) SearchItems(ctx context.Context, userID, prefix string, terms []string, limit int, allowedFeedIDs map[string]bool) ([]domain.Item, error) {
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
func (f *fakeAPIStore) SetHeart(ctx context.Context, userID, itemID string, hearted bool) (string, int, error) {
	return f.setHeart(ctx, userID, itemID, hearted)
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
func (f *fakeAPIStore) SetSignal(ctx context.Context, userID string, item domain.Item, value int) error {
	return f.setSignal(ctx, userID, item, value)
}
func (f *fakeAPIStore) ResolveRead(ctx context.Context, userID string, items []domain.Item, snapshot map[string]bool) error {
	return f.resolveRead(ctx, userID, items, snapshot)
}
func (f *fakeAPIStore) SetRead(ctx context.Context, userID string, ids []string, read bool) error {
	if f.setRead == nil {
		return nil
	}
	return f.setRead(ctx, userID, ids, read)
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
