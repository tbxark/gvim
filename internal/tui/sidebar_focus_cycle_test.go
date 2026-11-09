package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/pluginpanel"
	"github.com/tbxark/gvim/internal/tui/sidebar"
	"github.com/tbxark/gvim/pluginapi"
)

// mountFocusSection mounts a real plugin sidebar section so the focus cycle can
// enter it (the panel needs a Focused state and a focus ring).
func mountFocusSection(m *Model, id pluginapi.PanelID) *mountedPanel {
	mp := &mountedPanel{id: id, owner: "p", mount: pluginapi.WidgetMount{
		Placement: pluginapi.PlacementSidebar,
		Rows:      5,
		Closable:  true,
	}, panel: pluginpanel.New(pluginapi.WidgetPanel{}, pluginpanel.Options{})}
	m.plug.panels.add(mp)
	if !m.explorerOpen {
		m.explorerOpen = true
	}
	m.syncViewport()
	return mp
}

// TestFocusNextSidebarSectionVisitsRenderedOrder checks the command path
// (focus_next_sidebar_section / focus_prev_sidebar_section): it follows the column's
// rendered order — explorer, Contents, Diagnostics, each plugin section — then the
// editor, and walks backwards the same way. Diagnostics folded is unfolded by the
// focus, as the native focus commands already do.
func TestFocusNextSidebarSectionVisitsRenderedOrder(t *testing.T) {
	t.Parallel()
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.diag.open = true
	m.diag.collapsed = true
	one := mountFocusSection(m, 1)
	two := mountFocusSection(m, 2)

	steps := []struct {
		name  string
		check func() bool
	}{
		{"explorer", func() bool { return m.explorerFocus }},
		{"contents", func() bool { return m.contentsFocus }},
		{"diagnostics", func() bool { return m.diag.focus && !m.diag.Collapsed() }},
		{"plugin 1", func() bool { return m.plug.panels.focus == one.id && one.panel.Focused() }},
		{"plugin 2", func() bool { return m.plug.panels.focus == two.id && two.panel.Focused() }},
		{"editor", func() bool {
			return !m.explorerFocus && !m.contentsFocus && !m.diag.focus && m.plug.panels.focus == 0
		}},
	}
	for i, step := range steps {
		if err := m.Run("focus_next_sidebar_section"); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if !step.check() {
			t.Fatalf("step %d: focus is not on %s (explorer %v contents %v diag %v plugin %v)",
				i, step.name, m.explorerFocus, m.contentsFocus, m.diag.focus, m.plug.panels.focus)
		}
	}
	// Backwards the same rendered order.
	back := []struct {
		name  string
		check func() bool
	}{
		{"plugin 2", func() bool { return m.plug.panels.focus == two.id }},
		{"plugin 1", func() bool { return m.plug.panels.focus == one.id }},
		{"diagnostics", func() bool { return m.diag.focus }},
		{"contents", func() bool { return m.contentsFocus }},
		{"explorer", func() bool { return m.explorerFocus }},
		{"editor", func() bool { return !m.explorerFocus && !m.contentsFocus && !m.diag.focus }},
	}
	for i, step := range back {
		if err := m.Run("focus_prev_sidebar_section"); err != nil {
			t.Fatalf("back step %d: %v", i, err)
		}
		if !step.check() {
			t.Fatalf("back step %d: focus is not on %s (explorer %v contents %v diag %v plugin %v)",
				i, step.name, m.explorerFocus, m.contentsFocus, m.diag.focus, m.plug.panels.focus)
		}
	}
}

// TestSidebarFocusBlursThePluginKeyLayer checks that entering a plugin section takes
// the keyboard (its keys are swallowed while focused), and that leaving it by the
// command cycle, by the editor stop or by F6 hands the keyboard back: the old plugin
// no longer swallows the key.
func TestSidebarFocusBlursThePluginKeyLayer(t *testing.T) {
	t.Parallel()
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	mountFocusSection(m, 3)
	// Editor -> explorer -> Contents -> plugin.
	for range 3 {
		if err := m.Run("focus_next_sidebar_section"); err != nil {
			t.Fatal(err)
		}
	}
	if m.plug.panels.focus != 3 {
		t.Fatalf("the cycle did not reach the plugin section: %v", m.plug.panels.focus)
	}
	ed := m.ed.(*tocEditor)
	ed.actions = nil
	press(m, typed("x"))
	if len(ed.actions) != 0 {
		t.Fatalf("a focused plugin section let the key reach the editor: %v", ed.actions)
	}
	// The next command stop is the editor; the plugin key layer is gone.
	if err := m.Run("focus_next_sidebar_section"); err != nil {
		t.Fatal(err)
	}
	if m.plug.panels.focus != 0 {
		t.Fatalf("leaving the plugin section left its key layer focused: %v", m.plug.panels.focus)
	}
	press(m, typed("x"))
	if len(ed.actions) == 0 {
		t.Fatal("the editor did not get the key after the plugin section was left")
	}

	// F6 from a focused plugin section also blurs it (the F6 stage order itself is
	// unchanged; only the stale key layer is cleared).
	if err := m.Run("focus_next_sidebar_section"); err != nil { // editor -> explorer
		t.Fatal(err)
	}
	for range 2 {
		if err := m.Run("focus_next_sidebar_section"); err != nil {
			t.Fatal(err)
		}
	}
	if m.plug.panels.focus != 3 {
		t.Fatalf("setup: plugin is not focused: %v", m.plug.panels.focus)
	}
	press(m, special(tea.KeyF6))
	if m.plug.panels.focus != 0 {
		t.Fatalf("F6 from the plugin section left the plugin key layer focused: %v", m.plug.panels.focus)
	}
}

// TestSidebarFocusSkipsClosedNativeSections checks the cycle lists only rendered
// native sections: a closed Contents/Diagnostics section is not a stop, while a
// folded (rendered) one is and gets unfolded.
func TestSidebarFocusSkipsClosedNativeSections(t *testing.T) {
	t.Parallel()
	m := newTOCModel(t, newTOCEditor("a.txt", "not markdown"), nil)
	m.contents.SetOpen(false)
	m.diag.open = false
	steps := []struct {
		name  string
		check func() bool
	}{
		{"explorer", func() bool { return m.explorerFocus }},
		{"editor", func() bool { return !m.explorerFocus }},
	}
	for i, step := range steps {
		if err := m.Run("focus_next_sidebar_section"); err != nil {
			t.Fatal(err)
		}
		if !step.check() {
			t.Fatalf("step %d: expected %s", i, step.name)
		}
	}

	// Diagnostics open but folded is rendered and is unfolded when focused.
	other := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	other.diag.open = true
	other.diag.collapsed = true
	for range 3 { // editor -> explorer -> contents -> diagnostics
		if err := other.Run("focus_next_sidebar_section"); err != nil {
			t.Fatal(err)
		}
	}
	if !other.diag.focus || other.diag.Collapsed() {
		t.Fatalf("diagnostics focus = %v collapsed %v, want focused and unfolded", other.diag.focus, other.diag.Collapsed())
	}
	_ = sidebar.KindDiagnostics // the rendered order is checked through layout above
}
