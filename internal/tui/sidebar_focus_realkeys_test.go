package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/pluginapi"
)

// focusCycleKeys adds the real Alt+Shift+N/P chords to the test keymap, as the
// default keymap binds them for the sidebar focus cycle.
func focusCycleKeys() fakeKeymap {
	keys := testKeys()
	keys["normal|n|alt+shift"] = Command{Action: "focus_next_sidebar_section"}
	keys["normal|p|alt+shift"] = Command{Action: "focus_prev_sidebar_section"}
	return keys
}

// altShift is the terminal's Alt+Shift+<letter> key event; a real terminal reports
// the uppercase letter with both modifiers.
func altShift(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Mod: tea.ModAlt | tea.ModShift, Text: string(r)}
}

// TestSidebarFocusCycleRealKeysFromPluginPanel checks the real Update path: while a
// plugin section has the keyboard, Alt+Shift+N/P must leave it (the panel's Alt/Ctrl
// drop must not swallow the root action), moving to the next/previous rendered stop
// and clearing the panel's focus.
func TestSidebarFocusCycleRealKeysFromPluginPanel(t *testing.T) {
	t.Parallel()
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.keys = focusCycleKeys()
	mp := mountFocusSection(m, 5)
	m.focusPluginSection(mp)
	if m.plug.panels.focus != mp.id {
		t.Fatalf("setup: plugin not focused: %v", m.plug.panels.focus)
	}
	press(m, altShift('N'))
	if m.plug.panels.focus != 0 || mp.panel.Focused() {
		t.Fatalf("Alt+Shift+N was swallowed by the plugin panel: focus %v/%v", m.plug.panels.focus, mp.panel.Focused())
	}
	if !m.editorHasKeys() || m.explorerFocus || m.contentsFocus || m.diag.focus {
		t.Fatalf("Alt+Shift+N did not reach the editor stop: explorer %v contents %v diag %v", m.explorerFocus, m.contentsFocus, m.diag.focus)
	}
	// Backwards from the editor: the last rendered stop is the plugin section.
	press(m, altShift('P'))
	if m.plug.panels.focus != mp.id || !mp.panel.Focused() {
		t.Fatalf("Alt+Shift+P did not reach the plugin stop: %v", m.plug.panels.focus)
	}
	// And one more backwards: Contents, with the panel blurred.
	press(m, altShift('P'))
	if !m.contentsFocus || m.plug.panels.focus != 0 {
		t.Fatalf("Alt+Shift+P did not move from the plugin to Contents: contents %v plugin %v", m.contentsFocus, m.plug.panels.focus)
	}
}

// TestSidebarFocusCycleRealKeysWithPluginModeActive checks the second gate: a plugin
// input mode is active while the panel has the keyboard; the root cycle keys still
// reach the keymap (the plugin key layer passes them through), move focus and blur the
// panel, while the plugin-owned mode itself is not ended.
func TestSidebarFocusCycleRealKeysWithPluginModeActive(t *testing.T) {
	t.Parallel()
	e := newKeyEnv(t)
	e.m.keys = focusCycleKeys()
	mp := mountFocusSection(e.m, 6)
	e.m.focusPluginSection(mp)
	e.mode(t, pluginapi.ModeInfo{Name: "m", Text: pluginapi.TextSwallow, Bindings: map[string]string{"n": "insert_char"}})
	if e.m.plug.keys.editor == (modeRef{}) {
		t.Fatal("setup: plugin mode not active")
	}
	press(e.m, altShift('N'))
	if e.m.plug.panels.focus != 0 || mp.panel.Focused() {
		t.Fatalf("the plugin mode/panel swallowed Alt+Shift+N: focus %v", e.m.plug.panels.focus)
	}
	if e.m.stage() != stageEditor {
		t.Fatalf("Alt+Shift+N did not reach the editor stop: stage %v", e.m.stage())
	}
	if e.m.plug.keys.editor == (modeRef{}) {
		t.Fatal("the focus cycle cleared the plugin-owned mode")
	}
}
