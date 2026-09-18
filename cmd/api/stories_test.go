package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

func TestRenderedStoriesCache(t *testing.T) {
	calls := 0
	fail := false
	db := &fakeAPIStore{clusters: func(context.Context, string) ([]domain.Cluster, error) {
		calls++
		if fail {
			return nil, errors.New("unavailable")
		}
		return nil, nil
	}}
	s := &server{store: db}
	load := func(user string, allowed map[string]bool, unread bool, tag string, window domain.FetchWindow) {
		t.Helper()
		if _, _, err := s.loadAndRenderStories(context.Background(), user, allowed, unread, domain.Model{}, tag, window, nil); err != nil {
			t.Fatal(err)
		}
	}
	load("user", map[string]bool{"a": true, "b": true}, true, "", domain.FetchWindow{})
	load("user", map[string]bool{"b": true, "a": true}, true, "", domain.FetchWindow{})
	if calls != 1 {
		t.Fatalf("two consecutive renders loaded store %d times", calls)
	}
	load("other", map[string]bool{"a": true, "b": true}, true, "", domain.FetchWindow{})
	load("user", map[string]bool{"a": true}, true, "", domain.FetchWindow{})
	load("user", map[string]bool{"a": true}, false, "", domain.FetchWindow{})
	load("user", map[string]bool{"a": true}, false, "tag", domain.FetchWindow{})
	load("user", map[string]bool{"a": true}, false, "tag", domain.FetchWindow{From: time.Now()})
	if calls != 6 {
		t.Fatalf("filters shared cached results: calls = %d", calls)
	}
	s.invalidateStories("user")
	load("other", map[string]bool{"a": true, "b": true}, true, "", domain.FetchWindow{})
	load("user", map[string]bool{"a": true, "b": true}, true, "", domain.FetchWindow{})
	if calls != 7 {
		t.Fatalf("invalidation calls = %d", calls)
	}
	for key, entry := range s.storyCache {
		entry.loaded = time.Now().Add(-storyCacheTTL)
		s.storyCache[key] = entry
	}
	load("user", map[string]bool{"a": true, "b": true}, true, "", domain.FetchWindow{})
	if calls != 8 {
		t.Fatalf("expired cache calls = %d", calls)
	}
	s.invalidateStories("user")
	fail = true
	if _, _, err := s.loadAndRenderStories(context.Background(), "user", nil, true, domain.Model{}, "", domain.FetchWindow{}, nil); err == nil {
		t.Fatal("expected store error")
	}
	fail = false
	load("user", nil, true, "", domain.FetchWindow{})
	if calls != 10 {
		t.Fatalf("error was cached: calls = %d", calls)
	}
	for _, action := range []string{"heart", "signal"} {
		t.Run(action+" invalidation", func(t *testing.T) {
			item := domain.Item{PK: "U#user", SK: "I#item", ItemID: "item", TTL: time.Now().Add(time.Hour).Unix()}
			if action == "heart" {
				item.ArchiveSK = "A#archive"
			}
			db.item = func(context.Context, string, string) (domain.Item, error) { return item, nil }
			db.setHeart = func(context.Context, string, string, bool) (string, int, error) { return "A#archive", 1, nil }
			db.setSignal = func(context.Context, string, domain.Item, int) error { return nil }
			load("user", nil, true, "", domain.FetchWindow{})
			load("other", nil, true, "", domain.FetchWindow{})
			before := calls
			body := `{"hearted":true}`
			if action == "signal" {
				body = `{"value":0}`
			}
			got := s.itemRoute(context.Background(), "user", "POST", "item/"+action, body)
			if got.StatusCode != 200 {
				t.Fatalf("mutation = %d, %s", got.StatusCode, got.Body)
			}
			load("user", nil, true, "", domain.FetchWindow{})
			load("other", nil, true, "", domain.FetchWindow{})
			if calls != before+1 {
				t.Fatalf("mutation did not invalidate only this user: calls=%d, before=%d", calls, before)
			}
		})
	}

}

func TestStoryRenderingIgnoresMissingMembersWithoutWriting(t *testing.T) {
	now := time.Now()
	source := &fakeAPIStore{
		clusters: func(context.Context, string) ([]domain.Cluster, error) {
			return []domain.Cluster{{StoryID: "story", MemberIDs: []string{"dead", "archive", "expired", "lead", "member"}}}, nil
		},
		resolve: func(context.Context, string, []string, map[string]bool) ([]domain.Item, error) {
			return []domain.Item{
				{ItemID: "archive", SK: "A#archive"}, {ItemID: "expired", TTL: now.Unix()},
				{ItemID: "lead", FeedID: "one", TTL: now.Add(time.Hour).Unix()},
				{ItemID: "member", FeedID: "two", TTL: now.Add(time.Hour).Unix()},
			}, nil
		},
	}
	s := &server{store: source, feedCache: map[string]cachedFeedList{"user": {loaded: now}}}
	stories, hidden, err := s.renderStories(context.Background(), "user", nil, false, domain.Model{}, "", domain.FetchWindow{}, nil)
	if err != nil || len(stories) != 1 || len(stories[0].Items) != 2 || !hidden["lead"] || !hidden["member"] {
		t.Fatalf("stories=%+v hidden=%v error=%v", stories, hidden, err)
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
