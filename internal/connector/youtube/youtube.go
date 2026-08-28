package youtube

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nuntz/sema/internal/connector"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
	"golang.org/x/net/html"
)

const (
	feedBase  = "https://www.youtube.com/feeds/videos.xml?channel_id="
	watchBase = "https://www.youtube.com/watch?v="
)

var channelIDPattern = regexp.MustCompile(`(?i)(?:"channelId"\s*:\s*"|/channel/|content=["'])(UC[A-Za-z0-9_-]+)`)
var channelAvatarPattern = regexp.MustCompile(`https:(?:\\?/){2}yt3\.(?:ggpht|googleusercontent)\.com(?:\\?/)[^"'<> ]+`)

type fetcher interface {
	Get(context.Context, string, http.Header) (httpx.Response, error)
}

type Connector struct{ client fetcher }

func New(client *httpx.Client) *Connector { return &Connector{client: client} }

func (c *Connector) Fetch(ctx context.Context, feed domain.Feed) (domain.FetchResult, error) {
	response, err := c.client.Get(ctx, feed.URL, connector.ConditionalHeaders(feed))
	if err != nil {
		return domain.FetchResult{}, err
	}
	if response.StatusCode == http.StatusNotModified {
		return domain.FetchResult{NotModified: true, ETag: feed.ETag, Modified: feed.LastModified}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.FetchResult{}, &connector.HTTPStatusError{StatusCode: response.StatusCode, Header: response.Header.Clone()}
	}
	result, err := parseFeed(response.Body)
	if err != nil {
		return domain.FetchResult{}, fmt.Errorf("parse YouTube feed: %w", err)
	}
	result.ETag = response.Header.Get("ETag")
	result.Modified = response.Header.Get("Last-Modified")
	return result, nil
}

type atomFeed struct {
	Title   string      `xml:"title"`
	Links   []atomLink  `xml:"link"`
	Entries []atomEntry `xml:"entry"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

type atomEntry struct {
	ID        string     `xml:"id"`
	VideoID   string     `xml:"videoId"`
	Title     string     `xml:"title"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
	Links     []atomLink `xml:"link"`
	Author    struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Media struct {
		Title       string `xml:"title"`
		Description string `xml:"description"`
		Thumbnails  []struct {
			URL string `xml:"url,attr"`
		} `xml:"thumbnail"`
	} `xml:"group"`
}

func parseFeed(body []byte) (domain.FetchResult, error) {
	var parsed atomFeed
	decoder := xml.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&parsed); err != nil {
		return domain.FetchResult{}, err
	}
	result := domain.FetchResult{Title: strings.TrimSpace(parsed.Title), Entries: make([]domain.Entry, 0, len(parsed.Entries))}
	for _, link := range parsed.Links {
		if link.Rel == "alternate" || result.SiteURL == "" {
			result.SiteURL = strings.TrimSpace(link.Href)
		}
		if link.Rel == "alternate" {
			break
		}
	}
	for _, item := range parsed.Entries {
		videoID := strings.TrimSpace(item.VideoID)
		if videoID == "" {
			videoID = strings.TrimPrefix(strings.TrimSpace(item.ID), "yt:video:")
		}
		if videoID == "" {
			continue
		}
		published, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(item.Published))
		if err != nil {
			published, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(item.Updated))
		}
		if err != nil {
			return domain.FetchResult{}, fmt.Errorf("video %s publication date: %w", videoID, err)
		}
		title := strings.TrimSpace(item.Media.Title)
		if title == "" {
			title = strings.TrimSpace(item.Title)
		}
		description := strings.TrimSpace(item.Media.Description)
		entry := domain.Entry{
			GUID: "yt:video:" + videoID, URL: watchBase + url.QueryEscape(videoID), Title: title,
			ContentRaw: description, Author: strings.TrimSpace(item.Author.Name), Published: published.UTC(),
			DisplayDate: domain.Timestamp(published), VideoID: videoID,
		}
		if len(item.Media.Thumbnails) > 0 && strings.TrimSpace(item.Media.Thumbnails[0].URL) != "" {
			entry.Enclosures = []domain.Enclosure{{URL: strings.TrimSpace(item.Media.Thumbnails[0].URL), Type: "image/jpeg"}}
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

type Discoverer struct{ client fetcher }

func NewDiscoverer(client *httpx.Client) *Discoverer { return &Discoverer{client: client} }

func IsYouTubeInput(raw string) bool {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "@") && !strings.Contains(raw, "/") {
		return true
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	return err == nil && youtubeHost(parsed.Hostname())
}

func IsFeedURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && youtubeHost(parsed.Hostname()) && channelIDFromFeedURL(parsed) != ""
}

func (d *Discoverer) Discover(ctx context.Context, raw string) ([]domain.FeedCandidate, error) {
	normalized, err := normalizeURL(raw)
	if err != nil {
		return nil, err
	}
	parsed, _ := url.Parse(normalized)
	if !youtubeHost(parsed.Hostname()) {
		return nil, fmt.Errorf("not a YouTube URL")
	}

	channelID := channelIDFromURL(parsed)
	pageURL := normalized
	if feedChannelID := channelIDFromFeedURL(parsed); feedChannelID != "" {
		channelID = feedChannelID
		pageURL = "https://www.youtube.com/channel/" + url.PathEscape(channelID)
	}
	page, err := d.get(ctx, pageURL)
	if err != nil {
		return nil, err
	}
	metadata := pageMetadata(page)
	if channelID == "" {
		channelID = extractChannelID(page.Body)
	}
	if channelID == "" {
		return nil, fmt.Errorf("YouTube page does not identify a channel")
	}
	feedURL := feedBase + url.QueryEscape(channelID)
	feedResponse, err := d.get(ctx, feedURL)
	if err != nil {
		return nil, err
	}
	result, err := parseFeed(feedResponse.Body)
	if err != nil {
		return nil, fmt.Errorf("parse YouTube channel feed: %w", err)
	}
	title := strings.TrimSpace(result.Title)
	if title == "" {
		title = metadata.title
	}
	siteURL := strings.TrimSpace(result.SiteURL)
	if siteURL == "" {
		siteURL = "https://www.youtube.com/channel/" + channelID
	}
	newest := ""
	if len(result.Entries) > 0 {
		newest = domain.Timestamp(result.Entries[0].Published)
		for _, entry := range result.Entries[1:] {
			if domain.Timestamp(entry.Published) > newest {
				newest = domain.Timestamp(entry.Published)
			}
		}
	}
	return []domain.FeedCandidate{{
		FeedURL: feedURL, Title: title, Type: "uploads", Connector: domain.ConnectorYouTube,
		SiteURL: siteURL, BadgeURL: metadata.avatar, AvatarURL: metadata.avatar, Cadence: cadence(result.Entries),
		ItemCount: len(result.Entries), NewestItemTS: newest,
	}}, nil
}

func (d *Discoverer) get(ctx context.Context, rawURL string) (httpx.Response, error) {
	response, err := d.client.Get(ctx, rawURL, nil)
	if err != nil {
		return httpx.Response{}, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return httpx.Response{}, fmt.Errorf("GET %s: HTTP %d", rawURL, response.StatusCode)
	}
	if response.FinalURL == nil {
		response.FinalURL, _ = url.Parse(rawURL)
	}
	return response, nil
}

type metadata struct{ title, avatar string }

func pageMetadata(response httpx.Response) metadata {
	document, err := html.Parse(bytes.NewReader(response.Body))
	if err != nil {
		return metadata{}
	}
	result := metadata{}
	if match := channelAvatarPattern.Find(response.Body); len(match) > 0 {
		result.avatar = strings.NewReplacer(`\/`, `/`, `\u0026`, `&`).Replace(string(match))
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, "meta") {
			attributes := nodeAttributes(node)
			name := strings.ToLower(firstNonEmpty(attributes["property"], attributes["name"], attributes["itemprop"]))
			content := strings.TrimSpace(attributes["content"])
			switch name {
			case "og:title", "title":
				if result.title == "" {
					result.title = strings.TrimSuffix(content, " - YouTube")
				}
			case "og:image", "thumbnailurl":
				if result.avatar == "" {
					if reference, parseErr := url.Parse(content); parseErr == nil && response.FinalURL != nil {
						result.avatar = response.FinalURL.ResolveReference(reference).String()
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return result
}

func nodeAttributes(node *html.Node) map[string]string {
	attributes := make(map[string]string, len(node.Attr))
	for _, attribute := range node.Attr {
		attributes[strings.ToLower(attribute.Key)] = strings.TrimSpace(attribute.Val)
	}
	return attributes
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func extractChannelID(body []byte) string {
	match := channelIDPattern.FindSubmatch(body)
	if len(match) == 2 {
		return string(match[1])
	}
	return ""
}

func channelIDFromURL(parsed *url.URL) string {
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) >= 2 && strings.EqualFold(parts[0], "channel") && strings.HasPrefix(parts[1], "UC") {
		return parts[1]
	}
	return ""
}

func channelIDFromFeedURL(parsed *url.URL) string {
	if !strings.EqualFold(strings.TrimRight(parsed.Path, "/"), "/feeds/videos.xml") {
		return ""
	}
	value := strings.TrimSpace(parsed.Query().Get("channel_id"))
	if strings.HasPrefix(value, "UC") {
		return value
	}
	return ""
}

func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "@") && !strings.Contains(raw, "/") {
		raw = "https://www.youtube.com/" + raw
	} else if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("invalid HTTP URL %q", raw)
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func youtubeHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "youtube.com" || strings.HasSuffix(host, ".youtube.com") || host == "youtu.be"
}

func cadence(entries []domain.Entry) string {
	if len(entries) < 2 {
		return ""
	}
	dates := make([]time.Time, 0, len(entries))
	for _, entry := range entries {
		dates = append(dates, entry.Published)
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
	weeks := dates[len(dates)-1].Sub(dates[0]).Hours() / (24 * 7)
	if weeks <= 0 {
		return ""
	}
	perWeek := float64(len(dates)-1) / weeks
	switch {
	case perWeek >= 7:
		return fmt.Sprintf("~%d / wk", int(perWeek+0.5))
	case perWeek >= 1:
		low := max(1, int(perWeek))
		return fmt.Sprintf("%d–%d / wk", low, low+1)
	default:
		perMonth := max(1, int(perWeek*4.35+0.5))
		return fmt.Sprintf("~%d / mo", perMonth)
	}
}

type headClient interface {
	Head(context.Context, string, http.Header) (httpx.Response, error)
}

type ShortsDetector struct{ client headClient }

func NewShortsDetector(client *httpx.Client) *ShortsDetector { return &ShortsDetector{client: client} }

func (d *ShortsDetector) IsShort(ctx context.Context, videoID string) bool {
	response, err := d.client.Head(ctx, "https://www.youtube.com/shorts/"+url.PathEscape(videoID), nil)
	return err == nil && response.StatusCode == http.StatusOK
}
