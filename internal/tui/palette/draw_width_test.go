package palette

import (
	"strings"
	"testing"
)

// TestGrepRowShowsTheMatchedTextInAWideFrame checks that a grep row is cut at the
// frame edge and not at the shortcut column, so the line number and the matched
// text are visible after a long path.
func TestGrepRowShowsTheMatchedTextInAWideFrame(t *testing.T) {
	t.Parallel()
	rel := "internal/search/some/deeply/nested/package/search.go"
	g := &fakeGrepper{hits: []GrepHit{{Rel: rel, Line: 103, Col: 1, Text: "compileCache()"}}}
	p := New(liveOptions("", &fakeFiles{files: []string{rel}}, g))
	draw(p, 120, 30)
	settle(t, p, p.update(typed("c")), 4)
	for _, c := range "ompileCache" {
		settle(t, p, p.update(typed(string(c))), 4)
	}
	if got := draw(p, 120, 30); !hasRow(got, rel+":103: compileCache()") {
		t.Fatalf("grep row is cut:\n%s", got)
	}
}

// TestFileRowShowsTheWholePathBeforeTheSourceLabel checks that a file row keeps its
// path up to the "file" label when the frame has room for it.
func TestFileRowShowsTheWholePathBeforeTheSourceLabel(t *testing.T) {
	t.Parallel()
	name := "plugin/bundled/plugins/git_gutter/main.lua"
	p := New(Options{Files: &fakeFiles{files: []string{name}}, Ranker: &fakeRanker{}})
	settle(t, p, p.Init(), 4)
	got := draw(p, 120, 30)
	var line string
	for _, l := range got.Plain() {
		if strings.Contains(l, name) {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("file row is cut:\n%s", got)
	}
	line = strings.TrimSuffix(strings.TrimRight(line, " "), "│")
	if !strings.HasSuffix(strings.TrimRight(line, " "), "file") {
		t.Errorf("row %q lost its file label", line)
	}
}

// TestNameEnd checks where each kind of row cuts its name.
func TestNameEnd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		w    int
		r    row
		want int
	}{
		{"command stops before the shortcut column", 120, row{kind: rowCommand, name: "x"}, 51},
		{"row with keys stops before the shortcut column", 120, row{kind: rowFile, keys: "Ctrl+S"}, 51},
		{"file stops before its label", 120, row{kind: rowFile, source: "file"}, 111},
		{"grep runs to the end of the text area", 120, row{kind: rowGrep}, 117},
		{"narrow frame has no label", 40, row{kind: rowFile, source: "file"}, 38},
	}
	for _, tt := range tests {
		if got := nameEnd(layoutFor(tt.w, 30), tt.r); got != tt.want {
			t.Errorf("%s: nameEnd = %d, want %d", tt.name, got, tt.want)
		}
	}
}
