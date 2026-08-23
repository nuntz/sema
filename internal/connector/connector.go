package connector

import (
	"context"
	"fmt"
	"net/http"

	"github.com/nuntz/sema/internal/domain"
)

type Connector interface {
	Fetch(context.Context, domain.Feed) (domain.FetchResult, error)
}

// HTTPStatusError preserves response metadata needed for status-aware retry
// decisions without exposing the response body.
type HTTPStatusError struct {
	StatusCode int
	Header     http.Header
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("feed returned HTTP %d", e.StatusCode)
}
