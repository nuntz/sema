package story

import "github.com/nuntz/sema/internal/domain"

// LiveMembers preserves membership order and removes missing, expired, Archive
// and duplicate rows. Being kept does not remove a live Member.
func LiveMembers(cluster domain.Cluster, resolved map[string]domain.Item, now int64) []domain.Item {
	members := make([]domain.Item, 0, len(cluster.MemberIDs))
	seen := make(map[string]bool)
	for _, id := range cluster.MemberIDs {
		item, ok := resolved[id]
		if !ok || seen[id] || !domain.Live(item, now) {
			continue
		}
		seen[id] = true
		members = append(members, item)
	}
	return members
}

func IsStory(members []domain.Item) bool { return SourceCount(members) >= 2 }

type Reconciliation struct {
	Cluster  domain.Cluster
	Members  []domain.Item
	Dissolve bool
}

// Reconcile owns Cluster lifetime, distinct from Story qualification: a
// same-Source Cluster is retained for future membership but never rendered as
// a Story. Fewer than two live Members no longer need a Cluster.
func Reconcile(cluster domain.Cluster, resolved map[string]domain.Item, now int64) Reconciliation {
	members := LiveMembers(cluster, resolved, now)
	cluster.MemberIDs = make([]string, 0, len(members))
	cluster.TTL = 0
	for _, item := range members {
		cluster.MemberIDs = append(cluster.MemberIDs, item.ItemID)
		cluster.TTL = max(cluster.TTL, item.TTL)
	}
	return Reconciliation{Cluster: cluster, Members: members, Dissolve: len(members) < 2}
}
