package editorpane

import (
	"strings"
	"testing"
)

// A page wider than the pane is clamped to the pane's text width, as Fresh's
// effectiveComposeWidth clamps to the viewport: no margin and no horizontal
// scrolling, the text wraps at the pane.
func TestPageWiderThanThePaneIsClamped(t *testing.T) {
	t.Parallel()
	doc := composeDoc("alpha beta gamma delta epsilon", "plain end")
	p := Pane{Doc: doc, PageView: PageView{On: true, Measure: 200, Markdown: true}}
	g := p.Render(40, 8)
	rows := g.Plain()
	if strings.HasPrefix(rows[0], " ") {
		t.Fatalf("row 0 = %q, want no left margin in a clamped page", rows[0])
	}
	if left, most := p.HScroll(40, 8); left != 0 || most != 0 {
		t.Fatalf("clamped page scrolls: left %d most %d", left, most)
	}
	joined := strings.Join(rows, " ")
	for _, word := range []string{"alpha", "epsilon"} {
		if !strings.Contains(joined, word) {
			t.Fatalf("rows = %q, want %q wrapped into the pane", rows, word)
		}
	}
}
