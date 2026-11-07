package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rlommers/castdock/internal/config"
	"github.com/rlommers/castdock/internal/server"
	"github.com/rlommers/castdock/internal/storage"
	"github.com/rlommers/castdock/internal/syndication"
)

func main() {
	if err := run(); err != nil {
		slog.Error("castdock stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := newLogger(os.Stdout, cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	store, err := storage.Open(ctx, cfg.DatabasePath, logger)
	if err != nil {
		return err
	}
	defer store.Close()

	fetcher := syndication.NewFetcher(cfg.HTTPTimeout, cfg.MaxFeedBytes, cfg.AllowPrivateFeeds)
	feedService := syndication.NewService(store, fetcher, logger, cfg.RefreshConcurrency)
	webServer, err := server.New(store, feedService, logger, cfg.MaxOPMLBytes, cfg.MaxOPMLFeeds, cfg.AccessLogEnabled)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           webServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("CastDock listening", "address", cfg.ListenAddr)
		serverErrors <- httpServer.ListenAndServe()
	}()
	if cfg.RefreshInterval > 0 {
		go scheduleRefresh(ctx, logger, feedService, cfg.RefreshInterval)
	}

	select {
	case <-ctx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func newLogger(output io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, attribute slog.Attr) slog.Attr {
			if attribute.Key == slog.TimeKey {
				attribute.Value = slog.StringValue(attribute.Value.Time().Format("2006-01-02 15:04:05"))
			}
			return attribute
		},
	}))
}

func scheduleRefresh(ctx context.Context, logger *slog.Logger, service *syndication.Service, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result := service.RefreshAll(ctx)
			logger.Info("scheduled refresh complete", "refreshed", result.Refreshed, "failed", result.Failed)
		}
	}
}
