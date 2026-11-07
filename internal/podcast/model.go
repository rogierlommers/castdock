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
	PlaybackSpeed        float64
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func ValidPlaybackSpeed(speed float64) bool {
	return speed >= 0.5 && speed <= 3.0
}

type Episode struct {
	ID                int64
	FeedID            int64
	FeedTitle         string
	FeedImageURL      string
	FeedPlaybackSpeed float64
	GUID              string
	Title             string
	Description       string
	DescriptionHTML   string
	PublishedAt       *time.Time
	DurationSeconds   int
	EnclosureURL      string
	EnclosureType     string
	ImageURL          string
	PositionSeconds   float64
	PlayedDuration    float64
	Completed         bool
	Playing           bool
	PlaybackUpdated   *time.Time
	State             EpisodeState
	QueuePosition     *int
	CreatedAt         time.Time
	UpdatedAt         time.Time
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

type LibraryStatistics struct {
	FeedCount       int
	EpisodeCount    int
	InboxCount      int
	QueuedCount     int
	ArchivedCount   int
	CompletedCount  int
	InProgressCount int
	DatabaseBytes   int64
	LatestFeedScan  *time.Time
}
