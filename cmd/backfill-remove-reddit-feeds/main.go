// Command backfill-remove-reddit-feeds removes every Reddit Feed and freezes
// what each one lent its Kept Items, so they look as they did (ADR 0009).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/send"
	"github.com/nuntz/sema/internal/store"
)

type feedStore interface {
	UserIDs(context.Context) ([]string, error)
	Feeds(context.Context, string) ([]domain.Feed, error)
	ArchiveItems(context.Context, string) ([]domain.Item, error)
	UpdateItemPresentation(context.Context, domain.Item) error
	DeleteFeed(context.Context, string, string) (int64, error)
}

type removal struct {
	FeedsRemoved    int
	KeptItemsFrozen int
}

func run(ctx context.Context, repository feedStore, apply bool, out io.Writer) (removal, error) {
	users, err := repository.UserIDs(ctx)
	if err != nil {
		return removal{}, err
	}
	var total removal
	for _, user := range users {
		counts, err := removeUserFeeds(ctx, repository, user, apply)
		total.FeedsRemoved += counts.FeedsRemoved
		total.KeptItemsFrozen += counts.KeptItemsFrozen
		if err != nil {
			return total, fmt.Errorf("user %s: %w", user, err)
		}
		if counts != (removal{}) {
			fmt.Fprintf(out, "user=%s reddit_feeds_removed=%d kept_items_frozen=%d apply=%t\n", user, counts.FeedsRemoved, counts.KeptItemsFrozen, apply)
		}
	}
	fmt.Fprintf(out, "reddit_feeds_removed=%d kept_items_frozen=%d apply=%t\n", total.FeedsRemoved, total.KeptItemsFrozen, apply)
	return total, nil
}

func removeUserFeeds(ctx context.Context, repository feedStore, user string, apply bool) (removal, error) {
	feeds, err := repository.Feeds(ctx, user)
	if err != nil {
		return removal{}, err
	}
	redditFeeds := []domain.Feed{}
	for _, feed := range feeds {
		if domain.FeedConnector(feed) == domain.ConnectorReddit {
			redditFeeds = append(redditFeeds, feed)
		}
	}
	if len(redditFeeds) == 0 {
		return removal{}, nil
	}
	kept, err := repository.ArchiveItems(ctx, user)
	if err != nil {
		return removal{}, err
	}
	var counts removal
	for _, feed := range redditFeeds {
		for _, item := range kept {
			if item.FeedID != feed.FeedID {
				continue
			}
			frozen, changed := freeze(item, feed)
			if !changed {
				continue
			}
			counts.KeptItemsFrozen++
			if apply {
				if err := repository.UpdateItemPresentation(ctx, frozen); err != nil {
					return counts, fmt.Errorf("freeze kept item %s: %w", item.ItemID, err)
				}
			}
		}
		// Removal comes after every freeze, so a rerun after a failure still
		// finds the Feed and finishes its Kept Items.
		counts.FeedsRemoved++
		if apply {
			if _, err := repository.DeleteFeed(ctx, user, feed.FeedID); err != nil {
				return counts, fmt.Errorf("remove feed %s: %w", feed.FeedID, err)
			}
		}
	}
	return counts, nil
}

// freeze writes onto a Kept Item what the API serves while its Feed exists,
// so it looks the same once the Feed is gone, and the Feed URL and Tags Send
// reads from the Feed, so a Send of it carries the same payload.
func freeze(item domain.Item, feed domain.Feed) (domain.Item, bool) {
	frozen := domain.PresentWithFeed(item, feed)
	frozen.FeedURL, frozen.FeedTags = send.FeedURL(feed), feed.Tags
	changed := frozen.Connector != item.Connector || frozen.FeedTitle != item.FeedTitle || frozen.FaviconKey != item.FaviconKey ||
		frozen.FeedURL != item.FeedURL || !sameTags(frozen.FeedTags, item.FeedTags)
	return frozen, changed
}

// sameTags compares Tags as the string sets they are stored as.
func sameTags(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

func main() {
	apply := flag.Bool("apply", false, "remove Reddit Feeds after freezing their Kept Items' presentation")
	flag.Parse()
	ctx := context.Background()
	repository, _, err := store.FromEnv(ctx)
	if err != nil {
		panic(err)
	}
	if _, err := run(ctx, repository, *apply, os.Stdout); err != nil {
		panic(err)
	}
}
