package style

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/theme"
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
	if editor.FG != (color.RGBA{R: 230, G: 237, B: 243, A: 255}) {
		t.Errorf("editor fg = %v", editor.FG)
	}
	if editor.BG != (color.RGBA{R: 13, G: 17, B: 23, A: 255}) {
		t.Errorf("editor bg = %v", editor.BG)
	}
	if sel := Cell(s.Selection).BG; sel != (color.RGBA{R: 23, G: 51, B: 90, A: 255}) {
		t.Errorf("selection bg = %v", sel)
	}
}

func TestNewReadsTheSearchMatchColors(t *testing.T) {
	s := New(loadDark(t))
	match := Cell(s.Match)
	if want := (color.RGBA{R: 82, G: 65, B: 27, A: 255}); match.BG != want {
		t.Errorf("match bg = %v, want %v", match.BG, want)
	}
	if match.FG != nil || match.Bold {
		t.Errorf("match sets a foreground or bold: %+v", match)
	}
	cur := Cell(s.CurrentMatch)
	if want := (color.RGBA{R: 227, G: 179, B: 65, A: 255}); cur.BG != want {
		t.Errorf("current match bg = %v, want %v", cur.BG, want)
	}
	if want := (color.RGBA{R: 13, G: 17, B: 23, A: 255}); cur.FG != want {
		t.Errorf("current match fg = %v, want %v", cur.FG, want)
	}
	if !cur.Bold {
		t.Errorf("current match is not bold: %+v", cur)
	}
}

func TestAccentComesFromTheTheme(t *testing.T) {
	t.Parallel()
	c := Cell(New(loadDark(t)).Accent)
	if want := color.Color(color.RGBA{R: 47, G: 129, B: 247, A: 255}); c.BG != want {
		t.Errorf("dark accent bg = %v, want %v", c.BG, want)
	}
	if want := color.Color(color.RGBA{R: 13, G: 17, B: 23, A: 255}); c.FG != want {
		t.Errorf("dark accent fg = %v, want %v", c.FG, want)
	}
	if c := Cell(New(theme.Theme{}).Accent); c.FG != nil || c.BG != nil {
		t.Errorf("zero theme gave an accent %+v", c)
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
		{"current line", Cell(s.CurrentLine), nil, rgb(24, 28, 34)},
		{"ruler", Cell(s.Ruler), nil, rgb(26, 30, 36)},
		{"secondary caret", Cell(s.SecondaryCaret), rgb(230, 237, 243), rgb(72, 79, 88)},
		{"whitespace", Cell(s.Whitespace), rgb(72, 79, 88), rgb(13, 17, 23)},
		{"occurrence", Cell(s.Occurrence), nil, rgb(39, 43, 49)},
		{"diagnostic error", Cell(s.DiagError), rgb(255, 123, 114), nil},
		{"diagnostic warning", Cell(s.DiagWarning), rgb(210, 153, 34), nil},
		{"diagnostic info", Cell(s.DiagInfo), rgb(88, 166, 255), nil},
		{"diagnostic hint", Cell(s.DiagHint), rgb(110, 118, 129), nil},
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

func TestOverlayTakesThePopupColorsAndTheAccent(t *testing.T) {
	t.Parallel()
	ov := New(loadDark(t)).Overlay
	if got := ov.Box; got.FG != color.Color(rgb(230, 237, 243)) || got.BG != color.Color(rgb(13, 17, 23)) {
		t.Errorf("box = %v on %v, want the popup text on the popup fill", got.FG, got.BG)
	}
	if got := ov.Selected; got.FG != color.Color(rgb(13, 17, 23)) || got.BG != color.Color(rgb(47, 129, 247)) {
		t.Errorf("selected = %v on %v, want the accent text on the accent", got.FG, got.BG)
	}
	if got := ov.Border; got.FG != color.Color(rgb(47, 129, 247)) {
		t.Errorf("border fg = %v, want the accent", got.FG)
	}
	if got := ov.Idle; got.BG != color.Color(rgb(67, 72, 78)) {
		t.Errorf("idle button bg = %v, want the popup fill mixed toward its text", got.BG)
	}
	if got := ov.Hover; got.BG != color.Color(rgb(100, 105, 111)) {
		t.Errorf("hover button bg = %v", got.BG)
	}
	if got := ov.Muted; got.FG != color.Color(rgb(132, 138, 144)) {
		t.Errorf("muted fg = %v", got.FG)
	}
}

func TestOverlayWithoutRGBUsesAttributes(t *testing.T) {
	t.Parallel()
	th, err := theme.Parse([]byte(`{"ui": {"popup_bg": "Default", "popup_text_fg": "Default"}}`))
	if err != nil {
		t.Fatal(err)
	}
	ov := NewOverlay(th)
	if !ov.Muted.Faint || !ov.Idle.Reverse || !ov.Hover.Reverse || !ov.Hover.Bold {
		t.Errorf("secondary roles without RGB = muted %+v idle %+v hover %+v", ov.Muted, ov.Idle, ov.Hover)
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
			refPopupText, refPopupBG, refAccentFG, refAccentBG,
		} {
			if _, ok := th.Value(r.section, r.key); !ok {
				t.Errorf("theme %s has no %s.%s", name, r.section, r.key)
			}
		}
	}
}

func TestActiveTabReadsItsOwnThemeKeys(t *testing.T) {
	t.Parallel()
	c := Cell(New(loadDark(t)).TabActive)
	if want := color.Color(rgb(23, 51, 90)); c.BG != want {
		t.Errorf("active tab bg = %v, want the weak accent tint %v", c.BG, want)
	}
	if want := color.Color(rgb(230, 237, 243)); c.FG != want {
		t.Errorf("active tab fg = %v, want the editor text %v", c.FG, want)
	}
	if c := Cell(New(loadDark(t)).Accent); c.BG == Cell(New(loadDark(t)).TabActive).BG {
		t.Error("the active tab takes the accent color")
	}
}
