package palette

import (
	"strings"
	"testing"
	"unicode/utf8"

	model "github.com/tbxark/gvim/internal/palette"
)

func columnEntries() []model.Entry {
	return []model.Entry{
		{Name: "Save", Keybinding: "Ctrl+S", Description: "Save the file to disk", Action: "save", Source: model.SourceBuiltin},
		{Name: "Add Cursor Above", Keybinding: "Ctrl+Alt+Up", Description: "Place a cursor on the line above the current one", Action: "add_cursor_above", Source: model.SourceBuiltin},
	}
}

// TestWideListPullsTheShortcutColumnToTheNames checks that on a wide terminal the
// name column is as wide as the longest name and its gap, not the share of the box:
// the shortcut starts next to the names and the description gets the rest.
func TestWideListPullsTheShortcutColumnToTheNames(t *testing.T) {
	for _, w := range []int{120, 200} {
		p := New(Options{Query: ">", Commands: columnEntries()})
		g := draw(p, w, 40)
		base := layoutFor(w, 40)
		fit := base.fitColumns(p.rows)
		longest := len("Add Cursor Above")
		if want := base.nameX + longest; fit.nameEnd != want {
			t.Errorf("width %d: name column ends at %d, want %d", w, fit.nameEnd, want)
		}
		if fit.keysX != fit.nameEnd+columnGap {
			t.Errorf("width %d: shortcut column at %d, want %d (the name column and one gap)", w, fit.keysX, fit.nameEnd+columnGap)
		}
		if fit.descW <= base.descW || fit.descX+fit.descW != base.descX+base.descW {
			t.Errorf("width %d: description column [%d, +%d) did not grow to the left of [%d, +%d)", w, fit.descX, fit.descW, base.descX, base.descW)
		}
		var found bool
		for y := fit.top + 1; y < fit.bottom; y++ {
			row := rowText(g, y)
			if strings.Contains(row, "Add Cursor Above") {
				found = true
				if i := utf8.RuneCountInString(row[:strings.Index(row, "Ctrl+Alt+Up")]); i != fit.keysX {
					t.Errorf("width %d: shortcut painted at %d, want %d in %q", w, i, fit.keysX, row)
				}
				if !strings.Contains(row, "Place a cursor on the line above the current one") {
					t.Errorf("width %d: description cut in %q", w, row)
				}
			}
		}
		if !found {
			t.Errorf("width %d: no row for Add Cursor Above", w)
		}
	}
}

func TestPaletteBoxHasRoundedCorners(t *testing.T) {
	g := draw(New(commandOptions(">", nil)), 100, 30)
	geo := layoutFor(100, 30)
	for _, c := range []struct {
		x, y int
		want string
	}{{geo.left, geo.top, "╭"}, {geo.right, geo.top, "╮"}, {geo.left, geo.bottom, "╰"}, {geo.right, geo.bottom, "╯"}} {
		if got := g.At(c.x, c.y).Text; got != c.want {
			t.Errorf("corner (%d, %d) = %q, want %q", c.x, c.y, got, c.want)
		}
	}
}
