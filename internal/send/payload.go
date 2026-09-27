// Package send builds, signs, and delivers Send payloads to a user's
// Destination. The wire format is a versioned public contract.
package send

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

const (
	Version       = 1
	EventItemSend = "item.send"
	EventPing     = "ping"

	// MaxPayloadBytes caps a Send body; text is shortened before anything else.
	MaxPayloadBytes = 64 * 1024
)

type envelope struct {
	Version int          `json:"version"`
	Event   string       `json:"event"`
	SentAt  string       `json:"sent_at"`
	UserID  string       `json:"user_id"`
	Item    *itemPayload `json:"item,omitempty"`
}

type itemPayload struct {
	ID            string         `json:"id"`
	URL           string         `json:"url"`
	DiscussionURL string         `json:"discussion_url,omitempty"`
	Title         string         `json:"title,omitempty"`
	Summary       string         `json:"summary,omitempty"`
	Author        string         `json:"author,omitempty"`
	PublishedAt   string         `json:"published_at,omitempty"`
	Feed          feedPayload    `json:"feed"`
	Images        []imagePayload `json:"images"`
	Kept          bool           `json:"kept"`
	Tags          []string       `json:"tags"`
}

type feedPayload struct {
	Connector string `json:"connector"`
	Title     string `json:"title,omitempty"`
	URL       string `json:"url,omitempty"`
}

type imagePayload struct {
	URL       string `json:"url"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Role      string `json:"role"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// CopyImage is a short-lived signed URL to Sema's own stored copy of the
// lead image. It always follows the origin image, which is usually larger.
type CopyImage struct {
	URL       string
	Width     int
	Height    int
	ExpiresAt time.Time
}

// ItemPayload builds the item.send body. receiverUserID is the Destination's
// opaque user id, never the identity-provider subject.
func ItemPayload(item domain.Item, feed domain.Feed, receiverUserID string, sentAt time.Time, copyImage *CopyImage) ([]byte, error) {
	link, discussion := originalLinks(item)
	body := itemPayload{
		ID:            item.ItemID,
		URL:           link,
		DiscussionURL: discussion,
		Title:         item.Title,
		Summary:       item.Summary,
		Author:        item.Author,
		PublishedAt:   formatTime(item.PublishedTS),
		Feed:          feedFields(item, feed),
		Images:        images(item, copyImage),
		Kept:          item.Hearted || item.ArchiveSK != "" || domain.IsArchive(item),
		Tags:          sortedTags(feed.Tags),
	}
	message := envelope{Version: Version, Event: EventItemSend, SentAt: sentAt.UTC().Format(time.RFC3339), UserID: receiverUserID, Item: &body}
	// Shorten text first, then drop optional fields one at a time; an Item
	// whose required fields alone exceed the cap cannot be sent.
	drops := []func(){
		func() { body.Author = "" },
		func() { body.Tags = []string{} },
		func() { body.DiscussionURL = "" },
		func() { body.Feed.Title, body.Feed.URL = "", "" },
		func() { body.Images = []imagePayload{} },
	}
	for {
		encoded, err := marshal(message)
		if err != nil || len(encoded) <= MaxPayloadBytes {
			return encoded, err
		}
		if shortenLongest(&body.Title, &body.Summary) {
			continue
		}
		if len(drops) == 0 {
			return nil, fmt.Errorf("payload is %d bytes, over the %d byte limit", len(encoded), MaxPayloadBytes)
		}
		drops[0]()
		drops = drops[1:]
	}
}

// shortenLongest halves the longer of two texts, reporting false once both
// are empty and nothing more can be trimmed.
func shortenLongest(first, second *string) bool {
	target := first
	if len(*second) > len(*first) {
		target = second
	}
	runes := []rune(*target)
	if len(runes) == 0 {
		return false
	}
	*target = string(runes[:len(runes)/2])
	return true
}

// originalLinks mirrors the reader's "open original" link: a Reddit link post
// opens its external article, and its thread becomes the discussion URL.
func originalLinks(item domain.Item) (string, string) {
	if item.Connector == domain.ConnectorReddit && item.PostType == "link" && item.ExternalURL != "" {
		return item.ExternalURL, item.URL
	}
	return item.URL, ""
}

func feedFields(item domain.Item, feed domain.Feed) feedPayload {
	title := firstNonEmpty(feed.CustomTitle, feed.Title, item.FeedTitle)
	connector := domain.FeedConnector(feed)
	if feed.FeedID == "" && item.Connector != "" {
		connector = item.Connector
	}
	return feedPayload{Connector: connector, Title: title, URL: firstNonEmpty(feed.SiteURL, feed.URL)}
}

func images(item domain.Item, copyImage *CopyImage) []imagePayload {
	out := []imagePayload{}
	if absoluteHTTP(item.MediaSourceURL) {
		// Stored dimensions describe Sema's resized copy, not the publisher's
		// original, so the origin entry carries none.
		out = append(out, imagePayload{URL: item.MediaSourceURL, Role: "hero"})
	}
	if copyImage != nil && absoluteHTTP(copyImage.URL) {
		out = append(out, imagePayload{
			URL: copyImage.URL, Width: copyImage.Width, Height: copyImage.Height, Role: "copy",
			ExpiresAt: copyImage.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

// marshal keeps URLs readable: signed URLs carry '&', which the default
// encoder would escape.
func marshal(value any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

func absoluteHTTP(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}

func formatTime(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return ""
	}
	return parsed.UTC().Format(time.RFC3339)
}

func sortedTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag = strings.TrimSpace(tag); tag != "" {
			out = append(out, tag)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// PingPayload builds the body for the Destination test event.
func PingPayload(receiverUserID string, sentAt time.Time) ([]byte, error) {
	return marshal(envelope{Version: Version, Event: EventPing, SentAt: sentAt.UTC().Format(time.RFC3339), UserID: receiverUserID})
}
