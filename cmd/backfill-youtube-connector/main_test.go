package main

import (
	"context"
	"testing"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/media"
)

type fakeYouTubeStore struct {
	feeds  []domain.Feed
	writes int
	assets int
}

func (*fakeYouTubeStore) UserIDs(context.Context) ([]string, error) { return []string{"user"}, nil }
func (f *fakeYouTubeStore) Feeds(context.Context, string) ([]domain.Feed, error) {
	return append([]domain.Feed(nil), f.feeds...), nil
}
func (f *fakeYouTubeStore) PutFeed(_ context.Context, feed domain.Feed) error {
	f.writes++
	for i := range f.feeds {
		if f.feeds[i].FeedID == feed.FeedID {
			f.feeds[i] = feed
		}
	}
	return nil
}
func (f *fakeYouTubeStore) PutContent(context.Context, string, string, []byte) error {
	f.assets++
	return nil
}

type fakeResolver struct{ calls int }

func (f *fakeResolver) Resolve(context.Context, domain.Feed) (resolvedChannel, error) {
	f.calls++
	return resolvedChannel{title: "Sema Channel", siteURL: "https://www.youtube.com/channel/UC123", avatar: media.Image{Bytes: []byte("png"), ContentType: "image/png"}}, nil
}

func TestBackfillYouTubeConnectorDryRunAndIdempotence(t *testing.T) {
	feedURL := "https://www.youtube.com/feeds/videos.xml?channel_id=UC123"
	feedID := domain.FeedID(feedURL)
	guid, itemURL := "yt:video:abc", "https://www.youtube.com/watch?v=abc"
	before := domain.ItemID(feedID, guid, itemURL)
	store := &fakeYouTubeStore{feeds: []domain.Feed{{FeedID: feedID, URL: feedURL}}}
	resolver := &fakeResolver{}
	total, affected, err := run(context.Background(), store, resolver, false)
	if err != nil || total != 1 || affected != 1 || store.writes != 0 || resolver.calls != 0 {
		t.Fatalf("dry run = total %d affected %d writes %d calls %d err %v", total, affected, store.writes, resolver.calls, err)
	}
	if _, _, err := run(context.Background(), store, resolver, true); err != nil {
		t.Fatal(err)
	}
	if store.writes != 1 || store.assets != 1 || store.feeds[0].Connector != domain.ConnectorYouTube {
		t.Fatalf("migrated feed = %#v, writes %d assets %d", store.feeds[0], store.writes, store.assets)
	}
	_, affected, err = run(context.Background(), store, resolver, true)
	if err != nil || affected != 0 || store.writes != 1 {
		t.Fatalf("second apply = affected %d writes %d err %v", affected, store.writes, err)
	}
	after := domain.ItemID(store.feeds[0].FeedID, guid, itemURL)
	if before != after {
		t.Fatalf("item id changed: %s -> %s", before, after)
	}
}
