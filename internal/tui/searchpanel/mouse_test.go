package searchpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMousePanelControls(t *testing.T) {
	p := New(searchOptions(&fakeSearcher{result: sampleResult()}))
	search(t, p, "foo")
	draw(p, 160, 30)
	geo := layoutFor(160, 30, false)
	click := func(x, y int) tea.Cmd {
		_, cmd := p.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
		return cmd
	}
	click(geo.x0+labelCols, geo.replY)
	if p.focus != focusReplace {
		t.Fatal("replacement field did not receive focus")
	}
	click(geo.x0, geo.optY)
	if p.scope != OpenBuffers {
		t.Fatal("scope did not cycle")
	}
	draw(p, 160, 30)
	click(geo.x0, geo.resultsTop+1)
	if p.focus != focusResults || p.selected != 1 || p.checked[0][0] {
		t.Fatal("match checkbox did not toggle")
	}
	draw(p, 160, 30)
	cmd := click(geo.x0+6, geo.resultsTop+1)
	if cmd == nil {
		t.Fatal("match click did not open")
	}
	if msg, ok := cmd().(OpenMsg); !ok || msg.Path != "/w/a.go" {
		t.Fatalf("open message = %#v", msg)
	}
}

func TestMouseGitAndResultBoundaries(t *testing.T) {
	p := New(Options{Git: true, Query: "foo"})
	draw(p, 100, 30)
	g := p.mouseGeo
	_, cmd := p.Update(tea.MouseClickMsg(tea.Mouse{X: g.x0, Y: g.optY, Button: tea.MouseLeft}))
	if !p.modes.CaseSensitive || cmd == nil {
		t.Fatal("Git case click did not requery")
	}
	p.Update(tea.MouseClickMsg(tea.Mouse{X: g.x0 + labelCols, Y: g.globY, Button: tea.MouseLeft}))
	if p.focus != focusGlob {
		t.Fatal("Git files field did not focus")
	}

	s := New(searchOptions(&fakeSearcher{result: sampleResult()}))
	search(t, s, "foo")
	draw(s, 100, 30)
	g = s.mouseGeo
	s.Update(tea.MouseClickMsg(tea.Mouse{X: g.x0 + 4, Y: g.resultsTop, Button: tea.MouseLeft}))
	if s.expanded[0] {
		t.Fatal("file row did not fold")
	}
	draw(s, 100, 30)
	s.Update(tea.MouseClickMsg(tea.Mouse{X: g.x0 + 4, Y: g.resultsTop, Button: tea.MouseLeft}))
	if !s.expanded[0] {
		t.Fatal("file row did not unfold")
	}
}

func TestMouseOptionsAndBounds(t *testing.T) {
	p := New(searchOptions(&fakeSearcher{result: sampleResult()}))
	search(t, p, "foo")
	draw(p, 160, 30)
	geo := p.mouseGeo
	click := func(x, y int, button tea.MouseButton) tea.Cmd {
		_, c := p.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: button}))
		return c
	}
	// Locate visible controls from the painted row, including their spacing.
	line := draw(p, 160, 30).Plain()[geo.optY]
	for _, tc := range []struct {
		label string
		check func() bool
	}{
		{"Case (Alt+C)", func() bool { return p.modes.CaseSensitive }},
		{"Regex (Alt+R)", func() bool { return p.modes.Regex }},
		{"Whole (Alt+W)", func() bool { return p.modes.WholeWord }},
	} {
		x := strings.Index(line, tc.label)
		if x < 0 {
			t.Fatalf("missing %s", tc.label)
		}
		click(x, geo.optY, tea.MouseLeft)
		if !tc.check() {
			t.Fatalf("%s did not toggle", tc.label)
		}
	}
	// Redraw after option changes; Replace All follows the keyboard command.
	line = draw(p, 160, 30).Plain()[geo.optY]
	x := strings.Index(line, "Replace All")
	if x < 0 {
		t.Fatal("missing Replace All")
	}
	if cmd := click(x, geo.optY, tea.MouseLeft); cmd == nil {
		t.Fatal("Replace All did not issue a command")
	}
	before := p.scope
	click(geo.x0, geo.optY, tea.MouseRight)
	click(geo.x0-1, geo.optY, tea.MouseLeft)
	if p.scope != before {
		t.Fatal("outside or right click changed scope")
	}
	draw(p, 20, 5)
	click(geo.x0, geo.optY, tea.MouseLeft)
	if p.scope != before {
		t.Fatal("stale geometry accepted click")
	}
}
