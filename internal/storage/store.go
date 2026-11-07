package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rlommers/castdock/internal/playback"
	"github.com/rlommers/castdock/internal/podcast"
	"github.com/rlommers/castdock/migrations"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound             = errors.New("not found")
	ErrInvalidQueuePosition = errors.New("invalid queue position")
)

type Store struct {
	db     *sql.DB
	logger *slog.Logger
}

func Open(ctx context.Context, path string, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	dsnURL := &url.URL{Scheme: "file", Path: filepath.ToSlash(absolutePath)}
	dsnURL.RawQuery = "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	dsn := dsnURL.String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)

	store := &Store{db: db, logger: logger}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	logger.Info("database ready", "path", path)
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Health(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) LibraryStatistics(ctx context.Context) (podcast.LibraryStatistics, error) {
	var statistics podcast.LibraryStatistics
	var latestFeedScan sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM feeds),
			COUNT(e.id),
			COALESCE(SUM(CASE WHEN e.state = 'inbox' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN e.state = 'queued' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN e.state = 'archived' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN p.completed = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN p.position_seconds > 0 AND p.completed = 0 THEN 1 ELSE 0 END), 0),
			(SELECT MAX(last_checked) FROM feeds)
		FROM episodes e
		LEFT JOIN playback p ON p.episode_id = e.id`).Scan(
		&statistics.FeedCount,
		&statistics.EpisodeCount,
		&statistics.InboxCount,
		&statistics.QueuedCount,
		&statistics.ArchivedCount,
		&statistics.CompletedCount,
		&statistics.InProgressCount,
		&latestFeedScan,
	)
	if err != nil {
		return podcast.LibraryStatistics{}, fmt.Errorf("load library statistics: %w", err)
	}
	statistics.LatestFeedScan = parseOptionalTime(latestFeedScan)
	var pageCount, pageSize int64
	if err := s.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return podcast.LibraryStatistics{}, fmt.Errorf("read database page count: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return podcast.LibraryStatistics{}, fmt.Errorf("read database page size: %w", err)
	}
	statistics.DatabaseBytes = pageCount * pageSize
	return statistics, nil
}

func (s *Store) migrate(ctx context.Context) error {
	s.logger.Info("database migration check started")
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	appliedThisRun := 0
	currentVersion := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		versionText, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return fmt.Errorf("migration %q has no numeric prefix", entry.Name())
		}
		version, err := strconv.Atoi(versionText)
		if err != nil {
			return fmt.Errorf("migration %q has invalid version: %w", entry.Name(), err)
		}
		currentVersion = max(currentVersion, version)
		var applied int
		err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if applied != 0 {
			s.logger.Debug("database migration already applied", "version", version, "file", entry.Name())
			continue
		}
		contents, err := fs.ReadFile(migrations.Files, entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %d: %w", version, err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", version, err)
		}
		started := time.Now()
		s.logger.Info("applying database migration", "version", version, "file", entry.Name())
		if _, err = tx.ExecContext(ctx, string(contents)); err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)", version, formatTime(time.Now()))
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", version, err)
		}
		appliedThisRun++
		s.logger.Info("database migration applied", "version", version, "file", entry.Name(), "duration", time.Since(started))
	}
	s.logger.Info("database migrations complete", "current_version", currentVersion, "applied", appliedThisRun)
	return nil
}

func (s *Store) AddFeed(ctx context.Context, rawURL string) (podcast.Feed, bool, error) {
	now := formatTime(time.Now())
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO feeds(url, created_at, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(url) DO NOTHING`, rawURL, now, now)
	if err != nil {
		return podcast.Feed{}, false, fmt.Errorf("insert feed: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return podcast.Feed{}, false, fmt.Errorf("feed rows affected: %w", err)
	}
	feed, err := s.FeedByURL(ctx, rawURL)
	return feed, rows == 1, err
}

func (s *Store) FeedByURL(ctx context.Context, rawURL string) (podcast.Feed, error) {
	return scanFeed(s.db.QueryRowContext(ctx, feedSelect+" WHERE f.url = ? GROUP BY f.id", rawURL))
}

func (s *Store) Feed(ctx context.Context, id int64) (podcast.Feed, error) {
	return scanFeed(s.db.QueryRowContext(ctx, feedSelect+" WHERE f.id = ? GROUP BY f.id", id))
}

func (s *Store) ListFeeds(ctx context.Context) ([]podcast.Feed, error) {
	rows, err := s.db.QueryContext(ctx, feedSelect+" GROUP BY f.id ORDER BY lower(f.title), f.id")
	if err != nil {
		return nil, fmt.Errorf("list feeds: %w", err)
	}
	defer rows.Close()
	var feeds []podcast.Feed
	for rows.Next() {
		feed, err := scanFeed(rows)
		if err != nil {
			return nil, err
		}
		feeds = append(feeds, feed)
	}
	return feeds, rows.Err()
}

const feedSelect = `
	SELECT f.id, f.url, f.title, f.description, f.site_url, f.image_url,
	       f.etag, f.last_modified, f.last_checked, f.last_success, f.last_error,
	       f.created_at, f.updated_at, COUNT(e.id), MAX(e.published_at)
	FROM feeds f LEFT JOIN episodes e ON e.feed_id = f.id`

type scanner interface{ Scan(...any) error }

func scanFeed(row scanner) (podcast.Feed, error) {
	var feed podcast.Feed
	var lastChecked, lastSuccess, lastEpisodePublished sql.NullString
	var created, updated string
	err := row.Scan(
		&feed.ID, &feed.URL, &feed.Title, &feed.Description, &feed.SiteURL, &feed.ImageURL,
		&feed.ETag, &feed.LastModified, &lastChecked, &lastSuccess, &feed.LastError,
		&created, &updated, &feed.EpisodeCount, &lastEpisodePublished,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return podcast.Feed{}, ErrNotFound
	}
	if err != nil {
		return podcast.Feed{}, fmt.Errorf("scan feed: %w", err)
	}
	feed.LastChecked = parseOptionalTime(lastChecked)
	feed.LastSuccess = parseOptionalTime(lastSuccess)
	feed.LastEpisodePublished = parseOptionalTime(lastEpisodePublished)
	feed.CreatedAt = parseTime(created)
	feed.UpdatedAt = parseTime(updated)
	return feed, nil
}

func (s *Store) SaveParsedFeed(ctx context.Context, feedID int64, parsed podcast.ParsedFeed, initialRefresh bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin feed save: %w", err)
	}
	defer tx.Rollback()
	now := formatTime(time.Now())
	newEpisodeState := podcast.EpisodeStateInbox
	if initialRefresh {
		newEpisodeState = podcast.EpisodeStateArchived
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE feeds SET title = ?, description = ?, site_url = ?, image_url = ?, etag = ?,
		last_modified = ?, last_checked = ?, last_success = ?, last_error = '', updated_at = ?
		WHERE id = ?`, parsed.Title, parsed.Description, parsed.SiteURL, parsed.ImageURL,
		parsed.ETag, parsed.LastModified, now, now, now, feedID)
	if err != nil {
		return fmt.Errorf("update feed: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	for _, episode := range parsed.Episodes {
		var published any
		if episode.PublishedAt != nil {
			published = formatTime(*episode.PublishedAt)
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO episodes(
				feed_id, guid, title, description, description_html, published_at, duration_seconds,
				enclosure_url, enclosure_type, image_url, created_at, updated_at, state
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(feed_id, guid) DO UPDATE SET
				title = excluded.title,
				description = excluded.description,
				description_html = excluded.description_html,
				published_at = excluded.published_at,
				duration_seconds = excluded.duration_seconds,
				enclosure_url = excluded.enclosure_url,
				enclosure_type = excluded.enclosure_type,
				image_url = excluded.image_url,
				updated_at = excluded.updated_at`,
			feedID, episode.GUID, episode.Title, episode.Description, episode.DescriptionHTML, published,
			episode.DurationSeconds, episode.EnclosureURL, episode.EnclosureType,
			episode.ImageURL, now, now, newEpisodeState,
		)
		if err != nil {
			return fmt.Errorf("upsert episode %q: %w", episode.GUID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit feed save: %w", err)
	}
	return nil
}

func (s *Store) MarkFeedError(ctx context.Context, feedID int64, feedErr error) error {
	now := formatTime(time.Now())
	_, err := s.db.ExecContext(ctx, `
		UPDATE feeds SET last_checked = ?, last_error = ?, updated_at = ? WHERE id = ?`,
		now, truncate(feedErr.Error(), 1000), now, feedID)
	if err != nil {
		return fmt.Errorf("mark feed error: %w", err)
	}
	return nil
}

func (s *Store) MarkFeedNotModified(ctx context.Context, feedID int64, etag, lastModified string) error {
	now := formatTime(time.Now())
	_, err := s.db.ExecContext(ctx, `
		UPDATE feeds SET etag = ?, last_modified = ?, last_checked = ?, last_success = ?,
		last_error = '', updated_at = ? WHERE id = ?`,
		etag, lastModified, now, now, now, feedID)
	if err != nil {
		return fmt.Errorf("mark feed not modified: %w", err)
	}
	return nil
}

func (s *Store) EpisodesForFeed(ctx context.Context, feedID int64, limit, offset int) ([]podcast.Episode, error) {
	rows, err := s.db.QueryContext(ctx, episodeSelect+`
		WHERE e.feed_id = ?
		ORDER BY e.published_at DESC, e.id DESC
		LIMIT ? OFFSET ?`, feedID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list episodes: %w", err)
	}
	defer rows.Close()
	return scanEpisodes(rows)
}

func (s *Store) RecentEpisodes(ctx context.Context, limit, offset int) ([]podcast.Episode, error) {
	rows, err := s.db.QueryContext(ctx, episodeSelect+`
		ORDER BY e.published_at DESC, e.id DESC
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list recent episodes: %w", err)
	}
	defer rows.Close()
	return scanEpisodes(rows)
}

func (s *Store) PlayHistoryEpisodes(ctx context.Context, limit int) ([]podcast.Episode, error) {
	rows, err := s.db.QueryContext(ctx, episodeSelect+`
		WHERE p.updated_at IS NOT NULL AND (p.position_seconds > 0 OR p.completed = 1)
		ORDER BY p.updated_at DESC, e.id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list play history: %w", err)
	}
	defer rows.Close()
	return scanEpisodes(rows)
}

func (s *Store) InboxEpisodes(ctx context.Context) ([]podcast.Episode, error) {
	rows, err := s.db.QueryContext(ctx, episodeSelect+`
		WHERE e.state = ?
		ORDER BY e.published_at DESC, e.id DESC`, podcast.EpisodeStateInbox)
	if err != nil {
		return nil, fmt.Errorf("list inbox episodes: %w", err)
	}
	defer rows.Close()
	return scanEpisodes(rows)
}

func (s *Store) EpisodesByState(ctx context.Context, state podcast.EpisodeState, search string, limit, offset int) ([]podcast.Episode, error) {
	search = strings.TrimSpace(search)
	rows, err := s.db.QueryContext(ctx, episodeSelect+`
		WHERE e.state = ? AND (
			? = '' OR e.title LIKE '%' || ? || '%' OR e.description LIKE '%' || ? || '%'
			OR f.title LIKE '%' || ? || '%'
		)
		ORDER BY e.published_at DESC, e.id DESC
		LIMIT ? OFFSET ?`, state, search, search, search, search, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list %s episodes: %w", state, err)
	}
	defer rows.Close()
	return scanEpisodes(rows)
}

func (s *Store) QueuedEpisodes(ctx context.Context) ([]podcast.Episode, error) {
	rows, err := s.db.QueryContext(ctx, episodeSelect+`
		WHERE e.state = ?
		ORDER BY e.queue_position`, podcast.EpisodeStateQueued)
	if err != nil {
		return nil, fmt.Errorf("list queued episodes: %w", err)
	}
	defer rows.Close()
	return scanEpisodes(rows)
}

func (s *Store) NextQueuedEpisode(ctx context.Context) (podcast.Episode, error) {
	episode, err := scanEpisode(s.db.QueryRowContext(ctx, episodeSelect+`
		WHERE e.state = ?
		ORDER BY e.queue_position
		LIMIT 1`, podcast.EpisodeStateQueued))
	if errors.Is(err, sql.ErrNoRows) {
		return podcast.Episode{}, ErrNotFound
	}
	return episode, err
}

func (s *Store) Episode(ctx context.Context, id int64) (podcast.Episode, error) {
	row := s.db.QueryRowContext(ctx, episodeSelect+" WHERE e.id = ?", id)
	episode, err := scanEpisode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return podcast.Episode{}, ErrNotFound
	}
	return episode, err
}

const episodeSelect = `
	SELECT e.id, e.feed_id, COALESCE(NULLIF(f.title, ''), f.url), f.image_url, e.guid, e.title, e.description, e.description_html, e.published_at,
	       e.duration_seconds, e.enclosure_url, e.enclosure_type, e.image_url,
	       e.created_at, e.updated_at,
	       COALESCE(p.position_seconds, 0), COALESCE(p.duration_seconds, 0),
	       COALESCE(p.completed, 0), COALESCE(p.playing, 0), p.updated_at,
	       e.state, e.queue_position
	FROM episodes e
	JOIN feeds f ON f.id = e.feed_id
	LEFT JOIN playback p ON p.episode_id = e.id`

func scanEpisodes(rows *sql.Rows) ([]podcast.Episode, error) {
	var episodes []podcast.Episode
	for rows.Next() {
		episode, err := scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, episode)
	}
	return episodes, rows.Err()
}

func scanEpisode(row scanner) (podcast.Episode, error) {
	var episode podcast.Episode
	var published, playbackUpdated sql.NullString
	var queuePosition sql.NullInt64
	var created, updated string
	err := row.Scan(
		&episode.ID, &episode.FeedID, &episode.FeedTitle, &episode.FeedImageURL, &episode.GUID, &episode.Title, &episode.Description, &episode.DescriptionHTML,
		&published, &episode.DurationSeconds, &episode.EnclosureURL, &episode.EnclosureType,
		&episode.ImageURL, &created, &updated, &episode.PositionSeconds, &episode.PlayedDuration,
		&episode.Completed, &episode.Playing, &playbackUpdated, &episode.State, &queuePosition,
	)
	if err != nil {
		return podcast.Episode{}, err
	}
	episode.PublishedAt = parseOptionalTime(published)
	episode.PlaybackUpdated = parseOptionalTime(playbackUpdated)
	if queuePosition.Valid {
		position := int(queuePosition.Int64)
		episode.QueuePosition = &position
	}
	episode.CreatedAt = parseTime(created)
	episode.UpdatedAt = parseTime(updated)
	return episode, nil
}

func (s *Store) SetEpisodeState(ctx context.Context, episodeID int64, state podcast.EpisodeState) error {
	if state != podcast.EpisodeStateInbox && state != podcast.EpisodeStateQueued && state != podcast.EpisodeStateArchived {
		return fmt.Errorf("invalid episode state %q", state)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin episode state change: %w", err)
	}
	defer tx.Rollback()
	var currentState podcast.EpisodeState
	var oldPosition sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT state, queue_position FROM episodes WHERE id = ?", episodeID).Scan(&currentState, &oldPosition); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read episode state: %w", err)
	}
	if currentState == state {
		return tx.Commit()
	}
	if state == podcast.EpisodeStateQueued {
		var nextPosition int
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(queue_position), 0) + 1 FROM episodes WHERE state = ?", podcast.EpisodeStateQueued).Scan(&nextPosition); err != nil {
			return fmt.Errorf("choose queue position: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE episodes SET state = ?, queue_position = ? WHERE id = ?", state, nextPosition, episodeID); err != nil {
			return fmt.Errorf("queue episode: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE playback SET position_seconds = 0, completed = 0 WHERE episode_id = ? AND completed = 1", episodeID); err != nil {
			return fmt.Errorf("reset completed playback: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, "UPDATE episodes SET state = ?, queue_position = NULL WHERE id = ?", state, episodeID); err != nil {
			return fmt.Errorf("change episode state: %w", err)
		}
	}
	if oldPosition.Valid {
		if _, err := tx.ExecContext(ctx, "UPDATE episodes SET queue_position = queue_position - 1 WHERE queue_position > ?", oldPosition.Int64); err != nil {
			return fmt.Errorf("compact queue: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit episode state change: %w", err)
	}
	return nil
}

func (s *Store) MoveQueuedEpisode(ctx context.Context, episodeID int64, direction int) error {
	if direction != -1 && direction != 1 {
		return fmt.Errorf("queue direction must be -1 or 1")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin queue move: %w", err)
	}
	defer tx.Rollback()
	var currentPosition int
	if err := tx.QueryRowContext(ctx, "SELECT queue_position FROM episodes WHERE id = ? AND state = ?", episodeID, podcast.EpisodeStateQueued).Scan(&currentPosition); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read queue position: %w", err)
	}
	targetPosition := currentPosition + direction
	var targetID int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM episodes WHERE state = ? AND queue_position = ?", podcast.EpisodeStateQueued, targetPosition).Scan(&targetID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tx.Commit()
		}
		return fmt.Errorf("read target queue position: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE episodes SET queue_position = -1 WHERE id = ?", episodeID); err != nil {
		return fmt.Errorf("reserve queue position: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE episodes SET queue_position = ? WHERE id = ?", currentPosition, targetID); err != nil {
		return fmt.Errorf("move target queue episode: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE episodes SET queue_position = ? WHERE id = ?", targetPosition, episodeID); err != nil {
		return fmt.Errorf("move queue episode: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit queue move: %w", err)
	}
	return nil
}

func (s *Store) ReorderQueuedEpisode(ctx context.Context, episodeID int64, targetPosition int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin queue reorder: %w", err)
	}
	defer tx.Rollback()
	var currentPosition, queueLength int
	if err := tx.QueryRowContext(ctx, "SELECT queue_position FROM episodes WHERE id = ? AND state = ?", episodeID, podcast.EpisodeStateQueued).Scan(&currentPosition); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read queue position: %w", err)
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM episodes WHERE state = ?", podcast.EpisodeStateQueued).Scan(&queueLength); err != nil {
		return fmt.Errorf("count queue: %w", err)
	}
	if targetPosition < 1 || targetPosition > queueLength {
		return fmt.Errorf("%w: must be between 1 and %d", ErrInvalidQueuePosition, queueLength)
	}
	if targetPosition == currentPosition {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, "UPDATE episodes SET queue_position = -queue_position WHERE state = ?", podcast.EpisodeStateQueued); err != nil {
		return fmt.Errorf("reserve queue positions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE episodes SET queue_position = CASE
			WHEN -queue_position = ? THEN ?
			WHEN ? < ? AND -queue_position > ? AND -queue_position <= ? THEN -queue_position - 1
			WHEN ? > ? AND -queue_position >= ? AND -queue_position < ? THEN -queue_position + 1
			ELSE -queue_position
		END
		WHERE state = ?`,
		currentPosition, targetPosition,
		currentPosition, targetPosition, currentPosition, targetPosition,
		currentPosition, targetPosition, targetPosition, currentPosition,
		podcast.EpisodeStateQueued,
	); err != nil {
		return fmt.Errorf("reorder queue: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit queue reorder: %w", err)
	}
	return nil
}

func (s *Store) SavePlayback(ctx context.Context, state playback.State) (bool, error) {
	if err := state.Validate(); err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin playback save: %w", err)
	}
	defer tx.Rollback()
	now := formatTime(time.Now())
	clientTime := formatTime(state.ClientUpdatedAt)
	result, err := tx.ExecContext(ctx, `
		INSERT INTO playback(
			episode_id, position_seconds, duration_seconds, completed, playing,
			client_updated_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(episode_id) DO UPDATE SET
			position_seconds = excluded.position_seconds,
			duration_seconds = excluded.duration_seconds,
			completed = excluded.completed,
			playing = excluded.playing,
			client_updated_at = excluded.client_updated_at,
			updated_at = excluded.updated_at
		WHERE excluded.client_updated_at >= playback.client_updated_at`,
		state.EpisodeID, state.Position, state.Duration, state.Completed, state.Playing,
		clientTime, now,
	)
	if err != nil {
		return false, fmt.Errorf("save playback: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return false, tx.Commit()
	}
	advancedQueue := false
	if state.Completed {
		var episodeState podcast.EpisodeState
		var queuePosition sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT state, queue_position FROM episodes WHERE id = ?", state.EpisodeID).Scan(&episodeState, &queuePosition); err != nil {
			return false, fmt.Errorf("read completed episode state: %w", err)
		}
		advancedQueue = episodeState == podcast.EpisodeStateQueued
		if _, err := tx.ExecContext(ctx, "UPDATE episodes SET state = ?, queue_position = NULL WHERE id = ?", podcast.EpisodeStateArchived, state.EpisodeID); err != nil {
			return false, fmt.Errorf("archive completed episode: %w", err)
		}
		if queuePosition.Valid {
			if _, err := tx.ExecContext(ctx, "UPDATE episodes SET queue_position = queue_position - 1 WHERE queue_position > ?", queuePosition.Int64); err != nil {
				return false, fmt.Errorf("compact completed queue: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit playback save: %w", err)
	}
	return advancedQueue, nil
}

func (s *Store) MarkEpisodePlayed(ctx context.Context, episodeID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin mark episode played: %w", err)
	}
	defer tx.Rollback()
	var state podcast.EpisodeState
	var queuePosition sql.NullInt64
	var duration float64
	if err := tx.QueryRowContext(ctx, "SELECT state, queue_position, duration_seconds FROM episodes WHERE id = ?", episodeID).Scan(&state, &queuePosition, &duration); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read episode for played state: %w", err)
	}
	now := formatTime(time.Now())
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO playback(
			episode_id, position_seconds, duration_seconds, completed, playing,
			client_updated_at, updated_at
		) VALUES (?, ?, ?, 1, 0, ?, ?)
		ON CONFLICT(episode_id) DO UPDATE SET
			position_seconds = excluded.position_seconds,
			duration_seconds = excluded.duration_seconds,
			completed = 1,
			playing = 0,
			client_updated_at = excluded.client_updated_at,
			updated_at = excluded.updated_at`,
		episodeID, duration, duration, now, now,
	); err != nil {
		return fmt.Errorf("save played state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE episodes SET state = ?, queue_position = NULL WHERE id = ?", podcast.EpisodeStateArchived, episodeID); err != nil {
		return fmt.Errorf("archive played episode: %w", err)
	}
	if state == podcast.EpisodeStateQueued && queuePosition.Valid {
		if _, err := tx.ExecContext(ctx, "UPDATE episodes SET queue_position = queue_position - 1 WHERE queue_position > ?", queuePosition.Int64); err != nil {
			return fmt.Errorf("compact played queue: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mark episode played: %w", err)
	}
	return nil
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func parseOptionalTime(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed := parseTime(value.String)
	return &parsed
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
