package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// keysOf is a test shorthand for a theme key id.
func keysOf(section, key string) uint16 { return style.KeyIDFor(section, key) }

// findCaptured returns the first cell whose captured ids match want on the given
// side ("fg" or "bg" and its id; the other side is unconstrained).
func findCaptured(g *grid.Grid, side string, id uint16) (x, y int, ok bool) {
	for y := 0; y < g.Height(); y++ {
		for x := 0; x < g.Width(); x++ {
			fg, bg := g.KeysAt(x, y)
			if (side == "fg" && fg == id) || (side == "bg" && bg == id) {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

// inspectText returns the inspector's lines as one string, opened at x, y.
func inspectText(t *testing.T, m *Model, x, y int) string {
	t.Helper()
	m.inspectThemeAt(x, y)
	top := m.overlays[len(m.overlays)-1]
	page, ok := top.(*themeInfoPage)
	if !ok {
		t.Fatalf("the top overlay is %T, want the inspector", top)
	}
	var b strings.Builder
	for _, line := range page.lines {
		b.WriteString(line.text)
		b.WriteString("\n")
	}
	return b.String()
}

// TestInspectReportsPaintedKeys checks the exact provenance block for the first
// surfaces: editor text, gutter, status bar and an overlay selection row report
// the theme keys that painted them, not just matching colours.
func TestInspectReportsPaintedKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		side  string
		key   uint16
		want  []string
		setup func(*Model) (*grid.Grid, int, int)
	}{
		{
			name: "editor text", side: "fg", key: keysOf("editor", "fg"),
			want: []string{"Painted by (fg): editor.fg", "Painted by (bg): editor.bg", "Painted by: editor.fg on editor.bg"},
			setup: func(m *Model) (*grid.Grid, int, int) {
				g := m.paintCaptured()
				x, y, ok := findCaptured(g, "fg", keysOf("editor", "fg"))
				if !ok {
					t.Fatal("no cell painted from editor.fg")
				}
				return g, x, y
			},
		},
		{
			name: "gutter", side: "fg", key: keysOf("editor", "line_number_fg"),
			want: []string{"Painted by (fg): editor.line_number_fg", "Painted by (bg): editor.line_number_bg"},
			setup: func(m *Model) (*grid.Grid, int, int) {
				g := m.paintCaptured()
				x, y, ok := findCaptured(g, "fg", keysOf("editor", "line_number_fg"))
				if !ok {
					t.Fatal("no gutter cell painted from editor.line_number_fg")
				}
				return g, x, y
			},
		},
		{
			name: "status bar", side: "bg", key: keysOf("ui", "status_bar_bg"),
			want: []string{"Painted by (fg): ui.status_bar_fg", "Painted by (bg): ui.status_bar_bg"},
			setup: func(m *Model) (*grid.Grid, int, int) {
				g := m.paintCaptured()
				x, y, ok := findCaptured(g, "bg", keysOf("ui", "status_bar_bg"))
				if !ok {
					t.Fatal("no status bar cell painted from ui.status_bar_bg")
				}
				return g, x, y
			},
		},
		{
			name: "menu bar", side: "fg", key: keysOf("ui", "menu_fg"),
			want: []string{"Painted by (fg): ui.menu_fg", "Painted by (bg): ui.menu_bg"},
			setup: func(m *Model) (*grid.Grid, int, int) {
				g := m.paintCaptured()
				x, y, ok := findCaptured(g, "fg", keysOf("ui", "menu_fg"))
				if !ok {
					t.Fatal("no menu bar cell painted from ui.menu_fg")
				}
				return g, x, y
			},
		},
		{
			name: "palette selection", side: "bg", key: keysOf("ui", "accent_bg"),
			want: []string{"Painted by (fg): ui.accent_fg", "Painted by (bg): ui.accent_bg"},
			setup: func(m *Model) (*grid.Grid, int, int) {
				m.Update(palette.RunCommandMsg{Action: "open_settings"})
				g := m.paintCaptured()
				x, y, ok := findCaptured(g, "bg", keysOf("ui", "accent_bg"))
				if !ok {
					t.Fatal("no accent cell in the settings list")
				}
				return g, x, y
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newThemeTestModel(t, nil)
			_, x, y := tt.setup(m)
			body := inspectText(t, m, x, y)
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("inspect output lacks %q:\n%s", want, body)
				}
			}
		})
	}
}

// TestInspectFallsBackToColourMatch checks a cell no surface attributed (the theme
// editor's swatch cells are ad-hoc styles): the output says the keys were matched
// by colour instead of claiming provenance.
func TestInspectFallsBackToColourMatch(t *testing.T) {
	t.Parallel()
	m := newThemeTestModel(t, nil)
	m.Update(palette.RunCommandMsg{Action: "edit_theme"})
	g := m.paintCaptured()
	var x, y int
	found := false
	for yy := 0; yy < g.Height() && !found; yy++ {
		for xx := 0; xx < g.Width(); xx++ {
			fg, bg := g.KeysAt(xx, yy)
			cell := g.At(xx, yy)
			if fg == 0 && bg == 0 && cell.Style.FG != nil {
				x, y, found = xx, yy, true
				break
			}
		}
	}
	if !found {
		t.Fatal("no unattributed coloured cell in the theme editor")
	}
	body := inspectText(t, m, x, y)
	if !strings.Contains(body, "matched by colour") {
		t.Errorf("an unattributed cell did not say it matched by colour:\n%s", body)
	}
	if strings.Contains(body, "Painted by (fg):") || strings.Contains(body, "Painted by (bg):") {
		t.Errorf("an unattributed cell claimed provenance:\n%s", body)
	}
}

// TestNormalPaintHasNoAttribution checks the cost boundary: the normal frame never
// allocates the capture buffer and records nothing, while the captured repaint
// draws exactly the same text.
func TestNormalPaintHasNoAttribution(t *testing.T) {
	t.Parallel()
	m := newThemeTestModel(t, nil)
	normal := m.paint()
	if normal.CapturingKeys() {
		t.Fatal("the normal paint enabled key capture")
	}
	normalText := normal.Plain()
	if fg, bg := normal.KeysAt(1, 1); fg != 0 || bg != 0 {
		t.Errorf("normal paint recorded keys %d/%d", fg, bg)
	}
	captured := m.paintCaptured()
	if !captured.CapturingKeys() {
		t.Fatal("paintCaptured did not enable capture")
	}
	if got, want := strings.Join(captured.Plain(), "\n"), strings.Join(normalText, "\n"); got != want {
		t.Errorf("the captured repaint changed the frame text:\n%s\n--- want ---\n%s", got, want)
	}
}

// spanEditor reports the active document with a syntax span on line 0, so the
// editor pane paints a keyword-styled cell.
type spanEditor struct{ *fakeEditor }

func (e spanEditor) Active() Document { return spanDoc{Document: e.fakeEditor.Active()} }

// TestInspectReportsSyntaxKey checks the editor text/syntax surface: a keyword
// cell reports syntax.keyword as its foreground provenance.
func TestInspectReportsSyntaxKey(t *testing.T) {
	t.Parallel()
	m := New(Deps{Editor: spanEditor{newFakeEditor("package main\n")}, Keys: testKeys(), Theme: mustDarkTheme(t), Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	g := m.paintCaptured()
	x, y, ok := findCaptured(g, "fg", keysOf("syntax", "keyword"))
	if !ok {
		t.Fatal("no cell painted from syntax.keyword")
	}
	body := inspectText(t, m, x, y)
	for _, want := range []string{"Painted by (fg): syntax.keyword", "Painted by (bg): editor.bg"} {
		if !strings.Contains(body, want) {
			t.Errorf("inspect output lacks %q:\n%s", want, body)
		}
	}
}

// mustDarkTheme loads the bundled dark theme for a test.
func mustDarkTheme(t *testing.T) theme.Theme {
	t.Helper()
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	return th
}
