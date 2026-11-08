package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
)

func TestWhitespaceMarksFollowThePositionOfTheBlank(t *testing.T) {
	all := func(spaces, tabs bool) Whitespace {
		return Whitespace{
			SpacesLeading: spaces, SpacesInner: spaces, SpacesTrailing: spaces,
			TabsLeading: tabs, TabsInner: tabs, TabsTrailing: tabs,
		}
	}
	tests := []struct {
		name string
		line string
		ws   Whitespace
		want string // the text cells from column 6
	}{
		{"nothing", " a b ", all(false, false), " a b "},
		{"all spaces", " a b ", all(true, false), "·a·b·"},
		{"leading spaces only", "  a b  ", Whitespace{SpacesLeading: true}, "··a b  "},
		{"inner spaces only", "  a b  ", Whitespace{SpacesInner: true}, "  a·b  "},
		{"trailing spaces only", "  a b  ", Whitespace{SpacesTrailing: true}, "  a b··"},
		{"a line of blanks is leading", "   ", Whitespace{SpacesLeading: true}, "···"},
		{"a line of blanks is trailing", "   ", Whitespace{SpacesTrailing: true}, "···"},
		{"a line of blanks is not inner", "   ", Whitespace{SpacesInner: true}, "   "},
		{"tabs only", "\ta\tb ", all(false, true), "→   a→  b "},
		{"inner tab only", "\ta\tb", Whitespace{TabsInner: true}, "    a→  b"},
		{"leading tab only", "\ta\tb", Whitespace{TabsLeading: true}, "→   a   b"},
		{"tabs and spaces", "\t a ", all(true, true), "→   ·a·"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ws := tc.ws
			g := Pane{Doc: newDoc(tc.line), Opts: Options{Whitespace: &ws}}.Render(30, 1)
			if got := textFrom(t, g, 0, 6, len([]rune(tc.want))); got != tc.want {
				t.Errorf("cells = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWhitespaceInSelectionIgnoresThePositionFlags(t *testing.T) {
	tests := []struct {
		name        string
		inSelection bool
		want        string
	}{
		{"on", true, "·→   a"},
		{"off", false, "     a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoc(" \t a")
			d.selected, d.anchor, d.head = true, buffer.Pos{}, buffer.Pos{Col: 2}
			ws := Whitespace{InSelection: tc.inSelection}
			g := Pane{
				Doc: d, Opts: Options{Whitespace: &ws},
				Styles: Styles{Selection: grid.Style{BG: cBlue}, Whitespace: grid.Style{FG: cGray}},
			}.Render(30, 1)
			// The selection covers the first space and the tab, not the second space.
			if got := textFrom(t, g, 0, 6, 6); got != tc.want {
				t.Errorf("cells = %q, want %q", got, tc.want)
			}
			if tc.inSelection && cellAt(t, g, 6, 0).Style.FG != cGray {
				t.Error("a selected mark is not in the whitespace color")
			}
		})
	}
}

func TestSelectedWhitespaceMarkUsesTheSelectedColor(t *testing.T) {
	d := newDoc("a b")
	d.selected, d.anchor, d.head = true, buffer.Pos{}, buffer.Pos{Col: 3}
	ws := Whitespace{InSelection: true}
	st := Styles{
		Selection:  grid.Style{BG: cBlue},
		Whitespace: grid.Style{FG: cGray},
		Decor:      DecorStyles{WhitespaceSelected: grid.Style{FG: cGreen}},
	}
	g := Pane{Doc: d, Opts: Options{Whitespace: &ws}, Styles: st}.Render(20, 1)
	if c := cellAt(t, g, 7, 0); c.Text != "·" || c.Style.FG != cGreen {
		t.Errorf("selected space = %q fg %v, want a green dot", c.Text, c.Style.FG)
	}
}

func TestHiddenTabArrowsHoldInASelectionToo(t *testing.T) {
	d := newDoc("\tx")
	d.selected, d.anchor, d.head = true, buffer.Pos{}, buffer.Pos{Col: 1}
	ws := Whitespace{InSelection: true}
	g := Pane{Doc: d, HideTabArrows: true, Opts: Options{Whitespace: &ws}}.Render(20, 1)
	if got := textFrom(t, g, 0, 6, 2); got != "  " {
		t.Errorf("cells = %q, want blanks", got)
	}
}
