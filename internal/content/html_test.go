package content

import (
	"strings"
	"testing"
)

func TestSanitizeShowNotesPreservesFormattingAndRemovesActiveContent(t *testing.T) {
	raw := `<p onclick="steal()">Hello <strong>listener</strong>.</p><ul><li>First</li></ul><a href="https://example.com">Source</a><a href="javascript:alert(1)">Bad</a><script>alert(2)</script>`
	got := SanitizeShowNotes(raw)
	for _, expected := range []string{"<p>", "<strong>listener</strong>", "<ul>", "<li>First</li>", `href="https://example.com"`} {
		if !strings.Contains(got, expected) {
			t.Errorf("sanitized show notes missing %q: %s", expected, got)
		}
	}
	for _, forbidden := range []string{"onclick", "javascript:", "<script", "alert(2)"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("sanitized show notes contain %q: %s", forbidden, got)
		}
	}
}
