package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/theme"
)

// pickerFromEditor opens the colour picker for the editor's highlighted key with
// 'p' and returns it.
func pickerFromEditor(t *testing.T, m *Model, page *themeEditorPage) *themeColorPicker {
	t.Helper()
	acceptEditorOverlay(t, m, press(m, typed("p")))
	top := m.overlays[len(m.overlays)-1]
	picker, ok := top.(*themeColorPicker)
	if !ok {
		t.Fatalf("the top overlay is %T, want the colour picker", top)
	}
	return picker
}

// pickerChip maps a swatch index to a screen cell after Draw laid the grid out.
func pickerChip(t *testing.T, picker *themeColorPicker) func(i int) (int, int) {
	t.Helper()
	g := grid.New(100, 30)
	picker.Draw(g)
	if picker.box.W == 0 {
		t.Fatal("the picker has no box")
	}
	return func(i int) (int, int) {
		return picker.gridX + (i%picker.cols)*3, picker.gridY + i/picker.cols
	}
}

// TestThemePickerOpensFromKeyAndSwatch checks both entry points: 'p' on a key row,
// and a left click on that row's swatch in the editor.
func TestThemePickerOpensFromKeyAndSwatch(t *testing.T) {
	t.Parallel()
	m, page := editorWithCatalog(t, nil)
	page.sel = editorRowIndex(t, page, "editor.bg")
	picker := pickerFromEditor(t, m, page)
	if !picker.Active() || picker.key.Name() != "editor.bg" {
		t.Fatalf("picker = %+v, want the editor.bg picker", picker)
	}
	prior, _ := page.working.Value("editor", "bg")
	if len(picker.colors) == 0 || !sameTUIColor(picker.prior, prior) {
		t.Fatalf("picker prior = %+v (colors %d), want the working background", picker.prior, len(picker.colors))
	}
	// The editor is still under the picker, unchanged and not dirty.
	if !page.Active() || page.dirty {
		t.Error("opening the picker changed the editor or dirtied it")
	}

	// The mouse entry: a click on the row's swatch opens the picker too.
	m2, page2 := editorWithCatalog(t, nil)
	page2.sel = editorRowIndex(t, page2, "editor.fg")
	g := grid.New(100, 30)
	page2.Draw(g)
	y := page2.box.Y + 1 + (page2.sel - page2.top)
	_, cmd := m2.Update(tea.MouseClickMsg{X: page2.box.X + 2, Y: y, Button: tea.MouseLeft})
	m2.Update(firstOf[themeEditorOverlayMsg](t, collect(cmd)))
	top := m2.overlays[len(m2.overlays)-1]
	if picker2, ok := top.(*themeColorPicker); !ok || picker2.key.Name() != "editor.fg" {
		t.Fatalf("click on the swatch opened %T, want the editor.fg picker", top)
	}
}

// TestThemePickerClickDragPreviewApply checks the mouse path and Enter: a click
// and a drag select and live-preview a colour (the frame and the editor rows), a
// release keeps the preview but does not commit, and Enter applies it as a normal
// colour edit.
func TestThemePickerClickDragPreviewApply(t *testing.T) {
	t.Parallel()
	m, page := editorWithCatalog(t, nil)
	page.sel = editorRowIndex(t, page, "editor.bg")
	picker := pickerFromEditor(t, m, page)
	chip := pickerChip(t, picker)
	if len(picker.colors) < 2 {
		t.Fatalf("palette has %d colours", len(picker.colors))
	}
	// Find two different candidates.
	first, second := 0, 1
	for second < len(picker.colors) && sameTUIColor(picker.colors[first], picker.colors[second]) {
		second++
	}
	if second >= len(picker.colors) {
		t.Skip("the theme has only one colour")
	}
	x, y := chip(first)
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if got, _ := page.working.Value("editor", "bg"); !sameTUIColor(got, picker.colors[first]) {
		t.Fatalf("click preview = %+v, want %+v", got, picker.colors[first])
	}
	x, y = chip(second)
	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	if got, _ := page.working.Value("editor", "bg"); !sameTUIColor(got, picker.colors[second]) {
		t.Fatalf("drag preview = %+v, want %+v", got, picker.colors[second])
	}
	m.Update(tea.MouseReleaseMsg{X: x, Y: y})
	if !picker.Active() {
		t.Fatal("the release closed the picker")
	}
	if _, ok := page.values["editor.bg"]; ok || page.dirty {
		t.Errorf("a mouse preview committed an edit: values %v dirty %v", page.values, page.dirty)
	}
	// Enter applies the previewed colour as a normal edit.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if picker.Active() {
		t.Fatal("Enter left the picker open")
	}
	want, _ := page.working.Value("editor", "bg")
	committed, ok := page.values["editor.bg"]
	if !ok || !sameTUIColor(committed, want) || !sameTUIColor(want, picker.colors[second]) || !page.dirty {
		t.Fatalf("applied colour = %+v (ok %v), want %+v committed and dirty", committed, ok, picker.colors[second])
	}
}

// TestThemePickerEscapeRestores checks the cancel path: after a preview, Esc puts
// the working theme back and closes without dirtying the editor; a click outside
// does the same.
func TestThemePickerEscapeRestores(t *testing.T) {
	t.Parallel()
	m, page := editorWithCatalog(t, nil)
	page.sel = editorRowIndex(t, page, "editor.bg")
	before, _ := page.working.Value("editor", "bg")
	picker := pickerFromEditor(t, m, page)
	chip := pickerChip(t, picker)
	for i := range picker.colors {
		if !sameTUIColor(picker.colors[i], before) {
			x, y := chip(i)
			m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			break
		}
	}
	if after, _ := page.working.Value("editor", "bg"); sameTUIColor(after, before) {
		t.Fatal("the preview did not change the working theme")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if picker.Active() {
		t.Fatal("Esc left the picker open")
	}
	if got, _ := page.working.Value("editor", "bg"); !sameTUIColor(got, before) {
		t.Fatalf("Esc restored %+v, want %+v", got, before)
	}
	if page.dirty || len(page.values) != 0 {
		t.Errorf("Esc left edits: values %v dirty %v", page.values, page.dirty)
	}

	// A click outside the grid restores and closes too.
	m2, page2 := editorWithCatalog(t, nil)
	page2.sel = editorRowIndex(t, page2, "editor.bg")
	before2, _ := page2.working.Value("editor", "bg")
	picker2 := pickerFromEditor(t, m2, page2)
	chip2 := pickerChip(t, picker2)
	for i := range picker2.colors {
		if !sameTUIColor(picker2.colors[i], before2) {
			x, y := chip2(i)
			m2.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			break
		}
	}
	m2.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if picker2.Active() {
		t.Fatal("a click outside left the picker open")
	}
	if got, _ := page2.working.Value("editor", "bg"); !sameTUIColor(got, before2) {
		t.Fatalf("outside click restored %+v, want %+v", got, before2)
	}
}

// TestThemePickerPalette checks the candidate colours: the working theme's own
// values first, all distinct, then the named ANSI colours; the labels are the
// theme's hex forms.
func TestThemePickerPalette(t *testing.T) {
	t.Parallel()
	dark, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	palette := pickerPalette(dark)
	if len(palette) == 0 {
		t.Fatal("the palette is empty")
	}
	// The palette leads with the working theme's own colours: its first chip is the
	// first key's value (the key table order starts with editor.bg).
	firstKey := dark.Keys()[0]
	firstValue, _ := dark.Value(firstKey.Section, firstKey.Key)
	if !sameTUIColor(palette[0], firstValue) {
		t.Fatalf("palette first chip = %+v, want the first theme key's %+v", palette[0], firstValue)
	}
	seen := map[string]bool{}
	for _, c := range palette {
		id := c.String()
		if seen[id] {
			t.Errorf("duplicate palette colour %s", id)
		}
		seen[id] = true
		if c.String() == "" {
			t.Errorf("palette entry has no display form: %+v", c)
		}
	}
	// The named colours are present even when the theme does not use one.
	named, err := theme.ParseColorText("Red")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range palette {
		if sameTUIColor(c, named) {
			found = true
		}
	}
	if !found {
		t.Error("the named Red is not in the picker palette")
	}
}
