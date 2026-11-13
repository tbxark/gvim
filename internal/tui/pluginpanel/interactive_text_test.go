package pluginpanel

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/pluginapi"
)

func TestFocusedTextWithWideRunesPaintsCaret(t *testing.T) {
	// The caret column is in cells; a wide rune must not index the rune slice by cells.
	w := pluginapi.Widget{Kind: pluginapi.WidgetText, Key: "t", Text: "你好"}
	p, g := interactivePanel(w)
	if got := g.Plain()[0]; !strings.Contains(got, "你好") {
		t.Fatalf("row = %q", got)
	}
	p.HandleKey(Key{Name: "left"})
	p.HandleKey(Key{Name: "x", Text: "x", Shift: true})
	g, _ = draw(p, 40, 10)
	if got := g.Plain()[0]; !strings.Contains(got, "你x好") {
		t.Fatalf("row = %q", got)
	}
}

func TestSelectedWideTextKeepsItsCells(t *testing.T) {
	w := pluginapi.Widget{Kind: pluginapi.WidgetText, Key: "t", Text: "你好a"}
	p, _ := interactivePanel(w)
	p.setSelection(w, 0, 6)
	g, rows := draw(p, 40, 10)
	if !strings.Contains(rows[0], "你好a") {
		t.Fatalf("row = %q", rows[0])
	}
	if g.At(1, 0).Text != "你" || g.At(3, 0).Text != "好" {
		t.Fatalf("cells = %q %q", g.At(1, 0).Text, g.At(3, 0).Text)
	}
}

func TestMultilineCaretColumnIsInCells(t *testing.T) {
	w := pluginapi.Widget{Kind: pluginapi.WidgetText, Key: "t", Text: "你好", Rows: 2}
	_, g := interactivePanel(w, func(o *Options) { o.Chrome.Focus = grid.Style{Reverse: true} })
	// The caret is at the end of the line, two wide runes in: cell column 4.
	if got := g.At(4, 0); got.Text != " " || !got.Style.Reverse || g.At(6, 0).Style.Reverse {
		t.Fatalf("caret cell = %+v", got)
	}
}

func TestByteColumnAtCountsCells(t *testing.T) {
	if got := byteColumnAt("你好a", 0, 2); got != 3 {
		t.Fatalf("col 2 = byte %d, want 3", got)
	}
	if got := byteColumnAt("你好a", 0, 3); got != 3 {
		t.Fatalf("col 3 (inside the wide rune 好) = byte %d, want 3", got)
	}
	if got := byteColumnAt("你好a", 0, 9); got != 7 {
		t.Fatalf("col 9 = byte %d, want 7", got)
	}
}

func TestCaretUpDownStaysOnRuneBoundary(t *testing.T) {
	w := pluginapi.Widget{Kind: pluginapi.WidgetText, Key: "t", Text: "éé\nab", Rows: 2}
	p, _ := interactivePanel(w)
	p.setText(w, "éé\nab", len("éé\na"))
	p.HandleKey(Key{Name: "up"})
	if got := p.textCursor(w); got != 2 {
		t.Fatalf("caret = %d, want 2 (between the two runes)", got)
	}
}

func TestTextFieldRowsFollowMeasuredHeight(t *testing.T) {
	w := pluginapi.Widget{Kind: pluginapi.WidgetText, Key: "t", Text: "a\nb\nc", MaxRows: 5}
	_, g := interactivePanel(w)
	rows := g.Plain()
	for i, want := range []string{"a", "b", "c"} {
		if !strings.Contains(rows[i], want) {
			t.Fatalf("row %d = %q, want %q (rows %q)", i, rows[i], want, rows[:4])
		}
	}
}

func TestOptionTextIsSanitised(t *testing.T) {
	cases := []pluginapi.Widget{
		{Kind: pluginapi.WidgetRadio, Key: "r", Options: []string{"a\x07b"}},
		{Kind: pluginapi.WidgetDualList, Key: "d", Options: []string{"a\x07b"}},
		{Kind: pluginapi.WidgetDropdown, Key: "x", Options: []string{"a\x07b"}},
	}
	for _, w := range cases {
		_, g := interactivePanel(w)
		if got := g.Plain()[0]; !strings.Contains(got, "a b") {
			t.Fatalf("%s row = %q, want the control replaced by a space", w.Kind, got)
		}
	}
}
