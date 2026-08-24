package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/nuntz/sema/internal/connector/youtube"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
	"github.com/nuntz/sema/internal/media"
	"github.com/nuntz/sema/internal/store"
)

type youtubeFeedStore interface {
	UserIDs(context.Context) ([]string, error)
	Feeds(context.Context, string) ([]domain.Feed, error)
	PutFeed(context.Context, domain.Feed) error
	PutContent(context.Context, string, string, []byte) error
}

type resolvedChannel struct {
	title, siteURL string
	avatar         media.Image
}

type channelResolver interface {
	Resolve(context.Context, domain.Feed) (resolvedChannel, error)
}

type liveResolver struct {
	discover *youtube.Discoverer
	media    *media.Processor
}

func (r liveResolver) Resolve(ctx context.Context, feed domain.Feed) (resolvedChannel, error) {
	candidates, err := r.discover.Discover(ctx, feed.URL)
	if err != nil {
		return resolvedChannel{}, fmt.Errorf("discover channel: %w", err)
	}
	if len(candidates) != 1 {
		return resolvedChannel{}, fmt.Errorf("discover channel: got %d candidates", len(candidates))
	}
	candidate := candidates[0]
	if candidate.AvatarURL == "" {
		return resolvedChannel{}, fmt.Errorf("channel %s has no avatar", candidate.Title)
	}
	avatar, err := r.media.Avatar(ctx, candidate.AvatarURL)
	if err != nil {
		return resolvedChannel{}, fmt.Errorf("fetch avatar: %w", err)
	}
	return resolvedChannel{title: candidate.Title, siteURL: candidate.SiteURL, avatar: avatar}, nil
}

func main() {
	apply := flag.Bool("apply", false, "migrate YouTube RSS feeds to the youtube connector")
	flag.Parse()
	ctx := context.Background()
	repository, _, err := store.FromEnv(ctx)
	if err != nil {
		panic(err)
	}
	httpClient := httpx.New(15*time.Second, 10<<20)
	resolver := liveResolver{discover: youtube.NewDiscoverer(httpClient), media: media.New(httpClient)}
	if _, _, err := run(ctx, repository, resolver, *apply); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, repository youtubeFeedStore, resolver channelResolver, apply bool) (int, int, error) {
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
			if !youtube.IsFeedURL(feed.URL) || domain.FeedConnector(feed) == domain.ConnectorYouTube && feed.FaviconKey != "" {
				continue
			}
			affected++
			if !apply {
				continue
			}
			channel, err := resolver.Resolve(ctx, feed)
			if err != nil {
				return total, affected, fmt.Errorf("resolve %s/%s: %w", userID, feed.FeedID, err)
			}
			key := store.FaviconKey(feed.FeedID)
			if err := repository.PutContent(ctx, key, channel.avatar.ContentType, channel.avatar.Bytes); err != nil {
				return total, affected, fmt.Errorf("cache avatar %s/%s: %w", userID, feed.FeedID, err)
			}
			feed.Connector = domain.ConnectorYouTube
			feed.FaviconKey = key
			if channel.title != "" {
				feed.Title = channel.title
			}
			if channel.siteURL != "" {
				feed.SiteURL = channel.siteURL
			}
			if err := repository.PutFeed(ctx, feed); err != nil {
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
