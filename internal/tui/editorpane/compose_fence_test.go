package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/syntax"
)

// fenceDoc returns a Markdown document with a fenced block and a tail line whose
// caret keeps the block concealed.
func fenceDoc(lines ...string) *fakeDoc {
	all := append(append([]string{}, lines...), "tail")
	doc := newDoc(all...)
	doc.cursor = buffer.Pos{Line: len(all) - 1, Col: 0}
	return doc
}

func TestFenceFrameCells(t *testing.T) {
	t.Parallel()
	doc := fenceDoc("```go", "fmt.Println(1)", "```", "after")
	p := composePane(doc, 30)
	g := p.Render(40, 8)
	rows := g.Plain()
	// Frame width = measure-4 = 26; margin = (39-30)/2 = 4.
	top := strings.TrimRight(rows[0][4:], " ")
	if !strings.HasPrefix(top, "╭ go ─") || !strings.HasSuffix(top, "╮") || len([]rune(top)) != 26 {
		t.Fatalf("top border = %q, want a 26-cell frame with the go label", top)
	}
	body := strings.TrimRight(rows[1][4:], " ")
	if !strings.HasPrefix(body, "│ fmt.Println(1)") || !strings.HasSuffix(body, "│") {
		t.Fatalf("body row = %q, want rails around the code", body)
	}
	if len([]rune(body)) != 26 {
		t.Fatalf("body row = %q is %d cells, want the 26-cell frame", body, len([]rune(body)))
	}
	bottom := strings.TrimRight(rows[2][4:], " ")
	if !strings.HasPrefix(bottom, "╰") || !strings.HasSuffix(bottom, "╯") || len([]rune(bottom)) != 26 {
		t.Fatalf("bottom border = %q, want the closing frame", bottom)
	}
}

func TestFenceBodyKeepsSyntaxKinds(t *testing.T) {
	t.Parallel()
	doc := fenceDoc("```go", "fmt.Println(1)", "```")
	doc.spans = map[int][]syntax.Span{
		1: {{Start: 0, End: 3, Kind: syntax.KindFunction}, {Start: 4, End: 11, Kind: syntax.KindFunction}},
	}
	p := composePane(doc, 30)
	f, ok := p.newFrame(40, 8)
	if !ok {
		t.Fatal("no frame")
	}
	ln := f.layoutLine(1, true)
	// The body row is "│ " + code..., so the code's "f" is cell 2.
	if got := ln.cells[2].kind; got != syntax.KindFunction {
		t.Fatalf("code cell kind = %v, want the function kind from the syntax spans", got)
	}
}

func TestFenceReveal(t *testing.T) {
	t.Parallel()
	doc := fenceDoc("```go", "fmt.Println(1)", "```", "after")
	p := composePane(doc, 30)
	// The caret on the open delimiter reveals the raw line.
	doc.cursor = buffer.Pos{Line: 0, Col: 2}
	rows := p.Render(40, 8).Plain()
	if got := strings.TrimRight(pageRow2(rows, 4, 0), " "); got != "```go" {
		t.Fatalf("caret line = %q, want the raw fence", got)
	}
	// The caret inside the body keeps the frame (no reveal).
	doc.cursor = buffer.Pos{Line: 1, Col: 1}
	rows = p.Render(40, 8).Plain()
	body := strings.TrimRight(pageRow2(rows, 4, 1), " ")
	if !strings.HasPrefix(body, "│ fmt.Println(1)") {
		t.Fatalf("body row under the caret = %q, want the frame kept", body)
	}
}

// pageRow2 returns row y of rows from column margin on.
func pageRow2(rows []string, margin, y int) string {
	if y >= len(rows) || margin >= len(rows[y]) {
		return ""
	}
	return rows[y][margin:]
}

func TestFenceFrameIndent(t *testing.T) {
	t.Parallel()
	doc := fenceDoc("  ```", "x := 1", "  ```", "after")
	p := composePane(doc, 30)
	rows := p.Render(40, 8).Plain()
	// The frame sits two cells in: margin 4 plus the indent 2.
	top := strings.TrimRight(rows[0][4:], " ")
	if !strings.HasPrefix(top, "  ╭") || len([]rune(top)) != 2+24 {
		t.Fatalf("indented top border = %q, want it two cells in and four cells narrower", top)
	}
}
