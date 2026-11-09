package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// remoteMenuSetup is one focus scene from which Fresh's global F6 must open the
// Remote Indicator menu.
type remoteMenuSetup struct {
	name  string
	setup func(t *testing.T) *Model
}

// TestF6OpensRemoteIndicatorMenuEverywhere checks Fresh's F6 semantics on the real
// key path: the global Remote Indicator menu opens from the editor and every chrome
// focus scene (tab strip, explorer, Contents, Diagnostics, dock, focused plugin
// section, open menu bar), reports the local workspace and that this session has no
// remote authority (the remote wording is covered by
// TestF6RemoteMenuShowsTheRemoteAuthority), and does not move the chrome focus.
func TestF6OpensRemoteIndicatorMenuEverywhere(t *testing.T) {
	t.Parallel()
	base := func(t *testing.T) *Model {
		return newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	}
	scenes := []remoteMenuSetup{
		{name: "editor", setup: base},
		{name: "tab strip", setup: func(t *testing.T) *Model {
			m := base(t)
			m.tabFocus = true
			return m
		}},
		{name: "explorer", setup: func(t *testing.T) *Model {
			m := base(t)
			m.takeExplorerFocus()
			return m
		}},
		{name: "contents", setup: func(t *testing.T) *Model {
			m := base(t)
			m.takeContentsFocus()
			return m
		}},
		{name: "diagnostics", setup: func(t *testing.T) *Model {
			m := base(t)
			m.diag.open = true
			m.takeDiagFocus()
			return m
		}},
		{name: "dock", setup: func(t *testing.T) *Model {
			m := base(t)
			m.dock.SetFocus(true)
			return m
		}},
		{name: "focused plugin section", setup: func(t *testing.T) *Model {
			m := base(t)
			mp := mountFocusSection(m, 31)
			m.focusPluginSection(mp)
			return m
		}},
		{name: "open menu bar", setup: func(t *testing.T) *Model {
			m := base(t)
			m.menu = m.menu.Open(0)
			return m
		}},
	}
	for _, scene := range scenes {
		t.Run(scene.name, func(t *testing.T) {
			t.Parallel()
			m := scene.setup(t)
			stageBefore := m.stage()
			press(m, special(tea.KeyF6))
			if len(m.overlays) != 1 {
				t.Fatalf("F6 opened %d overlays, want the Remote Indicator menu", len(m.overlays))
			}
			if !frameHas(m, "Remote") || !frameRowHas(m, "Workspace", "Local") {
				t.Fatal("F6 menu does not report the local workspace")
			}
			if !frameHas(m, "No remote authority in this session") {
				t.Fatal("F6 local menu does not say this session has no remote authority")
			}
			if frameHas(m, "not supported") {
				t.Fatal("F6 local menu still claims remote workspaces are unsupported")
			}
			if got := m.stage(); got != stageBefore {
				t.Fatalf("F6 moved the chrome focus: stage %v -> %v", stageBefore, got)
			}
			// The menu is modal: Escape closes it and the previous focus remains.
			press(m, special(tea.KeyEscape))
			if len(m.overlays) != 0 {
				t.Fatalf("Escape left %d overlays open", len(m.overlays))
			}
		})
	}
}

// TestF6RemoteMenuLeavesAPluginSectionFocused checks F6 does not blur a focused
// plugin section: the Remote Indicator menu is a global popup, not a chrome focus
// move (the F7 chrome cycle is the key that leaves the section).
func TestF6RemoteMenuLeavesAPluginSectionFocused(t *testing.T) {
	t.Parallel()
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	mp := mountFocusSection(m, 32)
	m.focusPluginSection(mp)
	press(m, special(tea.KeyF6))
	if m.plug.panels.focus != mp.id || !mp.panel.Focused() {
		t.Fatalf("F6 blurred the plugin section: %v/%v", m.plug.panels.focus, mp.panel.Focused())
	}
	press(m, special(tea.KeyEscape))
	if m.plug.panels.focus != mp.id || !mp.panel.Focused() {
		t.Fatalf("closing the F6 menu blurred the plugin section: %v/%v", m.plug.panels.focus, mp.panel.Focused())
	}
}

// TestF6RemoteMenuShowsTheRemoteAuthority checks F6 from a remote workspace: the
// menu shows the actual SSH authority, its state and the remote root (and the local
// wording is not used).
func TestF6RemoteMenuShowsTheRemoteAuthority(t *testing.T) {
	t.Parallel()
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.fileRoot = "/remote/w"
	m.SetAuthorityHooks(AuthorityHooks{
		Status: func() AuthorityStatus {
			return AuthorityStatus{Text: "SSH:alice@build-host", Connected: true}
		},
	})
	m.takeExplorerFocus()
	press(m, special(tea.KeyF6))
	if len(m.overlays) != 1 {
		t.Fatal("F6 did not open the Remote Indicator menu")
	}
	if !frameHas(m, "Workspace: Remote") || !frameHas(m, "alice@build-host") {
		t.Fatal("the remote F6 menu does not show the SSH authority")
	}
	if !frameHas(m, "Connected") || !frameHas(m, "Remote root") || !frameHas(m, "/remote/w") {
		t.Fatal("the remote F6 menu does not show the state and remote root")
	}
	if frameHas(m, "Workspace: Local") {
		t.Fatal("the remote F6 menu calls the workspace local")
	}
}

// TestF6RemoteMenuFromTheTerminalDock checks the terminal path: with a running dock
// focused, the global F6 keymap binding still opens the Remote Indicator menu before
// the dock's key handler or the shell sees the key.
func TestF6RemoteMenuFromTheTerminalDock(t *testing.T) {
	t.Parallel()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: newFakeStarter()})
	m.keys = testKeys()
	dockSettle(t, m, m.runCommand("open_terminal_in_dock"))
	if !m.dock.Focus() {
		t.Fatal("setup: the terminal dock does not have the focus")
	}
	press(m, special(tea.KeyF6))
	if len(m.overlays) != 1 {
		t.Fatalf("F6 from the terminal dock opened %d overlays, want the Remote Indicator menu", len(m.overlays))
	}
	if !frameHas(m, "Workspace: Local") {
		t.Fatal("the terminal-dock F6 menu is not the Remote Indicator menu")
	}
	press(m, special(tea.KeyEscape))
}

// TestRemoteMenuIsAKeymapAction checks the action is configurable like any keymap
// binding: it can be moved to another global chord, F6 can be reassigned, and a more
// specific context binding wins over the global one. It also works from a focused
// plugin panel after the rebind (the panel key layer does not swallow it).
func TestRemoteMenuIsAKeymapAction(t *testing.T) {
	t.Parallel()
	rebuilt := func() fakeKeymap {
		keys := testKeys()
		delete(keys, "global|F6|")
		keys["global|F8|"] = Command{Action: "show_remote_indicator_menu"}
		return keys
	}
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.keys = rebuilt()
	press(m, special(tea.KeyF6))
	if len(m.overlays) != 0 {
		t.Fatal("F6 still opens the Remote Indicator menu after the rebind")
	}
	press(m, special(tea.KeyF8))
	if len(m.overlays) != 1 || !frameHas(m, "Workspace: Local") {
		t.Fatal("the action did not move to F8 through the keymap")
	}
	press(m, special(tea.KeyEscape))

	// F6 reassigned to another action runs that action, not the remote menu.
	keys := testKeys()
	keys["normal|F6|"] = Command{Action: "toggle_file_explorer"}
	m = newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.keys = keys
	wasOpen := m.explorerOpen
	press(m, special(tea.KeyF6))
	if len(m.overlays) != 0 {
		t.Fatal("the reassigned F6 still opened the Remote Indicator menu")
	}
	if m.explorerOpen == wasOpen {
		t.Fatal("the reassigned F6 did not run its own action")
	}

	// A normal-context binding shadows the global one in the editor, while a focused
	// plugin panel has no normal context and uses the global binding.
	keys = testKeys()
	delete(keys, "global|F6|")
	keys["global|F8|"] = Command{Action: "show_remote_indicator_menu"}
	keys["normal|F8|"] = Command{Action: "toggle_file_explorer"}
	m = newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.keys = keys
	before := m.explorerOpen
	press(m, special(tea.KeyF8))
	if len(m.overlays) != 0 || m.explorerOpen == before {
		t.Fatal("the normal-context F8 binding did not win in the editor")
	}
	mp := mountFocusSection(m, 41)
	m.focusPluginSection(mp)
	press(m, special(tea.KeyF8))
	if len(m.overlays) != 1 || !frameHas(m, "Workspace: Local") {
		t.Fatal("the rebind action did not reach a focused plugin panel")
	}
}

// exactFakeKeymap is fakeKeymap with exact-unbind tombstones, for the dock and
// remote-menu context-precedence tests.
type exactFakeKeymap struct {
	fakeKeymap
	unbound map[string]bool
}

func (k exactFakeKeymap) UnboundExact(key KeyPress, ctx KeyContext) bool {
	return k.unbound[string(ctx)+"|"+key.Name+"|"+strings.Join(key.Mods, "+")]
}

// TestF6TerminalContextRebindAndUnbind checks the exact dock/terminal precedence with
// the real key path: a Terminal F6 rebind runs instead of the global remote menu, and a
// Terminal F6 unbind swallows the key instead of falling through to the global F6.
func TestF6TerminalContextRebindAndUnbind(t *testing.T) {
	t.Parallel()
	openDock := func(t *testing.T, keys Keymap) *Model {
		t.Helper()
		m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: newFakeStarter()})
		m.keys = keys
		dockSettle(t, m, m.runCommand("open_terminal_in_dock"))
		if !m.dock.Focus() {
			t.Fatal("setup: the terminal dock does not have the focus")
		}
		return m
	}

	// A user Terminal F6 binding wins over the preset's Global F6.
	keys := testKeys()
	keys["terminal|F6|"] = Command{Action: "toggle_file_explorer"}
	m := openDock(t, exactFakeKeymap{fakeKeymap: keys})
	before := m.explorerOpen
	press(m, special(tea.KeyF6))
	if len(m.overlays) != 0 {
		t.Fatal("the global F6 opened the remote menu over the Terminal rebind")
	}
	if m.explorerOpen == before {
		t.Fatal("the Terminal F6 rebind did not run its own action")
	}

	// A user Terminal F6 unbind swallows F6: the Global binding must not take over.
	keys = testKeys()
	m = openDock(t, exactFakeKeymap{fakeKeymap: keys, unbound: map[string]bool{"terminal|F6|": true}})
	press(m, special(tea.KeyF6))
	if len(m.overlays) != 0 {
		t.Fatal("a Terminal F6 unbind fell through to the global remote menu")
	}
}

// TestRemoteActionKeepsNormalAndPanelInput checks the rebind does not disturb ordinary
// input: a typed character still reaches the editor, and a focused plugin panel keeps
// swallowing widget keys without opening the remote menu.
func TestRemoteActionKeepsNormalAndPanelInput(t *testing.T) {
	t.Parallel()
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.keys = testKeys()
	ed := m.ed.(*tocEditor)
	ed.actions = nil
	press(m, typed("x"))
	if len(m.overlays) != 0 {
		t.Fatal("a typed character opened the remote menu")
	}
	if len(ed.actions) == 0 {
		t.Fatal("a typed character did not reach the editor")
	}
	mp := mountFocusSection(m, 51)
	m.focusPluginSection(mp)
	before := len(ed.actions)
	press(m, typed("y"))
	if len(m.overlays) != 0 {
		t.Fatal("a typed character in a focused plugin panel opened the remote menu")
	}
	if len(ed.actions) != before {
		t.Fatal("a typed character in a focused plugin panel reached the editor")
	}
}

// TestF6ExactUnbindConsumesNormalAndFileExplorer checks the normal and file-explorer
// tombstones: an explicit unbind in the focused exact context consumes F6 instead of
// letting handleKey fall back to the global Remote Indicator binding.
func TestF6ExactUnbindConsumesNormalAndFileExplorer(t *testing.T) {
	t.Parallel()
	// Editor / normal context.
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.keys = exactFakeKeymap{fakeKeymap: testKeys(), unbound: map[string]bool{"normal|F6|": true}}
	press(m, special(tea.KeyF6))
	if len(m.overlays) != 0 {
		t.Fatal("a normal-context F6 unbind fell through to the global remote menu")
	}
	// The same model still allows a normal typed key through.
	if len(m.overlays) != 0 {
		t.Fatal("unexpected overlay after the normal input")
	}

	// File explorer context.
	m = newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.keys = exactFakeKeymap{fakeKeymap: testKeys(), unbound: map[string]bool{"file_explorer|F6|": true}}
	m.takeExplorerFocus()
	press(m, special(tea.KeyF6))
	if len(m.overlays) != 0 {
		t.Fatal("a file-explorer F6 unbind fell through to the global remote menu")
	}
	// A normal-context F6 (after leaving the explorer) is still the default action.
	m.focusEditor()
	press(m, special(tea.KeyF6))
	if len(m.overlays) != 1 || !frameHas(m, "Workspace: Local") {
		t.Fatal("the unbind in file_explorer also disabled the normal-context F6")
	}
}

// TestExactUnbindOnlyConsumesTheRemoteFallback checks the unbind scope: an exact
// tombstone consumes the chord only when the weaker contexts would otherwise run
// show_remote_indicator_menu. A printable unbound key must still type (normal and
// global), and a printable unbound in the terminal must still reach the shell.
func TestExactUnbindOnlyConsumesTheRemoteFallback(t *testing.T) {
	t.Parallel()
	typedKeys := func(unbound map[string]bool) exactFakeKeymap {
		return exactFakeKeymap{fakeKeymap: testKeys(), unbound: unbound}
	}

	// normal printable unbind: the key reaches the editor.
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.keys = typedKeys(map[string]bool{"normal|x|": true})
	ed := m.ed.(*tocEditor)
	ed.actions = nil
	press(m, typed("x"))
	if len(m.overlays) != 0 {
		t.Fatal("a normal printable unbind was consumed by the remote path")
	}
	if len(ed.actions) == 0 {
		t.Fatal("a normal printable unbind did not type into the editor")
	}

	// global printable unbind: the key reaches the editor too.
	m = newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.keys = typedKeys(map[string]bool{"global|x|": true})
	ed = m.ed.(*tocEditor)
	ed.actions = nil
	press(m, typed("x"))
	if len(m.overlays) != 0 {
		t.Fatal("a global printable unbind was consumed by the remote path")
	}
	if len(ed.actions) == 0 {
		t.Fatal("a global printable unbind did not type into the editor")
	}

	// terminal printable unbind: the key reaches the shell.
	starter := newFakeStarter()
	d := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	d.keys = typedKeys(map[string]bool{"terminal|x|": true})
	dockSettle(t, d, d.runCommand("open_terminal_in_dock"))
	dockSettle(t, d, press(d, typed("x")))
	if !strings.Contains(strings.Join(starter.term.writes, ""), "x") {
		t.Fatalf("a terminal printable unbind did not reach the shell: writes %q", starter.term.writes)
	}

	// F6 unbind still consumes the chord when Global F6 would run the remote action.
	m = newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.keys = typedKeys(map[string]bool{"normal|F6|": true})
	press(m, special(tea.KeyF6))
	if len(m.overlays) != 0 {
		t.Fatal("an unbind of the remote chord fell through to the Global action")
	}

	// The same holds after the action moves: unbind the new chord, its Global fallback
	// must not run either.
	keys := testKeys()
	delete(keys, "global|F6|")
	keys["global|F8|"] = Command{Action: "show_remote_indicator_menu"}
	m = newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.keys = exactFakeKeymap{fakeKeymap: keys, unbound: map[string]bool{"normal|F8|": true}}
	press(m, special(tea.KeyF8))
	if len(m.overlays) != 0 {
		t.Fatal("an unbind of the moved remote chord fell through to the Global action")
	}
}
