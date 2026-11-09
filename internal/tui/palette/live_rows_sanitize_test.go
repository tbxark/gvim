package palette

import (
	"strings"
	"testing"
)

// TestLiveGrepRowsSanitizeHostileDisplay: bidi/C1 in a file name or line never reaches the
// painted row, the match offset is remapped onto the cleaned text, and r.rel stays raw so
// the open action still targets the real path.
func TestLiveGrepRowsSanitizeHostileDisplay(t *testing.T) {
	t.Parallel()
	p := New(liveOptions("", &fakeFiles{}, &fakeGrepper{}))
	draw(p, 120, 30)
	rawRel := "\u202Eevil.go"
	p.seq = 1
	p.applyGrep(GrepResultMsg{Seq: 1, Text: "todo", Hits: []GrepHit{
		{Rel: rawRel, Line: 2, Col: 2, Text: "\u202Etodo\u0080 x"},
		{Rel: "term\u200Binal", Line: 1, Col: 1, Text: "\uFEFFtodo term", Source: GrepTerminals, Index: 1},
	}})
	if len(p.rows) != 2 {
		t.Fatalf("rows = %+v", p.rows)
	}
	row := p.rows[0]
	if row.rel != rawRel {
		t.Fatalf("r.rel = %q, want the raw open target %q", row.rel, rawRel)
	}
	if strings.ContainsAny(row.name, "\u202e\u0080") || strings.ContainsAny(row.text, "\u202e\u0080") {
		t.Fatalf("row display kept spoof bytes: name %q text %q", row.name, row.text)
	}
	if row.text != "todo x" || row.mcol != 0 || row.qlen != 4 {
		t.Fatalf("row display = %+v, want text %q mcol 0 qlen 4", row, "todo x")
	}
	if p.rows[1].grepSource != GrepTerminals || strings.ContainsAny(p.rows[1].name, "\u200b\ufeff") {
		t.Fatalf("terminal row = %+v, want sanitized label", p.rows[1])
	}
	joined := strings.Join(shownGrepRows(t, p), "\n")
	if strings.ContainsAny(joined, "\u202e\u0080\u200b\ufeff") {
		t.Fatalf("painted rows kept spoof bytes: %q", joined)
	}
}

// shownGrepRows draws a live palette wide enough and returns the row text for a check
// that the display sanitizer reached the grid.
func shownGrepRows(t *testing.T, p *Palette) []string {
	t.Helper()
	g := draw(p, 120, 30)
	return g.Plain()
}
