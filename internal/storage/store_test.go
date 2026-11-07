package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rlommers/castdock/internal/playback"
	"github.com/rlommers/castdock/internal/podcast"
)

func TestStoreFeedEpisodeAndPlayback(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "castdock.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })

	feed, created, err := store.AddFeed(ctx, "https://example.com/feed.xml")
	if err != nil || !created {
		t.Fatalf("AddFeed() = %+v, %v, %v", feed, created, err)
	}
	if _, duplicate, err := store.AddFeed(ctx, feed.URL); err != nil || duplicate {
		t.Fatalf("duplicate AddFeed() created = %v, error = %v", duplicate, err)
	}
	published := time.Date(2026, 11, 7, 8, 0, 0, 0, time.UTC)
	err = store.SaveParsedFeed(ctx, feed.ID, podcast.ParsedFeed{
		Title: "Example podcast",
		Episodes: []podcast.ParsedEpisode{{
			GUID: "episode-1", Title: "Episode one", PublishedAt: &published,
			EnclosureURL: "https://cdn.example.com/one.mp3", DurationSeconds: 120,
		}},
	})
	if err != nil {
		t.Fatalf("SaveParsedFeed() error = %v", err)
	}
	episodes, err := store.EpisodesForFeed(ctx, feed.ID, 100, 0)
	if err != nil || len(episodes) != 1 {
		t.Fatalf("EpisodesForFeed() = %+v, %v", episodes, err)
	}

	newer := time.Now().UTC()
	err = store.SavePlayback(ctx, playback.State{
		EpisodeID: episodes[0].ID, Position: 30, Duration: 120,
		Playing: true, ClientUpdatedAt: newer,
	})
	if err != nil {
		t.Fatalf("SavePlayback() error = %v", err)
	}
	// An out-of-order request must not overwrite newer progress.
	err = store.SavePlayback(ctx, playback.State{
		EpisodeID: episodes[0].ID, Position: 10, Duration: 120,
		ClientUpdatedAt: newer.Add(-time.Second),
	})
	if err != nil {
		t.Fatalf("stale SavePlayback() error = %v", err)
	}
	got, err := store.Episode(ctx, episodes[0].ID)
	if err != nil {
		t.Fatalf("Episode() error = %v", err)
	}
	if got.PositionSeconds != 30 || !got.Playing {
		t.Fatalf("Episode() playback = %+v", got)
	}
}

func TestOpenMigratesExistingDatabaseIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "castdock.db")
	for range 2 {
		store, err := Open(context.Background(), path)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
}
