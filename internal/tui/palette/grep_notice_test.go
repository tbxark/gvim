package palette

import (
	"strings"
	"testing"
)

// TestLiveGrepShowsCompletenessNotice: a remote result's non-fatal Notice/Truncated is
// visible in the palette chrome while the successful hits stay as rows; a stale result
// cannot replace the current query's notice or rows.
func TestLiveGrepShowsCompletenessNotice(t *testing.T) {
	t.Parallel()
	p := New(Options{Live: true, Root: "/remote/w", Remote: true, Files: &fakeFiles{}, Grep: &fakeGrepper{}})
	draw(p, 120, 30)
	p.seq = 2
	p.applyGrep(GrepResultMsg{Seq: 2, Text: "todo", Hits: []GrepHit{
		{Rel: "a.go", Line: 1, Col: 1, Text: "todo a"},
	}, Notice: "remote search degraded", Truncated: true})
	if p.grep.notice != "remote search degraded" || !p.grep.truncated {
		t.Fatalf("grep state = notice %q truncated %v", p.grep.notice, p.grep.truncated)
	}
	g := draw(p, 120, 30)
	if !hasRow(g, "incomplete") || !hasRow(g, "remote search degraded") {
		t.Fatalf("completeness notice is not visible:\n%s", g)
	}
	if !hasRow(g, "a.go:1: todo a") {
		t.Fatalf("successful hits were dropped:\n%s", g)
	}
	// A late result from the previous query must not overwrite the current notice/rows.
	p.applyGrep(GrepResultMsg{Seq: 1, Text: "old", Hits: []GrepHit{{Rel: "old.go", Line: 1, Col: 1, Text: "old"}}, Notice: "old notice"})
	if p.grep.notice != "remote search degraded" || p.grep.count != 1 || hasRow(draw(p, 120, 30), "old.go") {
		t.Fatalf("a stale result replaced the current query's completeness state: %+v", p.grep)
	}
}

// TestLiveGrepQueryChangeClearsCompletenessNotice: typing a new query clears the previous
// result's notice before the new search answers (the old answer is dropped by seq guard).
func TestLiveGrepQueryChangeClearsCompletenessNotice(t *testing.T) {
	t.Parallel()
	p := New(Options{Live: true, Root: "/remote/w", Remote: true, Files: &fakeFiles{}, Grep: &fakeGrepper{}})
	draw(p, 120, 30)
	p.seq = 1
	p.applyGrep(GrepResultMsg{Seq: 1, Text: "todo", Hits: []GrepHit{{Rel: "a.go", Line: 1, Col: 1, Text: "todo"}}, Notice: "degraded", Truncated: true})
	p.update(typed("x"))
	if p.grep.notice != "" || p.grep.truncated {
		t.Fatalf("a query change kept the previous completeness notice: notice %q truncated %v", p.grep.notice, p.grep.truncated)
	}
	if strings.Contains(p.grep.text, "x") {
		t.Fatalf("the old result text was not superseded: %q", p.grep.text)
	}
}
