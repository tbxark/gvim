package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
)

// concealsAt returns a conceal hook for the spans of the given lines.
func concealsAt(spans map[int][]ConcealSpan) func(int) []ConcealSpan {
	return func(line int) []ConcealSpan { return spans[line] }
}

// TestConcealHidesCellsAndKeepsColumns checks that conceal blanks the cells of a range,
// writes the replacement text into its first cells, applies the style and does not move
// the rest of the line (the columns are unchanged).
func TestConcealHidesCellsAndKeepsColumns(t *testing.T) {
	p := Pane{
		Doc: newDoc("secret value"),
		Conceals: concealsAt(map[int][]ConcealSpan{0: {{
			Start: 0, End: 6, Text: "...", Style: grid.Style{FG: cRed, Bold: true},
		}}}),
	}
	g := p.Render(30, 2)
	if got := cellAt(t, g, textX, 0).Text; got != "." {
		t.Fatalf("first concealed cell = %q, want the replacement %q", got, ".")
	}
	if c := cellAt(t, g, textX, 0); c.Style.Bold != true || fgOf(c) != cRed {
		t.Fatalf("conceal style not applied: %+v", c.Style)
	}
	// The three replacement cells, then the three hidden cells that stay blanks, then the
	// space after "secret" and "value".
	for x, want := range []string{".", ".", ".", " ", " ", " ", " ", "v"} {
		if got := cellAt(t, g, textX+x, 0).Text; got != want {
			t.Errorf("cell %d = %q, want %q", x, got, want)
		}
	}
	// The mapping is source-based and unchanged by conceal.
	if x, y, ok := p.Caret(30, 2); !ok || x != textX || y != 0 {
		t.Fatalf("caret = (%d,%d,%v)", x, y, ok)
	}
	if got := p.PosAt(30, 2, textX+4, 0); got != (buffer.Pos{Line: 0, Col: 4}) {
		t.Fatalf("PosAt over a hidden cell = %+v", got)
	}
}

// TestConcealActivationAndScope checks unless-cursor-in, if-cursor-in and the line scope.
func TestConcealActivationAndScope(t *testing.T) {
	unless := []ConcealSpan{{Start: 0, End: 3, Text: "...", Activation: ConcealUnlessCursorIn}}
	doc := newDoc("abc", "abc")
	doc.cursor = buffer.Pos{Line: 1}
	g := Pane{Doc: doc, Conceals: func(line int) []ConcealSpan {
		if line == 0 {
			return unless
		}
		return nil
	}}.Render(20, 2)
	if got := cellAt(t, g, textX, 0).Text; got != "." {
		t.Fatalf("unless-cursor-in did not conceal the other line: %q", got)
	}

	// if-cursor-in: only the caret's line is concealed.
	ifOnly := func(line int) []ConcealSpan {
		return []ConcealSpan{{Start: 0, End: 3, Text: "...", Activation: ConcealIfCursorIn}}
	}
	doc2 := newDoc("abc", "abc")
	doc2.cursor = buffer.Pos{Line: 1}
	g = Pane{Doc: doc2, Conceals: ifOnly}.Render(20, 2)
	if got := cellAt(t, g, textX, 0).Text; got != "a" {
		t.Fatalf("if-cursor-in concealed a non-caret line: %q", got)
	}
	if got := cellAt(t, g, textX, 1).Text; got != "." {
		t.Fatalf("if-cursor-in did not conceal the caret line: %q", got)
	}

	// A scope outside the caret line skips the span.
	scoped := func(line int) []ConcealSpan {
		return []ConcealSpan{{Start: 0, End: 3, Text: "...", Scoped: true, ScopeStart: 5, ScopeEnd: 9}}
	}
	g = Pane{Doc: newDoc("abc"), Conceals: scoped}.Render(20, 1)
	if got := cellAt(t, g, textX, 0).Text; got != "a" {
		t.Fatalf("out-of-scope conceal applied: %q", got)
	}
}

// TestCursorLineOverlayPaintsTheCaretRow checks the overlay paints only the caret row and
// wins over the current-line tint, while the caret itself still paints on top.
func TestCursorLineOverlayPaintsTheCaretRow(t *testing.T) {
	doc := newDoc("abc", "def")
	doc.cursor = buffer.Pos{Line: 1, Col: 1}
	p := Pane{
		Doc: doc, CurrentLine: true,
		CursorLineOverlay: func() (grid.Style, bool) {
			return grid.Style{BG: cGray}, true
		},
	}
	g := p.Render(20, 2)
	if got := bgOf(cellAt(t, g, textX, 1)); got != cGray {
		t.Fatalf("caret row background = %v, want the overlay", got)
	}
	if got := bgOf(cellAt(t, g, textX, 0)); got == cGray {
		t.Fatal("the overlay painted a non-caret row")
	}
	if c := cellAt(t, g, textX+1, 1); !c.Style.Reverse {
		t.Fatalf("the caret did not paint over the overlay: %+v", c.Style)
	}
}

// TestConcealDoesNotApplyToPlainLayout checks the plain (non-styled) layout used by caret and
// hit paths is not affected by conceal hooks: the geometry stays the source geometry.
func TestConcealDoesNotApplyToPlainLayout(t *testing.T) {
	doc := newDoc("secret value")
	p := Pane{Doc: doc, Conceals: concealsAt(map[int][]ConcealSpan{0: {{Start: 0, End: 6}}})}
	f, ok := p.newFrame(30, 2)
	if !ok {
		t.Fatal("no frame")
	}
	ln := f.layoutLine(0, false)
	if len(ln.geom) != len("secret value") {
		t.Fatalf("geometry changed by conceal: %v", ln.geom)
	}
}

// TestCursorLineOverlayKeepsWideCharacters checks the overlay restyles a wide character
// of the caret row without blanking it or its continuation cell.
func TestCursorLineOverlayKeepsWideCharacters(t *testing.T) {
	doc := newDoc("a世b")
	p := Pane{
		Doc: doc,
		CursorLineOverlay: func() (grid.Style, bool) {
			return grid.Style{BG: cGray}, true
		},
	}
	g := p.Render(20, 1)
	if got := cellAt(t, g, textX+1, 0); got.Text != "世" || bgOf(got) != cGray {
		t.Errorf("wide cell = %q on %v, want the character on the overlay", got.Text, bgOf(got))
	}
	if got := cellAt(t, g, textX+2, 0); got.Text != "" || bgOf(got) != cGray {
		t.Errorf("continuation cell = %q on %v, want an empty cell on the overlay", got.Text, bgOf(got))
	}
}
