package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/nuntz/sema/internal/store"
)

type fakeUnreadStore struct {
	reports map[string]store.UnreadMembership
	applied []string
}

func (f *fakeUnreadStore) UserIDs(context.Context) ([]string, error) {
	return []string{"one", "two"}, nil
}

func (f *fakeUnreadStore) ReconcileUnreadMembership(_ context.Context, userID string, apply bool) (store.UnreadMembership, error) {
	if apply {
		f.applied = append(f.applied, userID)
	}
	return f.reports[userID], nil
}

func TestBackfillUnreadPKSumsUsersAndOnlyWritesWithApply(t *testing.T) {
	fake := &fakeUnreadStore{reports: map[string]store.UnreadMembership{
		"one": {Live: 10, Missing: 3}, "two": {Live: 5, Missing: 1, Surplus: 2},
	}}
	var out bytes.Buffer
	total, err := run(context.Background(), fake, false, &out)
	if err != nil || total != (store.UnreadMembership{Live: 15, Missing: 4, Surplus: 2}) || len(fake.applied) != 0 {
		t.Fatalf("dry run = %+v, applied %v, err %v", total, fake.applied, err)
	}
	if out.String() != "mode=dry-run users=2 live=15 missing=4 surplus=2 repaired=0\n" {
		t.Fatalf("output = %q", out.String())
	}
	if _, err := run(context.Background(), fake, true, &out); err != nil || len(fake.applied) != 2 {
		t.Fatalf("apply = %v, err %v", fake.applied, err)
	}
}
