package podcast

import "time"

type EpisodeState string

const (
	EpisodeStateInbox    EpisodeState = "inbox"
	EpisodeStateQueued   EpisodeState = "queued"
	EpisodeStateArchived EpisodeState = "archived"
)

type Feed struct {
	ID                   int64
	URL                  string
	Title                string
	Description          string
	SiteURL              string
	ImageURL             string
	ETag                 string
	LastModified         string
	LastChecked          *time.Time
	LastSuccess          *time.Time
	LastEpisodePublished *time.Time
	LastError            string
	EpisodeCount         int
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type Episode struct {
	ID              int64
	FeedID          int64
	FeedTitle       string
	FeedImageURL    string
	GUID            string
	Title           string
	Description     string
	DescriptionHTML string
	PublishedAt     *time.Time
	DurationSeconds int
	EnclosureURL    string
	EnclosureType   string
	ImageURL        string
	PositionSeconds float64
	PlayedDuration  float64
	Completed       bool
	Playing         bool
	PlaybackUpdated *time.Time
	State           EpisodeState
	QueuePosition   *int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ParsedFeed struct {
	Title        string
	Description  string
	SiteURL      string
	ImageURL     string
	ETag         string
	LastModified string
	Episodes     []ParsedEpisode
}

type ParsedEpisode struct {
	GUID            string
	Title           string
	Description     string
	DescriptionHTML string
	PublishedAt     *time.Time
	DurationSeconds int
	EnclosureURL    string
	EnclosureType   string
	ImageURL        string
}
