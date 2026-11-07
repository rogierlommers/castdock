package syndication

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/rlommers/castdock/internal/podcast"
)

var ErrNoFeeds = errors.New("OPML contains no valid podcast feed URLs")

type opmlExportDocument struct {
	XMLName xml.Name       `xml:"opml"`
	Version string         `xml:"version,attr"`
	Head    opmlExportHead `xml:"head"`
	Body    opmlExportBody `xml:"body"`
}

type opmlExportHead struct {
	Title string `xml:"title"`
}

type opmlExportBody struct {
	Outlines []opmlExportOutline `xml:"outline"`
}

type opmlExportOutline struct {
	Text    string `xml:"text,attr"`
	Title   string `xml:"title,attr"`
	Type    string `xml:"type,attr"`
	XMLURL  string `xml:"xmlUrl,attr"`
	HTMLURL string `xml:"htmlUrl,attr,omitempty"`
}

type opmlDocument struct {
	Body opmlBody `xml:"body"`
}

type opmlBody struct {
	Outlines []opmlOutline `xml:"outline"`
}

type opmlOutline struct {
	XMLURL   string        `xml:"xmlUrl,attr"`
	Outlines []opmlOutline `xml:"outline"`
}

func ParseOPML(reader io.Reader) ([]string, error) {
	decoder := xml.NewDecoder(reader)
	decoder.Strict = true
	var document opmlDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("parse OPML: %w", err)
	}
	seen := make(map[string]struct{})
	var urls []string
	var visit func([]opmlOutline)
	visit = func(outlines []opmlOutline) {
		for _, outline := range outlines {
			if raw := strings.TrimSpace(outline.XMLURL); raw != "" {
				normalized, err := NormalizeURL(raw)
				if err == nil {
					if _, exists := seen[normalized]; !exists {
						seen[normalized] = struct{}{}
						urls = append(urls, normalized)
					}
				}
			}
			visit(outline.Outlines)
		}
	}
	visit(document.Body.Outlines)
	if len(urls) == 0 {
		return nil, ErrNoFeeds
	}
	return urls, nil
}

func ExportOPML(feeds []podcast.Feed) ([]byte, error) {
	document := opmlExportDocument{Version: "2.0", Head: opmlExportHead{Title: "CastDock subscriptions"}}
	for _, feed := range feeds {
		title := strings.TrimSpace(feed.Title)
		if title == "" {
			title = feed.URL
		}
		document.Body.Outlines = append(document.Body.Outlines, opmlExportOutline{
			Text: title, Title: title, Type: "rss", XMLURL: feed.URL, HTMLURL: feed.SiteURL,
		})
	}
	contents, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("export OPML: %w", err)
	}
	return append([]byte(xml.Header), contents...), nil
}
