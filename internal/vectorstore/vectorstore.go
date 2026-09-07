package vectorstore

import (
	"context"
	"encoding/base64"
	"errors"
	"math"
	"strings"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
)

var ErrNotFound = errors.New("vector not found")

type Kind string

const (
	KindLive    Kind = "live"
	KindArchive Kind = "archive"
)

type Record struct {
	UserID      string
	Key         string
	Data        []float32
	Kind        Kind
	FeedID      string
	PublishedTS string
	ExpiresTS   int64
	Title       string
}

type Match struct {
	Key        string
	Similarity int
}

type Store interface {
	Put(context.Context, Record) error
	PutBatch(context.Context, []Record) error
	Delete(context.Context, string) error
	Get(context.Context, string) ([]float32, error)
	Query(context.Context, string, []float32, int, int64) ([]Match, error)
	Cleanup(context.Context, int64) (deleted int, size int, err error)
}

func FromItem(item domain.Item, kind Kind) Record {
	expires := item.TTL
	if kind == KindArchive {
		expires = 0
	}
	return Record{
		UserID: strings.TrimPrefix(item.PK, "U#"), Key: Key(strings.TrimPrefix(item.PK, "U#"), item.ItemID), Data: score.DecodeVector(item.Vector), Kind: kind,
		FeedID: item.FeedID, PublishedTS: item.PublishedTS, ExpiresTS: expires, Title: item.Title,
	}
}

func ImageRecordFromItem(item domain.Item, kind Kind) (Record, bool) {
	if len(item.ImageVector) == 0 {
		return Record{}, false
	}
	expires := item.TTL
	if kind == KindArchive {
		expires = 0
	}
	return Record{
		UserID: strings.TrimPrefix(item.PK, "U#"), Key: Key(strings.TrimPrefix(item.PK, "U#"), item.ItemID), Data: score.DecodeVector(item.ImageVector), Kind: kind,
		FeedID: item.FeedID, PublishedTS: item.PublishedTS, ExpiresTS: expires, Title: item.Title,
	}, true
}

func Similarity(distance float32) int {
	return int(math.Round(math.Max(0, math.Min(1, 1-float64(distance))) * 100))
}

// Key separates account identity from item identity without delimiter collisions.
func Key(userID, itemID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(userID)) + ":" + itemID
}
