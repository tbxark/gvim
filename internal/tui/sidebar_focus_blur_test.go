package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/sidebar"
	"github.com/tbxark/gvim/pluginapi"
)

// TestNativeFocusEntriesBlurPluginPanel checks every native focus entry blurts a
// focused plugin section's widget-panel layer: the explorer/Contents/Diagnostics
// takes, the editor, and the mouse path through the explorer header.
func TestNativeFocusEntriesBlurPluginPanel(t *testing.T) {
	t.Parallel()
	entries := []struct {
		name  string
		focus func(*Model)
	}{
		{"takeExplorerFocus", func(m *Model) { m.takeExplorerFocus() }},
		{"takeContentsFocus", func(m *Model) { m.takeContentsFocus() }},
		{"takeDiagFocus", func(m *Model) { m.takeDiagFocus() }},
		{"focusEditor", func(m *Model) { m.focusEditor() }},
		{"mouse explorer header", func(m *Model) {
			l := m.layout()
			m.Update(click(l.ExX+3, l.Sections[0].HeaderY, tea.MouseLeft))
		}},
		{"mouse Contents header", func(m *Model) {
			l := m.layout()
			cs, ok := contentsSection(l)
			if !ok {
				t.Fatal("no Contents section")
			}
			m.Update(click(l.ExX+3, cs.HeaderY, tea.MouseLeft))
		}},
	}
	for _, tt := range entries {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
			m.diag.open = true
			mp := mountFocusSection(m, 9)
			m.focusPluginSection(mp)
			if m.plug.panels.focus != mp.id || !mp.panel.Focused() {
				t.Fatalf("setup: plugin not focused: %v/%v", m.plug.panels.focus, mp.panel.Focused())
			}
			tt.focus(m)
			if m.plug.panels.focus != 0 || mp.panel.Focused() {
				t.Fatalf("native focus %s left the plugin panel focused: %v/%v", tt.name, m.plug.panels.focus, mp.panel.Focused())
			}
		})
	}
}

// TestPluginModeAndWaitDoNotSeeNativeSectionKeys checks the plugin mode and key
// capture are gated by the native chrome's key ownership: Contents/Diagnostics
// focus keeps its keys even with a plugin mode active or a key capture pending.
// The mode and wait themselves are plugin state: they are not cleared by a native
// focus change (documented), and become active again when the editor gets the keys.
func TestPluginModeAndWaitDoNotSeeNativeSectionKeys(t *testing.T) {
	t.Parallel()
	e := newKeyEnv(t)
	e.mode(t, pluginapi.ModeInfo{Name: "m", Bindings: map[string]string{"right": "cursor_right"}})

	key := special(tea.KeyRight)
	if _, taken := e.m.pluginKey(tea.Key(key)); !taken {
		t.Fatal("setup: the editor mode did not take the key with the editor focused")
	}
	// Contents section focus: the plugin mode must not see the key.
	e.m.contents.SetOpen(true)
	e.m.takeContentsFocus()
	if _, taken := e.m.pluginKey(tea.Key(key)); taken {
		t.Fatal("a plugin mode took a key while the Contents section had the keyboard")
	}
	// Diagnostics section focus: same.
	e.m.diag.open = true
	e.m.takeDiagFocus()
	if _, taken := e.m.pluginKey(tea.Key(key)); taken {
		t.Fatal("a plugin mode took a key while Diagnostics had the keyboard")
	}
	// Native focus does not clear the plugin-owned mode; it owns the editor again.
	if e.m.plug.keys.editor == (modeRef{}) {
		t.Fatal("native focus cleared the plugin input mode")
	}
	e.m.focusEditor()
	if _, taken := e.m.pluginKey(tea.Key(key)); !taken {
		t.Fatal("the plugin mode was not active again after the editor regained the keys")
	}

	// A pending key capture is gated exactly like the mode: not consumed while a
	// native section has the keyboard, captured when the editor gets it back.
	e.m.plug.waits = map[pluginapi.Token]string{99: "key"}
	e.m.plug.keys.wait = 99
	e.m.takeContentsFocus()
	if _, taken := e.m.pluginKey(tea.Key(key)); taken {
		t.Fatal("a pending key capture took a key while the Contents section had the keyboard")
	}
	if e.m.plug.keys.wait != 99 {
		t.Fatalf("native focus consumed the pending key capture: %d", e.m.plug.keys.wait)
	}
	e.m.focusEditor()
	if _, taken := e.m.pluginKey(tea.Key(key)); !taken {
		t.Fatal("the pending key capture did not take the key after the editor regained the keyboard")
	}
	if e.m.plug.keys.wait != 0 {
		t.Fatalf("the captured key did not finish the wait: %d", e.m.plug.keys.wait)
	}
}

// TestNativeFocusBlurKeepsHeaderGestureState checks the blur is limited to the panel
// layer: blurring does not fold a plugin section or cancel its header drag state.
func TestNativeFocusBlurKeepsHeaderGestureState(t *testing.T) {
	t.Parallel()
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	mp := mountFocusSection(m, 11)
	m.focusPluginSection(mp)
	m.takeContentsFocus()
	if m.plug.panels.collapsed[mp.id] {
		t.Fatal("the native focus folded the plugin section")
	}
	if got := mp.mount.Rows; got != 5 {
		t.Fatalf("the native focus changed the plugin rows: %d", got)
	}
}

var _ = sidebar.KindPlugin
