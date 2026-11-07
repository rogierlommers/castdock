package syndication

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrNoFeeds = errors.New("OPML contains no valid podcast feed URLs")

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
