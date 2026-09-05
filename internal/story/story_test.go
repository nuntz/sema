package story

import (
	"math"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

func storyItem(id, feed string, published time.Time) domain.Item {
	return domain.Item{ItemID: id, FeedID: feed, URL: "https://example.com/" + id, PublishedTS: domain.Timestamp(published)}
}

func TestQualifiesThresholdWindowAndURLs(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	config := Config{Threshold: 80, Window: 72 * time.Hour}
	item := storyItem("new", "new-feed", now)
	candidate := storyItem("candidate", "old-feed", now.Add(-72*time.Hour))
	if !Qualifies(candidate, item, 80, config) {
		t.Fatal("threshold and window boundaries should qualify")
	}
	if Qualifies(candidate, item, 79, config) {
		t.Fatal("similarity below threshold qualified")
	}
	candidate.PublishedTS = domain.Timestamp(now.Add(-72*time.Hour - time.Nanosecond))
	if Qualifies(candidate, item, 100, config) {
		t.Fatal("item outside window qualified")
	}
	candidate.PublishedTS = domain.Timestamp(now)
	candidate.URL = "https://news.example/article#comments"
	item.URL = "https://reddit.example/post"
	item.ExternalURL = "https://NEWS.example/article"
	if !Qualifies(candidate, item, 1, config) {
		t.Fatal("url/external_url match should override similarity")
	}
	candidate.URL, candidate.ExternalURL = "https://other.example/post", "https://news.example/article#share"
	item.URL, item.ExternalURL = "https://news.example/article", ""
	if !Qualifies(candidate, item, 1, config) {
		t.Fatal("external_url/url reverse match should qualify")
	}
}

func TestFromEnvDefaultsAndOverrides(t *testing.T) {
	t.Setenv("STORY_SIMILARITY", "")
	t.Setenv("STORY_WINDOW_HOURS", "")
	if got := FromEnv(); got.Threshold != 80 || got.Window != 72*time.Hour {
		t.Fatalf("defaults = %#v", got)
	}
	t.Setenv("STORY_SIMILARITY", "91")
	t.Setenv("STORY_WINDOW_HOURS", "24")
	if got := FromEnv(); got.Threshold != 91 || got.Window != 24*time.Hour {
		t.Fatalf("overrides = %#v", got)
	}
}

func TestChoosePrefersHighestSimilarityExistingStory(t *testing.T) {
	storyID, found := Choose([]Candidate{
		{Item: domain.Item{ItemID: "a", StoryID: "low"}, Similarity: 82},
		{Item: domain.Item{ItemID: "b"}, Similarity: 99},
		{Item: domain.Item{ItemID: "c", StoryID: "high"}, Similarity: 91},
	})
	if !found || storyID != "high" {
		t.Fatalf("Choose = %q, %t", storyID, found)
	}
	if _, found := Choose([]Candidate{{Item: domain.Item{ItemID: "b"}, Similarity: 99}}); found {
		t.Fatal("unclustered candidates should not report a story")
	}
}

func TestLeadTieBreaks(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	items := []domain.Item{
		{ItemID: "plain", Score: 1, ExtractQuality: .9, PublishedTS: domain.Timestamp(now)},
		{ItemID: "media-low", Score: 1, MediaKey: "image", ExtractQuality: .2, PublishedTS: domain.Timestamp(now)},
		{ItemID: "media-quality", Score: 1, MediaKey: "image", ExtractQuality: .8, PublishedTS: domain.Timestamp(now)},
		{ItemID: "winner", Score: 1, MediaKey: "image", ExtractQuality: .8, PublishedTS: domain.Timestamp(now.Add(-time.Hour))},
	}
	if got := Lead(items); got.ItemID != "winner" {
		t.Fatalf("Lead = %q", got.ItemID)
	}
}

func TestRenderDemotionUnreadOrderingAndHiddenIDs(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	rows := []domain.Story{{StoryID: "broad"}, {StoryID: "same-feed"}, {StoryID: "tagged-down"}, {StoryID: "read"}}
	members := map[string][]domain.Item{
		"broad": {
			{ItemID: "b1", FeedID: "a", Score: 1, PublishedTS: domain.Timestamp(now.Add(-time.Hour))},
			{ItemID: "b2", FeedID: "b", Score: .8, PublishedTS: domain.Timestamp(now)},
			{ItemID: "b3", FeedID: "c", Score: .7, PublishedTS: domain.Timestamp(now.Add(-2 * time.Hour)), Read: true},
		},
		"same-feed":   {{ItemID: "s1", FeedID: "a"}, {ItemID: "s2", FeedID: "a"}},
		"tagged-down": {{ItemID: "t1", FeedID: "a"}, {ItemID: "t2", FeedID: "x"}},
		"read":        {{ItemID: "r1", FeedID: "a", Read: true}, {ItemID: "r2", FeedID: "b", Read: true}},
	}
	rendered, hidden := Render(rows, members, map[string]bool{"a": true, "b": true, "c": true}, true, domain.Model{})
	if len(rendered) != 1 || rendered[0].StoryID != "broad" || rendered[0].SourceCount != 3 {
		t.Fatalf("rendered = %#v", rendered)
	}
	if rendered[0].Items[0].ItemID != "b1" || rendered[0].Items[1].ItemID != "b2" {
		t.Fatalf("member order = %#v", rendered[0].Items)
	}
	if len(hidden) != 3 || !hidden["b1"] || !hidden["b2"] || !hidden["b3"] || hidden["t1"] {
		t.Fatalf("hidden = %#v", hidden)
	}
}

func TestOrderKeyStoryOrderingAndSize(t *testing.T) {
	if got := OrderKey(domain.Item{Score: 2}, 8); math.Abs(got-3.2) > 1e-9 {
		t.Fatalf("OrderKey = %v", got)
	}
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	rows := []domain.Story{{StoryID: "narrow"}, {StoryID: "broad"}, {StoryID: "newer"}}
	members := map[string][]domain.Item{
		"narrow": {{ItemID: "n1", FeedID: "a", Score: 1.2, PublishedTS: domain.Timestamp(now)}, {ItemID: "n2", FeedID: "b"}},
		"broad":  {{ItemID: "b1", FeedID: "a", Score: 1, PublishedTS: domain.Timestamp(now)}, {ItemID: "b2", FeedID: "b"}, {ItemID: "b3", FeedID: "c"}, {ItemID: "b4", FeedID: "d"}},
		"newer":  {{ItemID: "x1", FeedID: "a", Score: 1, PublishedTS: domain.Timestamp(now.Add(time.Hour))}, {ItemID: "x2", FeedID: "b"}, {ItemID: "x3", FeedID: "c"}, {ItemID: "x4", FeedID: "d"}},
	}
	model := domain.Model{ExplicitCount: 10, SizeCutoffs: &domain.SizeCutoffs{P60: 1.3, P90: 1.5}}
	rendered, _ := Render(rows, members, nil, false, model)
	if got := []string{rendered[0].StoryID, rendered[1].StoryID, rendered[2].StoryID}; got[0] != "newer" || got[1] != "broad" || got[2] != "narrow" {
		t.Fatalf("order = %#v", got)
	}
	if math.Abs(rendered[0].OrderKey-1.45) > 1e-9 || rendered[0].Size != "M" {
		t.Fatalf("newer ordering = %#v", rendered[0])
	}
}
