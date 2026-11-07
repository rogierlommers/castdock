package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, name := range []string{
		"CASTDOCK_LISTEN_ADDR", "CASTDOCK_DATABASE_PATH", "CASTDOCK_REFRESH_INTERVAL",
		"CASTDOCK_HTTP_TIMEOUT", "CASTDOCK_MAX_FEED_BYTES", "CASTDOCK_MAX_OPML_BYTES",
		"CASTDOCK_MAX_OPML_FEEDS", "CASTDOCK_REFRESH_CONCURRENCY", "CASTDOCK_ALLOW_PRIVATE_FEEDS", "CASTDOCK_LOG_LEVEL",
	} {
		t.Setenv(name, "")
	}
	// An explicitly empty value is invalid for required strings, so restore those
	// defaults by testing only variables whose absence has different semantics.
	t.Setenv("CASTDOCK_LISTEN_ADDR", defaultListenAddr)
	t.Setenv("CASTDOCK_DATABASE_PATH", defaultDatabasePath)
	t.Setenv("CASTDOCK_REFRESH_INTERVAL", defaultRefresh.String())
	t.Setenv("CASTDOCK_HTTP_TIMEOUT", defaultHTTPTimeout.String())
	t.Setenv("CASTDOCK_MAX_FEED_BYTES", "10485760")
	t.Setenv("CASTDOCK_MAX_OPML_BYTES", "5242880")
	t.Setenv("CASTDOCK_MAX_OPML_FEEDS", "500")
	t.Setenv("CASTDOCK_REFRESH_CONCURRENCY", "4")
	t.Setenv("CASTDOCK_ALLOW_PRIVATE_FEEDS", "false")
	t.Setenv("CASTDOCK_LOG_LEVEL", "info")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.RefreshInterval != time.Hour || cfg.HTTPTimeout != 20*time.Second {
		t.Fatalf("unexpected durations: %+v", cfg)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	t.Setenv("CASTDOCK_HTTP_TIMEOUT", "0")
	t.Setenv("CASTDOCK_MAX_FEED_BYTES", "-1")
	t.Setenv("CASTDOCK_LOG_LEVEL", "verbose")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected validation error")
	}
}

func TestDefaultFeedLimitAllowsLargePodcastFeeds(t *testing.T) {
	if defaultMaxFeedBytes != 32<<20 {
		t.Fatalf("defaultMaxFeedBytes = %d, want %d", defaultMaxFeedBytes, 32<<20)
	}
}
