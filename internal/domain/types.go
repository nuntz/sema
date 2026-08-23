package domain

import "time"

const (
	Retention       = 7 * 24 * time.Hour
	MaxSummaryRunes = 400
	TimeLayout      = "2006-01-02T15:04:05.000000000Z"
)

type Order string

const (
	OrderChrono   Order = "chrono"
	OrderInterest Order = "interest"
)

type User struct {
	PK               string `dynamodbav:"PK" json:"-"`
	SK               string `dynamodbav:"SK" json:"-"`
	Email            string `dynamodbav:"email,omitempty" json:"email"`
	CreatedAt        string `dynamodbav:"created_at" json:"created_at"`
	OrderPref        Order  `dynamodbav:"order_pref" json:"order_pref"`
	InterestPosition string `dynamodbav:"interest_position,omitempty" json:"interest_position,omitempty"`
	HeartCount       int    `dynamodbav:"heart_count" json:"heart_count"`
	SignalCount      int    `dynamodbav:"signal_count" json:"signal_count"`
}

type Feed struct {
	PK           string `dynamodbav:"PK" json:"-"`
	SK           string `dynamodbav:"SK" json:"-"`
	GSI1PK       string `dynamodbav:"gsi1pk,omitempty" json:"-"`
	FeedID       string `dynamodbav:"feed_id" json:"feed_id"`
	URL          string `dynamodbav:"url" json:"url"`
	SiteURL      string `dynamodbav:"site_url,omitempty" json:"site_url,omitempty"`
	Title        string `dynamodbav:"title,omitempty" json:"title,omitempty"`
	FaviconKey   string `dynamodbav:"favicon_key,omitempty" json:"favicon_key,omitempty"`
	ETag         string `dynamodbav:"etag,omitempty" json:"-"`
	LastModified string `dynamodbav:"last_modified,omitempty" json:"-"`
	LastFetchAt  string `dynamodbav:"last_fetch_at,omitempty" json:"last_fetch_at,omitempty"`
	LastStatus   string `dynamodbav:"last_status,omitempty" json:"last_status,omitempty"`
	ErrorCount   int    `dynamodbav:"error_count" json:"error_count"`
	NextFetchAt  string `dynamodbav:"next_fetch_at" json:"next_fetch_at"`
}

type Item struct {
	PK          string  `dynamodbav:"PK" json:"-"`
	SK          string  `dynamodbav:"SK" json:"-"`
	FeedPK      string  `dynamodbav:"feed_pk" json:"-"`
	ItemID      string  `dynamodbav:"item_id" json:"item_id"`
	FeedID      string  `dynamodbav:"feed_id" json:"feed_id"`
	FeedTitle   string  `dynamodbav:"feed_title,omitempty" json:"feed_title,omitempty"`
	FaviconKey  string  `dynamodbav:"favicon_key,omitempty" json:"favicon_url,omitempty"`
	URL         string  `dynamodbav:"url" json:"url"`
	Title       string  `dynamodbav:"title" json:"title"`
	Summary     string  `dynamodbav:"summary,omitempty" json:"summary,omitempty"`
	Author      string  `dynamodbav:"author,omitempty" json:"author,omitempty"`
	PublishedTS string  `dynamodbav:"published_ts" json:"published_ts"`
	FetchedTS   string  `dynamodbav:"fetched_ts" json:"fetched_ts"`
	MediaKey    string  `dynamodbav:"media_key,omitempty" json:"media_url,omitempty"`
	MediaW      int     `dynamodbav:"media_w,omitempty" json:"media_w,omitempty"`
	MediaH      int     `dynamodbav:"media_h,omitempty" json:"media_h,omitempty"`
	BodyKey     string  `dynamodbav:"body_key,omitempty" json:"body_url,omitempty"`
	HasBody     bool    `dynamodbav:"has_body" json:"has_body"`
	Score       float64 `dynamodbav:"score" json:"score"`
	Size        string  `dynamodbav:"size" json:"size"`
	Vector      []byte  `dynamodbav:"vector,omitempty" json:"-"`
	TTL         int64   `dynamodbav:"ttl,omitempty" json:"-"`
	ArchiveSK   string  `dynamodbav:"archive_sk,omitempty" json:"-"`
	HeartedTS   string  `dynamodbav:"hearted_ts,omitempty" json:"hearted_ts,omitempty"`
	Read        bool    `dynamodbav:"-" json:"read"`
	Signal      int     `dynamodbav:"-" json:"signal"`
	Hearted     bool    `dynamodbav:"-" json:"hearted"`
}

type Signal struct {
	PK        string `dynamodbav:"PK"`
	SK        string `dynamodbav:"SK"`
	ItemID    string `dynamodbav:"item_id"`
	Value     int    `dynamodbav:"value"`
	Vector    []byte `dynamodbav:"vector"`
	Title     string `dynamodbav:"title"`
	FeedID    string `dynamodbav:"feed_id"`
	CreatedAt string `dynamodbav:"created_at"`
	Source    string `dynamodbav:"source,omitempty"`
}

type Read struct {
	PK     string `dynamodbav:"PK"`
	SK     string `dynamodbav:"SK"`
	ReadAt string `dynamodbav:"read_at"`
	TTL    int64  `dynamodbav:"ttl"`
}

type Enclosure struct {
	URL    string `json:"url"`
	Type   string `json:"type,omitempty"`
	Length string `json:"length,omitempty"`
}

type Entry struct {
	GUID         string
	URL          string
	Title        string
	SummaryRaw   string
	ContentRaw   string
	Author       string
	Published    time.Time
	Enclosures   []Enclosure
	PageHTMLHint string
}

type FetchResult struct {
	NotModified bool
	Title       string
	SiteURL     string
	ETag        string
	Modified    string
	Entries     []Entry
}

type FeedMessage struct {
	User   string `json:"user"`
	FeedID string `json:"feed_id"`
}

type ItemMessage struct {
	User          string      `json:"user"`
	FeedID        string      `json:"feed_id"`
	ItemID        string      `json:"item_id"`
	URL           string      `json:"url"`
	Title         string      `json:"title"`
	SummaryRaw    string      `json:"summary_raw,omitempty"`
	ContentRaw    string      `json:"content_raw,omitempty"`
	Author        string      `json:"author,omitempty"`
	PublishedTS   string      `json:"published_ts"`
	EnclosureURLs []Enclosure `json:"enclosure_urls,omitempty"`
}

func UserPK(user string) string { return "U#" + user }
func FeedSK(id string) string   { return "F#" + id }
func SignalSK(id string) string { return "S#" + id }
func ReadSK(id string) string   { return "R#" + id }

func ArchiveSK(hearted time.Time, id string) string {
	return "A#" + Timestamp(hearted) + "#" + id
}

func ItemSK(published time.Time, id string) string {
	return "I#" + Timestamp(published) + "#" + id
}

// Timestamp has a fixed-width fractional component so lexical DynamoDB order
// is identical to chronological order.
func Timestamp(value time.Time) string { return value.UTC().Format(TimeLayout) }
