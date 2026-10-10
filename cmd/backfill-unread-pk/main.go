// Command backfill-unread-pk aligns each live item's unread_pk with its Read
// marker so the unread-by-score index holds exactly the unread Items. Counts
// are taken before repairs, so a dry run after --apply is the audit: readers
// may switch to the index once it reports zero missing and zero surplus rows.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/nuntz/sema/internal/store"
)

type unreadStore interface {
	UserIDs(context.Context) ([]string, error)
	ReconcileUnreadMembership(context.Context, string, bool) (store.UnreadMembership, error)
}

func main() {
	apply := flag.Bool("apply", false, "write unread_pk changes to live item rows")
	flag.Parse()
	repository, _, err := store.FromEnv(context.Background())
	if err != nil {
		panic(err)
	}
	if _, err := run(context.Background(), repository, *apply, os.Stdout); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, repository unreadStore, apply bool, out io.Writer) (store.UnreadMembership, error) {
	users, err := repository.UserIDs(ctx)
	if err != nil {
		return store.UnreadMembership{}, err
	}
	var total store.UnreadMembership
	for _, userID := range users {
		report, err := repository.ReconcileUnreadMembership(ctx, userID, apply)
		if err != nil {
			return total, fmt.Errorf("reconcile %s: %w", userID, err)
		}
		total.Live += report.Live
		total.Missing += report.Missing
		total.Surplus += report.Surplus
		total.Repaired += report.Repaired
	}
	mode := "dry-run"
	if apply {
		mode = "applied"
	}
	fmt.Fprintf(out, "mode=%s users=%d live=%d missing=%d surplus=%d repaired=%d\n", mode, len(users), total.Live, total.Missing, total.Surplus, total.Repaired)
	return total, nil
}
