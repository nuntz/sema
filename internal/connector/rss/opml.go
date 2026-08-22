package rss

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strings"
)

type Subscription struct {
	Title string
	URL   string
}

const YouTubeUnsupportedReason = "YouTube channel feeds are not supported because YouTube blocks feed requests from AWS"

func UnsupportedReason(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	path := strings.TrimSuffix(parsed.Path, "/")
	if (host == "youtube.com" || strings.HasSuffix(host, ".youtube.com")) && strings.EqualFold(path, "/feeds/videos.xml") {
		return YouTubeUnsupportedReason
	}
	return ""
}

func ParseOPML(reader io.Reader) ([]Subscription, error) {
	decoder := xml.NewDecoder(io.LimitReader(reader, 5<<20))
	seen := make(map[string]bool)
	var subscriptions []Subscription
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse OPML: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || !strings.EqualFold(start.Name.Local, "outline") {
			continue
		}
		var title, feedURL string
		for _, attribute := range start.Attr {
			switch strings.ToLower(attribute.Name.Local) {
			case "xmlurl":
				feedURL = strings.TrimSpace(attribute.Value)
			case "title", "text":
				if title == "" {
					title = strings.TrimSpace(attribute.Value)
				}
			}
		}
		if feedURL != "" && !seen[feedURL] {
			seen[feedURL] = true
			subscriptions = append(subscriptions, Subscription{Title: title, URL: feedURL})
		}
	}
	if len(subscriptions) == 0 {
		return nil, fmt.Errorf("OPML contains no feed outlines")
	}
	return subscriptions, nil
}
