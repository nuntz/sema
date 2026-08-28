package connector

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	ext "github.com/mmcdole/gofeed/extensions"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
)

// ConditionalHeaders is shared by connectors whose validators are stored on
// the feed row. Callers may add connector-specific headers to the returned map.
func ConditionalHeaders(feed domain.Feed) http.Header {
	headers := make(http.Header)
	if feed.ETag != "" {
		headers.Set("If-None-Match", feed.ETag)
	}
	if feed.LastModified != "" {
		headers.Set("If-Modified-Since", feed.LastModified)
	}
	return headers
}

// ParseFeedResponse applies the shared status, validator, gofeed, URL, and
// Media RSS behavior used by RSS-compatible connectors.
func ParseFeedResponse(response httpx.Response, feed domain.Feed) (domain.FetchResult, error) {
	if response.StatusCode == http.StatusNotModified {
		return domain.FetchResult{NotModified: true, ETag: feed.ETag, Modified: feed.LastModified}, nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return domain.FetchResult{}, &HTTPStatusError{StatusCode: response.StatusCode, Header: response.Header.Clone()}
	}
	parsed, err := gofeed.NewParser().Parse(bytes.NewReader(response.Body))
	if err != nil {
		return domain.FetchResult{}, fmt.Errorf("parse feed: %w", err)
	}
	baseURL := response.FinalURL
	result := domain.FetchResult{
		Title:    parsed.Title,
		SiteURL:  ResolveURL(baseURL, parsed.Link),
		ETag:     response.Header.Get("ETag"),
		Modified: response.Header.Get("Last-Modified"),
		Entries:  make([]domain.Entry, 0, len(parsed.Items)),
	}
	for _, item := range parsed.Items {
		published := time.Now().UTC()
		displayDate := ""
		if item.PublishedParsed != nil {
			published = item.PublishedParsed.UTC()
			displayDate = domain.Timestamp(published)
		} else if item.UpdatedParsed != nil {
			published = item.UpdatedParsed.UTC()
			displayDate = domain.Timestamp(published)
		}
		author := ""
		if item.Author != nil {
			author = item.Author.Name
		}
		entry := domain.Entry{
			GUID: item.GUID, URL: ResolveURL(baseURL, item.Link), Title: strings.TrimSpace(item.Title),
			SummaryRaw: item.Description, ContentRaw: item.Content, Author: author,
			Published: published, DisplayDate: displayDate,
		}
		for _, enclosure := range item.Enclosures {
			entry.Enclosures = append(entry.Enclosures, domain.Enclosure{
				URL: ResolveURL(baseURL, enclosure.URL), Type: enclosure.Type, Length: enclosure.Length,
			})
		}
		entry.Enclosures = append(entry.Enclosures, mediaRSSEnclosures(item, baseURL)...)
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

func mediaRSSEnclosures(item *gofeed.Item, base *url.URL) []domain.Enclosure {
	seen := make(map[string]bool)
	result := []domain.Enclosure{}
	var walk func(map[string][]ext.Extension)
	walk = func(elements map[string][]ext.Extension) {
		for _, kind := range []string{"thumbnail", "content"} {
			for _, extension := range extensionsNamed(elements, kind) {
				imageURL := ResolveURL(base, extensionAttr(extension.Attrs, "url"))
				if imageURL == "" || seen[imageURL] {
					continue
				}
				mediaType := extensionAttr(extension.Attrs, "type")
				if kind == "thumbnail" || strings.EqualFold(extensionAttr(extension.Attrs, "medium"), "image") {
					if mediaType == "" {
						mediaType = "image/*"
					}
				}
				seen[imageURL] = true
				result = append(result, domain.Enclosure{URL: imageURL, Type: mediaType, Length: extensionAttr(extension.Attrs, "fileSize")})
			}
		}
		for _, matches := range elements {
			for _, extension := range matches {
				if len(extension.Children) > 0 {
					walk(extension.Children)
				}
			}
		}
	}
	for namespace, elements := range item.Extensions {
		if strings.EqualFold(namespace, "media") {
			walk(elements)
		}
	}
	return result
}

func extensionsNamed(elements map[string][]ext.Extension, name string) []ext.Extension {
	for key, matches := range elements {
		if strings.EqualFold(key, name) {
			return matches
		}
	}
	return nil
}

func extensionAttr(attributes map[string]string, name string) string {
	for key, value := range attributes {
		if strings.EqualFold(key, name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ResolveURL(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	reference, err := url.Parse(raw)
	if err != nil || base == nil {
		return raw
	}
	return base.ResolveReference(reference).String()
}
