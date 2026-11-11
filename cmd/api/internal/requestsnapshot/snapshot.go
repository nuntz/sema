// Package requestsnapshot owns the API's read-your-writes protocol for a
// user's state, which docs/adr/0005-read-state-is-read-your-writes.md,
// docs/adr/0006-feed-edits-are-read-your-writes.md and
// docs/adr/0007-stories-are-read-your-writes.md set out. Read them before
// changing this package.
//
// A Request snapshot is one user's state for one API request: each kind loads
// lazily, at most once, labelled with the per-user version row read strongly
// consistent before it, and stays fixed for the rest of the request apart
// from the request's own writes. Every write that moves a version goes
// through the Snapshot, which patches or drops the instance's cached copy.
//
// A Cache lives as long as the API instance and holds what its Snapshots
// loaded, under one eviction policy with a TTL per kind. Handlers never see
// cache entries or version rows.
package requestsnapshot

import (
	"context"
	"sync"
	"time"
)

// Store is the state the Request snapshot reads and the writes that move a
// version. *store.Store satisfies it.
type Store interface {
	ReadVersion(ctx context.Context, userID string) (int64, error)
	LoadReadMarkers(ctx context.Context, userID string) (map[string]bool, error)
	SetRead(ctx context.Context, userID string, ids []string, read bool) (int64, error)
}

// readTTL bounds how long a Read marker copy can serve writes that did not
// move READ_VERSION, such as those of an instance running older code.
const readTTL = time.Minute

// Cache is one API instance's copy of its users' state, shared by every
// Snapshot it starts. It is safe for concurrent use.
type Cache struct {
	store Store
	now   func() time.Time
	read  *entries[string, map[string]bool]
}

// NewCache returns an empty Cache over store that tells time with now, or
// with time.Now when now is nil.
func NewCache(store Store, now func() time.Time) *Cache {
	if now == nil {
		now = time.Now
	}
	return &Cache{store: store, now: now, read: newEntries[string, map[string]bool](readTTL)}
}

// Snapshot starts one request's view of userID's state. Nothing loads until
// the request asks for it.
func (c *Cache) Snapshot(userID string) *Snapshot {
	return &Snapshot{cache: c, userID: userID, outcomes: map[string]string{}}
}

// Snapshot is one user's state for one request. It is safe for concurrent
// use, and each kind still loads only once. Each kind has its own lock, held
// across its loads and writes, so one kind's load may use another kind.
type Snapshot struct {
	cache  *Cache
	userID string

	readMu sync.Mutex
	read   *readState

	outcomesMu sync.Mutex
	outcomes   map[string]string
}

// readState is a Snapshot's Read markers and the READ_VERSION read before
// them. The markers map is never changed once built: writes replace it.
type readState struct {
	markers map[string]bool
	version int64
}

// UserID is the user whose state this is.
func (s *Snapshot) UserID() string { return s.userID }

// ReadMarkers returns the IDs of the user's Read Items and the READ_VERSION
// they are labelled with. They load on first use and stay fixed for the
// request, apart from its own SetRead calls. The map is never nil and is
// shared: callers must not change it.
func (s *Snapshot) ReadMarkers(ctx context.Context) (map[string]bool, int64, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	if s.read == nil {
		read, err := s.loadRead(ctx)
		if err != nil {
			return nil, 0, err
		}
		s.read = read
	}
	return s.read.markers, s.read.version, nil
}

func (s *Snapshot) loadRead(ctx context.Context) (*readState, error) {
	cache := s.cache
	now := cache.now()
	// Read the version before the markers, so a copy is never labelled newer
	// than its contents.
	version, err := cache.store.ReadVersion(ctx, s.userID)
	if err != nil {
		return nil, err
	}
	markers, outcome := cache.read.lookup(s.userID, version, now)
	s.record("ReadCache", outcome)
	if outcome != outcomeHit {
		markers, err = cache.store.LoadReadMarkers(ctx, s.userID)
		if err != nil {
			cache.read.drop(s.userID)
			return nil, err
		}
		if markers == nil {
			// The store reads a nil snapshot as "load from storage".
			markers = map[string]bool{}
		}
		cache.read.fill(s.userID, version, markers, now)
	}
	return &readState{markers: markers, version: version}, nil
}

// SetRead marks ids Read, or unread when read is false. The request sees its
// own write afterwards. When the write moved READ_VERSION by exactly one, no
// other write landed in between, so the instance's copy is patched forward;
// otherwise it is dropped. A write error drops it too, because the write may
// have committed partway.
func (s *Snapshot) SetRead(ctx context.Context, ids []string, read bool) error {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	cache := s.cache
	version, err := cache.store.SetRead(ctx, s.userID, ids, read)
	if err != nil {
		cache.read.drop(s.userID)
		s.read = nil
		return err
	}
	if version == 0 {
		// Nothing was written, so nothing moved.
		return nil
	}
	cache.read.update(s.userID, func(cached entry[map[string]bool]) (entry[map[string]bool], bool) {
		if version != cached.version+1 {
			return cached, false
		}
		cached.value, cached.version = patchRead(cached.value, ids, read), version
		return cached, true
	})
	if s.read != nil {
		if version == s.read.version+1 {
			s.read = &readState{markers: patchRead(s.read.markers, ids, read), version: version}
		} else {
			s.read = nil
		}
	}
	return nil
}

// patchRead returns a copy of markers with ids set Read or unread.
func patchRead(markers map[string]bool, ids []string, read bool) map[string]bool {
	patched := make(map[string]bool, len(markers)+len(ids))
	for id, isRead := range markers {
		patched[id] = isRead
	}
	for _, id := range ids {
		if read {
			patched[id] = true
		} else {
			delete(patched, id)
		}
	}
	return patched
}

// CacheOutcomes returns each kind's cache outcome for this request, by its
// request event field name: ReadCache. Each records the first load's
// outcome, so a later hit in the same request can't hide an earlier miss.
func (s *Snapshot) CacheOutcomes() map[string]string {
	s.outcomesMu.Lock()
	defer s.outcomesMu.Unlock()
	outcomes := make(map[string]string, len(s.outcomes))
	for field, outcome := range s.outcomes {
		outcomes[field] = outcome
	}
	return outcomes
}

// record keeps the first outcome a request records for field.
func (s *Snapshot) record(field, outcome string) {
	s.outcomesMu.Lock()
	defer s.outcomesMu.Unlock()
	if _, recorded := s.outcomes[field]; !recorded {
		s.outcomes[field] = outcome
	}
}
