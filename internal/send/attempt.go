package send

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/nuntz/sema/internal/httpx"
)

const (
	AttemptTimeout = 10 * time.Second
	UserAgent      = "Sema-Webhook/1"
)

// Poster is the HTTP seam; production uses the SSRF-guarded httpx client.
type Poster interface {
	Post(ctx context.Context, rawURL string, headers http.Header, body []byte) (httpx.Response, error)
}

// Delivery is one Send to one Destination. ID and Body stay fixed across
// attempts; each attempt gets a fresh timestamp and signature.
type Delivery struct {
	ID     string
	Event  string
	URL    string
	Secret string
	Body   []byte
}

type OutcomeKind string

const (
	Delivered OutcomeKind = "delivered"
	Retryable OutcomeKind = "retryable"
	Permanent OutcomeKind = "permanent"
)

// Outcome carries either the Destination's HTTP status or, when no response
// arrived, a short reason word.
type Outcome struct {
	Kind   OutcomeKind
	Status int
	Reason string
}

// Label is the value recorded as the Destination's last status.
func (o Outcome) Label() string {
	if o.Status != 0 {
		return strconv.Itoa(o.Status)
	}
	return o.Reason
}

type Sender struct {
	Poster Poster
	Now    func() time.Time
}

func NewSender() *Sender {
	return &Sender{Poster: httpx.New(AttemptTimeout, 64*1024), Now: time.Now}
}

// ValidURL reports whether raw is acceptable as a Destination URL.
func ValidURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

// Attempt makes one signed POST and classifies the result.
func (s *Sender) Attempt(ctx context.Context, delivery Delivery) Outcome {
	if !ValidURL(delivery.URL) {
		return Outcome{Kind: Permanent, Reason: "invalid url"}
	}
	timestamp := s.Now().Unix()
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("User-Agent", UserAgent)
	headers.Set("X-Sema-Event", delivery.Event)
	headers.Set("X-Sema-Delivery", delivery.ID)
	headers.Set("X-Sema-Timestamp", strconv.FormatInt(timestamp, 10))
	headers.Set("X-Sema-Signature", Sign(delivery.Secret, timestamp, delivery.Body))
	response, err := s.Poster.Post(ctx, delivery.URL, headers, delivery.Body)
	if err != nil {
		return classifyError(err)
	}
	status := response.StatusCode
	switch {
	case status >= 200 && status < 300:
		return Outcome{Kind: Delivered, Status: status}
	case status == http.StatusTooManyRequests || status >= 500:
		return Outcome{Kind: Retryable, Status: status}
	default:
		return Outcome{Kind: Permanent, Status: status}
	}
}

func classifyError(err error) Outcome {
	var dnsError *net.DNSError
	var certificateError *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var recordError tls.RecordHeaderError
	switch {
	case errors.Is(err, httpx.ErrBlockedAddress):
		return Outcome{Kind: Permanent, Reason: "blocked address"}
	case errors.As(err, &certificateError), errors.As(err, &unknownAuthority), errors.As(err, &hostnameError), errors.As(err, &recordError):
		return Outcome{Kind: Permanent, Reason: "tls"}
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		return Outcome{Kind: Retryable, Reason: "timeout"}
	case errors.As(err, &dnsError):
		return Outcome{Kind: Retryable, Reason: "dns"}
	default:
		return Outcome{Kind: Retryable, Reason: "network"}
	}
}
