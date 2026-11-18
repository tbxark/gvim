package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

func TestPageViewTableBordersInTheFrame(t *testing.T) {
	t.Parallel()
	m, ed := viewModelWith(t, editor.New(editor.Options{}), DefaultViewOptions(),
		"| Name | Qty |\n| --- | --- |\n| a | 1 |\nafter\n", 60, 14)
	ed.Active().SetLanguage("markdown")
	ed.SetCursor(buffer.Pos{Line: 3, Col: 0}, false)
	runAction(t, m, "toggle_page_view")
	rows := frameText(m)
	index := func(want string) int {
		for i, row := range rows {
			if strings.Contains(row, want) {
				return i
			}
		}
		return -1
	}
	top := index("┌──────┬─────┐")
	header := index("│ Name │ Qty │")
	sep := index("├──────┼─────┤")
	bottom := index("└──────┴─────┘")
	if top < 0 || header < 0 || sep < 0 || bottom < 0 {
		t.Fatalf("borders or rows missing: top %d header %d sep %d bottom %d\n%s",
			top, header, sep, bottom, strings.Join(rows, "\n"))
	}
	if top >= header || header >= sep || sep >= bottom {
		t.Fatalf("wrong order: top %d header %d sep %d bottom %d", top, header, sep, bottom)
	}
}

func TestPageViewTableBordersRespectsNoCaretReveal(t *testing.T) {
	t.Parallel()
	m, ed := viewModelWith(t, editor.New(editor.Options{}), DefaultViewOptions(),
		"| Name | Qty |\n| --- | --- |\n| a | 1 |\n", 60, 14)
	ed.Active().SetLanguage("markdown")
	ed.SetCursor(buffer.Pos{Line: 0, Col: 2}, false) // caret inside table header
	runAction(t, m, "markdown_mode_dual")
	rows := frameText(m)
	joined := strings.Join(rows, "\n")
	// Focused left side shows raw markdown source; right composed side has NoCaretReveal=true
	// so it must keep composed table borders even while the caret is on row 0.
	if !strings.Contains(joined, "┌──────┬─────┐") || !strings.Contains(joined, "| --- | --- |") {
		t.Fatalf("dual view missing source or composed table borders:\n%s", joined)
	}
}
