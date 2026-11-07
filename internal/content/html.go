package content

import (
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

var showNotesPolicy = func() *bluemonday.Policy {
	policy := bluemonday.UGCPolicy()
	policy.RequireNoFollowOnLinks(true)
	policy.RequireNoReferrerOnLinks(true)
	policy.AddTargetBlankToFullyQualifiedLinks(true)
	return policy
}()

// SanitizeShowNotes preserves useful podcast formatting while removing active
// or unsafe publisher content. Call it during ingestion and again before HTML
// rendering so persisted content never becomes an implicit trust boundary.
func SanitizeShowNotes(value string) string {
	return strings.TrimSpace(showNotesPolicy.Sanitize(value))
}
