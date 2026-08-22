package connector

import (
	"context"

	"github.com/nuntz/sema/internal/domain"
)

type Connector interface {
	Fetch(context.Context, domain.Feed) (domain.FetchResult, error)
}
