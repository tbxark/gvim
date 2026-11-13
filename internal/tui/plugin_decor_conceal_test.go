package tui

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/split"
	"github.com/tbxark/gvim/pluginapi"
)

// TestPlugDecorConcealAndCursorLine checks the root hands the pane's tier-1c hooks the
// conceal spans and the cursor-line overlay, and that the pane paints them.
func TestPlugDecorConcealAndCursorLine(t *testing.T) {
	m, f, _ := decorModel(t, []string{"secret value", "next"})
	h := m.plug.host
	if err := h.DecorSet("p", "ns", 1, []pluginapi.Decoration{
		{Kind: pluginapi.DecorConceal, Range: pluginapi.Range{
			From: pluginapi.Pos{Line: 0, Col: 0}, To: pluginapi.Pos{Line: 0, Col: 6},
		}, Text: "..."},
		{Kind: pluginapi.DecorCursorLine, Style: pluginapi.Style{BG: pluginapi.Color{Set: true, R: 1}}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	doc := f.Active()
	p := m.paneFor(split.Pane{Focused: true}, doc)
	if p.Conceals == nil || p.CursorLineOverlay == nil {
		t.Fatalf("hooks not wired: conceal %v cursor %v", p.Conceals != nil, p.CursorLineOverlay != nil)
	}
	if got := p.Conceals(0); len(got) != 1 || got[0].Start != 0 || got[0].End != 6 {
		t.Fatalf("conceals = %+v", got)
	}
	if _, ok := p.CursorLineOverlay(); !ok {
		t.Fatal("no cursor-line overlay")
	}
	g := p.Render(30, 2)
	if got := g.At(6, 0).Text; got != "." {
		t.Fatalf("concealed first cell = %q", got)
	}
}

// TestPlugDecorComposeOnlyPerPane checks compose_only items reach a composing pane only.
func TestPlugDecorComposeOnlyPerPane(t *testing.T) {
	m, f, _ := decorModel(t, []string{"abc"})
	h := m.plug.host
	if _, err := h.DecorAdd("p", "ns", 1, pluginapi.Decoration{
		Kind: pluginapi.DecorConceal, ComposeOnly: true,
		Range: pluginapi.Range{From: pluginapi.Pos{Line: 0, Col: 0}, To: pluginapi.Pos{Line: 0, Col: 3}},
		Text:  "#",
	}, nil); err != nil {
		t.Fatal(err)
	}
	doc := f.Active()
	p := m.paneFor(split.Pane{Focused: true}, doc)
	if got := p.Conceals(0); len(got) != 0 {
		t.Fatalf("a compose-only conceal reached a normal pane: %+v", got)
	}
	page := m.viewPane(split.Pane{}, editorpane.Pane{}, doc)
	page.PageView.On = true
	page = m.plugDecor(page, doc)
	if got := page.Conceals(0); len(got) != 1 {
		t.Fatalf("the compose-only conceal did not reach the composing pane: %+v", got)
	}
}
