package extract

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-shiori/go-readability"
	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type Result struct {
	Title          string
	HTML           string
	Text           string
	FirstParagraph string
	LeadImage      string
}

func Article(document []byte, pageURL *url.URL) (Result, error) {
	article, err := readability.FromReader(bytes.NewReader(document), pageURL)
	if err != nil {
		return Result{}, fmt.Errorf("readability: %w", err)
	}
	cleaned, err := Sanitize(article.Content, pageURL)
	if err != nil {
		return Result{}, err
	}
	text := PlainText(cleaned)
	if strings.TrimSpace(text) == "" {
		return Result{}, fmt.Errorf("readability returned empty content")
	}
	return Result{
		Title:          strings.TrimSpace(article.Title),
		HTML:           cleaned,
		Text:           text,
		FirstParagraph: FirstParagraph(cleaned),
		LeadImage:      resolveURL(pageURL, article.Image),
	}, nil
}

func FeedContent(raw string, pageURL *url.URL) (Result, error) {
	cleaned, err := Sanitize(raw, pageURL)
	if err != nil {
		return Result{}, err
	}
	text := PlainText(cleaned)
	if strings.TrimSpace(text) == "" {
		return Result{}, fmt.Errorf("feed content is empty")
	}
	return Result{HTML: cleaned, Text: text, FirstParagraph: FirstParagraph(cleaned)}, nil
}

func Substantial(raw string) bool { return utf8.RuneCountInString(PlainText(raw)) > 1500 }

func Summary(summaryRaw, firstParagraph string) string {
	value := strings.TrimSpace(PlainText(summaryRaw))
	if value == "" {
		value = strings.TrimSpace(PlainText(firstParagraph))
	}
	return truncateWords(value, 400)
}

func FirstParagraph(raw string) string {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return ""
	}
	var find func(*html.Node) string
	find = func(node *html.Node) string {
		if node.Type == html.ElementNode && node.Data == "p" {
			value := strings.TrimSpace(nodeText(node))
			if value != "" {
				return value
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if value := find(child); value != "" {
				return value
			}
		}
		return ""
	}
	return find(doc)
}

func PlainText(raw string) string {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return strings.TrimSpace(raw)
	}
	return strings.Join(strings.Fields(nodeText(doc)), " ")
}

func Sanitize(raw string, pageURL *url.URL) (string, error) {
	doc, err := html.ParseFragment(strings.NewReader(raw), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return "", fmt.Errorf("parse extracted HTML: %w", err)
	}
	for _, node := range doc {
		rewriteURLs(node, pageURL)
	}
	var rendered strings.Builder
	for _, node := range doc {
		if err := html.Render(&rendered, node); err != nil {
			return "", err
		}
	}
	policy := bluemonday.NewPolicy()
	policy.AllowElements("p", "h1", "h2", "h3", "h4", "ul", "ol", "li", "blockquote", "pre", "code", "figure", "figcaption", "strong", "em", "table", "thead", "tbody", "tfoot", "tr", "th", "td", "br", "hr")
	policy.AllowAttrs("href", "title").OnElements("a")
	policy.AllowAttrs("src", "alt", "title", "width", "height", "loading").OnElements("img")
	policy.AllowAttrs("colspan", "rowspan", "scope").OnElements("th", "td")
	policy.AllowURLSchemes("http", "https", "mailto")
	policy.RequireNoFollowOnLinks(true)
	policy.RequireNoReferrerOnLinks(true)
	policy.AddTargetBlankToFullyQualifiedLinks(true)
	return strings.TrimSpace(policy.Sanitize(rendered.String())), nil
}

// RemoveLeadingImage removes the first content node only when that node
// consists solely of the image selected for lead media. This keeps inline
// article images intact and preserves the body if lead-media processing fails.
func RemoveLeadingImage(raw, sourceURL string) (string, bool) {
	sourceURL = strings.TrimSpace(sourceURL)
	if strings.TrimSpace(raw) == "" || sourceURL == "" {
		return raw, false
	}
	nodes, err := html.ParseFragment(strings.NewReader(raw), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return raw, false
	}
	for index, node := range nodes {
		if ignorableNode(node) {
			continue
		}
		if !containsOnlyImage(node, sourceURL) {
			return raw, false
		}
		nodes = append(nodes[:index], nodes[index+1:]...)
		var rendered strings.Builder
		for _, remaining := range nodes {
			if err := html.Render(&rendered, remaining); err != nil {
				return raw, false
			}
		}
		return strings.TrimSpace(rendered.String()), true
	}
	return raw, false
}

func containsOnlyImage(node *html.Node, sourceURL string) bool {
	if node.Type == html.ElementNode && node.Data == "img" {
		for _, attribute := range node.Attr {
			if attribute.Key == "src" {
				return strings.TrimSpace(attribute.Val) == sourceURL
			}
		}
		return false
	}
	if node.Type != html.ElementNode {
		return false
	}
	found := false
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if ignorableNode(child) {
			continue
		}
		if found || !containsOnlyImage(child, sourceURL) {
			return false
		}
		found = true
	}
	return found
}

func ignorableNode(node *html.Node) bool {
	return node.Type == html.CommentNode || (node.Type == html.TextNode && strings.TrimSpace(node.Data) == "")
}

func rewriteURLs(node *html.Node, base *url.URL) {
	if node.Type == html.ElementNode {
		if node.Data == "img" && trackingPixel(node) {
			node.Data = "span"
			node.Attr = nil
		}
		for i := range node.Attr {
			if (node.Data == "a" && node.Attr[i].Key == "href") || (node.Data == "img" && node.Attr[i].Key == "src") {
				node.Attr[i].Val = resolveURL(base, node.Attr[i].Val)
			}
		}
		if node.Data == "img" {
			node.Attr = append(node.Attr, html.Attribute{Key: "loading", Val: "lazy"})
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		rewriteURLs(child, base)
	}
}

func trackingPixel(node *html.Node) bool {
	width, height := "", ""
	for _, attr := range node.Attr {
		switch attr.Key {
		case "width":
			width = strings.TrimSpace(attr.Val)
		case "height":
			height = strings.TrimSpace(attr.Val)
		}
	}
	return (width == "1" || width == "0") && (height == "1" || height == "0")
}

func nodeText(node *html.Node) string {
	var output strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			output.WriteString(current.Data)
			output.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return output.String()
}

func resolveURL(base *url.URL, raw string) string {
	reference, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || base == nil {
		return strings.TrimSpace(raw)
	}
	return base.ResolveReference(reference).String()
}

func truncateWords(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	cut := maxRunes
	for cut > 0 && !unicode.IsSpace(runes[cut-1]) {
		cut--
	}
	if cut < maxRunes/2 {
		cut = maxRunes
	}
	return strings.TrimSpace(string(runes[:cut])) + "…"
}
