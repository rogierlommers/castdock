package syndication

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

var ErrUnsafeURL = errors.New("feed URL is not allowed")

func NormalizeURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%w: invalid URL", ErrUnsafeURL)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: scheme must be http or https", ErrUnsafeURL)
	}
	if parsed.Hostname() == "" || parsed.User != nil {
		return "", fmt.Errorf("%w: URL must have a host and no credentials", ErrUnsafeURL)
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func safeIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
		return false
	}
	_, shared, _ := net.ParseCIDR("100.64.0.0/10")
	return !shared.Contains(ip)
}
