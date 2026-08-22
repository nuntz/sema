package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

const DefaultUserAgent = "Sema/0.1 (+https://sema.app)"

type Client struct {
	http    *http.Client
	maxBody int64
	agent   string
}

func New(timeout time.Duration, maxBody int64) *Client {
	dialer := &net.Dialer{Timeout: timeout}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := resolveHost(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, address := range addresses {
			if !safeIP(address) {
				return nil, fmt.Errorf("refusing non-public address for %s", host)
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
	}
	c := &http.Client{Timeout: timeout, Transport: transport}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("redirect limit exceeded")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("unsupported redirect scheme")
		}
		return nil
	}
	return &Client{http: c, maxBody: maxBody, agent: DefaultUserAgent}
}

func resolveHost(ctx context.Context, host string) ([]netip.Addr, error) {
	if parsed, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{parsed.Unmap()}, nil
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	for i := range addresses {
		addresses[i] = addresses[i].Unmap()
	}
	return addresses, nil
}

func safeIP(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsUnspecified() {
		return false
	}
	cgnat := netip.MustParsePrefix("100.64.0.0/10")
	documentation := []netip.Prefix{
		netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8::/32"),
	}
	if cgnat.Contains(address) {
		return false
	}
	for _, prefix := range documentation {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	FinalURL   *url.URL
}

func (c *Client) Get(ctx context.Context, rawURL string, headers http.Header) (Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return Response{}, fmt.Errorf("invalid HTTP URL %q", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("User-Agent", c.agent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/feed+json, application/json, text/html;q=0.9, */*;q=0.5")
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, c.maxBody+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return Response{}, err
	}
	if int64(len(body)) > c.maxBody {
		return Response{}, fmt.Errorf("response exceeds %d bytes", c.maxBody)
	}
	return Response{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: body, FinalURL: resp.Request.URL}, nil
}
