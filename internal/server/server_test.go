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
)

type fakeRepository struct {
	saved        playback.State
	advanceQueue bool
	next         podcast.Episode
	stateID      int64
	state        podcast.EpisodeState
	queued       []podcast.Episode
}

func (f *fakeRepository) Health(context.Context) error                      { return nil }
func (f *fakeRepository) ListFeeds(context.Context) ([]podcast.Feed, error) { return nil, nil }
func (f *fakeRepository) RecentEpisodes(context.Context, int) ([]podcast.Episode, error) {
	return nil, nil
}
func (f *fakeRepository) Feed(context.Context, int64) (podcast.Feed, error) {
	return podcast.Feed{}, nil
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
	return podcast.Episode{}, nil
}
func (f *fakeRepository) SetEpisodeState(_ context.Context, id int64, state podcast.EpisodeState) error {
	f.stateID = id
	f.state = state
	return nil
}
func (f *fakeRepository) MoveQueuedEpisode(context.Context, int64, int) error { return nil }
func (f *fakeRepository) SavePlayback(_ context.Context, state playback.State) (bool, error) {
	f.saved = state
	return f.advanceQueue, nil
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
	server := newTestServer(t, &fakeRepository{})
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "No subscriptions yet") {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
	if response.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("Content-Security-Policy is missing")
	}
}

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
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
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
	request := httptest.NewRequest(http.MethodPost, "http://castdock.test/episodes/42/queue", strings.NewReader("return_to=%2Finbox"))
	request.Header.Set("Origin", "http://castdock.test")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || repository.stateID != 42 || repository.state != podcast.EpisodeStateQueued {
		t.Fatalf("status = %d, state = %d/%s", response.Code, repository.stateID, repository.state)
	}
	if location := response.Header().Get("Location"); !strings.HasPrefix(location, "/inbox?") {
		t.Fatalf("Location = %q", location)
	}
}

func TestSafeReturnToRejectsExternalURL(t *testing.T) {
	if got := safeReturnTo("https://attacker.example/"); got != "/" {
		t.Fatalf("safeReturnTo() = %q", got)
	}
}

func TestUpNextRendersQueuePositionAndActions(t *testing.T) {
	position := 1
	server := newTestServer(t, &fakeRepository{queued: []podcast.Episode{{
		ID: 8, Title: "Queued episode", FeedTitle: "Example podcast",
		State: podcast.EpisodeStateQueued, QueuePosition: &position,
	}}})
	request := httptest.NewRequest(http.MethodGet, "http://castdock.test/up-next", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{"Queued episode", "Example podcast", "Move Queued episode up", ">1</span>"} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("queue page missing %q: %s", expected, response.Body.String())
		}
	}
}
