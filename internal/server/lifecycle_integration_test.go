package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rlommers/castdock/internal/podcast"
	"github.com/rlommers/castdock/internal/storage"
)

func TestInboxArchiveAndQueueWorkflow(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "castdock.db"), logger)
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	feed, _, err := store.AddFeed(ctx, "https://example.com/feed.xml")
	if err != nil {
		t.Fatalf("AddFeed() error = %v", err)
	}
	if err := store.SaveParsedFeed(ctx, feed.ID, podcast.ParsedFeed{Title: "Example podcast", Episodes: []podcast.ParsedEpisode{{GUID: "old", Title: "Old episode"}}}, true); err != nil {
		t.Fatalf("initial SaveParsedFeed() error = %v", err)
	}
	if err := store.SaveParsedFeed(ctx, feed.ID, podcast.ParsedFeed{Title: "Example podcast", Episodes: []podcast.ParsedEpisode{{GUID: "old", Title: "Old episode"}, {GUID: "new", Title: "New episode"}}}, false); err != nil {
		t.Fatalf("later SaveParsedFeed() error = %v", err)
	}
	server, err := New(store, nil, logger, Options{MaxOPMLBytes: 1024, MaxOPMLFeeds: 10})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	inbox := performRequest(server, http.MethodGet, "http://castdock.test/", "", "")
	if inbox.Code != http.StatusOK || !strings.Contains(inbox.Body.String(), "New episode") || strings.Contains(inbox.Body.String(), "Old episode") {
		t.Fatalf("unexpected inbox: status=%d body=%s", inbox.Code, inbox.Body.String())
	}
	archive := performRequest(server, http.MethodGet, "http://castdock.test/archive?q=Old", "", "")
	if archive.Code != http.StatusOK || !strings.Contains(archive.Body.String(), "Old episode") {
		t.Fatalf("unexpected archive: status=%d body=%s", archive.Code, archive.Body.String())
	}
	episodes, err := store.EpisodesByState(ctx, podcast.EpisodeStateInbox, "", 10, 0)
	if err != nil || len(episodes) != 1 {
		t.Fatalf("EpisodesByState() = %+v, %v", episodes, err)
	}
	queue := performRequest(server, http.MethodPost, "http://castdock.test/episodes/"+strconv.FormatInt(episodes[0].ID, 10)+"/queue", "return_to=%2Finbox", "http://castdock.test")
	if queue.Code != http.StatusSeeOther {
		t.Fatalf("queue status=%d body=%s", queue.Code, queue.Body.String())
	}
	upNext := performRequest(server, http.MethodGet, "http://castdock.test/", "", "")
	if upNext.Code != http.StatusOK || !strings.Contains(upNext.Body.String(), "New episode") {
		t.Fatalf("unexpected queue: status=%d body=%s", upNext.Code, upNext.Body.String())
	}
}

func performRequest(server *Server, method, target, body, origin string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}
