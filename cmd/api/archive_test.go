package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

func TestArchiveFiltersAcrossPages(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query map[string]string
		want  []string
	}{
		{"tag", map[string]string{"tag": " TeCh "}, []string{"tech"}},
		{"feed", map[string]string{"feed": "tech"}, []string{"tech"}},
		{"untagged", map[string]string{"tag": "__untagged"}, []string{"plain"}},
		{"missing", map[string]string{"tag": "missing"}, []string{}},
		{"all", map[string]string{}, []string{"muted", "plain", "tech", "deleted"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []string{"muted", "plain", "tech", "deleted"}
			calls := 0
			db := &fakeAPIStore{archives: func(_ context.Context, _ string, cursor string, _ int) ([]domain.Item, string, error) {
				index := 0
				if cursor != "" {
					var err error
					index, err = strconv.Atoi(cursor)
					if err != nil {
						t.Fatal(err)
					}
				}
				calls++
				id := source[index]
				next := ""
				if index+1 < len(source) {
					next = strconv.Itoa(index + 1)
				}
				return []domain.Item{{SK: "A#" + id, ItemID: id, FeedID: id, Archived: true, Hearted: true}}, next, nil
			}}
			s := &server{store: db, feedCache: map[string]cachedFeedList{"user": {loaded: time.Now(), feeds: []domain.Feed{
				{FeedID: "muted", Muted: true, Tags: []string{"tech"}}, {FeedID: "plain"}, {FeedID: "tech", Tags: []string{"tech"}},
			}}}}
			tc.query["limit"] = "1"
			got := []string{}
			for {
				response := s.getArchive(context.Background(), "user", tc.query)
				if response.StatusCode != http.StatusOK {
					t.Fatalf("response = %d %s", response.StatusCode, response.Body)
				}
				var body struct {
					Items []domain.Item `json:"items"`
					Next  string        `json:"next_cursor"`
				}
				if err := json.Unmarshal([]byte(response.Body), &body); err != nil {
					t.Fatal(err)
				}
				for _, item := range body.Items {
					if !item.Archived || !item.Hearted {
						t.Fatal("archive flags missing")
					}
					got = append(got, item.ItemID)
				}
				if body.Next == "" {
					break
				}
				if len(body.Items) == 0 {
					t.Fatal("empty intermediate filtered page")
				}
				tc.query["cursor"] = body.Next
			}
			if calls != len(source) {
				t.Fatalf("queries = %d", calls)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("items = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("items = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
