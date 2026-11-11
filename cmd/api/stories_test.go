package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

// Every filter a Story list depends on must be part of what it is cached
// under, so lists for different filters never share a render.
func TestRenderedStoriesAreCachedPerFilters(t *testing.T) {
	renders := 0
	db := &fakeAPIStore{clusters: func(context.Context, string) ([]domain.Cluster, error) {
		renders++
		return nil, nil
	}}
	s := &server{store: db, snapshotStore: &fakeSnapshotStore{}}
	load := func(user string, allowed map[string]bool, unread bool, tag string, window domain.FetchWindow, model domain.Model) {
		t.Helper()
		filters := storyFilters{Allowed: allowed, UnreadOnly: unread, Tag: tag, Window: window, Model: model}
		if _, _, err := s.loadAndRenderStories(context.Background(), s.requestSnapshot(user), filters); err != nil {
			t.Fatal(err)
		}
	}
	load("user", map[string]bool{"a": true, "b": true}, true, "", domain.FetchWindow{}, domain.Model{})
	load("user", map[string]bool{"b": true, "a": true}, true, "", domain.FetchWindow{}, domain.Model{})
	if renders != 1 {
		t.Fatalf("the same filters rendered %d times", renders)
	}
	load("other", map[string]bool{"a": true, "b": true}, true, "", domain.FetchWindow{}, domain.Model{})
	load("user", map[string]bool{"a": true}, true, "", domain.FetchWindow{}, domain.Model{})
	load("user", map[string]bool{"a": true}, false, "", domain.FetchWindow{}, domain.Model{})
	load("user", map[string]bool{"a": true}, false, "tag", domain.FetchWindow{}, domain.Model{})
	load("user", map[string]bool{"a": true}, false, "tag", domain.FetchWindow{From: time.Now()}, domain.Model{})
	load("user", map[string]bool{"a": true}, false, "tag", domain.FetchWindow{From: time.Now()}, domain.Model{SizeCutoffs: &domain.SizeCutoffs{P60: 1}})
	if renders != 7 {
		t.Fatalf("filters shared cached renders: renders = %d, want 7", renders)
	}
}

func TestStoryRenderingIgnoresMissingMembersWithoutWriting(t *testing.T) {
	now := time.Now()
	source := &fakeAPIStore{
		clusters: func(context.Context, string) ([]domain.Cluster, error) {
			return []domain.Cluster{{StoryID: "story", MemberIDs: []string{"dead", "archive", "expired", "lead", "member"}}}, nil
		},
		resolveConsistent: func(context.Context, string, []string, map[string]bool) ([]domain.Item, error) {
			return []domain.Item{
				{ItemID: "archive", SK: "A#archive"}, {ItemID: "expired", TTL: now.Unix()},
				{ItemID: "lead", FeedID: "one", TTL: now.Add(time.Hour).Unix()},
				{ItemID: "member", FeedID: "two", TTL: now.Add(time.Hour).Unix()},
			}, nil
		},
	}
	s := &server{store: source, snapshotStore: &fakeSnapshotStore{}}
	stories, hidden, err := s.renderStories(context.Background(), s.requestSnapshot("user"), storyFilters{}, nil)
	if err != nil || len(stories) != 1 || len(stories[0].Items) != 2 || !hidden["lead"] || !hidden["member"] {
		t.Fatalf("stories=%+v hidden=%v error=%v", stories, hidden, err)
	}
}

// A Story is cached under the FEEDBACK_VERSION read before it renders. An
// eventually consistent read of a Lead's Boost or Keep could predate that
// version and serve it reverted until the Story expires (#38).
func TestStoriesRenderFeedbackReadStrongly(t *testing.T) {
	now := time.Now()
	lead := domain.Item{ItemID: "lead", FeedID: "one", SK: "I#lead", ArchiveSK: "A#lead", TTL: now.Add(time.Hour).Unix()}
	source := &fakeAPIStore{
		clusters: func(context.Context, string) ([]domain.Cluster, error) {
			return []domain.Cluster{{StoryID: "story", MemberIDs: []string{"lead", "member"}}}, nil
		},
		resolve: func(context.Context, string, []string, map[string]bool) ([]domain.Item, error) {
			t.Fatal("Story Members were resolved eventually consistent")
			return nil, nil
		},
		resolveConsistent: func(_ context.Context, _ string, _ []string, read map[string]bool) ([]domain.Item, error) {
			if read == nil {
				t.Fatal("Story Members must take Read state from the Request snapshot")
			}
			return []domain.Item{lead, {ItemID: "member", FeedID: "two", SK: "I#member", TTL: now.Add(time.Hour).Unix()}}, nil
		},
		signalValues: func(context.Context, string, []string) (map[string]int, error) {
			t.Fatal("Story Signal values were read eventually consistent")
			return nil, nil
		},
		signalValuesConsistent: func(context.Context, string, []string) (map[string]int, error) {
			return map[string]int{"lead": 1}, nil
		},
	}
	s := &server{store: source, snapshotStore: withFeeds([]domain.Feed{{FeedID: "one"}, {FeedID: "two"}})}
	got, _ := handleSignedIn(t, s, http.MethodGet, "/api/stories", map[string]string{"include_read": "true"})
	if got.StatusCode != http.StatusOK {
		t.Fatalf("stories = %d: %s", got.StatusCode, got.Body)
	}
	var body struct {
		Stories []struct {
			Items []struct {
				ItemID  string `json:"item_id"`
				Signal  int    `json:"signal"`
				Hearted bool   `json:"hearted"`
			} `json:"items"`
		} `json:"stories"`
	}
	decodeJSONBody(t, got.Body, &body)
	if len(body.Stories) != 1 || len(body.Stories[0].Items) == 0 {
		t.Fatalf("stories = %s", got.Body)
	}
	for _, item := range body.Stories[0].Items {
		if item.ItemID == "lead" && (item.Signal != 1 || !item.Hearted) {
			t.Fatalf("Lead = %+v, want Boosted and Kept", item)
		}
	}
}

func TestConditionalStories(t *testing.T) {
	original := response(200, map[string]any{"stories": []string{"one"}})
	first := conditionalStories(original, nil)
	etag := first.Headers["etag"]
	if etag == "" || first.StatusCode != 200 || first.Body != original.Body {
		t.Fatalf("initial response = %#v", first)
	}
	for _, match := range []string{etag, "W/" + etag, `"other", ` + etag, "*"} {
		got := conditionalStories(original, map[string]string{"If-None-Match": match})
		if got.StatusCode != 304 || got.Body != "" || got.Headers["etag"] != etag {
			t.Fatalf("match %q: %#v", match, got)
		}
	}
	changed := conditionalStories(response(200, map[string]any{"stories": []string{"two"}}), map[string]string{"if-none-match": etag})
	if changed.StatusCode != 200 || changed.Headers["etag"] == etag {
		t.Fatalf("changed response = %#v", changed)
	}
	failed := conditionalStories(response(500, nil), map[string]string{"if-none-match": "*"})
	if failed.StatusCode != 500 || failed.Headers["etag"] != "" {
		t.Fatalf("failure = %#v", failed)
	}
}
