package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/menubar"
	"github.com/tbxark/gvim/theme"
)

// mnemonicKeymap is fakeKeymap with the keymap's menu accelerators: the port half of
// the menubar.Mnemonicer interface, as internal/keybindings' adapter provides it.
type mnemonicKeymap struct {
	fakeKeymap
	letters map[string]rune
}

func (k mnemonicKeymap) MenuMnemonic(name string) (rune, bool) {
	r, ok := k.letters[name]
	return r, ok
}

// menuMnemonicKeys is menuKeys with the accelerators of the default keymap.
func menuMnemonicKeys() mnemonicKeymap {
	keys := menuKeys()
	// gvim's own LSP menu has no Fresh counterpart; the keymap binds Alt+L to it.
	keys["normal|l|alt"] = Command{Action: "menu_open", Args: map[string]any{"name": "LSP"}}
	return mnemonicKeymap{
		fakeKeymap: keys,
		letters: map[string]rune{
			"File": 'f', "Edit": 'e', "View": 'v', "Selection": 's',
			"Go": 'g', "Help": 'h', "Explorer": 'x', "LSP": 'l',
		},
	}
}

// mnemonicModel returns a sized model with keys and the given view options.
func mnemonicModel(t *testing.T, keys Keymap, opts *ViewOptions) *Model {
	t.Helper()
	m := New(Deps{
		Editor: newFakeEditor("hello"), Explorer: newFakeTree(3), Keys: keys,
		Theme: theme.Theme{}, Clock: testClock, View: opts,
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

// menuLabelRun is one menu bar label as the menu bar's hit map places it.
type menuLabelRun struct {
	X, W int
	Text string
}

// menuLabelRuns reads the bar's labels back from the menu bar's own hit map, so a
// test does not repeat the bar's geometry.
func menuLabelRuns(m *Model) []menuLabelRun {
	var runs []menuLabelRun
	y := m.layout().MenuLabelY
	bar := m.menu.SetSize(m.width, m.height).SetRow(y).WithExplorerFocus(m.explorerFocus)
	for x := range m.width {
		t := bar.Hit(x, y)
		if t.Kind != menubar.TargetLabel {
			continue
		}
		if n := len(runs); n > 0 && runs[n-1].X+runs[n-1].W == x && runs[n-1].Text == menubar.Labels(m.explorerFocus)[t.Index] {
			runs[n-1].W++
			continue
		}
		runs = append(runs, menuLabelRun{X: x, W: 1, Text: menubar.Labels(m.explorerFocus)[t.Index]})
	}
	return runs
}

// labelCell returns the painted cell of the menu label called title, character i.
func labelCell(t *testing.T, m *Model, title string, i int) int {
	t.Helper()
	for _, r := range menuLabelRuns(m) {
		if r.Text == title {
			return r.X + i
		}
	}
	t.Fatalf("no menu label %q on the bar", title)
	return 0
}

// TestMenuBarUnderlinesTheKeymapMnemonics checks the root wiring: the bar asks the
// keymap (through menubar.Mnemonicer) for each label's accelerator and underlines the
// matching character, so Alt+F's File label marks the F.
func TestMenuBarUnderlinesTheKeymapMnemonics(t *testing.T) {
	m := mnemonicModel(t, menuMnemonicKeys(), nil)
	g := m.paint()
	for title, index := range map[string]int{"File": 0, "Edit": 0, "View": 0, "Selection": 0, "Go": 0, "Help": 0, "LSP": 0} {
		x := labelCell(t, m, title, index)
		if !g.At(x, m.layout().MenuLabelY).Style.Underline {
			t.Errorf("%s: the mnemonic letter is not underlined", title)
		}
	}
}

// TestMenuBarMnemonicsOffDropsUnderlinesAndChords checks editor.menu_bar_mnemonics
// off: no underlined letters, and an Alt+letter that only the menu binding uses does
// nothing. F10 is not a mnemonic and still opens the bar.
func TestMenuBarMnemonicsOffDropsUnderlinesAndChords(t *testing.T) {
	m := mnemonicModel(t, menuMnemonicKeys(), &ViewOptions{
		ShowStatusBar:    true,
		MenuBarMnemonics: false,
	})
	g := m.paint()
	for _, r := range menuLabelRuns(m) {
		for i := range []rune(r.Text) {
			if g.At(r.X+i, m.layout().MenuLabelY).Style.Underline {
				t.Fatalf("%s: column %d underlines a letter with the setting off", r.Text, i)
			}
		}
	}
	press(m, altKey('f'))
	if m.menu.IsOpen() {
		t.Fatalf("Alt+F opened %q with menu_bar_mnemonics off", m.menu.Title())
	}
	press(m, special(tea.KeyF10))
	if !m.menu.IsOpen() {
		t.Fatal("F10 does not open the bar with menu_bar_mnemonics off")
	}
}

// TestAltLetterOpensTheMenuWhenMnemonicsAreOn checks the routing stays the existing
// keymap dispatch: with mnemonics on the keymap's alt+letter menu_open binding opens
// the dropdown, and the setting gates nothing else.
func TestAltLetterOpensTheMenuWhenMnemonicsAreOn(t *testing.T) {
	m := mnemonicModel(t, menuMnemonicKeys(), nil)
	press(m, altKey('f'))
	if got := m.menu.Title(); got != "File" {
		t.Fatalf("Alt+F opened %q, want File", got)
	}
	m.menu = m.menu.Close()
	press(m, altKey('l'))
	if got := m.menu.Title(); got != "LSP" {
		t.Fatalf("Alt+L opened %q, want LSP", got)
	}
}

// TestModelMenuMnemonicDelegates checks the port method the menubar asks: it answers
// from the keymap when the keymap provides the lookup, and no mnemonic otherwise.
func TestModelMenuMnemonicDelegates(t *testing.T) {
	m := mnemonicModel(t, menuMnemonicKeys(), nil)
	if r, ok := m.MenuMnemonic("File"); !ok || r != 'f' {
		t.Errorf("MenuMnemonic(File) = %q, %v; want 'f'", r, ok)
	}
	plain := mnemonicModel(t, testKeys(), nil)
	if _, ok := plain.MenuMnemonic("File"); ok {
		t.Error("a keymap without the lookup answered a mnemonic")
	}
	_ = menubar.Labels // the interface is menubar.Mnemonicer; keep the import honest
}

// TestThemeChangeKeepsTheMenuMnemonics checks that rebuilding the menu bar for a new
// theme keeps the accelerator underlines.
func TestThemeChangeKeepsTheMenuMnemonics(t *testing.T) {
	m := mnemonicModel(t, menuMnemonicKeys(), nil)
	m.applyTheme("nord")
	g := m.paint()
	x := labelCell(t, m, "File", 0)
	if !g.At(x, m.layout().MenuLabelY).Style.Underline {
		t.Error("a theme change dropped the menu mnemonic underline")
	}
}
