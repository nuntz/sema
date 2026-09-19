// Package ingest owns admission, replay planning and Item construction. Its
// decisions are pure; the worker supplies fetch, media, embedding and storage.
package ingest

import (
	"fmt"
	"strings"
	"time"

	"github.com/nuntz/sema/internal/connector"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
)

type Work struct {
	Message    domain.ItemMessage
	Published  time.Time
	Expired    bool
	Video      bool
	Assets     bool
	Text       bool
	ReuseImage bool
}

func Plan(message domain.ItemMessage, existing domain.Item, now time.Time, textVersion, imageVersion string) (Work, error) {
	published, err := time.Parse(time.RFC3339Nano, message.PublishedTS)
	if err != nil {
		return Work{}, fmt.Errorf("published_ts: %w", err)
	}
	message.Title = connector.EntryTitle(message.Title, message.SummaryRaw, message.ContentRaw)
	if message.Reprocess {
		if message.Title == "" {
			message.Title = existing.Title
		}
		if message.ExternalURL == "" {
			message.ExternalURL = existing.ExternalURL
		}
		if message.PostType == "" {
			message.PostType = existing.PostType
		}
		if message.MediaType == "" {
			message.MediaType = existing.MediaType
		}
		if message.VideoID == "" {
			message.VideoID = existing.VideoID
		}
		if message.ContentRaw == "" && message.VideoID != "" {
			message.ContentRaw = existing.Description
		}
		message.IsShort = message.IsShort || existing.IsShort
		message.LinkItem = message.LinkItem || existing.LinkItem
	}
	video := message.MediaType == "video" || message.VideoID != ""
	return Work{
		Message: message, Published: published, Expired: domain.LiveWindowTTL(published) <= now.Unix(), Video: video,
		Assets:     !message.Reprocess || message.ForceExtract,
		Text:       !message.Reprocess || message.ForceExtract || message.ForceSummary || len(existing.Vector) == 0 || !score.CompatibleVersion(existing.ModelVersion, textVersion),
		ReuseImage: !video && message.Reprocess && !message.ForceExtract && len(existing.ImageVector) > 0 && score.CompatibleVersion(existing.ImageModelVersion, imageVersion),
	}, nil
}

func HasBody(video bool, html string, quality float64) bool {
	return !video && html != "" && quality >= 0.3
}
func TextInput(title, summary, paragraph string) string {
	runes := []rune(strings.TrimSpace(title + "\n" + summary + "\n" + paragraph))
	if len(runes) > 2048 {
		runes = runes[:2048]
	}
	return string(runes)
}

// Build completes the output of the injected stages. Replay preserves identity,
// Keep state and the original Live Window; a new ingest gets publication TTL.
func Build(work Work, existing, stages domain.Item, now time.Time, scoringVersion string, model domain.Model) domain.Item {
	message := work.Message
	stages.PK = domain.UserPK(message.User)
	stages.SK = domain.ItemSK(work.Published, message.ItemID)
	stages.FeedPK = domain.FeedSK(message.FeedID)
	stages.ItemID, stages.FeedID = message.ItemID, message.FeedID
	stages.URL, stages.ExternalURL, stages.PostType = message.URL, message.ExternalURL, message.PostType
	stages.PublishedTS, stages.FetchedTS = domain.Timestamp(work.Published), domain.Timestamp(now)
	stages.TTL = domain.LiveWindowTTL(work.Published)
	stages.SearchText = domain.DeriveSearchText(stages.Title, stages.Summary)
	if scoringVersion == "1" {
		model = domain.Model{}
	}
	stages.Size = score.Size(stages.Score, model)
	if work.Video {
		stages.ImageVector = nil
		stages.ImageModelVersion = ""
		stages.HasBody = false
	}
	if message.Reprocess {
		stages.PK, stages.SK, stages.FeedPK = existing.PK, existing.SK, existing.FeedPK
		stages.URL = existing.URL
		stages.FetchedTS, stages.TTL = existing.FetchedTS, existing.TTL
		stages.ArchiveSK, stages.HeartedTS = existing.ArchiveSK, existing.HeartedTS
		stages.StoryID = existing.StoryID
	}
	return stages
}
