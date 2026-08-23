package rss

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

type Subscription struct {
	Title     string
	URL       string
	Tags      []string
	Muted     bool
	IntervalH int
}

func UnsupportedReason(rawURL string) string {
	// YouTube feeds were previously rejected because the upstream occasionally
	// blocked AWS egress. They are now first-class discovery/import targets; keep
	// the hook for compatibility with callers that display unsupported entries.
	_ = rawURL
	return ""
}

func ParseOPML(reader io.Reader) ([]Subscription, error) {
	decoder := xml.NewDecoder(io.LimitReader(reader, 5<<20))
	seen := make(map[string]int)
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
		var tags []string
		muted := false
		interval := 1
		for _, attribute := range start.Attr {
			switch strings.ToLower(attribute.Name.Local) {
			case "xmlurl":
				feedURL = strings.TrimSpace(attribute.Value)
			case "title", "text":
				if title == "" {
					title = strings.TrimSpace(attribute.Value)
				}
			case "category":
				tags = splitTags(attribute.Value)
			case "muted":
				muted = strings.EqualFold(strings.TrimSpace(attribute.Value), "true")
			case "interval":
				switch strings.ToLower(strings.TrimSpace(attribute.Value)) {
				case "6h":
					interval = 6
				case "24h":
					interval = 24
				}
			}
		}
		if feedURL == "" {
			continue
		}
		if index, ok := seen[feedURL]; ok {
			existing := &subscriptions[index]
			existing.Tags = mergeTags(existing.Tags, tags)
			if existing.Title == "" {
				existing.Title = title
			}
			existing.Muted = existing.Muted || muted
			if interval > existing.IntervalH {
				existing.IntervalH = interval
			}
			continue
		}
		seen[feedURL] = len(subscriptions)
		subscriptions = append(subscriptions, Subscription{Title: title, URL: feedURL, Tags: tags, Muted: muted, IntervalH: interval})
	}
	if len(subscriptions) == 0 {
		return nil, fmt.Errorf("OPML contains no feed outlines")
	}
	return subscriptions, nil
}

func ExportOPML(subscriptions []Subscription) ([]byte, error) {
	type outline struct {
		Text     string `xml:"text,attr"`
		Title    string `xml:"title,attr,omitempty"`
		Type     string `xml:"type,attr"`
		XMLURL   string `xml:"xmlUrl,attr"`
		Category string `xml:"category,attr,omitempty"`
		Muted    string `xml:"sema:muted,attr,omitempty"`
		Interval string `xml:"sema:interval,attr,omitempty"`
	}
	type document struct {
		XMLName xml.Name `xml:"opml"`
		Version string   `xml:"version,attr"`
		Sema    string   `xml:"xmlns:sema,attr"`
		Head    struct {
			Title string `xml:"title"`
		} `xml:"head"`
		Body struct {
			Outlines []outline `xml:"outline"`
		} `xml:"body"`
	}

	value := document{Version: "2.0", Sema: "https://sema.app/opml"}
	value.Head.Title = "Sema feeds"
	for _, subscription := range subscriptions {
		title := strings.TrimSpace(subscription.Title)
		if title == "" {
			title = subscription.URL
		}
		entry := outline{Text: title, Title: title, Type: "rss", XMLURL: subscription.URL, Category: strings.Join(splitTags(strings.Join(subscription.Tags, ",")), ",")}
		if subscription.Muted {
			entry.Muted = "true"
		}
		switch subscription.IntervalH {
		case 6, 24:
			entry.Interval = fmt.Sprintf("%dh", subscription.IntervalH)
		default:
			entry.Interval = "1h"
		}
		value.Body.Outlines = append(value.Body.Outlines, entry)
	}
	var output bytes.Buffer
	output.WriteString(xml.Header)
	encoder := xml.NewEncoder(&output)
	encoder.Indent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encode OPML: %w", err)
	}
	return output.Bytes(), nil
}

func splitTags(value string) []string {
	tags := make([]string, 0)
	seen := make(map[string]bool)
	for _, raw := range strings.Split(value, ",") {
		tag := strings.ToLower(strings.TrimSpace(raw))
		if tag != "" && !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	return tags
}

func mergeTags(first, second []string) []string {
	return splitTags(strings.Join(append(append([]string{}, first...), second...), ","))
}
