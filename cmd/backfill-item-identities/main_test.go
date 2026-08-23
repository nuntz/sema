package main

import (
	"testing"

	"github.com/nuntz/sema/internal/domain"
)

func TestGroupItemsKeepsNewestRowAndCountsDuplicates(t *testing.T) {
	items := []domain.Item{
		{PK: "U#user", SK: "I#old", ItemID: "same", PublishedTS: "2026-08-21T12:00:00Z"},
		{PK: "U#user", SK: "I#other", ItemID: "other", PublishedTS: "2026-08-20T12:00:00Z"},
		{PK: "U#user", SK: "I#new", ItemID: "same", PublishedTS: "2026-08-22T12:00:00Z"},
	}
	groups := groupItems(items)
	if len(groups) != 2 || groups[1].canonical.SK != "I#new" || len(groups[1].duplicates) != 1 || groups[1].duplicates[0].SK != "I#old" {
		t.Fatalf("groups = %#v", groups)
	}
}
