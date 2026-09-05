package story

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

const (
	defaultThreshold = 80
	defaultWindow    = 72 * time.Hour
)

type Config struct {
	Threshold int
	Window    time.Duration
}

type Candidate struct {
	Item       domain.Item
	Similarity int
}

type Rendered struct {
	StoryID     string        `json:"story_id"`
	SourceCount int           `json:"source_count"`
	Items       []domain.Item `json:"items"`
}

func FromEnv() Config {
	config := Config{Threshold: defaultThreshold, Window: defaultWindow}
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("STORY_SIMILARITY"))); err == nil && value >= 0 && value <= 100 {
		config.Threshold = value
	}
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("STORY_WINDOW_HOURS"))); err == nil && value > 0 {
		config.Window = time.Duration(value) * time.Hour
	}
	return config
}

func Qualifies(candidate, item domain.Item, similarity int, config Config) bool {
	if candidate.ItemID == "" || item.ItemID == "" || candidate.ItemID == item.ItemID {
		return false
	}
	candidatePublished, candidateErr := time.Parse(time.RFC3339Nano, candidate.PublishedTS)
	itemPublished, itemErr := time.Parse(time.RFC3339Nano, item.PublishedTS)
	if candidateErr != nil || itemErr != nil || absDuration(candidatePublished.Sub(itemPublished)) > config.Window {
		return false
	}
	return similarity >= config.Threshold || URLsMatch(candidate, item)
}

func URLsMatch(a, b domain.Item) bool {
	aURLs := normalizedURLs(a)
	bURLs := normalizedURLs(b)
	for value := range aURLs {
		if bURLs[value] {
			return true
		}
	}
	return false
}

func Choose(matches []Candidate) (string, bool) {
	ordered := append([]Candidate(nil), matches...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Similarity != ordered[j].Similarity {
			return ordered[i].Similarity > ordered[j].Similarity
		}
		return ordered[i].Item.ItemID < ordered[j].Item.ItemID
	})
	for _, match := range ordered {
		if match.Item.StoryID != "" {
			return match.Item.StoryID, true
		}
	}
	return "", false
}

func Lead(members []domain.Item) domain.Item {
	if len(members) == 0 {
		return domain.Item{}
	}
	best := members[0]
	for _, item := range members[1:] {
		if betterLead(item, best) {
			best = item
		}
	}
	return best
}

func SourceCount(members []domain.Item) int {
	feeds := make(map[string]bool)
	for _, item := range members {
		if item.FeedID != "" {
			feeds[item.FeedID] = true
		}
	}
	return len(feeds)
}

func OrderKey(lead domain.Item, sourceCount int) float64 {
	breadth := min(max(sourceCount-1, 0), 4)
	return lead.Score * (1 + 0.15*float64(breadth))
}

func Render(stories []domain.Story, members map[string][]domain.Item, allowed map[string]bool, unreadOnly bool) ([]Rendered, map[string]bool) {
	rendered := make([]Rendered, 0, len(stories))
	hidden := make(map[string]bool)
	for _, row := range stories {
		visible := make([]domain.Item, 0, len(members[row.StoryID]))
		hasUnread := false
		for _, item := range members[row.StoryID] {
			if allowed != nil && !allowed[item.FeedID] {
				continue
			}
			visible = append(visible, item)
			hasUnread = hasUnread || !item.Read
		}
		sourceCount := SourceCount(visible)
		if sourceCount < 2 || (unreadOnly && !hasUnread) {
			continue
		}
		lead := Lead(visible)
		sort.SliceStable(visible, func(i, j int) bool {
			if visible[i].ItemID == lead.ItemID {
				return true
			}
			if visible[j].ItemID == lead.ItemID {
				return false
			}
			if visible[i].PublishedTS != visible[j].PublishedTS {
				return visible[i].PublishedTS > visible[j].PublishedTS
			}
			return visible[i].ItemID < visible[j].ItemID
		})
		for _, item := range visible {
			hidden[item.ItemID] = true
		}
		rendered = append(rendered, Rendered{StoryID: row.StoryID, SourceCount: sourceCount, Items: visible})
	}
	sort.SliceStable(rendered, func(i, j int) bool {
		iLead, jLead := rendered[i].Items[0], rendered[j].Items[0]
		iKey, jKey := OrderKey(iLead, rendered[i].SourceCount), OrderKey(jLead, rendered[j].SourceCount)
		if iKey != jKey {
			return iKey > jKey
		}
		if iLead.PublishedTS != jLead.PublishedTS {
			return iLead.PublishedTS > jLead.PublishedTS
		}
		return rendered[i].StoryID < rendered[j].StoryID
	})
	return rendered, hidden
}

func betterLead(candidate, current domain.Item) bool {
	if candidate.Score != current.Score {
		return candidate.Score > current.Score
	}
	candidateMedia := candidate.MediaKey != "" || len(candidate.MediaVariants) > 0
	currentMedia := current.MediaKey != "" || len(current.MediaVariants) > 0
	if candidateMedia != currentMedia {
		return candidateMedia
	}
	if candidate.ExtractQuality != current.ExtractQuality {
		return candidate.ExtractQuality > current.ExtractQuality
	}
	if candidate.PublishedTS != current.PublishedTS {
		return candidate.PublishedTS < current.PublishedTS
	}
	return candidate.ItemID < current.ItemID
}

func normalizedURLs(item domain.Item) map[string]bool {
	values := make(map[string]bool, 2)
	for _, raw := range []string{item.URL, item.ExternalURL} {
		if strings.TrimSpace(raw) != "" {
			values[domain.NormalizeURL(raw)] = true
		}
	}
	return values
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
