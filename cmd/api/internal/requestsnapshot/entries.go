package requestsnapshot

import (
	"sync"
	"time"
)

// maxEntries bounds each kind's cache across users and filter combinations
// in a warm instance.
const maxEntries = 128

// Cache outcomes, recorded once per request for each kind.
const (
	outcomeHit   = "hit"   // fresh, and labelled with the current version
	outcomeStale = "stale" // fresh, but another write moved the version
	outcomeMiss  = "miss"  // absent or expired
)

// entry is one cached value, labelled with the version read before it was
// loaded, so it is never labelled newer than what it holds.
type entry[V any] struct {
	loaded  time.Time
	version int64
	value   V
}

// entries is the eviction policy every kind shares: a per-kind TTL, a TTL
// sweep on fill, and at most maxEntries entries with the oldest evicted. Its
// lock covers map access only, never store I/O: version checks, not the lock,
// keep a concurrent fill or patch from serving another write's state.
type entries[K comparable, V any] struct {
	ttl time.Duration
	mu  sync.Mutex
	m   map[K]entry[V]
}

func newEntries[K comparable, V any](ttl time.Duration) *entries[K, V] {
	return &entries[K, V]{ttl: ttl, m: make(map[K]entry[V])}
}

// lookup returns the value cached for key if it is fresh at now and labelled
// with version, and the outcome either way.
func (e *entries[K, V]) lookup(key K, version int64, now time.Time) (V, string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cached, ok := e.m[key]
	switch {
	case !ok || now.Sub(cached.loaded) >= e.ttl:
		var zero V
		return zero, outcomeMiss
	case cached.version != version:
		var zero V
		return zero, outcomeStale
	default:
		return cached.value, outcomeHit
	}
}

// fill caches value for key, loaded at now and labelled with version.
func (e *entries[K, V]) fill(key K, version int64, value V, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for cachedKey, cached := range e.m {
		if now.Sub(cached.loaded) >= e.ttl {
			delete(e.m, cachedKey)
		}
	}
	if _, replacing := e.m[key]; !replacing && len(e.m) >= maxEntries {
		var oldest K
		oldestTime := now
		for cachedKey, cached := range e.m {
			if !cached.loaded.After(oldestTime) {
				oldest, oldestTime = cachedKey, cached.loaded
			}
		}
		delete(e.m, oldest)
	}
	e.m[key] = entry[V]{loaded: now, version: version, value: value}
}

// update replaces key's entry with what change returns, or drops it when
// change reports false. An absent key passes ok false and stays absent.
func (e *entries[K, V]) update(key K, change func(cached entry[V]) (entry[V], bool)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cached, ok := e.m[key]
	if !ok {
		return
	}
	if next, keep := change(cached); keep {
		e.m[key] = next
	} else {
		delete(e.m, key)
	}
}

// drop forgets key's entry.
func (e *entries[K, V]) drop(key K) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.m, key)
}
