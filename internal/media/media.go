package media

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime"
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

// Keep this list limited to formats registered with the image package above.
const imageAccept = "image/webp,image/jpeg,image/png"

type Image struct {
	Bytes       []byte
	ContentType string
	Extension   string
	SourceURL   string
	Width       int
	Height      int
}

// Lead contains responsive encodings in ascending size order and embeds the
// largest encoding for compatibility with callers that only need one image.
type Lead struct {
	Image
	Variants []Image
}

type LeadError struct {
	URL         string
	ContentType string
	Err         error
}

func (e *LeadError) Error() string {
	if e.ContentType != "" {
		return fmt.Sprintf("lead image %q with content type %q: %v", e.URL, e.ContentType, e.Err)
	}
	return fmt.Sprintf("lead image %q: %v", e.URL, e.Err)
}

func (e *LeadError) Unwrap() error { return e.Err }

type client interface {
	Get(context.Context, string, http.Header) (httpx.Response, error)
}

type Processor struct{ client client }

func New(client *httpx.Client) *Processor { return &Processor{client: client} }

func Candidates(enclosures []domain.Enclosure, pageHTML, articleHTML, feedHTML []byte, articleImage string, pageURL, feedURL *url.URL) []string {
	return candidates(enclosures, pageHTML, articleHTML, feedHTML, articleImage, pageURL, feedURL, html.Parse)
}

func candidates(enclosures []domain.Enclosure, pageHTML, articleHTML, feedHTML []byte, articleImage string, pageURL, feedURL *url.URL, parse func(io.Reader) (*html.Node, error)) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, 4)
	add := func(base *url.URL, raw string) {
		value := resolve(base, raw)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	for _, enclosure := range enclosures {
		if strings.HasPrefix(strings.ToLower(enclosure.Type), "image/") || imageExtension(enclosure.URL) {
			add(pageURL, enclosure.URL)
		}
	}
	if len(pageHTML) > 0 {
		doc, err := parse(bytes.NewReader(pageHTML))
		if err != nil {
			slog.Warn("parse page HTML failed", "page_url", pageURL, "error", err)
		}
		if doc != nil {
			var walk func(*html.Node)
			walk = func(node *html.Node) {
				if node.Type == html.ElementNode && node.Data == "meta" {
					property, content := attr(node, "property"), attr(node, "content")
					if property == "" {
						property = attr(node, "name")
					}
					if property == "og:image" || property == "twitter:image" || property == "twitter:image:src" {
						add(pageURL, content)
					}
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					walk(child)
				}
			}
			walk(doc)
		}
	}
	add(pageURL, articleImage)
	add(pageURL, firstImage(articleHTML))
	add(feedURL, firstImage(feedHTML))
	return result
}

func firstImage(document []byte) string {
	if len(document) == 0 {
		return ""
	}
	doc, _ := html.Parse(bytes.NewReader(document))
	var result string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if result == "" && node.Type == html.ElementNode && node.Data == "img" {
			result = attr(node, "src")
		}
		for child := node.FirstChild; child != nil && result == ""; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return result
}

func (p *Processor) FetchLead(ctx context.Context, candidates []string) (Lead, error) {
	return p.fetchLead(ctx, candidates, false)
}

// FetchVideoLead normalizes provider thumbnails to 16:9 before the grid's
// justified crop. In particular, this removes the letterbox bands from
// YouTube's 4:3 hqdefault fallback.
func (p *Processor) FetchVideoLead(ctx context.Context, candidates []string) (Lead, error) {
	return p.fetchLead(ctx, candidates, true)
}

func (p *Processor) fetchLead(ctx context.Context, candidates []string, cropVideo bool) (Lead, error) {
	var lastErr error
	for _, candidate := range candidates {
		response, err := p.client.Get(ctx, candidate, http.Header{"Accept": []string{imageAccept}})
		if err != nil {
			lastErr = &LeadError{URL: candidate, Err: err}
			continue
		}
		contentType := response.Header.Get("Content-Type")
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			lastErr = &LeadError{URL: candidate, ContentType: contentType, Err: fmt.Errorf("HTTP status %d", response.StatusCode)}
			continue
		}
		mediaType, _, parseErr := mime.ParseMediaType(contentType)
		if parseErr != nil || !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
			lastErr = &LeadError{URL: candidate, ContentType: contentType, Err: fmt.Errorf("unexpected content type")}
			continue
		}
		decoded, err := decode(response.Body, 40_000_000)
		if err != nil {
			lastErr = &LeadError{URL: candidate, ContentType: contentType, Err: fmt.Errorf("decode: %w", err)}
			continue
		}
		if cropVideo {
			decoded = cropAspect(decoded, 16, 9)
		}
		bounds := decoded.Bounds()
		if bounds.Dx() < 300 {
			lastErr = &LeadError{URL: candidate, ContentType: contentType, Err: fmt.Errorf("image width %d is below 300 pixels", bounds.Dx())}
			continue
		}
		lead, err := EncodeLead(decoded)
		if err != nil {
			lastErr = &LeadError{URL: candidate, ContentType: contentType, Err: fmt.Errorf("encode: %w", err)}
			continue
		}
		lead.SourceURL = candidate
		return lead, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no usable lead image")
	}
	return Lead{}, lastErr
}

// FetchEmbed applies the lead-image safety, decode, resize, and byte limits to
// a provider thumbnail. The caller stores it under the embed-specific key.
func (p *Processor) FetchEmbed(ctx context.Context, sourceURL string) (Image, error) {
	lead, err := p.FetchLead(ctx, []string{sourceURL})
	return lead.Image, err
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
		response, fetchErr := p.client.Get(ctx, candidate, http.Header{"Accept": []string{imageAccept}})
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

// Avatar caches a centre-cropped 96px channel image in the same public asset
// namespace as favicons while preserving its distinct circular presentation.
func (p *Processor) Avatar(ctx context.Context, sourceURL string) (Image, error) {
	response, err := p.client.Get(ctx, sourceURL, http.Header{"Accept": []string{imageAccept}})
	if err != nil {
		return Image{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Image{}, fmt.Errorf("avatar returned HTTP %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return Image{}, fmt.Errorf("avatar has unexpected content type %q", response.Header.Get("Content-Type"))
	}
	decoded, err := decode(response.Body, 16_000_000)
	if err != nil {
		return Image{}, err
	}
	square := cropAspect(decoded, 1, 1)
	target := image.NewRGBA(image.Rect(0, 0, 96, 96))
	draw.CatmullRom.Scale(target, target.Bounds(), square, square.Bounds(), draw.Over, nil)
	var output bytes.Buffer
	if err := png.Encode(&output, target); err != nil {
		return Image{}, err
	}
	return Image{Bytes: output.Bytes(), ContentType: "image/png", Extension: ".png", SourceURL: sourceURL, Width: 96, Height: 96}, nil
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

// EncodeLead creates JPEG encodings that fit within 384, 768, and 1280 pixel
// boxes. The first box that contains the source contributes the source-sized
// encoding and terminates the sequence, so images are never upscaled.
func EncodeLead(source image.Image) (Lead, error) {
	bounds := source.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return Lead{}, fmt.Errorf("empty source image")
	}
	variants := make([]Image, 0, 3)
	for _, box := range []int{384, 768, 1280} {
		width, height := fit(bounds.Dx(), bounds.Dy(), box)
		candidate := source
		if width != bounds.Dx() || height != bounds.Dy() {
			target := image.NewRGBA(image.Rect(0, 0, width, height))
			draw.CatmullRom.Scale(target, target.Bounds(), source, bounds, draw.Over, nil)
			candidate = target
		}
		quality := 85
		if box == 384 {
			quality = 80
		}
		var output bytes.Buffer
		if err := jpeg.Encode(&output, candidate, &jpeg.Options{Quality: quality}); err != nil {
			return Lead{}, err
		}
		variants = append(variants, Image{Bytes: output.Bytes(), ContentType: "image/jpeg", Extension: ".jpg", Width: width, Height: height})
		if width == bounds.Dx() && height == bounds.Dy() {
			break
		}
	}
	largest := variants[len(variants)-1]
	return Lead{Image: largest, Variants: variants}, nil
}

// EncodeLeadBytes is the backfill entry point for an already-stored lead.
func EncodeLeadBytes(body []byte) (Lead, error) {
	decoded, err := decode(body, 40_000_000)
	if err != nil {
		return Lead{}, err
	}
	return EncodeLead(decoded)
}

func fit(width, height, box int) (int, int) {
	if width <= box && height <= box {
		return width, height
	}
	if width >= height {
		return box, max(1, height*box/width)
	}
	return max(1, width*box/height), box
}

func cropAspect(source image.Image, ratioW, ratioH int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	targetW, targetH := width, height
	if width*ratioH > height*ratioW {
		targetW = height * ratioW / ratioH
	} else if width*ratioH < height*ratioW {
		targetH = width * ratioH / ratioW
	}
	if targetW == width && targetH == height {
		return source
	}
	left := bounds.Min.X + (width-targetW)/2
	top := bounds.Min.Y + (height-targetH)/2
	target := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	draw.Draw(target, target.Bounds(), source, image.Point{X: left, Y: top}, draw.Src)
	return target
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
