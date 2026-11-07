package syndication

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/rlommers/castdock/internal/podcast"
)

type Repository interface {
	AddFeed(context.Context, string) (podcast.Feed, bool, error)
	Feed(context.Context, int64) (podcast.Feed, error)
	ListFeeds(context.Context) ([]podcast.Feed, error)
	SaveParsedFeed(context.Context, int64, podcast.ParsedFeed, bool) error
	MarkFeedError(context.Context, int64, error) error
	MarkFeedNotModified(context.Context, int64, string, string) error
}

type Service struct {
	appContext   context.Context
	repository   Repository
	fetcher      *Fetcher
	logger       *slog.Logger
	concurrency  int
	refreshAllMu sync.Mutex
}

type ImportResult struct {
	Imported  int
	Skipped   int
	Refreshed int
	Failed    int
	Canceled  int
	Busy      bool
	Errors    []string
}

func NewService(appContext context.Context, repository Repository, fetcher *Fetcher, logger *slog.Logger, concurrency int) *Service {
	if appContext == nil {
		appContext = context.Background()
	}
	return &Service{appContext: appContext, repository: repository, fetcher: fetcher, logger: logger, concurrency: concurrency}
}

func (s *Service) Subscribe(ctx context.Context, rawURL string) (podcast.Feed, bool, error) {
	normalized, err := NormalizeURL(rawURL)
	if err != nil {
		return podcast.Feed{}, false, err
	}
	feed, created, err := s.repository.AddFeed(ctx, normalized)
	if err != nil || !created {
		return feed, created, err
	}
	if err := s.Refresh(ctx, feed.ID); err != nil {
		return feed, true, err
	}
	feed, err = s.repository.Feed(ctx, feed.ID)
	return feed, true, err
}

func (s *Service) Refresh(ctx context.Context, feedID int64) error {
	started := time.Now()
	feed, err := s.repository.Feed(ctx, feedID)
	if err != nil {
		s.logger.Warn("feed refresh failed", "feed_id", feedID, "duration", time.Since(started), "error", err)
		return err
	}
	result, err := s.fetcher.Fetch(ctx, feed)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			s.logger.Debug("feed refresh canceled", "feed_id", feedID, "feed_title", feed.Title, "duration", time.Since(started))
			return err
		}
		if markErr := s.repository.MarkFeedError(ctx, feedID, err); markErr != nil {
			s.logger.Error("failed to record feed refresh error", "feed_id", feedID, "error", markErr)
		}
		s.logger.Warn("feed refresh failed", "feed_id", feedID, "feed_title", feed.Title, "duration", time.Since(started), "error", err)
		return err
	}
	if result.NotModified {
		if err := s.repository.MarkFeedNotModified(ctx, feedID, result.ETag, result.LastModified); err != nil {
			s.logger.Warn("feed refresh failed", "feed_id", feedID, "feed_title", feed.Title, "duration", time.Since(started), "error", err)
			return err
		}
		s.logRefreshComplete(feed, result, time.Since(started))
		return nil
	}
	if err := s.repository.SaveParsedFeed(ctx, feedID, result.Feed, feed.LastSuccess == nil); err != nil {
		s.logger.Warn("feed refresh failed", "feed_id", feedID, "feed_title", feed.Title, "duration", time.Since(started), "error", err)
		return err
	}
	s.logRefreshComplete(feed, result, time.Since(started))
	return nil
}

func (s *Service) logRefreshComplete(previous podcast.Feed, result FetchResult, duration time.Duration) {
	parsedURL, _ := url.Parse(previous.URL)
	feedTitle := previous.Title
	episodeCount := previous.EpisodeCount
	outcome := "not_modified"
	if !result.NotModified {
		feedTitle = result.Feed.Title
		episodeCount = len(result.Feed.Episodes)
		outcome = "updated"
	}
	s.logger.Debug("feed refresh complete",
		"feed_id", previous.ID,
		"feed_title", feedTitle,
		"feed_host", parsedURL.Hostname(),
		"outcome", outcome,
		"episode_count", episodeCount,
		"duration", duration,
	)
}

func (s *Service) RefreshAll(ctx context.Context) ImportResult {
	if ctx.Err() != nil {
		return ImportResult{Canceled: 1}
	}
	if !s.refreshAllMu.TryLock() {
		return ImportResult{Busy: true}
	}
	defer s.refreshAllMu.Unlock()
	return s.refreshAll(ctx)
}

func (s *Service) StartRefreshAll() bool {
	if s.appContext.Err() != nil {
		return false
	}
	if !s.refreshAllMu.TryLock() {
		return false
	}
	go func() {
		defer s.refreshAllMu.Unlock()
		result := s.refreshAll(s.appContext)
		s.logger.Info("manual library refresh complete",
			"refreshed", result.Refreshed,
			"failed", result.Failed,
			"canceled", result.Canceled,
		)
	}()
	return true
}

func (s *Service) WaitForRefreshes() {
	s.refreshAllMu.Lock()
	s.refreshAllMu.Unlock()
}

func (s *Service) refreshAll(ctx context.Context) ImportResult {
	feeds, err := s.repository.ListFeeds(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return ImportResult{Canceled: 1}
		}
		return ImportResult{Failed: 1, Errors: []string{err.Error()}}
	}
	return s.run(ctx, feeds)
}

func (s *Service) Import(ctx context.Context, urls []string) ImportResult {
	result := ImportResult{}
	feeds := make([]podcast.Feed, 0, len(urls))
	for _, rawURL := range urls {
		feed, created, err := s.repository.AddFeed(ctx, rawURL)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", rawURL, err))
			continue
		}
		if !created {
			result.Skipped++
			continue
		}
		feeds = append(feeds, feed)
		result.Imported++
	}
	refreshed := s.run(ctx, feeds)
	result.Refreshed += refreshed.Refreshed
	result.Failed += refreshed.Failed
	result.Errors = append(result.Errors, refreshed.Errors...)
	return result
}

func (s *Service) run(ctx context.Context, feeds []podcast.Feed) ImportResult {
	if len(feeds) == 0 {
		return ImportResult{}
	}
	type outcome struct {
		url string
		err error
	}
	jobs := make(chan podcast.Feed)
	results := make(chan outcome, len(feeds))
	workers := min(s.concurrency, len(feeds))
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for feed := range jobs {
				results <- outcome{url: feed.URL, err: s.Refresh(ctx, feed.ID)}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, feed := range feeds {
			select {
			case jobs <- feed:
			case <-ctx.Done():
				return
			}
		}
	}()
	group.Wait()
	close(results)

	result := ImportResult{}
	for outcome := range results {
		if outcome.err != nil {
			if errors.Is(outcome.err, context.Canceled) {
				result.Canceled++
				continue
			}
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", outcome.url, outcome.err))
		} else {
			result.Refreshed++
		}
	}
	return result
}
