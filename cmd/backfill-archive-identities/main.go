package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

type archiveStore interface {
	UserIDs(context.Context) ([]string, error)
	ArchiveItems(context.Context, string) ([]domain.Item, error)
	BackfillArchiveIdentity(context.Context, string, domain.Item) error
}

func main() {
	apply := flag.Bool("apply", false, "write permanent archive identity rows")
	flag.Parse()
	ctx := context.Background()
	repository, _, err := store.FromEnv(ctx)
	if err != nil {
		panic(err)
	}
	if err := run(ctx, repository, *apply); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, repository archiveStore, apply bool) error {
	users, err := repository.UserIDs(ctx)
	if err != nil {
		return err
	}
	count := 0
	for _, userID := range users {
		items, err := repository.ArchiveItems(ctx, userID)
		if err != nil {
			return fmt.Errorf("list archives for %s: %w", userID, err)
		}
		for _, item := range items {
			count++
			if apply {
				if err := repository.BackfillArchiveIdentity(ctx, userID, item); err != nil {
					return fmt.Errorf("archive identity %s/%s: %w", userID, item.ItemID, err)
				}
			}
		}
	}
	mode := "dry-run"
	if apply {
		mode = "applied"
	}
	fmt.Printf("mode=%s users=%d archive_rows=%d\n", mode, len(users), count)
	return nil
}
