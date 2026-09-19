package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

type feedStore interface {
	UserIDs(context.Context) ([]string, error)
	Feeds(context.Context, string) ([]domain.Feed, error)
	PutFeed(context.Context, domain.Feed) error
}

func run(ctx context.Context, repository feedStore, apply bool, now time.Time, out io.Writer) (int, error) {
	users, err := repository.UserIDs(ctx)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, user := range users {
		feeds, err := repository.Feeds(ctx, user)
		if err != nil {
			return changed, err
		}
		for _, feed := range feeds {
			// The history start also marks migrated feeds, preserving subsequent hourly pins.
			if feed.HistoryStartedAt != "" {
				continue
			}
			feed.HistoryStartedAt = domain.Timestamp(now)
			if domain.FeedConnector(feed) != domain.ConnectorReddit && feed.FetchIntervalH == 1 {
				feed.FetchIntervalH = 0
			}
			changed++
			fmt.Fprintf(out, "user=%s feed=%s cadence_pin_h=%d history_started_at=%s apply=%t\n", user, feed.FeedID, feed.FetchIntervalH, feed.HistoryStartedAt, apply)
			if apply {
				if err := repository.PutFeed(ctx, feed); err != nil {
					return changed, err
				}
			}
		}
	}
	fmt.Fprintf(out, "affected=%d apply=%t\n", changed, apply)
	return changed, nil
}

func main() {
	apply := flag.Bool("apply", false, "migrate existing feed Cadence pins and start publish history")
	flag.Parse()
	ctx := context.Background()
	repository, _, err := store.FromEnv(ctx)
	if err != nil {
		panic(err)
	}
	if _, err := run(ctx, repository, *apply, time.Now().UTC(), os.Stdout); err != nil {
		panic(err)
	}
}
