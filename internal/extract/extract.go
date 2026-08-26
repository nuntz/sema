package extract

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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
	Author         string
	DisplayDate    string
	HTML           string
	Text           string
	FirstParagraph string
	LeadImage      string
	Quality        float64
}

func Article(document []byte, pageURL *url.URL) (Result, error) {
	normalized, pageWords, err := normalizeDocument(document, pageURL)
	if err != nil {
		return Result{}, err
	}
	article, err := readability.FromReader(bytes.NewReader(normalized), pageURL)
	if err != nil {
		return Result{}, fmt.Errorf("readability: %w", err)
	}
	pruned, err := pruneBoilerplate(article.Content)
	if err != nil {
		return Result{}, err
	}
	cleaned, err := Sanitize(pruned, pageURL)
	if err != nil {
		return Result{}, err
	}
	text := PlainText(cleaned)
	if strings.TrimSpace(text) == "" {
		return Result{}, fmt.Errorf("readability returned empty content")
	}
	result := Result{
		Title:          strings.TrimSpace(article.Title),
		Author:         strings.TrimSpace(article.Byline),
		HTML:           cleaned,
		Text:           text,
		FirstParagraph: FirstParagraph(cleaned),
		LeadImage:      resolveURL(pageURL, article.Image),
		Quality:        Quality(cleaned, pageWords),
	}
	if article.PublishedTime != nil {
		result.DisplayDate = article.PublishedTime.UTC().Format(time.RFC3339)
	}
	return result, nil
}

func FeedContent(raw string, pageURL *url.URL) (Result, error) {
	normalized, pageWords, err := normalizeDocument([]byte(raw), pageURL)
	if err != nil {
		return Result{}, err
	}
	pruned, err := pruneBoilerplate(string(normalized))
	if err != nil {
		return Result{}, err
	}
	cleaned, err := Sanitize(pruned, pageURL)
	if err != nil {
		return Result{}, err
	}
	text := PlainText(cleaned)
	if strings.TrimSpace(text) == "" {
		return Result{}, fmt.Errorf("feed content is empty")
	}
	return Result{HTML: cleaned, Text: text, FirstParagraph: FirstParagraph(cleaned), Quality: Quality(cleaned, pageWords)}, nil
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
		replaceEmbeds(node, pageURL)
		rewriteURLs(node, pageURL)
	}
	var rendered strings.Builder
	for _, node := range doc {
		if err := html.Render(&rendered, node); err != nil {
			return "", err
		}
	}
	policy := bluemonday.NewPolicy()
	policy.AllowElements("p", "h1", "h2", "h3", "h4", "ul", "ol", "li", "blockquote", "pre", "code", "figure", "figcaption", "strong", "b", "i", "em", "small", "span", "table", "thead", "tbody", "tfoot", "tr", "th", "td", "br", "hr")
	policy.AllowAttrs("href", "title").OnElements("a")
	policy.AllowAttrs("src", "srcset", "alt", "title", "width", "height", "loading").OnElements("img")
	policy.AllowAttrs("colspan", "rowspan", "scope").OnElements("th", "td")
	policy.AllowAttrs("class").Matching(regexp.MustCompile(`^(?:media-card|media-card-thumbnail|media-provider|media-card-info|media-card-external|media-card-open|video-card-play|video-provider-strip)$`)).OnElements("a", "span")
	policy.AllowAttrs("class").Matching(regexp.MustCompile(`^(?:language-[A-Za-z0-9_+.-]+|lang-[A-Za-z0-9_+.-]+)$`)).OnElements("pre", "code")
	policy.AllowAttrs("data-provider", "data-thumbnail-url").OnElements("a")
	policy.AllowAttrs("aria-hidden").OnElements("span")
	policy.AllowURLSchemes("http", "https", "mailto")
	policy.RequireNoFollowOnLinks(true)
	policy.RequireNoReferrerOnLinks(true)
	policy.AddTargetBlankToFullyQualifiedLinks(true)
	return strings.TrimSpace(policy.Sanitize(rendered.String())), nil
}

// MediaCard describes a sanitized third-party embed that the item worker can
// finish without loading the provider in the reader.
type MediaCard struct {
	Index        int
	Provider     string
	URL          string
	ThumbnailURL string
}

// ResolveMediaCards rewrites remote embed thumbnails to caller-provided URLs.
// A resolver error deliberately produces the compact, no-thumbnail card.
func ResolveMediaCards(raw string, resolver func(MediaCard) (string, error)) (string, []error) {
	nodes, err := html.ParseFragment(strings.NewReader(raw), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return raw, []error{err}
	}
	root := &html.Node{Type: html.DocumentNode}
	for _, node := range nodes {
		root.AppendChild(node)
	}
	var failures []error
	index := 0
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" && hasClass(node, "media-card") {
			card := MediaCard{Index: index, Provider: attr(node, "data-provider"), URL: attr(node, "href"), ThumbnailURL: attr(node, "data-thumbnail-url")}
			index++
			thumbnail := descendantWithClass(node, "media-card-thumbnail")
			removeAttr(node, "data-thumbnail-url")
			if thumbnail != nil {
				removeChildrenByElement(thumbnail, "img")
			}
			if resolver != nil {
				cached, resolveErr := resolver(card)
				if resolveErr != nil {
					failures = append(failures, resolveErr)
				} else if cached != "" && thumbnail != nil {
					thumbnail.AppendChild(&html.Node{Type: html.ElementNode, Data: "img", DataAtom: atom.Img, Attr: []html.Attribute{
						{Key: "src", Val: cached}, {Key: "alt", Val: ""}, {Key: "loading", Val: "lazy"},
					}})
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	var rendered strings.Builder
	for node := root.FirstChild; node != nil; node = node.NextSibling {
		if renderErr := html.Render(&rendered, node); renderErr != nil {
			return raw, append(failures, renderErr)
		}
	}
	return strings.TrimSpace(rendered.String()), failures
}

var boilerplatePatterns = []string{"related", "newsletter", "subscribe", "share", "comments", "cookie", "promo"}

func normalizeDocument(document []byte, pageURL *url.URL) ([]byte, int, error) {
	doc, err := html.Parse(bytes.NewReader(document))
	if err != nil {
		return nil, 0, fmt.Errorf("parse document: %w", err)
	}
	normalizeImages(doc, pageURL)
	pageWords := visibleWordCount(doc)
	var rendered bytes.Buffer
	if err := html.Render(&rendered, doc); err != nil {
		return nil, 0, fmt.Errorf("render normalized document: %w", err)
	}
	return rendered.Bytes(), pageWords, nil
}

func normalizeImages(node *html.Node, base *url.URL) {
	if node.Type == html.ElementNode && node.Data == "img" {
		if lazy := firstAttr(node, "data-src", "data-lazy-src"); lazy != "" {
			setAttr(node, "src", resolveURL(base, lazy))
		}
		if lazySet := firstAttr(node, "data-srcset"); lazySet != "" {
			setAttr(node, "srcset", lazySet)
		}
		if rawSet := attr(node, "srcset"); rawSet != "" {
			if selected, candidates := selectSrcset(rawSet, base); selected != "" {
				setAttr(node, "src", selected)
				setAttr(node, "srcset", strings.Join(candidates, ", "))
			}
		}
		if source := attr(node, "src"); source != "" {
			setAttr(node, "src", resolveURL(base, source))
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		normalizeImages(child, base)
	}
}

type srcsetCandidate struct {
	url   string
	width int
}

func selectSrcset(raw string, base *url.URL) (string, []string) {
	parsed := make([]srcsetCandidate, 0)
	for _, part := range strings.Split(raw, ",") {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}
		width := 0
		if len(fields) > 1 && strings.HasSuffix(fields[len(fields)-1], "w") {
			width, _ = strconv.Atoi(strings.TrimSuffix(fields[len(fields)-1], "w"))
		}
		if width <= 0 || width > 1600 {
			continue
		}
		parsed = append(parsed, srcsetCandidate{url: resolveURL(base, fields[0]), width: width})
	}
	if len(parsed) == 0 {
		return "", nil
	}
	sort.Slice(parsed, func(i, j int) bool { return parsed[i].width < parsed[j].width })
	result := make([]string, 0, len(parsed))
	for _, candidate := range parsed {
		result = append(result, fmt.Sprintf("%s %dw", candidate.url, candidate.width))
	}
	return parsed[len(parsed)-1].url, result
}

func pruneBoilerplate(raw string) (string, error) {
	nodes, err := html.ParseFragment(strings.NewReader(raw), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return "", fmt.Errorf("parse extracted HTML for pruning: %w", err)
	}
	root := &html.Node{Type: html.DocumentNode}
	for _, node := range nodes {
		root.AppendChild(node)
	}
	var prune func(*html.Node)
	prune = func(parent *html.Node) {
		for child := parent.FirstChild; child != nil; {
			next := child.NextSibling
			if boilerplateNode(child) {
				parent.RemoveChild(child)
			} else {
				prune(child)
			}
			child = next
		}
	}
	prune(root)
	var rendered strings.Builder
	for node := root.FirstChild; node != nil; node = node.NextSibling {
		if err := html.Render(&rendered, node); err != nil {
			return "", err
		}
	}
	return rendered.String(), nil
}

func boilerplateNode(node *html.Node) bool {
	if node.Type != html.ElementNode {
		return false
	}
	identity := strings.ToLower(attr(node, "class") + " " + attr(node, "id"))
	for _, pattern := range boilerplatePatterns {
		if strings.Contains(identity, pattern) {
			return true
		}
	}
	return false
}

func replaceEmbeds(node *html.Node, pageURL *url.URL) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		replaceEmbeds(child, pageURL)
	}
	if node.Type != html.ElementNode || (node.Data != "iframe" && node.Data != "embed") {
		return
	}
	source := resolveURL(pageURL, attr(node, "src"))
	provider, external, thumbnail, ok := supportedEmbed(source)
	if !ok {
		return
	}
	title := strings.TrimSpace(attr(node, "title"))
	if title == "" {
		title = provider + " video"
	}
	replacement := &html.Node{Type: html.ElementNode, Data: "a", DataAtom: atom.A, Attr: []html.Attribute{
		{Key: "class", Val: "media-card"}, {Key: "data-provider", Val: provider}, {Key: "href", Val: external}, {Key: "title", Val: title},
	}}
	if thumbnail != "" {
		replacement.Attr = append(replacement.Attr, html.Attribute{Key: "data-thumbnail-url", Val: thumbnail})
	}
	frame := &html.Node{Type: html.ElementNode, Data: "span", DataAtom: atom.Span, Attr: []html.Attribute{{Key: "class", Val: "media-card-thumbnail"}}}
	if thumbnail != "" {
		frame.AppendChild(&html.Node{Type: html.ElementNode, Data: "img", DataAtom: atom.Img, Attr: []html.Attribute{{Key: "src", Val: thumbnail}, {Key: "alt", Val: ""}}})
	}
	play := &html.Node{Type: html.ElementNode, Data: "span", DataAtom: atom.Span, Attr: []html.Attribute{{Key: "class", Val: "video-card-play"}, {Key: "aria-hidden", Val: "true"}}}
	frame.AppendChild(play)
	replacement.AppendChild(frame)
	strip := &html.Node{Type: html.ElementNode, Data: "span", DataAtom: atom.Span, Attr: []html.Attribute{{Key: "class", Val: "video-provider-strip"}}}
	providerLabel := &html.Node{Type: html.ElementNode, Data: "b", DataAtom: atom.B}
	providerLabel.AppendChild(&html.Node{Type: html.TextNode, Data: strings.ToUpper(provider)})
	strip.AppendChild(providerLabel)
	strip.AppendChild(&html.Node{Type: html.ElementNode, Data: "i", DataAtom: atom.I})
	displayURL := &html.Node{Type: html.ElementNode, Data: "span", DataAtom: atom.Span}
	displayURL.AppendChild(&html.Node{Type: html.TextNode, Data: embedDisplayURL(external)})
	strip.AppendChild(displayURL)
	watch := &html.Node{Type: html.ElementNode, Data: "strong", DataAtom: atom.Strong}
	watch.AppendChild(&html.Node{Type: html.TextNode, Data: "Watch"})
	watch.AppendChild(&html.Node{Type: html.ElementNode, Data: "span", DataAtom: atom.Span, Attr: []html.Attribute{{Key: "class", Val: "media-card-open"}, {Key: "aria-hidden", Val: "true"}}})
	strip.AppendChild(watch)
	replacement.AppendChild(strip)
	replaceNode(node, replacement)
}

func embedDisplayURL(raw string) string {
	display := strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	return strings.TrimSuffix(display, "/")
}

func supportedEmbed(raw string) (provider, external, thumbnail string, ok bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return "", "", "", false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	switch {
	case host == "youtube.com" || host == "youtube-nocookie.com" || strings.HasSuffix(host, ".youtube.com") || strings.HasSuffix(host, ".youtube-nocookie.com"):
		id := ""
		if len(parts) >= 2 && (parts[0] == "embed" || parts[0] == "shorts") {
			id = parts[1]
		}
		if id == "" {
			id = parsed.Query().Get("v")
		}
		if id == "" {
			return "", "", "", false
		}
		return "YouTube", "https://www.youtube.com/watch?v=" + url.QueryEscape(id), "https://i.ytimg.com/vi/" + url.PathEscape(id) + "/hqdefault.jpg", true
	case host == "youtu.be":
		if len(parts) == 0 || parts[0] == "" {
			return "", "", "", false
		}
		id := parts[0]
		return "YouTube", "https://www.youtube.com/watch?v=" + url.QueryEscape(id), "https://i.ytimg.com/vi/" + url.PathEscape(id) + "/hqdefault.jpg", true
	case host == "vimeo.com" || strings.HasSuffix(host, ".vimeo.com"):
		id := ""
		for i := len(parts) - 1; i >= 0; i-- {
			if _, err := strconv.ParseInt(parts[i], 10, 64); err == nil {
				id = parts[i]
				break
			}
		}
		if id == "" {
			return "", "", "", false
		}
		return "Vimeo", "https://vimeo.com/" + id, "", true
	default:
		return "", "", "", false
	}
}

func replaceNode(target, source *html.Node) {
	parent := target.Parent
	previous, next := target.PrevSibling, target.NextSibling
	*target = *source
	target.Parent, target.PrevSibling, target.NextSibling = parent, previous, next
	for child := target.FirstChild; child != nil; child = child.NextSibling {
		child.Parent = target
	}
}

func visibleWordCount(node *html.Node) int {
	if node.Type == html.ElementNode {
		switch node.Data {
		case "script", "style", "noscript", "svg", "template":
			return 0
		}
	}
	if node.Type == html.TextNode {
		return len(strings.Fields(node.Data))
	}
	count := 0
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		count += visibleWordCount(child)
	}
	return count
}

func firstAttr(node *html.Node, keys ...string) string {
	for _, key := range keys {
		if value := attr(node, key); value != "" {
			return value
		}
	}
	return ""
}

func attr(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, key) {
			return strings.TrimSpace(attribute.Val)
		}
	}
	return ""
}

func setAttr(node *html.Node, key, value string) {
	for i := range node.Attr {
		if strings.EqualFold(node.Attr[i].Key, key) {
			node.Attr[i].Key, node.Attr[i].Val = key, value
			return
		}
	}
	node.Attr = append(node.Attr, html.Attribute{Key: key, Val: value})
}

func removeAttr(node *html.Node, key string) {
	kept := node.Attr[:0]
	for _, attribute := range node.Attr {
		if !strings.EqualFold(attribute.Key, key) {
			kept = append(kept, attribute)
		}
	}
	node.Attr = kept
}

func hasClass(node *html.Node, class string) bool {
	for _, value := range strings.Fields(attr(node, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func descendantWithClass(node *html.Node, class string) *html.Node {
	if node.Type == html.ElementNode && hasClass(node, class) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := descendantWithClass(child, class); found != nil {
			return found
		}
	}
	return nil
}

func removeChildrenByElement(node *html.Node, element string) {
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		if child.Type == html.ElementNode && child.Data == element {
			node.RemoveChild(child)
		} else {
			removeChildrenByElement(child, element)
		}
		child = next
	}
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func clamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

// Quality returns the extraction quality composite in the range [0,1].
func Quality(extractedHTML string, pageWordCount int) float64 {
	nodes, err := html.ParseFragment(strings.NewReader(extractedHTML), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return 0
	}
	root := &html.Node{Type: html.DocumentNode}
	for _, node := range nodes {
		root.AppendChild(node)
	}
	extractedWords, linkWords, paragraphs := 0, 0, 0
	var walk func(*html.Node, bool)
	walk = func(node *html.Node, inLink bool) {
		if node.Type == html.ElementNode {
			inLink = inLink || node.Data == "a"
			if node.Data == "p" {
				paragraphs++
			}
		}
		if node.Type == html.TextNode {
			count := len(strings.Fields(node.Data))
			extractedWords += count
			if inLink {
				linkWords += count
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, inLink)
		}
	}
	walk(root, false)
	if extractedWords == 0 {
		return 0
	}
	if pageWordCount <= 0 {
		pageWordCount = extractedWords
	}
	coverage := minFloat(1, float64(extractedWords)/float64(pageWordCount))
	linkScore := 1 - minFloat(1, float64(linkWords)/float64(extractedWords))
	paragraphScore := minFloat(1, float64(paragraphs)/5)
	return clamp(0.5*coverage + 0.3*linkScore + 0.2*paragraphScore)
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
		if node.Data == "pre" || node.Data == "code" {
			languageClass := ""
			for _, class := range strings.Fields(attr(node, "class")) {
				if strings.HasPrefix(class, "language-") || strings.HasPrefix(class, "lang-") {
					languageClass = class
					break
				}
			}
			if languageClass == "" {
				removeAttr(node, "class")
			} else {
				setAttr(node, "class", languageClass)
			}
		}
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
			setAttr(node, "loading", "lazy")
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
		if current.Type == html.ElementNode {
			switch current.Data {
			case "script", "style", "noscript", "svg", "template":
				return
			}
		}
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
