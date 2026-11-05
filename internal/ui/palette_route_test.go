package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/theme"
)

// presetKeymap resolves key presses through a built-in keymap, as the app adapter
// does. The root tests use it so that Ctrl+P goes through the real macOS keymap.
type presetKeymap struct{ km *keymap.Map }

func (k presetKeymap) Resolve(kp KeyPress, ctx KeyContext) (Command, bool) {
	chord, err := keymap.Canonical(kp.Name, kp.Mods)
	if err != nil {
		return Command{}, false
	}
	kc := keymap.ContextNormal
	if ctx == KeyContextFileExplorer {
		kc = keymap.ContextFileExplorer
	}
	b, ok := k.km.Resolve(chord, kc)
	if !ok {
		return Command{}, false
	}
	return Command{Action: b.Action, Args: b.Args}, true
}

// newPresetModel returns a root model with the named keymap preset and a fake
// editor, sized w by h.
func newPresetModel(t *testing.T, preset string, ed *fakeEditor, w, h int) *Model {
	t.Helper()
	km, err := keymap.ByName(preset)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Editor: ed, Keys: presetKeymap{km: km}, Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// screenRows returns the rows of the root's View with the styles removed.
func screenRows(m *Model) []string {
	return strings.Split(ansi.Strip(m.View().Content), "\n")
}

func TestCtrlPOpensThePaletteThroughTheRoot(t *testing.T) {
	m := newPresetModel(t, "macos", newFakeEditor("hello world"), 100, 30)
	press(m, ctrl('p'))

	if len(m.overlays) != 1 {
		t.Fatalf("overlay stack has %d entries after Ctrl+P, want the palette", len(m.overlays))
	}
	o, ok := m.overlays[0].(paletteOverlay)
	if !ok || !o.Active() {
		t.Fatalf("top overlay = %T, want an active palette", m.overlays[0])
	}

	rows := screenRows(m)
	h := len(rows)
	if got := strings.TrimSpace(rows[h-1]); got != ">" {
		t.Errorf("query row = %q, want the palette prompt %q", got, ">")
	}
	if got := strings.TrimSpace(rows[h-2]); got != "file  |  >command  |  :line  |  #buffer" {
		t.Errorf("hint row = %q, want the palette hint", got)
	}
	if !strings.HasPrefix(rows[h-3], "└") {
		t.Errorf("row %d = %q, want the bottom border of the palette box", h-3, rows[h-3])
	}
	if !strings.Contains(strings.Join(rows, "\n"), "Clear Warnings") {
		t.Error("the command rows are not painted")
	}
}

func TestPaletteRowRunsItsActionInTheEditor(t *testing.T) {
	ed := newFakeEditor("hello world")
	m := newPresetModel(t, "macos", ed, 100, 30)
	press(m, ctrl('p'))
	for _, r := range "select all" {
		press(m, typed(string(r)))
	}
	cmd := press(m, special(tea.KeyEnter))
	if len(m.overlays) != 0 {
		t.Fatal("Enter left the palette open")
	}
	if cmd == nil {
		t.Fatal("Enter on a row returned no command")
	}
	m.Update(cmd())
	if !slices.Contains(ed.actions, editor.Action("select_all")) {
		t.Fatalf("editor actions = %v, want select_all from the palette row", ed.actions)
	}
}

func TestEscClosesThePaletteOpenedByCtrlP(t *testing.T) {
	m := newPresetModel(t, "macos", newFakeEditor("hello"), 100, 30)
	press(m, ctrl('p'))
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 0 {
		t.Fatalf("overlay stack has %d entries after Esc, want none", len(m.overlays))
	}
	if rows := screenRows(m); strings.TrimSpace(rows[len(rows)-1]) == ">" {
		t.Error("the query row is still painted after Esc")
	}
}
