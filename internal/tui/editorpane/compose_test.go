package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// composeDoc returns a Markdown-looking document whose caret is on a final tail
// line, so the lines under test are concealed (the line just above the caret is
// revealed by the next-line-start rule, and the tail keeps that off the tests).
func composeDoc(lines ...string) *fakeDoc {
	all := append(append([]string{}, lines...), "tail")
	doc := newDoc(all...)
	doc.cursor = buffer.Pos{Line: len(all) - 1, Col: 0}
	return doc
}

// composePane returns the Markdown page view over doc.
func composePane(doc *fakeDoc, measure int) Pane {
	return Pane{Doc: doc, PageView: PageView{On: true, Measure: measure, Markdown: true}}
}

// pageRow returns the visible text of row y of a rendered page view, from the left
// margin.
func pageRow(t *testing.T, g interface{ Plain() []string }, margin, y int) string {
	t.Helper()
	rows := g.Plain()
	if y >= len(rows) {
		t.Fatalf("row %d out of range (%d rows)", y, len(rows))
	}
	row := rows[y]
	if margin >= len(row) {
		return ""
	}
	return strings.TrimRight(row[margin:], " ")
}

func TestComposeConcealsInlineMarkers(t *testing.T) {
	t.Parallel()
	doc := composeDoc(
		"Some **bold** and *em* text",
		"`code` here",
		"[link](http://x)",
		"~~gone~~",
		"plain end",
	)
	p := composePane(doc, 30)
	g := p.Render(40, 8)
	tests := []struct {
		y    int
		want string
	}{
		{0, "Some bold and em text"},
		{1, "code here"},
		{2, "link"},
		{3, "gone"},
	}
	for _, tt := range tests {
		if got := pageRow(t, g, 4, tt.y); got != tt.want {
			t.Fatalf("row %d = %q, want %q", tt.y, got, tt.want)
		}
	}
}

func TestComposeRevealsTheCaretLine(t *testing.T) {
	t.Parallel()
	doc := composeDoc("Some **bold** text", "plain end")
	doc.cursor = buffer.Pos{Line: 0, Col: 2}
	p := composePane(doc, 30)
	g := p.Render(40, 4)
	if got := pageRow(t, g, 4, 0); got != "Some **bold** text" {
		t.Fatalf("caret line = %q, want the markers revealed", got)
	}
	// A caret at the start of the next line reveals the line above.
	doc.cursor = buffer.Pos{Line: 1, Col: 0}
	g = p.Render(40, 4)
	if got := pageRow(t, g, 4, 0); got != "Some **bold** text" {
		t.Fatalf("line before the caret = %q, want it revealed", got)
	}
}

func TestComposeRuleAndQuote(t *testing.T) {
	t.Parallel()
	doc := composeDoc("---", "> quoted", "plain end")
	p := composePane(doc, 20)
	g := p.Render(30, 5)
	if got := pageRow(t, g, 4, 0); got != strings.Repeat("─", 18) {
		t.Fatalf("rule row = %q, want 18 rule cells", got)
	}
	if got := pageRow(t, g, 4, 1); got != "│ quoted" {
		t.Fatalf("quote row = %q, want the bar before the text", got)
	}
}

func TestComposeListStaysWithAHangingIndent(t *testing.T) {
	t.Parallel()
	doc := composeDoc("- a list item that wraps over two rows", "plain end")
	p := composePane(doc, 20)
	g := p.Render(32, 6)
	first := pageRow(t, g, 5, 0)
	second := pageRow(t, g, 5, 1)
	if !strings.HasPrefix(first, "- a list item") {
		t.Fatalf("first row = %q rows=%q, want the list marker kept", first, g.Plain()[:4])
	}
	if !strings.HasPrefix(second, "  ") || strings.HasPrefix(strings.TrimLeft(second, " "), "-") {
		t.Fatalf("continuation row = %q, want it indented under the item", second)
	}
}

func TestComposeLeavesFencesLiteral(t *testing.T) {
	t.Parallel()
	doc := composeDoc("```", "**not bold**", "```", "plain end")
	p := composePane(doc, 30)
	g := p.Render(40, 6)
	// The body is framed as code and its contents stay literal (not composed).
	body := pageRow(t, g, 4, 1)
	if !strings.HasPrefix(body, "│ **not bold**") || !strings.HasSuffix(body, "│") {
		t.Fatalf("fenced body = %q, want the framed literal code", body)
	}
}

func TestComposeHitMapsThroughHiddenMarkers(t *testing.T) {
	t.Parallel()
	doc := composeDoc("Some **bold** text", "plain end")
	p := composePane(doc, 30)
	// The composed row is "Some bold text"; clicking its "b" must map to the
	// source column of the "b" after the hidden "**".
	if hit := p.Hit(40, 4, 4+5, 0); hit.Kind != HitText || hit.Pos.Col != 7 {
		t.Fatalf("hit = %+v, want source column 7", hit)
	}
	// A click past the end of the composed row maps to the end of the source line.
	if hit := p.Hit(40, 4, 4+14, 0); hit.Pos.Col != 18 {
		t.Fatalf("end hit = %+v, want the source line end 18", hit)
	}
}

// composeSelDoc is a document with a selection.
type composeSelDoc struct {
	*fakeDoc
	anchor, head buffer.Pos
}

func (d composeSelDoc) Selection() (buffer.Pos, buffer.Pos, bool) { return d.anchor, d.head, true }

func TestComposeSelectionPaintsVisibleCells(t *testing.T) {
	t.Parallel()
	inner := composeDoc("a **b** c", "plain end")
	// Select source columns 2..6 ("**b**") on line 0; the caret stays elsewhere.
	doc := composeSelDoc{fakeDoc: inner, anchor: buffer.Pos{Line: 0, Col: 2}, head: buffer.Pos{Line: 0, Col: 6}}
	p := Pane{Doc: doc, PageView: PageView{On: true, Measure: 20, Markdown: true}}
	g := p.Render(30, 4)
	row := g.Plain()[0]
	// The composed line starts after margin 4: "a b c"; the b cell (index 2 of
	// the composed text) is inside the selection and is painted selected.
	if bg := g.At(4+2, 0).Style.BG; bg != nil {
		t.Fatalf("row = %q: the selected cell has an unexpected background %v", row, bg)
	}
	if !g.At(4+2, 0).Style.Reverse {
		t.Fatalf("row = %q: the selected b cell is not painted selected", row)
	}
}

func TestNonMarkdownPageViewKeepsTheMarkers(t *testing.T) {
	t.Parallel()
	doc := composeDoc("Some **bold** text", "plain end")
	p := Pane{Doc: doc, PageView: PageView{On: true, Measure: 30}}
	g := p.Render(40, 4)
	if got := pageRow(t, g, 4, 0); got != "Some **bold** text" {
		t.Fatalf("non-Markdown page view = %q, want the markers", got)
	}
}
