package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddr   = "127.0.0.1:8080"
	defaultDatabasePath = "./data/castdock.db"
	defaultRefresh      = time.Hour
	defaultHTTPTimeout  = 20 * time.Second
	defaultMaxFeedBytes = int64(32 << 20)
	defaultMaxOPMLBytes = int64(5 << 20)
)

type Config struct {
	ListenAddr         string
	DatabasePath       string
	RefreshInterval    time.Duration
	HTTPTimeout        time.Duration
	MaxFeedBytes       int64
	MaxOPMLBytes       int64
	MaxOPMLFeeds       int
	RefreshConcurrency int
	AllowPrivateFeeds  bool
	LogLevel           slog.Level
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddr:         envOrDefault("CASTDOCK_LISTEN_ADDR", defaultListenAddr),
		DatabasePath:       envOrDefault("CASTDOCK_DATABASE_PATH", defaultDatabasePath),
		RefreshInterval:    defaultRefresh,
		HTTPTimeout:        defaultHTTPTimeout,
		MaxFeedBytes:       defaultMaxFeedBytes,
		MaxOPMLBytes:       defaultMaxOPMLBytes,
		MaxOPMLFeeds:       500,
		RefreshConcurrency: 4,
		LogLevel:           slog.LevelInfo,
	}

	var errs []error
	if cfg.ListenAddr == "" {
		errs = append(errs, errors.New("CASTDOCK_LISTEN_ADDR must not be empty"))
	}
	if cfg.DatabasePath == "" {
		errs = append(errs, errors.New("CASTDOCK_DATABASE_PATH must not be empty"))
	}

	parseDurationEnv("CASTDOCK_REFRESH_INTERVAL", &cfg.RefreshInterval, true, &errs)
	parseDurationEnv("CASTDOCK_HTTP_TIMEOUT", &cfg.HTTPTimeout, false, &errs)
	parsePositiveInt64Env("CASTDOCK_MAX_FEED_BYTES", &cfg.MaxFeedBytes, &errs)
	parsePositiveInt64Env("CASTDOCK_MAX_OPML_BYTES", &cfg.MaxOPMLBytes, &errs)
	if raw, ok := os.LookupEnv("CASTDOCK_MAX_OPML_FEEDS"); ok {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			errs = append(errs, fmt.Errorf("CASTDOCK_MAX_OPML_FEEDS must be a positive integer"))
		} else {
			cfg.MaxOPMLFeeds = value
		}
	}
	if raw, ok := os.LookupEnv("CASTDOCK_REFRESH_CONCURRENCY"); ok {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 32 {
			errs = append(errs, fmt.Errorf("CASTDOCK_REFRESH_CONCURRENCY must be between 1 and 32"))
		} else {
			cfg.RefreshConcurrency = value
		}
	}

	if raw, ok := os.LookupEnv("CASTDOCK_ALLOW_PRIVATE_FEEDS"); ok {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("CASTDOCK_ALLOW_PRIVATE_FEEDS: %w", err))
		} else {
			cfg.AllowPrivateFeeds = value
		}
	}

	if raw, ok := os.LookupEnv("CASTDOCK_LOG_LEVEL"); ok {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "debug":
			cfg.LogLevel = slog.LevelDebug
		case "info":
			cfg.LogLevel = slog.LevelInfo
		case "warn":
			cfg.LogLevel = slog.LevelWarn
		case "error":
			cfg.LogLevel = slog.LevelError
		default:
			errs = append(errs, fmt.Errorf("CASTDOCK_LOG_LEVEL must be debug, info, warn, or error"))
		}
	}

	return cfg, errors.Join(errs...)
}

func envOrDefault(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func parseDurationEnv(name string, target *time.Duration, allowZero bool, errs *[]error) {
	raw, ok := os.LookupEnv(name)
	if !ok {
		return
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 || (!allowZero && value == 0) {
		*errs = append(*errs, fmt.Errorf("%s must be a valid %sduration", name, map[bool]string{true: "non-negative ", false: "positive "}[allowZero]))
		return
	}
	*target = value
}

func parsePositiveInt64Env(name string, target *int64, errs *[]error) {
	raw, ok := os.LookupEnv(name)
	if !ok {
		return
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		*errs = append(*errs, fmt.Errorf("%s must be a positive integer", name))
		return
	}
	*target = value
}
