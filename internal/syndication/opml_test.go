package syndication

import (
	"strings"
	"testing"
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
