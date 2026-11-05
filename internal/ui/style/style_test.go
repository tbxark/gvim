package style

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/internal/theme"
)

func loadDark(t *testing.T) theme.Theme {
	t.Helper()
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func TestNewReadsTheDarkThemeColors(t *testing.T) {
	s := New(loadDark(t))
	editor := Cell(s.Editor)
	if editor.FG != (color.RGBA{R: 212, G: 212, B: 212, A: 255}) {
		t.Errorf("editor fg = %v", editor.FG)
	}
	if editor.BG != (color.RGBA{R: 30, G: 30, B: 30, A: 255}) {
		t.Errorf("editor bg = %v", editor.BG)
	}
	if sel := Cell(s.Selection).BG; sel != (color.RGBA{R: 50, G: 50, B: 60, A: 255}) {
		t.Errorf("selection bg = %v", sel)
	}
}

func TestNamedColorsBecomeANSIEntries(t *testing.T) {
	s := New(loadDark(t))
	// The dark theme names Yellow for the active tab, which is ANSI entry 3.
	if got := Cell(s.TabActive).FG; got != lipgloss.Color("3") {
		t.Errorf("active tab fg = %v, want ANSI 3", got)
	}
}

func TestColorReturnsNilForMissingDefaultAndUnknown(t *testing.T) {
	th := loadDark(t)
	if got := Color(th, "ui", "no_such_key"); got != nil {
		t.Errorf("missing key = %v", got)
	}
	if got := Color(th, "ui", "terminal_bg"); got != nil {
		t.Errorf("terminal default = %v", got)
	}
	unknown, err := theme.Parse([]byte(`{"ui": {"x": "Neon"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := Color(unknown, "ui", "x"); got != nil {
		t.Errorf("unknown name = %v", got)
	}
}

func TestANSINameTableMatchesRatatui(t *testing.T) {
	want := map[string]int{
		"Black": 0, "Red": 1, "Green": 2, "Yellow": 3, "Blue": 4, "Magenta": 5,
		"Cyan": 6, "Gray": 7, "DarkGray": 8, "LightRed": 9, "LightGreen": 10,
		"LightYellow": 11, "LightBlue": 12, "LightMagenta": 13, "LightCyan": 14, "White": 15,
	}
	for name, idx := range want {
		if got := ansiNames[name]; got != idx {
			t.Errorf("%s = %d, want %d", name, got, idx)
		}
	}
	if len(ansiNames) != len(want) {
		t.Errorf("table has %d names, want %d", len(ansiNames), len(want))
	}
}

func TestModifiersReachTheStyle(t *testing.T) {
	th, err := theme.Parse([]byte(`{
		"ui": {
			"tab_inactive_fg": {"color": [1, 2, 3], "modifier": ["bold", "underlined", "italic", "dimmed"]},
			"tab_inactive_bg": [9, 9, 9]
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	st := Cell(New(th).Tab)
	if !st.Bold || !st.Underline || !st.Italic || !st.Faint {
		t.Errorf("modifiers not applied: %+v", st)
	}
	if st.Reverse {
		t.Error("reverse set without a modifier")
	}
	if st.BG != (color.RGBA{R: 9, G: 9, B: 9, A: 255}) {
		t.Errorf("background = %v", st.BG)
	}
}

func TestCellLeavesUnsetColorsNil(t *testing.T) {
	st := Cell(lipgloss.NewStyle())
	if st.FG != nil || st.BG != nil {
		t.Errorf("unset style has colors: %+v", st)
	}
	if st.Bold || st.Reverse || st.Italic || st.Underline || st.Faint {
		t.Errorf("unset style has attributes: %+v", st)
	}
}

func TestCellReadsSetAttributes(t *testing.T) {
	st := Cell(lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Reverse(true))
	if st.FG != lipgloss.Color("1") || !st.Reverse || st.BG != nil {
		t.Errorf("cell = %+v", st)
	}
}

func TestZeroThemeGivesUnstyledRoles(t *testing.T) {
	s := New(theme.Theme{})
	for _, st := range []lipgloss.Style{s.Editor, s.Menu, s.Status, s.TabBar} {
		if c := Cell(st); c.FG != nil || c.BG != nil {
			t.Errorf("zero theme gave colors %+v", c)
		}
	}
}
