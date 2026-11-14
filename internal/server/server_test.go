package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authn "github.com/rlommers/castdock/internal/auth"
	"github.com/rlommers/castdock/internal/playback"
	"github.com/rlommers/castdock/internal/podcast"
	"github.com/rlommers/castdock/internal/storage"
	"github.com/rlommers/castdock/internal/syndication"
	webassets "github.com/rlommers/castdock/web"
)

type fakeRepository struct {
	saved         playback.State
	advanceQueue  bool
	next          podcast.Episode
	stateID       int64
	state         podcast.EpisodeState
	queued        []podcast.Episode
	inbox         []podcast.Episode
	recent        []podcast.Episode
	history       []podcast.Episode
	historyLimit  int
	feeds         []podcast.Feed
	feed          podcast.Feed
	episode       podcast.Episode
	nowPlaying    podcast.Episode
	reorderID     int64
	reorderTarget int
	playedID      int64
	speedFeedID   int64
	playbackSpeed float64
	statistics    podcast.LibraryStatistics
}

func (f *fakeRepository) Health(context.Context) error                      { return nil }
func (f *fakeRepository) ListFeeds(context.Context) ([]podcast.Feed, error) { return f.feeds, nil }
func (f *fakeRepository) RecentEpisodes(context.Context, int, int) ([]podcast.Episode, error) {
	return f.recent, nil
}
func (f *fakeRepository) PlayHistoryEpisodes(_ context.Context, limit int) ([]podcast.Episode, error) {
	f.historyLimit = limit
	return f.history, nil
}
func (f *fakeRepository) InboxEpisodes(context.Context) ([]podcast.Episode, error) {
	return f.inbox, nil
}
func (f *fakeRepository) Feed(context.Context, int64) (podcast.Feed, error) {
	return f.feed, nil
}
func (f *fakeRepository) EpisodesForFeed(context.Context, int64, int, int) ([]podcast.Episode, error) {
	return nil, nil
}
func (f *fakeRepository) EpisodesByState(context.Context, podcast.EpisodeState, string, int, int) ([]podcast.Episode, error) {
	return nil, nil
}
func (f *fakeRepository) QueuedEpisodes(context.Context) ([]podcast.Episode, error) {
	return f.queued, nil
}
func (f *fakeRepository) CurrentPlayingEpisode(context.Context) (podcast.Episode, error) {
	if f.nowPlaying.ID == 0 {
		return podcast.Episode{}, storage.ErrNotFound
	}
	return f.nowPlaying, nil
}
func (f *fakeRepository) NextQueuedEpisode(context.Context) (podcast.Episode, error) {
	if f.next.ID == 0 {
		return podcast.Episode{}, storage.ErrNotFound
	}
	return f.next, nil
}
func (f *fakeRepository) Episode(context.Context, int64) (podcast.Episode, error) {
	return f.episode, nil
}
func (f *fakeRepository) SetEpisodeState(_ context.Context, id int64, state podcast.EpisodeState) error {
	f.stateID = id
	f.state = state
	return nil
}
func (f *fakeRepository) MoveQueuedEpisode(context.Context, int64, int) error { return nil }
func (f *fakeRepository) ReorderQueuedEpisode(_ context.Context, id int64, target int) error {
	f.reorderID = id
	f.reorderTarget = target
	return nil
}
func (f *fakeRepository) MarkEpisodePlayed(_ context.Context, id int64) error {
	f.playedID = id
	return nil
}
func (f *fakeRepository) SavePlayback(_ context.Context, state playback.State) (bool, error) {
	f.saved = state
	return f.advanceQueue, nil
}
func (f *fakeRepository) LibraryStatistics(context.Context) (podcast.LibraryStatistics, error) {
	return f.statistics, nil
}
func (f *fakeRepository) UpdateFeedPlaybackSpeed(_ context.Context, id int64, speed float64) error {
	f.speedFeedID = id
	f.playbackSpeed = speed
	return nil
}

func newTestServer(t *testing.T, repository Repository) *Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, err := New(repository, nil, logger, Options{MaxOPMLBytes: 1024, MaxOPMLFeeds: 10})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return server
}

func TestIndexRendersAndSetsSecurityHeaders(t *testing.T) {
	position := 1
	secondPosition := 2
	server := newTestServer(t, &fakeRepository{
		nowPlaying: podcast.Episode{
			ID: 10, Title: "Currently playing", FeedTitle: "Playing podcast", ImageURL: "https://example.com/playing.jpg",
			DurationSeconds: 7200, PositionSeconds: 1800, FeedPlaybackSpeed: 1.5, Playing: true,
		},
		inbox: []podcast.Episode{{
			ID: 11, Title: "Inbox episode", FeedTitle: "Example podcast", State: podcast.EpisodeStateInbox,
			DurationSeconds: 5400, FeedPlaybackSpeed: 1.5,
		}},
		queued: []podcast.Episode{{
			ID: 12, Title: "Queued at home", FeedTitle: "Example podcast", FeedImageURL: "https://example.com/feed-cover.jpg", State: podcast.EpisodeStateQueued, QueuePosition: &position,
			DurationSeconds: 3600, PositionSeconds: 900, FeedPlaybackSpeed: 1,
		}, {
			ID: 13, Title: "Queued with episode art", FeedTitle: "Example podcast", ImageURL: "https://example.com/episode-cover.jpg", FeedImageURL: "https://example.com/unused-feed-cover.jpg", State: podcast.EpisodeStateQueued, QueuePosition: &secondPosition,
		}},
	})
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if nowPlayingPosition, inboxPosition := strings.Index(body, "Now Playing"), strings.Index(body, "Inbox · 1"); nowPlayingPosition < 0 || inboxPosition < 0 || nowPlayingPosition > inboxPosition {
		t.Fatalf("now playing is not above Inbox: %s", body)
	}
	for _, expected := range []string{"Currently playing", "Playing podcast", "playing.jpg", `/?play=11#now-playing`, `/?play=12#now-playing`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("landing page now-playing section missing %q: %s", expected, body)
		}
	}
	if strings.Contains(body, `href="/episodes/`) {
		t.Fatalf("landing page still links to the dedicated episode page: %s", body)
	}
	if !strings.Contains(body, "Inbox episode") || !strings.Contains(body, "/episodes/11/queue") || !strings.Contains(body, "/episodes/11/archive") {
		t.Fatalf("landing page inbox actions missing: %s", body)
	}
	if strings.Count(body, "Mark as played") != 3 || !strings.Contains(body, "/episodes/11/played") || !strings.Contains(body, "/episodes/12/played") {
		t.Fatalf("landing page mark-as-played actions missing: %s", body)
	}
	if inboxPosition, queuePosition := strings.Index(body, "Inbox · 1"), strings.Index(body, "Up Next · 2"); inboxPosition < 0 || queuePosition < 0 || inboxPosition > queuePosition {
		t.Fatalf("inbox is not above Up Next: %s", body)
	}
	if !strings.Contains(body, "Queued at home") || !strings.Contains(body, "/episodes/12/queue/remove") {
		t.Fatalf("landing page queue actions missing: %s", body)
	}
	for _, expected := range []string{"Play time left: 1 hour", "Play time left: 45 minutes"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("landing page play-time estimate missing %q: %s", expected, body)
		}
	}
	if strings.Count(body, "Play time left:") != 2 {
		t.Fatalf("landing page should omit play time when duration is unknown: %s", body)
	}
	for _, expected := range []string{"feed-cover.jpg", "episode-cover.jpg"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("landing page queue artwork missing %q: %s", expected, body)
		}
	}
	for _, expected := range []string{`draggable="true"`, `Drag Queued at home to reorder`, `/static/queue.js`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("landing page drag-and-drop missing %q: %s", expected, body)
		}
	}
	if strings.Contains(body, "unused-feed-cover.jpg") {
		t.Fatalf("feed artwork was used despite episode artwork: %s", body)
	}
	if strings.Contains(body, "Subscriptions") || strings.Contains(body, "Podcast feed URL") {
		t.Fatalf("library content still appears on homepage: %s", body)
	}
	if strings.Contains(body, "Play Up Next") || strings.Contains(body, "Browse recent episodes") || strings.Contains(body, "Manage library") || strings.Contains(body, "Listen on your terms") {
		t.Fatalf("landing page still contains promotional header controls: %s", body)
	}
	if strings.Contains(body, "Open Inbox") || strings.Contains(body, "Needs your attention") || strings.Contains(body, "Listening order") {
		t.Fatalf("legacy homepage headings or controls remain: %s", body)
	}
	homeNav := strings.Index(body, `href="/">Home`)
	historyNav := strings.Index(body, `href="/history">Play History`)
	recentNav := strings.Index(body, `href="/recent">Recent Published`)
	libraryNav := strings.Index(body, `href="/library">Library`)
	if homeNav < 0 || historyNav < homeNav || recentNav < historyNav || libraryNav < recentNav {
		t.Fatalf("navigation is not Home, Play History, Recent Published, Library: %s", body)
	}
	if response.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("Content-Security-Policy is missing")
	}
	if got := response.Header().Get("Referrer-Policy"); got != "same-origin" {
		t.Fatalf("Referrer-Policy = %q, want same-origin", got)
	}
	if !strings.Contains(body, `<a class="brand" href="/"><img src="/static/favicon.svg"`) {
		t.Fatalf("favicon missing from site header brand: %s", body)
	}
}

func TestIndexSelectsEpisodeIntoInlinePlayer(t *testing.T) {
	server := newTestServer(t, &fakeRepository{episode: podcast.Episode{
		ID: 12, FeedID: 7, Title: "Selected episode", FeedTitle: "Selected podcast",
		EnclosureURL: "https://example.com/selected.mp3", DurationSeconds: 3600,
		PositionSeconds: 120, FeedPlaybackSpeed: 1.25,
	}})
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/?play=12", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		"Selected episode", `id="player"`, `data-episode-id="12"`, `data-stay-on-home="true"`, `data-autoplay="true"`,
		`data-feed-id="7"`, `id="player-playback-rate"`, `value="1.25" selected`,
		`id="rewind-15"`, `id="forward-15"`, `/static/player.js`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("inline player missing %q: %s", expected, response.Body.String())
		}
	}
}

func TestIndexHidesNowPlayingEpisodeFromUpNext(t *testing.T) {
	firstPosition := 1
	secondPosition := 2
	server := newTestServer(t, &fakeRepository{
		nowPlaying: podcast.Episode{ID: 12, Title: "Playing from queue", Playing: true},
		queued: []podcast.Episode{
			{ID: 12, Title: "Playing from queue", State: podcast.EpisodeStateQueued, QueuePosition: &firstPosition},
			{ID: 13, Title: "Still up next", State: podcast.EpisodeStateQueued, QueuePosition: &secondPosition},
		},
	})
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "Up Next · 1") || !strings.Contains(body, `data-episode-id="13"`) || strings.Contains(body, `data-episode-id="12"`) {
		t.Fatalf("now-playing episode was not removed from Up Next: %s", body)
	}
}

func TestIndexShowsEmptyNowPlayingState(t *testing.T) {
	server := newTestServer(t, &fakeRepository{})
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Nothing playing") {
		t.Fatalf("empty now-playing state missing: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEpisodePlayTimeLeft(t *testing.T) {
	tests := []struct {
		name    string
		episode podcast.Episode
		want    string
	}{
		{name: "uses playback speed", episode: podcast.Episode{DurationSeconds: 5400, FeedPlaybackSpeed: 1.5}, want: "1 hour"},
		{name: "subtracts saved position", episode: podcast.Episode{DurationSeconds: 5440, PositionSeconds: 610, FeedPlaybackSpeed: 1}, want: "1 hour 21 minutes"},
		{name: "rounds up partial minute", episode: podcast.Episode{DurationSeconds: 60, PositionSeconds: 1, FeedPlaybackSpeed: 1}, want: "1 minute"},
		{name: "omits unknown duration", episode: podcast.Episode{FeedPlaybackSpeed: 1}, want: ""},
		{name: "omits invalid speed", episode: podcast.Episode{DurationSeconds: 60}, want: ""},
		{name: "omits completed episode", episode: podcast.Episode{DurationSeconds: 60, FeedPlaybackSpeed: 1, Completed: true}, want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := episodePlayTimeLeft(test.episode); got != test.want {
				t.Fatalf("episodePlayTimeLeft() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestLibraryPageRendersSubscriptionsAndManagement(t *testing.T) {
	lastScan := time.Date(2026, 11, 7, 9, 15, 0, 0, time.Local)
	latestEpisode := time.Date(2026, 11, 6, 8, 0, 0, 0, time.Local)
	server := newTestServer(t, &fakeRepository{feeds: []podcast.Feed{{
		ID: 7, Title: "Library podcast", EpisodeCount: 42,
		LastChecked: &lastScan, LastEpisodePublished: &latestEpisode,
	}}})
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/library", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		"Library podcast", "42 episodes", "Podcast feed URL", "Refresh all", "/feeds/7",
		"Last scan", "7 Nov 2026, 09:15", "Latest episode", "6 Nov 2026",
		"Import OPML", "/library/import", "Export OPML", "/library/export.opml",
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("library page missing %q: %s", expected, response.Body.String())
		}
	}
}

func TestFeedPageRendersSubscriptionPlaybackSpeed(t *testing.T) {
	server := newTestServer(t, &fakeRepository{feed: podcast.Feed{ID: 7, Title: "Fast podcast", PlaybackSpeed: 1.5}})
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/feeds/7", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{"Playback options", "Playback speed", `value="1.5" selected`, "/feeds/7/playback-options"} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("feed playback options missing %q: %s", expected, response.Body.String())
		}
	}
}

func TestUpdateFeedPlaybackSpeed(t *testing.T) {
	repository := &fakeRepository{}
	server := newTestServer(t, repository)
	request := httptest.NewRequest(http.MethodPost, "http://castdock.test/feeds/7/playback-options", strings.NewReader("playback_speed=1.75"))
	request.Header.Set("Origin", "http://castdock.test")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || repository.speedFeedID != 7 || repository.playbackSpeed != 1.75 {
		t.Fatalf("status=%d speed update=%d/%v body=%s", response.Code, repository.speedFeedID, repository.playbackSpeed, response.Body.String())
	}
}

func TestNativeAPIUpdateFeedPlaybackSpeed(t *testing.T) {
	repository := &fakeRepository{}
	server := newTestServer(t, repository)
	request := httptest.NewRequest(http.MethodPost, "http://castdock.test/api/feeds/7/playback-options", strings.NewReader(`{"playbackSpeed":1.75}`))
	request.Header.Set("X-CastDock-Client", "ios")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || repository.speedFeedID != 7 || repository.playbackSpeed != 1.75 {
		t.Fatalf("status=%d speed update=%d/%v body=%s", response.Code, repository.speedFeedID, repository.playbackSpeed, response.Body.String())
	}
}

func TestNativeAPIUpdateFeedPlaybackSpeedRejectsInvalidPayload(t *testing.T) {
	for _, body := range []string{`{}`, `{"playbackSpeed":3.5}`, `{"playbackSpeed":1.5,"unexpected":true}`} {
		t.Run(body, func(t *testing.T) {
			server := newTestServer(t, &fakeRepository{})
			request := httptest.NewRequest(http.MethodPost, "http://castdock.test/api/feeds/7/playback-options", strings.NewReader(body))
			request.Header.Set("X-CastDock-Client", "ios")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want %d, body=%s", response.Code, http.StatusBadRequest, response.Body.String())
			}
		})
	}
}

func TestRecentPageRendersEpisodeDetailsAndStatus(t *testing.T) {
	published := time.Date(2026, 11, 7, 8, 0, 0, 0, time.UTC)
	server := newTestServer(t, &fakeRepository{recent: []podcast.Episode{{
		ID: 21, Title: "Detailed episode", FeedTitle: "Example podcast",
		Description: "These are the show notes.", DescriptionHTML: `<p>These are the <strong>show notes</strong>.</p><script>bad()</script>`, ImageURL: "https://example.com/episode.jpg",
		PublishedAt: &published, State: podcast.EpisodeStateQueued, Completed: true,
	}}})
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/recent", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		"Detailed episode", "Example podcast", "<strong>show notes</strong>",
		"episode.jpg", "queued", "Played", "Remove from Up Next",
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("recent page missing %q: %s", expected, response.Body.String())
		}
	}
	if strings.Contains(response.Body.String(), "<script") || strings.Contains(response.Body.String(), "bad()") {
		t.Fatalf("recent page rendered unsafe show notes: %s", response.Body.String())
	}
}

func TestSharedHomeAPIUsesCanonicalEpisodeState(t *testing.T) {
	position := 1
	repository := &fakeRepository{
		inbox: []podcast.Episode{{
			ID: 31, FeedID: 4, FeedTitle: "Inbox podcast", FeedImageURL: "https://example.com/feed.jpg",
			Title: "Inbox API episode", DescriptionHTML: `<ul><li>First</li></ul><p><a href="https://example.com">Details</a></p><script>bad()</script>`, State: podcast.EpisodeStateInbox, FeedPlaybackSpeed: 1.5,
		}},
		queued: []podcast.Episode{{
			ID: 32, FeedID: 5, FeedTitle: "Queue podcast", ImageURL: "https://example.com/episode.jpg",
			Title: "Queued API episode", State: podcast.EpisodeStateQueued, QueuePosition: &position,
			EnclosureURL: "https://example.com/audio.mp3", PositionSeconds: 42, FeedPlaybackSpeed: 1.25,
		}},
	}
	server := newTestServer(t, repository)
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/api/home", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status=%d content-type=%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	for _, expected := range []string{`"inbox"`, `"upNext"`, `"Inbox API episode"`, `"imageUrl":"https://example.com/feed.jpg"`, `"playbackSpeed":1.5`, `"queuePosition":1`, `"positionSeconds":42`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("home API missing %q: %s", expected, response.Body.String())
		}
	}
	var home apiHomeResponse
	if err := json.Unmarshal(response.Body.Bytes(), &home); err != nil {
		t.Fatalf("decode home API response: %v", err)
	}
	if got := home.Inbox[0].DescriptionHTML; !strings.Contains(got, "<ul><li>First</li></ul>") || !strings.Contains(got, `href="https://example.com"`) || strings.Contains(got, "<script") || strings.Contains(got, "bad()") {
		t.Fatalf("descriptionHtml was not safely preserved: %q", got)
	}
}

func TestNativeAPIWriteUsesClientHeaderWithoutBrowserOrigin(t *testing.T) {
	repository := &fakeRepository{}
	server := newTestServer(t, repository)
	request := httptest.NewRequest(http.MethodPost, "http://castdock.test/api/episodes/42/queue", nil)
	request.Header.Set("X-CastDock-Client", "ios")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || repository.stateID != 42 || repository.state != podcast.EpisodeStateQueued {
		t.Fatalf("status=%d state=%d/%s body=%s", response.Code, repository.stateID, repository.state, response.Body.String())
	}
}

func TestEpisodePageVisuallyBoundsFormattedShowNotes(t *testing.T) {
	server := newTestServer(t, &fakeRepository{
		feed: podcast.Feed{ID: 4, Title: "Example podcast", PlaybackSpeed: 1.5},
		episode: podcast.Episode{
			ID: 23, FeedID: 4, Title: "Formatted episode", State: podcast.EpisodeStateArchived,
			DescriptionHTML: `<p>Start <strong>formatted</strong> end.</p>`, EnclosureURL: "https://example.com/episode.mp3",
		},
	})
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/episodes/23", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		`class="show-notes-panel"`, `id="show-notes-heading"`, `>Show notes</h2>`, `<strong>formatted</strong>`,
		`id="play-time-left"`, `id="rewind-15"`, `Go back 15 seconds`, `id="forward-15"`, `Skip forward 15 seconds`,
		`data-playback-rate="1.5"`, `data-feed-id="4"`, `id="player-playback-rate"`, `/static/player.js`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("episode page show-notes boundary missing %q: %s", expected, response.Body.String())
		}
	}
}

func TestPlayerScriptSupportsPlaybackShortcutsAndTimeLeft(t *testing.T) {
	script, err := webassets.Files.ReadFile("static/player.js")
	if err != nil {
		t.Fatalf("read player script: %v", err)
	}
	contents := string(script)
	for _, expected := range []string{
		"seekBy(-15)", "seekBy(15)", "Math.max(0", "Math.min(upperBound", "player.playbackRate = configuredPlaybackRate",
		"Play time left:", "duration - currentTime", "/ playbackRate", `"timeupdate", "durationchange"`, `"ratechange"`,
		"stayOnHome", "?play=", "#now-playing", "shouldAutoplay", "player.play()",
		"player-playback-rate", `/api/feeds/${feedID}/playback-options`, "playbackSpeed", "confirmedPlaybackRate",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("player script missing %q", expected)
		}
	}
	if strings.Contains(contents, "Playback position saved.") {
		t.Fatal("player script still renders the playback-saved success string")
	}
}

func TestPlayHistoryRendersLatestTwentyWithRelativeTimestamp(t *testing.T) {
	playedAt := time.Now().Add(-90 * time.Minute)
	repository := &fakeRepository{history: []podcast.Episode{{
		ID: 22, Title: "Recently played", FeedTitle: "Example podcast",
		Description: "History show notes", State: podcast.EpisodeStateArchived,
		PlaybackUpdated: &playedAt, FeedImageURL: "https://example.com/feed.jpg",
	}}}
	server := newTestServer(t, repository)
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/history", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || repository.historyLimit != 20 {
		t.Fatalf("status = %d, requested limit = %d", response.Code, repository.historyLimit)
	}
	for _, expected := range []string{"Play History", "Recently played", "History show notes", "Finished playing:", "one hour ago", "feed.jpg"} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("history page missing %q: %s", expected, response.Body.String())
		}
	}
}

func TestSystemPageRendersAllowListedConfigurationAndStatistics(t *testing.T) {
	startedAt := time.Date(2026, 11, 7, 8, 0, 0, 0, time.Local)
	latestScan := time.Date(2026, 11, 7, 9, 0, 0, 0, time.Local)
	repository := &fakeRepository{statistics: podcast.LibraryStatistics{
		FeedCount: 12, EpisodeCount: 345, InboxCount: 4, QueuedCount: 5,
		ArchivedCount: 336, CompletedCount: 120, InProgressCount: 3,
		DatabaseBytes: 2 << 20, LatestFeedScan: &latestScan,
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, err := New(repository, nil, logger, Options{MaxOPMLBytes: 1024, MaxOPMLFeeds: 10, SystemConfig: SystemConfiguration{
		StartedAt: startedAt, ListenAddress: "0.0.0.0:8080", DatabasePath: "./data/castdock.db",
		RefreshInterval: time.Hour, RefreshConcurrency: 4, HTTPTimeout: 20 * time.Second,
		MaxFeedBytes: 32 << 20, MaxOPMLBytes: 5 << 20, MaxOPMLFeeds: 500,
		LogLevel: "DEBUG", AccessLogEnabled: true, AuthenticationEnabled: true, SessionTTL: 30 * 24 * time.Hour,
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/system", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		"System", "Read only", "Subscriptions", "<dd>12</dd>", "Total episodes", "<dd>345</dd>",
		"2.0 MiB", "0.0.0.0:8080", "./data/castdock.db", "32.0 MiB", "DEBUG", "Authentication enabled", "720h0m0s",
		"Last network events", "No client network events recorded since startup.",
		"network-events-status", "Connecting", "/static/system-events.js",
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("system page missing %q: %s", expected, response.Body.String())
		}
	}
}

func TestSystemPageRendersRecentClientNetworkEventsNewestFirst(t *testing.T) {
	server := newTestServer(t, &fakeRepository{})

	healthRequest := httptest.NewRequest(http.MethodGet, "http://castdock.test/healthz", nil)
	healthRequest.Header.Set("X-CastDock-Client", "ios")
	server.Handler().ServeHTTP(httptest.NewRecorder(), healthRequest)

	body := `{"position":12.5,"duration":60,"completed":false,"playing":true,"clientUpdatedAt":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}`
	playbackRequest := httptest.NewRequest(http.MethodPost, "http://castdock.test/api/episodes/42/playback", strings.NewReader(body))
	playbackRequest.Header.Set("X-CastDock-Client", "ios")
	server.Handler().ServeHTTP(httptest.NewRecorder(), playbackRequest)

	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/system", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	bodyText := response.Body.String()
	for _, expected := range []string{
		"Playback timestamp saved", "POST /api/episodes/42/playback", "Health checked", "GET /healthz", "ios", "just now", "HTTP 200",
	} {
		if !strings.Contains(bodyText, expected) {
			t.Fatalf("system page missing network event %q: %s", expected, bodyText)
		}
	}
	if strings.Index(bodyText, "Playback timestamp saved") > strings.Index(bodyText, "Health checked") {
		t.Fatalf("network events are not newest first: %s", bodyText)
	}
}

func TestNetworkEventStoreRetainsLatestFiftyEvents(t *testing.T) {
	store := &networkEventStore{}
	for index := 0; index <= networkEventLimit; index++ {
		store.record(networkEvent{Status: index})
	}

	events := store.recent()
	if len(events) != networkEventLimit {
		t.Fatalf("event count = %d, want %d", len(events), networkEventLimit)
	}
	if events[0].Status != networkEventLimit || events[len(events)-1].Status != 1 {
		t.Fatalf("retained statuses run from %d to %d, want %d to 1", events[0].Status, events[len(events)-1].Status, networkEventLimit)
	}
}

func TestNetworkEventStoreNotifiesSubscribers(t *testing.T) {
	store := &networkEventStore{}
	updates, unsubscribe := store.subscribe()
	defer unsubscribe()
	store.record(networkEvent{Description: "Home synchronized"})
	select {
	case <-updates:
	default:
		t.Fatal("subscriber was not notified")
	}
}

type cancelOnFlushRecorder struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (r *cancelOnFlushRecorder) Flush() {
	r.ResponseRecorder.Flush()
	r.cancel()
}

func TestNetworkEventStreamSendsInitialSnapshot(t *testing.T) {
	server := newTestServer(t, &fakeRepository{})
	server.networkEvents.record(networkEvent{
		OccurredAt: time.Date(2026, 11, 12, 8, 0, 0, 0, time.UTC),
		Client:     "ios", Description: "Home synchronized", Method: http.MethodGet,
		Path: "/api/home", Status: http.StatusOK,
	})
	ctx, cancel := context.WithCancel(context.Background())
	response := &cancelOnFlushRecorder{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/system/network-events/stream", nil).WithContext(ctx)
	server.Handler().ServeHTTP(response, request)

	if got := response.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	for _, expected := range []string{`event: network-events`, `"description":"Home synchronized"`, `"path":"/api/home"`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("stream missing %q: %s", expected, response.Body.String())
		}
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		at   *time.Time
		want string
	}{
		{name: "unknown", want: "Unknown"},
		{name: "one hour", at: timePointer(now.Add(-time.Hour)), want: "one hour ago"},
		{name: "two days", at: timePointer(now.Add(-48 * time.Hour)), want: "2 days ago"},
		{name: "old date", at: timePointer(now.Add(-31 * 24 * time.Hour)), want: "7 Oct 2026"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := relativeTime(test.at, now); got != test.want {
				t.Fatalf("relativeTime() = %q, want %q", got, test.want)
			}
		})
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func TestMiddlewareRejectsCrossOriginWrite(t *testing.T) {
	server := newTestServer(t, &fakeRepository{})
	request := httptest.NewRequest(http.MethodPost, "http://castdock.test/refresh", nil)
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestMiddlewareRejectsCrossSiteFetchMetadata(t *testing.T) {
	server := newTestServer(t, &fakeRepository{})
	request := httptest.NewRequest(http.MethodPost, "http://localhost:8080/import", nil)
	request.Header.Set("Origin", "http://localhost:8080")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestMiddlewareAcceptsSameOriginFetchMetadataWithoutOriginHeaders(t *testing.T) {
	server := newTestServer(t, &fakeRepository{})
	request := httptest.NewRequest(http.MethodPost, "http://localhost:8080/import", nil)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || !strings.HasPrefix(response.Header().Get("Location"), "/library?") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestMiddlewareAcceptsSameSiteWhenExactOriginMatches(t *testing.T) {
	server := newTestServer(t, &fakeRepository{})
	request := httptest.NewRequest(http.MethodPost, "http://services.hake-cod.ts.net:7007/library/import", nil)
	request.Header.Set("Origin", "http://services.hake-cod.ts.net:7007")
	request.Header.Set("Sec-Fetch-Site", "same-site")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestMiddlewareAcceptsExactSameOriginRefererFallback(t *testing.T) {
	server := newTestServer(t, &fakeRepository{})
	request := httptest.NewRequest(http.MethodPost, "http://services.hake-cod.ts.net:7007/library/import", nil)
	request.Header.Set("Referer", "http://services.hake-cod.ts.net:7007/library")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestMiddlewareRejectsSameSiteWhenOriginHostDiffers(t *testing.T) {
	server := newTestServer(t, &fakeRepository{})
	request := httptest.NewRequest(http.MethodPost, "http://services.hake-cod.ts.net:7007/library/import", nil)
	request.Header.Set("Origin", "http://attacker.hake-cod.ts.net:7007")
	request.Header.Set("Sec-Fetch-Site", "same-site")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestLegacyInboxAndImportPagesRedirect(t *testing.T) {
	server := newTestServer(t, &fakeRepository{})
	tests := []struct {
		path     string
		location string
	}{
		{path: "/inbox", location: "/#inbox"},
		{path: "/import", location: "/library#opml"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodGet, "http://castdock.test"+test.path, nil)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusPermanentRedirect || response.Header().Get("Location") != test.location {
			t.Errorf("GET %s: status=%d location=%q", test.path, response.Code, response.Header().Get("Location"))
		}
	}
}

func TestExportOPMLDownloadsAllSubscriptions(t *testing.T) {
	server := newTestServer(t, &fakeRepository{feeds: []podcast.Feed{
		{Title: "First & Best", URL: "https://example.com/one.xml", SiteURL: "https://example.com/one"},
		{Title: "Second", URL: "https://example.com/two.xml"},
	}})
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/library/export.opml", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/x-opml; charset=utf-8" {
		t.Fatalf("status=%d content-type=%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	if !strings.Contains(response.Header().Get("Content-Disposition"), "attachment; filename=\"castdock-subscriptions-") {
		t.Fatalf("Content-Disposition = %q", response.Header().Get("Content-Disposition"))
	}
	urls, err := syndication.ParseOPML(strings.NewReader(response.Body.String()))
	if err != nil || len(urls) != 2 || urls[0] != "https://example.com/one.xml" || urls[1] != "https://example.com/two.xml" {
		t.Fatalf("exported OPML URLs = %#v, error = %v", urls, err)
	}
}

func TestSavePlayback(t *testing.T) {
	repository := &fakeRepository{}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	server, err := New(repository, nil, logger, Options{MaxOPMLBytes: 1024, MaxOPMLFeeds: 10})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	body := `{"position":12.5,"duration":60,"completed":false,"playing":true,"clientUpdatedAt":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}`
	request := httptest.NewRequest(http.MethodPost, "http://castdock.test/api/episodes/42/playback", strings.NewReader(body))
	request.Header.Set("Origin", "http://castdock.test")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repository.saved.EpisodeID != 42 || repository.saved.Position != 12.5 || !repository.saved.Playing {
		t.Fatalf("saved state = %+v", repository.saved)
	}
	if !strings.Contains(logs.String(), `"msg":"playback position saved"`) || !strings.Contains(logs.String(), `"episode_id":42`) {
		t.Fatalf("playback log missing event fields: %s", logs.String())
	}
	if strings.Contains(logs.String(), `"msg":"http request"`) {
		t.Fatalf("access log emitted while disabled: %s", logs.String())
	}
}

func TestAccessLogCanBeEnabledIndependently(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	server, err := New(&fakeRepository{}, nil, logger, Options{MaxOPMLBytes: 1024, MaxOPMLFeeds: 10, AccessLog: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	for _, expected := range []string{`msg="http request"`, `method=GET`, `path=/`, `status=200`, `response_bytes=`} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("access log missing %q: %s", expected, logs.String())
		}
	}
}

func TestOptionalAuthenticationLoginSessionAndLogout(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authentication := authn.New("listener", "correct horse battery staple", time.Hour)
	server, err := New(&fakeRepository{}, nil, logger, Options{
		MaxOPMLBytes: 1024, MaxOPMLFeeds: 10, Authentication: authentication,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	protectedRequest := httptest.NewRequest(http.MethodGet, "http://castdock.test/", nil)
	protectedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(protectedResponse, protectedRequest)
	if protectedResponse.Code != http.StatusSeeOther || !strings.HasPrefix(protectedResponse.Header().Get("Location"), "/login?next=") {
		t.Fatalf("protected response: status=%d location=%q", protectedResponse.Code, protectedResponse.Header().Get("Location"))
	}

	loginPageRequest := httptest.NewRequest(http.MethodGet, "http://castdock.test/login?next=%2Fsystem", nil)
	loginPageResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(loginPageResponse, loginPageRequest)
	if loginPageResponse.Code != http.StatusOK || !strings.Contains(loginPageResponse.Body.String(), `name="username"`) || !strings.Contains(loginPageResponse.Body.String(), `name="password"`) {
		t.Fatalf("login page: status=%d body=%s", loginPageResponse.Code, loginPageResponse.Body.String())
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "http://castdock.test/login", strings.NewReader("username=listener&password=correct+horse+battery+staple&next=%2Fsystem"))
	loginRequest.Header.Set("Origin", "http://castdock.test")
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusSeeOther || loginResponse.Header().Get("Location") != "/system" {
		t.Fatalf("login response: status=%d location=%q body=%s", loginResponse.Code, loginResponse.Header().Get("Location"), loginResponse.Body.String())
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookies = %#v", cookies)
	}

	authenticatedRequest := httptest.NewRequest(http.MethodGet, "http://castdock.test/", nil)
	authenticatedRequest.AddCookie(cookies[0])
	authenticatedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(authenticatedResponse, authenticatedRequest)
	if authenticatedResponse.Code != http.StatusOK || !strings.Contains(authenticatedResponse.Body.String(), "Sign out") {
		t.Fatalf("authenticated response: status=%d body=%s", authenticatedResponse.Code, authenticatedResponse.Body.String())
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "http://castdock.test/logout", nil)
	logoutRequest.Header.Set("Origin", "http://castdock.test")
	logoutRequest.AddCookie(cookies[0])
	logoutResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(logoutResponse, logoutRequest)
	valid, validationErr := authentication.Valid(context.Background(), cookies[0].Value)
	if logoutResponse.Code != http.StatusSeeOther || logoutResponse.Header().Get("Location") != "/login" || validationErr != nil || valid {
		t.Fatalf("logout response: status=%d location=%q", logoutResponse.Code, logoutResponse.Header().Get("Location"))
	}
}

func TestAuthenticationLeavesHealthPublicAndProtectsAPI(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, err := New(&fakeRepository{}, nil, logger, Options{
		MaxOPMLBytes: 1024, MaxOPMLFeeds: 10,
		Authentication: authn.New("listener", "password", time.Hour),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	healthRequest := httptest.NewRequest(http.MethodGet, "http://castdock.test/healthz", nil)
	healthResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(healthResponse, healthRequest)
	if healthResponse.Code != http.StatusOK {
		t.Fatalf("health status = %d", healthResponse.Code)
	}
	apiRequest := httptest.NewRequest(http.MethodPost, "http://castdock.test/api/queue/reorder", strings.NewReader(`{"episodeId":1,"targetPosition":1}`))
	apiRequest.Header.Set("Origin", "http://castdock.test")
	apiResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(apiResponse, apiRequest)
	if apiResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated API status = %d", apiResponse.Code)
	}
	streamRequest := httptest.NewRequest(http.MethodGet, "http://castdock.test/system/network-events/stream", nil)
	streamResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(streamResponse, streamRequest)
	if streamResponse.Code != http.StatusSeeOther || !strings.HasPrefix(streamResponse.Header().Get("Location"), "/login?") {
		t.Fatalf("unauthenticated stream status=%d location=%q", streamResponse.Code, streamResponse.Header().Get("Location"))
	}
}

func TestNativeAPISessionCreatesAuthenticatedCookie(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authentication := authn.New("listener", "password", time.Hour)
	server, err := New(&fakeRepository{}, nil, logger, Options{
		MaxOPMLBytes: 1024, MaxOPMLFeeds: 10, Authentication: authentication,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://castdock.test/api/session", strings.NewReader(`{"username":"listener","password":"password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CastDock-Client", "ios")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	valid, validationErr := authentication.Valid(context.Background(), cookies[0].Value)
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || validationErr != nil || !valid {
		t.Fatalf("API session cookies = %#v", cookies)
	}
}

func TestCompletedQueuedPlaybackReturnsNextEpisode(t *testing.T) {
	repository := &fakeRepository{advanceQueue: true, next: podcast.Episode{ID: 99}}
	server := newTestServer(t, repository)
	body := `{"position":60,"duration":60,"completed":true,"playing":false,"clientUpdatedAt":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}`
	request := httptest.NewRequest(http.MethodPost, "http://castdock.test/api/episodes/42/playback", strings.NewReader(body))
	request.Header.Set("Origin", "http://castdock.test")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"nextEpisodeUrl":"/episodes/99"`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestQueueEpisodeTransitionsStateAndRedirectsLocally(t *testing.T) {
	repository := &fakeRepository{}
	server := newTestServer(t, repository)
	request := httptest.NewRequest(http.MethodPost, "http://castdock.test/episodes/42/queue", strings.NewReader("return_to=%2F"))
	request.Header.Set("Origin", "http://castdock.test")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || repository.stateID != 42 || repository.state != podcast.EpisodeStateQueued {
		t.Fatalf("status = %d, state = %d/%s", response.Code, repository.stateID, repository.state)
	}
	if location := response.Header().Get("Location"); !strings.HasPrefix(location, "/?") {
		t.Fatalf("Location = %q", location)
	}
}

func TestMarkEpisodePlayedRedirectsToHomepage(t *testing.T) {
	repository := &fakeRepository{}
	server := newTestServer(t, repository)
	request := httptest.NewRequest(http.MethodPost, "http://castdock.test/episodes/42/played", strings.NewReader("return_to=%2F"))
	request.Header.Set("Origin", "http://castdock.test")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || repository.playedID != 42 || !strings.HasPrefix(response.Header().Get("Location"), "/?") {
		t.Fatalf("status=%d playedID=%d location=%q", response.Code, repository.playedID, response.Header().Get("Location"))
	}
}

func TestSafeReturnToRejectsExternalURL(t *testing.T) {
	if got := safeReturnTo("https://attacker.example/"); got != "/" {
		t.Fatalf("safeReturnTo() = %q", got)
	}
}

func TestLegacyUpNextRedirectsToHomepageQueue(t *testing.T) {
	server := newTestServer(t, &fakeRepository{})
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/up-next", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusPermanentRedirect || response.Header().Get("Location") != "/#up-next" {
		t.Fatalf("status = %d, location = %q", response.Code, response.Header().Get("Location"))
	}
}

func TestReorderQueueEndpoint(t *testing.T) {
	repository := &fakeRepository{}
	server := newTestServer(t, repository)
	request := httptest.NewRequest(http.MethodPost, "http://castdock.test/api/queue/reorder", strings.NewReader(`{"episodeId":42,"targetPosition":3}`))
	request.Header.Set("Origin", "http://castdock.test")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || repository.reorderID != 42 || repository.reorderTarget != 3 {
		t.Fatalf("status = %d, reorder = %d/%d, body = %s", response.Code, repository.reorderID, repository.reorderTarget, response.Body.String())
	}
}

func TestSwaggerUIAndOpenAPIDocumentAreServed(t *testing.T) {
	server := newTestServer(t, &fakeRepository{})
	specRequest := httptest.NewRequest(http.MethodGet, "http://castdock.test/api/openapi.yaml", nil)
	specResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(specResponse, specRequest)
	if specResponse.Code != http.StatusOK || !strings.Contains(specResponse.Body.String(), "openapi: 3.0.3") {
		t.Fatalf("OpenAPI response: status=%d body=%s", specResponse.Code, specResponse.Body.String())
	}
	uiRequest := httptest.NewRequest(http.MethodGet, "http://castdock.test/swagger/index.html", nil)
	uiResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(uiResponse, uiRequest)
	if uiResponse.Code != http.StatusOK || !strings.Contains(uiResponse.Body.String(), "openapi.yaml") {
		t.Fatalf("Swagger UI response: status=%d body=%s", uiResponse.Code, uiResponse.Body.String())
	}
	if !strings.Contains(uiResponse.Header().Get("Content-Security-Policy"), "'unsafe-inline'") {
		t.Fatalf("Swagger UI CSP = %q", uiResponse.Header().Get("Content-Security-Policy"))
	}
}

func TestQueueScriptReloadsAfterPersistedReorder(t *testing.T) {
	script, err := webassets.Files.ReadFile("static/queue.js")
	if err != nil {
		t.Fatalf("read queue script: %v", err)
	}
	contents := string(script)
	for _, expected := range []string{`window.location.reload()`, `window.sessionStorage.setItem(scrollKey, "true")`, `queue.scrollIntoView`} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("queue script missing %q", expected)
		}
	}
	if strings.Contains(contents, `window.location.assign("/#up-next")`) {
		t.Fatal("queue script still uses hash-only navigation after reorder")
	}
}
