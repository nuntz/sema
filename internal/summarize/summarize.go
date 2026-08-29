package summarize

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nuntz/sema/internal/extract"
)

const (
	MaxWords     = 50
	MaxBodyRunes = 6000 // Approximately 1,500 model tokens for article prose.
)

type Summarizer interface {
	Summarize(context.Context, string, string) (string, error)
}

type TextProvider interface {
	Generate(context.Context, string) (string, error)
}

type Service struct{ provider TextProvider }

func New(provider TextProvider) *Service { return &Service{provider: provider} }

func (s *Service) Summarize(ctx context.Context, title, body string) (string, error) {
	if s == nil || s.provider == nil {
		return "", fmt.Errorf("summary provider is not configured")
	}
	generated, err := s.provider.Generate(ctx, Prompt(title, body))
	if err != nil {
		return "", err
	}
	generated = strings.Trim(strings.Join(strings.Fields(generated), " "), " \t\r\n\"")
	if generated == "" {
		return "", fmt.Errorf("summary provider returned empty output")
	}
	if len(strings.Fields(generated)) > MaxWords {
		return "", fmt.Errorf("summary provider returned more than %d words", MaxWords)
	}
	if sentenceCount(generated) > 3 {
		generated = firstSentences(generated, 2)
	}
	return generated, nil
}

func Prompt(title, body string) string {
	body = strings.Join(strings.Fields(extract.PlainText(body)), " ")
	body = capRunes(body, MaxBodyRunes)
	return "Write exactly two sentences totaling no more than 50 words. Keep the source language. " +
		"State only facts present in the source, without editorializing, a preamble, or phrases such as ‘this article discusses’.\n\n" +
		"Title: " + strings.TrimSpace(title) + "\n\nSource:\n" + body
}

var truncationEnding = regexp.MustCompile(`(?i)(?:…|\.\.\.|\[\s*(?:…|\.\.\.)?\s*\]|(?:read\s+more|continue\s+reading)[\s\p{P}\p{S}]*)\s*$`)
var markupResidue = regexp.MustCompile(`(?s)<[^>]*>|&(?:#[0-9]+|#x[0-9a-fA-F]+|[A-Za-z][A-Za-z0-9]+);`)
var summaryURL = regexp.MustCompile(`(?i)https?://[^\s<>]+`)

// IsJunk classifies the feed-provided summary before a body fallback is used.
func IsJunk(summaryRaw, title string, alwaysGenerate bool) bool {
	if alwaysGenerate {
		return true
	}
	plain := strings.TrimSpace(extract.PlainText(summaryRaw))
	if utf8.RuneCountInString(plain) < 40 {
		return true
	}
	if normalizeComparable(plain) == normalizeComparable(title) {
		return true
	}
	if truncationEnding.MatchString(plain) {
		return true
	}
	links := summaryURL.FindAllString(plain, -1)
	if len(links) >= 2 {
		withoutLinks := strings.TrimSpace(summaryURL.ReplaceAllString(plain, ""))
		if utf8.RuneCountInString(withoutLinks) < 40 {
			return true
		}
	}
	plainRunes := utf8.RuneCountInString(plain)
	residueRunes := 0
	for _, match := range markupResidue.FindAllString(plain, -1) {
		residueRunes += utf8.RuneCountInString(match)
	}
	return plainRunes > 0 && float64(residueRunes)/float64(plainRunes) > 0.3
}

func normalizeComparable(value string) string {
	value = strings.ToLower(strings.TrimSpace(extract.PlainText(value)))
	var normalized strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			normalized.WriteRune(r)
		}
	}
	return normalized.String()
}

func sentenceCount(value string) int {
	return len(sentenceEnds(value))
}

func firstSentences(value string, count int) string {
	ends := sentenceEnds(value)
	if len(ends) <= count {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(string([]rune(value)[:ends[count-1]]))
}

func sentenceEnds(value string) []int {
	runes := []rune(value)
	ends := []int{}
	lastEnd := 0
	for index := 0; index < len(runes); index++ {
		if !strings.ContainsRune(".!?。！？", runes[index]) {
			continue
		}
		end := index
		for end+1 < len(runes) && strings.ContainsRune(".!?。！？", runes[end+1]) {
			end++
		}
		if runes[index] == '.' && !periodEndsSentence(runes, index, end) {
			continue
		}
		for end+1 < len(runes) && strings.ContainsRune("\"'”’)]}", runes[end+1]) {
			end++
		}
		if end+1 < len(runes) && !unicode.IsSpace(runes[end+1]) {
			continue
		}
		ends = append(ends, end+1)
		lastEnd = end + 1
		index = end
	}
	if strings.TrimSpace(string(runes[lastEnd:])) != "" {
		ends = append(ends, len(runes))
	}
	return ends
}

func periodEndsSentence(runes []rune, index, clusterEnd int) bool {
	if clusterEnd > index {
		return true
	}
	if index > 0 && index+1 < len(runes) && unicode.IsDigit(runes[index-1]) && unicode.IsDigit(runes[index+1]) {
		return false
	}
	if index+1 < len(runes) && !unicode.IsSpace(runes[index+1]) && !strings.ContainsRune("\"'”’)]}", runes[index+1]) {
		return false
	}
	start := index
	for start > 0 && (unicode.IsLetter(runes[start-1]) || runes[start-1] == '.') {
		start--
	}
	token := strings.ToLower(strings.Trim(string(runes[start:index]), "."))
	if token == "" {
		return true
	}
	if nonEndingAbbreviation(token) || dottedInitialism(token) {
		return false
	}
	if token == "a.m" || token == "p.m" || token == "etc" {
		next := nextNonSpace(runes, index+1)
		return next < 0 || unicode.IsUpper(runes[next])
	}
	return true
}

func nonEndingAbbreviation(token string) bool {
	switch token {
	case "mr", "mrs", "ms", "dr", "prof", "sr", "jr", "st", "vs", "e.g", "i.e", "fig", "no", "inc", "ltd", "co":
		return true
	default:
		return len([]rune(token)) == 1
	}
}

func dottedInitialism(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if len([]rune(part)) != 1 {
			return false
		}
	}
	return true
}

func nextNonSpace(runes []rune, start int) int {
	for index := start; index < len(runes); index++ {
		if !unicode.IsSpace(runes[index]) && !strings.ContainsRune("\"'“‘([{", runes[index]) {
			return index
		}
	}
	return -1
}

func capRunes(value string, count int) string {
	runes := []rune(value)
	if len(runes) <= count {
		return value
	}
	return string(runes[:count])
}
