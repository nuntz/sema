package main

import (
	"context"

	"github.com/nuntz/sema/cmd/api/internal/requestsnapshot"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

// apiStore is the persistence seam consumed by HTTP handlers. It lacks what
// the Request snapshot owns: the READ_VERSION, FEED_VERSION and
// FEEDBACK_VERSION rows, the writes that move them (Read, Feed, Keep and
// Boost or Bury writes), and the loads of a user's whole Read marker set and
// Feed list. Handlers reach those only through the Snapshot, whose own
// requestsnapshot.Store the same *store.Store satisfies. Handlers still read
// one Feed (Feed) and Boost or Bury values (SignalValues) here directly. The
// read parameters below are the Request snapshot's Read markers.
type apiStore interface {
	EnsureUser(ctx context.Context, userID, email string) error
	User(ctx context.Context, userID string) (domain.User, error)
	UpdateUser(ctx context.Context, userID string, order *domain.Order, position, tag, feed *string) error
	Feed(ctx context.Context, userID, feedID string) (domain.Feed, error)
	FeedItemCounts(ctx context.Context, userID string, window domain.FetchWindow, read map[string]bool) (map[string]domain.FeedItemCount, error)
	ItemsForFeeds(ctx context.Context, userID string, order domain.Order, encodedCursor string, limit int, includeRead, fillFilteredPage bool, allowedFeedIDs, excludeItemIDs map[string]bool, window domain.FetchWindow, read map[string]bool) ([]domain.Item, string, *domain.Item, error)
	Clusters(ctx context.Context, userID string) ([]domain.Cluster, error)
	SearchItems(ctx context.Context, userID, prefix string, terms []string, limit int, allowedFeedIDs, read map[string]bool) ([]domain.Item, error)
	ResolveItemIDs(ctx context.Context, userID string, ids []string, read map[string]bool) ([]domain.Item, error)
	Item(ctx context.Context, userID, itemID string) (domain.Item, error)
	Archives(ctx context.Context, userID, encodedCursor string, limit int) ([]domain.Item, string, error)
	ArchiveItem(ctx context.Context, userID, itemID string) (domain.Item, error)
	RecordBehaviour(ctx context.Context, userID string, item domain.Item, event store.BehaviourEvent) error
	Model(ctx context.Context, userID string) (domain.Model, error)
	SignalValues(ctx context.Context, userID string, itemIDs []string) (map[string]int, error)
	ResolveRead(ctx context.Context, userID string, items []domain.Item, read map[string]bool) error
	PutContent(ctx context.Context, objectKey, contentType string, body []byte) error
	ContentURL(objectKey string) string
	PublicItem(item domain.Item) domain.Item
}

var (
	_ apiStore              = (*store.Store)(nil)
	_ requestsnapshot.Store = (*store.Store)(nil)
)
