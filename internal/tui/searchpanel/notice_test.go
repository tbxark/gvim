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

// TestBatchNoticeKeepsResults checks the streaming seam keeps a partial result when the
// batch carries a non-fatal notice (for example a truncated remote candidate listing):
// the rows stay, the notice is on the hint, and the Truncated flag reaches Stats.
func TestBatchNoticeKeepsResults(t *testing.T) {
	p := New(Options{})
	p.seq = 7
	p.loading = true
	p.applyBatch(searchBatchMsg{seq: 7, ok: true, batch: SearchBatch{
		Files:     []FileMatches{{Path: "/w/a.go", Name: "a.go", Matches: []Match{{Line: 1, Col: 1, Len: 4, Text: "todo"}}}},
		Notice:    "remote ignore scope incomplete: the remote git candidate listing was truncated",
		Truncated: true,
		Done:      true,
	}})
	if len(p.fileMatches) != 1 || p.hint() == "" || !strings.Contains(p.hint(), "candidate listing was truncated") {
		t.Fatalf("panel files = %v hint = %q, want the notice with retained results", p.fileMatches, p.hint())
	}
	if matches, files, truncated := p.Stats(); matches != 1 || files != 1 || !truncated {
		t.Fatalf("Stats = %d %d %v, want 1 1 true", matches, files, truncated)
	}
}
