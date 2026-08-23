package score

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

var ErrModelNotFound = errors.New("ranking model not found")

type ModelLoader interface {
	Model(context.Context, string) (domain.Model, error)
	PutModel(context.Context, domain.Model) error
	Signals(context.Context, string) ([]domain.Signal, error)
	Behaviours(context.Context, string) ([]domain.Behaviour, error)
}

type cachedModel struct {
	loadedAt time.Time
	value    domain.Model
}

type Cache struct {
	mu      sync.Mutex
	loader  ModelLoader
	maxAge  time.Duration
	version string
	byUser  map[string]cachedModel
}

func NewCache(loader ModelLoader, maxAge time.Duration, version string) *Cache {
	return &Cache{loader: loader, maxAge: maxAge, version: version, byUser: make(map[string]cachedModel)}
}

func (c *Cache) Get(ctx context.Context, userID string) (domain.Model, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.byUser[userID]; ok && time.Since(cached.loadedAt) < c.maxAge {
		return cached.value, nil
	}
	model, err := c.loader.Model(ctx, userID)
	if err == nil && (model.Version == c.version || c.version == "") {
		c.byUser[userID] = cachedModel{loadedAt: time.Now(), value: model}
		return model, nil
	}
	if err != nil && !errors.Is(err, ErrModelNotFound) {
		return domain.Model{}, err
	}
	if err == nil && model.ReplayTS != "" && model.Version != c.version {
		cold := domain.Model{PK: domain.UserPK(userID), SK: "MODEL", Version: c.version, FeedPrior: model.FeedPrior, FeedSignalCount: model.FeedSignalCount, ReplayTS: model.ReplayTS, ReplayVersion: model.ReplayVersion}
		c.byUser[userID] = cachedModel{loadedAt: time.Now(), value: cold}
		return cold, nil
	}
	signals, loadErr := c.loader.Signals(ctx, userID)
	if loadErr != nil {
		return domain.Model{}, loadErr
	}
	behaviours, loadErr := c.loader.Behaviours(ctx, userID)
	if loadErr != nil {
		return domain.Model{}, loadErr
	}
	built := BuildModel(userID, signals, behaviours, time.Now().UTC(), c.version)
	if err == nil {
		built.ReplayTS, built.ReplayVersion = model.ReplayTS, model.ReplayVersion
	}
	if putErr := c.loader.PutModel(ctx, built); putErr != nil {
		return domain.Model{}, putErr
	}
	c.byUser[userID] = cachedModel{loadedAt: time.Now(), value: built}
	return built, nil
}

func (c *Cache) Invalidate(userID string) {
	c.mu.Lock()
	delete(c.byUser, userID)
	c.mu.Unlock()
}

type Signal struct {
	Value  int
	Vector []float32
}
