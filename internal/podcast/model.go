package podcast

import "time"

type Feed struct {
	ID           int64
	URL          string
	Title        string
	Description  string
	SiteURL      string
	ImageURL     string
	ETag         string
	LastModified string
	LastChecked  *time.Time
	LastSuccess  *time.Time
	LastError    string
	EpisodeCount int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Episode struct {
	ID              int64
	FeedID          int64
	GUID            string
	Title           string
	Description     string
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
	PublishedAt     *time.Time
	DurationSeconds int
	EnclosureURL    string
	EnclosureType   string
	ImageURL        string
}
