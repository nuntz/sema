package rss

import (
	"bytes"
	"context"
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

type Connector struct {
	client fetcher
	parser *gofeed.Parser
}

type fetcher interface {
	Get(context.Context, string, http.Header) (httpx.Response, error)
}

func New(client *httpx.Client) *Connector {
	return &Connector{client: client, parser: gofeed.NewParser()}
}

func (c *Connector) Fetch(ctx context.Context, feed domain.Feed) (domain.FetchResult, error) {
	headers := make(http.Header)
	if feed.ETag != "" {
		headers.Set("If-None-Match", feed.ETag)
	}
	if feed.LastModified != "" {
		headers.Set("If-Modified-Since", feed.LastModified)
	}
	response, err := c.client.Get(ctx, feed.URL, headers)
	if err != nil {
		return domain.FetchResult{}, err
	}
	if response.StatusCode == http.StatusNotModified {
		return domain.FetchResult{NotModified: true, ETag: feed.ETag, Modified: feed.LastModified}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.FetchResult{}, fmt.Errorf("feed returned HTTP %d", response.StatusCode)
	}
	parsed, err := c.parser.Parse(bytes.NewReader(response.Body))
	if err != nil {
		return domain.FetchResult{}, fmt.Errorf("parse feed: %w", err)
	}
	baseURL := response.FinalURL
	result := domain.FetchResult{
		Title:    parsed.Title,
		SiteURL:  resolve(baseURL, parsed.Link),
		ETag:     response.Header.Get("ETag"),
		Modified: response.Header.Get("Last-Modified"),
		Entries:  make([]domain.Entry, 0, len(parsed.Items)),
	}
	for _, item := range parsed.Items {
		published := time.Now().UTC()
		if item.PublishedParsed != nil {
			published = item.PublishedParsed.UTC()
		} else if item.UpdatedParsed != nil {
			published = item.UpdatedParsed.UTC()
		}
		author := ""
		if item.Author != nil {
			author = item.Author.Name
		}
		entry := domain.Entry{
			GUID:       item.GUID,
			URL:        resolve(baseURL, item.Link),
			Title:      strings.TrimSpace(item.Title),
			SummaryRaw: item.Description,
			ContentRaw: item.Content,
			Author:     author,
			Published:  published,
		}
		for _, enclosure := range item.Enclosures {
			entry.Enclosures = append(entry.Enclosures, domain.Enclosure{
				URL: resolve(baseURL, enclosure.URL), Type: enclosure.Type, Length: enclosure.Length,
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
			matches := extensionsNamed(elements, kind)
			for _, extension := range matches {
				imageURL := resolve(base, extensionAttr(extension.Attrs, "url"))
				if imageURL != "" && !seen[imageURL] {
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

func resolve(base *url.URL, raw string) string {
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
