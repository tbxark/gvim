package editorpane

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/style"
)

// virtualDoc is a document whose caret may sit past the end of its line.
type virtualDoc struct {
	*fakeDoc
	virtual int
}

func (d virtualDoc) VirtualColumns() int { return d.virtual }

func TestCaretDrawnInVirtualSpace(t *testing.T) {
	t.Parallel()
	doc := virtualDoc{fakeDoc: newDoc("abc"), virtual: 2}
	doc.cursor = buffer.Pos{Line: 0, Col: 3}
	p := Pane{Doc: doc, HideGutter: true}
	g := p.Render(8, 2)
	if got := rowsOf(g)[0]; got != "abc" {
		t.Fatalf("row = %q, want the text", got)
	}
	// The caret is drawn two cells past the text, in the blank.
	cell := g.At(5, 0)
	if !cell.Style.Reverse {
		t.Fatalf("caret cell at (5,0) = %+v, want reverse video", cell)
	}
	if g.At(3, 0).Style.Reverse {
		t.Fatal("the caret is also drawn at the line end")
	}
	if x, y, ok := p.Caret(8, 2); !ok || x != 5 || y != 0 {
		t.Fatalf("Caret = (%d,%d,%v), want (5,0,true)", x, y, ok)
	}
}

func TestCaretOnTheTextWithoutVirtualSpace(t *testing.T) {
	t.Parallel()
	doc := newDoc("abc")
	doc.cursor = buffer.Pos{Line: 0, Col: 3}
	p := Pane{Doc: doc, HideGutter: true}
	if x, _, _ := p.Caret(8, 2); x != 3 {
		t.Fatalf("caret x = %d, want 3 (the line end)", x)
	}
}

func TestHorizontalScrollReachesTheVirtualCaret(t *testing.T) {
	t.Parallel()
	doc := virtualDoc{fakeDoc: newDoc("abc"), virtual: 8}
	doc.cursor = buffer.Pos{Line: 0, Col: 3}
	p := Pane{Doc: doc, HideGutter: true, Wrap: false}
	g := p.Render(8, 3)
	x, y, ok := p.Caret(8, 3)
	if !ok || y != 0 {
		t.Fatalf("Caret = (%d,%d,%v)", x, y, ok)
	}
	if x < 0 || x >= 7 {
		t.Fatalf("caret x = %d, want it inside the text area", x)
	}
	if !g.At(x, y).Style.Reverse {
		t.Fatalf("no caret at (%d,%d)", x, y)
	}
	// The window moved right: the horizontal scroll has a positive offset.
	if left, most := p.HScroll(8, 3); left == 0 || most == 0 {
		t.Fatalf("HScroll = %d of %d, want the window scrolled to the virtual caret", left, most)
	}
}

func TestHitReportsAVirtualColumn(t *testing.T) {
	t.Parallel()
	doc := newDoc("abc")
	doc.cursor = buffer.Pos{Line: 0, Col: 0}
	p := Pane{Doc: doc, HideGutter: true, Wrap: false}
	hit := p.Hit(8, 2, 6, 0)
	if hit.Kind != HitText || hit.Pos.Col != 3 || hit.Virtual != 3 {
		t.Fatalf("hit past the line end = %+v, want the line end with 3 virtual columns", hit)
	}
	hit = p.Hit(8, 2, 1, 0)
	if hit.Kind != HitText || hit.Pos.Col != 1 || hit.Virtual != 0 {
		t.Fatalf("hit on text = %+v, want no virtual column", hit)
	}
}

func diagPane(doc *fakeDoc, spans []DiagnosticSpan, styles Styles) Pane {
	return Pane{
		Doc:               doc,
		HideGutter:        true,
		Wrap:              false,
		InlineDiagnostics: true,
		Diagnostics:       func(int) []DiagnosticSpan { return spans },
		Styles:            styles,
	}
}

func TestInlineDiagnosticsDrawsTheMostSevereMessageRightAligned(t *testing.T) {
	t.Parallel()
	styles := Styles{DiagError: style.Cell(lipgloss.NewStyle().Foreground(lipgloss.Color("1")))}
	doc := newDoc("hello")
	p := diagPane(doc, []DiagnosticSpan{
		{Start: 0, End: 5, Severity: SeverityWarning, Message: "a warning"},
		{Start: 0, End: 5, Severity: SeverityError, Message: "bad"},
	}, styles)
	g := p.Render(30, 2)
	row := rowsOf(g)[0]
	if !strings.HasSuffix(row, "bad") {
		t.Fatalf("row = %q, want the error message at its end", row)
	}
	if got := strings.Count(row, "bad"); got != 1 {
		t.Fatalf("row = %q: %d messages, want the most severe one only", row, got)
	}
	x := ansi.StringWidth(row) - 3
	if x != 26 {
		t.Fatalf("message starts at %d, want 26 (right-aligned with one blank edge)", x)
	}
	if got, want := g.At(x, 0).Style.FG, style.Cell(lipgloss.NewStyle().Foreground(lipgloss.Color("1"))).FG; got != want {
		t.Fatalf("message FG = %v, want the error colour %v", got, want)
	}
	if !strings.HasPrefix(row, "hello") {
		t.Fatalf("row = %q, the text comes first", row)
	}
}

func TestInlineDiagnosticsTruncatesWhenNarrow(t *testing.T) {
	t.Parallel()
	doc := newDoc("hello")
	p := diagPane(doc, []DiagnosticSpan{{Start: 0, End: 5, Severity: SeverityInfo, Message: "a very long message that cannot fit"}}, Styles{})
	row := rowsOf(p.Render(16, 2))[0]
	if !strings.Contains(row, "…") {
		t.Fatalf("row = %q, want a truncated message", row)
	}
	if !strings.HasPrefix(row, "hello") {
		t.Fatalf("row = %q, want the text first", row)
	}
}

func TestInlineDiagnosticsNeedsRoomAndTheOption(t *testing.T) {
	t.Parallel()
	spans := []DiagnosticSpan{{Start: 0, End: 5, Severity: SeverityError, Message: "bad"}}
	doc := newDoc("hello")
	// A narrow pane has fewer than inlineDiagMin cells to spare.
	if row := rowsOf(diagPane(doc, spans, Styles{}).Render(12, 2))[0]; strings.Contains(row, "bad") {
		t.Fatalf("row = %q, the message was drawn without room", row)
	}
	// Without the option no message is drawn.
	p := diagPane(doc, spans, Styles{})
	p.InlineDiagnostics = false
	if row := rowsOf(p.Render(30, 2))[0]; strings.Contains(row, "bad") {
		t.Fatalf("row = %q, the message was drawn with the option off", row)
	}
	// A diagnostic with no message draws nothing.
	empty := diagPane(doc, []DiagnosticSpan{{Start: 0, End: 5, Severity: SeverityError}}, Styles{})
	if row := rowsOf(empty.Render(30, 2))[0]; strings.Contains(row, "…") {
		t.Fatalf("row = %q, a message with no text was drawn", row)
	}
}

func TestInlineDiagnosticsOnTheLastWrappedRow(t *testing.T) {
	t.Parallel()
	doc := newDoc("hello")
	p := Pane{
		Doc:               doc,
		HideGutter:        true,
		Wrap:              true,
		InlineDiagnostics: true,
		Diagnostics: func(int) []DiagnosticSpan {
			return []DiagnosticSpan{{Start: 0, End: 5, Severity: SeverityError, Message: "bad"}}
		},
	}
	row := rowsOf(p.Render(30, 2))[0]
	if !strings.HasSuffix(row, "bad") {
		t.Fatalf("row = %q, want the message on the last row of the line", row)
	}
}
