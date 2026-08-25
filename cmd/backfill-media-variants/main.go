package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/media"
	"github.com/nuntz/sema/internal/store"
)

type mediaVariantStore interface {
	UserIDs(context.Context) ([]string, error)
	LiveItems(context.Context, string) ([]domain.Item, error)
	ArchiveItems(context.Context, string) ([]domain.Item, error)
	Content(context.Context, string) ([]byte, string, error)
	PutContent(context.Context, string, string, []byte) error
	UpdateMediaVariants(context.Context, domain.Item, []domain.MediaVariant, int, int) error
}

func main() {
	apply := flag.Bool("apply", false, "write responsive variants and update item records")
	delay := flag.Duration("delay", 50*time.Millisecond, "minimum delay between item updates")
	flag.Parse()
	ctx := context.Background()
	repository, _, err := store.FromEnv(ctx)
	if err != nil {
		panic(err)
	}
	if _, _, err := run(ctx, repository, *apply, *delay); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, repository mediaVariantStore, apply bool, delay time.Duration) (int, int, error) {
	users, err := repository.UserIDs(ctx)
	if err != nil {
		return 0, 0, err
	}
	total, affected := 0, 0
	for _, userID := range users {
		live, err := repository.LiveItems(ctx, userID)
		if err != nil {
			return total, affected, fmt.Errorf("list live items for %s: %w", userID, err)
		}
		archive, err := repository.ArchiveItems(ctx, userID)
		if err != nil {
			return total, affected, fmt.Errorf("list archive items for %s: %w", userID, err)
		}
		for _, item := range append(live, archive...) {
			total++
			if !needsMediaBackfill(item) {
				continue
			}
			affected++
			if !apply {
				continue
			}
			if err := backfillItem(ctx, repository, item); err != nil {
				return total, affected, fmt.Errorf("backfill %s/%s: %w", userID, item.ItemID, err)
			}
			if err := wait(ctx, delay); err != nil {
				return total, affected, err
			}
		}
	}
	mode := "dry-run"
	if apply {
		mode = "applied"
	}
	fmt.Fprintf(os.Stdout, "mode=%s users=%d affected=%d total=%d delay=%s\n", mode, len(users), affected, total, delay)
	return total, affected, nil
}

func needsMediaBackfill(item domain.Item) bool {
	return item.MediaKey != "" && (len(item.MediaVariants) == 0 || item.MediaW > 1280 || item.MediaH > 1280)
}

func backfillItem(ctx context.Context, repository mediaVariantStore, item domain.Item) error {
	body, _, err := repository.Content(ctx, item.MediaKey)
	if err != nil {
		return fmt.Errorf("read lead: %w", err)
	}
	lead, err := media.EncodeLeadBytes(body)
	if err != nil {
		return fmt.Errorf("encode lead: %w", err)
	}
	variants := make([]domain.MediaVariant, 0, len(lead.Variants))
	for index, image := range lead.Variants {
		key := item.MediaKey
		if index != len(lead.Variants)-1 {
			key = store.MediaVariantKey(item.MediaKey, image.Width)
		}
		if err := repository.PutContent(ctx, key, image.ContentType, image.Bytes); err != nil {
			return fmt.Errorf("write %s: %w", key, err)
		}
		variants = append(variants, domain.MediaVariant{Key: key, Width: image.Width, Height: image.Height})
	}
	if err := repository.UpdateMediaVariants(ctx, item, variants, lead.Width, lead.Height); err != nil {
		return fmt.Errorf("update item: %w", err)
	}
	return nil
}

func wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
