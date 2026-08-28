package reddit

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type Sort string

const (
	SortHot    Sort = "hot"
	SortTopDay Sort = "top-day"
	SortNew    Sort = "new"
)

type Input struct {
	Subreddit string
	Sort      Sort
}

var subredditPattern = regexp.MustCompile(`^[A-Za-z0-9_]{2,21}$`)

func Matches(raw string) bool {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(value), "r/") || strings.HasPrefix(strings.ToLower(value), "/r/") {
		return true
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	return err == nil && redditHost(parsed.Hostname())
}

func ParseInput(raw string) (Input, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return Input{}, fmt.Errorf("subreddit address is required")
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "r/") || strings.HasPrefix(lower, "/r/") {
		name := strings.TrimPrefix(strings.TrimPrefix(value, "/"), "r/")
		if len(name) >= 2 && strings.EqualFold(name[:2], "r/") {
			name = name[2:]
		}
		name = strings.TrimSuffix(name, "/")
		if strings.ContainsAny(name, "/?#") {
			return Input{}, unsupportedInput()
		}
		return validatedInput(name, SortTopDay)
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || !redditHost(parsed.Hostname()) {
		return Input{}, unsupportedInput()
	}
	parts := splitPath(parsed.EscapedPath())
	if len(parts) < 2 || !strings.EqualFold(parts[0], "r") {
		return Input{}, unsupportedInput()
	}
	name, err := url.PathUnescape(parts[1])
	if err != nil {
		return Input{}, unsupportedInput()
	}
	sort := SortTopDay
	switch {
	case len(parts) == 2:
		// Irrelevant query parameters on a subreddit page are discarded.
	case len(parts) == 3 && strings.EqualFold(parts[2], ".rss"):
		sort = SortHot
	case len(parts) == 3 && strings.EqualFold(parts[2], "top.rss"):
		values := parsed.Query()
		if len(values["t"]) != 1 || !strings.EqualFold(values.Get("t"), "day") {
			return Input{}, unsupportedInput()
		}
		sort = SortTopDay
	case len(parts) == 3 && strings.EqualFold(parts[2], "new.rss"):
		sort = SortNew
	default:
		return Input{}, unsupportedInput()
	}
	return validatedInput(name, sort)
}

func CanonicalURL(subreddit string, sort Sort) string {
	base := "https://www.reddit.com/r/" + strings.ToLower(subreddit)
	switch sort {
	case SortHot:
		return base + "/.rss"
	case SortNew:
		return base + "/new.rss"
	default:
		return base + "/top.rss?t=day"
	}
}

func SiteURL(subreddit string) string {
	return "https://www.reddit.com/r/" + strings.ToLower(subreddit) + "/"
}

func Title(subreddit string) string { return "r/" + strings.ToLower(subreddit) }

func IntervalHours(sort Sort) int {
	switch sort {
	case SortHot:
		return 3
	case SortNew:
		return 1
	default:
		return 24
	}
}

func SortLabel(sort Sort) string {
	switch sort {
	case SortHot:
		return "Hot"
	case SortNew:
		return "New"
	default:
		return "Top · day"
	}
}

func redditHost(host string) bool {
	switch strings.ToLower(strings.TrimSuffix(host, ".")) {
	case "reddit.com", "www.reddit.com", "old.reddit.com", "np.reddit.com", "m.reddit.com":
		return true
	default:
		return false
	}
}

func validatedInput(name string, sort Sort) (Input, error) {
	if !subredditPattern.MatchString(name) {
		return Input{}, unsupportedInput()
	}
	return Input{Subreddit: strings.ToLower(name), Sort: sort}, nil
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "/")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func unsupportedInput() error {
	return fmt.Errorf("multireddits, user feeds, post URLs, and bare subreddit names aren't supported")
}
