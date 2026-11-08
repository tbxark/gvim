package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/syntax"
)

func TestTaskListGlyphConceal(t *testing.T) {
	t.Parallel()
	doc := composeDoc("- [ ] todo", "- [x] done", "- [X] DONE", "- plain")
	p := composePane(doc, 30)
	rows := p.Render(40, 10).Plain()
	trim := func(y int) string { return strings.TrimRight(rows[y][4:], " ") }
	tests := []struct {
		y    int
		want string
	}{
		{0, "- ☐ todo"},
		{1, "- ☑ done"},
		{2, "- ☑ DONE"},
		{3, "- plain"},
	}
	for _, tt := range tests {
		if got := trim(tt.y); got != tt.want {
			t.Fatalf("row %d = %q, want %q", tt.y, got, tt.want)
		}
	}
	// The caret's line reveals the raw box.
	doc.cursor = buffer.Pos{Line: 0, Col: 2}
	rows = p.Render(40, 10).Plain()
	if got := strings.TrimRight(rows[0][4:], " "); got != "- [ ] todo" {
		t.Fatalf("caret task row = %q, want the raw box", got)
	}
}

func TestImageAltTextConceal(t *testing.T) {
	t.Parallel()
	doc := composeDoc("![alt text](img.png)", "plain end")
	p := composePane(doc, 30)
	rows := p.Render(40, 10).Plain()
	if got := strings.TrimRight(rows[0][4:], " "); got != "alt text" {
		t.Fatalf("image row = %q, want the alt text", got)
	}
	doc.cursor = buffer.Pos{Line: 0, Col: 1}
	rows = p.Render(40, 10).Plain()
	if got := strings.TrimRight(rows[0][4:], " "); got != "![alt text](img.png)" {
		t.Fatalf("caret image row = %q, want the raw syntax", got)
	}
}

func TestInlineHTMLStaysLiteral(t *testing.T) {
	t.Parallel()
	doc := composeDoc("<b>hi</b> and <i>x</i>", "plain end")
	p := composePane(doc, 30)
	if got := strings.TrimRight(p.Render(40, 10).Plain()[0][4:], " "); got != "<b>hi</b> and <i>x</i>" {
		t.Fatalf("inline HTML row = %q, want it literal", got)
	}
}

func TestSetextHeadingConceal(t *testing.T) {
	t.Parallel()
	// The tokenizer paints both the text and the underline as a heading.
	doc := newDoc("Title", "=====", "after", "tail")
	doc.cursor = buffer.Pos{Line: 3, Col: 0}
	doc.spans = map[int][]syntax.Span{
		0: {{Start: 0, End: 5, Kind: syntax.KindHeading}},
		1: {{Start: 0, End: 5, Kind: syntax.KindHeading}},
	}
	p := composePane(doc, 30)
	rows := p.Render(40, 10).Plain()
	if got := strings.TrimRight(rows[0][4:], " "); got != "Title" {
		t.Fatalf("setext text row = %q", got)
	}
	if got := strings.TrimRight(rows[1][4:], " "); got != "" {
		t.Fatalf("setext underline row = %q, want it concealed", got)
	}
	// The caret on the underline reveals it.
	doc.cursor = buffer.Pos{Line: 1, Col: 0}
	rows = p.Render(40, 10).Plain()
	if got := strings.TrimRight(rows[1][4:], " "); got != "=====" {
		t.Fatalf("caret underline row = %q, want the raw underline", got)
	}
}

func TestThematicBreakAfterABlankIsNotASetextUnderline(t *testing.T) {
	t.Parallel()
	doc := newDoc("", "---", "after", "tail")
	doc.cursor = buffer.Pos{Line: 3, Col: 0}
	p := composePane(doc, 30)
	rows := p.Render(40, 10).Plain()
	if got := strings.TrimRight(rows[1][4:], " "); !strings.HasPrefix(got, "─") {
		t.Fatalf("row 1 = %q, want the thematic rule", got)
	}
}
