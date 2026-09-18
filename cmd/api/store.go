package main

import (
	"context"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

// apiStore is the persistence seam consumed by HTTP handlers. Read-marker
// snapshots are immutable request values, never mutations of the shared store.
type apiStore interface {
	EnsureUser(ctx context.Context, userID, email string) error
	User(ctx context.Context, userID string) (domain.User, error)
	UpdateUser(ctx context.Context, userID string, order *domain.Order, position, tag, feed *string) error
	PutFeed(ctx context.Context, feed domain.Feed) error
	Feed(ctx context.Context, userID, feedID string) (domain.Feed, error)
	Feeds(ctx context.Context, userID string) ([]domain.Feed, error)
	DeleteFeed(ctx context.Context, userID, feedID string) error
	FeedItemCounts(ctx context.Context, userID string, window domain.FetchWindow, snapshot map[string]bool) (map[string]domain.FeedItemCount, error)
	ItemsForFeeds(ctx context.Context, userID string, order domain.Order, encodedCursor string, limit int, includeRead, fillFilteredPage bool, allowedFeedIDs, excludeItemIDs map[string]bool, window domain.FetchWindow, snapshot map[string]bool) ([]domain.Item, string, *domain.Item, error)
	LoadReadMarkers(ctx context.Context, userID string) (map[string]bool, error)
	Clusters(ctx context.Context, userID string) ([]domain.Cluster, error)
	SearchItems(ctx context.Context, userID, prefix string, terms []string, limit int, allowedFeedIDs map[string]bool) ([]domain.Item, error)
	ResolveItemIDs(ctx context.Context, userID string, ids []string, snapshot map[string]bool) ([]domain.Item, error)
	Item(ctx context.Context, userID, itemID string) (domain.Item, error)
	Archives(ctx context.Context, userID, encodedCursor string, limit int) ([]domain.Item, string, error)
	ArchiveItem(ctx context.Context, userID, itemID string) (domain.Item, error)
	SetHeart(ctx context.Context, userID, itemID string, hearted bool) (string, int, error)
	RecordBehaviour(ctx context.Context, userID string, item domain.Item, event store.BehaviourEvent) error
	Model(ctx context.Context, userID string) (domain.Model, error)
	SignalValues(ctx context.Context, userID string, itemIDs []string) (map[string]int, error)
	SetSignal(ctx context.Context, userID string, item domain.Item, value int) error
	ResolveRead(ctx context.Context, userID string, items []domain.Item, snapshot map[string]bool) error
	SetRead(ctx context.Context, userID string, ids []string, read bool) error
	PutContent(ctx context.Context, objectKey, contentType string, body []byte) error
	ContentURL(objectKey string) string
	PublicItem(item domain.Item) domain.Item
}

var _ apiStore = (*store.Store)(nil)
