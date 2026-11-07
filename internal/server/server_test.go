package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	server, err := New(repository, nil, logger, 1024, 10, false)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return server
}

func TestIndexRendersAndSetsSecurityHeaders(t *testing.T) {
	position := 1
	secondPosition := 2
	server := newTestServer(t, &fakeRepository{
		inbox: []podcast.Episode{{
			ID: 11, Title: "Inbox episode", FeedTitle: "Example podcast", State: podcast.EpisodeStateInbox,
		}},
		queued: []podcast.Episode{{
			ID: 12, Title: "Queued at home", FeedTitle: "Example podcast", FeedImageURL: "https://example.com/feed-cover.jpg", State: podcast.EpisodeStateQueued, QueuePosition: &position,
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
	if !strings.Contains(body, `href="https://github.com/rogierlommers/castdock"`) || !strings.Contains(body, "View on GitHub") {
		t.Fatalf("GitHub repository link missing from layout: %s", body)
	}
	if !strings.Contains(body, `class="hero-github-link"`) {
		t.Fatalf("GitHub repository link missing from homepage introduction: %s", body)
	}
	if !strings.Contains(body, `<a class="brand" href="/"><img src="/static/favicon.svg"`) {
		t.Fatalf("favicon missing from site header brand: %s", body)
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
		`id="rewind-15"`, `Go back 15 seconds`, `id="forward-15"`, `Skip forward 15 seconds`, `data-playback-rate="1.5"`, `/static/player.js`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("episode page show-notes boundary missing %q: %s", expected, response.Body.String())
		}
	}
}

func TestPlayerScriptSeeksFifteenSecondsWithBounds(t *testing.T) {
	script, err := webassets.Files.ReadFile("static/player.js")
	if err != nil {
		t.Fatalf("read player script: %v", err)
	}
	contents := string(script)
	for _, expected := range []string{"seekBy(-15)", "seekBy(15)", "Math.max(0", "Math.min(upperBound", "player.playbackRate = configuredPlaybackRate"} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("player script missing %q", expected)
		}
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
	server, err := New(repository, nil, logger, 1024, 10, false, SystemConfiguration{
		StartedAt: startedAt, ListenAddress: "0.0.0.0:8080", DatabasePath: "./data/castdock.db",
		RefreshInterval: time.Hour, RefreshConcurrency: 4, HTTPTimeout: 20 * time.Second,
		MaxFeedBytes: 32 << 20, MaxOPMLBytes: 5 << 20, MaxOPMLFeeds: 500,
		LogLevel: "DEBUG", AccessLogEnabled: true,
	})
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
		"2.0 MiB", "0.0.0.0:8080", "./data/castdock.db", "32.0 MiB", "DEBUG", "true",
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("system page missing %q: %s", expected, response.Body.String())
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
	server, err := New(repository, nil, logger, 1024, 10, false)
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
	server, err := New(&fakeRepository{}, nil, logger, 1024, 10, true)
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
