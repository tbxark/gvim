package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/pluginapi"
)

// TestPlugDecorComposeOrdering checks the composed-page ordering of the plugin overlay
// kinds: compose_only line/highlight/conceal items and virtual rows are delivered to the
// composing pane and painted after the compose, virtual rows keep their order around the
// composed text rows, and a normal pane gets none of the compose-only items.
func TestPlugDecorComposeOrdering(t *testing.T) {
	m, f, _ := decorModel(t, []string{"alpha beta", "gamma", "delta"})
	h := m.plug.host
	decs := []pluginapi.Decoration{
		{Kind: pluginapi.DecorLine, Line: 0, Style: bgStyle(dBlue)},
		{Kind: pluginapi.DecorHighlight, ScopeEnd: -1,
			Range: pluginapi.Range{From: pluginapi.Pos{Line: 0, Col: 0}, To: pluginapi.Pos{Line: 0, Col: 5}},
			Style: pluginapi.Style{FG: lit(dRed)}},
		{Kind: pluginapi.DecorConceal, ComposeOnly: true, ScopeEnd: -1,
			Range: pluginapi.Range{From: pluginapi.Pos{Line: 1, Col: 0}, To: pluginapi.Pos{Line: 1, Col: 5}},
			Text:  "_"},
		{Kind: pluginapi.DecorVirtualLineAbove, Line: 1, Text: "above", ComposeOnly: true, ScopeEnd: -1,
			Style: pluginapi.Style{FG: lit(dGreen)}},
		{Kind: pluginapi.DecorVirtualLineBelow, Line: 0, Text: "below", ComposeOnly: true, ScopeEnd: -1,
			Style: pluginapi.Style{FG: lit(dGreen)}},
	}
	if err := h.DecorSet("p", "ns", 1, decs, nil); err != nil {
		t.Fatal(err)
	}
	doc := f.Active()

	// The composing pane gets every kind, outside the compose: the virtual rows sandwich
	// the composed text rows and the compose-only decorations paint on them.
	page := m.paneFor(splitPane{Focused: true}, doc)
	page.PageView = editorpane.PageView{On: true, Measure: 30, Markdown: true}
	page.PageExtraRows = editorpane.PageViewTableBorders(doc, page.Styles)
	page = m.plugDecor(page, doc)
	if page.ExtraRows == nil || page.Conceals == nil || page.Overlays == nil {
		t.Fatalf("compose hooks not wired: extra rows %v conceals %v overlays %v",
			page.ExtraRows != nil, page.Conceals != nil, page.Overlays != nil)
	}

	g := page.Render(40, 14)
	rows := g.Plain()
	index := func(sub string) int {
		for i, r := range rows {
			if strings.Contains(r, sub) {
				return i
			}
		}
		return -1
	}
	alpha, below, above, gamma, delta := index("alpha"), index("below"), index("above"), index("_"), index("delta")
	if alpha < 0 || below < 0 || above < 0 || gamma < 0 || delta < 0 {
		t.Fatalf("composed rows missing: alpha %d below %d above %d gamma %d delta %d / %s",
			alpha, below, above, gamma, delta, strings.Join(rows, " / "))
	}
	if alpha >= below || below >= above || above >= gamma || gamma >= delta {
		t.Fatalf("overlay order around the composed rows is wrong: alpha %d below %d above %d gamma %d delta %d",
			alpha, below, above, gamma, delta)
	}
	if !strings.Contains(rows[gamma], "_") || strings.Contains(rows[gamma], "gamma") {
		t.Fatalf("composed conceal row = %q, want the replacement text and the hidden line", rows[gamma])
	}
	// The overlay kinds stay off the composed rows by design (the mapped-decoration
	// no-paint decision in docs/markdown.md): conceal and virtual rows survive the
	// compose, a line background and a highlight do not.
	for x := 0; x < g.Width(); x++ {
		c := g.At(x, alpha)
		if c.Style.BG == dBlue || c.Style.FG == dRed {
			t.Fatalf("a line/highlight overlay painted on the composed row at x=%d: %+v", x, c.Style)
		}
	}

	// A normal pane gets none of the compose-only overlay: no virtual rows, no line
	// background, the line is not concealed.
	normal := m.paneFor(splitPane{Focused: true}, doc)
	ng := normal.Render(40, 14)
	nrows := ng.Plain()
	joined := strings.Join(nrows, " / ")
	if strings.Contains(joined, "above") || strings.Contains(joined, "below") {
		t.Fatalf("compose-only virtual rows reached the normal pane: %s", joined)
	}
	// Ordinary (not compose-only) items paint on the normal pane: the mapped-decoration
	// no-paint rule is a page-view rule, not a normal-pane one.
	findBlue, findRed := false, false
	for x := 0; x < ng.Width(); x++ {
		if ng.At(x, 0).Style.BG == dBlue {
			findBlue = true
		}
		if ng.At(x, 0).Style.FG == dRed {
			findRed = true
		}
	}
	if !findBlue || !findRed {
		t.Fatalf("the normal pane misses its line/highlight decorations: blue %v red %v", findBlue, findRed)
	}
	if !strings.Contains(joined, "gamma") {
		t.Fatalf("the normal pane lost its text: %s", joined)
	}
}
