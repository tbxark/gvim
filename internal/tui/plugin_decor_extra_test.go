package tui

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui/split"
	"github.com/tbxark/gvim/pluginapi"
)

// TestPlugDecorVirtualRowsAndSoftBreaks checks the root hands the pane virtual rows above
// and below a line and soft breaks, and that the pane paints them with the source mapping.
func TestPlugDecorVirtualRowsAndSoftBreaks(t *testing.T) {
	m, f, _ := decorModel(t, []string{"abcdef"})
	h := m.plug.host
	if _, err := h.DecorAdd("p", "ns", 1, pluginapi.Decoration{
		Kind: pluginapi.DecorVirtualLineAbove, Line: 0, Text: "removed", GutterText: "-",
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DecorAdd("p", "ns", 1, pluginapi.Decoration{
		Kind: pluginapi.DecorSoftBreak, Pos: pluginapi.Pos{Line: 0, Col: 3}, Indent: 2, PrefixText: "> ",
	}, nil); err != nil {
		t.Fatal(err)
	}
	doc := f.Active()
	p := m.paneFor(split.Pane{Focused: true}, doc)
	if p.ExtraRows == nil || p.SoftBreaks == nil {
		t.Fatalf("hooks not wired: extras %v soft %v", p.ExtraRows != nil, p.SoftBreaks != nil)
	}
	g := p.Render(30, 4)
	row := func(y int) string {
		var b []byte
		for x := 6; x < 30 && len(b) < 20; x++ {
			b = append(b, g.At(x, y).Text...)
		}
		return string(b)
	}
	if got := row(1); len(got) < 3 || got[:3] != "abc" {
		t.Fatalf("row 1 = %q, want the first soft-break row", got)
	}
	if got := row(2); len(got) < 8 || got[:8] != "  > def " {
		t.Fatalf("row 2 = %q, want the soft-break continuation", got)
	}
	if got := row(0); len(got) < 7 || got[:7] != "removed" {
		t.Fatalf("row 0 = %q, want the virtual line", got)
	}
}

// TestPlugDecorPluginFolds checks the pane merges plugin folds: a collapsed range hides its
// body, shows the placeholder and paints the fold marker, and the root toggles it.
func TestPlugDecorPluginFolds(t *testing.T) {
	m, f, pe := decorModel(t, []string{"a", "b", "c", "d", "e"})
	pe.bufs = []pluginapi.BufferInfo{{ID: 1, LineCount: 5}}
	h := m.plug.host
	if err := h.DecorSetFoldingRanges("p", "git_log", 1, []pluginapi.FoldRange{{
		Range:     pluginapi.Range{From: pluginapi.Pos{Line: 0}, To: pluginapi.Pos{Line: 4}},
		Collapsed: true, Placeholder: "[5 lines]",
	}}, nil); err != nil {
		t.Fatal(err)
	}
	doc := f.Active()
	p := m.paneFor(split.Pane{Focused: true}, doc)
	if p.PluginFolds == nil {
		t.Fatal("plugin folds not wired into the pane")
	}
	g := p.Render(40, 5)
	plain := g.Plain()
	// The collapsed body is hidden: the second body row is not "b", and the placeholder shows.
	body := append([]string(nil), plain...)
	if len(body) < 2 {
		t.Fatalf("rows = %v", body)
	}
	found := false
	for _, row := range body {
		for i := 0; i+7 < len(row); i++ {
			if row[i:i+8] == "[5 lines" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("no fold placeholder in %q", body)
	}
	// Toggling opens the plugin fold and the body lines appear.
	if !m.toggleFoldAt(0) {
		t.Fatal("toggleFoldAt did not report a plugin fold")
	}
	p = m.paneFor(split.Pane{Focused: true}, doc)
	g = p.Render(40, 6)
	shown := false
	for y := 0; y < 6; y++ {
		if g.At(6, y).Text == "e" {
			shown = true
		}
	}
	if !shown {
		t.Fatalf("the folded body is still hidden")
	}
}
