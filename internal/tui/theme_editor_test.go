package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/theme"
)

// editorWithCatalog opens the theme editor over a model with a fake catalog.
func editorWithCatalog(t *testing.T, catalog ThemeCatalog) (*Model, *themeEditorPage) {
	t.Helper()
	m := newThemeTestModel(t, catalog)
	m.openThemeEditor()
	top := m.overlays[len(m.overlays)-1]
	page, ok := top.(*themeEditorPage)
	if !ok {
		t.Fatalf("the top overlay is %T, want the theme editor", top)
	}
	return m, page
}

// editorRowIndex returns the row of a theme key in the editor.
func editorRowIndex(t *testing.T, page *themeEditorPage, key string) int {
	t.Helper()
	for i, row := range page.rows {
		if !row.header && row.key.Name() == key {
			return i
		}
	}
	t.Fatalf("theme key %q is not listed", key)
	return 0
}

// acceptEditorOverlay pushes the overlay the editor asked for.
func acceptEditorOverlay(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	m.Update(firstOf[themeEditorOverlayMsg](t, collect(cmd)))
}

func TestThemeEditorOpensAndPreviewsColorEdits(t *testing.T) {
	t.Parallel()
	m, page := editorWithCatalog(t, nil)
	if len(page.rows) < 10 {
		t.Fatalf("the editor lists %d rows, want the theme's keys", len(page.rows))
	}
	page.sel = editorRowIndex(t, page, "editor.bg")
	acceptEditorOverlay(t, m, press(m, special(tea.KeyEnter)))
	for _, r := range "#112233" {
		press(m, typed(string(r)))
	}
	cmd := press(m, special(tea.KeyEnter))
	m.Update(firstOf[prompt.Done](t, collect(cmd)))
	bg, _ := page.working.Value("editor", "bg")
	if !bg.IsRGB || bg.RGB != (theme.RGB{R: 0x11, G: 0x22, B: 0x33}) {
		t.Fatalf("working editor.bg = %+v, want #112233", bg)
	}
	if !page.dirty {
		t.Error("the editor is not dirty after an edit")
	}
	if m.theme.Name != "dark" {
		// The preview keeps the theme name, the colors are in memory.
		t.Errorf("theme name = %q, want dark", m.theme.Name)
	}
}

func TestThemeEditorEscapeRestoresTheOriginalTheme(t *testing.T) {
	t.Parallel()
	m, page := editorWithCatalog(t, nil)
	before, _ := m.theme.Value("editor", "bg")
	page.sel = editorRowIndex(t, page, "editor.bg")
	acceptEditorOverlay(t, m, press(m, special(tea.KeyEnter)))
	for _, r := range "#112233" {
		press(m, typed(string(r)))
	}
	m.Update(firstOf[prompt.Done](t, collect(press(m, special(tea.KeyEnter)))))
	// Esc opens the discard question (pushed through a message).
	cmd := press(m, special(tea.KeyEscape))
	acceptEditorOverlay(t, m, cmd)
	if len(m.overlays) < 2 {
		t.Fatal("Esc did not ask before discarding")
	}
	// Yes restores the originally applied theme and closes the editor.
	page.Update(prompt.Done{Tag: themeEditorDiscardTag{page: page}, Outcome: prompt.Yes})
	if page.Active() {
		t.Error("the editor stayed open after discarding")
	}
	after, _ := m.theme.Value("editor", "bg")
	if !sameTUIColor(after, before) {
		t.Fatalf("theme bg after discard = %+v, want the original %+v", after, before)
	}
}

func TestThemeEditorSaveAsFromABuiltin(t *testing.T) {
	t.Parallel()
	catalog := &fakeThemeCatalog{
		themes: map[string]theme.Theme{},
		refs:   map[string]string{"dark": "dark"},
	}
	m, page := editorWithCatalog(t, catalog)
	if !page.readOnly {
		t.Fatal("the dark builtin theme is not read-only in the editor")
	}
	acceptEditorOverlay(t, m, press(m, ctrl('s')))
	if !strings.Contains(m.notice, "bundled") {
		t.Errorf("notice = %q, want the bundled-theme hint", m.notice)
	}
	// The Save As prompt is on the stack: type a name and submit.
	for _, r := range "my-copy" {
		press(m, typed(string(r)))
	}
	cmd := press(m, special(tea.KeyEnter))
	done := firstOf[prompt.Done](t, collect(cmd))
	_, saveCmd := m.Update(done)
	save := firstOf[themeEditorSaveDoneMsg](t, collect(saveCmd))
	m.Update(save)
	if page.Active() {
		t.Error("the editor stayed open after a successful save")
	}
	if _, ok := catalog.themes["my-copy.json"]; !ok {
		t.Errorf("saved themes = %v, want my-copy.json", catalog.themes)
	}
	if !strings.Contains(m.notice, "Theme saved") {
		t.Errorf("notice = %q, want the save notice", m.notice)
	}
}

func TestThemeEditorEditAttributes(t *testing.T) {
	t.Parallel()
	m, page := editorWithCatalog(t, nil)
	page.sel = editorRowIndex(t, page, "editor.bg")
	acceptEditorOverlay(t, m, press(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}))
	for _, r := range "bold,underlined" {
		press(m, typed(string(r)))
	}
	m.Update(firstOf[prompt.Done](t, collect(press(m, special(tea.KeyEnter)))))
	bg, _ := page.working.Value("editor", "bg")
	if !strings.Contains(strings.Join(bg.Modifiers, ","), "bold") || !strings.Contains(strings.Join(bg.Modifiers, ","), "underlined") {
		t.Fatalf("editor.bg modifiers = %v, want bold and underlined", bg.Modifiers)
	}
}

// sameTUIColor compares two theme colors, attributes included.
func sameTUIColor(a, b theme.Color) bool {
	if a.IsRGB != b.IsRGB || a.RGB != b.RGB || a.Name != b.Name || len(a.Modifiers) != len(b.Modifiers) {
		return false
	}
	for i := range a.Modifiers {
		if a.Modifiers[i] != b.Modifiers[i] {
			return false
		}
	}
	return true
}

// TestThemeEditorPickerPreviewRaisesNoThemeChanged checks that a colour picker preview
// (one per arrow press) rebuilds the frame without telling plugins, and that applying
// the colour raises theme_changed once.
func TestThemeEditorPickerPreviewRaisesNoThemeChanged(t *testing.T) {
	t.Parallel()
	h := newEvHarness(t, "theme_changed")
	dark, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	h.m.theme, h.m.themeRef = dark, "dark"
	h.m.openThemeEditor()
	page, ok := h.m.overlays[len(h.m.overlays)-1].(*themeEditorPage)
	if !ok {
		t.Fatalf("the top overlay is %T, want the theme editor", h.m.overlays[len(h.m.overlays)-1])
	}
	row := page.rows[editorRowIndex(t, page, "editor.bg")]
	evRowsOf(h) // drain what opening queued
	for i := range 3 {
		page.previewValue(row.key, theme.Color{IsRGB: true, RGB: theme.RGB{R: byte(i), G: 1, B: 2}})
	}
	page.reapplyValues()
	wantRows(t, evRowsOf(h), nil)
	page.setColor("editor.bg", theme.Color{IsRGB: true, RGB: theme.RGB{R: 9, G: 9, B: 9}})
	if got := evRowsOf(h); len(got) != 1 || !strings.HasPrefix(got[0], "theme_changed") {
		t.Fatalf("events after applying = %v, want one theme_changed", got)
	}
}
