package score

import (
	"context"
	"sync"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

type SignalLoader interface {
	Signals(context.Context, string) ([]domain.Signal, error)
}

type cachedSignals struct {
	loadedAt time.Time
	values   []Signal
}

type Cache struct {
	mu     sync.Mutex
	loader SignalLoader
	maxAge time.Duration
	byUser map[string]cachedSignals
}

func NewCache(loader SignalLoader, maxAge time.Duration) *Cache {
	return &Cache{loader: loader, maxAge: maxAge, byUser: make(map[string]cachedSignals)}
}

func (c *Cache) Get(ctx context.Context, userID string) ([]Signal, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.byUser[userID]; ok && time.Since(cached.loadedAt) < c.maxAge {
		return cached.values, nil
	}
	rows, err := c.loader.Signals(ctx, userID)
	if err != nil {
		return nil, err
	}
	values := make([]Signal, 0, len(rows))
	for _, row := range rows {
		values = append(values, Signal{Value: row.Value, Vector: DecodeVector(row.Vector)})
	}
	c.byUser[userID] = cachedSignals{loadedAt: time.Now(), values: values}
	return values, nil
}
