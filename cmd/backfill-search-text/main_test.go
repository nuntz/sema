package main

import (
	"context"
	"testing"

	"github.com/nuntz/sema/internal/domain"
)

type fakeSearchTextStore struct {
	items  []domain.Item
	writes int
}

func (f *fakeSearchTextStore) UserIDs(context.Context) ([]string, error) {
	return []string{"user"}, nil
}
func (f *fakeSearchTextStore) ArchiveItems(context.Context, string) ([]domain.Item, error) {
	return append([]domain.Item(nil), f.items...), nil
}
func (f *fakeSearchTextStore) UpdateSearchText(_ context.Context, item domain.Item) error {
	f.writes++
	for index := range f.items {
		if f.items[index].ItemID == item.ItemID {
			f.items[index].SearchText = domain.DeriveSearchText(item.Title, item.Summary)
		}
	}
	return nil
}

func TestBackfillSearchTextDryRunAndIdempotence(t *testing.T) {
	fake := &fakeSearchTextStore{items: []domain.Item{
		{ItemID: "missing", Title: "Pulumi", Summary: "Lambda"},
		{ItemID: "done", Title: "Kept", SearchText: "kept"},
	}}
	total, affected, err := run(context.Background(), fake, false)
	if err != nil || total != 2 || affected != 1 || fake.writes != 0 {
		t.Fatalf("dry run = total %d affected %d writes %d err %v", total, affected, fake.writes, err)
	}
	if _, _, err := run(context.Background(), fake, true); err != nil || fake.writes != 1 {
		t.Fatalf("apply writes = %d, err %v", fake.writes, err)
	}
	_, affected, err = run(context.Background(), fake, true)
	if err != nil || affected != 0 || fake.writes != 1 {
		t.Fatalf("second apply = affected %d writes %d err %v", affected, fake.writes, err)
	}
}
