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
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
	"golang.org/x/net/html"
)

var commonFeedPaths = []string{
	"/feed", "/rss", "/rss.xml", "/atom.xml", "/index.xml", "/feed.json", "/feeds/posts/default",
}

type Candidate = domain.FeedCandidate

type Discoverer struct {
	client fetcher
	parser *gofeed.Parser
}

func NewDiscoverer(client *httpx.Client) *Discoverer {
	return &Discoverer{client: client, parser: gofeed.NewParser()}
}

func (d *Discoverer) Discover(ctx context.Context, rawURL string) ([]Candidate, error) {
	normalized, err := normalizeDiscoveryURL(rawURL)
	if err != nil {
		return nil, err
	}
	page, err := d.get(ctx, normalized)
	if err != nil {
		return nil, err
	}
	if candidate, ok := d.parseCandidate(page); ok {
		return []Candidate{candidate}, nil
	}

	links := alternateFeedLinks(page)
	if len(links) > 0 {
		return d.fetchCandidates(ctx, links, 5), nil
	}

	origin := &url.URL{Scheme: page.FinalURL.Scheme, Host: page.FinalURL.Host}
	probes := make([]Candidate, 0, 2)
	for _, path := range commonFeedPaths {
		response, getErr := d.get(ctx, origin.ResolveReference(&url.URL{Path: path}).String())
		if getErr != nil {
			continue
		}
		if candidate, ok := d.parseCandidate(response); ok {
			probes = append(probes, candidate)
			if len(probes) == 2 {
				break
			}
		}
	}
	return probes, nil
}

func (d *Discoverer) get(ctx context.Context, rawURL string) (httpx.Response, error) {
	response, err := d.client.Get(ctx, rawURL, nil)
	if err != nil {
		return httpx.Response{}, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return httpx.Response{}, fmt.Errorf("GET %s: HTTP %d", rawURL, response.StatusCode)
	}
	return response, nil
}

func (d *Discoverer) fetchCandidates(ctx context.Context, rawURLs []string, limit int) []Candidate {
	result := make([]Candidate, 0, min(limit, len(rawURLs)))
	seen := make(map[string]bool)
	for _, rawURL := range rawURLs {
		if len(result) == limit {
			break
		}
		if seen[rawURL] {
			continue
		}
		seen[rawURL] = true
		response, err := d.get(ctx, rawURL)
		if err != nil {
			continue
		}
		candidate, ok := d.parseCandidate(response)
		if !ok || seen[candidate.FeedURL] && candidate.FeedURL != rawURL {
			continue
		}
		seen[candidate.FeedURL] = true
		result = append(result, candidate)
	}
	return result
}

func (d *Discoverer) parseCandidate(response httpx.Response) (Candidate, bool) {
	parsed, err := d.parser.Parse(bytes.NewReader(response.Body))
	if err != nil {
		return Candidate{}, false
	}
	newest := time.Time{}
	for _, item := range parsed.Items {
		published := item.PublishedParsed
		if published == nil {
			published = item.UpdatedParsed
		}
		if published != nil && published.After(newest) {
			newest = published.UTC()
		}
	}
	timestamp := ""
	if !newest.IsZero() {
		timestamp = newest.Format(time.RFC3339Nano)
	}
	title := strings.TrimSpace(parsed.Title)
	if title == "" {
		title = response.FinalURL.Hostname()
	}
	return Candidate{
		FeedURL: response.FinalURL.String(), Title: title, Type: parsed.FeedType, Connector: domain.ConnectorRSS,
		ItemCount: len(parsed.Items), NewestItemTS: timestamp,
	}, true
}

func alternateFeedLinks(response httpx.Response) []string {
	document, err := html.Parse(bytes.NewReader(response.Body))
	if err != nil {
		return nil
	}
	allowed := map[string]bool{
		"application/rss+xml": true, "application/atom+xml": true,
		"application/feed+json": true, "application/json": true,
	}
	links := make([]string, 0)
	seen := make(map[string]bool)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, "link") {
			attributes := make(map[string]string)
			for _, attribute := range node.Attr {
				attributes[strings.ToLower(attribute.Key)] = strings.TrimSpace(attribute.Val)
			}
			rels := strings.Fields(strings.ToLower(attributes["rel"]))
			alternate := false
			for _, rel := range rels {
				alternate = alternate || rel == "alternate"
			}
			mediaType := strings.ToLower(strings.TrimSpace(strings.Split(attributes["type"], ";")[0]))
			if alternate && allowed[mediaType] && attributes["href"] != "" {
				if reference, parseErr := url.Parse(attributes["href"]); parseErr == nil {
					resolved := response.FinalURL.ResolveReference(reference).String()
					if !seen[resolved] {
						seen[resolved] = true
						links = append(links, resolved)
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return links
}

func normalizeDiscoveryURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("url is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("invalid HTTP URL %q", raw)
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}
