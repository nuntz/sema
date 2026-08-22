package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
)

func FeedID(rawURL string) string {
	return hashID(normalizeURL(rawURL))
}

func ItemID(feedID, guid, itemURL string) string {
	identity := strings.TrimSpace(guid)
	if identity == "" {
		identity = normalizeURL(itemURL)
	}
	return hashID(feedID + "\x00" + identity)
}

func hashID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

func normalizeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimSpace(raw)
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	return u.String()
}
