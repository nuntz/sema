package domain

import "time"

// FetchWindow selects items that arrived in [From, Before). Its zero value
// includes the entire retained window.
type FetchWindow struct {
	From   time.Time
	Before time.Time
}

func (w FetchWindow) Contains(timestamp string) bool {
	if w.From.IsZero() && w.Before.IsZero() {
		return true
	}
	fetched, err := time.Parse(time.RFC3339Nano, timestamp)
	return err == nil && !fetched.Before(w.From) && fetched.Before(w.Before)
}
