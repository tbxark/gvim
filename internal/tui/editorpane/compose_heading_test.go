package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

func TestHeadingMarkerConceal(t *testing.T) {
	t.Parallel()
	doc := composeDoc("# Title", "## Two", "#nothash", "text")
	p := composePane(doc, 30)
	rows := p.Render(40, 8).Plain()
	if got := strings.TrimRight(rows[0][4:], " "); got != "Title" {
		t.Fatalf("h1 row = %q, want the marker hidden", got)
	}
	if got := strings.TrimRight(rows[1][4:], " "); got != "Two" {
		t.Fatalf("h2 row = %q, want the marker hidden", got)
	}
	if got := strings.TrimRight(rows[2][4:], " "); got != "#nothash" {
		t.Fatalf("non-heading row = %q, want it literal", got)
	}
	// The caret line reveals the markers, as the other compose conceals do.
	doc.cursor = buffer.Pos{Line: 0, Col: 2}
	rows = p.Render(40, 8).Plain()
	if got := strings.TrimRight(rows[0][4:], " "); got != "# Title" {
		t.Fatalf("caret heading row = %q, want the raw marker", got)
	}
}
