package searchpanel

import (
	"strings"
	"testing"
)

// TestResultNoticeShowsOnHint checks the non-fatal search notice (for example a degraded
// remote ignore scope) is shown on the panel's bottom border without failing the search.
func TestResultNoticeShowsOnHint(t *testing.T) {
	p := &Panel{}
	p.resultNotice = "remote ignore scope unavailable: tracked files may be omitted"
	if got := p.hint(); !strings.Contains(got, "remote ignore scope unavailable") {
		t.Fatalf("hint = %q, want the result notice", got)
	}
	p.SetNotice("a status summary")
	if got := p.hint(); got != "a status summary" {
		t.Fatalf("hint = %q, want the transient notice to take precedence", got)
	}
}
