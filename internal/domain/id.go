package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
)

func FeedID(rawURL string) string {
	return hashID(NormalizeURL(rawURL))
}

func ItemID(feedID, guid, itemURL string) string {
	identity := strings.TrimSpace(guid)
	if identity == "" {
		identity = NormalizeURL(itemURL)
	}
	return hashID(feedID + "\x00" + identity)
}

func hashID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

// NormalizeURL is the canonical URL comparison used for feed and story identity.
func NormalizeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimSpace(raw)
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	return u.String()
}

func normalizeURL(raw string) string { return NormalizeURL(raw) }
