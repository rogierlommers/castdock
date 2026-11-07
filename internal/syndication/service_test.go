package syndication

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rlommers/castdock/internal/podcast"
)

type serviceRepository struct {
	feed        podcast.Feed
	saved       podcast.ParsedFeed
	initial     bool
	savedSignal chan struct{}
	saveOnce    sync.Once
	markErrors  int
}

func (r *serviceRepository) AddFeed(context.Context, string) (podcast.Feed, bool, error) {
	return r.feed, true, nil
}
func (r *serviceRepository) Feed(context.Context, int64) (podcast.Feed, error) {
	return r.feed, nil
}
func (r *serviceRepository) ListFeeds(context.Context) ([]podcast.Feed, error) {
	return []podcast.Feed{r.feed}, nil
}
func (r *serviceRepository) SaveParsedFeed(_ context.Context, _ int64, feed podcast.ParsedFeed, initial bool) error {
	r.saved = feed
	r.initial = initial
	if r.savedSignal != nil {
		r.saveOnce.Do(func() { close(r.savedSignal) })
	}
	return nil
}

func TestStartRefreshAllIsSingleFlight(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var requestOnce sync.Once
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestOnce.Do(func() { close(requestStarted) })
		<-releaseRequest
		io.WriteString(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Single flight</title><link>https://example.com</link><description>Example</description></channel></rss>`)
	}))
	defer feedServer.Close()

	repository := &serviceRepository{
		feed:        podcast.Feed{ID: 8, URL: feedServer.URL, Title: "Single flight"},
		savedSignal: make(chan struct{}),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewService(context.Background(), repository, NewFetcher(time.Second, 1<<20, true), logger, 1)
	if !service.StartRefreshAll() {
		t.Fatal("first StartRefreshAll() did not start")
	}
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("first refresh did not reach feed server")
	}
	if service.StartRefreshAll() {
		t.Fatal("overlapping StartRefreshAll() was accepted")
	}
	close(releaseRequest)
	select {
	case <-repository.savedSignal:
	case <-time.After(time.Second):
		t.Fatal("background refresh did not complete")
	}
	service.WaitForRefreshes()
}
func (r *serviceRepository) MarkFeedError(context.Context, int64, error) error {
	r.markErrors++
	return nil
}
func (r *serviceRepository) MarkFeedNotModified(context.Context, int64, string, string) error {
	return nil
}

func TestRefreshLogsOneDetailedCompletionEvent(t *testing.T) {
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `<?xml version="1.0"?><rss version="2.0"><channel>
			<title>Logged podcast</title><link>https://example.com</link><description>Example</description>
			<item><title>Episode</title><guid>episode-1</guid><enclosure url="https://cdn.example.com/episode.mp3" type="audio/mpeg"/></item>
		</channel></rss>`)
	}))
	defer feedServer.Close()

	repository := &serviceRepository{feed: podcast.Feed{ID: 7, URL: feedServer.URL, Title: "Old title"}}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	service := NewService(context.Background(), repository, NewFetcher(time.Second, 1<<20, true), logger, 1)
	if err := service.Refresh(context.Background(), repository.feed.ID); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if repository.saved.Title != "Logged podcast" || len(repository.saved.Episodes) != 1 {
		t.Fatalf("saved feed = %+v", repository.saved)
	}
	if !repository.initial {
		t.Fatal("first successful refresh was not marked initial")
	}
	for _, expected := range []string{
		`"msg":"feed refresh complete"`,
		`"feed_id":7`,
		`"feed_host":"127.0.0.1"`,
		`"outcome":"updated"`,
		`"episode_count":1`,
	} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("refresh log missing %s: %s", expected, logs.String())
		}
	}
	if count := strings.Count(logs.String(), `"msg":"feed refresh complete"`); count != 1 {
		t.Fatalf("feed refresh emitted %d completion events: %s", count, logs.String())
	}
	if strings.Contains(logs.String(), `"msg":"feed refresh started"`) {
		t.Fatalf("feed refresh still emits a start event: %s", logs.String())
	}
	for _, removed := range []string{"not_modified", "initial_refresh", "previous_episode_count", "received_episode_count", "title_changed", "etag_received", "last_modified_received"} {
		if strings.Contains(logs.String(), `"`+removed+`":`) {
			t.Fatalf("feed refresh log still contains %q: %s", removed, logs.String())
		}
	}
}

func TestCanceledRefreshDoesNotRecordFeedError(t *testing.T) {
	repository := &serviceRepository{feed: podcast.Feed{ID: 9, URL: "https://example.com/feed.xml", Title: "Canceled"}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewService(context.Background(), repository, NewFetcher(time.Second, 1<<20, true), logger, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := service.Refresh(ctx, repository.feed.ID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Refresh() error = %v, want context.Canceled", err)
	}
	if repository.markErrors != 0 {
		t.Fatalf("MarkFeedError() called %d times", repository.markErrors)
	}
}
