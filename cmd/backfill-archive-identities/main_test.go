package main

import (
	"context"
	"errors"
	"testing"

	"github.com/nuntz/sema/internal/domain"
)

type fakeArchives struct {
	writes int
	err    error
}

func (*fakeArchives) UserIDs(context.Context) ([]string, error) { return []string{"user"}, nil }
func (*fakeArchives) ArchiveItems(context.Context, string) ([]domain.Item, error) {
	return []domain.Item{{PK: "U#user", SK: "A#archive", ItemID: "item"}}, nil
}
func (f *fakeArchives) BackfillArchiveIdentity(context.Context, string, domain.Item) error {
	f.writes++
	return f.err
}

func TestRunDryRunAndApply(t *testing.T) {
	f := &fakeArchives{}
	if err := run(context.Background(), f, false); err != nil || f.writes != 0 {
		t.Fatalf("dry run: writes=%d, err=%v", f.writes, err)
	}
	if err := run(context.Background(), f, true); err != nil || f.writes != 1 {
		t.Fatalf("apply: writes=%d, err=%v", f.writes, err)
	}
	f.err = errors.New("unavailable")
	if err := run(context.Background(), f, true); !errors.Is(err, f.err) {
		t.Fatalf("error = %v", err)
	}
}
