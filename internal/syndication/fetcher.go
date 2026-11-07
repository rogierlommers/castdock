package syndication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/rlommers/castdock/internal/podcast"
	xhtml "golang.org/x/net/html"
)

var ErrFeedTooLarge = errors.New("feed exceeds configured size limit")

type FetchResult struct {
	Feed         podcast.ParsedFeed
	NotModified  bool
	ETag         string
	LastModified string
}

type Fetcher struct {
	client   *http.Client
	parser   *gofeed.Parser
	maxBytes int64
}

func NewFetcher(timeout time.Duration, maxBytes int64, allowPrivate bool) *Fetcher {
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	if !allowPrivate {
		transport.Proxy = nil
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("split feed address: %w", err)
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("resolve feed host: %w", err)
			}
			for _, resolved := range addresses {
				if safeIP(resolved.IP) {
					return dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
				}
			}
			return nil, ErrUnsafeURL
		}
	}

	client := &http.Client{Timeout: timeout, Transport: transport}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many feed redirects")
		}
		_, err := NormalizeURL(req.URL.String())
		return err
	}
	return &Fetcher{client: client, parser: gofeed.NewParser(), maxBytes: maxBytes}
}

func (f *Fetcher) Fetch(ctx context.Context, feed podcast.Feed) (FetchResult, error) {
	normalized, err := NormalizeURL(feed.URL)
	if err != nil {
		return FetchResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		return FetchResult{}, fmt.Errorf("create feed request: %w", err)
	}
	req.Header.Set("User-Agent", "CastDock/1.0 (+self-hosted podcast reader)")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml;q=0.9, */*;q=0.1")
	if feed.ETag != "" {
		req.Header.Set("If-None-Match", feed.ETag)
	}
	if feed.LastModified != "" {
		req.Header.Set("If-Modified-Since", feed.LastModified)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return FetchResult{}, fmt.Errorf("fetch feed: %w", err)
	}
	defer resp.Body.Close()
	result := FetchResult{ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}
	if resp.StatusCode == http.StatusNotModified {
		result.NotModified = true
		return result, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return FetchResult{}, fmt.Errorf("fetch feed: unexpected HTTP status %d", resp.StatusCode)
	}
	if resp.ContentLength > f.maxBytes {
		return FetchResult{}, feedTooLargeError(resp.ContentLength, f.maxBytes)
	}
	contents, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return FetchResult{}, fmt.Errorf("read feed: %w", err)
	}
	if int64(len(contents)) > f.maxBytes {
		return FetchResult{}, feedTooLargeError(0, f.maxBytes)
	}

	parsed, err := f.parser.Parse(strings.NewReader(string(contents)))
	if err != nil {
		return FetchResult{}, fmt.Errorf("parse feed: %w", err)
	}
	result.Feed = mapFeed(parsed)
	result.Feed.ETag = result.ETag
	result.Feed.LastModified = result.LastModified
	return result, nil
}

func feedTooLargeError(responseSize, limit int64) error {
	if responseSize > 0 {
		return fmt.Errorf("%w: response size %d bytes exceeds configured limit %d bytes; increase CASTDOCK_MAX_FEED_BYTES", ErrFeedTooLarge, responseSize, limit)
	}
	return fmt.Errorf("%w: response exceeds configured limit %d bytes; increase CASTDOCK_MAX_FEED_BYTES", ErrFeedTooLarge, limit)
}

func mapFeed(feed *gofeed.Feed) podcast.ParsedFeed {
	result := podcast.ParsedFeed{
		Title:       strings.TrimSpace(feed.Title),
		Description: plainText(feed.Description),
		SiteURL:     strings.TrimSpace(feed.Link),
	}
	if feed.Image != nil {
		result.ImageURL = strings.TrimSpace(feed.Image.URL)
	}
	for _, item := range feed.Items {
		episode := podcast.ParsedEpisode{
			GUID:        strings.TrimSpace(item.GUID),
			Title:       strings.TrimSpace(item.Title),
			Description: plainText(firstNonEmpty(item.Content, item.Description)),
			PublishedAt: item.PublishedParsed,
		}
		if episode.PublishedAt == nil {
			episode.PublishedAt = item.UpdatedParsed
		}
		if len(item.Enclosures) > 0 {
			episode.EnclosureURL = strings.TrimSpace(item.Enclosures[0].URL)
			episode.EnclosureType = strings.TrimSpace(item.Enclosures[0].Type)
		}
		if item.Image != nil {
			episode.ImageURL = strings.TrimSpace(item.Image.URL)
		}
		if item.ITunesExt != nil {
			episode.DurationSeconds = parseDuration(item.ITunesExt.Duration)
			if episode.ImageURL == "" {
				episode.ImageURL = strings.TrimSpace(item.ITunesExt.Image)
			}
		}
		if episode.GUID == "" {
			episode.GUID = firstNonEmpty(episode.EnclosureURL, item.Link)
		}
		if episode.GUID == "" {
			sum := sha256.Sum256([]byte(episode.Title + "\x00" + item.Published))
			episode.GUID = "castdock:" + hex.EncodeToString(sum[:])
		}
		result.Episodes = append(result.Episodes, episode)
	}
	return result
}

func plainText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	root, err := xhtml.Parse(strings.NewReader(value))
	if err != nil {
		return value
	}
	var output strings.Builder
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && (node.Data == "script" || node.Data == "style") {
			return
		}
		if node.Type == xhtml.TextNode {
			text := strings.TrimSpace(node.Data)
			if text != "" {
				if output.Len() > 0 {
					output.WriteByte(' ')
				}
				output.WriteString(text)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return strings.Join(strings.Fields(output.String()), " ")
}

func parseDuration(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parts := strings.Split(value, ":")
	if len(parts) == 1 {
		seconds, _ := strconv.Atoi(parts[0])
		return max(seconds, 0)
	}
	total := 0
	for _, part := range parts {
		unit, err := strconv.Atoi(part)
		if err != nil || unit < 0 {
			return 0
		}
		total = total*60 + unit
	}
	return total
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
