// Package reddit recognizes Reddit addresses so Sema can refuse them. Reddit
// ended its public feeds and Sema no longer reads Reddit; see ADR 0009.
package reddit

import (
	"net/url"
	"strings"
)

// Matches reports whether raw names a subreddit (`r/name`) or any reddit.com
// address.
func Matches(raw string) bool {
	value := strings.TrimSpace(raw)
	if lower := strings.ToLower(value); strings.HasPrefix(lower, "r/") || strings.HasPrefix(lower, "/r/") {
		return true
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	return err == nil && redditHost(parsed.Hostname())
}

func redditHost(host string) bool {
	switch strings.ToLower(strings.TrimSuffix(host, ".")) {
	case "reddit.com", "www.reddit.com", "old.reddit.com", "new.reddit.com", "np.reddit.com", "m.reddit.com":
		return true
	default:
		return false
	}
}
