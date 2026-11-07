package syndication

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

	"github.com/rlommers/castdock/internal/podcast"
)

type serviceRepository struct {
	feed    podcast.Feed
	saved   podcast.ParsedFeed
	initial bool
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
	return nil
}
func (r *serviceRepository) MarkFeedError(context.Context, int64, error) error { return nil }
func (r *serviceRepository) MarkFeedNotModified(context.Context, int64, string, string) error {
	return nil
}

func TestRefreshLogsStartAndCompletion(t *testing.T) {
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
	service := NewService(repository, NewFetcher(time.Second, 1<<20, true), logger, 1)
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
		`"msg":"feed refresh started"`,
		`"msg":"feed refresh complete"`,
		`"feed_id":7`,
		`"episode_count":1`,
	} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("refresh log missing %s: %s", expected, logs.String())
		}
	}
}
