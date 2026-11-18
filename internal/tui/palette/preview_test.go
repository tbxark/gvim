package palette

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
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
	p.applyPreview(previewMsg{Token: p.token, Seq: bSeq, Rel: bRel, Preview: Preview{Lines: []string{"late"}}})
	if !p.preview.ready || p.preview.rel != "b.go" {
		t.Fatalf("preview state = %+v, want the b.go read", p.preview)
	}
	// A message from an older read is dropped.
	p.applyPreview(previewMsg{Token: p.token, Seq: bSeq - 1, Rel: "a.go", Preview: Preview{Lines: []string{"stale"}}})
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
		{"binary", previewRead{data: "ab\x00cd"}, previewBinaryText()},
		{"unreadable", previewRead{err: errors.New("permission denied")}, previewUnavailableText()},
		{"empty", previewRead{data: ""}, previewNoLinesText()},
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

// wheelAt is one notch of the wheel at (x, y).
func wheelAt(x, y int, b tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg(tea.Mouse{X: x, Y: y, Button: b})
}

// TestPreviewExpandedCapacity: preview loads up to 128 lines for files > 128 lines (marked truncated),
// and keeps all lines for files <= 128 lines (not truncated).
func TestPreviewExpandedCapacity(t *testing.T) {
	t.Parallel()

	// 1. File with 150 lines (> 128 lines)
	var lines150 []string
	for i := 1; i <= 150; i++ {
		lines150 = append(lines150, fmt.Sprintf("line %d", i))
	}
	content150 := strings.Join(lines150, "\n") + "\n"

	// 2. File with exactly 128 lines
	var lines128 []string
	for i := 1; i <= 128; i++ {
		lines128 = append(lines128, fmt.Sprintf("line %d", i))
	}
	content128 := strings.Join(lines128, "\n") + "\n"

	// 3. File with 50 lines (< 128 lines)
	var lines50 []string
	for i := 1; i <= 50; i++ {
		lines50 = append(lines50, fmt.Sprintf("line %d", i))
	}
	content50 := strings.Join(lines50, "\n") + "\n"

	pv := &fakePreviewer{files: map[string]previewRead{
		"/w/large.txt": {data: content150},
		"/w/exact.txt": {data: content128},
		"/w/small.txt": {data: content50},
	}}

	// Test large file (>128 lines)
	pLarge := New(fileOptions("", []string{"large.txt"}, pv))
	settle(t, pLarge, pLarge.Init(), 8)
	if !pLarge.preview.ready {
		t.Fatal("large.txt preview not ready")
	}
	if got := len(pLarge.preview.data.Lines); got != 128 {
		t.Fatalf("large.txt lines = %d, want 128", got)
	}
	if !pLarge.preview.data.Truncated {
		t.Fatal("large.txt truncated = false, want true")
	}

	// Test exact 128 lines
	pExact := New(fileOptions("", []string{"exact.txt"}, pv))
	settle(t, pExact, pExact.Init(), 8)
	if !pExact.preview.ready {
		t.Fatal("exact.txt preview not ready")
	}
	if got := len(pExact.preview.data.Lines); got != 128 {
		t.Fatalf("exact.txt lines = %d, want 128", got)
	}
	if pExact.preview.data.Truncated {
		t.Fatal("exact.txt truncated = true, want false")
	}

	// Test small file (<128 lines)
	pSmall := New(fileOptions("", []string{"small.txt"}, pv))
	settle(t, pSmall, pSmall.Init(), 8)
	if !pSmall.preview.ready {
		t.Fatal("small.txt preview not ready")
	}
	if got := len(pSmall.preview.data.Lines); got != 50 {
		t.Fatalf("small.txt lines = %d, want 50", got)
	}
	if pSmall.preview.data.Truncated {
		t.Fatal("small.txt truncated = true, want false")
	}
}

// TestPreviewSyntaxHighlighting verifies syntax coloring on .go files with theme styling assertions.
func TestPreviewSyntaxHighlighting(t *testing.T) {
	t.Parallel()
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatalf("failed to load dark theme: %v", err)
	}

	pv := &fakePreviewer{files: map[string]previewRead{
		"/w/main.go": {data: "package main\n\nfunc main() {}\n"},
	}}
	opts := fileOptions("", []string{"main.go"}, pv)
	opts.Theme = th
	p := New(opts)
	settle(t, p, p.Init(), 8)

	if !p.preview.ready {
		t.Fatal("main.go preview not ready")
	}
	if len(p.preview.data.Spans) < 3 {
		t.Fatalf("expected spans for lines, got %d", len(p.preview.data.Spans))
	}

	geo := p.layout(120, 30)
	if !geo.preview {
		t.Fatal("preview column inactive")
	}
	g := draw(p, 120, 30)

	// Gutter on row geo.rowsTop is "1 │ "
	gutterText := "1 │ "
	packageCellX := geo.prevX + len(gutterText)
	cell := g.At(packageCellX, geo.rowsTop)

	// Assert keyword style FG matches theme's syntax[KindKeyword].FG
	kwStyle := p.chrome.syntax[syntax.KindKeyword]
	if cell.Style.FG != kwStyle.FG {
		t.Fatalf("package keyword FG = %+v, want %+v", cell.Style.FG, kwStyle.FG)
	}
	// Assert gutter cell uses chrome.panel style
	gutterCell := g.At(geo.prevX, geo.rowsTop)
	if gutterCell.Style != p.chrome.panel {
		t.Fatalf("gutter cell style = %+v, want panel style %+v", gutterCell.Style, p.chrome.panel)
	}
}

// TestPreviewKeyboardScrolling verifies Alt+Up/Down, PageUp/Down, and boundary clamping.
func TestPreviewKeyboardScrolling(t *testing.T) {
	t.Parallel()

	var lines []string
	for i := 1; i <= 60; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	content := strings.Join(lines, "\n") + "\n"

	pv := &fakePreviewer{files: map[string]previewRead{
		"/w/scroll.txt": {data: content},
	}}
	p := New(fileOptions("", []string{"scroll.txt"}, pv))
	settle(t, p, p.Init(), 8)
	draw(p, 120, 30)

	geo := p.layout(120, 30)
	if !geo.preview || geo.rows <= 0 {
		t.Fatal("preview column inactive")
	}
	visibleRows := geo.rows
	maxScroll := 60 - visibleRows

	if p.preview.scrollOffset != 0 {
		t.Fatalf("initial scrollOffset = %d, want 0", p.preview.scrollOffset)
	}

	// Alt+Down scrolls down by 1
	p.update(altKey(tea.KeyDown))
	if p.preview.scrollOffset != 1 {
		t.Fatalf("scrollOffset after Alt+Down = %d, want 1", p.preview.scrollOffset)
	}
	rows := shownPreview(t, p)
	if !strings.Contains(rows[0], "line 02") {
		t.Fatalf("expected top row to show line 02, got %q", rows[0])
	}

	// Alt+Up scrolls back up by 1
	p.update(altKey(tea.KeyUp))
	if p.preview.scrollOffset != 0 {
		t.Fatalf("scrollOffset after Alt+Up = %d, want 0", p.preview.scrollOffset)
	}
	rows = shownPreview(t, p)
	if !strings.Contains(rows[0], "line 01") {
		t.Fatalf("expected top row to show line 01, got %q", rows[0])
	}

	// Alt+Up at offset 0 clamps at 0
	p.update(altKey(tea.KeyUp))
	if p.preview.scrollOffset != 0 {
		t.Fatalf("scrollOffset clamped at top = %d, want 0", p.preview.scrollOffset)
	}

	// PageDown scrolls by visibleRows
	p.update(key(tea.KeyPgDown))
	expected := min(visibleRows, maxScroll)
	if p.preview.scrollOffset != expected {
		t.Fatalf("scrollOffset after PageDown = %d, want %d", p.preview.scrollOffset, expected)
	}

	// PageDown multiple times clamps at maxScroll
	for i := 0; i < 5; i++ {
		p.update(key(tea.KeyPgDown))
	}
	if p.preview.scrollOffset != maxScroll {
		t.Fatalf("scrollOffset clamped at maxScroll = %d, want %d", p.preview.scrollOffset, maxScroll)
	}

	// PageUp scrolls up
	p.update(key(tea.KeyPgUp))
	expectedUp := max(0, maxScroll-visibleRows)
	if p.preview.scrollOffset != expectedUp {
		t.Fatalf("scrollOffset after PageUp = %d, want %d", p.preview.scrollOffset, expectedUp)
	}

	// PageUp clamps at 0
	for i := 0; i < 5; i++ {
		p.update(key(tea.KeyPgUp))
	}
	if p.preview.scrollOffset != 0 {
		t.Fatalf("scrollOffset after PageUp clamp = %d, want 0", p.preview.scrollOffset)
	}

	// Alt+PgDown and Alt+PgUp
	p.update(altKey(tea.KeyPgDown))
	if p.preview.scrollOffset != expected {
		t.Fatalf("scrollOffset after Alt+PgDown = %d, want %d", p.preview.scrollOffset, expected)
	}
	p.update(altKey(tea.KeyPgUp))
	if p.preview.scrollOffset != 0 {
		t.Fatalf("scrollOffset after Alt+PgUp = %d, want 0", p.preview.scrollOffset)
	}

	// List candidate selection must remain at 0 throughout
	if p.nav.Selected() != 0 {
		t.Fatalf("candidate selection changed to %d, want 0", p.nav.Selected())
	}
}

// TestPreviewMouseWheelScrolling verifies wheel inside preview pane vs inside candidate list.
func TestPreviewMouseWheelScrolling(t *testing.T) {
	t.Parallel()

	var lines []string
	for i := 1; i <= 60; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	content := strings.Join(lines, "\n") + "\n"

	// Create multiple candidate files so candidate list can scroll too
	var files []string
	fileMap := make(map[string]previewRead)
	for i := 1; i <= 50; i++ {
		name := fmt.Sprintf("file_%02d.txt", i)
		files = append(files, name)
		fileMap["/w/"+name] = previewRead{data: content}
	}

	pv := &fakePreviewer{files: fileMap}
	p := New(fileOptions("", files, pv))
	settle(t, p, p.Init(), 8)
	draw(p, 120, 30)

	geo := p.layout(120, 30)
	if !geo.preview {
		t.Fatal("preview column inactive")
	}

	// 1. Wheel down inside preview pane (X = geo.prevX + 2, Y = geo.rowsTop + 1)
	previewX := geo.prevX + 2
	previewY := geo.rowsTop + 1
	p.update(wheelAt(previewX, previewY, tea.MouseWheelDown))

	if p.preview.scrollOffset != wheelRows {
		t.Fatalf("preview scrollOffset after wheel down = %d, want %d", p.preview.scrollOffset, wheelRows)
	}
	// Candidate list should not have scrolled
	first, _ := p.nav.Window()
	if first != 0 {
		t.Fatalf("candidate list scrolled to %d, want 0", first)
	}

	// 2. Wheel up inside preview pane
	p.update(wheelAt(previewX, previewY, tea.MouseWheelUp))
	if p.preview.scrollOffset != 0 {
		t.Fatalf("preview scrollOffset after wheel up = %d, want 0", p.preview.scrollOffset)
	}

	// 3. Wheel down inside candidate list pane (X = geo.nameX, Y = geo.rowsTop + 1)
	listX := geo.nameX
	listY := geo.rowsTop + 1
	p.update(wheelAt(listX, listY, tea.MouseWheelDown))

	// Preview scroll offset should remain unchanged
	if p.preview.scrollOffset != 0 {
		t.Fatalf("preview scrollOffset changed to %d on list wheel, want 0", p.preview.scrollOffset)
	}
	// Candidate list should have scrolled by wheelRows
	first, _ = p.nav.Window()
	if first != wheelRows {
		t.Fatalf("candidate list window first = %d, want %d", first, wheelRows)
	}
}

// TestPreviewSelectionChangeResetsScrollOffset verifies scrollOffset resets to 0 when selection moves.
func TestPreviewSelectionChangeResetsScrollOffset(t *testing.T) {
	t.Parallel()

	var lines []string
	for i := 1; i <= 60; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	content := strings.Join(lines, "\n") + "\n"

	pv := &fakePreviewer{files: map[string]previewRead{
		"/w/a.txt": {data: content},
		"/w/b.txt": {data: content},
	}}
	p := New(fileOptions("", []string{"a.txt", "b.txt"}, pv))
	settle(t, p, p.Init(), 8)
	draw(p, 120, 30)

	// Scroll a.txt down
	for i := 0; i < 5; i++ {
		p.update(altKey(tea.KeyDown))
	}
	if p.preview.scrollOffset != 5 {
		t.Fatalf("scrollOffset before move = %d, want 5", p.preview.scrollOffset)
	}

	// Move selection to b.txt
	settle(t, p, p.update(key(tea.KeyDown)), 8)
	if p.preview.rel != "b.txt" {
		t.Fatalf("selected rel = %q, want b.txt", p.preview.rel)
	}
	if p.preview.scrollOffset != 0 {
		t.Fatalf("b.txt scrollOffset = %d, want 0", p.preview.scrollOffset)
	}

	// Scroll b.txt down
	for i := 0; i < 3; i++ {
		p.update(altKey(tea.KeyDown))
	}
	if p.preview.scrollOffset != 3 {
		t.Fatalf("b.txt scrollOffset = %d, want 3", p.preview.scrollOffset)
	}

	// Move back to cached a.txt
	settle(t, p, p.update(key(tea.KeyUp)), 8)
	if p.preview.rel != "a.txt" {
		t.Fatalf("selected rel = %q, want a.txt", p.preview.rel)
	}
	// Returning to cached file must also reset scrollOffset to 0
	if p.preview.scrollOffset != 0 {
		t.Fatalf("cached a.txt scrollOffset = %d, want 0", p.preview.scrollOffset)
	}
}

func TestPreviewTargetLineContrastAndWideRuneClipping(t *testing.T) {
	t.Parallel()
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	cjk := "func 测() { /* " + strings.Repeat("汉", 40) + " */ }"
	pv := &fakePreviewer{files: map[string]previewRead{
		"/w/main.go": {data: "package main\n" + cjk + "\n"},
	}}
	opts := fileOptions("main.go:2", []string{"main.go"}, pv)
	opts.Theme = th
	p := New(opts)
	settle(t, p, p.Init(), 8)
	geo := p.layout(100, 24)
	if !geo.preview {
		t.Fatal("preview column inactive")
	}
	g := draw(p, 100, 24)
	row := geo.rowsTop + 1
	cellX := geo.prevX + len("2 │ ")
	cell := g.At(cellX, row)
	if cell.Style.FG != p.chrome.selected.FG {
		t.Fatalf("target line keyword FG = %+v, want selected FG %+v", cell.Style.FG, p.chrome.selected.FG)
	}
	if endCell := g.At(geo.prevEnd-1, row); endCell.Text != "…" {
		t.Fatalf("truncated preview end cell = %q, want …", endCell.Text)
	}
}

func TestPreviewKeepsCombiningMarksInSyntaxChunks(t *testing.T) {
	t.Parallel()
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	pv := &fakePreviewer{files: map[string]previewRead{
		"/w/main.go": {data: "package main\n// e\u0301\n"},
	}}
	opts := fileOptions("main.go", []string{"main.go"}, pv)
	opts.Theme = th
	p := New(opts)
	settle(t, p, p.Init(), 8)
	geo := p.layout(100, 24)
	g := draw(p, 100, 24)
	row := geo.rowsTop + 1
	// Line 2 renders "// é" after "2 │ " (7 display cells before 'e').
	cellX := geo.prevX + 7
	if got := g.At(cellX, row).Text; got != "e\u0301" {
		t.Fatalf("combining mark cell = %q, want %q (row=%q)", got, "e\u0301", rowText(g, row))
	}
}
