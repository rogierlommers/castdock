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
)

type fakeRepository struct {
	saved playback.State
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
func (f *fakeRepository) Episode(context.Context, int64) (podcast.Episode, error) {
	return podcast.Episode{}, nil
}
func (f *fakeRepository) SavePlayback(_ context.Context, state playback.State) error {
	f.saved = state
	return nil
}

func newTestServer(t *testing.T, repository Repository) *Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, err := New(repository, nil, logger, 1024, 10)
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
	server, err := New(repository, nil, logger, 1024, 10)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	body := `{"position":12.5,"duration":60,"completed":false,"playing":true,"clientUpdatedAt":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}`
	request := httptest.NewRequest(http.MethodPost, "http://castdock.test/api/episodes/42/playback", strings.NewReader(body))
	request.Header.Set("Origin", "http://castdock.test")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repository.saved.EpisodeID != 42 || repository.saved.Position != 12.5 || !repository.saved.Playing {
		t.Fatalf("saved state = %+v", repository.saved)
	}
	if !strings.Contains(logs.String(), `"msg":"playback position saved"`) || !strings.Contains(logs.String(), `"episode_id":42`) {
		t.Fatalf("playback log missing event fields: %s", logs.String())
	}
}
