package syndication

import (
	"net"
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	got, err := NormalizeURL(" HTTPS://example.com/feed.xml#top ")
	if err != nil || got != "https://example.com/feed.xml" {
		t.Fatalf("NormalizeURL() = %q, %v", got, err)
	}
	for _, value := range []string{"file:///etc/passwd", "http://user:pass@example.com/feed", "example.com/feed"} {
		if _, err := NormalizeURL(value); err == nil {
			t.Errorf("NormalizeURL(%q) expected error", value)
		}
	}
}

func TestSafeIP(t *testing.T) {
	for _, value := range []string{"0.0.0.0", "127.0.0.1", "10.0.0.1", "169.254.1.1", "100.64.0.1", "::1"} {
		if safeIP(net.ParseIP(value)) {
			t.Errorf("safeIP(%q) = true", value)
		}
	}
	if !safeIP(net.ParseIP("1.1.1.1")) {
		t.Error("safeIP(public) = false")
	}
}
