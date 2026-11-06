package style

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
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

func TestNewReadsTheSearchMatchColors(t *testing.T) {
	s := New(loadDark(t))
	match := Cell(s.Match)
	if want := (color.RGBA{R: 100, G: 100, B: 20, A: 255}); match.BG != want {
		t.Errorf("match bg = %v, want %v", match.BG, want)
	}
	if match.FG != nil || match.Bold {
		t.Errorf("match sets a foreground or bold: %+v", match)
	}
	cur := Cell(s.CurrentMatch)
	if want := (color.RGBA{R: 64, G: 170, B: 230, A: 255}); cur.BG != want {
		t.Errorf("current match bg = %v, want %v", cur.BG, want)
	}
	if want := (color.RGBA{A: 255}); cur.FG != want {
		t.Errorf("current match fg = %v, want %v", cur.FG, want)
	}
	if !cur.Bold {
		t.Errorf("current match is not bold: %+v", cur)
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

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: 0xff} }

func TestDisplayRolesReadTheirThemeKeys(t *testing.T) {
	s := New(loadDark(t))
	cases := []struct {
		name string
		got  grid.Style
		fg   color.Color
		bg   color.Color
	}{
		{"current line", Cell(s.CurrentLine), nil, rgb(40, 40, 40)},
		{"ruler", Cell(s.Ruler), nil, rgb(50, 50, 50)},
		{"secondary caret", Cell(s.SecondaryCaret), rgb(212, 212, 212), rgb(100, 100, 100)},
		{"whitespace", Cell(s.Whitespace), rgb(70, 70, 70), rgb(30, 30, 30)},
		{"occurrence", Cell(s.Occurrence), nil, rgb(75, 75, 75)},
		{"diagnostic error", Cell(s.DiagError), lipgloss.Color("1"), nil},
		{"diagnostic warning", Cell(s.DiagWarning), lipgloss.Color("3"), nil},
		{"diagnostic info", Cell(s.DiagInfo), lipgloss.Color("4"), nil},
		{"diagnostic hint", Cell(s.DiagHint), lipgloss.Color("7"), nil},
		{"popup", Cell(s.Popup), lipgloss.Color("15"), rgb(30, 30, 30)},
		{"popup selected", Cell(s.PopupSelected), rgb(255, 255, 255), rgb(58, 79, 120)},
	}
	for _, tc := range cases {
		if tc.got.FG != tc.fg {
			t.Errorf("%s fg = %v, want %v", tc.name, tc.got.FG, tc.fg)
		}
		if tc.got.BG != tc.bg {
			t.Errorf("%s bg = %v, want %v", tc.name, tc.got.BG, tc.bg)
		}
	}
}

func TestEveryBundledThemeDefinesTheDisplayKeys(t *testing.T) {
	names := theme.Names()
	if len(names) == 0 {
		t.Fatal("no bundled themes")
	}
	for _, name := range names {
		th, err := theme.Load(name)
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		for _, r := range []ref{
			refCurrentLine, refInactiveCursor, refRuler, refWhitespace, refOccurrence,
			refDiagError, refDiagWarning, refDiagInfo, refDiagHint,
			refPopupText, refPopupBG, refPopupSelFG, refPopupSelBG,
		} {
			if _, ok := th.Value(r.section, r.key); !ok {
				t.Errorf("theme %s has no %s.%s", name, r.section, r.key)
			}
		}
	}
}
