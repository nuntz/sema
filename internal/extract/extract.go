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
	"golang.org/x/net/publicsuffix"
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

const LinkblogCommentaryMinRunes = 200

func Substantial(raw string) bool { return utf8.RuneCountInString(PlainText(raw)) > 1500 }

func IsLinkblogEntry(itemURL, siteURL, rawContent string) bool {
	if utf8.RuneCountInString(PlainText(rawContent)) <= LinkblogCommentaryMinRunes {
		return false
	}
	item, itemErr := url.Parse(strings.TrimSpace(itemURL))
	site, siteErr := url.Parse(strings.TrimSpace(siteURL))
	if itemErr != nil || siteErr != nil || item.Hostname() == "" || site.Hostname() == "" {
		return false
	}
	itemHost := strings.ToLower(strings.TrimSuffix(item.Hostname(), "."))
	siteHost := strings.ToLower(strings.TrimSuffix(site.Hostname(), "."))
	itemDomain, itemErr := publicsuffix.EffectiveTLDPlusOne(itemHost)
	siteDomain, siteErr := publicsuffix.EffectiveTLDPlusOne(siteHost)
	if itemErr == nil && siteErr == nil {
		return !strings.EqualFold(itemDomain, siteDomain)
	}
	return itemHost != siteHost
}

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

// RemoveLeadImage removes the image selected for lead media from the body.
func RemoveLeadImage(raw, sourceURL string) (string, bool) {
	sourceURL = strings.TrimSpace(sourceURL)
	if strings.TrimSpace(raw) == "" || sourceURL == "" {
		return raw, false
	}
	nodes, err := html.ParseFragment(strings.NewReader(raw), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return raw, false
	}
	root := &html.Node{Type: html.DocumentNode}
	for _, node := range nodes {
		root.AppendChild(node)
	}
	image := matchingImage(root, sourceURL)
	if image == nil || hasCaptionedFigure(image, root) {
		return raw, false
	}
	parent := image.Parent
	parent.RemoveChild(image)
	for node := parent; node != root && emptyNode(node); {
		parent = node.Parent
		parent.RemoveChild(node)
		node = parent
	}
	var rendered strings.Builder
	for node := root.FirstChild; node != nil; node = node.NextSibling {
		if err := html.Render(&rendered, node); err != nil {
			return raw, false
		}
	}
	return strings.TrimSpace(rendered.String()), true
}

func matchingImage(node *html.Node, sourceURL string) *html.Node {
	if node.Type == html.ElementNode && node.Data == "img" {
		for _, attr := range node.Attr {
			if attr.Key == "src" && sameURL(attr.Val, sourceURL) {
				return node
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if image := matchingImage(child, sourceURL); image != nil {
			return image
		}
	}
	return nil
}

func sameURL(left, right string) bool {
	left, right = strings.TrimSpace(left), strings.TrimSpace(right)
	if left == right {
		return true
	}
	leftURL, leftErr := url.Parse(left)
	rightURL, rightErr := url.Parse(right)
	return leftErr == nil && rightErr == nil && leftURL.String() == rightURL.String()
}

func hasCaptionedFigure(node, root *html.Node) bool {
	for ancestor := node.Parent; ancestor != root; ancestor = ancestor.Parent {
		if ancestor.Type == html.ElementNode && ancestor.Data == "figure" && containsElement(ancestor, "figcaption") {
			return true
		}
	}
	return false
}

func containsElement(node *html.Node, name string) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == name || containsElement(child, name) {
			return true
		}
	}
	return false
}

func emptyNode(node *html.Node) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.CommentNode && (child.Type != html.TextNode || strings.TrimSpace(child.Data) != "") {
			return false
		}
	}
	return true
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
