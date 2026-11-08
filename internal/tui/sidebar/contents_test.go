package sidebar

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/style"
)

// fakeLines is an outline.Lines over a document written in a test.
type fakeLines []string

func (f fakeLines) LineCount() int    { return len(f) }
func (f fakeLines) Line(i int) string { return f[i] }

// scanContents returns a Contents section scanned over text.
func scanContents(t *testing.T, st Settings, text string) Contents {
	t.Helper()
	c := NewContents(st)
	c.SetOpen(true)
	c.SetViewRows(10)
	if !c.Scan(fakeLines(strings.Split(text, "\n"))) {
		t.Fatal("the scan did not complete")
	}
	return c
}

// titles is the heading titles of the visible rows, in order.
func titles(c Contents) []string {
	out := make([]string, c.Len())
	for pos := 0; pos < c.Len(); pos++ {
		row, _ := c.RowAt(pos - c.Scroll())
		out[pos] = row.Heading.Title
	}
	return out
}

func TestContentsScanBuildsTheTree(t *testing.T) {
	t.Parallel()
	c := scanContents(t, DefaultSettings(), "# A\n## B\n### C\n## D\n# E")
	want := []string{"A", "B", "C", "D", "E"}
	if got := titles(c); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	rows := []Row{}
	for pos := 0; pos < c.Len(); pos++ {
		r, _ := c.RowAt(pos)
		rows = append(rows, r)
	}
	if !rows[0].HasChildren || rows[0].Depth != 0 || !rows[0].Expanded {
		t.Errorf("A = %+v, want a level-1 heading with listed children", rows[0])
	}
	if rows[2].Depth != 2 || rows[2].HasChildren {
		t.Errorf("C = %+v, want a level-3 leaf", rows[2])
	}
	if rows[4].HasChildren || rows[4].Depth != 0 {
		t.Errorf("E = %+v, want a level-1 leaf", rows[4])
	}
	if got := rows[0].GlyphX; got != 2 {
		t.Errorf("level-1 glyph at %d, want 2", got)
	}
	if got := rows[2].GlyphX; got != 6 {
		t.Errorf("level-3 glyph at %d, want 6", got)
	}
}

func TestContentsFollow(t *testing.T) {
	t.Parallel()
	text := "# A\n## B\n### C\n## D\n# E"
	tests := []struct {
		name    string
		cursor  int
		top     int
		focused bool
		follow  Follow
		want    string
	}{
		{"the heading containing the caret", 2, 0, true, FollowCursor, "C"},
		{"the last heading at or above the caret", 3, 0, true, FollowCursor, "D"},
		{"before the first heading selects it", 0, 0, true, FollowCursor, "A"},
		{"scroll mode follows the pane top", 0, 4, true, FollowScroll, "E"},
		{"a pane without the keyboard follows the top", 0, 3, false, FollowCursor, "D"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := DefaultSettings()
			st.Follow = tt.follow
			c := scanContents(t, st, text)
			c.Follow(tt.cursor, tt.top, tt.focused)
			got, ok := c.Selected()
			if !ok || got.Title != tt.want {
				t.Fatalf("selected = %q, %v, want %q", got.Title, ok, tt.want)
			}
		})
	}
}

func TestContentsFollowKeepsAKeyboardMove(t *testing.T) {
	t.Parallel()
	c := scanContents(t, DefaultSettings(), "# A\n## B\n## C")
	c.Follow(0, 0, true) // selects A
	c.Move(2)            // the reader browsed to C without moving the caret
	c.Follow(0, 0, true) // the same inputs: the browse must stay
	if got, _ := c.Selected(); got.Title != "C" {
		t.Fatalf("selected = %q after the same follow, want C", got.Title)
	}
	// A caret move does take the selection back.
	c.Follow(1, 0, true)
	if got, _ := c.Selected(); got.Title != "B" {
		t.Fatalf("selected = %q after a caret move, want B", got.Title)
	}
}

func TestContentsFoldGlyphs(t *testing.T) {
	t.Parallel()
	c := scanContents(t, DefaultSettings(), "# A\n## B\n## C\n# D")
	// Fold A: its children go, the selection stays on A.
	c.SelectPos(0)
	if !c.ToggleFoldPos(0) {
		t.Fatal("A did not fold")
	}
	if got := titles(c); strings.Join(got, ",") != "A,D" {
		t.Fatalf("rows after folding A = %v, want A,D", got)
	}
	if got, _ := c.Selected(); got.Title != "A" {
		t.Fatalf("selected = %q after the fold, want A", got.Title)
	}
	// A leaf has no fold to toggle.
	if c.ToggleFoldPos(1) {
		t.Fatal("D, a leaf, reported a fold")
	}
	// Unfold, then Left/Right move between the tree and its parent.
	if !c.ToggleFoldPos(0) {
		t.Fatal("A did not unfold")
	}
	c.SelectPos(1) // B
	c.Collapse()   // a leaf moves to its parent
	if got, _ := c.Selected(); got.Title != "A" {
		t.Fatalf("Left on B selected %q, want its parent A", got.Title)
	}
	c.Expand() // A is expanded: Right steps into the first child
	if got, _ := c.Selected(); got.Title != "B" {
		t.Fatalf("Right on A selected %q, want B", got.Title)
	}
	// Collapsing a heading with children folds it.
	c.SelectPos(0)
	c.Collapse()
	if got := titles(c); strings.Join(got, ",") != "A,D" {
		t.Fatalf("rows after collapsing A = %v, want A,D", got)
	}
}

func TestContentsMovementAndWindow(t *testing.T) {
	t.Parallel()
	c := scanContents(t, DefaultSettings(), "# A\n## B\n## C\n## D\n## E")
	c.SetViewRows(2)
	c.Follow(0, 0, true)
	c.Move(3)
	if got, _ := c.Selected(); got.Title != "D" {
		t.Fatalf("selected = %q after three moves, want D", got.Title)
	}
	if got := c.Scroll(); got != 2 {
		t.Fatalf("scroll = %d, want 2 so D is visible", got)
	}
	c.Page(1)
	if got, _ := c.Selected(); got.Title != "E" {
		t.Fatalf("selected = %q after a page down, want E", got.Title)
	}
	c.Move(1)
	if got, _ := c.Selected(); got.Title != "E" {
		t.Fatalf("selected = %q past the end, want it clamped to E", got.Title)
	}
	c.First()
	if got := c.Scroll(); got != 0 {
		t.Fatalf("scroll = %d after First, want 0", got)
	}
	c.Last()
	if got, _ := c.Selected(); got.Title != "E" {
		t.Fatalf("selected = %q after Last, want E", got.Title)
	}
}

func TestContentsMaxDepth(t *testing.T) {
	t.Parallel()
	st := DefaultSettings()
	st.MaxDepth = 2
	c := scanContents(t, st, "# A\n## B\n### C\n## D")
	if got := titles(c); strings.Join(got, ",") != "A,B,D" {
		t.Fatalf("rows = %v, want A,B,D", got)
	}
	// The depth-limit change rebuilds the tree at once.
	st.MaxDepth = 3
	c.SetSettings(st)
	if got := titles(c); strings.Join(got, ",") != "A,B,C,D" {
		t.Fatalf("rows after the depth change = %v, want A,B,C,D", got)
	}
}

func TestContentsFoldStateSurvivesARescan(t *testing.T) {
	t.Parallel()
	text := "# A\n## B\n## C\n# D"
	c := scanContents(t, DefaultSettings(), text)
	c.SelectPos(0)
	c.ToggleFoldPos(0)
	// The same document again keeps A folded.
	if !c.Scan(fakeLines(strings.Split(text, "\n"))) {
		t.Fatal("the scan did not complete")
	}
	if got := titles(c); strings.Join(got, ",") != "A,D" {
		t.Fatalf("rows after the rescan = %v, want A,D", got)
	}
	// A new heading above renames A's key and it comes back expanded; headings
	// below the edit keep the reader's state.
	c.Scan(fakeLines([]string{"# New", "# A", "## B", "## C", "# D"}))
	if got := titles(c); strings.Join(got, ",") != "New,A,B,C,D" {
		t.Fatalf("rows after the edit = %v, want New,A,B,C,D", got)
	}
}

func TestContentsStaleScan(t *testing.T) {
	t.Parallel()
	c := scanContents(t, DefaultSettings(), "# A")
	if c.Stale() {
		t.Fatal("a small document is stale")
	}
	// A document over the scan bound is refused; the rows it had stay.
	if c.Scan(fakeLines{strings.Repeat("x", 2<<20)}) {
		t.Fatal("the oversized scan reported success")
	}
	if !c.Stale() {
		t.Fatal("the section does not report the oversized document")
	}
	if got := titles(c); len(got) != 1 || got[0] != "A" {
		t.Fatalf("rows after the oversized scan = %v, want the old A", got)
	}
	c.Reset()
	if c.Scan(fakeLines{strings.Repeat("x", 2<<20)}) {
		t.Fatal("the oversized scan reported success after Reset")
	}
	if got := titles(c); len(got) != 0 {
		t.Fatalf("rows after Reset = %v, want none", got)
	}
}

func TestParseSettings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  map[string]any
		want Settings
	}{
		{"no settings are the defaults", nil, DefaultSettings()},
		{"the documented values", map[string]any{"max_depth": float64(3), "follow": "scroll", "fold_buffer": true, "rows": float64(20), "auto_open": false}, Settings{MaxDepth: 3, Follow: FollowScroll, FoldBuffer: true, Rows: 20}},
		{"numbers are clamped", map[string]any{"max_depth": float64(9), "rows": float64(0)}, Settings{MaxDepth: 6, Follow: FollowCursor, Rows: 1, AutoOpen: true}},
		{"a wrong type keeps the default", map[string]any{"max_depth": "three", "follow": float64(1), "fold_buffer": "yes", "rows": true, "auto_open": float64(1)}, DefaultSettings()},
		{"an unknown key is ignored", map[string]any{"depth": float64(2)}, DefaultSettings()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ParseSettings(tt.raw); got != tt.want {
				t.Fatalf("ParseSettings = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// drawContents paints c into a 20-wide column whose Contents body is rows 2..5.
func drawContents(c *Contents, focus bool) *grid.Grid {
	c.SetFocus(focus)
	g := grid.New(20, 8)
	c.Draw(g, 0, 20, Section{Kind: KindContents, Index: 1, HeaderY: 1, BodyTop: 2, BodyBottom: 5}, icons.Plain, style.Styles{})
	return g
}

func TestContentsDrawRows(t *testing.T) {
	t.Parallel()
	c := scanContents(t, DefaultSettings(), "# Root\n## Child\nloose text")
	c.Follow(1, 0, true) // the second heading, selected
	g := drawContents(&c, true)
	lines := g.Plain()
	if got, want := lines[2], "│ ▼ Root           │"; got != want {
		t.Errorf("row A = %q, want %q", got, want)
	}
	// The selected row wears the focus marker at the padding cell and the
	// title is indented two cells for its level.
	child := lines[3]
	if !strings.HasPrefix(child, "│▌") {
		t.Errorf("selected row = %q, want the ▌ marker under focus", child)
	}
	if !strings.Contains(child, "    Child") {
		t.Errorf("selected row = %q, want Child at its indent", child)
	}
	if lines[4] != "│                  │" {
		t.Errorf("blank row = %q", lines[4])
	}
	if strings.Contains(lines[5], "▌") {
		t.Errorf("the collapsed section keeps painting rows: %q", lines[5])
	}
}

func TestContentsDrawSelectionWithoutFocus(t *testing.T) {
	t.Parallel()
	c := scanContents(t, DefaultSettings(), "# Root\n## Child")
	c.Follow(0, 0, true)
	g := drawContents(&c, false)
	row := g.Plain()[2]
	if strings.Contains(row, "▌") {
		t.Errorf("the unfocused section drew the focus marker: %q", row)
	}
	if !strings.Contains(row, "▼ Root") {
		t.Errorf("row = %q, want the fold glyph and title", row)
	}

	// The selection is still visible: the row carries the selection background
	// whether or not the section has the keyboard.
	styles := style.Styles{Selection: lipgloss.NewStyle().Background(lipgloss.Color("4"))}
	c2 := scanContents(t, DefaultSettings(), "# Root\n## Child")
	c2.Follow(0, 0, true)
	g2 := grid.New(20, 8)
	c2.Draw(g2, 0, 20, Section{Kind: KindContents, HeaderY: 1, BodyTop: 2, BodyBottom: 5}, icons.Plain, styles)
	if got, want := g2.At(5, 2).Style.BG, style.Cell(styles.Selection).BG; got != want {
		t.Errorf("selected row BG = %v, want the selection colour %v", got, want)
	}
}

func TestContentsDrawFoldGlyph(t *testing.T) {
	t.Parallel()
	c := scanContents(t, DefaultSettings(), "# Root\n## Child")
	c.ToggleFoldPos(0)
	g := drawContents(&c, true)
	row := g.Plain()[2]
	if !strings.Contains(row, "▶ Root") {
		t.Errorf("folded row = %q, want the ▶ glyph", row)
	}
	if got := g.Plain()[3]; got != "│                  │" {
		t.Errorf("the child is still drawn: %q", got)
	}
}

func TestContentsDrawDeepIndentAndTruncation(t *testing.T) {
	t.Parallel()
	c := scanContents(t, DefaultSettings(), "### A very long heading that does not fit the column")
	g := drawContents(&c, false)
	row := g.Plain()[2]
	// A level-3 heading's name starts at column 4+2*2 = 8 and is cut with an
	// ellipsis before the padding cell at column 18.
	if !strings.HasPrefix(row, "│       A ve") {
		t.Errorf("row = %q, want the deep indent", row)
	}
	if !strings.HasSuffix(strings.TrimRight(row, " │")+"…", "…") || !strings.Contains(row, "…") {
		t.Errorf("row = %q, want a truncated title", row)
	}
}

func TestContentsDrawMessages(t *testing.T) {
	t.Parallel()
	// A document with no headings says so.
	c := scanContents(t, DefaultSettings(), "plain text")
	if got := drawContents(&c, false).Plain()[2]; !strings.Contains(got, "(no headings)") {
		t.Errorf("row = %q, want the empty message", got)
	}
	// A document too large to scan says so.
	c = NewContents(DefaultSettings())
	c.SetOpen(true)
	c.Scan(fakeLines{strings.Repeat("x", 2<<20)})
	if got := drawContents(&c, false).Plain()[2]; !strings.Contains(got, "too") {
		t.Errorf("row = %q, want the stale message", got)
	}
}
