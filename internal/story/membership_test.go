package story

import (
	"testing"

	"github.com/nuntz/sema/internal/domain"
)

func TestReconcileLiveMembership(t *testing.T) {
	cluster := domain.Cluster{MemberIDs: []string{"a", "b", "a", "expired", "archive", "missing"}}
	rows := map[string]domain.Item{
		"a":       {ItemID: "a", FeedID: "one", TTL: 101, ArchiveSK: "A#kept"},
		"b":       {ItemID: "b", FeedID: "one", TTL: 110},
		"expired": {ItemID: "expired", FeedID: "two", TTL: 100},
		"archive": {ItemID: "archive", FeedID: "two", SK: "A#archive"},
	}
	plan := Reconcile(cluster, rows, 100)
	if plan.Dissolve || len(plan.Members) != 2 || plan.Cluster.TTL != 110 || IsStory(plan.Members) {
		t.Fatalf("same-Source Cluster = %+v", plan)
	}
	rows["b"] = domain.Item{ItemID: "b", FeedID: "two", TTL: 110}
	if !IsStory(Reconcile(cluster, rows, 100).Members) {
		t.Fatal("two Sources must form a Story")
	}
	if !Reconcile(cluster, rows, 101).Dissolve {
		t.Fatal("one remaining Member must dissolve")
	}
	if len(cluster.MemberIDs) != 6 {
		t.Fatal("mutated original membership")
	}
}
