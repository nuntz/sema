package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

type searchTextStore interface {
	UserIDs(context.Context) ([]string, error)
	ArchiveItems(context.Context, string) ([]domain.Item, error)
	UpdateSearchText(context.Context, domain.Item) error
}

func main() {
	apply := flag.Bool("apply", false, "write search_text to archive rows")
	flag.Parse()
	repository, _, err := store.FromEnv(context.Background())
	if err != nil {
		panic(err)
	}
	if _, _, err := run(context.Background(), repository, *apply); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, repository searchTextStore, apply bool) (int, int, error) {
	users, err := repository.UserIDs(ctx)
	if err != nil {
		return 0, 0, err
	}
	total, affected := 0, 0
	for _, userID := range users {
		items, err := repository.ArchiveItems(ctx, userID)
		if err != nil {
			return total, affected, fmt.Errorf("list archive for %s: %w", userID, err)
		}
		for _, item := range items {
			total++
			derived := domain.DeriveSearchText(item.Title, item.Summary)
			if item.SearchText == derived {
				continue
			}
			affected++
			if apply {
				if err := repository.UpdateSearchText(ctx, item); err != nil {
					return total, affected, fmt.Errorf("update %s/%s: %w", userID, item.ItemID, err)
				}
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
