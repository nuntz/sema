package media

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"golang.org/x/net/html"
)

type Image struct {
	Bytes       []byte
	ContentType string
	Extension   string
	SourceURL   string
	Width       int
	Height      int
}

type Processor struct{ client *httpx.Client }

func New(client *httpx.Client) *Processor { return &Processor{client: client} }

func Candidates(enclosures []domain.Enclosure, pageHTML, articleHTML []byte, articleImage string, pageURL *url.URL) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, 4)
	add := func(raw string) {
		value := resolve(pageURL, raw)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	for _, enclosure := range enclosures {
		if strings.HasPrefix(strings.ToLower(enclosure.Type), "image/") || imageExtension(enclosure.URL) {
			add(enclosure.URL)
		}
	}
	if len(pageHTML) > 0 {
		doc, _ := html.Parse(bytes.NewReader(pageHTML))
		var walk func(*html.Node)
		walk = func(node *html.Node) {
			if node.Type == html.ElementNode && node.Data == "meta" {
				property, content := attr(node, "property"), attr(node, "content")
				if property == "" {
					property = attr(node, "name")
				}
				if property == "og:image" || property == "twitter:image" || property == "twitter:image:src" {
					add(content)
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(doc)
	}
	add(articleImage)
	if len(articleHTML) > 0 {
		doc, _ := html.Parse(bytes.NewReader(articleHTML))
		var firstImage string
		var walk func(*html.Node)
		walk = func(node *html.Node) {
			if firstImage == "" && node.Type == html.ElementNode && node.Data == "img" {
				firstImage = attr(node, "src")
			}
			for child := node.FirstChild; child != nil && firstImage == ""; child = child.NextSibling {
				walk(child)
			}
		}
		walk(doc)
		add(firstImage)
	}
	return result
}

func (p *Processor) FetchLead(ctx context.Context, candidates []string) (Image, error) {
	var lastErr error
	for _, candidate := range candidates {
		response, err := p.client.Get(ctx, candidate, nil)
		if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
			lastErr = err
			continue
		}
		decoded, err := decode(response.Body, 40_000_000)
		if err != nil {
			lastErr = err
			continue
		}
		bounds := decoded.Bounds()
		if bounds.Dx() < 300 {
			continue
		}
		lead, err := encodeLead(decoded)
		if err != nil {
			lastErr = err
			continue
		}
		lead.SourceURL = candidate
		return lead, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no usable lead image")
	}
	return Image{}, lastErr
}

func (p *Processor) Favicon(ctx context.Context, siteURL string) (Image, error) {
	base, err := url.Parse(siteURL)
	if err != nil {
		return Image{}, err
	}
	root := &url.URL{Scheme: base.Scheme, Host: base.Host, Path: "/"}
	candidates := make([]string, 0, 4)
	if response, fetchErr := p.client.Get(ctx, root.String(), nil); fetchErr == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		doc, _ := html.Parse(bytes.NewReader(response.Body))
		var walk func(*html.Node)
		walk = func(node *html.Node) {
			if node.Type == html.ElementNode && node.Data == "link" && strings.Contains(strings.ToLower(attr(node, "rel")), "icon") {
				candidates = append(candidates, resolve(root, attr(node, "href")))
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(doc)
	}
	candidates = append(candidates, root.ResolveReference(&url.URL{Path: "/favicon.ico"}).String())
	for _, candidate := range candidates {
		response, fetchErr := p.client.Get(ctx, candidate, http.Header{"Accept": []string{"image/*"}})
		if fetchErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
			continue
		}
		decoded, decodeErr := decode(response.Body, 16_000_000)
		if decodeErr != nil {
			continue
		}
		target := image.NewRGBA(image.Rect(0, 0, 32, 32))
		draw.CatmullRom.Scale(target, target.Bounds(), decoded, decoded.Bounds(), draw.Over, nil)
		var output bytes.Buffer
		if err := png.Encode(&output, target); err != nil {
			return Image{}, err
		}
		return Image{Bytes: output.Bytes(), ContentType: "image/png", Extension: ".png", Width: 32, Height: 32}, nil
	}
	return Image{}, fmt.Errorf("favicon not found")
}

func decode(body []byte, maxPixels int64) (image.Image, error) {
	configuration, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if configuration.Width <= 0 || configuration.Height <= 0 || int64(configuration.Width)*int64(configuration.Height) > maxPixels {
		return nil, fmt.Errorf("image dimensions %dx%d exceed the decode limit", configuration.Width, configuration.Height)
	}
	decoded, _, err := image.Decode(bytes.NewReader(body))
	return decoded, err
}

func encodeLead(source image.Image) (Image, error) {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width > 1280 {
		height = max(1, height*1280/width)
		width = 1280
		target := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.CatmullRom.Scale(target, target.Bounds(), source, bounds, draw.Over, nil)
		source = target
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, source, &jpeg.Options{Quality: 85}); err != nil {
		return Image{}, err
	}
	return Image{Bytes: output.Bytes(), ContentType: "image/jpeg", Extension: ".jpg", Width: width, Height: height}, nil
}

func attr(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, key) {
			return strings.TrimSpace(attribute.Val)
		}
	}
	return ""
}

func resolve(base *url.URL, raw string) string {
	ref, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || raw == "" {
		return ""
	}
	if base == nil {
		return ref.String()
	}
	return base.ResolveReference(ref).String()
}

func imageExtension(raw string) bool {
	u, _ := url.Parse(raw)
	ext := strings.ToLower(path.Ext(u.Path))
	return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp"
}
