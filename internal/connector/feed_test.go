package connector

import (
	"strings"
	"testing"
)

func TestEntryTitleFallsBackToPostText(t *testing.T) {
	long := "<p>" + strings.Repeat("Bluesky post text ", 20) + "</p>"
	tests := []struct {
		name, title, summary, content string
		wantPrefix                    string
	}{
		{name: "feed title", title: "  Provided title  ", summary: "ignored", wantPrefix: "Provided title"},
		{name: "summary", summary: "<p>A title-less social post.</p>", wantPrefix: "A title-less social post."},
		{name: "content", content: "<p>Content-only post.</p>", wantPrefix: "Content-only post."},
		{name: "truncated", summary: long, wantPrefix: "Bluesky post text"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := EntryTitle(test.title, test.summary, test.content)
			if !strings.HasPrefix(got, test.wantPrefix) {
				t.Fatalf("EntryTitle() = %q", got)
			}
			if test.name == "truncated" && (len([]rune(got)) != derivedTitleRunes+1 || !strings.HasSuffix(got, "…")) {
				t.Fatalf("truncated title = %q (%d runes)", got, len([]rune(got)))
			}
		})
	}
}
