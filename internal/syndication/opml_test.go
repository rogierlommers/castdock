package syndication

import (
	"strings"
	"testing"

	"github.com/rlommers/castdock/internal/podcast"
)

func TestParseOPMLNestedAndDeduplicated(t *testing.T) {
	document := `<?xml version="1.0"?><opml version="2.0"><body>
		<outline text="Folder">
			<outline type="rss" xmlUrl="https://example.com/one.xml"/>
			<outline type="rss" xmlUrl="https://example.com/one.xml#fragment"/>
		</outline>
		<outline type="rss" xmlUrl="https://example.com/two.xml"/>
	</body></opml>`
	urls, err := ParseOPML(strings.NewReader(document))
	if err != nil {
		t.Fatalf("ParseOPML() error = %v", err)
	}
	if len(urls) != 2 || urls[0] != "https://example.com/one.xml" || urls[1] != "https://example.com/two.xml" {
		t.Fatalf("ParseOPML() = %#v", urls)
	}
}

func TestParseOPMLRejectsDocumentWithoutFeeds(t *testing.T) {
	_, err := ParseOPML(strings.NewReader(`<opml><body><outline text="Empty"/></body></opml>`))
	if err == nil {
		t.Fatal("ParseOPML() expected error")
	}
}

func TestExportOPMLRoundTrip(t *testing.T) {
	feeds := []podcast.Feed{
		{Title: "One & Only", URL: "https://example.com/one.xml?x=1&y=2", SiteURL: "https://example.com/one"},
		{URL: "https://example.com/two.xml"},
	}
	contents, err := ExportOPML(feeds)
	if err != nil {
		t.Fatalf("ExportOPML() error = %v", err)
	}
	if !strings.Contains(string(contents), `title="One &amp; Only"`) || !strings.Contains(string(contents), `<title>CastDock subscriptions</title>`) {
		t.Fatalf("ExportOPML() = %s", contents)
	}
	urls, err := ParseOPML(strings.NewReader(string(contents)))
	if err != nil {
		t.Fatalf("ParseOPML(export) error = %v", err)
	}
	if len(urls) != 2 || urls[0] != feeds[0].URL || urls[1] != feeds[1].URL {
		t.Fatalf("round-trip URLs = %#v", urls)
	}
}
