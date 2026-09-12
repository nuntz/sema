package domain

import "time"

// ItemFilter is independent of the fetched-time calendar window.
type ItemFilter struct {
	Published     FetchWindow
	Unkept        bool
	Ascending     bool
	TonightBefore time.Time
}

func (f ItemFilter) Contains(item Item) bool {
	return f.Published.Contains(item.PublishedTS) && (!f.Unkept || (item.ArchiveSK == "" && !item.Hearted && !item.Archived))
}

func (f ItemFilter) Tonight(published string) bool {
	t, err := time.Parse(time.RFC3339Nano, published)
	return err == nil && !f.TonightBefore.IsZero() && t.Add(Retention).Before(f.TonightBefore)
}
