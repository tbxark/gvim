package palette

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// fakeGrepper records each search and answers with hits, or with err.
type fakeGrepper struct {
	hits   []GrepHit
	err    error
	calls  []string // the query of each search
	roots  []string
	limits []int
	lists  [][]string
}

func (g *fakeGrepper) Search(root string, files []string, query string, limit int) ([]GrepHit, error) {
	g.calls = append(g.calls, query)
	g.roots = append(g.roots, root)
	g.limits = append(g.limits, limit)
	g.lists = append(g.lists, files)
	if g.err != nil {
		return nil, g.err
	}
	var out []GrepHit
	for _, h := range g.hits {
		if strings.Contains(h.Text, query) {
			out = append(out, h)
		}
	}
	return out, nil
}

// liveOptions are the options of a live grep palette over files and g.
func liveOptions(query string, files *fakeFiles, g *fakeGrepper) Options {
	return Options{Live: true, Query: query, Root: "/w", Files: files, Grep: g}
}

// TestLiveGrepSearchesInACommandOverTheFiles checks that the search runs in the
// command the palette returns, over the file list under the root, and that typing
// does not search in Update.
func TestLiveGrepSearchesInACommandOverTheFiles(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{}
	files := &fakeFiles{files: []string{"a.go", "src/b.go"}}
	p := New(liveOptions("", files, g))
	draw(p, 100, 30)
	cmd := p.Update(typed("t"))
	if len(g.calls) != 0 {
		t.Fatalf("typing searched %d times in Update, want 0", len(g.calls))
	}
	if cmd == nil {
		t.Fatal("typing returned no search command")
	}
	cmd()
	if len(g.calls) != 1 || g.calls[0] != "t" {
		t.Fatalf("searches = %v, want one search for t", g.calls)
	}
	if g.roots[0] != "/w" || len(g.lists[0]) != 2 {
		t.Fatalf("search root %q over %v, want /w over both files", g.roots[0], g.lists[0])
	}
	if g.limits[0] != fileLimit {
		t.Fatalf("limit = %d, want %d", g.limits[0], fileLimit)
	}
}

// TestLiveGrepShowsMatchesAndEnterOpensTheLine checks the rows of the hits, and that
// Enter on a row opens its file under the root at the match's line and column.
func TestLiveGrepShowsMatchesAndEnterOpensTheLine(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{hits: []GrepHit{{Rel: "src/b.go", Line: 3, Col: 5, Text: "\tfunc todo() {}"}}}
	files := &fakeFiles{files: []string{"src/b.go"}}
	p := New(liveOptions("", files, g))
	draw(p, 100, 30)
	settle(t, p, p.Update(typed("t")), 4)
	for _, c := range "odo" {
		settle(t, p, p.Update(typed(string(c))), 4)
	}
	g2 := draw(p, 100, 30)
	if !hasRow(g2, "src/b.go:3: func todo() {}") {
		t.Fatalf("hit row not shown:\n%s", g2)
	}
	want := OpenLineMsg{Path: "/w/src/b.go", Line: 3, Col: 5}
	if got := runCmd(p.Update(key(tea.KeyEnter))); got != want {
		t.Fatalf("Enter emitted %#v, want %#v", got, want)
	}
	if p.Active() {
		t.Fatal("palette still open after a match was chosen")
	}
}

// TestLiveGrepEmptyQueryHasNoRowsAndNoSearch checks that an empty query searches
// nothing and shows the live grep empty text.
func TestLiveGrepEmptyQueryHasNoRowsAndNoSearch(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{}
	p := New(liveOptions("", &fakeFiles{files: []string{"a.go"}}, g))
	if cmd := p.Init(); cmd != nil {
		t.Fatal("Init with an empty query returned a search")
	}
	g2 := draw(p, 100, 30)
	if !hasRow(g2, "No matches in files") {
		t.Fatalf("empty live grep does not say so:\n%s", g2)
	}
	if len(g.calls) != 0 {
		t.Fatalf("empty query searched %v", g.calls)
	}
}

// TestLiveGrepResumedQueryRunsAtOpen checks that a palette opened with a query, as
// resume does, searches that query at once.
func TestLiveGrepResumedQueryRunsAtOpen(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{hits: []GrepHit{{Rel: "a.go", Line: 1, Col: 1, Text: "todo"}}}
	p := New(liveOptions("todo", &fakeFiles{files: []string{"a.go"}}, g))
	draw(p, 100, 30)
	settle(t, p, p.Init(), 4)
	if !hasRow(draw(p, 100, 30), "a.go:1: todo") {
		t.Fatal("the resumed query shows no rows")
	}
}

// TestLiveGrepQueryHasNoModePrefix checks that a query that starts with ">" is
// searched as text. Live grep has no prefixes.
func TestLiveGrepQueryHasNoModePrefix(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{}
	p := New(liveOptions(">x", &fakeFiles{}, g))
	settle(t, p, p.Init(), 4)
	if len(g.calls) != 1 || g.calls[0] != ">x" {
		t.Fatalf("searches = %v, want the whole query >x", g.calls)
	}
}

// TestLiveGrepStaleResultIsDropped checks that a result for a query that was typed
// over does not replace the rows of the current query.
func TestLiveGrepStaleResultIsDropped(t *testing.T) {
	t.Parallel()
	p := New(liveOptions("", &fakeFiles{}, &fakeGrepper{}))
	draw(p, 100, 30)
	p.Update(typed("a"))
	p.Update(typed("b"))
	p.Update(GrepResultMsg{Seq: 2, Text: "a", Hits: []GrepHit{{Rel: "old.go", Line: 1, Col: 1, Text: "a"}}})
	if hasRow(draw(p, 100, 30), "old.go") {
		t.Fatal("a stale result replaced the rows of the current query")
	}
}

// TestLiveGrepErrorIsShownAsARow checks that a failed search shows its error in the
// box, not as an empty result.
func TestLiveGrepErrorIsShownAsARow(t *testing.T) {
	t.Parallel()
	p := New(liveOptions("", &fakeFiles{}, &fakeGrepper{}))
	draw(p, 100, 30)
	p.Update(typed("a"))
	p.Update(GrepResultMsg{Seq: p.seq, Text: "a", Err: errors.New("read failed")})
	if !hasRow(draw(p, 100, 30), "Live Grep: read failed") {
		t.Fatal("the search error is not shown")
	}
}

// Live Grep has no mode prefixes, so its hint row must not advertise the quick-open ones (PC54).
func TestLiveGrepHintRowAdvertisesNoPrefixes(t *testing.T) {
	t.Parallel()
	p := New(liveOptions("", &fakeFiles{}, &fakeGrepper{}))
	g := draw(p, 100, 30)
	foot := rowText(g, layoutFor(100, 30).footY)
	if strings.Contains(foot, ">command") || strings.Contains(foot, "#buffer") || strings.Contains(foot, ":line") {
		t.Errorf("hint row %q lists quick-open prefixes", foot)
	}
	if !strings.Contains(foot, "Enter open") {
		t.Errorf("hint row %q does not say what Enter does", foot)
	}
	q := draw(New(Options{Query: ">save"}), 100, 30)
	if got := rowText(q, layoutFor(100, 30).footY); !strings.Contains(got, ">command") {
		t.Errorf("the quick-open hint row changed: %q", got)
	}
}
