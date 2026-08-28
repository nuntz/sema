package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/nuntz/sema/internal/connector/reddit"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

type redditFeedStore interface {
	UserIDs(context.Context) ([]string, error)
	Feeds(context.Context, string) ([]domain.Feed, error)
	PutFeed(context.Context, domain.Feed) error
}

func main() {
	apply := flag.Bool("apply", false, "migrate Reddit RSS feeds to the reddit connector")
	flag.Parse()
	ctx := context.Background()
	repository, _, err := store.FromEnv(ctx)
	if err != nil {
		panic(err)
	}
	if _, _, err := run(ctx, repository, *apply); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, repository redditFeedStore, apply bool) (int, int, error) {
	users, err := repository.UserIDs(ctx)
	if err != nil {
		return 0, 0, err
	}
	total, affected := 0, 0
	for _, userID := range users {
		feeds, err := repository.Feeds(ctx, userID)
		if err != nil {
			return total, affected, fmt.Errorf("list feeds for %s: %w", userID, err)
		}
		for _, feed := range feeds {
			total++
			migrated, changed := migrateFeed(feed)
			if !changed {
				continue
			}
			affected++
			if !apply {
				continue
			}
			if err := repository.PutFeed(ctx, migrated); err != nil {
				return total, affected, fmt.Errorf("update %s/%s: %w", userID, feed.FeedID, err)
			}
		}
	}
	mode := "dry-run"
	if apply {
		mode = "applied"
	}
	fmt.Fprintf(os.Stdout, "mode=%s users=%d affected=%d total=%d\n", mode, len(users), affected, total)
	return total, affected, nil
}

func migrateFeed(feed domain.Feed) (domain.Feed, bool) {
	if !reddit.Matches(feed.URL) {
		return feed, false
	}
	input, err := reddit.ParseInput(feed.URL)
	if err != nil {
		return feed, false
	}
	canonicalURL := reddit.CanonicalURL(input.Subreddit, input.Sort)
	structuralChange := domain.FeedConnector(feed) != domain.ConnectorReddit || feed.URL != canonicalURL
	changed := structuralChange || feed.SiteURL != reddit.SiteURL(input.Subreddit) || feed.Title != reddit.Title(input.Subreddit) || feed.FetchIntervalH != reddit.IntervalHours(input.Sort)
	if !changed {
		return feed, false
	}
	feed.Connector = domain.ConnectorReddit
	feed.URL = canonicalURL
	feed.SiteURL = reddit.SiteURL(input.Subreddit)
	feed.Title = reddit.Title(input.Subreddit)
	feed.FetchIntervalH = reddit.IntervalHours(input.Sort)
	if structuralChange {
		feed.ETag = ""
		feed.LastModified = ""
	}
	return feed, true
}
