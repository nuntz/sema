package domain

import (
	"strings"
	"time"
)

// Live reports whether an Item is still in its Live Window. A kept live row
// remains live; its permanent Archive copy does not.
func Live(item Item, now int64) bool { return !IsArchive(item) && item.TTL > now }

// IsArchive distinguishes the permanent copy from a live Item with a Keep pointer.
func IsArchive(item Item) bool {
	return strings.HasPrefix(item.SK, "A#") || item.Archived || (item.TTL == 0 && item.HeartedTS != "")
}

func LiveWindowTTL(published time.Time) int64 { return published.Add(Retention).Unix() }
