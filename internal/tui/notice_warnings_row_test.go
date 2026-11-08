package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A warning row keeps the reason of the warning: the path shrinks first, and the rest
// is cut in its middle, so a reason in the middle or at the end stays on the row.
func TestWarningRowKeepsTheReason(t *testing.T) {
	t.Parallel()
	const config = "/private/tmp/gvim-dogfood/plugauthor/work/proj/.gvim/config.json: invalid JSON with comments: line 1, column 20: parsing value: unexpected EOF"
	const save = "save /private/tmp/gvim-dogfood/writer/w/se_e.txt: encoding: cannot represent U+4E2D in Windows-1252"
	cases := []struct {
		name string
		text string
		room int
		want []string
	}{
		{"config at 80 columns", config, 74, []string{"config.json", "invalid JSON with comments", "unexpected EOF"}},
		{"config at 120 columns", config, 116, []string{"config.json", "invalid JSON with comments", "unexpected EOF"}},
		{"save error at 80 columns", save, 74, []string{"se_e.txt", "Windows-1252"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row := warningText(c.text, c.room)
			if ansi.StringWidth(row) > c.room {
				t.Errorf("row %q is %d cells, room is %d", row, ansi.StringWidth(row), c.room)
			}
			for _, w := range c.want {
				if !strings.Contains(row, w) {
					t.Errorf("row %q lacks %q", row, w)
				}
			}
		})
	}
}

// A text that fits is shown whole, and a text with no path still fits by its middle.
func TestWarningRowFitsOrCutsWhatItIsGiven(t *testing.T) {
	t.Parallel()
	if got := warningText("config: bad key", 40); got != "config: bad key" {
		t.Errorf("a fitting warning = %q, want it whole", got)
	}
	for room := 1; room <= 90; room++ {
		got := warningText("a long warning without a path that goes on and on and on, ending in the reason", room)
		if ansi.StringWidth(got) > room {
			t.Errorf("room %d: %q is wider than the room", room, got)
		}
	}
}
