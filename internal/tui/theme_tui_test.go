package tui

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// fakeThemeCatalog is an in-memory ThemeCatalog for the theme TUI tests.
type fakeThemeCatalog struct {
	themes   map[string]theme.Theme
	refs     map[string]string
	reloads  int
	warnings []string
}

func (f *fakeThemeCatalog) Infos() []ThemeInfo {
	infos := make([]ThemeInfo, 0, len(f.refs))
	for ref, name := range f.refs {
		infos = append(infos, ThemeInfo{Name: name, Ref: ref, Builtin: !strings.Contains(ref, ".json")})
	}
	return infos
}

func (f *fakeThemeCatalog) Load(ref string) (theme.Theme, error) {
	if th, ok := f.themes[ref]; ok {
		return th, nil
	}
	return theme.Load(ref)
}

func (f *fakeThemeCatalog) Canonical(ref string) (string, error) {
	if _, ok := f.refs[ref]; ok {
		return ref, nil
	}
	return ref, nil
}

func (f *fakeThemeCatalog) Reload() error { f.reloads++; return nil }

func (f *fakeThemeCatalog) Warnings() []string { return f.warnings }

func (f *fakeThemeCatalog) SaveTheme(name string, th theme.Theme) (string, error) {
	f.themes[name+".json"] = th
	f.refs[name+".json"] = name
	return name + ".json", nil
}

func (f *fakeThemeCatalog) DeleteTheme(ref string) error {
	delete(f.themes, ref)
	delete(f.refs, ref)
	return nil
}

func newThemeTestModel(t *testing.T, catalog ThemeCatalog) *Model {
	t.Helper()
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	m := New(Deps{
		Editor: newFakeEditor("package main\n\nfunc main() {}\n"),
		Keys:   testKeys(), Theme: th, ThemeRef: "dark", ThemeCatalog: catalog, Clock: testClock,
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func TestInspectThemeAtCursor(t *testing.T) {
	t.Parallel()
	m := newThemeTestModel(t, nil)
	m.Update(palette.RunCommandMsg{Action: "inspect_theme_at_cursor"})
	if len(m.overlays) == 0 {
		t.Fatal("inspect_theme_at_cursor opened no overlay")
	}
	page, ok := m.overlays[len(m.overlays)-1].(*themeInfoPage)
	if !ok {
		t.Fatalf("the top overlay is %T, want the theme inspector", m.overlays[len(m.overlays)-1])
	}
	var text strings.Builder
	for _, line := range page.lines {
		text.WriteString(line.text)
		text.WriteString("\n")
	}
	body := text.String()
	for _, want := range []string{"Cell:", "Theme: dark"} {
		if !strings.Contains(body, want) {
			t.Errorf("inspect lines miss %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "Foreground:") && !strings.Contains(body, "Background:") {
		t.Errorf("inspect lines name no theme keys:\n%s", body)
	}
	// Any key dismisses it.
	press(m, special(tea.KeyEscape))
	for _, overlay := range m.overlays {
		if _, ok := overlay.(*themeInfoPage); ok {
			t.Fatal("the inspector stayed open after a key")
		}
	}
}

func TestCtrlRightClickInspectsTheClickedCell(t *testing.T) {
	t.Parallel()
	m := newThemeTestModel(t, nil)
	x, y := 10, 6
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight, Mod: tea.ModCtrl})
	found := false
	for _, overlay := range m.overlays {
		if _, ok := overlay.(*themeInfoPage); ok {
			found = true
		}
	}
	if !found {
		t.Fatalf("Ctrl+Right-click opened no inspector (overlays %d)", len(m.overlays))
	}
}

func TestThemePickerListsCatalogThemes(t *testing.T) {
	t.Parallel()
	catalog := &fakeThemeCatalog{
		themes: map[string]theme.Theme{"mine.json": mustTheme(t)},
		refs:   map[string]string{"dark": "dark", "mine.json": "mine"},
	}
	m := newThemeTestModel(t, catalog)
	m.Update(palette.RunCommandMsg{Action: "select_theme"})
	v, ok := m.overlays[len(m.overlays)-1].(*list.View)
	if !ok {
		t.Fatalf("the theme picker is %T, want a list", m.overlays[len(m.overlays)-1])
	}
	var labels []string
	for _, row := range v.Visible() {
		labels = append(labels, row.Left)
	}
	if !strings.Contains(strings.Join(labels, ","), "mine") {
		t.Fatalf("picker rows = %v, want the user theme", labels)
	}
}

// mustTheme returns a loaded theme for fixtures.
func mustTheme(t *testing.T) theme.Theme {
	t.Helper()
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func TestApplyThemeThroughTheCatalog(t *testing.T) {
	t.Parallel()
	light, err := theme.Load("light")
	if err != nil {
		t.Fatal(err)
	}
	catalog := &fakeThemeCatalog{
		themes: map[string]theme.Theme{"mine.json": light},
		refs:   map[string]string{"dark": "dark", "mine.json": "mine"},
	}
	m := newThemeTestModel(t, catalog)
	m.applyTheme("mine.json")
	if m.themeRef != "mine.json" {
		t.Errorf("themeRef = %q, want mine.json", m.themeRef)
	}
	if m.theme.Name != light.Name {
		t.Errorf("theme = %q, want the loaded light theme", m.theme.Name)
	}
	if m.util.settings.Theme != light.Name {
		t.Errorf("settings.Theme = %q, want the display name", m.util.settings.Theme)
	}
}

func TestThemeOverridesLayerUnderTheStyles(t *testing.T) {
	t.Parallel()
	m := newThemeTestModel(t, nil)
	if err := m.SetThemeOverrides(map[string]string{"editor.bg": "#112233"}); err != nil {
		t.Fatalf("SetThemeOverrides error = %v", err)
	}
	// The editor style background is the override.
	if got, want := style.Cell(m.styles.Editor).BG, color.Color(color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF}); got != want {
		t.Errorf("editor bg = %v, want the override %v", got, want)
	}
	if m.ThemeOverrides()["editor.bg"] != "#112233" {
		t.Errorf("overrides = %v, want the stored map", m.ThemeOverrides())
	}
	if err := m.SetThemeOverrides(map[string]string{"editor.bg": "not a color"}); err == nil {
		t.Error("a bad override color was accepted")
	}
	if err := m.SetThemeOverrides(map[string]string{"ui.nope": "#000000"}); err == nil {
		t.Error("an unknown override key was accepted")
	}
	m.ClearThemeOverrides()
	if len(m.ThemeOverrides()) != 0 {
		t.Errorf("overrides after clear = %v", m.ThemeOverrides())
	}
	// A theme change clears the overrides.
	if err := m.SetThemeOverrides(map[string]string{"editor.bg": "#112233"}); err != nil {
		t.Fatal(err)
	}
	m.applyTheme("dark")
	if len(m.ThemeOverrides()) != 0 {
		t.Errorf("a theme change kept the overrides: %v", m.ThemeOverrides())
	}
}

// spanDoc is a Document whose line 0 has one syntax span, for the syntax-kind
// lookup test.
type spanDoc struct {
	Document
}

func (d spanDoc) LineSpans(int) []syntax.Span {
	return []syntax.Span{{Start: 0, End: 7, Kind: syntax.KindKeyword}}
}

func TestSyntaxKindAt(t *testing.T) {
	t.Parallel()
	doc := spanDoc{Document: newFakeEditor("package main\n").Active()}
	kind, ok := syntaxKindAt(doc, buffer.Pos{Line: 0, Col: 1})
	if !ok || kind != syntax.KindKeyword {
		t.Fatalf("syntaxKindAt = %v, %v; want the keyword span", kind, ok)
	}
	kind, ok = syntaxKindAt(doc, buffer.Pos{Line: 0, Col: 20})
	if !ok || kind != syntax.KindPlain {
		t.Fatalf("syntaxKindAt past the span = %v, %v; want plain", kind, ok)
	}
}

func TestPluginHostThemeAPI(t *testing.T) {
	t.Parallel()
	m := newThemeTestModel(t, nil)
	h := &pluginHost{m: m}
	if err := h.SetThemeOverrides(map[string]string{"editor.bg": "#112233"}); err != nil {
		t.Fatalf("SetThemeOverrides error = %v", err)
	}
	if over, ok := h.ThemeOverrides(); !ok || over["editor.bg"] != "#112233" {
		t.Fatalf("ThemeOverrides = %v, %v", over, ok)
	}
	if info := h.Theme(); info.Name != "dark" || !info.Dark {
		t.Errorf("Theme = %+v, want dark and dark=true", info)
	}
	if r, g, b, ok := h.ThemeColor("editor.bg"); !ok || r != 0x11 || g != 0x22 || b != 0x33 {
		t.Errorf("ThemeColor(editor.bg) = %d,%d,%d,%v; want the override", r, g, b, ok)
	}
	// A named color answers with Fresh's fixed RGB.
	if r, g, b, ok := h.ThemeColor("ui.popup_text_fg"); !ok || r == 0 && g == 0 && b == 0 {
		t.Errorf("ThemeColor(named) = %d,%d,%d,%v; want an RGB", r, g, b, ok)
	}
	if err := h.SetThemeOverrides(nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.ThemeOverrides(); ok {
		t.Error("overrides not cleared")
	}
}
