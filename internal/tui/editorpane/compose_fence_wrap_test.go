package editorpane

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
)

// fenceWrapDoc returns a fenced block with a body line wider than the frame, and
// the caret on a trailing line so the block is composed.
func fenceWrapDoc(body string) *fakeDoc {
	lines := []string{"```", body, "```", "after", "tail"}
	doc := newDoc(lines...)
	doc.cursor = buffer.Pos{Line: len(lines) - 1, Col: 0}
	return doc
}

func TestFenceContinuationRails(t *testing.T) {
	t.Parallel()
	doc := fenceWrapDoc("0123456789ABCDEFGHIJ")
	p := composePane(doc, 20)
	p.PageExtraRows = PageViewTableBorders(doc, Styles{})
	rows := p.Render(24, 10).Plain()
	trim := func(y int) string { return strings.TrimRight(rows[y][1:], " ") }
	if !strings.HasPrefix(trim(0), "╭") {
		t.Fatalf("row 0 = %q, want the top border", trim(0))
	}
	if got := trim(1); got != "│ 0123456789AB │" {
		t.Fatalf("row 1 = %q, want the first framed segment", got)
	}
	if got := trim(2); got != "│ CDEFGHIJ     │" {
		t.Fatalf("row 2 = %q, want the railed continuation row", got)
	}
	if got := ansi.StringWidth(trim(2)); got != 16 {
		t.Fatalf("continuation row width = %d, want the 16-cell frame", got)
	}
	if !strings.HasPrefix(trim(3), "╰") {
		t.Fatalf("row 3 = %q, want the bottom border after the continuation", trim(3))
	}
}

func TestFenceCaretLineKeepsTheWholeBody(t *testing.T) {
	t.Parallel()
	doc := fenceWrapDoc("0123456789ABCDEFGHIJ")
	doc.cursor = buffer.Pos{Line: 1, Col: 0}
	p := composePane(doc, 20)
	p.PageExtraRows = PageViewTableBorders(doc, Styles{})
	rows := p.Render(24, 12).Plain()
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "0123456789ABCDEFGHIJ") {
		t.Fatalf("rows = %q, want the whole body text on the caret line", rows)
	}
	if strings.Contains(joined, "│ CDEFGHIJ     │") {
		t.Fatalf("rows = %q, the continuation rows should stand down under the caret", rows)
	}
}

func TestFenceInABlockquoteStaysLiteral(t *testing.T) {
	t.Parallel()
	lines := []string{"> ```", "> code here", "> ```", "after", "tail"}
	doc := newDoc(lines...)
	doc.cursor = buffer.Pos{Line: len(lines) - 1, Col: 0}
	p := composePane(doc, 30)
	joined := strings.Join(p.Render(40, 10).Plain(), "\n")
	if !strings.Contains(joined, "```") {
		t.Fatalf("rows = %q, want the quoted fence left literal", joined)
	}
	if strings.Contains(joined, "╭") {
		t.Fatalf("rows = %q, the quoted fence should not be framed", joined)
	}
}
