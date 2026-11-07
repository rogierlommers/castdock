package storage

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rlommers/castdock/internal/playback"
	"github.com/rlommers/castdock/internal/podcast"
	"github.com/rlommers/castdock/migrations"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestStoreFeedEpisodeAndPlayback(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "castdock.db"), discardLogger())
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
			Description: "Formatted notes", DescriptionHTML: "<p><strong>Formatted</strong> notes</p>",
			EnclosureURL: "https://cdn.example.com/one.mp3", DurationSeconds: 120,
		}},
	}, true)
	if err != nil {
		t.Fatalf("SaveParsedFeed() error = %v", err)
	}
	refreshedFeed, err := store.Feed(ctx, feed.ID)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if refreshedFeed.LastChecked == nil || refreshedFeed.LastEpisodePublished == nil || !refreshedFeed.LastEpisodePublished.Equal(published) {
		t.Fatalf("feed dates = checked %v, latest episode %v", refreshedFeed.LastChecked, refreshedFeed.LastEpisodePublished)
	}
	episodes, err := store.EpisodesForFeed(ctx, feed.ID, 100, 0)
	if err != nil || len(episodes) != 1 {
		t.Fatalf("EpisodesForFeed() = %+v, %v", episodes, err)
	}
	if episodes[0].DescriptionHTML != "<p><strong>Formatted</strong> notes</p>" {
		t.Fatalf("DescriptionHTML = %q", episodes[0].DescriptionHTML)
	}

	newer := time.Now().UTC()
	_, err = store.SavePlayback(ctx, playback.State{
		EpisodeID: episodes[0].ID, Position: 30, Duration: 120,
		Playing: true, ClientUpdatedAt: newer,
	})
	if err != nil {
		t.Fatalf("SavePlayback() error = %v", err)
	}
	// An out-of-order request must not overwrite newer progress.
	_, err = store.SavePlayback(ctx, playback.State{
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

func TestEpisodeLifecycleAndQueueOrdering(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "castdock.db"), discardLogger())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	feed, _, err := store.AddFeed(ctx, "https://example.com/feed.xml")
	if err != nil {
		t.Fatalf("AddFeed() error = %v", err)
	}
	oldPublished := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	newPublished := oldPublished.Add(24 * time.Hour)
	initial := podcast.ParsedFeed{Title: "Example", ImageURL: "https://example.com/feed-cover.jpg", Episodes: []podcast.ParsedEpisode{{GUID: "old", Title: "Old episode", PublishedAt: &oldPublished}}}
	if err := store.SaveParsedFeed(ctx, feed.ID, initial, true); err != nil {
		t.Fatalf("initial SaveParsedFeed() error = %v", err)
	}
	updated := podcast.ParsedFeed{Title: "Example", ImageURL: "https://example.com/feed-cover.jpg", Episodes: []podcast.ParsedEpisode{
		{GUID: "old", Title: "Old episode updated", PublishedAt: &oldPublished},
		{GUID: "new", Title: "New episode", PublishedAt: &newPublished},
	}}
	if err := store.SaveParsedFeed(ctx, feed.ID, updated, false); err != nil {
		t.Fatalf("later SaveParsedFeed() error = %v", err)
	}
	episodes, err := store.EpisodesForFeed(ctx, feed.ID, 10, 0)
	if err != nil || len(episodes) != 2 {
		t.Fatalf("EpisodesForFeed() = %+v, %v", episodes, err)
	}
	byGUID := map[string]podcast.Episode{episodes[0].GUID: episodes[0], episodes[1].GUID: episodes[1]}
	if byGUID["old"].State != podcast.EpisodeStateArchived || byGUID["new"].State != podcast.EpisodeStateInbox {
		t.Fatalf("unexpected states: old=%s new=%s", byGUID["old"].State, byGUID["new"].State)
	}
	if byGUID["new"].FeedImageURL != "https://example.com/feed-cover.jpg" {
		t.Fatalf("FeedImageURL = %q", byGUID["new"].FeedImageURL)
	}
	inbox, err := store.InboxEpisodes(ctx)
	if err != nil || len(inbox) != 1 || inbox[0].GUID != "new" {
		t.Fatalf("InboxEpisodes() = %+v, %v", inbox, err)
	}
	recent, err := store.RecentEpisodes(ctx, 10, 0)
	if err != nil || len(recent) != 2 || recent[0].GUID != "new" {
		t.Fatalf("RecentEpisodes() = %+v, %v", recent, err)
	}
	archived, err := store.EpisodesByState(ctx, podcast.EpisodeStateArchived, "Old episode", 10, 0)
	if err != nil || len(archived) != 1 || archived[0].GUID != "old" {
		t.Fatalf("EpisodesByState() search = %+v, %v", archived, err)
	}

	if err := store.SetEpisodeState(ctx, byGUID["old"].ID, podcast.EpisodeStateQueued); err != nil {
		t.Fatalf("queue old episode: %v", err)
	}
	if err := store.SetEpisodeState(ctx, byGUID["new"].ID, podcast.EpisodeStateQueued); err != nil {
		t.Fatalf("queue new episode: %v", err)
	}
	if err := store.MoveQueuedEpisode(ctx, byGUID["new"].ID, -1); err != nil {
		t.Fatalf("move queued episode: %v", err)
	}
	queued, err := store.QueuedEpisodes(ctx)
	if err != nil || len(queued) != 2 || queued[0].GUID != "new" || queued[1].GUID != "old" {
		t.Fatalf("QueuedEpisodes() after move = %+v, %v", queued, err)
	}
	if err := store.ReorderQueuedEpisode(ctx, byGUID["new"].ID, 2); err != nil {
		t.Fatalf("ReorderQueuedEpisode() down: %v", err)
	}
	queued, err = store.QueuedEpisodes(ctx)
	if err != nil || queued[0].GUID != "old" || queued[1].GUID != "new" {
		t.Fatalf("QueuedEpisodes() after drag down = %+v, %v", queued, err)
	}
	if err := store.ReorderQueuedEpisode(ctx, byGUID["new"].ID, 1); err != nil {
		t.Fatalf("ReorderQueuedEpisode() up: %v", err)
	}
	queued, err = store.QueuedEpisodes(ctx)
	if err != nil || queued[0].GUID != "new" || queued[1].GUID != "old" {
		t.Fatalf("QueuedEpisodes() after drag up = %+v, %v", queued, err)
	}
	if err := store.ReorderQueuedEpisode(ctx, byGUID["new"].ID, 3); !errors.Is(err, ErrInvalidQueuePosition) {
		t.Fatalf("ReorderQueuedEpisode() invalid target error = %v", err)
	}

	if err := store.SetEpisodeState(ctx, byGUID["new"].ID, podcast.EpisodeStateArchived); err != nil {
		t.Fatalf("remove queued episode: %v", err)
	}
	if err := store.SetEpisodeState(ctx, byGUID["new"].ID, podcast.EpisodeStateQueued); err != nil {
		t.Fatalf("requeue episode: %v", err)
	}
	advanced, err := store.SavePlayback(ctx, playback.State{
		EpisodeID: byGUID["old"].ID, Position: 60, Duration: 60,
		Completed: true, ClientUpdatedAt: time.Now().UTC(),
	})
	if err != nil || !advanced {
		t.Fatalf("completed SavePlayback() = %v, %v", advanced, err)
	}
	history, err := store.PlayHistoryEpisodes(ctx, 20)
	if err != nil || len(history) != 1 || history[0].GUID != "old" || history[0].PlaybackUpdated == nil {
		t.Fatalf("PlayHistoryEpisodes() = %+v, %v", history, err)
	}
	queued, err = store.QueuedEpisodes(ctx)
	if err != nil || len(queued) != 1 || queued[0].GUID != "new" || queued[0].QueuePosition == nil || *queued[0].QueuePosition != 1 {
		t.Fatalf("QueuedEpisodes() after completion = %+v, %v", queued, err)
	}
	if err := store.MarkEpisodePlayed(ctx, byGUID["new"].ID); err != nil {
		t.Fatalf("MarkEpisodePlayed() error = %v", err)
	}
	markedPlayed, err := store.Episode(ctx, byGUID["new"].ID)
	if err != nil || !markedPlayed.Completed || markedPlayed.State != podcast.EpisodeStateArchived || markedPlayed.QueuePosition != nil {
		t.Fatalf("marked-played episode = %+v, %v", markedPlayed, err)
	}
	queued, err = store.QueuedEpisodes(ctx)
	if err != nil || len(queued) != 0 {
		t.Fatalf("QueuedEpisodes() after mark played = %+v, %v", queued, err)
	}
	statistics, err := store.LibraryStatistics(ctx)
	if err != nil {
		t.Fatalf("LibraryStatistics() error = %v", err)
	}
	if statistics.FeedCount != 1 || statistics.EpisodeCount != 2 || statistics.QueuedCount != 0 || statistics.ArchivedCount != 2 || statistics.CompletedCount != 2 || statistics.DatabaseBytes <= 0 {
		t.Fatalf("LibraryStatistics() = %+v", statistics)
	}
}

func TestOpenMigratesExistingDatabaseIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "castdock.db")
	for range 2 {
		store, err := Open(context.Background(), path, discardLogger())
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
}

func TestLifecycleMigrationArchivesExistingEpisodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "castdock.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	initialSchema, err := migrations.Files.ReadFile("001_initial.sql")
	if err != nil {
		t.Fatalf("read initial migration: %v", err)
	}
	if _, err := db.Exec(string(initialSchema)); err != nil {
		t.Fatalf("apply initial migration: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (1, '2026-11-07T08:00:00Z')`); err != nil {
		t.Fatalf("record initial migration: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO feeds(id, url, created_at, updated_at) VALUES (1, 'https://example.com/feed.xml', '2026-11-07T08:00:00Z', '2026-11-07T08:00:00Z')`); err != nil {
		t.Fatalf("insert feed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO episodes(id, feed_id, guid, title, created_at, updated_at) VALUES (1, 1, 'existing', 'Existing episode', '2026-11-07T08:00:00Z', '2026-11-07T08:00:00Z')`); err != nil {
		t.Fatalf("insert episode: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close pre-migration database: %v", err)
	}

	store, err := Open(context.Background(), path, discardLogger())
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	defer store.Close()
	episode, err := store.Episode(context.Background(), 1)
	if err != nil {
		t.Fatalf("Episode() error = %v", err)
	}
	if episode.State != podcast.EpisodeStateArchived || episode.QueuePosition != nil {
		t.Fatalf("migrated episode state = %s, queue position = %v", episode.State, episode.QueuePosition)
	}
}

func TestOpenLogsMigrationProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "castdock.db")
	var firstRun bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&firstRun, &slog.HandlerOptions{Level: slog.LevelDebug}))
	store, err := Open(context.Background(), path, logger)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	store.Close()
	for _, expected := range []string{
		`msg="database migration check started"`,
		`msg="applying database migration"`,
		`msg="database migration applied"`,
		`msg="database migrations complete"`,
		`current_version=3`,
		`applied=3`,
	} {
		if !strings.Contains(firstRun.String(), expected) {
			t.Fatalf("first migration log missing %q: %s", expected, firstRun.String())
		}
	}

	var secondRun bytes.Buffer
	logger = slog.New(slog.NewTextHandler(&secondRun, &slog.HandlerOptions{Level: slog.LevelDebug}))
	store, err = Open(context.Background(), path, logger)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	store.Close()
	if !strings.Contains(secondRun.String(), `msg="database migration already applied"`) || !strings.Contains(secondRun.String(), `applied=0`) {
		t.Fatalf("second migration log lacks skip summary: %s", secondRun.String())
	}
}
