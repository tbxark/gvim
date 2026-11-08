package tui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// newInputEdgeModel is a model with an explorer, a plugin runtime and the plugin
// events the input-platform tests subscribe to.
func newInputEdgeModel(t *testing.T) (*Model, *fakeRuntime) {
	t.Helper()
	rt := newFakeRuntime(t)
	rt.wants["chrome_focus_changed"] = true
	rt.wants["key_release"] = true
	ed := newFakeEditor("hello")
	ed.quitAction = "quit"
	m := New(Deps{
		Editor: ed, Explorer: newFakeTree(3), ShowExplorer: true, HideMenuBar: true,
		Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock,
		Root: RootPorts{Plugins: rt},
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	useHost(rt, m)
	rt.emitted = nil
	return m, rt
}

// TestChromeFocusChangedEvent walks the chrome with F6: editor -> tab strip ->
// explorer -> editor, and checks one chrome_focus_changed per region change, with
// gvim's tab strip as the extension region "tabs".
func TestChromeFocusChangedEvent(t *testing.T) {
	m, rt := newInputEdgeModel(t)
	want := []string{
		"chrome_focus_changed region=tabs",
		"chrome_focus_changed region=explorer",
		"chrome_focus_changed region=editor",
	}
	for i, step := range want {
		press(m, special(tea.KeyF6))
		m.pluginPump()
		if got := evRows(rt.emitted); len(got) != 1 || got[0] != step {
			t.Fatalf("step %d (F6): events = %v, want [%s]", i, got, step)
		}
		rt.emitted = nil
	}
	// A key that does not move the chrome raises nothing.
	press(m, typed("x"))
	m.pluginPump()
	if got := evRows(rt.emitted); len(got) != 0 {
		t.Fatalf("a plain key raised %v", got)
	}
}

// TestKeyReleaseEvent checks the plugin-visible release event: forwarded when a
// release message arrives, absent without subscribers, and never typing.
func TestKeyReleaseEvent(t *testing.T) {
	m, rt := newInputEdgeModel(t)
	m.Update(tea.KeyReleaseMsg{Code: 'a', Text: "a", Mod: tea.ModShift})
	m.pluginPump()
	want := []string{"key_release alt=false ctrl=false key=a shift=true super=false text=a"}
	if got := evRows(rt.emitted); !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}

	// Without a subscriber the release is dropped before it is queued.
	rt.wants["key_release"] = false
	rt.emitted = nil
	m.Update(tea.KeyReleaseMsg{Code: 'b'})
	m.pluginPump()
	if got := evRows(rt.emitted); len(got) != 0 {
		t.Fatalf("unsubscribed key_release = %v", got)
	}
}

// TestTranslateKeyLegacyModifiers covers the terminal edges Fresh normalizes:
// Shift inferred for a legacy uppercase letter (unless CONTROL is set, so
// CapsLock+Ctrl+A keeps firing Ctrl+A), CapsLock/NumLock lock bits ignored as
// chord modifiers, and Windows AltGr (reported as Ctrl+Alt) treated as text.
func TestTranslateKeyLegacyModifiers(t *testing.T) {
	tests := []struct {
		name    string
		key     tea.Key
		want    string
		mods    []string
		text    string
		hasText bool
	}{
		{"legacy uppercase infers shift", tea.Key{Code: 'P', Text: "P"}, "p", []string{"shift"}, "P", true},
		{"caps lock uppercase infers shift", tea.Key{Code: 'P', Text: "P", Mod: tea.ModCapsLock}, "p", []string{"shift"}, "P", true},
		{"control wins over inferred shift", tea.Key{Code: 'P', Text: "P", Mod: tea.ModCtrl}, "p", []string{"ctrl"}, "", false},
		{"lowercase stays unshifted", tea.Key{Code: 'p', Text: "p"}, "p", nil, "p", true},
		{"altgr is text", tea.Key{Code: '@', Text: "@", Mod: tea.ModCtrl | tea.ModAlt}, "@", []string{"ctrl", "alt"}, "@", true},
		{"altgr with caps lock lock bit", tea.Key{Code: '@', Text: "@", Mod: tea.ModCtrl | tea.ModAlt | tea.ModCapsLock}, "@", []string{"ctrl", "alt"}, "@", true},
		{"ctrl-alt without printable text is not text", tea.Key{Code: 'x', Text: "ab", Mod: tea.ModCtrl | tea.ModAlt}, "x", []string{"ctrl", "alt"}, "", false},
		{"command is not altgr text", tea.Key{Code: '@', Text: "@", Mod: tea.ModCtrl | tea.ModAlt | tea.ModSuper}, "@", []string{"ctrl", "alt", "super"}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kp, text := translateKey(tt.key)
			if kp.Name != tt.want {
				t.Errorf("name = %q, want %q", kp.Name, tt.want)
			}
			if !slices.Equal(kp.Mods, tt.mods) {
				t.Errorf("mods = %v, want %v", kp.Mods, tt.mods)
			}
			if (text != "") != tt.hasText || text != tt.text {
				t.Errorf("text = %q, want %q (hasText %v)", text, tt.text, tt.hasText)
			}
		})
	}
}

var _ = pluginapi.Key{}

// TestChromeFocusRegions covers the mapping chromeFocusOf uses beyond the F6
// walk: the tab strip extension, the explorer, a native section (Contents,
// Diagnostics), and a mounted plugin section with its plugin/panel id. The dock
// branch is the same predicate the dock tests exercise (dock.Focus()).
func TestChromeFocusRegions(t *testing.T) {
	m, rt := newInputEdgeModel(t)
	tests := []struct {
		name  string
		set   func()
		focus chromeFocus
	}{
		{"editor", func() {}, chromeFocus{region: "editor"}},
		{"tabs", func() { m.tabFocus = true }, chromeFocus{region: "tabs"}},
		{"explorer", func() { m.tabFocus = false; m.explorerFocus = true }, chromeFocus{region: "explorer"}},
		{"native section (contents)", func() { m.explorerFocus = false; m.contentsFocus = true }, chromeFocus{region: "section"}},
		{"native section (diagnostics)", func() { m.contentsFocus = false; m.diag.focus = true }, chromeFocus{region: "section"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.set()
			if got := m.chromeFocusOf(); got != tt.focus {
				t.Fatalf("chromeFocusOf = %+v, want %+v", got, tt.focus)
			}
		})
	}
	// A mounted plugin section names its owner and panel id, and the event carries both.
	m.diag.focus = false
	m.plug.panels.byID = map[pluginapi.PanelID]*mountedPanel{7: {id: 7, owner: "p"}}
	m.plug.panels.focus = 7
	want := chromeFocus{region: "section", plugin: "p", panelID: 7}
	if got := m.chromeFocusOf(); got != want {
		t.Fatalf("plugin section = %+v, want %+v", got, want)
	}
	rt.emitted = nil
	m.emitChromeFocusChanged(chromeFocus{}, want)
	m.pluginPump()
	if got := evRows(rt.emitted); len(got) != 1 || got[0] != "chrome_focus_changed panel_id=7 plugin=p region=section" {
		t.Fatalf("plugin-section event = %v", got)
	}
}
