package syndication

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/rlommers/castdock/internal/podcast"
)

func TestMapFeed(t *testing.T) {
	parsed, err := gofeed.NewParser().Parse(strings.NewReader(`<?xml version="1.0"?>
		<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel>
		<title>Example</title><link>https://example.com</link><description><![CDATA[<p>A <strong>show</strong>.</p>]]></description>
		<item><title>Episode 1</title><guid>ep-1</guid><description><![CDATA[<script>bad()</script><p>Hello</p>]]></description>
		<itunes:duration>01:02:03</itunes:duration><enclosure url="https://cdn.example.com/one.mp3" type="audio/mpeg"/></item>
		</channel></rss>`))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	feed := mapFeed(parsed)
	if feed.Title != "Example" || feed.Description != "A show ." {
		t.Fatalf("unexpected feed: %+v", feed)
	}
	if len(feed.Episodes) != 1 || feed.Episodes[0].DurationSeconds != 3723 {
		t.Fatalf("unexpected episodes: %+v", feed.Episodes)
	}
	if strings.Contains(feed.Episodes[0].Description, "<") {
		t.Fatalf("description contains markup: %q", feed.Episodes[0].Description)
	}
	if strings.Contains(feed.Episodes[0].Description, "bad()") {
		t.Fatalf("description contains script text: %q", feed.Episodes[0].Description)
	}
	if feed.Episodes[0].DescriptionHTML != "<p>Hello</p>" {
		t.Fatalf("DescriptionHTML = %q", feed.Episodes[0].DescriptionHTML)
	}
}

func TestFetchReportsConfiguredSizeLimit(t *testing.T) {
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(9))
		w.Write([]byte("123456789"))
	}))
	defer feedServer.Close()

	fetcher := NewFetcher(time.Second, 8, true)
	_, err := fetcher.Fetch(context.Background(), podcast.Feed{URL: feedServer.URL})
	if !errors.Is(err, ErrFeedTooLarge) {
		t.Fatalf("Fetch() error = %v, want ErrFeedTooLarge", err)
	}
	for _, expected := range []string{"response size 9 bytes", "configured limit 8 bytes", "CASTDOCK_MAX_FEED_BYTES"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("Fetch() error missing %q: %v", expected, err)
		}
	}
}
