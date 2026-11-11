package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/dynamotest"
	"github.com/nuntz/sema/internal/store"
)

// Lambda serves a user's requests from separate warm instances, each caching
// rendered Stories for four minutes. A Story read on one instance used to come
// back in the unread Front Page served by another that still held it (#19).
func TestReadStoryDoesNotComeBackOnAnotherInstance(t *testing.T) {
	db, table := dynamotest.NewTable(t)
	repository := store.New(db, nil, table, "", "")
	now := time.Now().UTC()
	putTaggedFeedItems(t, repository, now, "news", "news-1")
	putTaggedFeedItems(t, repository, now, "wire", "wire-1")
	if err := repository.PutCluster(context.Background(), domain.Cluster{
		PK: domain.UserPK("user"), SK: domain.ClusterSK("story"), StoryID: "story", MemberIDs: []string{"news-1", "wire-1"},
		CreatedAt: domain.Timestamp(now), UpdatedAt: domain.Timestamp(now), TTL: now.Add(domain.Retention).Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	writer, reader := &server{store: repository}, &server{store: repository}
	if got, _ := unreadStoryIDs(t, reader); len(got) != 1 || got[0] != "story" {
		t.Fatalf("unread Stories before reading = %v, want the Story", got)
	}

	mustOK(t, writer, http.MethodPost, "/api/items/read-batch", nil, `{"ids":["news-1","wire-1"]}`)

	got, event := unreadStoryIDs(t, reader)
	if len(got) != 0 {
		t.Fatalf("unread Stories after another instance read the Story = %v, want none", got)
	}
	if event["StoryCache"] != "stale" {
		t.Errorf("StoryCache = %q, want stale: the reader's rendered Stories predate the Read", event["StoryCache"])
	}
}

// unreadStoryIDs returns the IDs of the unread Front Page's Stories, and the
// request event.
func unreadStoryIDs(t *testing.T, s *server) ([]string, map[string]string) {
	t.Helper()
	got, event := handleSignedIn(t, s, http.MethodGet, "/api/stories", nil)
	if got.StatusCode != http.StatusOK {
		t.Fatalf("stories = %d: %s", got.StatusCode, got.Body)
	}
	var body struct {
		Stories []struct {
			StoryID string `json:"story_id"`
		} `json:"stories"`
	}
	decodeJSONBody(t, got.Body, &body)
	ids := make([]string, 0, len(body.Stories))
	for _, story := range body.Stories {
		ids = append(ids, story.StoryID)
	}
	return ids, event
}
