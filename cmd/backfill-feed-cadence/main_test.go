package main

import (
	"bytes"
	"context"
	"github.com/nuntz/sema/internal/domain"
	"testing"
	"time"
)

type fakeStore struct {
	feeds  []domain.Feed
	writes int
}

func (s *fakeStore) UserIDs(context.Context) ([]string, error) { return []string{"user"}, nil }
func (s *fakeStore) Feeds(context.Context, string) ([]domain.Feed, error) {
	return append([]domain.Feed(nil), s.feeds...), nil
}
func (s *fakeStore) PutFeed(_ context.Context, feed domain.Feed) error {
	s.writes++
	for i := range s.feeds {
		if s.feeds[i].FeedID == feed.FeedID {
			s.feeds[i] = feed
		}
	}
	return nil
}
func TestMigrationDryRunApplyAndRepeat(t *testing.T) {
	repository := &fakeStore{feeds: []domain.Feed{{FeedID: "hourly", FetchIntervalH: 1}, {FeedID: "three", FetchIntervalH: 3}, {FeedID: "six", FetchIntervalH: 6}, {FeedID: "daily", FetchIntervalH: 24}, {FeedID: "reddit", Connector: domain.ConnectorReddit, FetchIntervalH: 1}, {FeedID: "auto"}}}
	now := time.Now().UTC()
	var output bytes.Buffer
	count, err := run(context.Background(), repository, false, now, &output)
	if err != nil || count != 6 || repository.writes != 0 || !bytes.Contains(output.Bytes(), []byte("cadence_pin_h=0")) {
		t.Fatalf("dry run=%d %v %s", count, err, output.String())
	}
	count, err = run(context.Background(), repository, true, now, &output)
	if err != nil || count != 6 || repository.writes != 6 {
		t.Fatalf("apply=%d %v", count, err)
	}
	for i, want := range []int{0, 3, 6, 24, 1, 0} {
		if repository.feeds[i].FetchIntervalH != want || repository.feeds[i].HistoryStartedAt != domain.Timestamp(now) {
			t.Fatalf("feed=%+v", repository.feeds[i])
		}
	}
	repository.feeds[0].FetchIntervalH = 1 // A deliberate hourly pin after migration must survive.
	count, err = run(context.Background(), repository, true, now.Add(time.Hour), &output)
	if err != nil || count != 0 || repository.writes != 6 || repository.feeds[0].FetchIntervalH != 1 {
		t.Fatalf("repeat=%d %v", count, err)
	}
}
