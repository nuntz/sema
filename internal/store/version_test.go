package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
)

// versionFake records writes and version bumps in order. A bump is an
// UpdateItem on a *_VERSION row and is recorded by its row; every other
// write is recorded as "write" and fails when failWrites is set.
type versionFake struct {
	fakeDynamoDB
	mu         sync.Mutex
	events     []string
	versions   map[string]int
	failWrites bool
	failBump   bool
}

func newVersionFake() *versionFake {
	fake := &versionFake{versions: map[string]int{}}
	write := func() error {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.events = append(fake.events, "write")
		if fake.failWrites {
			return errThrottled
		}
		return nil
	}
	fake.putItem = func(*dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
		return &dynamodb.PutItemOutput{}, write()
	}
	fake.deleteItem = func(*dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error) {
		return &dynamodb.DeleteItemOutput{}, write()
	}
	fake.batchWrite = func(*dynamodb.BatchWriteItemInput) (*dynamodb.BatchWriteItemOutput, error) {
		return &dynamodb.BatchWriteItemOutput{}, write()
	}
	fake.transactWrite = func(*dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		return &dynamodb.TransactWriteItemsOutput{}, write()
	}
	fake.updateItem = func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
		sk := input.Key["SK"].(*types.AttributeValueMemberS).Value
		if !strings.HasSuffix(sk, "_VERSION") {
			return &dynamodb.UpdateItemOutput{}, write()
		}
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.events = append(fake.events, sk)
		if fake.failBump {
			return nil, errBumpFailed
		}
		fake.versions[sk]++
		return &dynamodb.UpdateItemOutput{Attributes: map[string]types.AttributeValue{
			"version": &types.AttributeValueMemberN{Value: fmt.Sprint(fake.versions[sk])},
		}}, nil
	}
	return fake
}

var (
	errThrottled  = errors.New("throttled")
	errBumpFailed = errors.New("bump failed")
)

func (f *versionFake) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.events)
}

func putRow(ctx context.Context, s *Store) error {
	_, err := s.db.PutItem(ctx, &dynamodb.PutItemInput{})
	return err
}

func TestChangeBumpsOnceAfterItsWrites(t *testing.T) {
	fake := newVersionFake()
	repository := New(fake, nil, "table", "", "")
	version, err := repository.change(context.Background(), "user", domain.FeedVersionSK, func(s *Store) error {
		if err := putRow(context.Background(), s); err != nil {
			return err
		}
		return putRow(context.Background(), s)
	})
	if err != nil || version != 1 {
		t.Fatalf("version = %d, err = %v", version, err)
	}
	if got, want := fake.recorded(), []string{"write", "write", domain.FeedVersionSK}; !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

// A call that only reads, or is rejected before writing, leaves other
// instances nothing to reload.
func TestChangeWithoutWritesLeavesTheVersionAlone(t *testing.T) {
	fake := newVersionFake()
	repository := New(fake, nil, "table", "", "")
	version, err := repository.change(context.Background(), "user", domain.FeedVersionSK, func(s *Store) error {
		_, err := s.db.GetItem(context.Background(), &dynamodb.GetItemInput{})
		return err
	})
	if err != nil || version != 0 || len(fake.recorded()) != 0 {
		t.Fatalf("version = %d, err = %v, events = %v", version, err, fake.recorded())
	}
	if _, err := repository.change(context.Background(), "user", domain.FeedVersionSK, func(*Store) error {
		return errThrottled
	}); err != errThrottled || len(fake.recorded()) != 0 {
		t.Fatalf("rejected: err = %v, events = %v", err, fake.recorded())
	}
}

// A write that failed may still have committed; see change.
func TestChangeBumpsAfterAFailedWrite(t *testing.T) {
	fake := newVersionFake()
	fake.failWrites = true
	_, err := New(fake, nil, "table", "", "").change(context.Background(), "user", domain.ReadVersionSK, func(s *Store) error {
		return putRow(context.Background(), s)
	})
	if !errors.Is(err, errThrottled) {
		t.Fatalf("err = %v, want the write error", err)
	}
	if got, want := fake.recorded(), []string{"write", domain.ReadVersionSK}; !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

// A bump that fails fails the call, even after the write committed, so the
// client retries rather than trusting other instances to have seen it.
func TestChangeReportsAFailedBump(t *testing.T) {
	for _, failWrites := range []bool{false, true} {
		fake := newVersionFake()
		fake.failWrites, fake.failBump = failWrites, true
		_, err := New(fake, nil, "table", "", "").change(context.Background(), "user", domain.FeedbackVersionSK, func(s *Store) error {
			return putRow(context.Background(), s)
		})
		if !errors.Is(err, errBumpFailed) || errors.Is(err, errThrottled) != failWrites {
			t.Fatalf("failWrites = %v: err = %v, want the bump error and any write error", failWrites, err)
		}
	}
}

// SaveFeeds writes in parallel; the version still moves once, after them all.
func TestChangeBumpsOnceAfterParallelWrites(t *testing.T) {
	fake := newVersionFake()
	_, err := New(fake, nil, "table", "", "").change(context.Background(), "user", domain.FeedVersionSK, func(s *Store) error {
		var group sync.WaitGroup
		for range 8 {
			group.Go(func() { _ = putRow(context.Background(), s) })
		}
		group.Wait()
		return nil
	})
	want := append(slices.Repeat([]string{"write"}, 8), domain.FeedVersionSK)
	if got := fake.recorded(); err != nil || !slices.Equal(got, want) {
		t.Fatalf("err = %v, events = %v, want %v", err, got, want)
	}
}

func userFeed(id string) domain.Feed {
	return domain.Feed{PK: domain.UserPK("user"), SK: domain.FeedSK(id), FeedID: id}
}

// Each writer moves only its own version, once, after its writes, including
// when they fail. The Keep and Unkeep tests in store_test.go cover a SetHeart
// that commits.
func TestWritersMoveTheirVersion(t *testing.T) {
	ctx := context.Background()
	item := domain.Item{ItemID: "item", FeedID: "feed", Vector: []byte{1}}
	other := userFeed("b")
	other.PK = domain.UserPK("other")
	for _, test := range []struct {
		name       string
		failWrites bool
		call       func(*Store) error
		want       string
	}{
		{name: "save feeds", call: func(s *Store) error {
			_, err := s.SaveFeeds(ctx, "user", []domain.Feed{userFeed("a"), userFeed("b")})
			return err
		}, want: domain.FeedVersionSK},
		{name: "failed save feeds", failWrites: true, call: func(s *Store) error {
			_, err := s.SaveFeeds(ctx, "user", []domain.Feed{userFeed("a"), userFeed("b")})
			return err
		}, want: domain.FeedVersionSK},
		{name: "no feeds to save", call: func(s *Store) error {
			_, err := s.SaveFeeds(ctx, "user", nil)
			return err
		}},
		{name: "another user's feed", call: func(s *Store) error {
			_, err := s.SaveFeeds(ctx, "user", []domain.Feed{userFeed("a"), other})
			return err
		}},
		{name: "delete feed", call: func(s *Store) error {
			_, err := s.DeleteFeed(ctx, "user", "a")
			return err
		}, want: domain.FeedVersionSK},
		{name: "failed delete feed", failWrites: true, call: func(s *Store) error {
			_, err := s.DeleteFeed(ctx, "user", "a")
			return err
		}, want: domain.FeedVersionSK},
		// A worker fetch must not move the version, or every instance's Feed
		// cache would go stale after each fetch.
		{name: "worker feed write", call: func(s *Store) error {
			return s.PutFeed(ctx, userFeed("a"))
		}},
		{name: "mark unread", call: func(s *Store) error {
			_, err := s.SetRead(ctx, "user", []string{"a", "b"}, false)
			return err
		}, want: domain.ReadVersionSK},
		{name: "failed mark unread", failWrites: true, call: func(s *Store) error {
			_, err := s.SetRead(ctx, "user", []string{"a", "b"}, false)
			return err
		}, want: domain.ReadVersionSK},
		{name: "read nothing live", call: func(s *Store) error {
			_, err := s.SetRead(ctx, "user", []string{"a"}, true)
			return err
		}},
		{name: "boost", call: func(s *Store) error {
			return s.SetSignal(ctx, "user", item, 1)
		}, want: domain.FeedbackVersionSK},
		{name: "neutral signal", call: func(s *Store) error {
			return s.SetSignal(ctx, "user", item, 0)
		}, want: domain.FeedbackVersionSK},
		{name: "failed boost", failWrites: true, call: func(s *Store) error {
			return s.SetSignal(ctx, "user", item, 1)
		}, want: domain.FeedbackVersionSK},
		{name: "rejected bury on a kept item", call: func(s *Store) error {
			return s.SetSignal(ctx, "user", domain.Item{ItemID: "item", ArchiveSK: "A#item"}, -1)
		}},
		{name: "keep failing before writing", call: func(s *Store) error {
			_, _, err := s.SetHeart(ctx, "user", "item", true)
			return err
		}},
		{name: "unkeep failing before writing", call: func(s *Store) error {
			_, _, err := s.SetHeart(ctx, "user", "item", false)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newVersionFake()
			fake.failWrites = test.failWrites
			_ = test.call(New(fake, nil, "table", "", ""))
			events := fake.recorded()
			bumps := slices.DeleteFunc(slices.Clone(events), func(event string) bool { return event == "write" })
			switch {
			case test.want == "" && len(bumps) != 0:
				t.Fatalf("events = %v, want no bump", events)
			case test.want != "" && (!slices.Equal(bumps, []string{test.want}) || events[len(events)-1] != test.want):
				t.Fatalf("events = %v, want one %s after the writes", events, test.want)
			}
		})
	}
}

// The Request snapshot patches its Read markers forward only when SetRead
// returns exactly one more than its cached version, and reads 0 as nothing
// written, so writers must hand back change's version unaltered.
func TestWritersReturnTheirNewVersion(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name string
		sk   string
		call func(*Store) (int64, error)
		want int64
	}{
		{name: "save feeds", sk: domain.FeedVersionSK, call: func(s *Store) (int64, error) {
			return s.SaveFeeds(ctx, "user", []domain.Feed{userFeed("a")})
		}, want: 42},
		{name: "no feeds to save", sk: domain.FeedVersionSK, call: func(s *Store) (int64, error) {
			return s.SaveFeeds(ctx, "user", nil)
		}},
		{name: "delete feed", sk: domain.FeedVersionSK, call: func(s *Store) (int64, error) {
			return s.DeleteFeed(ctx, "user", "a")
		}, want: 42},
		{name: "mark unread", sk: domain.ReadVersionSK, call: func(s *Store) (int64, error) {
			return s.SetRead(ctx, "user", []string{"a"}, false)
		}, want: 42},
		{name: "no ids", sk: domain.ReadVersionSK, call: func(s *Store) (int64, error) {
			return s.SetRead(ctx, "user", nil, true)
		}},
		{name: "read nothing live", sk: domain.ReadVersionSK, call: func(s *Store) (int64, error) {
			return s.SetRead(ctx, "user", []string{"a"}, true)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newVersionFake()
			fake.versions[test.sk] = 41
			if got, err := test.call(New(fake, nil, "table", "", "")); err != nil || got != test.want {
				t.Fatalf("version = %d, err = %v, want %d", got, err, test.want)
			}
		})
	}
}

// SetRead writes in chunks; a later chunk failing still leaves the earlier
// one committed, so the version moves once, after both attempts.
func TestSetReadBumpsOnceWhenALaterChunkFails(t *testing.T) {
	fake := newVersionFake()
	chunks := 0
	transact := fake.transactWrite
	fake.transactWrite = func(input *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		chunks++
		output, err := transact(input)
		if chunks == 2 {
			return nil, errThrottled
		}
		return output, err
	}
	_, err := New(fake, nil, "table", "", "").SetRead(context.Background(), "user", readIDs(setReadTransactionItems+10), false)
	if !errors.Is(err, errThrottled) {
		t.Fatalf("err = %v, want the chunk error", err)
	}
	if got, want := fake.recorded(), []string{"write", "write", domain.ReadVersionSK}; !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}
