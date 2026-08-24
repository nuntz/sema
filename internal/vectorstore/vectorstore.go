package vectorstore

import (
	"context"
	"errors"
	"math"

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
	Delete(context.Context, string) error
	Get(context.Context, string) ([]float32, error)
	Query(context.Context, []float32, int, int64) ([]Match, error)
	Cleanup(context.Context, int64) (deleted int, size int, err error)
}

func FromItem(item domain.Item, kind Kind) Record {
	expires := item.TTL
	if kind == KindArchive {
		expires = 0
	}
	return Record{
		Key: item.ItemID, Data: score.DecodeVector(item.Vector), Kind: kind,
		FeedID: item.FeedID, PublishedTS: item.PublishedTS, ExpiresTS: expires, Title: item.Title,
	}
}

func Similarity(distance float32) int {
	return int(math.Round(math.Max(0, math.Min(1, 1-float64(distance))) * 100))
}
