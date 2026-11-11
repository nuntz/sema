package requestsnapshot_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/nuntz/sema/cmd/api/internal/requestsnapshot"
)

// scriptedStore returns whatever versions, markers and errors a test sets. It
// models none of the store's rules, so failure paths the real store only
// reaches by losing a write can be set up directly.
type scriptedStore struct {
	mu          sync.Mutex
	readVersion int64
	markers     map[string]bool
	loadErr     error
	// setReadVersion and setReadErr are what SetRead returns.
	setReadVersion int64
	setReadErr     error
}

func (s *scriptedStore) ReadVersion(context.Context, string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readVersion, nil
}

func (s *scriptedStore) LoadReadMarkers(context.Context, string) (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	copied := make(map[string]bool, len(s.markers))
	for id, read := range s.markers {
		copied[id] = read
	}
	return copied, nil
}

func (s *scriptedStore) SetRead(context.Context, string, []string, bool) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setReadVersion, s.setReadErr
}

// script sets what storage holds now.
func (s *scriptedStore) script(version int64, markers ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readVersion = version
	s.markers = map[string]bool{}
	for _, id := range markers {
		s.markers[id] = true
	}
}

func TestReadWriteThatFailsDropsTheCopy(t *testing.T) {
	for _, test := range []struct {
		name    string
		version int64 // READ_VERSION after the failed write
	}{
		// A later chunk failed after earlier chunks committed and bumped.
		{"partway", 2},
		// The markers committed but the bump failed.
		{"failed bump", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := &scriptedStore{}
			db.script(1)
			clock := newClock()
			cache := requestsnapshot.NewCache(db, clock.Now)
			wantMarkers(t, readMarkers(t, cache.Snapshot("user")))
			request := cache.Snapshot("user")
			wantMarkers(t, readMarkers(t, request))
			db.setReadVersion, db.setReadErr = 0, errors.New("write failed")
			db.script(test.version, "a")

			if err := request.SetRead(context.Background(), []string{"a", "b"}, true); err == nil {
				t.Fatal("SetRead hid the write error")
			}

			wantMarkers(t, readMarkers(t, request), "a")
			wantMarkers(t, readMarkers(t, cache.Snapshot("user")), "a")
		})
	}
}

func TestReadWriteThatJumpsTheVersionDropsTheCopy(t *testing.T) {
	db := &scriptedStore{}
	db.script(1)
	clock := newClock()
	cache := requestsnapshot.NewCache(db, clock.Now)
	request := cache.Snapshot("user")
	wantMarkers(t, readMarkers(t, request))
	db.setReadVersion = 3
	db.script(3, "a", "b")

	setRead(t, request, true, "a")

	wantMarkers(t, readMarkers(t, request), "a", "b")
	wantMarkers(t, readMarkers(t, cache.Snapshot("user")), "a", "b")
}

func TestReadWriteThatWroteNothingKeepsTheCopy(t *testing.T) {
	db := &scriptedStore{}
	db.script(1)
	clock := newClock()
	cache := requestsnapshot.NewCache(db, clock.Now)
	request := cache.Snapshot("user")
	wantMarkers(t, readMarkers(t, request))
	db.setReadVersion = 0
	db.script(1, "unbumped")

	setRead(t, request, true, "missing")

	wantMarkers(t, readMarkers(t, request))
	wantMarkers(t, readMarkers(t, cache.Snapshot("user")))
}

func TestReadMarkerLoadErrorDropsTheCopy(t *testing.T) {
	db := &scriptedStore{}
	db.script(1, "a")
	clock := newClock()
	cache := requestsnapshot.NewCache(db, clock.Now)
	wantMarkers(t, readMarkers(t, cache.Snapshot("user")), "a")
	db.script(2, "a", "b")
	db.loadErr = errors.New("load failed")
	if _, _, err := cache.Snapshot("user").ReadMarkers(context.Background()); err == nil {
		t.Fatal("ReadMarkers hid the load error")
	}

	db.loadErr = nil
	db.script(1, "a", "b", "c")

	wantMarkers(t, readMarkers(t, cache.Snapshot("user")), "a", "b", "c")
}

func TestSnapshotLoadsReadMarkersOnceUnderConcurrency(t *testing.T) {
	db := &countingStore{}
	request := requestsnapshot.NewCache(db, newClock().Now).Snapshot("user")
	var wg sync.WaitGroup
	got := make([]map[string]bool, 16)
	for i := range got {
		wg.Go(func() {
			markers, _, err := request.ReadMarkers(context.Background())
			if err != nil {
				t.Error(err)
			}
			got[i] = markers
		})
	}
	wg.Wait()
	for _, markers := range got {
		wantMarkers(t, markers, "load-1")
	}
}

// countingStore's markers name the load that returned them, so two loads in
// one request would hand out different markers.
type countingStore struct {
	scriptedStore
	loads int
}

func (s *countingStore) LoadReadMarkers(context.Context, string) (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	return map[string]bool{fmt.Sprint("load-", s.loads): true}, nil
}
