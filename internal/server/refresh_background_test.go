package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rlommers/castdock/internal/podcast"
	"github.com/rlommers/castdock/internal/syndication"
)

type backgroundRefreshRepository struct {
	feed  podcast.Feed
	saved chan struct{}
	once  sync.Once
}

func (r *backgroundRefreshRepository) AddFeed(context.Context, string) (podcast.Feed, bool, error) {
	return r.feed, true, nil
}
func (r *backgroundRefreshRepository) Feed(context.Context, int64) (podcast.Feed, error) {
	return r.feed, nil
}
func (r *backgroundRefreshRepository) ListFeeds(context.Context) ([]podcast.Feed, error) {
	return []podcast.Feed{r.feed}, nil
}
func (r *backgroundRefreshRepository) SaveParsedFeed(context.Context, int64, podcast.ParsedFeed, bool) error {
	r.once.Do(func() { close(r.saved) })
	return nil
}
func (r *backgroundRefreshRepository) MarkFeedError(context.Context, int64, error) error {
	return nil
}
func (r *backgroundRefreshRepository) MarkFeedNotModified(context.Context, int64, string, string) error {
	r.once.Do(func() { close(r.saved) })
	return nil
}

func TestRefreshAllContinuesAfterRequestCancellation(t *testing.T) {
	requestReachedFeed := make(chan struct{})
	releaseFeed := make(chan struct{})
	var requestOnce sync.Once
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestOnce.Do(func() { close(requestReachedFeed) })
		<-releaseFeed
		io.WriteString(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Background feed</title><link>https://example.com</link><description>Example</description></channel></rss>`)
	}))
	defer feedServer.Close()

	refreshRepository := &backgroundRefreshRepository{
		feed:  podcast.Feed{ID: 1, URL: feedServer.URL, Title: "Background feed"},
		saved: make(chan struct{}),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := syndication.NewService(context.Background(), refreshRepository, syndication.NewFetcher(time.Second, 1<<20, true), logger, 1)
	server := newTestServerWithService(t, &fakeRepository{}, service)

	request := httptest.NewRequest(http.MethodPost, "http://castdock.test/refresh", nil)
	request.Header.Set("Origin", "http://castdock.test")
	canceledContext, cancel := context.WithCancel(request.Context())
	cancel()
	request = request.WithContext(canceledContext)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	select {
	case <-requestReachedFeed:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("background refresh did not outlive the canceled request")
	}
	close(releaseFeed)
	select {
	case <-refreshRepository.saved:
	case <-time.After(time.Second):
		t.Fatal("background refresh did not save the feed")
	}
	service.WaitForRefreshes()
}

func newTestServerWithService(t *testing.T, repository Repository, service *syndication.Service) *Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, err := New(repository, service, logger, Options{MaxOPMLBytes: 1024, MaxOPMLFeeds: 10})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return server
}
