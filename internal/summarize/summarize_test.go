package summarize

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubProvider struct {
	output string
	err    error
	prompt string
}

func (s *stubProvider) Generate(_ context.Context, prompt string) (string, error) {
	s.prompt = prompt
	return s.output, s.err
}

func TestJunkDetection(t *testing.T) {
	tests := []struct {
		name, summary, title string
		always               bool
		want                 bool
	}{
		{name: "short", summary: strings.Repeat("a", 39), want: true},
		{name: "length boundary", summary: strings.Repeat("a", 40)},
		{name: "real 41 chars", summary: strings.Repeat("a", 41)},
		{name: "title", summary: "A Useful Title With Enough Characters To Pass", title: "a useful title—with enough characters to pass", want: true},
		{name: "ellipsis", summary: "A useful teaser that reaches the length threshold but stops…", want: true},
		{name: "read more", summary: "A useful teaser that reaches the length threshold. Read more", want: true},
		{name: "read more symbol", summary: "A useful teaser that reaches the length threshold. Read more »", want: true},
		{name: "continue reading", summary: "A useful teaser that reaches the length threshold. Continue reading...", want: true},
		{name: "markup residue", summary: `&lt;div&gt;&lt;b&gt;&lt;i&gt;forty useful characters remain here now&lt;/i&gt;&lt;/b&gt;&lt;/div&gt;`, want: true},
		{name: "normal html", summary: `<p>A complete and useful summary with more than forty characters for the reader.</p>`},
		{name: "always", summary: "A complete and useful summary with more than forty characters for the reader.", always: true, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsJunk(test.summary, test.title, test.always); got != test.want {
				t.Fatalf("IsJunk() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPromptAndOutputContract(t *testing.T) {
	provider := &stubProvider{output: "First factual sentence. Second factual sentence."}
	service := New(provider)
	got, err := service.Summarize(context.Background(), "Titel", strings.Repeat("Körper ", 2000))
	if err != nil {
		t.Fatal(err)
	}
	if got != provider.output || !strings.Contains(provider.prompt, "exactly two sentences") || !strings.Contains(provider.prompt, "Keep the source language") || !strings.Contains(provider.prompt, "Titel") {
		t.Fatalf("summary = %q, prompt = %q", got, provider.prompt)
	}
	if len([]rune(provider.prompt)) > MaxBodyRunes+500 {
		t.Fatalf("prompt did not cap body: %d runes", len([]rune(provider.prompt)))
	}
}

func TestSummaryProviderFailureAndInvalidOutput(t *testing.T) {
	for _, provider := range []*stubProvider{
		{err: errors.New("offline")},
		{output: "Only one sentence."},
		{output: strings.Repeat("word ", 51) + ". Another sentence."},
	} {
		if _, err := New(provider).Summarize(context.Background(), "Title", "Body"); err == nil {
			t.Fatal("Summarize() error = nil")
		}
	}
}
