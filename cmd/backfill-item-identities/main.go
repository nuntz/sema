package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

type identityStore interface {
	UserIDs(context.Context) ([]string, error)
	LiveItems(context.Context, string) ([]domain.Item, error)
	ReconcileItemIdentity(context.Context, string, domain.Item, []domain.Item) error
}

type itemGroup struct {
	canonical  domain.Item
	duplicates []domain.Item
}

func main() {
	apply := flag.Bool("apply", false, "write identity markers and remove duplicate live rows")
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

func run(ctx context.Context, repository identityStore, apply bool) error {
	users, err := repository.UserIDs(ctx)
	if err != nil {
		return err
	}
	liveRows, identities, duplicateRows := 0, 0, 0
	for _, userID := range users {
		items, err := repository.LiveItems(ctx, userID)
		if err != nil {
			return fmt.Errorf("list live items for %s: %w", userID, err)
		}
		groups := groupItems(items)
		liveRows += len(items)
		identities += len(groups)
		for _, group := range groups {
			duplicateRows += len(group.duplicates)
			if apply {
				if err := repository.ReconcileItemIdentity(ctx, userID, group.canonical, group.duplicates); err != nil {
					return fmt.Errorf("reconcile %s/%s: %w", userID, group.canonical.ItemID, err)
				}
			}
		}
	}
	mode := "dry-run"
	if apply {
		mode = "applied"
	}
	fmt.Fprintf(os.Stdout, "mode=%s users=%d live_rows=%d identities=%d duplicate_rows=%d\n", mode, len(users), liveRows, identities, duplicateRows)
	return nil
}

func groupItems(items []domain.Item) []itemGroup {
	byID := make(map[string][]domain.Item)
	for _, item := range items {
		if item.ItemID != "" {
			byID[item.ItemID] = append(byID[item.ItemID], item)
		}
	}
	ids := make([]string, 0, len(byID))
	for itemID := range byID {
		ids = append(ids, itemID)
	}
	sort.Strings(ids)
	groups := make([]itemGroup, 0, len(ids))
	for _, itemID := range ids {
		rows := byID[itemID]
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].PublishedTS != rows[j].PublishedTS {
				return rows[i].PublishedTS > rows[j].PublishedTS
			}
			return rows[i].SK > rows[j].SK
		})
		groups = append(groups, itemGroup{canonical: rows[0], duplicates: rows[1:]})
	}
	return groups
}
