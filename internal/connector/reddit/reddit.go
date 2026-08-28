package reddit

import (
	"bytes"
	"context"
	"fmt"
	stdhtml "html"
	"net/http"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/nuntz/sema/internal/connector"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/extract"
	"github.com/nuntz/sema/internal/httpx"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const UserAgent = "linux:sema:rss"

type fetcher interface {
	Get(context.Context, string, http.Header) (httpx.Response, error)
}

type Connector struct{ client fetcher }

func New(client *httpx.Client) *Connector { return &Connector{client: client} }

func (c *Connector) Fetch(ctx context.Context, feed domain.Feed) (domain.FetchResult, error) {
	input, err := ParseInput(feed.URL)
	if err != nil {
		return domain.FetchResult{}, fmt.Errorf("normalize Reddit feed: %w", err)
	}
	headers := connector.ConditionalHeaders(feed)
	headers.Set("User-Agent", UserAgent)
	response, err := c.client.Get(ctx, feed.URL, headers)
	if err != nil {
		return domain.FetchResult{}, err
	}
	result, err := connector.ParseFeedResponse(response, feed)
	if err != nil || result.NotModified {
		return result, err
	}
	result.Title = Title(input.Subreddit)
	result.SiteURL = SiteURL(input.Subreddit)
	baseURL, _ := url.Parse(feed.URL)
	for index := range result.Entries {
		transformEntry(&result.Entries[index], baseURL)
	}
	return result, nil
}

func transformEntry(entry *domain.Entry, baseURL *url.URL) {
	entry.GUID = strings.TrimSpace(entry.GUID)
	entry.URL = strings.TrimSpace(entry.URL)
	entry.Author = strings.TrimSpace(entry.Author)
	for index := range entry.Enclosures {
		entry.Enclosures[index].URL = stdhtml.UnescapeString(strings.TrimSpace(entry.Enclosures[index].URL))
	}
	body, externalURL, fallbackImage := redditContent(entry.ContentRaw, entry.URL, baseURL)
	entry.ContentRaw = body
	entry.SummaryRaw = capSummary(cleanPlainText(extract.PlainText(body)))
	entry.ExternalURL = externalURL
	if len(entry.Enclosures) == 0 && fallbackImage != "" {
		entry.Enclosures = append(entry.Enclosures, domain.Enclosure{URL: fallbackImage, Type: "image/*"})
	}
	entry.PostType = inferPostType(externalURL, len(entry.Enclosures) > 0)
}

func redditContent(raw, discussionURL string, baseURL *url.URL) (body, externalURL, fallbackImage string) {
	contextNode := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(raw), contextNode)
	if err != nil {
		return "", "", ""
	}
	var bodyNode *html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "div":
				if bodyNode == nil && hasClass(node, "md") {
					bodyNode = node
				}
			case "a":
				if strings.EqualFold(strings.TrimSpace(nodeText(node)), "[link]") {
					candidate := connector.ResolveURL(baseURL, stdhtml.UnescapeString(attribute(node, "href")))
					if validHTTPURL(candidate) && !sameURL(candidate, discussionURL) {
						externalURL = candidate
					}
				}
			case "img":
				if fallbackImage == "" {
					candidate := connector.ResolveURL(baseURL, stdhtml.UnescapeString(attribute(node, "src")))
					if validHTTPURL(candidate) {
						fallbackImage = candidate
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	for _, node := range nodes {
		walk(node)
	}
	if bodyNode == nil {
		return "", externalURL, fallbackImage
	}
	var rendered bytes.Buffer
	for child := bodyNode.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&rendered, child); err != nil {
			return "", externalURL, fallbackImage
		}
	}
	return strings.TrimSpace(rendered.String()), externalURL, fallbackImage
}

func inferPostType(externalURL string, hasThumbnail bool) string {
	if externalURL == "" {
		return "text"
	}
	parsed, err := url.Parse(externalURL)
	if err != nil {
		return "link"
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "v.redd.it" {
		return "video"
	}
	if redditHost(host) && strings.HasPrefix(strings.ToLower(parsed.Path), "/gallery/") {
		return "gallery"
	}
	extension := strings.ToLower(path.Ext(parsed.Path))
	if host == "i.redd.it" || host == "preview.redd.it" {
		return "image"
	}
	switch extension {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif":
		if hasThumbnail || host == "i.redd.it" || host == "preview.redd.it" || strings.HasSuffix(host, ".redditmedia.com") {
			return "image"
		}
	}
	return "link"
}

func hasClass(node *html.Node, wanted string) bool {
	for _, class := range strings.Fields(attribute(node, "class")) {
		if class == wanted {
			return true
		}
	}
	return false
}

func attribute(node *html.Node, name string) string {
	for _, item := range node.Attr {
		if strings.EqualFold(item.Key, name) {
			return strings.TrimSpace(item.Val)
		}
	}
	return ""
}

func nodeText(node *html.Node) string {
	if node.Type == html.TextNode {
		return node.Data
	}
	var value strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		value.WriteString(nodeText(child))
	}
	return value.String()
}

func validHTTPURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil
}

func sameURL(first, second string) bool {
	normalize := func(raw string) string {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			return strings.TrimSpace(raw)
		}
		parsed.Fragment = ""
		parsed.Host = strings.ToLower(parsed.Host)
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
		return parsed.String()
	}
	return normalize(first) == normalize(second)
}

func capSummary(value string) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= domain.MaxSummaryRunes {
		return value
	}
	return string([]rune(value)[:domain.MaxSummaryRunes])
}

func cleanPlainText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	return strings.NewReplacer(" .", ".", " ,", ",", " ;", ";", " :", ":", " !", "!", " ?", "?").Replace(value)
}
