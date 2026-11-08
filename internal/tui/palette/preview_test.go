package palette

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// previewRead is one file the fake previewer answers for.
type previewRead struct {
	data      string
	truncated bool
	err       error
}

// fakePreviewer answers ReadHead with the content of files, keyed by the absolute path,
// and counts the reads.
type fakePreviewer struct {
	files map[string]previewRead
	paths atomic.Int32
	reads []string
}

func (f *fakePreviewer) ReadHead(path string, maxBytes int) ([]byte, bool, error) {
	f.paths.Add(1)
	f.reads = append(f.reads, path)
	r := f.files[path]
	if len(r.data) > maxBytes {
		return []byte(r.data[:maxBytes]), true, r.err
	}
	return []byte(r.data), r.truncated, r.err
}

// fileOptions are the options of a file-mode palette over files, ranked by path
// substring, with the previewer pv.
func fileOptions(query string, files []string, pv Previewer) Options {
	return Options{Query: query, Root: "/w", Files: &fakeFiles{files: files}, Ranker: &fakeRanker{}, Preview: pv}
}

// shownPreview returns the preview rows the palette paints in a wide frame, trailing
// blanks removed. The rows run from the card's left border to its right one: the list
// columns, the divider and the preview text.
func shownPreview(t *testing.T, p *Palette) []string {
	t.Helper()
	geo := p.layout(120, 30)
	if !geo.preview {
		t.Fatal("the frame is wide enough, but the layout has no preview column")
	}
	g := draw(p, 120, 30)
	out := make([]string, 0, geo.rows)
	for k := range geo.rows {
		out = append(out, rowText(g, geo.rowsTop+k))
	}
	return out
}

// TestPreviewColumnShowsTheSelectedFile: the file mode reads the selected row's head in
// a command, paints it with a line-number gutter in a wide frame, and paints no column
// in a narrow one. Nothing is read in Update.
func TestPreviewColumnShowsTheSelectedFile(t *testing.T) {
	t.Parallel()
	pv := &fakePreviewer{files: map[string]previewRead{
		"/w/a.go": {data: "package main\n\nfunc main() {}\n"},
	}}
	p := New(fileOptions("", []string{"a.go"}, pv))
	if got := pv.paths.Load(); got != 0 {
		t.Fatalf("New read %d previews, want 0", got)
	}
	// The rank result starts the preview read, and the read's message fills the column.
	settle(t, p, p.Init(), 8)
	if got := pv.paths.Load(); got != 1 {
		t.Fatalf("preview reads = %d, want 1", got)
	}
	lines := shownPreview(t, p)
	if len(lines) < 3 || !strings.Contains(lines[0], "1 │ package main") || !strings.Contains(lines[2], "3 │ func main() {}") {
		t.Fatalf("preview lines = %q", lines)
	}
	// A narrow frame keeps the old full-width list and paints no preview.
	if geo := p.layout(60, 30); geo.preview {
		t.Fatalf("a 60-column frame paints the preview column: %+v", geo)
	}
}

// TestPreviewDoesNotReRankOrResizeTheRows: the preview reads happen beside the ranking,
// never through it: the row set, the rank limit and the ranker's call count stay those
// of the query.
func TestPreviewDoesNotReRankOrResizeTheRows(t *testing.T) {
	t.Parallel()
	pv := &fakePreviewer{files: map[string]previewRead{
		"/w/a.go": {data: "a\n"},
		"/w/b.go": {data: "b\n"},
	}}
	ranker := &countingRanker{}
	p := New(Options{Query: "", Root: "/w", Files: &fakeFiles{files: []string{"a.go", "b.go"}}, Ranker: ranker, Preview: pv})
	settle(t, p, p.Init(), 8)
	if got := ranker.calls.Load(); got != 1 {
		t.Fatalf("rank calls after open = %d, want 1", got)
	}
	if ranker.limit != fileLimit {
		t.Fatalf("rank limit = %d, want %d", ranker.limit, fileLimit)
	}
	rows := len(p.rows)
	settle(t, p, p.update(key(tea.KeyDown)), 8)
	if len(p.rows) != rows {
		t.Fatalf("rows = %d after a preview read, want %d", len(p.rows), rows)
	}
	if got := ranker.calls.Load(); got != 1 {
		t.Fatalf("rank calls after a preview read = %d, want 1", got)
	}
}

// TestPreviewFollowsTheHighlightAndCaches: moving the highlight reads the new file once;
// coming back to a file already read serves the cached preview and reads nothing.
func TestPreviewFollowsTheHighlightAndCaches(t *testing.T) {
	t.Parallel()
	pv := &fakePreviewer{files: map[string]previewRead{
		"/w/a.go": {data: "first file\n"},
		"/w/b.go": {data: "second file\n"},
	}}
	p := New(fileOptions("", []string{"a.go", "b.go"}, pv))
	settle(t, p, p.Init(), 8)
	if got := pv.paths.Load(); got != 1 {
		t.Fatalf("reads after open = %d, want 1", got)
	}
	settle(t, p, p.update(key(tea.KeyDown)), 8)
	if lines := shownPreview(t, p); !strings.Contains(lines[0], "second file") {
		t.Fatalf("preview after Down = %q, want the second file", lines)
	}
	if got := pv.paths.Load(); got != 2 {
		t.Fatalf("reads after the move = %d, want 2", got)
	}
	settle(t, p, p.update(key(tea.KeyUp)), 8)
	if lines := shownPreview(t, p); !strings.Contains(lines[0], "first file") {
		t.Fatalf("preview after Up = %q, want the cached first file", lines)
	}
	if got := pv.paths.Load(); got != 2 {
		t.Fatalf("reads after returning = %d, want the cached 2", got)
	}
}

// TestPreviewStaleReadIsDropped: a read that answers a highlight that has already moved
// does not replace the new file's preview.
func TestPreviewStaleReadIsDropped(t *testing.T) {
	t.Parallel()
	pv := &fakePreviewer{files: map[string]previewRead{
		"/w/a.go": {data: "first file\n"},
		"/w/b.go": {data: "second file\n"},
	}}
	p := New(fileOptions("", []string{"a.go", "b.go"}, pv))
	settle(t, p, p.Init(), 8)  // reads a.go and shows it
	p.update(key(tea.KeyDown)) // reads b.go, not run yet
	bSeq, bRel := p.preview.seq, p.preview.rel
	p.update(key(tea.KeyDown)) // at the end of the list, nothing new to read
	p.applyPreview(previewMsg{Seq: bSeq, Rel: bRel, Preview: Preview{Lines: []string{"late"}}})
	if !p.preview.ready || p.preview.rel != "b.go" {
		t.Fatalf("preview state = %+v, want the b.go read", p.preview)
	}
	// A message from an older read is dropped.
	p.applyPreview(previewMsg{Seq: bSeq - 1, Rel: "a.go", Preview: Preview{Lines: []string{"stale"}}})
	if strings.Contains(strings.Join(shownPreview(t, p), "\n"), "stale") {
		t.Fatal("a stale preview replaced the shown one")
	}
}

// TestPreviewDegradesForBinaryLargeAndUnreadableFiles checks the placeholders: a binary
// head, a read error and a file that continues.
func TestPreviewDegradesForBinaryLargeAndUnreadableFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		read previewRead
		want string
	}{
		{"binary", previewRead{data: "ab\x00cd"}, previewBinary},
		{"unreadable", previewRead{err: errors.New("permission denied")}, previewUnavailable},
		{"empty", previewRead{data: ""}, previewNoLines},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pv := &fakePreviewer{files: map[string]previewRead{"/w/a.txt": tt.read}}
			p := New(fileOptions("", []string{"a.txt"}, pv))
			settle(t, p, p.Init(), 8)
			if lines := shownPreview(t, p); len(lines) == 0 || !strings.Contains(lines[0], tt.want) {
				t.Fatalf("preview lines = %q, want %q", lines, tt.want)
			}
		})
	}
	// A file that continues ends with the more mark on the row after the last line.
	pv := &fakePreviewer{files: map[string]previewRead{"/w/a.txt": {data: "one\ntwo\n", truncated: true}}}
	p := New(fileOptions("", []string{"a.txt"}, pv))
	settle(t, p, p.Init(), 8)
	lines := shownPreview(t, p)
	if len(lines) < 3 || !strings.Contains(lines[2], previewMore) {
		t.Fatalf("truncated preview lines = %q, want the more mark", lines)
	}
}

// TestFileQueryLineTargetHighlightsAndCommits: a "path:line[:col]" query ranks the path,
// highlights that line in the preview, and Enter opens the file at the position.
func TestFileQueryLineTargetHighlightsAndCommits(t *testing.T) {
	t.Parallel()
	pv := &fakePreviewer{files: map[string]previewRead{
		"/w/a.go": {data: "one\ntwo\nthree\n"},
	}}
	p := New(fileOptions("a.go:2", []string{"a.go"}, pv))
	settle(t, p, p.Init(), 8)
	if p.fileLine != 2 || p.fileCol != 0 || p.fileText != "a.go" {
		t.Fatalf("target = %q:%d:%d, want a.go:2", p.fileText, p.fileLine, p.fileCol)
	}
	geo := p.layout(120, 30)
	g := draw(p, 120, 30)
	if !geo.preview {
		t.Fatal("no preview column")
	}
	// The target line is painted with the selected style, the others with the panel's.
	if g.At(geo.prevX, geo.rowsTop+1).Style != p.chrome.selected {
		t.Errorf("the target line is not highlighted")
	}
	if g.At(geo.prevX, geo.rowsTop).Style != p.chrome.panel {
		t.Errorf("a line before the target is highlighted")
	}
	if got := runCmd(p.update(key(tea.KeyEnter))); got != (OpenFileMsg{Path: "/w/a.go", Rel: "a.go", Line: 2}) {
		t.Fatalf("Enter emitted %#v, want the line target", got)
	}
	// ":line:col" names both.
	path, line, col := parseFileTarget("src/a.go:12:3")
	if path != "src/a.go" || line != 12 || col != 3 {
		t.Fatalf("parseFileTarget = %q, %d, %d", path, line, col)
	}
	if path, line, col := parseFileTarget("a.go"); path != "a.go" || line != 0 || col != 0 {
		t.Fatalf("a query with no target = %q, %d, %d", path, line, col)
	}
}

// TestNoPreviewWithoutAReaderOrOutsideTheFileMode: the column needs a reader and the
// file mode; other modes and a palette without a reader paint the plain sheet.
func TestNoPreviewWithoutAReaderOrOutsideTheFileMode(t *testing.T) {
	t.Parallel()
	p := New(Options{Query: "", Files: &fakeFiles{files: []string{"a.go"}}, Ranker: &fakeRanker{}})
	settle(t, p, p.Init(), 8)
	if geo := p.layout(120, 30); geo.preview {
		t.Fatalf("a palette without a Previewer paints a preview column: %+v", geo)
	}
	commands := New(Options{Query: ">save", Commands: fakeCommands(3)})
	if geo := commands.layout(120, 30); geo.preview {
		t.Fatalf("the command mode paints a preview column: %+v", geo)
	}
}

// TestPreviewColumnIsNotARow: a click in the preview column selects no row, so the
// highlight stays where it is, and the card still covers the column.
func TestPreviewColumnIsNotARow(t *testing.T) {
	t.Parallel()
	pv := &fakePreviewer{files: map[string]previewRead{"/w/a.go": {data: "one\n"}}}
	p := New(fileOptions("", []string{"a.go", "b.go"}, pv))
	settle(t, p, p.Init(), 8)
	draw(p, 120, 30)
	geo := p.layout(120, 30)
	if !p.Covers(geo.prevX, geo.rowsTop, 120, 30) {
		t.Fatal("the card does not cover its preview column")
	}
	if i, ok := p.rowAt(geo.prevX, geo.rowsTop); ok {
		t.Fatalf("a click on the preview column hit row %d", i)
	}
}
